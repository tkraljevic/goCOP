package main

import "testing"

// Redci kruga pisali su „1 letvi, 21 novih vrijednosti”, „za 2 postaja,
// izdano 2 paketa” i „Namješteno 1 pojasa”: broj je stajao uz isti oblik
// riječi. Sad se riječ i glagol slažu s brojem.
func TestRedciKrugaSlazuRijecSBrojem(t *testing.T) {
	for _, s := range []struct {
		n                                   int
		strana, oborine, mjerenja, ulaganje string
		izdanje, priprema                   string
	}{
		{1, "1 letva, 1 nova vrijednost", "oborine: 1 sat za kišomjere", "stvarni kišomjeri: 1 mjerenje",
			"stvarni kišomjeri: uloženo u arhivu za 1 postaju, izdan 1 paket", "1 letva, 1 vrijednost",
			"Namješten 1 pojas, zapisan 1 promašaj; prognoza je izdana."},
		{2, "2 letve, 2 nove vrijednosti", "oborine: 2 sata za kišomjere", "stvarni kišomjeri: 2 mjerenja",
			"stvarni kišomjeri: uloženo u arhivu za 2 postaje, izdana 2 paketa", "2 letve, 2 vrijednosti",
			"Namještena 2 pojasa, zapisana 2 promašaja; prognoza je izdana."},
		{5, "5 letvi, 5 novih vrijednosti", "oborine: 5 sati za kišomjere", "stvarni kišomjeri: 5 mjerenja",
			"stvarni kišomjeri: uloženo u arhivu za 5 postaja, izdano 5 paketa", "5 letvi, 5 vrijednosti",
			"Namješteno 5 pojasa, zapisano 5 promašaja; prognoza je izdana."},
		{11, "11 letvi, 11 novih vrijednosti", "oborine: 11 sati za kišomjere", "stvarni kišomjeri: 11 mjerenja",
			"stvarni kišomjeri: uloženo u arhivu za 11 postaja, izdano 11 paketa", "11 letvi, 11 vrijednosti",
			"Namješteno 11 pojasa, zapisano 11 promašaja; prognoza je izdana."},
		{21, "21 letva, 21 nova vrijednost", "oborine: 21 sat za kišomjere", "stvarni kišomjeri: 21 mjerenje",
			"stvarni kišomjeri: uloženo u arhivu za 21 postaju, izdan 21 paket", "21 letva, 21 vrijednost",
			"Namješten 21 pojas, zapisan 21 promašaj; prognoza je izdana."},
		{22, "22 letve, 22 nove vrijednosti", "oborine: 22 sata za kišomjere", "stvarni kišomjeri: 22 mjerenja",
			"stvarni kišomjeri: uloženo u arhivu za 22 postaje, izdana 22 paketa", "22 letve, 22 vrijednosti",
			"Namještena 22 pojasa, zapisana 22 promašaja; prognoza je izdana."},
	} {
		for _, p := range []struct{ ime, got, zeli string }{
			{"tuđa prognoza", opisStranePrognoze(s.n, s.n), s.strana},
			{"oborine", opisOborina(s.n), s.oborine},
			{"mjerenja kišomjera", opisMjerenjaKisomjera(s.n), s.mjerenja},
			{"ulaganje kišomjera", opisUlaganjaKisomjera(s.n, s.n), s.ulaganje},
			{"izdanje", letveIVrijednosti(s.n, s.n), s.izdanje},
			{"priprema modela", opisPripremeModela(s.n, s.n, "prognoza je izdana"), s.priprema},
		} {
			if p.got != p.zeli {
				t.Errorf("%s, %d: %q, želim %q", p.ime, s.n, p.got, p.zeli)
			}
		}
	}
	// različiti brojevi u istom retku: svaki bira svoj oblik
	if got := opisStranePrognoze(21, 0); got != "21 letva, 0 novih vrijednosti" {
		t.Errorf("21 i 0: %q", got)
	}
	if got := opisUlaganjaKisomjera(0, 1); got != "stvarni kišomjeri: uloženo u arhivu za 0 postaja, izdan 1 paket" {
		t.Errorf("0 i 1: %q", got)
	}
}
