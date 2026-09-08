package models

import (
	"testing"
	"time"
)

func dnevni(pocetak time.Time, cm ...float64) []HidroTocka {
	out := make([]HidroTocka, len(cm))
	for i, v := range cm {
		out[i] = HidroTocka{Kad: pocetak.AddDate(0, 0, i), Vrijednost: v}
	}
	return out
}

func pragoviPRIS() []PragObrane {
	return (Station{
		Prep:      Threshold{Cm: ptr(300)},
		Regular:   Threshold{Cm: ptr(500)},
		Emergency: Threshold{Cm: ptr(650)},
		State:     Threshold{Cm: ptr(800)},
	}).PragoviObrane()
}

func ptr(v int) *int { return &v }

// Stupnjevi obrane se gnijezde: dok traje izvanredna, traje i redovna, i
// pripremno stanje. Val koji dosegne izvanrednu mora dati tri stupnja, svaki
// sa svojim početkom i krajem, a viši uvijek unutar nižega.
func TestValGnijezdiStupnjeve(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	niz := dnevni(p, 200, 400, 600, 700, 600, 400, 200)
	valovi := Valovi(niz, pragoviPRIS())
	if len(valovi) != 1 {
		t.Fatalf("očekivan jedan val, dobiveno %d", len(valovi))
	}
	v := valovi[0]
	if len(v.Stupnjevi) != 3 {
		t.Fatalf("dosegnuta su tri stupnja, dobiveno %d: %+v", len(v.Stupnjevi), v.Stupnjevi)
	}
	if v.NajviseDosegnuto() != PhaseEmergency {
		t.Errorf("najviši stupanj: %v", v.NajviseDosegnuto())
	}
	if v.VrhCm != 700 || !v.VrhKad.Equal(p.AddDate(0, 0, 3)) {
		t.Errorf("vrh: %v u %v", v.VrhCm, v.VrhKad)
	}
	// gnijezdo: svaki viši stupanj počinje kasnije i završava ranije
	for i := 1; i < len(v.Stupnjevi); i++ {
		a, b := v.Stupnjevi[i-1], v.Stupnjevi[i]
		if !b.Pocelo.After(a.Pocelo) || !b.Zavrsilo.Before(a.Zavrsilo) {
			t.Errorf("%v nije unutar %v: %v–%v vs %v–%v",
				b.Stupanj, a.Stupanj, b.Pocelo, b.Zavrsilo, a.Pocelo, a.Zavrsilo)
		}
		// Iznad praga je ugniježđeno; vrijeme U STANJU nije — voda može
		// provesti više sati u izvanrednoj nego u pripremnom stanju.
		if b.TrajanjeIznad > a.TrajanjeIznad {
			t.Errorf("%v je iznad praga dulje od %v", b.Stupanj, a.Stupanj)
		}
	}
	// izvanredno stanje nije dosegnuto i ne smije se pojaviti
	if v.StupanjZa(PhaseState) != nil {
		t.Error("nedosegnut stupanj ne smije imati zapis")
	}
}

// Dnevni niz zna samo da je voda jučer bila ispod a danas iznad praga.
// Stavljanje prijelaza na mjerenje sustavno bi krivilo trajanje za do cijeli
// dan, pa se prijelaz pravocrtno interpolira između dva susjedna mjerenja.
func TestPrijelazSeInterpoliraIzmeduMjerenja(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	// 200 → 400 preko praga 300: točno na pola puta između dva dana
	niz := dnevni(p, 200, 400, 200)
	v := Valovi(niz, []PragObrane{{Faza: PhasePrep, Cm: 300}})
	if len(v) != 1 {
		t.Fatalf("očekivan jedan val, dobiveno %d", len(v))
	}
	s := v[0].Stupnjevi[0]
	wantOd := p.Add(12 * time.Hour)
	wantDo := p.AddDate(0, 0, 1).Add(12 * time.Hour)
	if !s.Pocelo.Equal(wantOd) {
		t.Errorf("početak %v, očekivan %v", s.Pocelo, wantOd)
	}
	if !s.Zavrsilo.Equal(wantDo) {
		t.Errorf("kraj %v, očekivan %v", s.Zavrsilo, wantDo)
	}
	if s.Trajanje != 24*time.Hour {
		t.Errorf("trajanje %v, očekivano 24 h", s.Trajanje)
	}
	if s.TrajanjeIznad != 24*time.Hour {
		t.Errorf("jedini stupanj: iznad praga %v, očekivano 24 h", s.TrajanjeIznad)
	}
	if s.Navrata != 1 || s.Prekidan() {
		t.Errorf("jedan prijelaz naviše: %+v", s)
	}
	if s.NaRubu() {
		t.Error("prijelaz je uhvaćen između mjerenja, nije na rubu niza")
	}
}

// Val koji na dan-dva padne ispod pripremnog pa se vrati u praksi je jedan
// val — obrana se ne prekida i ne proglašava nanovo. Dulji prekid dijeli.
func TestKratakPadIspodPragaNeDijeliVal(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	pragovi := []PragObrane{{Faza: PhasePrep, Cm: 300}}

	kratak := dnevni(p, 200, 400, 250, 400, 200) // ispod praga jedan dan
	if got := Valovi(kratak, pragovi); len(got) != 1 {
		t.Errorf("kratak pad je razbio val na %d", len(got))
	}
	dug := dnevni(p, 200, 400, 250, 250, 250, 250, 250, 400, 200) // pet dana
	if got := Valovi(dug, pragovi); len(got) != 2 {
		t.Errorf("dug prekid je dao %d valova, očekivana 2", len(got))
	}
}

// Val koji počinje prije prvog ili traje iza zadnjeg podatka nema uhvaćen
// prijelaz. Trajanje je tada donja granica i to se mora vidjeti, inače bi se
// odrezani val čitao kao potpun.
func TestValNaRubuNizaJeOznacen(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	pragovi := []PragObrane{{Faza: PhasePrep, Cm: 300}}

	odrezanPocetak := dnevni(p, 400, 400, 200)
	v := Valovi(odrezanPocetak, pragovi)[0].Stupnjevi[0]
	if !v.PocetakNaRubu || v.KrajNaRubu {
		t.Errorf("odrezan početak: %+v", v)
	}
	odrezanKraj := dnevni(p, 200, 400, 400)
	v = Valovi(odrezanKraj, pragovi)[0].Stupnjevi[0]
	if v.PocetakNaRubu || !v.KrajNaRubu {
		t.Errorf("odrezan kraj: %+v", v)
	}
}

// Valovi se čitaju od najnovijeg prema najstarijem.
func TestValoviIduOdNajnovijeg(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	niz := dnevni(p, 200, 400, 200, 200, 200, 200, 200, 200, 500, 200)
	v := Valovi(niz, []PragObrane{{Faza: PhasePrep, Cm: 300}})
	if len(v) != 2 {
		t.Fatalf("očekivana dva vala, dobiveno %d", len(v))
	}
	if !v[0].Pocelo.After(v[1].Pocelo) {
		t.Errorf("prvi val (%v) nije noviji od drugoga (%v)", v[0].Pocelo, v[1].Pocelo)
	}
	if v[0].VrhCm != 500 {
		t.Errorf("najnoviji val ima vrh %v", v[0].VrhCm)
	}
}

// Prag zapisan samo tekstom ne ulazi u izračun: iz „206,30 m n. m." se ne da
// računati, a nagađanjem bi se izmislio prijelaz kojeg nema.
func TestPragBezCentimetaraNeUlaziUIzracun(t *testing.T) {
	st := Station{
		Prep:      Threshold{Cm: ptr(300)},
		Regular:   Threshold{Raw: "206,30 m n. m."},
		Emergency: Threshold{Cm: ptr(650)},
	}
	p := st.PragoviObrane()
	if len(p) != 2 || p[0].Cm != 300 || p[1].Cm != 650 {
		t.Fatalf("pragovi: %+v", p)
	}
	niz := dnevni(time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC), 200, 400, 700, 400, 200)
	v := Valovi(niz, p)
	if len(v) != 1 || len(v[0].Stupnjevi) != 2 {
		t.Fatalf("stupnjevi: %+v", v)
	}
	if v[0].StupanjZa(PhaseRegular) != nil {
		t.Error("prag bez centimetara ne smije dati stupanj")
	}
}

// Postaja bez ijednog praga u centimetrima ne može dati valove, i to nije
// greška nego izostanak podatka.
func TestBezPragovaNemaValova(t *testing.T) {
	niz := dnevni(time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC), 200, 900, 200)
	if v := Valovi(niz, nil); v != nil {
		t.Errorf("bez pragova: %+v", v)
	}
	if v := Valovi(nil, pragoviPRIS()); v != nil {
		t.Errorf("bez niza: %+v", v)
	}
}

// Zbroj odgovara na pitanje koliko je koje stanje ukupno trajalo u nizu.
func TestZbrojValovaSabireStupnjevePoNizu(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	pragovi := pragoviPRIS()
	// dva vala: prvi do redovne, drugi samo do pripremnog
	niz := append(dnevni(p, 200, 400, 600, 400, 200),
		dnevni(p.AddDate(0, 0, 20), 200, 400, 200)...)
	z := ZbrojValova(Valovi(niz, pragovi), pragovi)
	if len(z) != 4 {
		t.Fatalf("zbroj mora imati redak po pragu, dobiveno %d", len(z))
	}
	if z[0].Stupanj != PhasePrep || z[0].Valova != 2 {
		t.Errorf("pripremno: %+v", z[0])
	}
	if z[1].Stupanj != PhaseRegular || z[1].Valova != 1 {
		t.Errorf("redovna: %+v", z[1])
	}
	if z[2].Valova != 0 || z[2].Ukupno != 0 {
		t.Errorf("nedosegnuta izvanredna mora biti prazna: %+v", z[2])
	}
	if z[0].Ukupno <= z[1].Ukupno {
		t.Error("pripremno stanje mora trajati dulje od redovne obrane")
	}
	if z[0].Najdulji <= 0 || z[0].NajduljiKad.IsZero() {
		t.Errorf("najdulji val: %+v", z[0])
	}
}

// Trajanje se čita u satima dok je kratko, u danima kad je dugo.
func TestTrajanjeHRBiraJedinicu(t *testing.T) {
	for _, s := range []struct {
		d    time.Duration
		want string
	}{
		{0, "—"}, {30 * time.Minute, "1 sat"}, {2 * time.Hour, "2 sata"},
		{5 * time.Hour, "5 sati"}, {21 * time.Hour, "21 sat"}, {47 * time.Hour, "47 sati"},
		{48 * time.Hour, "2 dana"}, {24 * 5 * time.Hour, "5 dana"},
		{24 * 21 * time.Hour, "21 dan"}, {24 * 42 * time.Hour, "42 dana"},
	} {
		if got := trajanjeHR(s.d); got != s.want {
			t.Errorf("%v → %q, očekivano %q", s.d, got, s.want)
		}
	}
}

// Unutar jednog vala voda prag zna prijeći više puta. Trajanje mora biti
// vrijeme STVARNO provedeno iznad praga, ne razmak od prvog uspona do zadnjeg
// pada — u Batininom valu 1920. to je razlika između 245 dana i dvanaest.
func TestTrajanjeNeUracunavaVrijemeIspodPraga(t *testing.T) {
	p := time.Date(1920, 1, 1, 0, 0, 0, 0, time.UTC)
	// dva odvojena vrha iznad 650, a između njih dugo ispod — ali sve vrijeme
	// iznad pripremnog, pa je to jedan val
	niz := dnevni(p, 200, 400, 700, 400, 400, 400, 400, 400, 400, 700, 400, 200)
	v := Valovi(niz, pragoviPRIS())
	if len(v) != 1 {
		t.Fatalf("očekivan jedan val, dobiveno %d", len(v))
	}
	iz := v[0].StupanjZa(PhaseEmergency)
	if iz == nil {
		t.Fatal("izvanredna obrana je dosegnuta")
	}
	if iz.Navrata != 2 || !iz.Prekidan() {
		t.Errorf("dva navrata iznad praga: %+v", iz)
	}
	if iz.Raspon() <= iz.Trajanje {
		t.Errorf("raspon (%v) mora biti dulji od trajanja (%v)", iz.Raspon(), iz.Trajanje)
	}
	// 400→700 siječe prag 650 na 83 % puta, 700→400 na 17 %: 8 sati po
	// navratu, dva navrata = 16 sati
	if iz.TrajanjeIznad != 16*time.Hour {
		t.Errorf("trajanje %v, očekivano 16 h", iz.TrajanjeIznad)
	}
	// pripremno stanje nije prekidano
	if pr := v[0].StupanjZa(PhasePrep); pr.Navrata != 1 || pr.Prekidan() {
		t.Errorf("pripremno stanje nije prekidano: %+v", pr)
	}
}

// Obrana se evidentira po stanjima koja se ne preklapaju: u obrascu Hrvatskih
// voda za Batinu i lipanjski val 2024. stoji pripremno stanje 433 h i redovna
// obrana 431 h — zbrojeno 864 h, koliko je cijeli val trajao. Ugniježđeno
// brojanje dalo bi 864 i 431, pa bi zbroj bio besmislen.
func TestTrajanjaStanjaSeNePreklapajuIZbrajajuUCijeliVal(t *testing.T) {
	p := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	// ravni platoi: 2 dana iznad 300, pa 4 dana iznad 500, pa 2 dana iznad 650
	niz := []HidroTocka{}
	dodaj := func(sati int, cm float64) {
		for i := 0; i < sati; i++ {
			niz = append(niz, HidroTocka{Kad: p.Add(time.Duration(len(niz)) * time.Hour), Vrijednost: cm})
		}
	}
	dodaj(1, 100)
	dodaj(48, 400) // pripremno
	dodaj(96, 600) // redovna
	dodaj(48, 700) // izvanredna
	dodaj(1, 100)

	v := Valovi(niz, pragoviPRIS())
	if len(v) != 1 {
		t.Fatalf("očekivan jedan val, dobiveno %d", len(v))
	}
	sati := func(f DefensePhase) float64 { return v[0].StupanjZa(f).Trajanje.Hours() }
	// prijelazi se interpoliraju, pa se dopušta sat odstupanja po granici
	blizu := func(got, want float64, ime string) {
		if got < want-2 || got > want+2 {
			t.Errorf("%s: %.0f h, očekivano oko %.0f h", ime, got, want)
		}
	}
	blizu(sati(PhasePrep), 48, "pripremno stanje")
	blizu(sati(PhaseRegular), 96, "redovna obrana")
	blizu(sati(PhaseEmergency), 48, "izvanredna obrana")

	// zbroj stanja mora dati cijeli val
	var zbroj time.Duration
	for _, s := range v[0].Stupnjevi {
		zbroj += s.Trajanje
	}
	if razlika := zbroj - v[0].Trajanje(); razlika > time.Hour || razlika < -time.Hour {
		t.Errorf("zbroj stanja %v ne daje cijeli val %v", zbroj, v[0].Trajanje())
	}
	// a vrijeme IZNAD praga ostaje ugniježđeno
	gore := func(f DefensePhase) float64 { return v[0].StupanjZa(f).TrajanjeIznad.Hours() }
	blizu(gore(PhasePrep), 192, "iznad pripremnog")
	blizu(gore(PhaseRegular), 144, "iznad redovne")
	blizu(gore(PhaseEmergency), 48, "iznad izvanredne")
}
