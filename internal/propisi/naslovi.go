package propisi

import (
	"regexp"
	"strings"
	"unicode"

	"gocop/internal/models"
)

// naslovClanka je vrsta odlomka s naslovom iznad članka („Predmet
// uređivanja”). Nije dio propisa, nego naslov sljedećeg članka.
const naslovClanka = "naslov-clanka"

// Klase NN koje čitač prepoznaje. Odlomci zalijepljenog teksta dobiju iste
// klase u razvrstajTekst, pa podjela radi jednako za oba ulaza.
const (
	klasaDonositelj    = "tb-na18"
	klasaBrojAkta      = "broj-d"
	klasaNaslov        = "tb-na16"
	klasaNaslovDalje   = "t-12-9-fett-s"
	klasaSredina       = "t-10-9-sred"
	klasaDioPlana      = "t-11-9-sred"
	klasaNaslovClanka  = "t-10-9-kurz-s"
	klasaClanak        = "clanak"
	klasaPrilog        = "prilog"
	klasaKlasa         = "klasa2"
	klasaTijeloPotpisa = "t-9-8-sredina"
	klasaPotpis        = "t-9-8-potpis"
)

var (
	uzorakGlave    = regexp.MustCompile(`^([IVXLC]+\.)\s+\S`)
	uzorakOdjeljka = regexp.MustCompile(`^(\d+\.)\s+\S`)
	uzorakClanka   = regexp.MustCompile(`^Članak\s+(\d+\.(?:[a-z]\.?)?)$`)
	uzorakTocke    = regexp.MustCompile(`^([IVXLC]+\.)$`)
	uzorakPriloga  = regexp.MustCompile(`(?i)^PRILOG\s+(\d+\.?)$`)
)

// vrstaNaslova javlja je li odlomak naslov ili oznaka dijela: glava
// („I. OPĆE ODREDBE”), odjeljak („1. Pravni status voda”), dio Plana,
// naslov iznad članka, članak („Članak 97.a”), točka Plana („XXIII.”) ili
// prilog („PRILOG 1.”). Vrsta se gleda po klasi NN, a broj po tekstu.
// Naslov t-10-9-sred bez broja vraća glavu bez broja.
func vrstaNaslova(o Odlomak) (vrsta, broj string, ok bool) {
	switch {
	case o.Klasa == klasaSredina:
		vrsta, broj = naslovSredine(o.Tekst)
		return vrsta, broj, true
	case o.Klasa == klasaDioPlana:
		return models.DioPlana, "", true
	case strings.HasPrefix(o.Klasa, klasaNaslovClanka):
		return naslovClanka, "", true
	case strings.HasPrefix(o.Klasa, klasaClanak):
		return oznakaClanka(o.Tekst)
	case o.Klasa == klasaPrilog:
		if g := uzorakPriloga.FindStringSubmatch(o.Tekst); g != nil {
			return models.DioPrilog, g[1], true
		}
	}
	return "", "", false
}

// naslovSredine razlikuje glavu („I. OPĆE ODREDBE”) od odjeljka („1. Pravni
// status voda”): obje imaju klasu t-10-9-sred. Bez broja je glava bez broja.
func naslovSredine(tekst string) (vrsta, broj string) {
	if b := rimskiBroj(uzorakGlave, tekst); b != "" {
		return models.DioGlava, b
	}
	if g := uzorakOdjeljka.FindStringSubmatch(tekst); g != nil {
		return models.DioOdjeljak, g[1]
	}
	return models.DioGlava, ""
}

// oznakaClanka čita oznaku s klasom članka: „Članak 97.a” je članak, a
// „XXIII.” točka Plana.
func oznakaClanka(tekst string) (vrsta, broj string, ok bool) {
	if g := uzorakClanka.FindStringSubmatch(tekst); g != nil {
		return models.DioClanak, g[1], true
	}
	if b := rimskiBroj(uzorakTocke, tekst); b != "" {
		return models.DioTocka, b, true
	}
	return "", "", false
}

// rimskiBroj vraća rimski broj s točkom s početka teksta kad je pravilan
// („XXIII.”), inače prazno.
func rimskiBroj(uzorak *regexp.Regexp, s string) string {
	g := uzorak.FindStringSubmatch(s)
	if g == nil {
		return ""
	}
	if _, ok := IzRimskog(strings.TrimSuffix(g[1], ".")); !ok {
		return ""
	}
	return g[1]
}

var (
	uzorakKlase   = regexp.MustCompile(`^(?i:klasa|urbroj|ur\.\s?broj)\s*:`)
	uzorakMjesta  = regexp.MustCompile(`^\p{Lu}[\p{L} ]*,\s+\d{1,2}\.\s+\p{L}+\s+\d{4}\.?$`)
	krajPotpisa   = regexp.MustCompile(`v\.\s?r\.$`)
	krajRecenice  = regexp.MustCompile(`[.;:,]$`)
	pocetakTijela = regexp.MustCompile(`^(\(|–|\d|»)`)
)

// najviseRedakaPotpisa je koliko redaka iza klase i urbroja smije imati
// potpis u zalijepljenom tekstu („Predsjednik / Hrvatskoga sabora / … v. r.”).
const najviseRedakaPotpisa = 6

// razvrstajTekst daje klase NN odlomcima zalijepljenog teksta, po obliku
// retka i po susjednim recima: „Članak 5.” je članak, redak iznad njega
// naslov članka, redak velikim slovima naslov glave ili dijela, a sve prije
// „Na temelju …” zaglavlje stranice.
func razvrstajTekst(o []Odlomak) []Odlomak {
	t := &razvrstavanje{o: append([]Odlomak(nil), o...)}
	t.plan = t.broji(uzorakTocke) > t.broji(uzorakClanka)
	pocetak := t.pocetak()
	for i := range pocetak {
		t.o[i].Klasa = klasaDonositelj
	}
	for i := pocetak; i < len(t.o); i++ {
		if t.o[i].Klasa == "" {
			t.o[i].Klasa = t.klasa(i)
		}
	}
	return t.o
}

type razvrstavanje struct {
	o    []Odlomak
	plan bool // točke s rimskim brojem, a ne članci: Državni plan
}

func (t *razvrstavanje) broji(uzorak *regexp.Regexp) int {
	n := 0
	for _, o := range t.o {
		if uzorak.MatchString(o.Tekst) {
			n++
		}
	}
	return n
}

// pocetak je prvi redak „Na temelju …” ispred prvog članka ili točke. Što
// je iznad njega, zaglavlje je stranice s koje je tekst kopiran.
func (t *razvrstavanje) pocetak() int {
	for i := range t.o {
		if t.oznaka(i) {
			return 0
		}
		if strings.HasPrefix(t.o[i].Tekst, "Na temelju ") {
			return i
		}
	}
	return 0
}

// oznaka javlja je li redak oznaka članka, točke Plana ili priloga.
func (t *razvrstavanje) oznaka(i int) bool {
	s := t.o[i].Tekst
	return uzorakClanka.MatchString(s) || rimskiBroj(uzorakTocke, s) != "" || uzorakPriloga.MatchString(s)
}

// klasa daje klasu NN retku i po potrebi i recima potpisa iza njega.
func (t *razvrstavanje) klasa(i int) string {
	s := t.o[i].Tekst
	switch {
	case uzorakPriloga.MatchString(s):
		return klasaPrilog
	case t.oznaka(i):
		return klasaClanak
	case t.klasaIliMjesto(i):
		t.potpis(i + 1)
		return klasaKlasa
	case rimskiBroj(uzorakGlave, s) != "":
		return klasaSredina
	case velikimSlovima(s):
		return t.naslovVelikim(i)
	case t.naslovIznadClanka(i):
		return klasaNaslovClanka
	case t.odjeljak(i):
		return klasaSredina
	}
	return ""
}

// klasaIliMjesto javlja je li redak „Klasa:”, „Urbroj:” ili mjesto i datum
// ispod njih („Zagreb, 28. lipnja 2019.”).
func (t *razvrstavanje) klasaIliMjesto(i int) bool {
	s := t.o[i].Tekst
	return uzorakKlase.MatchString(s) || (i > 0 && t.o[i-1].Klasa == klasaKlasa && uzorakMjesta.MatchString(s))
}

// odjeljak javlja je li redak naslov odjeljka („1. Pravni status voda”): iza
// njega je naslov članka ili sam članak, a nema točke na kraju kao točka
// nabrajanja.
func (t *razvrstavanje) odjeljak(i int) bool {
	s := t.o[i].Tekst
	return uzorakOdjeljka.MatchString(s) && !krajRecenice.MatchString(s) && (t.naslovIznadClanka(i+1) || t.clanak(i+1))
}

// potpis označi retke iza klase i urbroja do retka koji završava s „v. r.”:
// tijelo uz potpis i potpis. Bez „v. r.” u nekoliko redaka ne označi ništa.
func (t *razvrstavanje) potpis(od int) {
	for i := od; i < len(t.o) && i < od+najviseRedakaPotpisa; i++ {
		if uzorakKlase.MatchString(t.o[i].Tekst) || uzorakMjesta.MatchString(t.o[i].Tekst) {
			return
		}
		if krajPotpisa.MatchString(t.o[i].Tekst) {
			for j := od; j < i; j++ {
				t.o[j].Klasa = klasaTijeloPotpisa
			}
			t.o[i].Klasa = klasaPotpis
			return
		}
	}
}

// naslovVelikim razvrstava redak napisan velikim slovima: „ODLUKU” i
// „O PROGLAŠENJU …” su odluka o proglašenju, redak iza kojeg slijedi još
// jedan redak velikim slovima i nastavak „O …” su naslov akta, a ostalo je
// naslov dijela Plana ili glave bez broja.
func (t *razvrstavanje) naslovVelikim(i int) string {
	s := t.o[i].Tekst
	switch {
	case s == "ODLUKU", i+1 < len(t.o) && velikimSlovima(t.o[i+1].Tekst) && !t.oznaka(i+1):
		return klasaNaslov
	case strings.HasPrefix(s, "O ") && i > 0 && (t.o[i-1].Klasa == klasaNaslov || t.o[i-1].Klasa == klasaNaslovDalje):
		return klasaNaslovDalje
	case t.plan:
		return klasaDioPlana
	}
	return klasaSredina
}

func (t *razvrstavanje) clanak(i int) bool {
	return i < len(t.o) && uzorakClanka.MatchString(t.o[i].Tekst)
}

// naslovIznadClanka javlja je li redak naslov članka ispod njega: kratka
// rečenica bez točke na kraju, koja ne počinje oznakom stavka ili točke.
func (t *razvrstavanje) naslovIznadClanka(i int) bool {
	if i >= len(t.o) || !t.clanak(i+1) || t.oznaka(i) {
		return false
	}
	s := t.o[i].Tekst
	return len(s) <= 300 && !krajRecenice.MatchString(s) && !pocetakTijela.MatchString(s)
}

// velikimSlovima javlja je li redak napisan samo velikim slovima, s barem
// tri slova.
func velikimSlovima(s string) bool {
	slova := 0
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsLetter(r) {
			slova++
		}
	}
	return slova >= 3
}
