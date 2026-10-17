package razmjena

// Što računalo zna, javljeno na početku razgovora (docs/plan-web-sync.md §3,
// G2): oznake mogućnosti, vrste zapisa sa shemama koje razumije i koliko
// zapisa kod njega čeka nadogradnju. Nosi ga granica razgovora, pa ide
// svakim putem (port razmjene, Tailscale, tunel), a isti tip kasnije
// ugrađuju i prijava kod sastajališta i pozdrav WEB synca.
//
// Pravilo mogućnosti zapisano je samo ovdje:
//   - Oznaka je ime/N. Ime su mala slova, znamenke i '-', N je cijeli broj
//     od 1 naviše bez vodećih nula i predznaka; ime se odvaja na zadnjoj '/'.
//   - Nepoznata ili neispravna oznaka se zanemaruje i nije greška razgovora.
//   - Kad popisa nema ili u njemu nema nijedne ispravne oznake (čvor
//     0.0.38, null, prazan popis, samo neispravne), vrijedi razmjena/1.
//     Popis s barem jednom ispravnom oznakom je doslovan.
//   - Za isto ime vrijedi najviši ZAJEDNIČKI N (Dogovor), pa svaka strana
//     navodi svaki N koji još govori.
//   - Novo neobavezno polje ne treba oznaku. Nova vrsta poruke ili
//     promijenjeno značenje smiju samo kad Dogovor kaže da obje strane imaju
//     oznaku.
//
// Oznake nikad ne sadrže ime protokola ni „gocop”: ALPN je ime protokola,
// '-' i oznaka, a ime protokola postaje postavka. sadrzaj/1 je rezervirano
// za roj, a web/1 i ostale objavljuju tek faze koje ih uvode.
//
// Tipovi polja su zauvijek: mogucnosti je popis teksta, vrste mapa teksta na
// cijeli broj, cekaNadogradnju cijeli broj. Drugi tip srušio bi kod starijeg
// čvora dekodiranje cijele granice, pa novi oblik ide samo pod novi ključ.

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// OznakaRazmjena je današnji razgovor: granice, delte, želje, sadržaj i
// kraj, uz kraj umjesto želja od starijeg čvora. Današnji sadržaj spada
// ovamo i ne zove se sadrzaj/1.
const OznakaRazmjena = "razmjena/1"

// ImeGranicaOdbijenih je ime oznake granice odbijenih (G4 u
// docs/plan-web-sync.md §3): odmah iza granica računalo javi dokle je
// odbilo ono što mu je druga strana poslala, a druga strana šalje tek iza
// toga (internal/peers/granica_odbijenih.go). Nova je vrsta poruke, pa ide
// samo kad Dogovor kaže da je obje strane imaju.
const ImeGranicaOdbijenih = "granica-odbijenih"

// OznakaGranicaOdbijenih je prva inačica granice odbijenih
const OznakaGranicaOdbijenih = ImeGranicaOdbijenih + "/1"

// Ograde onoga što se od druge strane pamti: popis i mapa idu u bazu
const (
	najduljeIme   = 32   // znakova u imenu oznake
	najviseOznaka = 32   // oznaka druge strane
	najduljaVrsta = 64   // znakova u imenu vrste zapisa
	najviseVrsta  = 1024 // vrsta zapisa druge strane
	slovaImena    = "abcdefghijklmnopqrstuvwxyz0123456789-"
	slovaVrste    = slovaImena + "_" // vrste su imena entiteta (dnevna_izvjesca)
)

// Znanje je ono što računalo javlja o sebi u granici. Prazna polja
// izostaju, pa granica bez znanja ostaje bajt po bajt kao u 0.0.38. Ugrađuje
// se kao vrijednost, bez json oznake, pa su ključevi u istom objektu kao
// ostala polja granice; nijedan se ne smije poklopiti s njima.
type Znanje struct {
	Mogucnosti      []string       `json:"mogucnosti,omitempty"`      // oznake ime/N; bez njih vrijedi razmjena/1
	Vrste           map[string]int `json:"vrste,omitempty"`           // vrsta zapisa (entitet) → najviša shema koju pošiljatelj razumije
	CekaNadogradnju int            `json:"cekaNadogradnju,omitempty"` // broj ZAPISA, ne verzija, koji kod pošiljatelja čekaju nadogradnju
}

// NaseMogucnosti su oznake koje ovaj program objavljuje, poredane i bez
// ponavljanja. Svaki poziv daje novi popis.
func NaseMogucnosti() []string {
	return []string{OznakaGranicaOdbijenih, OznakaRazmjena}
}

// Dogovor je za svako ime koje obje strane imaju najviši zajednički N.
// Bez ijedne ispravne oznake druge strane vrijedi razmjena/1. Da obje strane
// imaju ime/N, znači Dogovor(...)[ime] == N, a ne >=: viša verzija ne mora
// uključivati nižu.
func Dogovor(moje, njihove []string) map[string]int {
	imaju := ispravne(njihove)
	if len(imaju) == 0 {
		imaju = map[string]bool{OznakaRazmjena: true}
	}
	imam := ispravne(moje)
	out := map[string]int{}
	for oznaka := range imaju {
		if ime, n, _ := procitajOznaku(oznaka); imam[oznaka] && n > out[ime] {
			out[ime] = n
		}
	}
	return out
}

// Ocisceno je znanje druge strane kakvo se smije spremiti: ispravne oznake
// bez ponavljanja, poredane i ograđene, vrste s ispravnim imenom i shemom
// od 1 naviše, i broj na čekanju ne manji od nule. Nepoznate oznake
// ostaju, jer se po njima vidi što druga strana zna, a ovaj program ne.
// Bez ijedne ispravne oznake ili vrste to polje je nil, pa spremljene
// oznake daju isti Dogovor kao popis koji je stigao.
func (z Znanje) Ocisceno() Znanje {
	return Znanje{Mogucnosti: ocisceneOznake(z.Mogucnosti), Vrste: ocisceneVrste(z.Vrste), CekaNadogradnju: max(z.CekaNadogradnju, 0)}
}

// procitajOznaku rastavi oznaku ime/N; ok je false za sve što nije
// ispravna oznaka
func procitajOznaku(oznaka string) (ime string, n int, ok bool) {
	i := strings.LastIndexByte(oznaka, '/')
	if i < 0 {
		return "", 0, false
	}
	n, ok = brojOznake(oznaka[i+1:])
	if !ok || !samoZnakovi(oznaka[:i], slovaImena, najduljeIme) {
		return "", 0, false
	}
	return oznaka[:i], n, true
}

// brojOznake je N iz ime/N: od 1 naviše, napisan kako ga piše strconv
// (bez vodećih nula, predznaka i decimala)
func brojOznake(tekst string) (int, bool) {
	n, err := strconv.Atoi(tekst)
	return n, err == nil && n >= 1 && strconv.Itoa(n) == tekst
}

// samoZnakovi javlja je li tekst neprazan, ne dulji od najdulje i samo od
// dopuštenih znakova
func samoZnakovi(tekst, dopusteni string, najdulje int) bool {
	return tekst != "" && len(tekst) <= najdulje && strings.Trim(tekst, dopusteni) == ""
}

// ispravne su ispravne oznake s popisa, bez ponavljanja
func ispravne(oznake []string) map[string]bool {
	out := map[string]bool{}
	for _, oznaka := range oznake {
		if _, _, ok := procitajOznaku(oznaka); ok {
			out[oznaka] = true
		}
	}
	return out
}

// ocisceneOznake su ispravne oznake, poredane i bez ponavljanja, najviše
// najviseOznaka; bez ijedne nil. Ograda prvo zadrži oznake koje ima i ovaj
// program, a tek onda prve po abecedi, pa ne mijenja Dogovor.
func ocisceneOznake(oznake []string) []string {
	sve := ispravne(oznake)
	out := zajednicke(sve)
	for _, oznaka := range slices.Sorted(maps.Keys(sve)) {
		if len(out) < najviseOznaka && !slices.Contains(out, oznaka) {
			out = append(out, oznaka)
		}
	}
	slices.Sort(out)
	return out
}

// zajednicke su naše oznake koje ima i druga strana; bez ijedne nil
func zajednicke(njihove map[string]bool) []string {
	var out []string
	for _, oznaka := range NaseMogucnosti() {
		if njihove[oznaka] {
			out = append(out, oznaka)
		}
	}
	return out
}

// ocisceneVrste su vrste s ispravnim imenom i shemom od 1 naviše, najviše
// najviseVrsta, prve po abecedi; bez ijedne nil
func ocisceneVrste(vrste map[string]int) map[string]int {
	out := map[string]int{}
	for _, vrsta := range slices.Sorted(maps.Keys(vrste)) {
		if len(out) < najviseVrsta && ispravnaVrsta(vrsta, vrste[vrsta]) {
			out[vrsta] = vrste[vrsta]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ispravnaVrsta: ime entiteta od dopuštenih znakova i shema od 1 naviše
func ispravnaVrsta(vrsta string, shema int) bool {
	return shema >= 1 && samoZnakovi(vrsta, slovaVrste, najduljaVrsta)
}
