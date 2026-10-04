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
