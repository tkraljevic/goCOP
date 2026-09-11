package poslovi

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func cekajKraj(t *testing.T, p *Posao) Pogled {
	t.Helper()
	for i := 0; i < 200; i++ {
		if !p.Traje() {
			return p.Stanje()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("posao se nije dovršio")
	return Pogled{}
}

// Gradnja svoj tijek piše u io.Writer i ne zna ništa o poslovima. Posao mora
// biti to odredište, inače bi se gradnja morala mijenjati zbog stranice.
func TestPosaoHvataRetkeIspisa(t *testing.T) {
	r := NoviRegistar()
	p := r.Pokreni("proba", "covjek", "", func(p *Posao) error {
		fmt.Fprintf(p, "prvi\ndru")
		fmt.Fprintf(p, "gi\n")
		fmt.Fprintf(p, "bez prijeloga na kraju")
		return nil
	})
	s := cekajKraj(t, p)
	if s.Stanje != Gotov {
		t.Fatalf("stanje %q, greška %q", s.Stanje, s.Greska)
	}
	if strings.Join(s.Redci, "|") != "prvi|drugi|bez prijeloga na kraju" {
		t.Errorf("retci %q", s.Redci)
	}
}

// Traka mora reći "ne znam" umjesto izmisliti postotak: gradnja dugo radi
// stvari kojima se broj koraka ne zna unaprijed.
func TestNepoznatUkupanBrojNeDajePostotak(t *testing.T) {
	r := NoviRegistar()
	gotovo := make(chan struct{})
	p := r.Pokreni("proba", "covjek", "", func(p *Posao) error {
		p.Korak("spajam", 0, 0)
		<-gotovo
		return nil
	})
	for i := 0; i < 200 && p.Stanje().Sto != "spajam"; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if got := p.Stanje().Postotak; got != -1 {
		t.Errorf("bez poznatog ukupnog broja postotak je %d, a mora biti -1", got)
	}
	p.Korak("upisujem", 3, 4)
	if got := p.Stanje().Postotak; got != 75 {
		t.Errorf("3 od 4 dalo %d %%", got)
	}
	close(gotovo)
	cekajKraj(t, p)
}

// Posao koji padne ne smije srušiti program, a čovjek mora vidjeti zašto.
func TestPosaoKojiPadneJavljaZasto(t *testing.T) {
	r := NoviRegistar()
	p := r.Pokreni("proba", "covjek", "", func(p *Posao) error {
		return fmt.Errorf("stablo nije nađeno")
	})
	s := cekajKraj(t, p)
	if s.Stanje != Pao || s.Greska != "stablo nije nađeno" {
		t.Errorf("stanje %q, greška %q", s.Stanje, s.Greska)
	}
	if s.Postotak != 100 {
		t.Errorf("pali posao ostavio traku na %d %%", s.Postotak)
	}
}

// Panika u poslu ruši dretvu, a s njom i cijeli program — osim ako je uhvaćena.
func TestPanikaUPosluNeRusiProgram(t *testing.T) {
	r := NoviRegistar()
	p := r.Pokreni("proba", "covjek", "", func(p *Posao) error {
		var m map[string]int
		m["pukni"] = 1
		return nil
	})
	s := cekajKraj(t, p)
	if s.Stanje != Pao {
		t.Fatalf("stanje %q", s.Stanje)
	}
	if !strings.Contains(s.Greska, "pukao") {
		t.Errorf("greška %q", s.Greska)
	}
}

// U dnevniku posla piše što je netko radio s arhivom; tuđi se ne pokazuje.
func TestTudiPosaoSeNePokazuje(t *testing.T) {
	r := NoviRegistar()
	p := r.Pokreni("proba", "ana", "", func(p *Posao) error { return nil })
	cekajKraj(t, p)
	if _, ima := r.Nadi(p.ID, "marko"); ima {
		t.Error("tuđi posao je pronađen")
	}
	if _, ima := r.Nadi(p.ID, "ana"); !ima {
		t.Error("vlastiti posao nije pronađen")
	}
}

// Odredište zna za broj posla tek kad ga posao dobije.
func TestOdredisteDobijeBrojPosla(t *testing.T) {
	r := NoviRegistar()
	p := r.Pokreni("proba", "ana", "/administracija/uvoz-niza?posao={id}", func(p *Posao) error { return nil })
	cekajKraj(t, p)
	if p.Odrediste != "/administracija/uvoz-niza?posao="+p.ID {
		t.Errorf("odredište %q", p.Odrediste)
	}
}

// Ono što posao izradi stranica mora dobiti natrag, da isto ne računa iznova.
func TestPlodStizeStranici(t *testing.T) {
	r := NoviRegistar()
	p := r.Pokreni("proba", "ana", "", func(p *Posao) error {
		p.Zavrsi("izdano 3 letve", map[string]int{"promijenjenih": 3})
		return nil
	})
	s := cekajKraj(t, p)
	if s.Sazetak != "izdano 3 letve" {
		t.Errorf("sažetak %q", s.Sazetak)
	}
	plod, ok := p.Plod().(map[string]int)
	if !ok || plod["promijenjenih"] != 3 {
		t.Errorf("plod %v", p.Plod())
	}
}
