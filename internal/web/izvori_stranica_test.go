package web

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/arhiva"
	_ "modernc.org/sqlite"
)

func arhivaZaStranicu(t *testing.T) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "vodostaji.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := arhiva.PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	// vituki ima podatke i uključen je; his2000-cs ih ima a isključen je
	for _, n := range []struct {
		izvor, letva string
		zapisa       int
	}{
		{"vituki", "paks", 263615}, {"vituki", "baja", 110827},
		{"his2000-cs", "donji-miholjac", 271721},
	} {
		if _, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
			VALUES ('dunav',?,?,'vodostaj','satni',?)`, n.letva, n.izvor, n.zapisa); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// Stranica mora pokazati koliko podataka stoji iza svakog izvora i, još važnije,
// koliko ih leži neiskorišteno — jer se upravo to prije nije vidjelo nigdje.
func TestStranicaIzvoraBrojiNeiskoristeno(t *testing.T) {
	izvori, zanemareno, err := citajIzvoreZaStranicu(arhivaZaStranicu(t))
	if err != nil {
		t.Fatal(err)
	}
	nadi := func(naziv string) IzvorURedu {
		for _, i := range izvori {
			if i.Naziv == naziv {
				return i
			}
		}
		t.Fatalf("izvora %q nema na popisu", naziv)
		return IzvorURedu{}
	}
	v := nadi("vituki")
	if v.Zapisa != 374442 || v.Nizova != 2 || len(v.Letve) != 2 {
		t.Errorf("vituki: %d zapisa, %d nizova, letve %v", v.Zapisa, v.Nizova, v.Letve)
	}
	if v.Neiskoristen() {
		t.Error("vituki je uključen, a označen je kao neiskorišten")
	}
	if cs := nadi("his2000-cs"); !cs.Neiskoristen() {
		t.Error("his2000-cs je isključen i ima podatke — mora se vidjeti da leži neiskorišten")
	}
	if zanemareno != 271721 {
		t.Errorf("zanemareno %d, očekivano 271721", zanemareno)
	}
	// Izvor bez ijednog niza je zapis o odluci, ne o podacima, i ne broji se.
	if p := nadi("his2000-ukinuto"); p.ImaPodatke() || p.Neiskoristen() {
		t.Error("izvor bez podataka ne smije se voditi kao neiskorišten")
	}
}

// Redoslijed na stranici mora biti red povjerenja, jer o njemu ovisi koja
// vrijednost ulazi u spoj kad su dva izvora jednako točna.
func TestStranicaIzvoraSlazePoRedu(t *testing.T) {
	izvori, _, err := citajIzvoreZaStranicu(arhivaZaStranicu(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(izvori) < 2 || izvori[0].Naziv != "his2000" {
		t.Fatalf("prvi mora biti his2000: %v", izvori)
	}
	for i := 1; i < len(izvori); i++ {
		if izvori[i-1].Red > izvori[i].Red {
			t.Errorf("%s (red %d) stoji prije %s (red %d)",
				izvori[i-1].Naziv, izvori[i-1].Red, izvori[i].Naziv, izvori[i].Red)
		}
	}
}

// Promjena napomene ne mijenja nijedan broj, pa ne smije pokrenuti ponovno
// spajanje — na Batini bi to bilo 626.065 redaka zbog ispravka tipfelera.
func TestNapomenaNePokreceSpajanje(t *testing.T) {
	put := arhivaZaStranicu(t)
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	letve, err := arhiva.PostaviIzvor(db, arhiva.Izvor{
		Naziv: "vituki", Tocnost: 5, Red: 50, Ukljucen: true, Napomena: "dopisano objašnjenje"})
	if err != nil {
		t.Fatal(err)
	}
	if len(letve) != 0 {
		t.Errorf("zbog napomene se ponovno spaja: %v", letve)
	}
	// A promjena točnosti mora javiti obje letve tog izvora.
	letve, err = arhiva.PostaviIzvor(db, arhiva.Izvor{
		Naziv: "vituki", Tocnost: 8, Red: 50, Ukljucen: true, Napomena: "dopisano objašnjenje"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(letve, ",") != "baja,paks" {
		t.Errorf("letve za ponovno spajanje: %v", letve)
	}
}
