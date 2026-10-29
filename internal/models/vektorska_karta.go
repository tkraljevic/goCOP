package models

import (
	"errors"
	"regexp"
	"strings"
)

// VektorskaKarta je karta bez interneta (PMTiles) koju čvor poslužuje s
// diska. Opis putuje u postavkama karata, pa svi čvorovi znaju koja je
// karta zadana; datoteka se na svaki čvor učitava paketom karte (.cop).
type VektorskaKarta struct {
	Naziv    string     `json:"naziv"`
	Izvor    string     `json:"izvor,omitempty"` // npr. „OpenStreetMap (Protomaps, 27. 10. 2026.)”
	Otisak   string     `json:"otisak"`          // SHA-256 datoteke, hex
	Bajtova  int64      `json:"bajtova"`
	Obuhvat  [4]float64 `json:"obuhvat"` // zapad, jug, istok, sjever (WGS84)
	NajviseZ int        `json:"najvise_z"`
}

var otisakKarte = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Provjereno vraća opis karte ili grešku riječima za korisnika
func (k VektorskaKarta) Provjereno() (VektorskaKarta, error) {
	k.Naziv, k.Izvor = strings.TrimSpace(k.Naziv), strings.TrimSpace(k.Izvor)
	for _, c := range k.provjere() {
		if c.ne {
			return k, errors.New(c.poruka)
		}
	}
	return k, nil
}

// provjere su uvjeti ispravnog opisa karte redom kojim se javljaju
func (k VektorskaKarta) provjere() []struct {
	ne     bool
	poruka string
} {
	o := k.Obuhvat
	return []struct {
		ne     bool
		poruka string
	}{
		{k.Naziv == "", "upišite naziv karte"},
		{len([]rune(k.Naziv)) > 40 || len([]rune(k.Izvor)) > 120, "naziv karte je do 40, a izvor do 120 znakova"},
		{strings.ContainsAny(k.Naziv+k.Izvor, "<>"), "naziv i izvor karte su običan tekst, bez znakova < i >"},
		{!otisakKarte.MatchString(k.Otisak) || k.Bajtova <= 0, "karta nema ispravan otisak"},
		{o[0] < -180 || o[2] > 180 || o[1] < -90 || o[3] > 90 || o[0] >= o[2] || o[1] >= o[3], "područje karte nije ispravno"},
		{k.NajviseZ < 1 || k.NajviseZ > 22, "najveće povećanje karte nije ispravno"},
	}
}

// ImeIkoneIzNaziva je ime vlastite ikone (v-…) za naziv; služi i za ime
// datoteke paketa karte
func ImeIkoneIzNaziva(naziv string) string { return imeIkone(naziv) }
