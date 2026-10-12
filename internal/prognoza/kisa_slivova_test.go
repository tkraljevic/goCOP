package prognoza

import (
	"reflect"
	"testing"
	"time"
)

// Pragovi su koliko padne u prosjeku tri puta, jednom godišnje i jednom u
// pet godina; razina se bira po najvišem pragu koji je prijeđen.
func TestKisaSlivova(t *testing.T) {
	n := DnevniNiz{}
	// 20 godina: svaki dan 1 mm, a 3., 1. i 0,2 puta godišnje sve jače kiše
	for d := int64(0); d < 20*365; d++ {
		n[d] = 1
	}
	for g := int64(0); g < 20; g++ {
		n[g*365+100] = 30 // 20 dana
		n[g*365+200] = 30
		n[g*365+300] = 30
		n[g*365+50] = 60 // jednom godišnje
	}
	for g := int64(0); g < 4; g++ {
		n[g*5*365+150] = 120 // jednom u pet godina
	}
	p, ok := PragoviIzNiza(n)
	if !ok {
		t.Fatal("niz od 20 godina mora dati pragove")
	}
	if p.Dan1[0] < 25 || p.Dan1[1] < 55 || p.Dan1[2] < 100 {
		t.Errorf("pragovi jednog dana %v", p.Dan1)
	}
	if _, ok := PragoviIzNiza(DnevniNiz{1: 1}); ok {
		t.Error("kratki niz ne smije dati pragove")
	}

	pragovi := map[string]PragoviKise{"A": p}
	slucaj := func(n DnevniNiz) StanjeSliva {
		for _, st := range StanjeSlivova(map[string]DnevniNiz{OborinaKljuc("A"): n}, pragovi) {
			if st.Sliv == "A" {
				return st
			}
		}
		t.Fatal("nema međusliva A")
		return StanjeSliva{}
	}
	if st := slucaj(DnevniNiz{0: 2, -1: 1, -2: 0, 1: 3, 2: 1}); st.Razina != 0 || st.Palo72 != 3 || st.Dolazi48 != 4 {
		t.Errorf("obična kiša: %+v", st)
	}
	if st := slucaj(DnevniNiz{0: 70, -1: 1, -2: 0, 1: 0, 2: 0}); st.Razina != 2 || st.Zasto != "palo u zadnja 24 sata" {
		t.Errorf("jaka pala kiša: razina %d, %q", st.Razina, st.Zasto)
	}
	// ništa još nije palo, ali dolazi: upozorenje unaprijed
	if st := slucaj(DnevniNiz{0: 5, -1: 0, -2: 0, 1: 60, 2: 70}); st.Razina == 0 || st.Zasto != "palo i očekuje se u 72 sata" {
		t.Errorf("kiša koja dolazi: razina %d, %q", st.Razina, st.Zasto)
	}
	if st := slucaj(DnevniNiz{}); st.Ima || st.Razina != 0 {
		t.Errorf("bez podataka nema upozorenja: %+v", st)
	}
	if len(slucaj(DnevniNiz{0: 1}).Letve) == 0 {
		t.Error("međusliv A mora imati letve u prognozi")
	}
}

// ocistiPragove počinje test s praznom zalihom pragova i vraća zatečenu kad
// test završi.
func ocistiPragove(t *testing.T) {
	t.Helper()
	z := &pragoviPredmemorija
	z.Lock()
	dan, pragovi := z.dan, z.pragovi
	z.dan, z.pragovi = "", nil
	z.Unlock()
	t.Cleanup(func() {
		z.Lock()
		z.dan, z.pragovi = dan, pragovi
		z.Unlock()
	})
}

// Pragovi se računaju iz dnevne kiše točaka međusliva u arhivi, samo za
// međuslivove s dovoljno dugim nizom i samo jednom na dan: drugi poziv istog
// dana vraća već izračunate i ne dira arhivu. Arhiva koja se ne da pročitati
// je greška.
func TestPragoviSlivovaJednomNaDan(t *testing.T) {
	ocistiPragove(t)
	arhiva, _ := arhivaUDatoteci(t)
	upisiDnevne(t, arhiva, "tocka-a", "oborina", 11*365, kisaIzmisljena)
	upisiDnevne(t, arhiva, "tocka-b", "oborina", 100, kisaIzmisljena) // prekratko
	tocke := []OborinskaTocka{{Code: "tocka-a", Sliv: "A", Tezina: 1}, {Code: "tocka-b", Sliv: "B", Tezina: 1}}

	pragovi, err := PragoviSlivova(arhiva, tocke)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := DnevneOborine(arhiva, tocke)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := PragoviIzNiza(ob[OborinaKljuc("A")])
	if len(pragovi) != 1 || !reflect.DeepEqual(pragovi["A"], want) {
		t.Errorf("pragovi %+v, želim samo A: %+v", pragovi, want)
	}

	danas := time.Now().Format("2006-01-02")
	arhiva.Close()
	ponovo, err := PragoviSlivova(arhiva, tocke)
	if time.Now().Format("2006-01-02") != danas {
		t.Skip("ponoć je prošla usred testa")
	}
	if err != nil || !reflect.DeepEqual(ponovo, pragovi) {
		t.Errorf("drugi poziv istog dana: %+v, %v — želim zapamćene pragove", ponovo, err)
	}

	ocistiPragove(t)
	if p, err := PragoviSlivova(arhiva, tocke); err == nil {
		t.Errorf("zatvorena arhiva bez greške: %+v", p)
	}
}
