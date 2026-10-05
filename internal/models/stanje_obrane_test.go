package models

import (
	"strings"
	"testing"
	"time"
)

// akt je ovjeren akt za dionicu P.1.1 u zadani sat (studeni 2026.)
func akt(id, radnja string, stupanj DefensePhase, dan, sat int) Akt {
	return Akt{ID: id, Radnja: radnja, Stupanj: stupanj, Status: AktOvjeren,
		Vrijedi: time.Date(2026, 11, dan, sat, 0, 0, 0, Zagreb), Dionice: []AktDionica{{Code: "P.1.1"}}}
}

func kad(dan, sat int) time.Time { return time.Date(2026, 11, dan, sat, 0, 0, 0, Zagreb) }

// stadiji su aktivni stadiji kao tekst, od najnižeg
func stadiji(s StanjeObrane) string {
	var out []string
	for _, x := range s.Aktivni {
		out = append(out, string(x.Stupanj))
	}
	return strings.Join(out, ",")
}

// Postupno: pripremno, redovna preko njega, prekid redovne vraća pripremno,
// prekid pripremnog završava obranu (primjer iz nacrta)
func TestStanjeObranePostupno(t *testing.T) {
	akti := []Akt{
		akt("a4", AktPrekid, PhasePrep, 5, 7),
		akt("a2", AktUspostava, PhaseRegular, 2, 14),
		akt("a1", AktUspostava, PhasePrep, 1, 8),
		akt("a3", AktPrekid, PhaseRegular, 4, 9),
	}
	for _, tc := range []struct {
		t       time.Time
		stadiji string
		najvisi DefensePhase
		traje   bool
	}{
		{kad(1, 7), "", PhaseNormal, false},
		{kad(1, 8), "PRIPREMNO", PhasePrep, true},
		{kad(3, 0), "PRIPREMNO,REDOVNA", PhaseRegular, true},
		{kad(4, 9), "PRIPREMNO", PhasePrep, true},
		{kad(5, 7), "", PhaseNormal, false},
	} {
		s, greske := StanjeDionice(akti, "P.1.1", tc.t)
		if stadiji(s) != tc.stadiji || s.Najvisi() != tc.najvisi || s.Traje() != tc.traje || len(greske) != 0 {
			t.Errorf("%s: %q %s %v %v", tc.t.Format("2.1. 15:04"), stadiji(s), s.Najvisi(), s.Traje(), greske)
		}
	}
	if s, _ := StanjeDionice(akti, "P.1.1", kad(3, 0)); s.Aktivni[1].AktID != "a2" || !s.Aktivni[1].Od.Equal(kad(2, 14)) {
		t.Errorf("redovna: %+v", s.Aktivni[1])
	}
}

// Odmah redovna, bez pripremnog; kad se prekine, zakašnjelo pripremno
// proglašava se novim aktom u istom trenutku (prekid ide prije uspostave)
func TestStanjeObraneOdmahViseZakasnjeloNize(t *testing.T) {
	akti := []Akt{
		akt("b3", AktUspostava, PhasePrep, 6, 10),
		akt("b1", AktUspostava, PhaseRegular, 3, 2),
		akt("b2", AktPrekid, PhaseRegular, 6, 10),
		akt("b4", AktPrekid, PhasePrep, 8, 7),
	}
	for sat, ocekivano := range map[time.Time]string{kad(3, 2): "REDOVNA", kad(6, 10): "PRIPREMNO", kad(8, 7): ""} {
		if s, g := StanjeDionice(akti, "P.1.1", sat); stadiji(s) != ocekivano || len(g) != 0 {
			t.Errorf("%s: %q %v", sat.Format("2.1. 15:04"), stadiji(s), g)
		}
	}
	// i sve četiri u istom trenutku, pa dolje obrnutim redom
	sve := []Akt{
		akt("c4", AktUspostava, PhaseState, 1, 0), akt("c2", AktUspostava, PhaseRegular, 1, 0),
		akt("c1", AktUspostava, PhasePrep, 1, 0), akt("c3", AktUspostava, PhaseEmergency, 1, 0),
		akt("d1", AktPrekid, PhaseState, 2, 0), akt("d2", AktPrekid, PhaseEmergency, 2, 0),
	}
	if s, g := StanjeDionice(sve, "P.1.1", kad(1, 0)); stadiji(s) != "PRIPREMNO,REDOVNA,IZVANREDNA,IZVANREDNO_STANJE" || len(g) != 0 {
		t.Errorf("sve četiri: %q %v", stadiji(s), g)
	}
	if s, g := StanjeDionice(sve, "P.1.1", kad(2, 0)); s.Najvisi() != PhaseRegular || len(g) != 0 {
		t.Errorf("nakon dva prekida: %q %v", stadiji(s), g)
	}
}

// Nacrt, akt druge dionice i akt koji još nije stupio na snagu ne ulaze u
// stanje; nemoguć akt (stigao razmjenom) ne mijenja stanje i javlja se
func TestStanjeObraneRubovi(t *testing.T) {
	nacrt := akt("n1", AktUspostava, PhaseRegular, 1, 8)
	nacrt.Status = AktNacrt
	tudja := akt("n2", AktUspostava, PhaseRegular, 1, 8)
	tudja.Dionice = []AktDionica{{Code: "P.1.2"}}
	unaprijed := akt("n3", AktUspostava, PhasePrep, 9, 20)
	if s, g := StanjeDionice([]Akt{nacrt, tudja, unaprijed}, "P.1.1", kad(9, 19)); s.Traje() || len(g) != 0 {
		t.Errorf("ništa ne vrijedi: %q %v", stadiji(s), g)
	}
	if s, _ := StanjeDionice([]Akt{unaprijed}, "P.1.1", kad(9, 20)); s.Najvisi() != PhasePrep {
		t.Errorf("akt stupa na snagu kad u njemu piše: %q", stadiji(s))
	}

	for _, tc := range []struct {
		ime    string
		akti   []Akt
		stanje string
		razlog string
	}{
		{"prekid stadija koji ne traje", []Akt{akt("x1", AktPrekid, PhaseRegular, 1, 8)}, "", "Redovna obrana ne traje"},
		{"prekid nižeg dok viši traje", []Akt{akt("x1", AktUspostava, PhasePrep, 1, 8), akt("x2", AktUspostava, PhaseRegular, 1, 9), akt("x3", AktPrekid, PhasePrep, 1, 10)}, "PRIPREMNO,REDOVNA", "traje redovna obrana; ukida se samo najviši"},
		{"uspostava stadija koji traje", []Akt{akt("x1", AktUspostava, PhasePrep, 1, 8), akt("x2", AktUspostava, PhasePrep, 1, 9)}, "PRIPREMNO", "Pripremno stanje već traje"},
		{"uspostava nižeg dok viši traje", []Akt{akt("x1", AktUspostava, PhaseRegular, 1, 8), akt("x2", AktUspostava, PhasePrep, 1, 9)}, "REDOVNA", "traje redovna obrana; niži stadij proglašava se kad viši završi"},
		{"nepoznat stadij", []Akt{akt("x1", AktUspostava, PhaseNormal, 1, 8)}, "", "nepoznat stadij"},
		{"nepoznata radnja", []Akt{akt("x1", "PRODULJENJE", PhasePrep, 1, 8)}, "", "nepoznata radnja"},
	} {
		s, g := StanjeDionice(tc.akti, "P.1.1", kad(2, 0))
		if stadiji(s) != tc.stanje || len(g) != 1 || !strings.Contains(g[0].Error(), tc.razlog) || !strings.HasPrefix(g[0].Error(), "P.1.1: ") {
			t.Errorf("%s: %q %v", tc.ime, stadiji(s), g)
		}
	}
}

// Ovjera pušta akt samo kad je cijeli slijed s njim moguć; zatečene greške
// (npr. stigle razmjenom) ne priječe ovjeru ispravnog akta
func TestProvjeriSlijedAkta(t *testing.T) {
	ovjereni := []Akt{akt("o1", AktUspostava, PhasePrep, 1, 8), akt("o2", AktUspostava, PhaseRegular, 2, 14)}
	novi := func(id, radnja string, stupanj DefensePhase, dan, sat int) Akt {
		a := akt(id, radnja, stupanj, dan, sat)
		a.Status = AktNacrt
		return a
	}
	if err := ProvjeriSlijed(ovjereni, novi("p1", AktPrekid, PhaseRegular, 4, 9)); err != nil {
		t.Errorf("prekid redovne: %v", err)
	}
	if err := ProvjeriSlijed(ovjereni, novi("p2", AktPrekid, PhasePrep, 4, 9)); err == nil || !strings.Contains(err.Error(), "ukida se samo najviši") {
		t.Errorf("prekid pripremnog dok redovna traje: %v", err)
	}
	if err := ProvjeriSlijed(ovjereni, novi("p3", AktUspostava, PhaseEmergency, 3, 0)); err != nil {
		t.Errorf("izvanredna preko redovne: %v", err)
	}
	// umetnut ispred već ovjerenog kasnijeg: prekid redovne 4. 11. ne bi
	// više bio najviši kad bi se 3. 11. proglasila izvanredna
	sPrekidom := append(append([]Akt(nil), ovjereni...), akt("o3", AktPrekid, PhaseRegular, 4, 9))
	if err := ProvjeriSlijed(sPrekidom, novi("p4", AktUspostava, PhaseEmergency, 3, 0)); err == nil || !strings.Contains(err.Error(), "već ovjeren kasniji akt") {
		t.Errorf("izvanredna ispred ovjerenog prekida redovne: %v", err)
	}
	// zatečena greška ne priječe ovjeru
	sGreskom := append(append([]Akt(nil), ovjereni...), akt("o4", AktPrekid, PhaseState, 2, 15))
	if err := ProvjeriSlijed(sGreskom, novi("p5", AktPrekid, PhaseRegular, 4, 9)); err != nil {
		t.Errorf("zatečena greška priječi ispravan akt: %v", err)
	}
	// akt bez dionica nema što provjeriti
	bez := novi("p6", AktPrekid, PhaseState, 4, 9)
	bez.Dionice = nil
	if err := ProvjeriSlijed(ovjereni, bez); err != nil {
		t.Errorf("akt bez dionica: %v", err)
	}
}

// Vrh je stadij koji vrijedi, pozadina niži od višeg prema nižem; najavljeni
// su akti dionice koji još nisu stupili na snagu, redom
func TestVrhPozadinaINajavljeni(t *testing.T) {
	if v := (StanjeObrane{}).Vrh(); v.Stupanj != "" || (StanjeObrane{}).Pozadina() != nil {
		t.Errorf("bez obrane: %+v", v)
	}
	akti := []Akt{
		akt("e1", AktUspostava, PhasePrep, 1, 8), akt("e2", AktUspostava, PhaseRegular, 1, 9),
		akt("e3", AktUspostava, PhaseEmergency, 1, 10), akt("e5", AktPrekid, PhasePrep, 5, 7),
		akt("e4", AktPrekid, PhaseEmergency, 3, 7),
	}
	s, _ := StanjeDionice(akti, "P.1.1", kad(2, 0))
	if s.Vrh().AktID != "e3" || len(s.Pozadina()) != 2 || s.Pozadina()[0].AktID != "e2" || s.Pozadina()[1].AktID != "e1" {
		t.Errorf("vrh %+v, pozadina %+v", s.Vrh(), s.Pozadina())
	}
	n := NajavljeniAkti(akti, "P.1.1", kad(2, 0))
	if len(n) != 2 || n[0].ID != "e4" || n[1].ID != "e5" || len(NajavljeniAkti(akti, "P.1.2", kad(2, 0))) != 0 {
		t.Errorf("najavljeni: %+v", n)
	}
}

// Stanje zna je li dionica imala akata: tada odlučuju akti i kad obrana ne
// traje; stanja sektora su sve dionice s ovjerenim aktima
func TestStanjeIzAkataIStanjaDionica(t *testing.T) {
	druga := akt("f3", AktUspostava, PhaseRegular, 1, 8)
	druga.Dionice = []AktDionica{{Code: "P.1.2"}, {Code: "P.1.1"}}
	akti := []Akt{akt("f1", AktUspostava, PhasePrep, 1, 6), akt("f2", AktPrekid, PhasePrep, 1, 7), druga}
	if s, _ := StanjeDionice(akti[:2], "P.1.1", kad(2, 0)); s.Traje() || !s.IzAkata {
		t.Errorf("prekinuta obrana iz akata: %+v", s)
	}
	if s, _ := StanjeDionice(akti, "P.9.9", kad(2, 0)); s.IzAkata {
		t.Error("dionica bez akata")
	}
	sva := StanjaDionica(akti, kad(2, 0))
	if len(sva) != 2 || sva["P.1.2"].Najvisi() != PhaseRegular || sva["P.1.1"].Najvisi() != PhaseRegular || !sva["P.1.1"].IzAkata {
		t.Errorf("stanja dionica: %+v", sva)
	}
}

// Poništen akt ne ulazi u stanje; poništava se najkasniji akt dionice
func TestStornoIKasnijiAkt(t *testing.T) {
	a1, a2, a3 := akt("g1", AktUspostava, PhasePrep, 1, 8), akt("g2", AktUspostava, PhaseRegular, 2, 8), akt("g3", AktPrekid, PhaseRegular, 3, 8)
	ponisten := a2
	ponisten.Storno = &StornoAkta{Razlog: "pogrešan stadij"}
	if s, g := StanjeDionice([]Akt{a1, ponisten}, "P.1.1", kad(4, 0)); s.Najvisi() != PhasePrep || len(g) != 0 {
		t.Errorf("poništen akt u stanju: %+v %v", s, g)
	}
	akti := []Akt{a3, a1, a2}
	if k, d, ima := KasnijiAkt(akti, a2); !ima || k.ID != "g3" || d != "P.1.1" {
		t.Errorf("kasniji od g2: %v %s %v", k.ID, d, ima)
	}
	if _, _, ima := KasnijiAkt(akti, a3); ima {
		t.Error("g3 je najkasniji")
	}
	if !ponisten.Storniran() || a2.Storniran() || len(a2.PorukaStorna()) != 0 || !strings.HasPrefix(string(ponisten.PorukaStorna()), "goCOP-storno-v1|g2|") {
		t.Errorf("storno: %q", ponisten.PorukaStorna())
	}
}

// Razdoblja obrane: postupno gore i dolje je jedno razdoblje s najvišim
// stadijem; odmah redovna pa zakašnjelo pripremno su dva; nemoguć akt se
// preskače; razdoblje koje traje nema kraja
func TestRazdobljaObrane(t *testing.T) {
	akti := []Akt{
		akt("h1", AktUspostava, PhasePrep, 1, 8), akt("h2", AktUspostava, PhaseRegular, 2, 8),
		akt("h3", AktPrekid, PhaseRegular, 3, 8), akt("h9", AktPrekid, PhaseState, 3, 9),
		akt("h4", AktPrekid, PhasePrep, 4, 8),
		akt("h5", AktUspostava, PhaseRegular, 6, 8), akt("h6", AktPrekid, PhaseRegular, 7, 8),
		akt("h7", AktUspostava, PhasePrep, 7, 8),
	}
	r := RazdobljaObrane(akti, "P.1.1", kad(9, 0))
	if len(r) != 3 {
		t.Fatalf("razdoblja: %+v", r)
	}
	if !r[0].Od.Equal(kad(1, 8)) || r[0].Do == nil || !r[0].Do.Equal(kad(4, 8)) || r[0].Najvisi != PhaseRegular || len(r[0].Akti) != 4 || r[0].Akti[0].ID != "h1" {
		t.Errorf("prvo: %+v", r[0])
	}
	if r[1].Najvisi != PhaseRegular || r[1].Do == nil || !r[1].Do.Equal(kad(7, 8)) || r[1].Akti[1].ID != "h6" {
		t.Errorf("drugo: %+v", r[1])
	}
	if r[2].Do != nil || r[2].Najvisi != PhasePrep || r[2].Akti[0].ID != "h7" {
		t.Errorf("treće (traje): %+v", r[2])
	}
	if len(RazdobljaObrane(akti, "P.1.1", kad(1, 7))) != 0 {
		t.Error("prije prvog akta nema razdoblja")
	}
	prvi, opet, druga := IDEpizodeIzAkta("P.1.1", "h1"), IDEpizodeIzAkta("P.1.1", "h1"), IDEpizodeIzAkta("P.1.2", "h1")
	if prvi != opet || prvi == druga {
		t.Error("identitet epizode iz akta")
	}
}

// naDionici premješta akt na zadanu dionicu
func naDionici(a Akt, dionica string) Akt {
	a.Dionice = []AktDionica{{Code: dionica}}
	return a
}

// Privremeno imenovanje vrijedi dok na dionicama traje redovna ili izvanredna
// obrana ili izvanredno stanje; pripremno stanje ga ne drži
func TestPrestanakRedovneObrane(t *testing.T) {
	postupno := []Akt{
		akt("a1", AktUspostava, PhasePrep, 1, 8),
		akt("a2", AktUspostava, PhaseRegular, 2, 14),
		akt("a3", AktUspostava, PhaseEmergency, 3, 10),
		akt("a4", AktPrekid, PhaseEmergency, 4, 6),
		akt("a5", AktPrekid, PhaseRegular, 4, 9),
		akt("a6", AktPrekid, PhasePrep, 5, 7),
	}
	jeKraj := func(got *PrestanakObrane, want time.Time, aktom string) bool {
		return got != nil && got.Kad.Equal(want) && got.Akt.ID == aktom
	}
	// imenovan prije proglašenja, u pripremnom stanju, u redovnoj i u izvanrednoj:
	// vrijedi do prekida redovne (pad s izvanredne na redovnu ga ne prekida)
	for _, od := range []time.Time{kad(1, 0), kad(1, 12), kad(2, 15), kad(3, 12)} {
		if k := PrestanakRedovneObrane(postupno, []string{"P.1.1"}, od); !jeKraj(k, kad(4, 9), "a5") {
			t.Errorf("od %s: %v", od.Format("2.1. 15:04"), k)
		}
	}
	// imenovan poslije redovne (u pripremnom ili poslije svega): nema kraja
	for _, od := range []time.Time{kad(4, 9), kad(4, 12), kad(6, 0)} {
		if k := PrestanakRedovneObrane(postupno, []string{"P.1.1"}, od); k != nil {
			t.Errorf("od %s: %v", od.Format("2.1. 15:04"), k)
		}
	}
	// redovna još traje: nema kraja
	if k := PrestanakRedovneObrane(postupno[:3], []string{"P.1.1"}, kad(2, 15)); k != nil {
		t.Errorf("traje: %v", k)
	}
	// izvanredno stanje odmah, bez nižih stadija, drži do svog prekida
	odmah := []Akt{akt("s1", AktUspostava, PhaseState, 1, 8), akt("s2", AktPrekid, PhaseState, 3, 8)}
	if k := PrestanakRedovneObrane(odmah, []string{"P.1.1"}, kad(1, 0)); !jeKraj(k, kad(3, 8), "s2") {
		t.Errorf("izvanredno stanje: %v", k)
	}
	// poništen prekid: redovna traje dalje
	ponisten := append([]Akt(nil), postupno[:5]...)
	ponisten[4].Storno = &StornoAkta{Razlog: "pogreška"}
	if k := PrestanakRedovneObrane(ponisten, []string{"P.1.1"}, kad(2, 15)); k != nil {
		t.Errorf("poništen prekid: %v", k)
	}
	// prekid koji stupa na snagu kasnije daje kraj u budućnosti
	kasnije := []Akt{akt("k1", AktUspostava, PhaseRegular, 1, 8), akt("k2", AktPrekid, PhaseRegular, 20, 8)}
	if k := PrestanakRedovneObrane(kasnije, []string{"P.1.1"}, kad(2, 0)); !jeKraj(k, kad(20, 8), "k2") {
		t.Errorf("kasnije: %v", k)
	}
	// nijedan akt ili tuđa dionica: nema kraja
	if k := PrestanakRedovneObrane(postupno, []string{"P.9.9"}, kad(1, 0)); k != nil {
		t.Errorf("tuđa dionica: %v", k)
	}
}

// Više dionica (ili cijelo branjeno područje): imenovanje vrijedi dok obrana
// traje na ijednoj; razdoblja koja se preklapaju ili dodiruju spajaju se
func TestPrestanakRedovneObraneViseDionica(t *testing.T) {
	akti := []Akt{
		naDionici(akt("b1", AktUspostava, PhaseRegular, 1, 8), "P.1.1"),
		naDionici(akt("b2", AktUspostava, PhaseRegular, 3, 8), "P.1.2"),
		naDionici(akt("b3", AktPrekid, PhaseRegular, 4, 8), "P.1.1"),
		naDionici(akt("b4", AktPrekid, PhaseRegular, 6, 8), "P.1.2"),
		// P.1.3 dodiruje: počinje točno kad P.1.2 prestaje
		naDionici(akt("b5", AktUspostava, PhaseRegular, 6, 8), "P.1.3"),
		naDionici(akt("b6", AktPrekid, PhaseRegular, 7, 8), "P.1.3"),
		// kasnije novo razdoblje na P.1.1, s prazninom između
		naDionici(akt("b7", AktUspostava, PhaseRegular, 10, 8), "P.1.1"),
		naDionici(akt("b8", AktPrekid, PhaseRegular, 12, 8), "P.1.1"),
	}
	sve := []string{"P.1.1", "P.1.2", "P.1.3"}
	for _, tc := range []struct {
		dionice []string
		od      time.Time
		kraj    time.Time
		aktom   string // akt koji je obranu ukinuo posljednji
	}{
		{sve, kad(1, 0), kad(7, 8), "b6"},
		{sve, kad(5, 0), kad(7, 8), "b6"},
		{[]string{"P.1.1", "P.1.2"}, kad(2, 0), kad(6, 8), "b4"},
		{[]string{"P.1.1"}, kad(2, 0), kad(4, 8), "b3"},
		{sve, kad(8, 0), kad(12, 8), "b8"},
	} {
		if k := PrestanakRedovneObrane(akti, tc.dionice, tc.od); k == nil || !k.Kad.Equal(tc.kraj) || k.Akt.ID != tc.aktom {
			t.Errorf("%v od %s: %+v, treba %s aktom %s", tc.dionice, tc.od.Format("2.1."), k, tc.kraj.Format("2.1. 15:04"), tc.aktom)
		}
	}
	// razdoblje koje traje (bez prekida) proguta kasnija na drugim dionicama
	otvoreno := []Akt{
		naDionici(akt("c1", AktUspostava, PhaseRegular, 1, 8), "P.1.1"),
		naDionici(akt("c2", AktUspostava, PhaseRegular, 2, 8), "P.1.2"),
		naDionici(akt("c3", AktPrekid, PhaseRegular, 3, 8), "P.1.2"),
	}
	if k := PrestanakRedovneObrane(otvoreno, []string{"P.1.1", "P.1.2"}, kad(1, 0)); k != nil {
		t.Errorf("otvoreno: %v", k)
	}
	// kasnije razdoblje koje traje produži raniji kraj
	produzeno := []Akt{
		naDionici(akt("d1", AktUspostava, PhaseRegular, 1, 8), "P.1.1"),
		naDionici(akt("d2", AktUspostava, PhaseRegular, 2, 8), "P.1.2"),
		naDionici(akt("d3", AktPrekid, PhaseRegular, 3, 8), "P.1.1"),
	}
	if k := PrestanakRedovneObrane(produzeno, []string{"P.1.1", "P.1.2"}, kad(1, 0)); k != nil {
		t.Errorf("produženo: %v", k)
	}
}
