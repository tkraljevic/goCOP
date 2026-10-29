package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func probniOpisKarte() VektorskaKarta {
	return VektorskaKarta{Naziv: " Sektor B ", Izvor: "OpenStreetMap", Otisak: strings.Repeat("ab", 32), Bajtova: 120,
		Obuhvat: [4]float64{17, 45.05, 19.65, 46.3}, NajviseZ: 15}
}

// Opis karte bez interneta: ispravan prolazi očišćen, a svaka greška kaže
// što ne valja. Neispravan opis iz razmjene se izostavi, a ispravan putuje
// do preglednika tek kad ga ovaj čvor ima (adresa nije prazna).
func TestOpisVektorskeKarte(t *testing.T) {
	k, err := probniOpisKarte().Provjereno()
	if err != nil || k.Naziv != "Sektor B" {
		t.Fatalf("ispravan opis: %+v %v", k, err)
	}
	for zeli, izmjena := range map[string]func(*VektorskaKarta){
		"upišite naziv":     func(k *VektorskaKarta) { k.Naziv = " " },
		"do 40":             func(k *VektorskaKarta) { k.Naziv = strings.Repeat("a", 41) },
		"običan tekst":      func(k *VektorskaKarta) { k.Izvor = "<b>" },
		"ispravan otisak":   func(k *VektorskaKarta) { k.Otisak = "ab" },
		"područje":          func(k *VektorskaKarta) { k.Obuhvat = [4]float64{19, 45, 17, 46} },
		"najveće povećanje": func(k *VektorskaKarta) { k.NajviseZ = 0 },
	} {
		x := probniOpisKarte()
		izmjena(&x)
		if _, err := x.Provjereno(); err == nil || !strings.Contains(err.Error(), zeli) {
			t.Errorf("%q: %v", zeli, err)
		}
		b, _ := json.Marshal(x) // samo tekst i brojevi
		if CitajVektorskuKartu(string(b)) != nil {
			t.Errorf("%q: loša karta iz razmjene ostala", zeli)
		}
	}

	ok := probniOpisKarte()
	b, _ := json.Marshal(ok) // samo tekst i brojevi
	c := CitajVektorskuKartu(string(b))
	if c == nil || c.Naziv != "Sektor B" || c.Otisak != ok.Otisak || c.Obuhvat != ok.Obuhvat {
		t.Fatalf("pročitana karta: %+v", c)
	}
	for _, s := range []string{"", " ", "{", `{"naziv":"x","otisak":"kratak"}`} {
		if CitajVektorskuKartu(s) != nil {
			t.Errorf("zapis %q dao je kartu", s)
		}
	}
	p := PostavkeKarte{Vektorska: c}
	za := func(vektor string) string {
		b, _ := json.Marshal(p.ZaPreglednik("/i.svg", "", vektor)) // samo tekst i brojevi
		return string(b)
	}
	if s := za(""); !strings.Contains(s, `"vektorska":null`) {
		t.Errorf("karta koje ovdje nema: %s", s)
	}
	if s := za("/karta/vektor/x.pmtiles"); !strings.Contains(s, `"url":"/karta/vektor/x.pmtiles"`) || !strings.Contains(s, `"najvise_z":15`) {
		t.Errorf("karta ovog čvora: %s", s)
	}
	if ImeIkoneIzNaziva("Sektor B") != "v-sektor-b" {
		t.Error("ime iz naziva")
	}
}

// Postavke karata više ne nose kartu bez interneta: Provjeri i spremanje
// je ne pišu, CitajPostavkeKarte je ne čita ni iz starog zapisa, a
// PostavkeKartePrograma je uzima iz zasebne postavke
func TestPostavkeKarteBezKarte(t *testing.T) {
	prijeP, prijeK := PostavkeKartePrograma(), VektorskaKartaPrograma()
	t.Cleanup(func() { SetPostavkeKarte(prijeP); SetVektorskaKarta(prijeK) })

	ok, err := probniOpisKarte().Provjereno()
	if err != nil {
		t.Fatal(err)
	}
	p, err := PostavkeKarte{Podloge: []PodlogaKarte{}, Vektorska: &ok}.Provjeri()
	if err != nil || p.Vektorska != nil {
		t.Fatalf("provjerene postavke: %+v %v", p.Vektorska, err)
	}
	b, _ := json.Marshal(PostavkeKarte{Podloge: []PodlogaKarte{}, Vektorska: &ok}) // samo tekst i brojevi
	if strings.Contains(string(b), "vektorska") {
		t.Errorf("postavke karata nose kartu: %s", b)
	}
	stari, _ := json.Marshal(ok) // samo tekst i brojevi
	if c := CitajPostavkeKarte(`{"podloge":[],"vektorska":` + string(stari) + `}`); c.Vektorska != nil {
		t.Errorf("karta pročitana iz postavki karata: %+v", c.Vektorska)
	}

	SetVektorskaKarta(nil)
	SetPostavkeKarte(PostavkeKarte{Podloge: []PodlogaKarte{}, Vektorska: &ok})
	if PostavkeKartePrograma().Vektorska != nil || VektorskaKartaPrograma() != nil {
		t.Error("karta stigla kroz postavke karata")
	}
	ok.Dio, ok.Dijelovi = 100, []string{strings.Repeat("cd", 32), strings.Repeat("ef", 32)}
	SetVektorskaKarta(&ok)
	ok.Dijelovi[0] = "izmijenjeno izvana"
	k := PostavkeKartePrograma().Vektorska
	if k == nil || k.Naziv != "Sektor B" || k.Dijelovi[0] != strings.Repeat("cd", 32) {
		t.Fatalf("karta programa: %+v", k)
	}
	k.Dijelovi[0] = "izmijenjena kopija"
	if VektorskaKartaPrograma().Dijelovi[0] != strings.Repeat("cd", 32) {
		t.Error("kopija karte dijeli popis dijelova s programom")
	}
	SetPostavkeKarte(ZadanePostavkeKarte())
	if PostavkeKartePrograma().Vektorska == nil {
		t.Error("zadane postavke karata maknule su kartu")
	}
}

// Dijelovi karte u opisu: bez dijelova je opis ispravan, a s dijelovima
// broj, veličina i otisci moraju odgovarati karti
func TestVektorskaKartaDijelovi(t *testing.T) {
	d := func(n int) []string {
		var out []string
		for i := 0; i < n; i++ {
			out = append(out, strings.Repeat("0a", 32))
		}
		return out
	}
	k := probniOpisKarte() // 120 bajtova
	if _, err := k.Provjereno(); err != nil {
		t.Fatalf("bez dijelova: %v", err)
	}
	for opis, c := range map[string]struct {
		dio      int64
		dijelovi []string
		valja    bool
	}{
		"tri dijela, zadnji kraći": {50, d(3), true},
		"točno dva dijela":         {60, d(2), true},
		"jedan dio veći od karte":  {NajveciDioKarte, d(1), true},
		"dijelova premalo":         {50, d(2), false},
		"dijelova previše":         {60, d(3), false},
		"dio bez popisa":           {50, nil, false},
		"popis bez veličine":       {0, d(1), false},
		"dio negativan":            {-1, d(1), false},
		"dio prevelik":             {NajveciDioKarte + 1, d(1), false},
		"kriv otisak dijela":       {60, []string{strings.Repeat("0a", 32), "nije otisak"}, false},
		"velika slova":             {120, []string{strings.Repeat("0A", 32)}, false},
	} {
		x := probniOpisKarte()
		x.Dio, x.Dijelovi = c.dio, c.dijelovi
		_, err := x.Provjereno()
		if (err == nil) != c.valja || (err != nil && !strings.Contains(err.Error(), "popis dijelova")) {
			t.Errorf("%s: %v", opis, err)
		}
	}
	// broj dijelova je ograđen i kad bi veličina i otisci odgovarali karti
	for n, valja := range map[int]bool{NajviseDijelovaKarte: true, NajviseDijelovaKarte + 1: false} {
		x := probniOpisKarte()
		x.Bajtova, x.Dio, x.Dijelovi = int64(n), 1, d(n)
		if _, err := x.Provjereno(); (err == nil) != valja {
			t.Errorf("%d dijelova: %v", n, err)
		}
	}

	x := probniOpisKarte()
	x.Dio, x.Dijelovi = 50, d(3)
	b, _ := json.Marshal(x) // samo tekst i brojevi
	if c := CitajVektorskuKartu(string(b)); c == nil || c.Dio != 50 || len(c.Dijelovi) != 3 {
		t.Errorf("pročitani dijelovi: %+v", c)
	}
	// popis dijelova koji ovaj program ne prihvaća (npr. noviji program s
	// većim dijelovima) ne miče kartu, nego se izostavi
	for opis, izmjena := range map[string]func(*VektorskaKarta){
		"dio veći od dopuštenog": func(k *VektorskaKarta) { k.Dio, k.Dijelovi = 2*NajveciDioKarte, d(1) },
		"dijelova premalo":       func(k *VektorskaKarta) { k.Dio, k.Dijelovi = 50, d(2) },
		"previše dijelova":       func(k *VektorskaKarta) { k.Bajtova, k.Dio, k.Dijelovi = 5000, 1, d(5000) },
	} {
		x := probniOpisKarte()
		izmjena(&x)
		b, _ := json.Marshal(x) // samo tekst i brojevi
		c := CitajVektorskuKartu(string(b))
		if c == nil || c.Otisak != x.Otisak || c.Bajtova != x.Bajtova || c.Dio != 0 || c.Dijelovi != nil {
			if c != nil {
				t.Errorf("%s: pročitana karta s %d dijelova od %d bajtova", opis, len(c.Dijelovi), c.Dio)
				continue
			}
			t.Errorf("%s: karta nije pročitana", opis)
		}
	}
	los := probniOpisKarte()
	los.Dio, los.Dijelovi, los.NajviseZ = 2*NajveciDioKarte, d(1), 0
	b, _ = json.Marshal(los) // samo tekst i brojevi
	if c := CitajVektorskuKartu(string(b)); c != nil {
		t.Errorf("opis koji ne valja ni bez dijelova dao je kartu: %+v", c)
	}
	b, _ = json.Marshal(probniOpisKarte()) // samo tekst i brojevi
	if strings.Contains(string(b), "dio") {
		t.Errorf("opis bez dijelova nosi prazna polja: %s", b)
	}
}
