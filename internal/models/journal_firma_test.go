package models

import "testing"

// Dnevnik vodi firma osobe kad naslovnica kao izvođača navodi firmu s
// računa osobe ili osobu kao voditelja usluga; nazivi se uspoređuju bez
// obzira na velika slova, interpunkciju i razmake, kao cijele riječi i kad
// jedan sadrži drugi. Prazan upis ne navodi nikoga. Podaci su izmišljeni.
func TestDnevnikVodiFirma(t *testing.T) {
	marko := &User{FullName: "Marko Markić", OrgName: "Primjer d.o.o."}
	slucajevi := []struct {
		naziv     string
		j         Journal
		u         *User
		ocekivano bool
	}{
		{"isti naziv", Journal{Contractor: "Primjer d.o.o."}, marko, true},
		{"naziv i sjedište", Journal{Contractor: "PRIMJER  d.o.o., Primjerska 1, Primjerica"}, marko, true},
		{"račun nosi i sjedište", Journal{Contractor: "Primjer"}, &User{OrgName: "Primjer d.o.o., Primjerska 1"}, true},
		{"druga firma", Journal{Contractor: "Primjerak d.o.o."}, marko, false},
		{"voditelj usluga", Journal{Contractor: "Drugi radovi d.o.o.", ContractorLead: "marko markić, ing."}, marko, true},
		{"drugi voditelj", Journal{Contractor: "Drugi radovi d.o.o.", ContractorLead: "Ivo Ivić"}, marko, false},
		{"prazna naslovnica", Journal{}, marko, false},
		{"račun bez firme", Journal{Contractor: "Primjer d.o.o."}, &User{FullName: "Ana Anić"}, false},
		{"bez osobe", Journal{Contractor: "Primjer d.o.o."}, nil, false},
	}
	for _, s := range slucajevi {
		if got := s.j.VodiFirma(s.u); got != s.ocekivano {
			t.Errorf("%s: %v, očekivano %v", s.naziv, got, s.ocekivano)
		}
	}
}
