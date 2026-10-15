package razmjena

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// Oznaka je ime/N: ime od malih slova, znamenki i '-', N cijeli broj od 1
// naviše u kanonskom obliku. Ime se odvaja od broja na zadnjoj '/'.
func TestProcitajOznaku(t *testing.T) {
	ispravne := []struct {
		oznaka string
		ime    string
		n      int
	}{
		{"razmjena/1", "razmjena", 1},
		{"razmjena/2", "razmjena", 2},
		{"web/1", "web", 1},
		{"granica-odbijenih/10", "granica-odbijenih", 10},
		// tuđa oznaka s „gocop” je samo nepoznata, ne neispravna
		{"gocop-veza/1", "gocop-veza", 1},
		{strings.Repeat("a", 32) + "/1", strings.Repeat("a", 32), 1},
	}
	for _, c := range ispravne {
		ime, n, ok := procitajOznaku(c.oznaka)
		if !ok || ime != c.ime || n != c.n {
			t.Errorf("%q: %q %d %v", c.oznaka, ime, n, ok)
		}
	}
	neispravne := []string{"", "razmjena", "razmjena/", "/1", "razmjena/0", "razmjena/-1",
		"razmjena/01", "razmjena/+1", "razmjena/1.0", "razmjena/1 ", " razmjena/1", "razmjena/x",
		"Razmjena/1", "raz mjena/1", "razmjena_1/1", "a/b/1", "čvor/1", strings.Repeat("a", 33) + "/1",
		"razmjena/99999999999999999999"}
	for _, oznaka := range neispravne {
		if ime, n, ok := procitajOznaku(oznaka); ok || ime != "" || n != 0 {
			t.Errorf("%q je pročitana: %q %d", oznaka, ime, n)
		}
	}
}

// Dogovor: za isto ime najviši ZAJEDNIČKI N; nepoznate i neispravne
// oznake se zanemaruju; bez popisa druge strane vrijedi razmjena/1, a
// popis koji postoji je doslovan.
func TestDogovor(t *testing.T) {
	slucajevi := []struct {
		ime           string
		moje, njihove []string
		dogovor       map[string]int
	}{
		{"bez popisa (0.0.38)", NaseMogucnosti(), nil, map[string]int{"razmjena": 1}},
		{"prazan popis", NaseMogucnosti(), []string{}, map[string]int{"razmjena": 1}},
		{"isti popis", NaseMogucnosti(), NaseMogucnosti(), map[string]int{"razmjena": 1}},
		{"nepoznate se zanemaruju", NaseMogucnosti(), []string{"razmjena/1", "web/1", "gocop-veza/1", "roj/3"}, map[string]int{"razmjena": 1}},
		{"neispravne se zanemaruju", []string{"razmjena/1", "web/01"}, []string{"razmjena/01", "razmjena/1", "web/01"}, map[string]int{"razmjena": 1}},
		{"najviši zajednički", []string{"razmjena/1", "razmjena/2", "razmjena/3"}, []string{"razmjena/4", "razmjena/2", "razmjena/1"}, map[string]int{"razmjena": 2}},
		// manji od dva najveća bio bi 2, a 2 jedna strana ne govori
		{"presjek, ne manji od najvećih", []string{"razmjena/1", "razmjena/3"}, []string{"razmjena/1", "razmjena/2"}, map[string]int{"razmjena": 1}},
		{"više imena", []string{"razmjena/1", "web/1", "web/2"}, []string{"web/2", "razmjena/1", "web/1"}, map[string]int{"razmjena": 1, "web": 2}},
		{"popis bez razmjene", NaseMogucnosti(), []string{"web/1"}, map[string]int{}},
		{"samo neispravne", NaseMogucnosti(), []string{"razmjena/01"}, map[string]int{}},
		{"ništa zajedničko", []string{"razmjena/2"}, nil, map[string]int{}},
	}
	for _, c := range slucajevi {
		if got := Dogovor(c.moje, c.njihove); !maps.Equal(got, c.dogovor) {
			t.Errorf("%s: %v, očekivano %v", c.ime, got, c.dogovor)
		}
	}
	// obje strane imaju ime/N kad je Dogovor[ime] == N, ne >=
	if d := Dogovor([]string{"web/1", "web/2"}, []string{"web/1", "web/2"}); d["web"] == 1 {
		t.Errorf("web/1 ne smije vrijediti kad je dogovoreno %v", d)
	}
}

// Naš popis: samo razmjena/1, ispravne oznake, poredan i bez ponavljanja.
// Oznake nikad ne nose ime protokola (ALPN je ime protokola, '-' i
// oznaka), a imena koja pripadaju kasnijim fazama ili rezervirana ne
// objavljuju se.
func TestNaseMogucnosti(t *testing.T) {
	nase := NaseMogucnosti()
	if !slices.Equal(nase, []string{OznakaRazmjena}) || OznakaRazmjena != "razmjena/1" {
		t.Fatalf("naše mogućnosti: %v", nase)
	}
	if !slices.IsSorted(nase) || len(slices.Compact(slices.Clone(nase))) != len(nase) {
		t.Errorf("popis nije poredan ili se ponavlja: %v", nase)
	}
	for _, oznaka := range nase {
		if _, _, ok := procitajOznaku(oznaka); !ok {
			t.Errorf("neispravna oznaka %q", oznaka)
		}
		for _, zabranjeno := range []string{"gocop", "sadrzaj/", "web/", "sastajaliste/", "signal/", "posrednik/", "veza/"} {
			if strings.Contains(oznaka, zabranjeno) {
				t.Errorf("oznaka %q sadrži %q", oznaka, zabranjeno)
			}
		}
	}
	nase[0] = "promijenjeno/1"
	if NaseMogucnosti()[0] != OznakaRazmjena {
		t.Error("pozivatelj je promijenio popis za sve")
	}
}

// Ključevi i tipovi na žici su zauvijek: mogucnosti popis teksta, vrste
// mapa teksta na broj (ključevi poredani, pa su bajtovi uvijek isti),
// cekaNadogradnju broj. Prazno izostaje.
func TestZnanjeNaZici(t *testing.T) {
	if b, err := json.Marshal(Znanje{}); err != nil || string(b) != `{}` {
		t.Errorf("prazno znanje: %s %v", b, err)
	}
	z := Znanje{Mogucnosti: []string{"razmjena/1"}, Vrste: map[string]int{"sektorska_izvjesca": 2, "akti": 1}, CekaNadogradnju: 120}
	const ocekivano = `{"mogucnosti":["razmjena/1"],"vrste":{"akti":1,"sektorska_izvjesca":2},"cekaNadogradnju":120}`
	for range 2 {
		if b, err := json.Marshal(z); err != nil || string(b) != ocekivano {
			t.Errorf("znanje na žici: %s %v", b, err)
		}
	}
	for _, popis := range []string{`{}`, `{"mogucnosti":null}`, `{"mogucnosti":[]}`} {
		var stiglo Znanje
		if err := json.Unmarshal([]byte(popis), &stiglo); err != nil {
			t.Fatal(err)
		}
		if d := Dogovor(NaseMogucnosti(), stiglo.Mogucnosti); !maps.Equal(d, map[string]int{"razmjena": 1}) {
			t.Errorf("%s: dogovor %v", popis, d)
		}
	}
	var krivTip Znanje
	if err := json.Unmarshal([]byte(`{"cekaNadogradnju":{"zapisa":1}}`), &krivTip); err == nil {
		t.Error("drugi tip polja mora biti greška dekodiranja, zato se tipovi ne mijenjaju")
	}
}

// Za spremanje ostaju ispravne oznake, i nepoznate, bez ponavljanja,
// poredane i ograničene; vrste s ispravnim imenom i shemom od 1 naviše;
// broj na čekanju nije manji od nule.
func TestZnanjeOcisceno(t *testing.T) {
	z := Znanje{
		Mogucnosti:      []string{"web/1", "razmjena/1", "razmjena/01", "razmjena/1", "gocop-veza/1", "Razmjena/2"},
		Vrste:           map[string]int{"akti": 1, "nema-ga": 3, "Akti": 1, "": 1, "a b": 1, "dnevna_izvjesca": 2, "nula": 0, "minus": -1, strings.Repeat("v", 65): 1},
		CekaNadogradnju: -5,
	}
	o := z.Ocisceno()
	if !slices.Equal(o.Mogucnosti, []string{"gocop-veza/1", "razmjena/1", "web/1"}) {
		t.Errorf("oznake: %v", o.Mogucnosti)
	}
	if !maps.Equal(o.Vrste, map[string]int{"akti": 1, "nema-ga": 3, "dnevna_izvjesca": 2}) {
		t.Errorf("vrste: %v", o.Vrste)
	}
	if o.CekaNadogradnju != 0 {
		t.Errorf("na čekanju: %d", o.CekaNadogradnju)
	}
	if prazno := (Znanje{CekaNadogradnju: 7}).Ocisceno(); prazno.Mogucnosti != nil || len(prazno.Vrste) != 0 || prazno.CekaNadogradnju != 7 {
		t.Errorf("bez popisa: %+v", prazno)
	}

	mnogo := Znanje{Vrste: map[string]int{}}
	for i := range 40 {
		mnogo.Mogucnosti = append(mnogo.Mogucnosti, fmt.Sprintf("oznaka-%02d/1", i))
	}
	for i := range 1100 {
		mnogo.Vrste[fmt.Sprintf("vrsta_%04d", i)] = 1
	}
	o = mnogo.Ocisceno()
	if len(o.Mogucnosti) != najviseOznaka || o.Mogucnosti[0] != "oznaka-00/1" || o.Mogucnosti[najviseOznaka-1] != "oznaka-31/1" {
		t.Errorf("ograda oznaka: %d %v", len(o.Mogucnosti), o.Mogucnosti)
	}
	if len(o.Vrste) != najviseVrsta || o.Vrste["vrsta_1023"] != 1 || o.Vrste["vrsta_1024"] != 0 {
		t.Errorf("ograda vrsta: %d", len(o.Vrste))
	}
}
