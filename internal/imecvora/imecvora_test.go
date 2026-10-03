package imecvora

import (
	"strings"
	"testing"
)

func TestOblikImena(t *testing.T) {
	for _, dobro := range []string{"pperic-thinkpad", "cop-osijek-unraid", "cop-osijek-node", Stari, "w10", "a1b"} {
		if err := Provjeri(dobro); err != nil {
			t.Errorf("%q odbijeno: %v", dobro, err)
		}
	}
	for _, lose := range []string{"", "ab", "-pocinje", "zavrsava-", "Velika", "kvačica", "dvije--crtice", "razmak u imenu", strings.Repeat("a", 41)} {
		if Provjeri(lose) == nil {
			t.Errorf("%q prihvaćeno", lose)
		}
	}
}

func TestOcistiIPredlozi(t *testing.T) {
	slucajevi := map[string]string{
		"Pero Perić — ThinkPad":    "pero-peric-thinkpad",
		"  ŠTEFICA_Žižić-laptop!!  ": "stefica-zizic-laptop",
		"DESKTOP-7Q2ĐAB":             "desktop-7q2dab",
		"---":                        "",
	}
	for ulaz, zelim := range slucajevi {
		if got := Ocisti(ulaz); got != zelim {
			t.Errorf("Ocisti(%q) = %q, želim %q", ulaz, got, zelim)
		}
	}
	if got := Predlozi("pperic", "ThinkCentre-5"); got != "pperic-thinkcentre-5" || Provjeri(got) != nil {
		t.Errorf("Predlozi: %q", got)
	}
	if got := Ocisti(strings.Repeat("ab-", 30)); len(got) > 40 || Provjeri(got) != nil {
		t.Errorf("predugo ime nije skraćeno ispravno: %q", got)
	}
}

func TestNasumicnoJeJedinstvenoIIspravno(t *testing.T) {
	vidjeno := map[string]bool{}
	for i := 0; i < 200; i++ {
		ime := Nasumicno("ThinkCentre 5")
		if err := Provjeri(ime); err != nil {
			t.Fatalf("%q: %v", ime, err)
		}
		if !strings.HasPrefix(ime, "thinkcentre-5-") {
			t.Fatalf("%q ne počinje imenom računala", ime)
		}
		vidjeno[ime] = true
	}
	if len(vidjeno) < 190 {
		t.Errorf("premalo različitih imena: %d od 200", len(vidjeno))
	}
	if ime := Nasumicno(""); Provjeri(ime) != nil || !strings.HasPrefix(ime, "cvor-") {
		t.Errorf("bez imena računala: %q", ime)
	}
	if ime := Nasumicno(strings.Repeat("x", 60)); Provjeri(ime) != nil {
		t.Errorf("dugo ime računala: %q", ime)
	}
}
