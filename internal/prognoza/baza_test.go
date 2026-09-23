package prognoza

import (
	"path/filepath"
	"testing"
)

// Karika izbačena iz lanca mora nestati iz baze. Ostane li, račun je traži i
// traži njezin vrh, a taj vrh očitanja nema — pa padne cijela prognoza, ne
// samo taj krak.
func TestSpremiBrisePojaseIzbaceneLetve(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sPlitvicom := []Pojas{
		{Letva: "ludbreg", Velicina: "protok", Od: 0, Do: 100, Rasap: 1,
			Ulazi: []Ulaz{{Letva: "tuhovec", Velicina: "protok", PomakH: 5, Sirina: 1, Nagib: 1}}},
		{Letva: "vidovicev-mlin", Velicina: "protok", Od: 0, Do: 10, Rasap: 1,
			Ulazi: []Ulaz{{Letva: "krkanec", Velicina: "protok", PomakH: 0, Sirina: 1, Nagib: 1}}},
	}
	if err := Spremi(db, sPlitvicom, "prva"); err != nil {
		t.Fatal(err)
	}
	if err := Spremi(db, sPlitvicom[:1], "druga"); err != nil {
		t.Fatal(err)
	}
	svi, err := SviPojasi(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, ima := svi["vidovicev-mlin"]; ima {
		t.Error("izbačena letva ostala je u bazi pojasa")
	}
	if len(svi["ludbreg"]) != 1 {
		t.Errorf("ludbreg ima %d pojasa, očekivan jedan", len(svi["ludbreg"]))
	}
	var ulaza int
	if err := db.QueryRow(`SELECT count(*) FROM ulazi WHERE letva = 'vidovicev-mlin'`).
		Scan(&ulaza); err != nil {
		t.Fatal(err)
	}
	if ulaza != 0 {
		t.Errorf("izbačena letva ostavila je %d ulaza", ulaza)
	}
}
