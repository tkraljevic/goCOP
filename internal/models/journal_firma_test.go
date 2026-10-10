package models

import "testing"

// Dnevnik vodi firma osobe kad naslovnica kao izvođača navodi firmu s
// računa osobe ili osobu kao voditelja usluga. Uspoređuje se naziv do
// pravnog oblika (d.o.o., d.d., j.d.o.o., …) i do prvog zareza, bez obzira
// na velika slova, interpunkciju i razmake, i mora biti isti: sjedište iza
// njega se ne gleda, a dio naziva nije ista firma. Prazan upis ne navodi
// nikoga. Podaci su izmišljeni.
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
		// pravni oblik napisan bez točaka, velikim slovima ili s razmacima
		{"DD i d.d.", Journal{Contractor: "PRIMJER DD Primjerica"}, &User{OrgName: "Primjer d.d."}, true},
		{"dd i d.d.", Journal{Contractor: "Primjer dd"}, &User{OrgName: "Primjer d.d."}, true},
		{"d.o.o. i doo", Journal{Contractor: "Primjer d.o.o."}, &User{OrgName: "Primjer doo"}, true},
		{"d. o. o. s razmacima", Journal{Contractor: "Primjer d. o. o., Primjerica"}, &User{OrgName: "Primjer d.o.o."}, true},
		{"j.d.o.o.", Journal{Contractor: "Primjer j.d.o.o."}, &User{OrgName: "PRIMJER JDOO"}, true},
		{"oblik na početku naziva", Journal{Contractor: "KD Primjer d.o.o."}, &User{OrgName: "KD Primjer doo"}, true},
		// dio naziva nije ista firma
		{"račun nosi dio naziva", Journal{Contractor: "Primjer Primjerica d.d."}, &User{OrgName: "Primjer"}, false},
		{"naslovnica nosi dio naziva", Journal{Contractor: "Primjer"}, &User{OrgName: "Primjer Primjerica d.d."}, false},
		{"naslovnica nosi samo mjesto", Journal{Contractor: "Primjerica"}, &User{OrgName: "Primjer d.o.o., Primjerica"}, false},
		{"samo prezime voditelja", Journal{ContractorLead: "Markić"}, &User{FullName: "Marko Markić"}, false},
		{"račun s prezimenom", Journal{ContractorLead: "Marko Markić"}, &User{FullName: "Markić"}, false},
	}
	for _, s := range slucajevi {
		if got := s.j.VodiFirma(s.u); got != s.ocekivano {
			t.Errorf("%s: %v, očekivano %v", s.naziv, got, s.ocekivano)
		}
	}
}

// Jezgra naziva: do prvog zareza i do pravnog oblika, bez interpunkcije;
// pravni oblik na početku naziva i slova koja ne čine pravni oblik ostaju
// dio naziva
func TestJezgraNaziva(t *testing.T) {
	for upis, jezgra := range map[string]string{
		"Primjer d. o. o. Primjerica":  "primjer",
		"PRIMJER DOO":                  "primjer",
		"Primjer j.t.d., Primjerica 1": "primjer",
		"Primjer k.d.":                 "primjer",
		"KD Primjer d.o.o.":            "kd primjer",
		"Primjer A":                    "primjer a",
		"Primjer A d.d.":               "primjer a",
		"Marko Markić, ing.":           "marko markić",
		"":                             "",
	} {
		if got := jezgraNaziva(upis); got != jezgra {
			t.Errorf("%q: %q, očekivano %q", upis, got, jezgra)
		}
	}
}
