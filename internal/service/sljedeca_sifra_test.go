package service

import "testing"

// Dionice sektora B unose se u nizu — 65 ih je u pet područja — pa program
// predlaže prvi slobodan broj. Prijedlog se smije prepisati: niz zna
// preskakati, a ukinuta dionica ne vraća svoj broj.
func TestSljedecaSifraPredlazePrviSlobodan(t *testing.T) {
	slucajevi := []struct {
		ime       string
		predmetak string
		sifre     []string
		zelim     string
	}{
		{"prazno područje", "B.15.", nil, "B.15.1"},
		{"dvije upisane", "B.34.", []string{"B.34.1", "B.34.2"}, "B.34.3"},
		{"niz preskače", "B.17.", []string{"B.17.1", "B.17.5"}, "B.17.6"},
		{"neporedano", "B.16.", []string{"B.16.3", "B.16.1", "B.16.2"}, "B.16.4"},
		{"dvoznamenkasti", "B.34.", []string{"B.34.9", "B.34.10"}, "B.34.11"},
		{"tuđe područje se ne broji", "B.15.", []string{"B.34.7", "B.15.1"}, "B.15.2"},
		// Šifra susjednog područja počinje istim znamenkama, ali predmetak
		// završava točkom pa se ne podudara.
		{"slično područje", "B.3.", []string{"B.34.9"}, "B.3.1"},
		{"rep nije broj", "B.18.", []string{"B.18.2a", "B.18.1"}, "B.18.2"},
	}
	for _, s := range slucajevi {
		if got := sljedecaSifra(s.predmetak, s.sifre); got != s.zelim {
			t.Errorf("%s: dobiveno %q, očekivano %q", s.ime, got, s.zelim)
		}
	}
}
