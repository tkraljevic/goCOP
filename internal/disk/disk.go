// Package disk prati koliko je mjesta ostalo na diskovima na kojima čvor
// drži bazu, arhivu vodostaja, izvorne datoteke arhive i pakete, i upozori
// kad ga ostane malo (Odluke (14. 10. 2026.)). Razmjenu zasad ne zaustavlja.
package disk

import (
	"strconv"
	"strings"
	"time"
)

const (
	// MaloBajtova: ispod 10 GB slobodnog upozorava se na svakom disku, i na
	// velikom (Odluke (14. 10. 2026.)). GB je decimalni, 10^9 bajtova, kao što
	// veličinu diska pišu proizvođači i macOS.
	MaloBajtova = 10_000_000_000
	// MaloPostotaka: upozorava se i kad je slobodno manje od 10 % diska, makar
	// to bilo više od 10 GB (Odluke (14. 10. 2026.))
	MaloPostotaka = 10
	// Svakih je razmak između dvije provjere
	Svakih = 10 * time.Minute
)

// Mjesto je mapa u kojoj čvor nešto drži, s riječima za upozorenje
type Mjesto struct {
	Put string // mapa; prazno = ovaj čvor to ne drži
	// Naziv u instrumentalu, kako stoji iza „Na disku s”: bazom, arhivom
	Naziv string
	// Premjesti u akuzativu, kako stoji iza „premjestite”: arhivu; prazno =
	// ne nudi se premještanje
	Premjesti string
}

// Mjera je jedno mjerenje diska
type Mjera struct {
	Disk     string // isti disk, ista oznaka: uređaj na Unixu, svezak na Windowsu
	Ukupno   uint64 // bajtova
	Slobodno uint64 // bajtova koje program još smije zauzeti
}

// Mjerac mjeri disk na kojem leži mapa; u testovima se podmetne
type Mjerac interface {
	Izmjeri(put string) (Mjera, error)
}

// Sustav mjeri pravi disk: statfs na Unixu, GetDiskFreeSpaceExW na Windowsu
type Sustav struct{}

// Disk je jedan disk sa svime što čvor na njemu drži
type Disk struct {
	Mjera
	Nazivi    []string // redom kojim su mjesta zadana
	Premjesti string   // prvo što se s njega može premjestiti
}

// Malo: slobodno je manje od 10 GB ili manje od 10 % diska
func (d Disk) Malo() bool {
	return d.Slobodno < MaloBajtova || d.Slobodno*100 < d.Ukupno*MaloPostotaka
}

// Upozorenje je redak za dnevnik: koliko je mjesta ostalo i što učiniti
func (d Disk) Upozorenje() string {
	return d.Poruka("") + " " + d.Savjet()
}

// Poruka kaže na kojem je disku i koliko je mjesta ostalo. Pločicu razmjene
// gledaju i s drugih računala, pa ona daje ime čvora; dnevniku ne treba.
func (d Disk) Poruka(cvor string) string {
	gdje := "Na disku s "
	if cvor != "" {
		gdje = "Na disku čvora " + cvor + " s "
	}
	return gdje + spoji(d.Nazivi) + " ostalo je " + d.slobodno() + "."
}

// Savjet kaže administratoru što može učiniti
func (d Disk) Savjet() string {
	if d.Premjesti == "" {
		return "Oslobodite mjesto na tom disku."
	}
	return "Oslobodite mjesto ili premjestite " + d.Premjesti + " na drugi disk."
}

// dovoljno je redak za dnevnik kad na disku opet ima dovoljno mjesta
func (d Disk) dovoljno() string {
	return "Na disku s " + spoji(d.Nazivi) + " opet ima dovoljno mjesta: slobodno je " + d.slobodno() + "."
}

func (d *Disk) dodaj(m Mjesto) {
	d.Nazivi = append(d.Nazivi, m.Naziv)
	if d.Premjesti == "" {
		d.Premjesti = m.Premjesti
	}
}

// slobodno: „8,2 GB (6 % diska)”. Postotak je odrezan, pa se uz rub ne
// prikaže 10 % kad je slobodno manje od 10 %.
func (d Disk) slobodno() string {
	p := d.Slobodno * 100 / d.Ukupno
	postotak := strconv.FormatUint(p, 10) + " %"
	if p == 0 {
		postotak = "manje od 1 %"
	}
	return Velicina(d.Slobodno) + " (" + postotak + " diska)"
}

// Velicina piše bajtove hrvatski: 420 MB, 8,2 GB, 25 GB, 1,2 TB, 12 TB.
// Ispod 10 jedinica s jednom decimalom, od 10 naviše bez nje. Decimala je
// odrezana, da se slobodnog mjesta ne prikaže više nego što ga ima.
func Velicina(b uint64) string {
	switch {
	case b < 1e9:
		return strconv.FormatUint(b/1e6, 10) + " MB"
	case b < 10e9:
		return decimalno(b, 1e9) + " GB"
	case b < 1e12:
		return strconv.FormatUint(b/1e9, 10) + " GB"
	case b < 10e12:
		return decimalno(b, 1e12) + " TB"
	}
	return strconv.FormatUint(b/1e12, 10) + " TB"
}

// decimalno: broj jedinica s jednom odrezanom decimalom i zarezom
func decimalno(b, jedinica uint64) string {
	desetine := b / (jedinica / 10)
	return strconv.FormatUint(desetine/10, 10) + "," + strconv.FormatUint(desetine%10, 10)
}

// spoji: „a”, „a i b”, „a, b i c”
func spoji(r []string) string {
	if len(r) < 2 {
		return strings.Join(r, "")
	}
	return strings.Join(r[:len(r)-1], ", ") + " i " + r[len(r)-1]
}
