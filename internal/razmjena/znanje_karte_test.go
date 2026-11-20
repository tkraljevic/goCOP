package razmjena

import (
	"strings"
	"testing"
)

// Znanje o karti druge strane prolazi samo s ispravnim otiskom i brojevima
// u granicama; ostalo otpada, jer ga stranica prikazuje
func TestOcisceneKarta(t *testing.T) {
	otisak := strings.Repeat("ab", 32)
	for _, c := range []struct {
		ime  string
		ulaz *ZnanjeKarte
		zeli *ZnanjeKarte
	}{
		{"nema znanja", nil, nil},
		{"nema karte", &ZnanjeKarte{}, &ZnanjeKarte{}},
		{"složena", &ZnanjeKarte{Otisak: otisak, Slozena: true, Dijelova: 10, Svih: 10}, &ZnanjeKarte{Otisak: otisak, Slozena: true, Dijelova: 10, Svih: 10}},
		{"kriv otisak", &ZnanjeKarte{Otisak: "<script>"}, nil},
		{"velika slova", &ZnanjeKarte{Otisak: strings.ToUpper(otisak)}, nil},
		{"brojevi izvan granica", &ZnanjeKarte{Otisak: otisak, Dijelova: 99999, Svih: -3}, &ZnanjeKarte{Otisak: otisak}},
		{"više dijelova nego svih", &ZnanjeKarte{Otisak: otisak, Dijelova: 9, Svih: 5}, &ZnanjeKarte{Otisak: otisak, Dijelova: 5, Svih: 5}},
		{"složena bez otiska", &ZnanjeKarte{Slozena: true}, &ZnanjeKarte{}},
	} {
		got := Znanje{Karta: c.ulaz}.Ocisceno().Karta
		if (got == nil) != (c.zeli == nil) || got != nil && *got != *c.zeli {
			t.Errorf("%s: %+v", c.ime, got)
		}
	}
}
