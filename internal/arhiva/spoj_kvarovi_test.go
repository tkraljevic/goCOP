package arhiva

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type spojeniRedak struct {
	v       float64
	izvor   string
	vrsta   string
	tocnost float64
}

func spojeno(t *testing.T, db *sql.DB, letva, korak string, kad int64) (spojeniRedak, bool) {
	t.Helper()
	var r spojeniRedak
	err := db.QueryRow(`SELECT vrijednost, izvor, vrsta, tocnost FROM spoj
		WHERE letva=? AND velicina='vodostaj' AND korak=? AND vrijeme=?`, letva, korak, kad).
		Scan(&r.v, &r.izvor, &r.vrsta, &r.tocnost)
	if err == sql.ErrNoRows {
		return r, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return r, true
}

// Zaključava da ovjereni dnevni srednjak ima prednost pred srednjakom
// izvedenim iz satnih dojava: točniji izvor pobjeđuje.
func TestOvjereniSrednjakNadjacavaIzvedeni(t *testing.T) {
	db := praznaArhiva(t)
	dan := time.Date(2024, 9, 14, 0, 0, 0, 0, time.UTC)
	upisiNiz(t, db, "vodograd", "his2000", "vodostaj", "srednjak", zapis{t: dan.Unix(), v: 150})
	upisiSatniNiz(t, db, "vodograd", "cop", "vodostaj", dan, ponovi(200, 24)...)
	uhvatiIspis(t, &os.Stdout, func() {
		if _, err := Spoji(db, "vodograd"); err != nil {
			t.Fatal(err)
		}
	})
	if r, ok := spojeno(t, db, "vodograd", "dnevni", dan.Unix()); !ok || r != (spojeniRedak{150, "his2000", "srednjak", 0}) {
		t.Errorf("dnevna vrijednost: %+v (ima %v), očekivan ovjereni srednjak 150", r, ok)
	}
	if n := broj(t, db, `SELECT count(*) FROM spoj WHERE korak='satni' AND izvor='cop'`); n != 24 {
		t.Errorf("satnih vrijednosti iz dojava %d, očekivano 24", n)
	}
}

// Zaključava da preračun ulazi samo ondje gdje mjerenja nema, i satni i
// dnevni, s točnošću preračuna.
func TestPreracunPopunjavaSamoPraznine(t *testing.T) {
	db := praznaArhiva(t)
	dan := time.Date(2024, 9, 14, 0, 0, 0, 0, time.UTC)
	upisiSatniNiz(t, db, "vodograd", "cop", "vodostaj", dan, 200, 201)
	upisiSatniNiz(t, db, "vodograd", "preracun-proba", "vodostaj", dan, 300, 301, 302, 303)
	drugiDan := dan.AddDate(0, 0, 1).Unix()
	upisiNiz(t, db, "vodograd", "preracun-proba", "vodostaj", "srednjak", zapis{t: drugiDan + 3600, v: 250})
	uhvatiIspis(t, &os.Stdout, func() {
		if _, err := Spoji(db, "vodograd"); err != nil {
			t.Fatal(err)
		}
	})
	for sat, want := range []spojeniRedak{
		{200, "cop", "trenutna", 3},
		{201, "cop", "trenutna", 3},
		{302, "preracun-proba", "trenutna", 14},
		{303, "preracun-proba", "trenutna", 14},
	} {
		if r, ok := spojeno(t, db, "vodograd", "satni", dan.Add(time.Duration(sat)*time.Hour).Unix()); !ok || r != want {
			t.Errorf("sat %d: %+v (ima %v), očekivano %+v", sat, r, ok, want)
		}
	}
	if r, ok := spojeno(t, db, "vodograd", "dnevni", drugiDan); !ok || r != (spojeniRedak{250, "preracun-proba", "srednjak", 14}) {
		t.Errorf("dan bez mjerenja: %+v (ima %v)", r, ok)
	}
	if _, ok := spojeno(t, db, "vodograd", "dnevni", dan.Unix()); ok {
		t.Error("dan s četiri sata dobio je dnevnu vrijednost")
	}
}

// Zaključava da očitanje koje se ne da pročitati, iz bilo koje vrste niza,
// zaustavlja spajanje i ostavlja stari spojeni niz.
func TestNecitljivoOcitanjeCuvaStariSpoj(t *testing.T) {
	dan := time.Date(2024, 9, 14, 0, 0, 0, 0, time.UTC).Unix()
	for _, s := range []struct{ izvor, vrsta string }{
		{"cop", "satni"},
		{"his2000", "srednjak"},
		{"cop", "jutarnji"},
		{"preracun-proba", "satni"},
		{"preracun-proba", "srednjak"},
	} {
		t.Run(s.izvor+" "+s.vrsta, func(t *testing.T) {
			db := praznaArhiva(t)
			id := upisiNiz(t, db, "vodograd", s.izvor, "vodostaj", s.vrsta, zapis{t: dan, v: 120}, zapis{t: dan + 3600, v: 121})
			uhvatiIspis(t, &os.Stdout, func() {
				if _, err := Spoji(db, "vodograd"); err != nil {
					t.Fatal(err)
				}
			})
			prije := broj(t, db, `SELECT count(*) FROM spoj`)
			if prije == 0 {
				t.Fatal("prvi spoj je prazan")
			}
			if _, err := db.Exec(`UPDATE ocitanja SET vrijednost = 'x' WHERE niz = ?`, id); err != nil {
				t.Fatal(err)
			}
			_, err := Spoji(db, "vodograd")
			greskaSadrzi(t, "spajanje", err, "vodograd/vodostaj: ")
			greskaSadrzi(t, "spajanje", err, `name "vrijednost"`)
			if poslije := broj(t, db, `SELECT count(*) FROM spoj`); poslije != prije {
				t.Errorf("u spoju je %d vrijednosti, prije %d", poslije, prije)
			}
		})
	}
}

// Zaključava da se spajanje iz krnje ili pokvarene arhive javlja greškom.
func TestSpajanjeIzKrnjeArhive(t *testing.T) {
	for _, s := range []struct {
		ime, kvar, greska string
	}{
		{"bez tablice spoja", `DROP TABLE spoj`, "no such table: spoj"},
		{"bez tablice očitanja", `DROP TABLE ocitanja`, "no such table: ocitanja"},
		{"bez tablice nizova", `DROP TABLE nizovi`, "no such table: nizovi"},
		{"niz bez letve", `DROP TABLE nizovi;
			CREATE TABLE nizovi (id INTEGER PRIMARY KEY, letva TEXT, izvor TEXT, velicina TEXT, vrsta TEXT);
			INSERT INTO nizovi (letva, izvor, velicina, vrsta) VALUES (NULL, 'cop', 'vodostaj', 'satni')`,
			"converting NULL to string"},
		{"niz bez izvora", `DROP TABLE nizovi;
			CREATE TABLE nizovi (id INTEGER PRIMARY KEY, letva TEXT, izvor TEXT, velicina TEXT, vrsta TEXT);
			INSERT INTO nizovi (letva, izvor, velicina, vrsta) VALUES ('vodograd', NULL, 'vodostaj', 'satni')`,
			"converting NULL to string"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := praznaArhiva(t)
			upisiNiz(t, db, "vodograd", "cop", "vodostaj", "satni", zapis{t: 0, v: 120})
			if _, err := db.Exec(s.kvar); err != nil {
				t.Fatal(err)
			}
			_, err := Spoji(db, "")
			greskaSadrzi(t, s.ime, err, s.greska)
		})
	}
}

// Zaključava micanje niza koje ne uspije: dok arhivu drži drugi posao ništa se
// ne dira, kvar prije potvrde ostavlja niz, a kvar u ponovnom spajanju javlja
// da je niz maknut ali spoj nije složen.
func TestMicanjeNizaKojeNeUspije(t *testing.T) {
	pripremi := func(t *testing.T) (*sql.DB, string) {
		db := praznaArhiva(t)
		upisiNiz(t, db, "vodograd", "cop", "vodostaj", "jutarnji", zapis{t: 0, v: 120}, zapis{t: 86400, v: 121})
		upisiNiz(t, db, "vodograd", "cop", "vodostaj", "satni", zapis{t: 3600, v: 119})
		return db, filepath.Join(t.TempDir(), "arhiva.db")
	}
	makni := func(db *sql.DB, baza string) (int, error) {
		return MakniNiz(db, baza, "vodograd", "cop", "vodostaj", "jutarnji")
	}

	t.Run("arhivu drži drugi posao", func(t *testing.T) {
		db, baza := pripremi(t)
		drugi, err := Uzmi(baza, "gradnja letve vodograd", "Ana Anić")
		if err != nil {
			t.Fatal(err)
		}
		defer drugi.Pusti()
		_, err = makni(db, baza)
		var z *Zauzeto
		if !asZauzeto(err, &z) || z.Sto != "gradnja letve vodograd" {
			t.Errorf("micanje uz zauzetu arhivu: %v", err)
		}
		if n := broj(t, db, `SELECT count(*) FROM nizovi`); n != 2 {
			t.Errorf("uz zauzetu arhivu ostalo je %d nizova", n)
		}
	})
	t.Run("zatvorena baza", func(t *testing.T) {
		db, baza := pripremi(t)
		db.Close()
		_, err := makni(db, baza)
		greskaSadrzi(t, "zatvorena baza", err, "database is closed")
		if p := PosaoUTijeku(baza); p != "" {
			t.Errorf("brava je ostala iza neuspjelog micanja: %q", p)
		}
	})
	t.Run("arhiva bez nizova", func(t *testing.T) {
		db, baza := pripremi(t)
		if _, err := db.Exec(`DROP TABLE nizovi`); err != nil {
			t.Fatal(err)
		}
		_, err := makni(db, baza)
		greskaSadrzi(t, "bez nizova", err, "no such table: nizovi")
	})
	for _, s := range []struct{ ime, kada, tablica string }{
		{"brisanje očitanja", "DELETE", "ocitanja"},
		{"brisanje opisa niza", "DELETE", "nizovi"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db, baza := pripremi(t)
			kvar(t, db, s.kada, s.tablica, "")
			_, err := makni(db, baza)
			greskaSadrzi(t, s.ime, err, "namjerni kvar")
			if n := broj(t, db, `SELECT count(*) FROM ocitanja`); n != 3 {
				t.Errorf("iza puknutog micanja ostalo je %d od 3 očitanja", n)
			}
			if n := broj(t, db, `SELECT count(*) FROM nizovi`); n != 2 {
				t.Errorf("iza puknutog micanja ostalo je %d od 2 niza", n)
			}
		})
	}
	t.Run("ponovno spajanje", func(t *testing.T) {
		db, baza := pripremi(t)
		kvar(t, db, "INSERT", "spoj", "")
		obrisano, err := makni(db, baza)
		greskaSadrzi(t, "spajanje", err, "niz je maknut, ali spojeni niz se nije složio iznova: ")
		if obrisano != 2 {
			t.Errorf("javljeno %d obrisanih očitanja, maknuta su 2", obrisano)
		}
		if n := broj(t, db, `SELECT count(*) FROM nizovi WHERE vrsta='jutarnji'`); n != 0 {
			t.Error("niz je ostao iako je javljeno da je maknut")
		}
	})
}

// Zaključava da se brava mrtvog procesa koja se ne da maknuti ne preuzima
// napola: greška se javlja, a zatečena brava ostaje.
func TestMrtvaBravaKojaSeNeDaMaknuti(t *testing.T) {
	mapa := t.TempDir()
	mrtva, _ := json.Marshal(stanjeBrave{Sto: "izdavanje arhive", Tko: "Ana Anić", PID: 999999,
		Od: time.Date(2026, 1, 1, 7, 0, 0, 0, time.UTC)})
	zapisiDatoteku(t, filepath.Join(mapa, ImeBrave), string(mrtva))
	bezPrava(t, mapa, 0o555)
	if b, err := Uzmi(mapa, "izdavanje arhive", "Marko Markić"); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("brava u mapi bez pisanja: %+v, %v", b, err)
	}
	if PosaoUTijeku(mapa) != "izdavanje arhive" {
		t.Error("zatečena brava je izgubljena")
	}
}
