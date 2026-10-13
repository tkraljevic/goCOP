package arhiva

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava ispis gradnje za ono što je ostalo bez datoteke: niz sa svojim
// stupcima i brojem vrijednosti u spojenom nizu, snimka korita s danom i
// brojem točaka. Riječ i glagol uz broj slažu se s brojem.
func TestIspisBezDatoteke(t *testing.T) {
	var b strings.Builder
	javiBezDatoteke(&b, []Sirotan{
		{Letva: "vodograd", Izvor: "cop-rucno", Velicina: "vodostaj", Vrsta: "jutarnji", Zapisa: 3, USpoju: 2},
		{Letva: "vodograd", Izvor: "cop-rucno", Velicina: "vodostaj", Vrsta: "dnevni", Zapisa: 1, USpoju: 1},
		{Letva: "vodograd", Izvor: "cop-rucno", Velicina: "vodostaj", Vrsta: "srednjak", Zapisa: 12, USpoju: 12},
		{Letva: "vodograd", Izvor: "cop", Velicina: "vodostaj", Vrsta: "satni", Zapisa: 1},
		{Letva: "vodograd", Snimka: true, Datum: "2010-03-22", Zapisa: 1},
		{Letva: "vodograd", Snimka: true, Datum: "2011-04-01", Zapisa: 5},
	})
	for _, redak := range []string{
		"vodograd         cop-rucno              vodostaj     jutarnji          3  SIROTAN — datoteke nema u stablu, a 2 vrijednosti su u spojenom nizu\n",
		"vodograd         cop-rucno              vodostaj     dnevni            1  SIROTAN — datoteke nema u stablu, a 1 vrijednost je u spojenom nizu\n",
		"vodograd         cop-rucno              vodostaj     srednjak         12  SIROTAN — datoteke nema u stablu, a 12 vrijednosti je u spojenom nizu\n",
		"vodograd         cop                    vodostaj     satni             1  SIROTAN — datoteke nema u stablu\n",
		"vodograd         snimka korita 2010-03-22, 1 točka  SIROTAN — datoteke nema u stablu\n",
		"vodograd         snimka korita 2011-04-01, 5 točaka  SIROTAN — datoteke nema u stablu\n",
	} {
		if !strings.Contains(b.String(), redak) {
			t.Errorf("ispis nema %q:\n%s", redak, b.String())
		}
	}
}

// Zaključava da se popis bez datoteke, kad ga se ne da složiti, javlja
// greškom: stablo koje se ne čita, snimka koja se ne čita, arhiva bez
// tablica i zatečena snimka bez dana.
func TestPopisBezDatotekeKojiSeNeDaSloziti(t *testing.T) {
	db := praznaArhiva(t)
	nema := filepath.Join(t.TempDir(), "nema")
	if _, err := snimkeBezDatoteke(db, nema, ""); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stablo koje ne postoji: %v", err)
	}
	if _, err := snimkeUStablu(filepath.Join(t.TempDir(), "mapa["), ""); !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("krivi uzorak putanje: %v", err)
	}

	koren := t.TempDir()
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"), "stacionaza_m;visina_m\n0;9\"0\n")
	_, err := snimkeBezDatoteke(db, koren, "")
	greskaSadrzi(t, "snimka s navodnikom", err, "vodograd_profil_2010-03-22.csv: ")
	if u, err := snimkeUStablu(koren, "rijecica"); err != nil || len(u) != 0 {
		t.Errorf("snimke druge letve: %v, %v", u, err)
	}

	veza := putProfila(t.TempDir(), "dunav", "vodograd", "2010-03-22")
	if err := os.MkdirAll(filepath.Dir(veza), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "nema.csv"), veza); err != nil {
		t.Fatal(err)
	}
	if _, err := datumSnimke(veza); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("veza na datoteku koje nema: %v", err)
	}

	bezTablica, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bezTablica.Close()
	if _, err := snimkeBezDatoteke(bezTablica, t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "profili") {
		t.Errorf("arhiva bez tablice snimki: %v", err)
	}
	if _, err := bezDatoteke(bezTablica, t.TempDir(), nil, "", nil); err == nil || !strings.Contains(err.Error(), "nizovi") {
		t.Errorf("arhiva bez tablice nizova: %v", err)
	}
	if _, err := bezDatoteke(db, nema, nil, "", nil); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("popis uz stablo koje ne postoji: %v", err)
	}

	if _, err := bezTablica.Exec(`CREATE TABLE profili (id INTEGER PRIMARY KEY, letva TEXT, datum TEXT);
		CREATE TABLE profil_tocke (profil INTEGER, stacionaza REAL, visina REAL);
		INSERT INTO profili (letva, datum) VALUES ('vodograd', NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := snimkeUArhivi(bezTablica, "vodograd"); err == nil {
		t.Error("snimka bez dana (NULL) pročitana je bez greške")
	}
}

// Zaključava da se snimka koja se ne da maknuti javlja greškom, a arhiva
// ostane kakva je bila: arhivu drži drugi posao, veza je zatvorena, ili
// brisanje točaka ili snimke ne prođe.
func TestSnimkaKojaSeNeDaMaknuti(t *testing.T) {
	snimka := func(t *testing.T) (*sql.DB, string) {
		t.Helper()
		put := filepath.Join(t.TempDir(), "arhiva.db")
		db := napraviArhivu(t, put)
		t.Cleanup(func() { db.Close() })
		res, err := db.Exec(`INSERT INTO profili (letva, datum) VALUES ('vodograd', '2010-03-22')`)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if _, err := db.Exec(`INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (?, 0, 90), (?, 6, 87)`, id, id); err != nil {
			t.Fatal(err)
		}
		return db, put
	}
	ostalo := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if n := broj(t, db, `SELECT count(*) FROM profili`) + broj(t, db, `SELECT count(*) FROM profil_tocke`); n != 3 {
			t.Errorf("neuspjelo micanje promijenilo je arhivu (%d redaka)", n)
		}
	}

	t.Run("arhivu drži drugi posao", func(t *testing.T) {
		db, put := snimka(t)
		drugi, err := Uzmi(put, "gradnja letve vodograd", "Ana Anić")
		if err != nil {
			t.Fatal(err)
		}
		defer drugi.Pusti()
		_, err = MakniSnimku(db, put, "vodograd", "2010-03-22")
		var z *Zauzeto
		if !asZauzeto(err, &z) || z.Tko != "Ana Anić" {
			t.Errorf("micanje uz zauzetu arhivu: %v", err)
		}
		ostalo(t, db)
	})
	t.Run("zatvorena veza", func(t *testing.T) {
		db, put := snimka(t)
		db.Close()
		if _, err := MakniSnimku(db, put, "vodograd", "2010-03-22"); err == nil {
			t.Error("micanje kroz zatvorenu vezu prošlo je")
		}
	})
	for _, s := range []struct{ ime, tablica string }{
		{"brisanje točaka", "profil_tocke"},
		{"brisanje snimke", "profili"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db, put := snimka(t)
			kvar(t, db, "DELETE", s.tablica, "")
			_, err := MakniSnimku(db, put, "vodograd", "2010-03-22")
			greskaSadrzi(t, s.ime, err, "namjerni kvar")
			ostalo(t, db)
		})
	}
}
