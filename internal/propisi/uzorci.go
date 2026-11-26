package propisi

import (
	"regexp"
	"strings"

	"gocop/internal/models"
)

// Dijelovi uzoraka uputa izmjena. Tekst je prije toga očišćen (ocisti), pa
// u njemu nema tvrdih razmaka ni dvostrukih razmaka.
const (
	// broj članka, stavka ili točke: „110.”, „97.a”, „2.1.”, „71.a”
	brojU = `\d+\.(?:[a-z]\.?|\d+\.)?`
	// broj ili rimski broj točke Plana: „XXIII.”
	brojIliRimskiU = `(?:` + brojU + `|[IVXLC]+\.)`
	// izraz u navodnicima: »…«
	izrazU = `»([^«»]*)«`
	// popis brojeva: „2. i 3.”, „1. do 4.”, „95., 99.”
	popisU = brojIliRimskiU + `(?:\s*(?:,|i|do)\s*` + brojIliRimskiU + `)*`
	// riječ koja kaže razinu cilja
	razinaU = `članku|člancima|stavku|stavcima|točki|točkama|podtočki|podtočkama|podstavku|alineji`
)

var (
	// uzorakUvoda: „U Zakonu o vodama (»Narodne novine«, br. 66/19. i 84/21.) ”
	uzorakUvoda = regexp.MustCompile(`^U\s+(?:cijelom tekstu\s+)?\p{Lu}[^()]*?\s*\([^()]*»Narodne novine«[^()]*\)\s*`)
	// uzorakMjesta: „u članku 141. stavku 2. točki 3. ”
	uzorakMjestaUpute = regexp.MustCompile(`^(?:[Uu]\s+)?((?:(?:u\s+)?(?:` + razinaU + `)\s+` + popisU + `,?\s*)+)`)
	dioMjesta         = regexp.MustCompile(`(` + razinaU + `)\s+(` + popisU + `)`)
	// uzorakDosadasnjeg: „U dosadašnjem stavku 3., koji postaje stavak 4., ”
	uzorakDosadasnjeg = regexp.MustCompile(`^U dosadašnjem (stavku|točki|članku) (` + brojU + `),? koj[ia] postaje (?:stavak|točka|članak) (` + brojU + `),?\s*`)
	uzorakUvodneRec   = regexp.MustCompile(`^u uvodnoj rečenici,?\s*`)
	// više radnji u jednoj rečenici: „…, a iza riječi: …”
	razdjelnikRadnji = regexp.MustCompile(`,\s+a\s+(iza riječi|ispred riječi|riječi)`)
	tokenBroja       = regexp.MustCompile(brojIliRimskiU + `|\bdo\b`)
	// glagol izmjene: rečenica s njim, a bez uzorka, nije prepoznata
	glagolIzmjene = regexp.MustCompile(`(?i)zamjenjuj[eu] se|dodaj[eu] se|briš[eu] se|mijenja(?:ju)? se i glas|postaj[eu] `)
	uzorakBlago   = regexp.MustCompile(`^riječi?:?\s*»(.*?)«?\s*zamjenjuj[eu] se riječ(?:ju|ima):?\s*»(.*?)«`)
)

// porukaBlago je upozorenje uz uputu koja je prošla samo blagi uzorak.
const porukaBlago = "Navodnik nije zatvoren u Narodnim novinama. Provjerite odredbu."

// prepoznavac je uzorak jedne vrste upute i kako se iz pogotka slaže uputa.
// Cilj je mjesto koje je rečenica već navela ili naslijedila.
type prepoznavac struct {
	re    *regexp.Regexp
	slozi func(m []string, c models.CiljUpute) models.UputaIzmjene
}

// prepoznavaci su uzorci uputa redom kojim se isprobavaju.
var prepoznavaci = []prepoznavac{
	{regexp.MustCompile(`^riječi?:?\s*` + izrazU + `(?:\s*u određenom broju i padežu)?\s*zamjenjuj[eu] se riječ(?:ju|ima):?\s*` + izrazU),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			return models.UputaIzmjene{Vrsta: models.UputaZamjenaRijeci, Cilj: c, Stari: m[1], Novi: m[2]}
		}},
	{regexp.MustCompile(`^broj(?:evi)?:?\s*` + izrazU + `\s*zamjenjuj[eu] se broj(?:em|evima):?\s*` + izrazU),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			return models.UputaIzmjene{Vrsta: models.UputaZamjenaBroja, Cilj: c, Stari: m[1], Novi: m[2]}
		}},
	{regexp.MustCompile(`^(iza|ispred)\s+riječi:?\s*` + izrazU + `\s*(?:stavlja se zarez i dodaj[eu]|dodaj[eu] se)\s+riječi?:?\s*` + izrazU),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			return models.UputaIzmjene{Vrsta: models.UputaDodavanjeRijeci, Cilj: c, Iza: m[1] + " riječi", Stari: m[2], Novi: m[3]}
		}},
	{regexp.MustCompile(`^riječi?:?\s*` + izrazU + `\s*briš[eu] se`),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			return models.UputaIzmjene{Vrsta: models.UputaBrisanjeRijeci, Cilj: c, Stari: m[1]}
		}},
	{regexp.MustCompile(`(?i)^naslov iznad članka\s+(` + brojU + `)\s+mijenja se i glasi:$`),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			postaviRazinu(&c, "članak", razvijPopis(m[1]))
			c.NaslovIznad = true
			return models.UputaIzmjene{Vrsta: models.UputaNoviTekst, Cilj: c}
		}},
	{regexp.MustCompile(`(?i)^(?:(članak|stavak|točka|podtočka)\s+(` + brojIliRimskiU + `)\s+)?mijenja(?:ju)? se i glas[ie]:$`),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			if m[1] != "" {
				postaviRazinu(&c, m[1], razvijPopis(m[2]))
			}
			return models.UputaIzmjene{Vrsta: models.UputaNoviTekst, Cilj: c}
		}},
	{regexp.MustCompile(`(?i)^iza\s+(članka|stavka|točke|podtočke)\s+(` + brojIliRimskiU + `)\s+dodaj[eu] se\s+(?:nov[iae]\s+)?(naslovi? iznad član(?:ka|aka) i\s+)?` +
		`(?:članak|članci|stavak|stavci|točka|točke|podtočka|podtočke)\s+(` + brojIliRimskiU + `)` +
		`(?:\s*(i|do)\s+(?:stavak\s+|točka\s+)?(` + brojIliRimskiU + `))?\s+koj[iae]\s+glas[ie]:$`),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			postaviRazinu(&c, m[1], razvijPopis(m[2]))
			c.NaslovIznad = m[3] != ""
			return models.UputaIzmjene{Vrsta: models.UputaDodavanjeDijela, Cilj: c, Iza: "iza " + strings.ToLower(m[1]) + " " + m[2],
				NoveOznake: razvijPopis(m[4] + " " + m[5] + " " + m[6])}
		}},
	{regexp.MustCompile(`(?i)^(naslovi? iznad član(?:ka|aka) i\s+)?(članak|članci|stavak|stavci|točka|točke|podtočka|podtočke)\s+(` + popisU + `)\s+briš[eu] se\.$`),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			postaviRazinu(&c, m[2], razvijPopis(m[3]))
			c.NaslovIznad = m[1] != ""
			return models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela, Cilj: c}
		}},
	{regexp.MustCompile(`(?i)^dosadašnj[iae]\s+(stavak|stavci|točka|točke|članak|članci)\s+(` + brojU + `(?:\s*(?:i|do)\s*` + brojU + `)?)` +
		`\s+postaj[eu]\s+(?:stavak|stavci|točka|točke|članak|članci)\s+(` + brojU + `(?:\s*(?:i|do)\s*` + brojU + `)?)`),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			postaviRazinu(&c, m[1], razvijPopis(m[2]))
			return models.UputaIzmjene{Vrsta: models.UputaPreoznacavanje, Cilj: c, NoveOznake: razvijPopis(m[3])}
		}},
	{regexp.MustCompile(`(?i)^na kraju (rečenice|stavka|točke|podstavka)\s+točka se zamjenjuje zarezom i dodaju se riječi:?\s*(?:` + izrazU + `)?`),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			return models.UputaIzmjene{Vrsta: models.UputaDodavanjeRijeci, Cilj: c, Iza: "na kraju " + strings.ToLower(m[1]), Novi: m[2]}
		}},
	{regexp.MustCompile(`(?i)^(prva|druga|treća|četvrta|peta|zadnja|posljednja) rečenica briše se\.?$`),
		func(m []string, c models.CiljUpute) models.UputaIzmjene {
			return models.UputaIzmjene{Vrsta: models.UputaBrisanjeRijeci, Cilj: c, Stari: strings.ToLower(m[1]) + " rečenica"}
		}},
	{regexp.MustCompile(`^U cijelom tekstu\s.*?riječi?:?\s*` + izrazU + `.*?zamjenjuj[eu] se riječ(?:ju|ima):?\s*` + izrazU),
		func(m []string, _ models.CiljUpute) models.UputaIzmjene {
			return models.UputaIzmjene{Vrsta: models.UputaCijeliTekst, Stari: m[1], Novi: m[2]}
		}},
	{uzorakStupanja,
		func([]string, models.CiljUpute) models.UputaIzmjene {
			return models.UputaIzmjene{Vrsta: models.UputaStupanje}
		}},
}

// postaviRazinu upisuje brojeve u razinu cilja i briše niže razine:
// novi članak briše stavak i točku, novi stavak točku. Rimski broj točke
// je točka Plana, dio propisa kao i članak.
func postaviRazinu(c *models.CiljUpute, rijec string, brojevi []string) {
	r := strings.ToLower(rijec)
	switch {
	case strings.HasPrefix(r, "član") || (strings.HasPrefix(r, "toč") && rimskiPopis(brojevi)):
		*c = models.CiljUpute{Clanci: brojevi}
	case strings.HasPrefix(r, "stav"):
		c.Stavci, c.Tocka, c.Podtocka = brojevi, "", ""
	case strings.HasPrefix(r, "toč"):
		c.Tocka, c.Podtocka = strings.Join(brojevi, ", "), ""
	default:
		c.Podtocka = strings.Join(brojevi, ", ")
	}
}

func rimskiPopis(brojevi []string) bool {
	if len(brojevi) == 0 {
		return false
	}
	_, ok := IzRimskog(brojevi[0])
	return ok
}

// razvijPopis rastavlja popis brojeva u brojeve bez završne točke, a raspon
// s „do” razvija: „1. do 4.” je 1, 2, 3, 4, a „100.a do 100.d” je 100.a,
// 100.b, 100.c, 100.d.
func razvijPopis(s string) []string {
	var out []string
	raspon := false
	for _, t := range tokenBroja.FindAllString(s, -1) {
		if t == "do" {
			raspon = true
			continue
		}
		t = strings.TrimSuffix(t, ".")
		if raspon && len(out) > 0 {
			out = append(out, izmedju(out[len(out)-1], t)...)
		}
		out = append(out, t)
		raspon = false
	}
	return out
}
