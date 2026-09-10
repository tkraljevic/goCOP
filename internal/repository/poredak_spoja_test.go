package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Poredak ulazi u SQL kao tekst, pa smije poprimiti samo zadane oblike.
// Sve što nije prepoznato pada natrag na vrijeme — nikad na ono što je došlo
// iz adrese.
func TestPoredakSpojaNePropustaTudeTekst(t *testing.T) {
	for _, ulaz := range []string{"", "vrijeme", "nesto", "vrijednost DESC; DROP TABLE spoj", "VRH"} {
		if got := poredakSpoja(ulaz); got != "vrijeme DESC" {
			t.Errorf("poredakSpoja(%q) = %q, očekivano vrijeme DESC", ulaz, got)
		}
	}
	if got := poredakSpoja(PoNajvisem); got != "vrijednost DESC, vrijeme DESC" {
		t.Errorf("najviši prvo: %q", got)
	}
	if got := poredakSpoja(PoNajnizem); got != "vrijednost ASC, vrijeme DESC" {
		t.Errorf("najniži prvo: %q", got)
	}
}

// Listanje po vrijednosti: tri najviša dana u godini bez listanja dvanaest
// stranica. Stranica se i dalje reže istim pagerom, pa poredak mora vrijediti
// nad cijelim razdobljem, ne samo nad prikazanim retkom.
func TestSpojRasponSlazePoVrijednosti(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE spoj (letva TEXT NOT NULL, velicina TEXT NOT NULL,
		korak TEXT NOT NULL, vrijeme INTEGER NOT NULL, vrijednost REAL NOT NULL,
		izvor TEXT NOT NULL DEFAULT 'his2000', vrsta TEXT NOT NULL DEFAULT 'srednjak',
		tocnost REAL NOT NULL DEFAULT 0,
		PRIMARY KEY (letva,velicina,korak,vrijeme)) WITHOUT ROWID`); err != nil {
		t.Fatal(err)
	}
	dan := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	for i, v := range []float64{120, 771, 300, 772, 95, 640} {
		if _, err := db.Exec(`INSERT INTO spoj (letva,velicina,korak,vrijeme,vrijednost)
			VALUES ('batina','vodostaj','dnevni',?,?)`, dan.AddDate(0, 0, i).Unix(), v); err != nil {
			t.Fatal(err)
		}
	}
	r := &ArhivaRepository{db: db}
	od, do := dan, dan.AddDate(0, 0, 30)

	prvi := func(poredak string, koliko int) []float64 {
		v, err := r.SpojRaspon(context.Background(), "batina", "vodostaj", "dnevni", od, do, koliko, 0, poredak)
		if err != nil {
			t.Fatal(err)
		}
		var out []float64
		for _, x := range v {
			out = append(out, x.Vrijednost)
		}
		return out
	}
	if got := prvi(PoNajvisem, 3); len(got) != 3 || got[0] != 772 || got[1] != 771 || got[2] != 640 {
		t.Errorf("najviši prvo: %v", got)
	}
	if got := prvi(PoNajnizem, 3); len(got) != 3 || got[0] != 95 || got[1] != 120 || got[2] != 300 {
		t.Errorf("najniži prvo: %v", got)
	}
	// Po vremenu ostaje kako je bilo: najnovije prvo.
	if got := prvi(PoVremenu, 2); len(got) != 2 || got[0] != 640 || got[1] != 95 {
		t.Errorf("po datumu: %v", got)
	}
	// Druga stranica po vrijednosti nastavlja gdje je prva stala.
	v, _ := r.SpojRaspon(context.Background(), "batina", "vodostaj", "dnevni", od, do, 3, 3, PoNajvisem)
	if len(v) != 3 || v[0].Vrijednost != 300 {
		t.Errorf("druga stranica: %+v", v)
	}
}
