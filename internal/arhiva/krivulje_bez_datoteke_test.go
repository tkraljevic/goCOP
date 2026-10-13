package arhiva

import (
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava pravilo iz Odluka (13. 10. 2026.): kad iz stabla nestane cijela
// datoteka krivulja, gradnja krivulje letve ne briše, nego ih pokaže na popisu
// bez datoteke, s brojem krivulja i početkom najstarije, a gumb Makni ih miče.
// Na popis ne idu letve koje datoteku imaju, letve bez mape u stablu ni letve
// koje su stigle paketom. Prije su takve krivulje tiho ostajale u arhivi.
func TestKrivuljeBezDatotekeCekajuNaPopisu(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	zaglavlje := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_his2000_vodostaj_dnevni_2024.csv"),
		"datum;vodostaj_cm\n2024-03-01;120\n")
	put := putKrivulja(koren, "dunav", "vodograd")
	zapisiDatoteku(t, put, zaglavlje+
		"2001-01-01;2010-12-31;-100;600;polinom;1;2;3;;DHMZ;\n"+
		"2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;\n")
	zapisiDatoteku(t, putKrivulja(koren, "dunav", "rijecica"), zaglavlje+"2005-01-01;;-100;600;polinom;1;2;3;;DHMZ;\n")
	if err := os.MkdirAll(filepath.Join(koren, "drava", "primljena"), 0o755); err != nil {
		t.Fatal(err)
	}
	gradi := func(zapisi io.Writer) Izvjestaj {
		t.Helper()
		var iz Izvjestaj
		uhvatiIspis(t, &os.Stdout, func() {
			var err error
			if iz, err = Izgradi(koren, baza, "", zapisi); err != nil {
				t.Fatal(err)
			}
		})
		return iz
	}
	if iz := gradi(nil); len(iz.Sirotani) != 0 {
		t.Fatalf("krivulje s datotekom javljene su kao sirotan: %+v", iz.Sirotani)
	}
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oznaciPrimljenu(t, db, "primljena")
	upisiKrivulju(t, db, "primljena", "2019-01-01", "", 1)
	upisiKrivulju(t, db, "bez-mape", "2019-01-01", "", 1)

	if err := os.Remove(put); err != nil {
		t.Fatal(err)
	}
	var ispis strings.Builder
	iz := gradi(&ispis)
	zeli := Sirotan{Letva: "vodograd", Krivulje: true, Zapisa: 2, Datum: "2001-01-01"}
	if len(iz.Sirotani) != 1 || iz.Sirotani[0] != zeli {
		t.Errorf("gradnja javlja %+v, očekivano %+v", iz.Sirotani, zeli)
	}
	if !strings.Contains(ispis.String(), "vodograd         krivulje protoka od 2001-01-01, 2 krivulje  SIROTAN — datoteke nema u stablu\n") {
		t.Errorf("ispis gradnje:\n%s", ispis.String())
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='vodograd'`); n != 2 {
		t.Errorf("u arhivi su %d krivulje, a gradnja ne smije sama maknuti nijednu", n)
	}
	if o, err := Sirotani(db, koren); err != nil || len(o) != 1 || o[0] != zeli {
		t.Errorf("popis bez gradnje: %+v, %v", o, err)
	}
	for letva, zeliBroj := range map[string]int{"vodograd": 1, "rijecica": 0, "primljena": 0, "bez-mape": 0} {
		if o, err := krivuljeBezDatoteke(db, koren, letva); err != nil || len(o) != zeliBroj {
			t.Errorf("popis samo za %s: %+v, %v", letva, o, err)
		}
	}

	n, err := MakniKrivulje(db, baza, "vodograd")
	if err != nil || n != 2 {
		t.Fatalf("micanje krivulja: %d, %v", n, err)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva='vodograd'`); n != 0 {
		t.Errorf("nakon micanja u arhivi su %d krivulje", n)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_odsjecci WHERE krivulja NOT IN (SELECT id FROM hq_krivulje)`); n != 0 {
		t.Errorf("iza maknutih krivulja ostalo je %d odsječaka", n)
	}
	if n := broj(t, db, `SELECT count(*) FROM hq_krivulje WHERE letva IN ('rijecica', 'primljena', 'bez-mape')`); n != 3 {
		t.Errorf("krivulje drugih letvi su dirane, ostalo ih je %d", n)
	}
	if o, err := Sirotani(db, koren); err != nil || len(o) != 0 {
		t.Errorf("nakon micanja popis bez datoteke: %+v, %v", o, err)
	}
	if _, err := MakniKrivulje(db, baza, "vodograd"); err == nil || err.Error() != "letva vodograd nema krivulja protoka u arhivi" {
		t.Errorf("micanje krivulja kojih nema: %v", err)
	}
}

// Zaključava riječ uz broj u retku ispisa gradnje za krivulje bez datoteke.
func TestIspisKrivuljaBezDatoteke(t *testing.T) {
	var b strings.Builder
	javiBezDatoteke(&b, []Sirotan{
		{Letva: "vodograd", Krivulje: true, Zapisa: 1, Datum: "2001-01-01"},
		{Letva: "rijecica", Krivulje: true, Zapisa: 5, Datum: "1926-01-01"},
	})
	for _, redak := range []string{
		"vodograd         krivulje protoka od 2001-01-01, 1 krivulja  SIROTAN — datoteke nema u stablu\n",
		"rijecica         krivulje protoka od 1926-01-01, 5 krivulja  SIROTAN — datoteke nema u stablu\n",
	} {
		if !strings.Contains(b.String(), redak) {
			t.Errorf("ispis nema %q:\n%s", redak, b.String())
		}
	}
}

// Zaključava da se popis krivulja bez datoteke, kad ga se ne da složiti,
// javlja greškom: stablo koje se ne čita, krivi uzorak putanje, arhiva bez
// tablice krivulja ili bez evidencije primljenih paketa i krivulja bez
// početka valjanosti.
func TestPopisKrivuljaBezDatotekeKojiSeNeDaSloziti(t *testing.T) {
	db := praznaArhiva(t)
	if _, err := krivuljeBezDatoteke(db, filepath.Join(t.TempDir(), "nema"), ""); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stablo koje ne postoji: %v", err)
	}
	if _, err := letveSDatotekomKrivulja(filepath.Join(t.TempDir(), "mapa[")); !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("krivi uzorak putanje: %v", err)
	}
	mapa := filepath.Join(t.TempDir(), "mapa[")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := krivuljeBezDatoteke(db, mapa, ""); !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("stablo s krivim uzorkom: %v", err)
	}

	for _, tablica := range []string{"primljena_izdanja", "hq_krivulje"} {
		bez := praznaArhiva(t)
		if _, err := bez.Exec(`DROP TABLE ` + tablica); err != nil {
			t.Fatal(err)
		}
		if _, err := bezDatoteke(bez, t.TempDir(), nil, "", nil); err == nil || !strings.Contains(err.Error(), tablica) {
			t.Errorf("arhiva bez tablice %s: %v", tablica, err)
		}
	}

	zateceno, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "zatecena.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer zateceno.Close()
	if _, err := zateceno.Exec(shemaPrimljenih + `CREATE TABLE hq_krivulje (id INTEGER PRIMARY KEY, letva TEXT, vrijedi_od TEXT);
		INSERT INTO hq_krivulje (letva, vrijedi_od) VALUES ('vodograd', NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := krivuljeUArhivi(zateceno, "vodograd"); err == nil {
		t.Error("krivulja bez početka valjanosti (NULL) pročitana je bez greške")
	}
}

// Zaključava da se krivulje koje se ne daju maknuti javljaju greškom, a
// arhiva ostane kakva je bila: arhivu drži drugi posao, veza je zatvorena, ili
// brisanje odsječaka ili krivulja ne prođe.
func TestKrivuljeKojeSeNeDajuMaknutiSPopisa(t *testing.T) {
	krivulja := func(t *testing.T) (*sql.DB, string) {
		t.Helper()
		put := filepath.Join(t.TempDir(), "arhiva.db")
		db := napraviArhivu(t, put)
		t.Cleanup(func() { db.Close() })
		upisiKrivulju(t, db, "vodograd", "2001-01-01", "", 1)
		return db, put
	}
	ostalo := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if n := broj(t, db, `SELECT count(*) FROM hq_krivulje`) + broj(t, db, `SELECT count(*) FROM hq_odsjecci`); n != 2 {
			t.Errorf("neuspjelo micanje promijenilo je arhivu (%d redaka)", n)
		}
	}

	t.Run("arhivu drži drugi posao", func(t *testing.T) {
		db, put := krivulja(t)
		drugi, err := Uzmi(put, "gradnja letve vodograd", "Ana Anić")
		if err != nil {
			t.Fatal(err)
		}
		defer drugi.Pusti()
		_, err = MakniKrivulje(db, put, "vodograd")
		var z *Zauzeto
		if !asZauzeto(err, &z) || z.Tko != "Ana Anić" {
			t.Errorf("micanje uz zauzetu arhivu: %v", err)
		}
		ostalo(t, db)
	})
	t.Run("zatvorena veza", func(t *testing.T) {
		db, put := krivulja(t)
		db.Close()
		if _, err := MakniKrivulje(db, put, "vodograd"); err == nil {
			t.Error("micanje kroz zatvorenu vezu prošlo je")
		}
	})
	for _, s := range []struct{ ime, tablica string }{
		{"brisanje odsječaka", "hq_odsjecci"},
		{"brisanje krivulja", "hq_krivulje"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db, put := krivulja(t)
			kvar(t, db, "DELETE", s.tablica, "")
			_, err := MakniKrivulje(db, put, "vodograd")
			greskaSadrzi(t, s.ime, err, "namjerni kvar")
			ostalo(t, db)
		})
	}
}
