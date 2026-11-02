package web

import (
	"fmt"
	"html/template"
	"strings"
)

// Naslovna se slaže iz kartica po ulozi (Odluke (31. 10. 2026.)): ista
// građa kartice za sve uloge, a uloga bira koje se kartice slažu i kojim
// redom. Red je isti na računalu, tabletu i mobitelu.
//
// Pravila kartice (Odluke (1. 11. 2026.)):
//   - strogo najviše 5 redaka na računalu i tabletu, 3 na mobitelu, bez
//     iznimke; najvažnije je na vrhu;
//   - zadnji redak je „Više: još 7 →” i vodi na cijeli popis; kad je među
//     skrivenima hitno, piše „Više: još 7, od toga 2 iznad praga →”;
//   - prazna kartica nikad ne kaže golo „Nema podataka”, nego jedno od
//     četiri stanja (PraznoGotovo, PraznoCeka, PraznoNedostupno, PraznoUIzradi).

// NajviseRedaka je koliko redaka kartica pokazuje na računalu i tabletu.
const NajviseRedaka = 5

// NajviseRedakaMobitel je koliko redaka kartica pokazuje na mobitelu.
const NajviseRedakaMobitel = 3

// Vrste prazne kartice
const (
	PraznoGotovo     = "gotovo"     // nema posla: „✓ Nema ništa za riješiti.”
	PraznoCeka       = "ceka"       // još nema zapisa: „Danas još nema očitanja za BP 16.”
	PraznoNedostupno = "nedostupno" // podaci nisu stigli: „Podaci DHMZ-a trenutačno nisu dostupni. Zadnji su od 07:40.”
	PraznoUIzradi    = "izrada"     // mogućnosti još nema: „U izradi”
)

// KarticaNaslovne je jedna kartica Naslovne.
type KarticaNaslovne struct {
	ID        string // za id elementa i za lijeno učitavanje
	Naslov    string
	Broj      int    // broj u oznaci uz naslov (npr. stavki za riješiti); 0 = bez oznake
	Poveznica string // tekst poveznice desno u glavi („Sve”, „Prognoze”)
	URL       string // kamo vodi poveznica i redak „Više”
	Redovi    []RedakKartice
	// Hitno broji redove koji su iznad praga ili im je istekao rok; redak
	// „Više” kaže koliko ih je među skrivenima
	HitnoNaziv string // „iznad praga”, „s rokom danas”
	Prazno     *PraznaKartica
	// Lijeno: adresa s koje se tijelo kartice učita nakon stranice, kad je
	// računanje sporo (vrijeme, vodostaji); kartica tada najprije ima samo glavu
	Lijeno string
	// Tekst je redak teksta iznad redaka, npr. „Otvoren u 06:40 · još nije
	// predan” na kartici dnevnog lista
	Tekst string
	// Napomena je redak teksta ispod redaka, npr. „Još 9 crpnih stanica
	// miruje.”
	Napomena string
	// Gumbi su radnje kartice na dnu („Otvori list”, „Novi zadatak”)
	Gumbi []GumbNaslovne
	// Tijelo je ime posebnog predloška tijela (npr. vrijeme); prazno znači
	// obični popis redaka
	Tijelo string
	// Podaci su podaci posebnog tijela
	Podaci any
}

// GumbNaslovne je gumb na Naslovnoj: radnja kartice ili radnja uloge iznad
// kartica („Nova prijava”). Glavni je ispunjen, ostali obrubljeni.
type GumbNaslovne struct {
	Tekst  string
	URL    string
	Glavni bool
	// SamoSiroko: gumb samo na računalu i tabletu („Ispiši list”)
	SamoSiroko bool
}

// RedakKartice je redak kartice: ikona, glavni i sporedni tekst, vrijednost
// desno i oznaka faze ili stanja ispod teksta.
type RedakKartice struct {
	Ikona      string
	Glavno     string
	Sporedno   string
	Vrijednost string // desno, npr. „312 cm”
	// Tiho: vrijednost je opis, a ne broj („preventivna obrana”), pa je
	// sitnija i sivlja
	Tiho bool
	// Staro: podatak je stariji od dana, pa je redak sivi (Odluke
	// (31. 10. 2026.), svježina podataka)
	Staro  bool
	URL    string
	Oznaka *OznakaRetka
	// Istaknuto je razred faze kad je cijeli redak ploha faze (sektor u
	// obrani u kartici Stanje obrane): prep, regular, emergency, state
	Istaknuto string
	Hitno     bool // iznad praga ili istekao rok: broji se u retku „Više”
	// Gumb je radnja retka desno („Upiši” uz letvu koja danas još nije
	// očitana)
	Gumb *GumbNaslovne
}

// OznakaRetka je mala oznaka u retku: faza obrane (puna ploha) ili prognoza
// faze (samo obrub), ili neutralno stanje objekta.
type OznakaRetka struct {
	Tekst  string
	Razred string // razred faze (prep, regular, emergency, state) ili prazno za neutralnu
	Obrub  bool   // prognoza faze: obrubljena oznaka bez ispune
}

// TrakaNaslovne je stanje obrane preko cijele širine: kod terenskih uloga
// govori o vlastitom području, uz napomenu o ostatku sektora (Odluke
// (31. 10. 2026.))
type TrakaNaslovne struct {
	Tekst  string
	Razred string // razred faze vlastitog područja; prazno je preventivna obrana
}

// OsobaZaPoziv je osoba u izboru „Nazovi”: ime i prezime s funkcijom i
// broj. Odluke (1. 11. 2026.) traže da izbor vodi na profil osobe, ali
// terenske uloge po pravima ne vide tuđe profile, pa izbor zasad zove
// ravno broj (tel:).
type OsobaZaPoziv struct {
	Ime      string
	Funkcija string
	Broj     string
}

// URL je poveznica poziva; prazna kad osoba nema upisan broj. Predložak
// inače shemu tel: zamijeni oznakom nesigurne adrese, a ovdje su u njoj samo
// znamenke i plus, pa je sigurna.
func (o OsobaZaPoziv) URL() template.URL {
	if o.Broj == "" {
		return ""
	}
	return template.URL("tel:" + strings.Map(func(r rune) rune {
		if r == '+' || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, o.Broj)) // #nosec G203 -- samo znamenke i plus
}

// PraznaKartica je ono što kartica kaže kad nema redaka.
type PraznaKartica struct {
	Vrsta string // PraznoGotovo, PraznoCeka, PraznoNedostupno, PraznoUIzradi
	Tekst string
}

// Vidljivi su redovi koje kartica crta: najviše NajviseRedaka. Na mobitelu
// se od njih skrivaju oni iza NajviseRedakaMobitel (SamoSiroko).
func (k KarticaNaslovne) Vidljivi() []RedakKartice {
	if len(k.Redovi) > NajviseRedaka {
		return k.Redovi[:NajviseRedaka]
	}
	return k.Redovi
}

// SamoSiroko javlja je li redak s indeksom i skriven na mobitelu.
func (k KarticaNaslovne) SamoSiroko(i int) bool { return i >= NajviseRedakaMobitel }

// Vise je tekst retka „Više” za računalo i tablet; prazan kad sve stane.
func (k KarticaNaslovne) Vise() string { return k.viseOd(NajviseRedaka) }

// ViseMobitel je tekst retka „Više” za mobitel; prazan kad sve stane.
func (k KarticaNaslovne) ViseMobitel() string { return k.viseOd(NajviseRedakaMobitel) }

// viseOd slaže „Više: još N →” za retke iza prvih n, s brojem hitnih među
// njima kad ih ima i kad kartica zna kako se hitno zove
func (k KarticaNaslovne) viseOd(n int) string {
	if len(k.Redovi) <= n {
		return ""
	}
	skriveno := k.Redovi[n:]
	hitnih := 0
	for _, r := range skriveno {
		if r.Hitno {
			hitnih++
		}
	}
	if hitnih > 0 && k.HitnoNaziv != "" {
		return fmt.Sprintf("Više: još %d, od toga %d %s", len(skriveno), hitnih, k.HitnoNaziv)
	}
	return fmt.Sprintf("Više: još %d", len(skriveno))
}
