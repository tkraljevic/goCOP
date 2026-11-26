package ledger

import (
	"context"
	"errors"
	"testing"
)

// Potpisnik dobije verziju točno kakva ide u knjigu, a potpis i ključ se
// spremaju uz nju; vlastita verzija je primljena kad je nastala. Bez
// potpisnika verzija ide bez potpisa, a potpisnik koji ne uspije ne
// ostavlja verziju. Primljeni potpis se ne sprema.
func TestPotpisnikKnjige(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	var potpisana Version
	rec.PostaviPotpisnika(func(v Version) (string, string, error) {
		potpisana = v
		return "UA==", "Sw==", nil
	})
	if _, err := rec.RecordIn(ctx, db, "prijave/16/2026", "probni", "p1", map[string]string{"a": "b<c"}); err != nil {
		t.Fatal(err)
	}
	v, err := rec.Latest(ctx, "probni", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Potpis != "UA==" || v.KljucAutora != "Sw==" {
		t.Fatalf("potpis %q, ključ %q", v.Potpis, v.KljucAutora)
	}
	if potpisana.VersionID != v.VersionID || potpisana.NodeID != v.NodeID || potpisana.Channel != v.Channel ||
		string(potpisana.Payload) != string(v.Payload) || !potpisana.CreatedAt.Equal(v.CreatedAt) || potpisana.SchemaVersion != v.SchemaVersion {
		t.Errorf("potpisano nije isto što je u knjizi:\n%+v\n%+v", potpisana, *v)
	}
	var primljenoKadNastalo int
	if err := db.QueryRow(`SELECT primljeno = created_at FROM record_versions WHERE version_id = ?`, v.VersionID).Scan(&primljenoKadNastalo); err != nil || primljenoKadNastalo != 1 {
		t.Errorf("vlastita verzija: primljeno = nastalo %d, %v", primljenoKadNastalo, err)
	}

	rec.PostaviPotpisnika(nil)
	if _, err := rec.Record(ctx, db, "probni", "p2", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if v, err := rec.Latest(ctx, "probni", "p2"); err != nil || v.Potpis != "" || v.KljucAutora != "" {
		t.Errorf("bez potpisnika: %+v, %v", v, err)
	}

	kvar := errors.New("nema ključa")
	rec.PostaviPotpisnika(func(Version) (string, string, error) { return "", "", kvar })
	if _, err := rec.Record(ctx, db, "probni", "p3", map[string]string{}); !errors.Is(err, kvar) {
		t.Errorf("potpisnik ne uspije: %v", err)
	}
	if _, err := rec.Latest(ctx, "probni", "p3"); !errors.Is(err, ErrNoVersion) {
		t.Errorf("verzija ostala bez potpisa: %v", err)
	}

	stigla := fiksna("01950000-0000-7000-8000-000000000009", "probni", "p4", "laptop-a", "", pocetak)
	stigla.Potpis, stigla.KljucAutora = "UA==", "Sw=="
	if n, err := rec.Apply(ctx, []Version{stigla}); err != nil || n != 1 {
		t.Fatalf("primanje: %d, %v", n, err)
	}
	var potpis string
	var primljeno int
	if err := db.QueryRow(`SELECT potpis, primljeno IS NOT NULL FROM record_versions WHERE version_id = ?`, stigla.VersionID).Scan(&potpis, &primljeno); err != nil {
		t.Fatal(err)
	}
	if potpis != "" || primljeno != 1 {
		t.Errorf("primljena verzija: potpis %q, vrijeme primanja upisano %d", potpis, primljeno)
	}
}
