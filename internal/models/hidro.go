package models

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Hidrološka arhiva: povijesni nizovi, oblik korita i krivulje protoka.
//
// Arhiva stoji izvan operativne baze i ne sinkronizira se — povijesni niz se
// ne uređuje i uvijek se može ponovno napraviti iz datoteka. Program iz nje
// računa, ali u nju ne piše.

// HidroNiz je jedan niz u arhivi: jedna letva, jedan izvor, jedna veličina i
// jedna gustoća zapisa.
type HidroNiz struct {
	ID       int64
	Sliv     string
	Letva    string
	Izvor    string // his2000 | cop | letva-hv | letva-dhmz | vituki | preracun-*
	Velicina string // vodostaj | protok | temperatura | koncentracija | pronos
	Vrsta    string // satni | dvokratni | jutarnji | srednjak | dnevni
	PoDanu   bool   // izvor daje samo datum, bez sata
	Od, Do   string
	Zapisa   int
	Otisak   string
}

// Jedinica je mjerna jedinica veličine.
func (n HidroNiz) Jedinica() string { return JedinicaVelicine(n.Velicina) }

// JedinicaVelicine vraća jedinicu u kojoj se veličina mjeri.
func JedinicaVelicine(v string) string {
	switch v {
	case "vodostaj":
		return "cm"
	case "protok":
		return "m³/s"
	case "temperatura":
		return "°C"
	case "koncentracija":
		return "g/m³"
	case "pronos":
		return "t"
	}
	return ""
}

// NazivVelicine je veličina za ispis.
func NazivVelicine(v string) string {
	switch v {
	case "vodostaj":
		return "Vodostaj"
	case "protok":
		return "Protok"
	case "temperatura":
		return "Temperatura vode"
	case "koncentracija":
		return "Koncentracija nanosa"
	case "pronos":
		return "Pronos nanosa"
	}
	return v
}

// NazivIzvora objašnjava odakle niz dolazi i koliko mu se vjeruje.
func NazivIzvora(i string) string {
	switch {
	case i == "his2000":
		return "DHMZ, ovjereno"
	case i == "cop":
		return "COP Osijek, odabrana jutarnja vrijednost"
	case i == "letva-hv":
		return "telemetrija, Hrvatske vode"
	case i == "letva-dhmz":
		return "telemetrija, DHMZ"
	case i == "vituki":
		return "vizugy.hu, Mađarska"
	case len(i) > 9 && i[:9] == "preracun-":
		return "preračunato iz " + i[9:]
	}
	return i
}

// JeIzmjereno govori je li niz mjeren na toj letvi ili izveden računom.
func (n HidroNiz) JeIzmjereno() bool { return len(n.Izvor) < 9 || n.Izvor[:9] != "preracun-" }

// NazivVrste objašnjava koja je to vrijednost dana. Razlika jutarnjeg
// očitanja i dnevnog srednjaka nije sitnica: na naglom porastu razilaze se i
// po više od metra.
func NazivVrste(v string) string {
	switch v {
	case "satni":
		return "svaki sat"
	case "dvokratni":
		return "dva puta dnevno"
	case "jutarnji":
		return "jutarnje očitanje"
	case "srednjak":
		return "dnevni srednjak"
	case "dnevni":
		return "jednom dnevno"
	}
	return v
}

// HidroTocka je jedna vrijednost niza u trenutku.
type HidroTocka struct {
	Kad        time.Time
	Vrijednost float64
}

// HidroGodina su karakteristične vrijednosti jedne godine, izračunate iz niza.
type HidroGodina struct {
	Godina       int
	Zapisa       int
	Min, Max     float64
	MinNa, MaxNa string
	Srednjak     float64
	Nepotpuna    bool // godina nije pokrivena cijela
}

// HidroPregled je ono što se o nizu može reći bez ijedne odluke: raspon,
// ekstremi i godine.
type HidroPregled struct {
	Niz          HidroNiz
	Godine       []HidroGodina
	Min, Max     float64
	MinNa, MaxNa string
	Srednjak     float64
}

// ProfilKorita je snimak poprečnog profila u jednom danu.
type ProfilKorita struct {
	ID       int64
	Datum    string
	Vodostaj int     // vodostaj pri snimanju, cm
	KotaNule float64 // apsolutna kota nule letve, m
	Tocke    []TockaProfila
}

// TockaProfila je jedna izmjerena točka: udaljenost od početka i kota dna.
type TockaProfila struct {
	Stacionaza float64 // m
	Visina     float64 // apsolutna kota, m
}

// Sirina je razmak između prve i zadnje izmjerene točke.
func (p ProfilKorita) Sirina() float64 {
	if len(p.Tocke) < 2 {
		return 0
	}
	return p.Tocke[len(p.Tocke)-1].Stacionaza - p.Tocke[0].Stacionaza
}

// Dno je najniža izmjerena kota korita.
func (p ProfilKorita) Dno() float64 {
	if len(p.Tocke) == 0 {
		return 0
	}
	n := p.Tocke[0].Visina
	for _, t := range p.Tocke {
		if t.Visina < n {
			n = t.Visina
		}
	}
	return n
}

// DubinaPri je dubina nad najnižom točkom korita pri zadanom vodostaju.
func (p ProfilKorita) DubinaPri(vodostajCm int) float64 {
	return p.KotaNule + float64(vodostajCm)/100 - p.Dno()
}

// HQKrivulja pretvara vodostaj u protok: Q = a·(H + h0)^b, H u metrima iznad
// kote nule. Vrijedi za razdoblje, jer se korito mijenja pa se krivulja
// povremeno iznova postavlja.
type HQKrivulja struct {
	ID         int64
	Letva      string
	VrijediOd  string
	VrijediDo  string // prazno = do sljedeće izmjere
	A, B, H0   float64
	Mjerenja   int
	Odstupanje float64 // postotak
	Napomena   string
}

// Protok računa protok iz vodostaja u centimetrima. Vraća false kad je
// vodostaj ispod kote na kojoj krivulja prestaje vrijediti.
func (k HQKrivulja) Protok(vodostajCm int) (float64, bool) {
	h := float64(vodostajCm)/100 + k.H0
	if h <= 0 {
		return 0, false
	}
	return k.A * math.Pow(h, k.B), true
}

// Zapis je krivulja ispisana onako kako se i citira, s decimalnim zarezom.
func (k HQKrivulja) Zapis() string {
	zarez := func(f float64, d int) string {
		return strings.Replace(strconv.FormatFloat(f, 'f', d, 64), ".", ",", 1)
	}
	return "Q = " + zarez(k.A, 4) + " · (H + " + zarez(k.H0, 2) + ")^" + zarez(k.B, 4)
}
