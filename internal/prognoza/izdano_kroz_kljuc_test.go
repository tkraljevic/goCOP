package prognoza

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

// Upiti po satu izdanja koji preskaču kroz ključ tablice izdane daju isto
// što i goli upiti po satu, koji prolaze cijelu tablicu: prvo i zadnje
// izdanje, izdanje do sata, početci zapisa, paket izdanja, izvoz razdoblja
// i brisanje izdanja. Proba ide i na praznoj tablici.
func TestIzdanoKrozKljucIstoKaoPoSatu(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	usporediSate(t, db)
	// letve s rupama u izdanjima i različitim rasponima, vodostaj i protok
	for _, l := range []struct {
		letva  string
		od, do int64
	}{{"a-letva", 1000, 1006}, {"b-letva", 998, 1003}, {"c-letva", 1002, 1009}} {
		for izdano := l.od; izdano <= l.do; izdano++ {
			if izdano == 1004 && l.letva == "a-letva" {
				continue
			}
			for _, velicina := range []string{"protok", "vodostaj"} {
				if l.letva == "b-letva" && velicina == "protok" {
					continue
				}
				for ciljni := izdano + 3; ciljni >= izdano; ciljni-- {
					v := float64(ciljni-izdano) + float64(len(l.letva))
					if _, err := db.Exec(`INSERT INTO izdane (letva, velicina, izdano, ciljni, vrijednost, dolje, gore, model)
						VALUES (?, ?, ?, ?, ?, ?, ?, 'proba')`, l.letva, velicina, izdano, ciljni, v, v-1, v+1); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := db.Exec(`INSERT INTO dnevne (letva, izdano, dan, ciljni, vrijednost, dolje, gore, model)
				VALUES (?, ?, 0, ?, 1, 0, 2, 'proba')`, l.letva, izdano, izdano+24); err != nil {
				t.Fatal(err)
			}
		}
	}
	usporediSate(t, db)

	for _, izdano := range []int64{997, 1000, 1004, 1009} {
		if dobio, htio := citaj(t, db, izdaneIzdanjaSQL, izdano), citaj(t, db, `SELECT * FROM izdane WHERE izdano = ?`, izdano); !reflect.DeepEqual(dobio, htio) {
			t.Errorf("paket izdanja %d:\ndobio %v\nhtio  %v", izdano, dobio, htio)
		}
	}
	for _, r := range [][2]int64{{990, 999}, {1001, 1004}, {1008, 1020}, {1005, 1004}} {
		dobio, err := IzdaneURazdoblju(db, r[0], r[1])
		if err != nil {
			t.Fatal(err)
		}
		htio := citaj(t, db, `SELECT letva, velicina, izdano, ciljni, vrijednost, dolje, gore, racunata, izvan, model
			FROM izdane WHERE izdano BETWEEN ? AND ? ORDER BY izdano, letva, velicina, ciljni`, r[0], r[1])
		if len(dobio) != len(htio.Redci) {
			t.Fatalf("razdoblje %v: %d redaka, želim %d", r, len(dobio), len(htio.Redci))
		}
		for i, d := range dobio {
			h := htio.Redci[i]
			if d.Letva != h[0] || d.Velicina != h[1] || d.Izdano != h[2] || d.Ciljni != h[3] || d.Vrijednost != h[4] {
				t.Errorf("razdoblje %v, redak %d: %+v, želim %v", r, i, d, h)
			}
		}
	}

	// brisanje izdanja: s površine nestaje samo taj sat, a njegovi retci
	// sele među ranije
	prije := citaj(t, db, `SELECT * FROM izdane WHERE izdano <> 1002 ORDER BY letva, velicina, izdano, ciljni`)
	seli := citaj(t, db, `SELECT letva, velicina, izdano, 0, ciljni, vrijednost, dolje, gore, racunata, izvan, model
		FROM izdane WHERE izdano = 1002 ORDER BY letva, velicina, izdano, ciljni`)
	if err := ObrisiIzdanje(db, 1002); err != nil {
		t.Fatal(err)
	}
	if poslije := citaj(t, db, `SELECT * FROM izdane ORDER BY letva, velicina, izdano, ciljni`); !reflect.DeepEqual(poslije.Redci, prije.Redci) {
		t.Errorf("nakon brisanja izdanja ostalo je %d redaka, želim %d", len(poslije.Redci), len(prije.Redci))
	}
	if ranije := citaj(t, db, `SELECT * FROM izdane_ranije ORDER BY letva, velicina, izdano, ciljni`); len(seli.Redci) != 20 || !reflect.DeepEqual(ranije.Redci, seli.Redci) {
		t.Errorf("među ranije preseljeno %d redaka, želim %d", len(ranije.Redci), len(seli.Redci))
	}
	usporediSate(t, db)
}

// usporediSate uspoređuje sate izdanja preko ključa s golim upitima
func usporediSate(t *testing.T, db *sql.DB) {
	t.Helper()
	jedan := func(upit string, args ...any) sql.NullInt64 {
		t.Helper()
		var n sql.NullInt64
		if err := db.QueryRow(upit, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	provjeri := func(ime string, sat int64, ima bool, err error, htio sql.NullInt64) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if sat != htio.Int64 || ima != htio.Valid {
			t.Errorf("%s: %d %v, želim %d %v", ime, sat, ima, htio.Int64, htio.Valid)
		}
	}
	sat, ima, err := ZadnjeIzdanje(db)
	provjeri("zadnje izdanje", sat, ima, err, jedan(`SELECT max(izdano) FROM izdane`))
	sat, ima, err = PrvoIzdanje(db)
	provjeri("prvo izdanje", sat, ima, err, jedan(`SELECT min(izdano) FROM izdane`))
	for _, do := range []int64{990, 998, 1001, 1004, 1007, 2000} {
		sat, ima, err = IzdanjeDo(db, do)
		provjeri("izdanje do", sat, ima, err, jedan(`SELECT max(izdano) FROM izdane WHERE izdano <= ?`, do))
	}
	p, err := PocetciZapisa(db)
	if err != nil {
		t.Fatal(err)
	}
	if izdane, dnevne := jedan(`SELECT min(izdano) FROM izdane`), jedan(`SELECT min(izdano) FROM dnevne`); p.Izdane != izdane.Int64 || p.Dnevne != dnevne.Int64 {
		t.Errorf("početci %+v, želim izdane %d, dnevne %d", p, izdane.Int64, dnevne.Int64)
	}
}

// citaj čita cijeli ishod upita kao tablicu paketa
func citaj(t *testing.T, db *sql.DB, upit string, args ...any) Tablica {
	t.Helper()
	tab, err := citajTablicu(db, upit, args...)
	if err != nil {
		t.Fatal(err)
	}
	return tab
}
