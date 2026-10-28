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
		if _, err := (PostavkeKarte{Vektorska: &x}).Provjeri(); err == nil {
			t.Errorf("%q: postavke s lošom kartom prošle", zeli)
		}
	}

	ok := probniOpisKarte()
	p, err := PostavkeKarte{Vektorska: &ok}.Provjeri()
	if err != nil || p.Vektorska == nil || p.Vektorska.Naziv != "Sektor B" {
		t.Fatalf("postavke s kartom: %+v %v", p.Vektorska, err)
	}
	b, _ := json.Marshal(p) // samo tekst i brojevi
	if c := CitajPostavkeKarte(string(b)); c.Vektorska == nil || *c.Vektorska != *p.Vektorska {
		t.Errorf("pročitana karta: %+v", c.Vektorska)
	}
	if c := CitajPostavkeKarte(`{"podloge":[],"vektorska":{"naziv":"x","otisak":"kratak"}}`); c.Vektorska != nil {
		t.Errorf("loša karta iz razmjene ostala: %+v", c.Vektorska)
	}
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
