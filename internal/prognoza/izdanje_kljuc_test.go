package prognoza

import (
	"path/filepath"
	"reflect"
	"testing"
)

// Izdanje preskakanjem kroz ključ daje iste retke istim redom kao upit po
// satu izdanja: više letava, vodostaj i protok, druga izdanja koja se ne
// uzimaju, izdanje koje nema i prazna tablica.
func TestIzdanjeKrozKljucIstoKaoPoSatu(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	poSatu := func(izdano int64) map[string][]Izdana {
		t.Helper()
		r, err := db.Query(`SELECT letva, velicina, izdano, ciljni, vrijednost, dolje, gore,
			racunata, izvan, model FROM izdane WHERE izdano = ? ORDER BY letva, velicina, ciljni`, izdano)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		out := map[string][]Izdana{}
		for r.Next() {
			var i Izdana
			if err := r.Scan(&i.Letva, &i.Velicina, &i.Izdano, &i.Ciljni, &i.Vrijednost, &i.Dolje, &i.Gore,
				&i.Racunata, &i.Izvan, &i.Model); err != nil {
				t.Fatal(err)
			}
			out[i.Letva] = append(out[i.Letva], i)
		}
		return out
	}
	provjeri := func(izdano int64) {
		t.Helper()
		dobio, err := Izdanje(db, izdano)
		if err != nil {
			t.Fatal(err)
		}
		if htio := poSatu(izdano); !reflect.DeepEqual(dobio, htio) {
			t.Errorf("izdanje %d:\ndobio %v\nhtio  %v", izdano, dobio, htio)
		}
	}
	provjeri(1000)
	for _, letva := range []string{"a-letva", "b-letva", "c-letva"} {
		for izdano := int64(1000); izdano < 1003; izdano++ {
			for _, velicina := range []string{"vodostaj", "protok"} {
				if letva == "c-letva" && velicina == "protok" {
					continue
				}
				for ciljni := izdano + 5; ciljni >= izdano; ciljni-- { // upis obrnutim redom
					v := float64(ciljni - izdano)
					if _, err := db.Exec(`INSERT INTO izdane (letva, velicina, izdano, ciljni, vrijednost, dolje, gore, racunata, model)
						VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'proba')`, letva, velicina, izdano, ciljni, v, v-1, v+1, len(velicina)%2); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	for _, izdano := range []int64{1000, 1001, 1002, 999} {
		provjeri(izdano)
	}
	if iz, err := Izdanje(db, 1001); err != nil || len(iz["a-letva"]) != 12 || len(iz["c-letva"]) != 6 {
		t.Errorf("izdanje 1001: %d i %d redaka (%v)", len(iz["a-letva"]), len(iz["c-letva"]), err)
	}
}
