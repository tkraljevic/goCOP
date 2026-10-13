package models

import (
	"testing"
	"time"
)

// Rukovoditelj dionice piše samo na svojim dionicama: dionica s popisa
// dužnosti daje pravo upisa i bez sektora i područja u pozivu, tuđa dionica
// ne, a sektor i područje dužnosti sami po sebi ne daju ništa.
func TestPisanjeNaDioniciDuznosti(t *testing.T) {
	p := ovlastiZa(aktivna(Duty{Role: RoleSectionLeader, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16),
		SectionCodes: "B.16.1, B.16.2"}))
	for _, s := range []struct {
		sektor  string
		podr    int
		dionica string
		ocek    bool
	}{
		{"", 0, "B.16.2", true},
		{"", 0, "B.16.1", true},
		{"", 0, "B.16.3", false},
		{"B", 0, "", false},
		{"", 16, "", false},
		{"", 0, "", false},
	} {
		if got := p.HasWriteAccess(s.sektor, s.podr, s.dionica); got != s.ocek {
			t.Errorf("HasWriteAccess(%q, %d, %q) = %v, očekivano %v", s.sektor, s.podr, s.dionica, got, s.ocek)
		}
	}
}

// Val koji prag samo dotakne jednim mjerenjem ima stupanj bez raspona:
// početak i kraj su isti trenutak, trajanje je nula. Isti trenutak vrate već
// prijelazGore i prijelazDolje. Zaštita u sastaviVal koja raniji kraj svodi
// na početak ovdje ništa ne mijenja, a s nizom složenim po vremenu kraj nikad
// nije raniji od početka; test čuva ishod prijelaza, ne tu zaštitu.
func TestValKojiPragSamoDotakne(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	pragovi := []PragObrane{{Faza: PhasePrep, Cm: 200}, {Faza: PhaseRegular, Cm: 400}}
	v := Valovi(dnevni(p, 100, 400, 100), pragovi)
	if len(v) != 1 || len(v[0].Stupnjevi) != 2 {
		t.Fatalf("valovi: %+v", v)
	}
	r := v[0].Stupnjevi[1]
	dan := p.AddDate(0, 0, 1)
	if !r.Pocelo.Equal(dan) || !r.Zavrsilo.Equal(dan) || r.Raspon() != 0 || r.TrajanjeIznad != 0 || r.Navrata != 1 {
		t.Errorf("dotaknuta redovna: %+v", r)
	}
	// prag od 200 cm voda prijeđe 16 h prije vrha i padne ispod njega 16 h iza
	if v[0].Stupnjevi[0].Trajanje != 32*time.Hour {
		t.Errorf("pripremno uz dotaknutu redovnu: %v, očekivano 32 h", v[0].Stupnjevi[0].Trajanje)
	}
}

// Val ispod svih pragova nije val; pragovi zadani obrnutim redom ne daju
// negativno trajanje nižeg stanja nego nulu.
func TestSastavljanjeValaRubovi(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	niz := dnevni(p, 100, 300, 500, 300, 100)
	if v, ok := sastaviVal(niz, 1, 3, []PragObrane{{Faza: PhasePrep, Cm: 600}}); ok || len(v.Stupnjevi) != 0 || v.VrhCm != 500 {
		t.Errorf("val ispod praga: %v %+v", ok, v)
	}

	obrnuto := []PragObrane{{Faza: PhaseRegular, Cm: 400}, {Faza: PhasePrep, Cm: 200}}
	v, ok := sastaviVal(niz, 1, 3, obrnuto)
	if !ok || len(v.Stupnjevi) != 2 {
		t.Fatalf("val s obrnutim pragovima: %v %+v", ok, v)
	}
	if v.Stupnjevi[0].Stupanj != PhaseRegular || v.Stupnjevi[0].Trajanje != 0 {
		t.Errorf("redovna ispred pripremnog: %+v", v.Stupnjevi[0])
	}
	if v.Stupnjevi[1].Trajanje != v.Stupnjevi[1].TrajanjeIznad || v.Stupnjevi[1].Trajanje != 72*time.Hour {
		t.Errorf("pripremno kao zadnji stupanj: %+v", v.Stupnjevi[1])
	}
}

// Zbroj bez pragova je prazan; stupanj vala kojeg nema među pragovima ne
// ulazi u zbroj; stupanj na rubu niza označi cijeli redak zbroja.
func TestZbrojValovaRubovi(t *testing.T) {
	if z := ZbrojValova([]Val{{Stupnjevi: []StupanjVala{{Stupanj: PhasePrep, Trajanje: time.Hour}}}}, nil); z != nil {
		t.Errorf("zbroj bez pragova: %+v", z)
	}
	kad := time.Date(2013, 6, 2, 0, 0, 0, 0, time.UTC)
	valovi := []Val{
		{Stupnjevi: []StupanjVala{
			{Stupanj: PhasePrep, Trajanje: 5 * time.Hour, TrajanjeIznad: 9 * time.Hour, Pocelo: kad, KrajNaRubu: true},
			{Stupanj: PhaseState, Trajanje: 4 * time.Hour, TrajanjeIznad: 4 * time.Hour},
		}},
		{Stupnjevi: []StupanjVala{{Stupanj: PhasePrep, Trajanje: 2 * time.Hour, TrajanjeIznad: 2 * time.Hour}}},
	}
	z := ZbrojValova(valovi, []PragObrane{{Faza: PhasePrep, Cm: 200}})
	if len(z) != 1 {
		t.Fatalf("zbroj: %+v", z)
	}
	ocek := ZbrojStupnja{Stupanj: PhasePrep, PragCm: 200, Valova: 2, Ukupno: 7 * time.Hour, UkupnoIznad: 11 * time.Hour,
		Najdulji: 5 * time.Hour, NajduljiKad: kad, NaRubu: true}
	if z[0] != ocek {
		t.Errorf("zbroj pripremnog: %+v\nočekivano %+v", z[0], ocek)
	}
}
