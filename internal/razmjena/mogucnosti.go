package razmjena

// Što računalo zna, javljeno na početku razgovora (plan WEB synca, §3,
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
// Oznake biblioteke nikad ne sadrže „gocop”, a oznaka koju prijavljuje
// aplikacija nema ime protokola kao cijeli dio imena (peers.PrijaviKorak):
// ALPN je ime protokola, '-' i oznaka, a ime protokola postaje postavka.
// Iznimka je oznaka iz NaseMogucnosti koja nije oznaka biblioteke (danas
// samo dijelovi-karte/1): nju aplikacija smije prijaviti uz svako ime
// protokola, privremeno do PR-a 8 (Odluke (28. 11. 2026.)).
// sadrzaj/1 je rezervirano za roj; web/1 (od F3b) javlja samo računalo
// prijavljeno za WEB sync.
//
// Tipovi polja su zauvijek: mogucnosti je popis teksta, vrste mapa teksta na
// cijeli broj, cekaNadogradnju cijeli broj. Drugi tip srušio bi kod starijeg
// čvora dekodiranje cijele granice, pa novi oblik ide samo pod novi ključ.

import (
	"errors"
	"fmt"
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
// planu WEB synca, §3): odmah iza granica računalo javi dokle je
// odbilo ono što mu je druga strana poslala, a druga strana šalje tek iza
// toga (internal/peers/granica_odbijenih.go). Nova je vrsta poruke, pa ide
// samo kad Dogovor kaže da je obje strane imaju.
const ImeGranicaOdbijenih = "granica-odbijenih"

// OznakaGranicaOdbijenih je prva inačica granice odbijenih
const OznakaGranicaOdbijenih = ImeGranicaOdbijenih + "/1"

// ImeRupe je ime oznake rupa u nizu (G4b u planu WEB synca, §3): iza
// granice odbijenih računalo javi gdje mu u nizu možda nešto nedostaje, a
// uz deltu za koji dio poslanoga ne jamči da ga ima cijelog i koji je dio
// tuđe rupe popunilo (internal/peers/rupe.go). Nova je vrsta poruke i novo
// značenje delte, pa ide samo kad Dogovor kaže da je obje strane imaju.
const ImeRupe = "rupe"

// OznakaRupe je prva inačica rupa u nizu
const OznakaRupe = ImeRupe + "/1"

// ImeDijeloviKarte je ime oznake dijelova karte bez interneta: želje
// smiju nositi otiske dijelova karte, a sadržaj stavke vrste "karta-dio"
// bez kanala. Primatelj bez oznake takvu stavku ne razumije (upisao bi je
// među sadržaje kao siroče), pa se dijelovi traže i šalju samo kad Dogovor
// kaže da je obje strane imaju.
const ImeDijeloviKarte = "dijelovi-karte"

// OznakaDijeloviKarte je prva inačica dijelova karte
const OznakaDijeloviKarte = ImeDijeloviKarte + "/1"

// ImeWeb je ime oznake WEB synca (plan WEB synca, F3b): računalo prima
// pozive WEB synca preko sastajališta i na njih odgovara, uz web_sync =
// false odzivom „isključeno”. Javlja je samo računalo koje je prijavljeno
// za WEB sync (internal/peers), pa je nema u NaseMogucnosti, ali je ovaj
// program zna (NepoznateOznake).
const ImeWeb = "web"

// OznakaWeb je prva inačica WEB synca
const OznakaWeb = ImeWeb + "/1"

// ImePotpisVerzije je ime oznake potpisa autora verzije (Odluke
// (26. 11. 2026.)): verzije u delti nose potpis i ključ autora (polja
// potpis i kljuc_autora). Računalo s oznakom primljeni potpis provjerava,
// sprema i prosljeđuje dalje, pa se šalje samo kad Dogovor kaže da obje
// strane imaju oznaku; drugoj strani delta ostaje bajt za bajt ista.
// Računalo s oznakom potpis ne skida, pa nepotpisanu verziju od njega
// primatelj smije tumačiti strože.
const ImePotpisVerzije = "potpis-verzije"

// OznakaPotpisVerzije je prva inačica potpisa autora verzije
const OznakaPotpisVerzije = ImePotpisVerzije + "/1"

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
	// Uloge su poslovi koje pošiljatelj radi za cijelu mrežu (UlogaIzdaje,
	// UlogaPreuzima), samo za prikaz na Stanju razmjene (Odluke
	// (30. 10. 2026.), 13). Ne mijenjaju razgovor ni ono što primatelj
	// prima ili šalje, pa ne trebaju oznaku; stariji program ih ne šalje i
	// zanemari.
	Uloge []string `json:"uloge,omitempty"`
	// Karta je stanje karte bez interneta kod pošiljatelja, isto samo za
	// prikaz na Stanju razmjene (Odluke (18. 11. 2026.), Administracija 8):
	// ne mijenja razgovor, pa ne treba oznaku; stariji program je ne šalje.
	// Prazan otisak znači da pošiljatelj karte nema.
	Karta *ZnanjeKarte `json:"karta,omitempty"`
}

// ZnanjeKarte je karta bez interneta kod pošiljatelja: otisak datoteke,
// je li složena i koliko dijelova ima od koliko
type ZnanjeKarte struct {
	Otisak   string `json:"otisak"`
	Slozena  bool   `json:"slozena,omitempty"`
	Dijelova int    `json:"dijelova,omitempty"`
	Svih     int    `json:"svih,omitempty"`
}

// najviseDijelovaZnanja ograđuje brojeve u znanju o karti (kao
// models.NajviseDijelovaKarte; razmjena ne uvozi models)
const najviseDijelovaZnanja = 4096

// ocisceneKarta je znanje o karti uz otisak od 64 heksadekadna znaka i
// brojeve unutar granica; ostalo otpada (nil)
func ocisceneKarta(k *ZnanjeKarte) *ZnanjeKarte {
	if k == nil {
		return nil
	}
	if k.Otisak != "" && (len(k.Otisak) != 64 || !samoZnakovi(k.Otisak, "0123456789abcdef", 64)) {
		return nil
	}
	svih := min(max(k.Svih, 0), najviseDijelovaZnanja)
	return &ZnanjeKarte{Otisak: k.Otisak, Slozena: k.Slozena && k.Otisak != "", Dijelova: min(max(k.Dijelova, 0), svih), Svih: svih}
}

// Uloge računala koje se javljaju u znanju; mijenjaju se samo na samom
// računalu
const (
	UlogaIzdaje   = "izdaje"   // izdaje prognozu
	UlogaPreuzima = "preuzima" // preuzima vodostaje s izvora
)

// NaseMogucnosti su oznake koje ovaj program objavljuje, poredane i bez
// ponavljanja. Svaki poziv daje novi popis.
func NaseMogucnosti() []string {
	return []string{OznakaDijeloviKarte, OznakaGranicaOdbijenih, OznakaPotpisVerzije, OznakaRazmjena, OznakaRupe}
}

// ErrNeispravnaOznaka: oznaka nije oblika ime/N iz pravila mogućnosti
var ErrNeispravnaOznaka = errors.New("oznaka mogućnosti nije oblika ime/N")

// ProvjeriOznaku rastavi oznaku ime/N po pravilu mogućnosti, a za sve
// drugo vrati ErrNeispravnaOznaka. Služi onome tko oznaku tek objavljuje
// (npr. dodatni korak razgovora), da neispravnu odbije prije razgovora.
func ProvjeriOznaku(oznaka string) (ime string, n int, err error) {
	ime, n, ok := procitajOznaku(oznaka)
	if !ok {
		return "", 0, fmt.Errorf("%w: %q", ErrNeispravnaOznaka, oznaka)
	}
	return ime, n, nil
}

// NepoznateOznake su NepoznateOznakeUz s oznakama iz NaseMogucnosti
func NepoznateOznake(njihove []string) []string {
	return NepoznateOznakeUz(njihove, NaseMogucnosti())
}

// NepoznateOznakeUz su ispravne oznake druge strane čije ime ovaj program
// ne zna (G7: djelomično razumljeno), poredane. znam su oznake koje ovaj
// program objavljuje (prednost iz OciscenoUz), sa ili bez web/1; ime web
// je uvijek poznato. Gledaju se samo oznake koje se pamte (ocisceneOznake
// uz prednost znam, kao kod spremljenog znanja), pa ih nikad nije više od
// najviseOznaka.
func NepoznateOznakeUz(njihove, znam []string) []string {
	imena := map[string]bool{ImeWeb: true}
	for _, o := range znam {
		if ime, _, ok := procitajOznaku(o); ok {
			imena[ime] = true
		}
	}
	var out []string
	for _, o := range ocisceneOznake(njihove, znam) {
		if ime, _, _ := procitajOznaku(o); !imena[ime] {
			out = append(out, o)
		}
	}
	return out
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

// Ocisceno je OciscenoUz s prednošću oznaka iz NaseMogucnosti
func (z Znanje) Ocisceno() Znanje {
	return z.OciscenoUz(NaseMogucnosti())
}

// OciscenoUz je znanje druge strane kakvo se smije spremiti: ispravne
// oznake bez ponavljanja, poredane i ograđene, vrste s ispravnim imenom i
// shemom od 1 naviše, i broj na čekanju ne manji od nule. U ogradi oznaka
// prednost imaju one iz prednost (oznake koje ovaj program objavljuje),
// sa ili bez web/1: oznake imena web prednost ne dobivaju, kao u 0.0.71.
// Nepoznate oznake ostaju, jer se po njima vidi što druga strana zna, a
// ovaj program ne. Bez ijedne ispravne oznake ili vrste to polje je nil,
// pa spremljene oznake daju isti Dogovor kao popis koji je stigao. Od
// uloga ostaju samo poznate.
func (z Znanje) OciscenoUz(prednost []string) Znanje {
	return Znanje{Mogucnosti: ocisceneOznake(z.Mogucnosti, prednost), Vrste: ocisceneVrste(z.Vrste), CekaNadogradnju: max(z.CekaNadogradnju, 0),
		Uloge: ocisceneUloge(z.Uloge), Karta: ocisceneKarta(z.Karta)}
}

// ocisceneUloge su poznate uloge druge strane, poredane i bez ponavljanja;
// nepoznate otpadaju, jer ih stranica ne bi znala imenovati. Bez ijedne nil.
func ocisceneUloge(uloge []string) []string {
	var out []string
	for _, u := range []string{UlogaIzdaje, UlogaPreuzima} {
		if slices.Contains(uloge, u) {
			out = append(out, u)
		}
	}
	return out
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
// najviseOznaka; bez ijedne nil. Ograda prvo zadrži oznake iz prednost
// (one koje ima i ovaj program) osim onih imena web, a tek onda prve po
// abecedi, pa ne mijenja Dogovor.
func ocisceneOznake(oznake, prednost []string) []string {
	sve := ispravne(oznake)
	out := zajednicke(sve, prednost)
	for _, oznaka := range slices.Sorted(maps.Keys(sve)) {
		if len(out) < najviseOznaka && !slices.Contains(out, oznaka) {
			out = append(out, oznaka)
		}
	}
	slices.Sort(out)
	return out
}

// zajednicke su oznake iz prednost koje ima i druga strana, redom iz
// prednost, bez ponavljanja i najviše najviseOznaka; bez ijedne nil. Oznake
// imena web se preskaču: web/1 javlja samo računalo prijavljeno za WEB
// sync, pa ga 0.0.71 u ogradi nije stavljao naprijed.
func zajednicke(njihove map[string]bool, prednost []string) []string {
	var out []string
	for _, oznaka := range prednost {
		if ime, _, _ := procitajOznaku(oznaka); ime == ImeWeb {
			continue
		}
		if len(out) < najviseOznaka && njihove[oznaka] && !slices.Contains(out, oznaka) {
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
