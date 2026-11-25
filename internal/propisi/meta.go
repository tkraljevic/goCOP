package propisi

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"gocop/internal/models"
)

// ontologijaELI je početak svojstava ELI u meta oznakama stranice NN.
const ontologijaELI = "http://data.europa.eu/eli/ontology#"

// vrsteVeza su veze s drugim aktima koje čitač pamti. Ostale (is_about,
// basis_for, transposes…) program ne koristi.
var vrsteVeza = map[string]bool{
	"amended_by": true, "changed_by": true, "amends": true, "changes": true,
	"corrected_by": true, "repeals": true, "based_on": true,
}

var (
	oblikIzdanja = regexp.MustCompile(`NN\s*(\d{1,3})\s*/\s*(\d{4})`)
	datumTiska   = regexp.MustCompile(`^(\d{1,2})\.\s*(\d{1,2})\.\s*(\d{4})\.?$`)
	datumELI     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// meta skuplja metapodatke prije nego što se slože u Procitano.
type meta struct {
	p                             *Procitano
	datumObjave, tisk, eliTablice string
	imaELI                        bool // stranica ima ELI meta oznake djela
	vidjeno                       map[models.VezaELI]bool
}

// procitajMeta čita naziv iz naslova stranice (#sticky.title > h2), podatke
// iz tablice uz akt (table.detailsTable) i datum objave i veze iz ELI meta
// oznaka. JSON-LD se ne čita: u njemu su veze pokvarene („https:/…”).
// ELI akta pamti se samo kad stranica ima ELI meta oznake: bez njih Narodne
// novine o aktu nemaju podatke o izmjenama (stariji akti), iako tablica uz
// akt navodi ELI adresu.
func procitajMeta(doc *html.Node, p *Procitano) {
	m := &meta{p: p, vidjeno: map[models.VezaELI]bool{}}
	hodajSve(doc, func(n *html.Node) {
		switch {
		case n.DataAtom == atom.Meta:
			m.metaELI(n)
		case n.DataAtom == atom.Div && atribut(n, "id") == "sticky":
			if h2 := nadjiCvor(n, func(c *html.Node) bool { return c.DataAtom == atom.H2 }); h2 != nil {
				p.Naziv = tekstCvora(h2)
			}
		case n.DataAtom == atom.Td:
			m.redakDetalja(n)
		}
	})
	p.DatumObjave = m.datumObjave
	if p.DatumObjave == "" {
		p.DatumObjave = m.tisk
	}
	if m.imaELI {
		p.ELI = m.eliTablice
	}
	if p.NN == (models.IzdanjeNN{}) {
		p.NN, _ = IzdanjeIzELI(m.eliTablice) // bez ELI-ja izdanje ostaje prazno
	}
}

// metaELI čita jednu ELI meta oznaku: datum objave i veze djela s drugim
// aktima. Veze se uzimaju samo s djela, ne s izraza (/hrv) ni oblika.
func (m *meta) metaELI(n *html.Node) {
	svojstvo, ok := strings.CutPrefix(atribut(n, "property"), ontologijaELI)
	if !ok || strings.Contains(atribut(n, "about"), "/hrv") {
		return
	}
	m.imaELI = true
	if svojstvo == "date_publication" && datumELI.MatchString(atribut(n, "content")) {
		m.datumObjave = atribut(n, "content")
		return
	}
	put := putELI(atribut(n, "resource"))
	v := models.VezaELI{Vrsta: svojstvo, ELI: put}
	if !vrsteVeza[svojstvo] || put == "" || m.vidjeno[v] {
		return
	}
	m.vidjeno[v] = true
	m.p.Veze = append(m.p.Veze, v)
}

// redakDetalja čita redak tablice uz akt: „<span class='key'>Izdanje:</span>
// NN 84/2021”.
func (m *meta) redakDetalja(td *html.Node) {
	kljuc := nadjiCvor(td, func(c *html.Node) bool { return c.DataAtom == atom.Span && imaKlasu(c, "key") })
	if kljuc == nil {
		return
	}
	var b strings.Builder
	for c := td.FirstChild; c != nil; c = c.NextSibling {
		if c != kljuc {
			pisiTekst(&b, c)
		}
	}
	if upisi := poljaDetalja[strings.TrimSuffix(tekstCvora(kljuc), ":")]; upisi != nil {
		upisi(m, ocisti(b.String()))
	}
}

// poljaDetalja su retci tablice uz akt koje čitač pamti, po ključu retka.
var poljaDetalja = map[string]func(m *meta, v string){
	"Vrsta dokumenta": func(m *meta, v string) { m.p.Vrsta = v },
	"Izdanje": func(m *meta, v string) {
		if g := oblikIzdanja.FindStringSubmatch(v); g != nil {
			m.p.NN.Broj, m.p.NN.Godina = broj(g[1]), broj(g[2])
		}
	},
	"Broj dokumenta u izdanju": func(m *meta, v string) {
		m.p.NN.Akt, _ = strconv.Atoi(v) // nebroj ostavlja 0
	},
	"Donositelj":             func(m *meta, v string) { m.p.Donositelj = v },
	"Datum tiskanog izdanja": func(m *meta, v string) { m.tisk = datumIzTiska(v) },
	"ELI":                    func(m *meta, v string) { m.eliTablice = putELI(v) },
}

// datumIzTiska pretvara „7.7.2010.” u „2010-07-07”; drugo daje prazno.
func datumIzTiska(s string) string {
	g := datumTiska.FindStringSubmatch(s)
	if g == nil {
		return ""
	}
	d, _ := strconv.Atoi(g[1])  // uzorak jamči znamenke
	mj, _ := strconv.Atoi(g[2]) // uzorak jamči znamenke
	return fmt.Sprintf("%s-%02d-%02d", g[3], mj, d)
}

// putELI ostavlja od ELI adrese samo put („/eli/sluzbeni/2021/84/1560”), i
// kad je adresa puna, s poslužiteljem NN. Drugo daje prazno.
func putELI(s string) string {
	s = strings.TrimSpace(s)
	for _, pocetak := range []string{"https://" + PosluziteljNN, "http://" + PosluziteljNN} {
		s = strings.TrimPrefix(s, pocetak)
	}
	if !strings.HasPrefix(s, "/eli/") {
		return ""
	}
	return strings.TrimSuffix(s, "/")
}
