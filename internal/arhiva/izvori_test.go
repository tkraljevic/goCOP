package arhiva

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func praznaArhiva(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	return db
}

// Popis izvora seli iz koda u arhivu, pa mora doći i u arhive koje su nastale
// prije njega — i to s istim vrijednostima koje su dotad stajale u kodu.
func TestTablicaIzvoraNastajeSPostojecimVrijednostima(t *testing.T) {
	db := praznaArhiva(t)
	for _, ocekivano := range []struct {
		naziv    string
		tocnost  float64
		ukljucen bool
	}{
		{"his2000", 0, true},
		{"letva-dhmz", 1, true},
		{"cop", 3, true},
		{"letva-hv", 5, true},
		{"vituki", 5, true},
		{"his2000-cs", 0, false},
	} {
		var t2 float64
		var u bool
		err := db.QueryRow(`SELECT tocnost, ukljucen FROM izvori WHERE naziv=?`, ocekivano.naziv).Scan(&t2, &u)
		if err != nil {
			t.Errorf("izvor %s nije upisan: %v", ocekivano.naziv, err)
			continue
		}
		if t2 != ocekivano.tocnost || u != ocekivano.ukljucen {
			t.Errorf("%s: točnost %v uključen %v, očekivano %v/%v",
				ocekivano.naziv, t2, u, ocekivano.tocnost, ocekivano.ukljucen)
		}
	}
}

// Red povjerenja mora izaći onim redom kojim je i stajao u kodu, jer o njemu
// ovisi koja vrijednost ulazi u spoj.
func TestRedPovjerenjaOstajeIsti(t *testing.T) {
	db := praznaArhiva(t)
	red, tocnosti, err := citajIzvore(db)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := []string{"his2000", "letva-dhmz", "cop", "letva-hv", "vituki"}
	if len(red) != len(ocekivano) {
		t.Fatalf("uključeni izvori: %v", red)
	}
	for i := range ocekivano {
		if red[i] != ocekivano[i] {
			t.Errorf("na mjestu %d stoji %q, očekivano %q", i, red[i], ocekivano[i])
		}
	}
	if tocnosti["letva-hv"] != 5 {
		t.Errorf("točnost letva-hv: %v", tocnosti["letva-hv"])
	}
}

// Izvor kojeg nitko nije upisao ulazi isključen. Bolje da čeka odluku nego da
// tiho promijeni brojeve po kojima se brani od poplave — dok je popis bio u
// kodu, takav izvor nije ni čekao ni javljao, nego je nestajao.
func TestNepoznatIzvorUlaziIskljucen(t *testing.T) {
	db := praznaArhiva(t)
	if _, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta)
		VALUES ('drava','koprivnica','neka-nova-postaja','vodostaj','satni')`); err != nil {
		t.Fatal(err)
	}
	if err := upisiZadaneIzvore(db); err != nil {
		t.Fatal(err)
	}
	var ukljucen bool
	var napomena string
	if err := db.QueryRow(`SELECT ukljucen, napomena FROM izvori WHERE naziv='neka-nova-postaja'`).
		Scan(&ukljucen, &napomena); err != nil {
		t.Fatalf("novi izvor se nije pojavio u tablici: %v", err)
	}
	if ukljucen {
		t.Error("novi izvor je sam ušao u spoj")
	}
	if napomena == "" {
		t.Error("novi izvor nema napomenu da čeka odluku")
	}
	red, _, err := citajIzvore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range red {
		if i == "neka-nova-postaja" {
			t.Error("isključen izvor ipak je u redu spajanja")
		}
	}
}

// Ručna odluka mora preživjeti ponovno pokretanje gradnje: dopuniShemu se zove
// pri svakoj gradnji i pri svakoj ugradnji paketa.
func TestOdlukaOIzvoruPrezivljavaPonovnuGradnju(t *testing.T) {
	db := praznaArhiva(t)
	if _, err := db.Exec(`UPDATE izvori SET ukljucen=1, napomena='uključeno za Dravu' WHERE naziv='his2000-cs'`); err != nil {
		t.Fatal(err)
	}
	if err := dopuniShemu(db); err != nil {
		t.Fatal(err)
	}
	var ukljucen bool
	var napomena string
	if err := db.QueryRow(`SELECT ukljucen, napomena FROM izvori WHERE naziv='his2000-cs'`).
		Scan(&ukljucen, &napomena); err != nil {
		t.Fatal(err)
	}
	if !ukljucen || napomena != "uključeno za Dravu" {
		t.Errorf("odluka pregažena zadanim vrijednostima: uključen=%v napomena=%q", ukljucen, napomena)
	}
}
