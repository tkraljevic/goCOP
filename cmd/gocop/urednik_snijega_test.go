package main

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
)

// Postavke snijega mijenja isti urednik kao i model: nacrt i proba samo na
// računalu koje izdaje, primjena još i dok satni krug ne traje. Primijenjene
// odmah vrijede u računu.
func TestUrednikSnijega(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	prazna, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	// postavke u radu su zajedničke cijelom programu: test ih vraća na zadane
	t.Cleanup(func() {
		if err := prognoza.UcitajPostavkeSnijega(prazna); err != nil {
			t.Error(err)
		}
		if err := prazna.Close(); err != nil {
			t.Error(err)
		}
	})

	stara := probajNacrtSnijega
	t.Cleanup(func() { probajNacrtSnijega = stara })
	var greska error
	probajNacrtSnijega = func(arhiva, baza *sql.DB, tocke []prognoza.OborinskaTocka, kad string) (prognoza.NacrtSnijega, error) {
		if greska != nil {
			return prognoza.NacrtSnijega{}, greska
		}
		// proba upiše ishod u nacrt, kao prava
		if _, err := baza.Exec(`UPDATE model_nacrt SET sadrzaj = json_set(sadrzaj, '$.proba',
			json('{"kad":"10:05","primijenjene":{"ukljucen":true,"topljenje":3,"prag":0}}')) WHERE letva = 'postavke:snijeg'`); err != nil {
			t.Fatal(err)
		}
		return prognoza.NacrtSnijega{Proba: &prognoza.ProbaSnijega{Letve: []prognoza.ProbaSnijegaLetve{
			{Ocjena: prognoza.SnijegBolje}, {Ocjena: prognoza.SnijegIsto}, {Ocjena: prognoza.MjeraNijeProvjereno}}}}, nil
	}
	uloge, krug := &probneUloge{}, &probniKrug{}
	u := &urednikModela{pb: pb, mreza: uloge, krug: krug}
	nove := prognoza.PostavkeSnijega{Ukljucen: true, Topljenje: 4}
	posao := func() poslovi.Pogled {
		t.Helper()
		p := poslovi.NoviRegistar().Pokreni("proba", "", "/", u.ProbajSnijeg)
		for range 400 {
			if !p.Traje() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return p.Stanje()
	}
	if u.SpremiSnijeg(nove, "Iva Proba") != errNeIzdaje || u.ObrisiNacrtSnijega() != errNeIzdaje ||
		u.PrimijeniSnijeg("Iva Proba", "proba") != errNeIzdaje || !strings.Contains(posao().Greska, "izdaje prognozu") {
		t.Error("računalo koje ne izdaje ne smije mijenjati postavke snijega")
	}
	uloge.izdaje = true
	if err := u.SpremiSnijeg(nove, "Iva Proba"); err != nil {
		t.Fatal(err)
	}
	if n, ima, err := prognoza.NacrtSnijegaIz(pb); err != nil || !ima || n.Postavke != nove || n.Tko != "Iva Proba" || n.Kad == "" {
		t.Fatalf("nacrt: %+v, %v, %v", n, ima, err)
	}
	if st := posao(); st.Greska != "" || !strings.Contains(st.Sazetak, "bolji nego sada na 1 od 2 letve") {
		t.Errorf("proba: %+v", st)
	}
	krug.traje = true
	if u.PrimijeniSnijeg("Iva Proba", "proba") != errKrugTraje {
		t.Error("dok satni krug traje, postavke se ne mijenjaju")
	}
	krug.traje = false
	if err := u.PrimijeniSnijeg("Iva Proba", "topi se brže"); err != nil {
		t.Fatal(err)
	}
	if z, ima, err := prognoza.PrimijenjeneSnijega(pb); err != nil || !ima || z.Postavke != nove || z.Zadnja().Tko != "Iva Proba" || z.Zadnja().Kad == "" {
		t.Errorf("primijenjeno: %+v, %v, %v", z, ima, err)
	}
	if prognoza.PostavkeSnijegaURadu() != nove {
		t.Errorf("primijenjene postavke moraju odmah vrijediti: %+v", prognoza.PostavkeSnijegaURadu())
	}
	if err := u.SpremiSnijeg(prognoza.PostavkeSnijega{Topljenje: 20}, "Iva Proba"); err == nil {
		t.Error("postavke izvan granica moraju biti odbijene")
	}
	if err := u.SpremiSnijeg(nove, "Iva Proba"); err != nil {
		t.Fatal(err)
	}
	if err := u.ObrisiNacrtSnijega(); err != nil {
		t.Fatal(err)
	}
	if _, ima, err := prognoza.NacrtSnijegaIz(pb); err != nil || ima {
		t.Error("nacrt nije odbačen")
	}
	greska = errors.New("proba se nije dala izmjeriti ni na jednoj letvi")
	if err := u.SpremiSnijeg(nove, "Iva Proba"); err != nil {
		t.Fatal(err)
	}
	if st := posao(); !strings.Contains(st.Greska, "ni na jednoj letvi") {
		t.Errorf("greška probe: %+v", st)
	}
	if s := opisProbeSnijega(prognoza.NacrtSnijega{}); s != "Proba je gotova." {
		t.Errorf("opis bez probe: %q", s)
	}
}
