package kisomjeri

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
)

// citacMjerenja vraća zadana mjerenja bez mreže
type citacMjerenja []Mjerenje

func (c citacMjerenja) Preuzmi(context.Context, []Postaja) ([]Mjerenje, error) { return c, nil }

// Upis koji padne na Commitu poništen je cijeli, pa se njegova mjerenja ne
// smiju brojati kao upisana: Upisi vraća 0, a Uvoznik broji tek nakon
// uspjelog upisa.
func TestPaliUpisSeNeBroji(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	// odgođeni strani ključ: redak se upiše, a Commit padne jer kišomjera
	// nema u popisu
	for _, q := range []string{
		`CREATE TABLE popis (kisomjer TEXT PRIMARY KEY)`,
		`CREATE TABLE izmjerene (
			kisomjer TEXT NOT NULL REFERENCES popis(kisomjer) DEFERRABLE INITIALLY DEFERRED,
			kraj INTEGER NOT NULL,
			sati INTEGER NOT NULL,
			oborina REAL NOT NULL,
			izvor TEXT NOT NULL,
			preuzeto INTEGER NOT NULL,
			PRIMARY KEY (kisomjer, kraj, sati)
		)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := &Spremiste{DB: baza}
	if err := s.Pripremi(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	kraj := time.Date(2026, 9, 26, 6, 0, 0, 0, time.UTC)
	m := []Mjerenje{
		{Kisomjer: "primjerovo", Kraj: kraj, Sati: 1, Oborina: 1.5, Izvor: "kisomjer-pljusak"},
		{Kisomjer: "primjerovo", Kraj: kraj.Add(time.Hour), Sati: 1, Oborina: 0.5, Izvor: "kisomjer-pljusak"},
	}

	if n, err := s.Upisi(ctx, m); err == nil || n != 0 {
		t.Errorf("pali Commit: upisano %d, greška %v; očekivano 0 i greška", n, err)
	}
	u := &Uvoznik{Spremiste: s, Postaje: func() ([]Postaja, error) {
		return []Postaja{{Code: "primjerovo", Naziv: "Primjerovo", Izvor: "pljusak", IzvorSifra: "primjerovo", Korak: "satni"}}, nil
	}, Citaci: map[string]Citac{"pljusak": citacMjerenja(m)}}
	if n, err := u.Preuzmi(ctx); err == nil || n != 0 {
		t.Errorf("uvoz s palim upisom: upisano %d, greška %v; očekivano 0 i greška", n, err)
	}
	if got, err := s.Od("primjerovo", kraj.Add(-time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("pali upis ostavio je mjerenja: %+v (%v)", got, err)
	}

	// kad je kišomjer u popisu, upis prolazi i broji se
	if _, err := baza.Exec(`INSERT INTO popis (kisomjer) VALUES ('primjerovo')`); err != nil {
		t.Fatal(err)
	}
	if n, err := u.Preuzmi(ctx); err != nil || n != 2 {
		t.Errorf("uvoz: upisano %d, greška %v; očekivano 2", n, err)
	}
}
