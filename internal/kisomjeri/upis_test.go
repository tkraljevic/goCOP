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

// Izmjerena kiša po satima: satni kišomjer ulazi satima, a dnevni zbroj koji
// uz njih javlja se ne broji drugi put; kišomjer koji javlja samo dan dijeli
// se jednako na 24 sata, a sati izvan traženog razdoblja ne ulaze.
func TestIzmjerenaKisaPoSatima(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Spremiste{DB: baza}
	if err := s.Pripremi(); err != nil {
		t.Fatal(err)
	}
	const sat = int64(490000)
	kraj := func(h int64) time.Time { return time.Unix(h*3600, 0) }
	if _, err := s.Upisi(context.Background(), []Mjerenje{
		{Kisomjer: "km-satni", Kraj: kraj(sat), Sati: 1, Oborina: 2, Izvor: "kisomjer-dhmz"},
		{Kisomjer: "km-satni", Kraj: kraj(sat - 1), Sati: 1, Oborina: 3, Izvor: "kisomjer-dhmz"},
		{Kisomjer: "km-satni", Kraj: kraj(sat), Sati: 24, Oborina: 40, Izvor: "kisomjer-dhmz"},
		{Kisomjer: "km-dnevni", Kraj: kraj(sat + 2), Sati: 24, Oborina: 12, Izvor: "kisomjer-hv"},
		{Kisomjer: "km-stari", Kraj: kraj(sat - 100), Sati: 1, Oborina: 9, Izvor: "kisomjer-dhmz"},
	}); err != nil {
		t.Fatal(err)
	}
	m, err := s.PoSatima(sat-5, sat)
	if err != nil {
		t.Fatal(err)
	}
	if n := m["km-satni"]; len(n) != 2 || n[sat] != 2 || n[sat-1] != 3 {
		t.Errorf("satni kišomjer: %v", n)
	}
	if n := m["km-dnevni"]; len(n) != 6 || n[sat] != 0.5 || n[sat-5] != 0.5 {
		t.Errorf("dnevni kišomjer: %v", n)
	}
	if _, ima := m["km-stari"]; ima {
		t.Error("mjerenje izvan razdoblja ne ulazi")
	}
	if _, err := baza.Exec(`DROP TABLE izmjerene; CREATE TABLE izmjerene (kisomjer, kraj, sati, oborina);
		INSERT INTO izmjerene VALUES ('km', ?, 'nije broj', 1)`, sat*3600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PoSatima(sat-5, sat); err == nil {
		t.Error("nečitljiv redak mora javiti grešku")
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PoSatima(sat-5, sat); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}
