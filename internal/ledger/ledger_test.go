package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(Schema); err != nil {
		t.Fatalf("shema knjige verzija: %v", err)
	}
	return db
}

type probni struct {
	Naziv string `json:"naziv"`
	Prag  int    `json:"prag"`
}

func TestSvakaIzmjenaJeNovaVerzijaIznadPrethodne(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	ctx := context.Background()

	v1, err := rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 600})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 610})
	if err != nil {
		t.Fatal(err)
	}

	latest, err := rec.Latest(ctx, "stations", "st-1")
	if err != nil {
		t.Fatal(err)
	}
	if latest.VersionID != v2 {
		t.Errorf("na površini je %s, očekivano %s", latest.VersionID, v2)
	}
	if latest.Supersedes != v1 {
		t.Errorf("druga verzija mora pokazivati na prvu: %q", latest.Supersedes)
	}

	history, err := rec.History(ctx, "stations", "st-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("povijest ima %d verzija, očekivano 2", len(history))
	}
	var first probni
	if err := json.Unmarshal(history[1].Payload, &first); err != nil || first.Prag != 600 {
		t.Errorf("starija verzija mora čuvati stari sadržaj, dobiveno %+v", first)
	}
}

func TestVracanjeStarijeVerzijeJeNovaVerzija(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	ctx := context.Background()

	rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 600})
	rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 610})

	// vraćanje = starija verzija se ponovno upiše na vrh
	history, _ := rec.History(ctx, "stations", "st-1")
	var older probni
	json.Unmarshal(history[1].Payload, &older)
	if _, err := rec.Record(ctx, db, "stations", "st-1", older); err != nil {
		t.Fatal(err)
	}

	history, _ = rec.History(ctx, "stations", "st-1")
	if len(history) != 3 {
		t.Fatalf("vraćanje mora dodati verziju, ne obrisati: %d", len(history))
	}
	var top probni
	json.Unmarshal(history[0].Payload, &top)
	if top.Prag != 600 {
		t.Errorf("na površini mora biti vraćeni sadržaj (600), dobiveno %d", top.Prag)
	}
}

func TestArhiviranjeCuvaSadrzaj(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	ctx := context.Background()

	rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 600})
	if _, err := rec.Archive(ctx, db, "stations", "st-1", probni{"Županja", 600}); err != nil {
		t.Fatal(err)
	}

	latest, _ := rec.Latest(ctx, "stations", "st-1")
	if !latest.Archived {
		t.Error("najnovija verzija mora biti arhivirana")
	}
	var p probni
	if err := json.Unmarshal(latest.Payload, &p); err != nil || p.Naziv != "Županja" {
		t.Error("arhivirana verzija mora čuvati sadržaj")
	}
}

// Razmjena između čvorova: samo dodavanje, dvaput primljeno ne mijenja ništa,
// a na površini završi verzija s većim identifikatorom bez obzira na
// redoslijed primanja
func TestRazmjenaJeIdempotentnaINeOvisiORedoslijedu(t *testing.T) {
	ctx := context.Background()

	dbA := openTestDB(t)
	dbB := openTestDB(t)
	a := New(dbA, "cop-osijek")
	b := New(dbB, "laptop-vinkovci")

	// oba čvora rade na istom zapisu bez mreže
	a.Record(ctx, dbA, "stations", "st-1", probni{"Županja", 600})
	b.Record(ctx, dbB, "stations", "st-1", probni{"Županja", 650})

	fromA, _ := a.Since(ctx, "", 0)
	fromB, _ := b.Since(ctx, "", 0)

	// B prima A, pa A prima B — i to dvaput
	if n, err := b.Apply(ctx, fromA); err != nil || n != 1 {
		t.Fatalf("B prima A: n=%d err=%v", n, err)
	}
	if n, err := b.Apply(ctx, fromA); err != nil || n != 0 {
		t.Fatalf("drugi primitak mora biti bez učinka: n=%d err=%v", n, err)
	}
	if n, err := a.Apply(ctx, fromB); err != nil || n != 1 {
		t.Fatalf("A prima B: n=%d err=%v", n, err)
	}

	latestA, _ := a.Latest(ctx, "stations", "st-1")
	latestB, _ := b.Latest(ctx, "stations", "st-1")
	if latestA.VersionID != latestB.VersionID {
		t.Errorf("čvorovi se ne slažu tko je na površini: A=%s B=%s", latestA.VersionID, latestB.VersionID)
	}

	histA, _ := a.History(ctx, "stations", "st-1")
	histB, _ := b.History(ctx, "stations", "st-1")
	if len(histA) != 2 || len(histB) != 2 {
		t.Errorf("oba čvora moraju imati obje verzije: A=%d B=%d", len(histA), len(histB))
	}
}

func TestSinceVracaSamoNovije(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	ctx := context.Background()

	v1, _ := rec.Record(ctx, db, "stations", "st-1", probni{"A", 1})
	rec.Record(ctx, db, "stations", "st-2", probni{"B", 2})
	rec.Record(ctx, db, "sections", "D.1.1", probni{"C", 3})

	newer, err := rec.Since(ctx, v1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(newer) != 2 {
		t.Errorf("nakon prve verzije očekujem 2 novije, dobiveno %d", len(newer))
	}
	for _, v := range newer {
		if v.VersionID <= v1 {
			t.Errorf("verzija %s nije novija od %s", v.VersionID, v1)
		}
	}

	counts, _ := rec.Count(ctx)
	if counts["stations"] != 2 || counts["sections"] != 1 {
		t.Errorf("brojanje po entitetu: %v", counts)
	}
}

func TestVerzijaBezIdentifikatoraSeOdbija(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	if _, err := rec.Record(context.Background(), db, "stations", "", probni{}); err == nil {
		t.Error("verzija bez identifikatora zapisa morala je biti odbijena")
	}
	if _, err := rec.Latest(context.Background(), "stations", "nema"); err != ErrNoVersion {
		t.Errorf("zapis bez verzija mora vratiti ErrNoVersion, dobiveno %v", err)
	}
}
