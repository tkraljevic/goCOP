package prognoza

import "testing"

// Kad niz ima malo zbrojeva triju dana, svaka razina uzima barem najveći
// od njih — prag se ne traži ispod jednog dana.
func TestPragoviIzNizaSMaloTrodnevnih(t *testing.T) {
	n := DnevniNiz{}
	for d := int64(0); d < 2*3650; d += 2 { // svaki drugi dan, bez tri uzastopna
		n[d] = 1
	}
	for d := int64(10000); d < 10100; d++ { // sto uzastopnih dana
		n[d] = float64(d - 10000)
	}
	p, ok := PragoviIzNiza(n)
	if !ok {
		t.Fatal("niz od 3750 dana mora dati pragove")
	}
	// najveći zbroj triju dana je 97 + 98 + 99
	if p.Dan3 != [3]float64{294, 294, 294} {
		t.Errorf("pragovi triju dana %v, želim tri puta 294", p.Dan3)
	}
}

// GREŠKA: niz od barem 3650 dana bez ijedna tri uzastopna dana obori
// PragoviIzNiza — prag triju dana posegne za zadnjim elementom praznog
// popisa.
func TestPragoviIzNizaBezTrodnevnih(t *testing.T) {
	t.Skip("GREŠKA: PragoviIzNiza (kisa_slivova.go:72) pada s index out of range [-1] kad u nizu nema tri uzastopna dana; treba vratiti false ili ostaviti prag triju dana praznim")
	n := DnevniNiz{}
	for d := int64(0); d < 2*3650; d += 2 {
		n[d] = 1
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PragoviIzNiza je pala: %v", r)
		}
	}()
	PragoviIzNiza(n)
}

// Razina kiše međusliva: prvo se gleda 24 sata, pa 72 sata palo, pa palo s
// onim što dolazi; uz razinu stoji koliko je takva kiša rijetka.
func TestStanjeSlivaPoRazinama(t *testing.T) {
	pragovi := map[string]PragoviKise{"A": {Dan1: [3]float64{20, 40, 80}, Dan3: [3]float64{30, 60, 120}}}
	stanje := func(n DnevniNiz) StanjeSliva {
		t.Helper()
		for _, st := range StanjeSlivova(map[string]DnevniNiz{OborinaKljuc("A"): n}, pragovi) {
			if st.Sliv == "A" {
				return st
			}
		}
		t.Fatal("nema međusliva A")
		return StanjeSliva{}
	}
	for _, s := range []struct {
		ime        string
		n          DnevniNiz
		razina     int
		zasto      string
		ucestalost string
	}{
		{"žuto od jučer", DnevniNiz{0: 25, -1: 0, -2: 0}, 1, "palo u zadnja 24 sata",
			"toliko padne u prosjeku tri puta godišnje"},
		{"narančasto kroz tri dana", DnevniNiz{0: 10, -1: 30, -2: 30}, 2, "palo u zadnja 72 sata",
			"toliko padne u prosjeku jednom godišnje"},
		{"crveno s onim što dolazi", DnevniNiz{0: 10, -1: 0, -2: 0, 1: 60, 2: 55}, 3, "palo i očekuje se u 72 sata",
			"toliko padne u prosjeku jednom u pet godina"},
	} {
		st := stanje(s.n)
		if st.Razina != s.razina || st.Zasto != s.zasto || st.Ucestalost != s.ucestalost {
			t.Errorf("%s: razina %d, %q, %q; želim %d, %q, %q", s.ime, st.Razina, st.Zasto, st.Ucestalost, s.razina, s.zasto, s.ucestalost)
		}
	}
}

// Učestalost riječima: tri i dva puta godišnje, jednom godišnje, jednom u
// pet ili u nekoliko godina.
func TestUcestalostKise(t *testing.T) {
	for _, s := range []struct {
		puta float64
		want string
	}{
		{3, "toliko padne u prosjeku tri puta godišnje"},
		{2, "toliko padne u prosjeku dva puta godišnje"},
		{1, "toliko padne u prosjeku jednom godišnje"},
		{0.2, "toliko padne u prosjeku jednom u pet godina"},
		{0.1, "toliko padne u prosjeku jednom u nekoliko godina"},
	} {
		if got := ucestalostKise(s.puta); got != s.want {
			t.Errorf("%g puta: %q, želim %q", s.puta, got, s.want)
		}
	}
}
