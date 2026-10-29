package models

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
)

// VektorskaKarta je karta bez interneta (PMTiles) koju čvor poslužuje s
// diska. Opis putuje kao zasebna opća postavka (karta_bez_interneta), pa svi
// čvorovi znaju koja je karta zadana; datoteka se na čvor učitava paketom
// karte (.cop).
type VektorskaKarta struct {
	Naziv    string     `json:"naziv"`
	Izvor    string     `json:"izvor,omitempty"` // npr. „OpenStreetMap (Protomaps, 27. 10. 2026.)”
	Otisak   string     `json:"otisak"`          // SHA-256 datoteke, hex
	Bajtova  int64      `json:"bajtova"`
	Obuhvat  [4]float64 `json:"obuhvat"` // zapad, jug, istok, sjever (WGS84)
	NajviseZ int        `json:"najvise_z"`
	// Dio je veličina dijela datoteke (bajtova), a Dijelovi SHA-256 svakog
	// dijela redom, hex; zadnji dio je kraći. Opis bez dijelova je ispravan,
	// ali takva karta ide samo paketom.
	Dio      int64    `json:"dio,omitempty"`
	Dijelovi []string `json:"dijelovi,omitempty"`
}

// NajveciDioKarte ograđuje veličinu dijela karte u opisu, a
// NajviseDijelovaKarte njihov broj: opis stiže razmjenom od bilo kojeg
// člana, a popis od 4096 otisaka (oko 270 kB) još stane u manifest paketa
// karte. Karta od 2 GB ima tada dijelove od najmanje 512 kB.
const (
	NajveciDioKarte      = 16 << 20
	NajviseDijelovaKarte = 4096
)

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
		{!k.dijeloviIspravni(), "popis dijelova karte nije ispravan"},
	}
}

// dijeloviIspravni: bez dijelova je opis ispravan; s dijelovima je veličina
// dijela do NajveciDioKarte, dijelova ima koliko ih stane u kartu, najviše
// NajviseDijelovaKarte, i svaki ima ispravan otisak
func (k VektorskaKarta) dijeloviIspravni() bool {
	if k.Dio == 0 && len(k.Dijelovi) == 0 {
		return true
	}
	if k.Dio < 1 || k.Dio > NajveciDioKarte || len(k.Dijelovi) > NajviseDijelovaKarte {
		return false
	}
	if int64(len(k.Dijelovi)) != (k.Bajtova+k.Dio-1)/k.Dio {
		return false
	}
	for _, d := range k.Dijelovi {
		if !otisakKarte.MatchString(d) {
			return false
		}
	}
	return true
}

// ImeIkoneIzNaziva je ime vlastite ikone (v-…) za naziv; služi i za ime
// datoteke paketa karte
func ImeIkoneIzNaziva(naziv string) string { return imeIkone(naziv) }

// CitajVektorskuKartu čita spremljeni opis karte bez interneta; prazan ili
// neispravan zapis znači da karte nema. Popis dijelova koji ovaj program ne
// prihvaća (npr. iz novijeg izdanja s drugim pravilima dijelova) se
// izostavi, a karta ostaje: s diska se poslužuje i bez dijelova, a na
// računalo koje je nema stiže paketom.
func CitajVektorskuKartu(s string) *VektorskaKarta {
	var k VektorskaKarta
	if strings.TrimSpace(s) == "" || json.Unmarshal([]byte(s), &k) != nil {
		return nil
	}
	if !k.dijeloviIspravni() {
		k.Dio, k.Dijelovi = 0, nil
	}
	k, err := k.Provjereno()
	if err != nil {
		return nil
	}
	return &k
}

var trenutnaVektorskaKarta atomic.Pointer[VektorskaKarta]

// SetVektorskaKarta postavlja zadanu kartu bez interneta (nil kad je nema);
// zove se pri startu, nakon učitavanja ili micanja i nakon primitka
// razmjenom
func SetVektorskaKarta(k *VektorskaKarta) {
	if k == nil {
		trenutnaVektorskaKarta.Store(nil)
		return
	}
	c := *k
	c.Dijelovi = slices.Clone(k.Dijelovi)
	trenutnaVektorskaKarta.Store(&c)
}

// VektorskaKartaPrograma vraća kopiju zadane karte bez interneta, nil kad
// je nema
func VektorskaKartaPrograma() *VektorskaKarta {
	k := trenutnaVektorskaKarta.Load()
	if k == nil {
		return nil
	}
	c := *k
	c.Dijelovi = slices.Clone(k.Dijelovi)
	return &c
}
