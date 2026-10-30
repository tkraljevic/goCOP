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
// oznake se zanemaruju; bez ijedne ispravne oznake druge strane vrijedi
// razmjena/1, a popis s ispravnom oznakom je doslovan.
func TestDogovor(t *testing.T) {
	slucajevi := []struct {
		ime           string
		moje, njihove []string
		dogovor       map[string]int
	}{
		{"bez popisa (0.0.38)", NaseMogucnosti(), nil, map[string]int{"razmjena": 1}},
		{"prazan popis", NaseMogucnosti(), []string{}, map[string]int{"razmjena": 1}},
		// namjerno promijenjeno: G4 (plan WEB synca, §3); i granica-odbijenih.
		// namjerno promijenjeno: G4b; i rupe. namjerno promijenjeno: karta
		// razmjenom; i dijelovi-karte
		{"isti popis", NaseMogucnosti(), NaseMogucnosti(), map[string]int{"dijelovi-karte": 1, "granica-odbijenih": 1, "razmjena": 1, "rupe": 1}},
		{"druga strana bez granice odbijenih", NaseMogucnosti(), []string{"razmjena/1"}, map[string]int{"razmjena": 1}},
		{"nepoznate se zanemaruju", NaseMogucnosti(), []string{"razmjena/1", "web/1", "gocop-veza/1", "roj/3"}, map[string]int{"razmjena": 1}},
		{"neispravne se zanemaruju", []string{"razmjena/1", "web/01"}, []string{"razmjena/01", "razmjena/1", "web/01"}, map[string]int{"razmjena": 1}},
		{"najviši zajednički", []string{"razmjena/1", "razmjena/2", "razmjena/3"}, []string{"razmjena/4", "razmjena/2", "razmjena/1"}, map[string]int{"razmjena": 2}},
		// manji od dva najveća bio bi 2, a 2 jedna strana ne govori
		{"presjek, ne manji od najvećih", []string{"razmjena/1", "razmjena/3"}, []string{"razmjena/1", "razmjena/2"}, map[string]int{"razmjena": 1}},
		{"više imena", []string{"razmjena/1", "web/1", "web/2"}, []string{"web/2", "razmjena/1", "web/1"}, map[string]int{"razmjena": 1, "web": 2}},
		{"popis bez razmjene", NaseMogucnosti(), []string{"web/1"}, map[string]int{}},
		// neispravne se zanemaruju, pa je to kao prazan popis
		{"samo neispravne", NaseMogucnosti(), []string{"razmjena/01", "Razmjena/1"}, map[string]int{"razmjena": 1}},
		{"neispravna uz ispravnu", NaseMogucnosti(), []string{"Razmjena/1", "web/1"}, map[string]int{}},
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

// Naš popis: dijelovi-karte/1, granica-odbijenih/1, razmjena/1 i rupe/1, ispravne oznake,
// poredan i bez ponavljanja. Oznake nikad ne nose ime protokola (ALPN je ime
// protokola, '-' i oznaka), a imena koja pripadaju kasnijim fazama ili
// rezervirana ne objavljuju se.
//
// Namjerno promijenjeno: G4 (plan WEB synca, §3). Do G4 popis je bio
// samo razmjena/1, a dijeljenje popisa provjeravalo se na prvoj oznaci.
// Namjerno promijenjeno: G4b; uz rupe/1. Namjerno promijenjeno: karta
// razmjenom; uz dijelovi-karte/1.
func TestNaseMogucnosti(t *testing.T) {
	nase := NaseMogucnosti()
	ocekivano := []string{"dijelovi-karte/1", "granica-odbijenih/1", "razmjena/1", "rupe/1"}
	if !slices.Equal(nase, ocekivano) || OznakaRazmjena != "razmjena/1" || OznakaGranicaOdbijenih != "granica-odbijenih/1" || OznakaRupe != "rupe/1" ||
		OznakaDijeloviKarte != "dijelovi-karte/1" {
		t.Fatalf("naše mogućnosti: %v", nase)
	}
	if !slices.IsSorted(nase) || len(slices.Compact(slices.Clone(nase))) != len(nase) {
		t.Errorf("popis nije poredan ili se ponavlja: %v", nase)
	}
	// ograda pamćenja mora zadržati sve naše oznake (ocisceneOznake)
	if len(nase) > najviseOznaka {
		t.Errorf("%d naših oznaka, a pamti se najviše %d", len(nase), najviseOznaka)
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
	if !slices.Equal(NaseMogucnosti(), ocekivano) {
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
	if prazno := (Znanje{CekaNadogradnju: 7}).Ocisceno(); prazno.Mogucnosti != nil || prazno.Vrste != nil || prazno.CekaNadogradnju != 7 {
		t.Errorf("bez popisa: %+v", prazno)
	}
	neispravno := Znanje{Mogucnosti: []string{"Razmjena/1"}, Vrste: map[string]int{"Akti": 1, "nula": 0}}.Ocisceno()
	if neispravno.Mogucnosti != nil || neispravno.Vrste != nil {
		t.Errorf("bez ijedne ispravne: %+v", neispravno)
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
	// razmjena/1 je po abecedi iza ograde, a ostaje jer je ima i ovaj program
	mnogo.Mogucnosti = append(mnogo.Mogucnosti, OznakaRazmjena)
	o = mnogo.Ocisceno()
	if len(o.Mogucnosti) != najviseOznaka || !slices.IsSorted(o.Mogucnosti) || !slices.Contains(o.Mogucnosti, OznakaRazmjena) ||
		o.Mogucnosti[najviseOznaka-2] != "oznaka-30/1" {
		t.Errorf("ograda uz našu oznaku: %d %v", len(o.Mogucnosti), o.Mogucnosti)
	}
	if len(o.Vrste) != najviseVrsta || o.Vrste["vrsta_1023"] != 1 || o.Vrste["vrsta_1024"] != 0 {
		t.Errorf("ograda vrsta: %d", len(o.Vrste))
	}
}

// Spremljeno znanje daje isti Dogovor kao popis koji je stigao: i kad u
// popisu nema nijedne ispravne oznake, i kad je oznaka više nego što se
// pamti, a razmjena/1 je po abecedi iza ograde.
func TestDogovorIzSpremljenog(t *testing.T) {
	var ispredRazmjene, bezRazmjene []string
	for i := range najviseOznaka {
		ispredRazmjene = append(ispredRazmjene, fmt.Sprintf("a%02d/1", i))
	}
	for i := range najviseOznaka + 8 {
		bezRazmjene = append(bezRazmjene, fmt.Sprintf("web-%02d/1", i))
	}
	stigli := [][]string{nil, {}, NaseMogucnosti(), {"web/1"}, {"Razmjena/1"}, {"razmjena/01"},
		{"granica_odbijenih/1", "Web/1"}, append(slices.Clone(ispredRazmjene), "razmjena/1"), bezRazmjene}
	for _, stiglo := range stigli {
		uzivo := Dogovor(NaseMogucnosti(), stiglo)
		if spremljeno := Dogovor(NaseMogucnosti(), Znanje{Mogucnosti: stiglo}.Ocisceno().Mogucnosti); !maps.Equal(uzivo, spremljeno) {
			t.Errorf("%v: uživo %v, iz spremljenog %v", stiglo, uzivo, spremljeno)
		}
	}
}
