package propisi

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"gocop/internal/models"
)

// ErrNemaTijelaAkta: stranica nema stupca s tekstom akta (div.article-column).
// Tako izgleda stranica NN koja nije akt, npr. obavijest da akta nema.
var ErrNemaTijelaAkta = errors.New("na stranici nema teksta akta")

// Odlomak je jedan odlomak teksta akta: klasa iz NN (malim slovima, bez
// šuma) i očišćen tekst, ili tablica. Odlomci zalijepljenog teksta nemaju
// klasu.
type Odlomak struct {
	Klasa, Tekst string
	Tablica      *models.TablicaPropisa

	navod bool // redak bloka u navodnicima (postavlja Podijeli)
}

// Procitano je ono što je čitač našao na stranici akta ili u tekstu.
type Procitano struct {
	Naziv, Vrsta, Donositelj, ELI, DatumObjave string
	NN                                         models.IzdanjeNN
	Veze                                       []models.VezaELI
	Odlomci                                    []Odlomak
}

// ProcitajHTML čita stranicu akta s narodne-novine.nn.hr: metapodatke iz
// zaglavlja i desnog stupca te odlomke iz stupca s tekstom akta, redom kako
// stoje, i kad su umotani u druge okvire. Stranica bez tog stupca daje
// ErrNemaTijelaAkta.
func ProcitajHTML(r io.Reader) (*Procitano, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	tijelo := nadjiCvor(doc, func(n *html.Node) bool {
		return n.DataAtom == atom.Div && imaKlasu(n, "article-column")
	})
	if tijelo == nil {
		return nil, ErrNemaTijelaAkta
	}
	p := &Procitano{}
	procitajMeta(doc, p)
	var s sakupljac
	s.hodaj(tijelo)
	p.Odlomci = s.odlomci
	return p, nil
}

// sakupljac skuplja odlomke i tablice tijela akta redom.
type sakupljac struct{ odlomci []Odlomak }

func (s *sakupljac) hodaj(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.DataAtom {
		case atom.P:
			s.odlomak(c)
		case atom.Table:
			s.tablica(c)
		case atom.Style, atom.Script:
		default:
			s.hodaj(c)
		}
	}
}

// odlomak dodaje tekst odlomka. Tablica upisana unutar odlomka postaje
// zaseban odlomak, a tekst oko nje ostaje na svom mjestu.
func (s *sakupljac) odlomak(p *html.Node) {
	klasa := klasaOdlomka(atribut(p, "class"))
	if klasa == "bezreda" {
		return
	}
	var b strings.Builder
	for c := p.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Table {
			s.dodaj(klasa, b.String())
			b.Reset()
			s.tablica(c)
			continue
		}
		pisiTekst(&b, c)
	}
	s.dodaj(klasa, b.String())
}

func (s *sakupljac) dodaj(klasa, tekst string) {
	if t := ocisti(tekst); t != "" {
		s.odlomci = append(s.odlomci, Odlomak{Klasa: klasa, Tekst: t})
	}
}

// tablica dodaje tablicu kao odlomak s ćelijama. Tablica bez redaka se
// preskače.
func (s *sakupljac) tablica(t *html.Node) {
	var retci [][]models.CelijaTablice
	hodajRetke(t, func(tr *html.Node) {
		var redak []models.CelijaTablice
		for c := tr.FirstChild; c != nil; c = c.NextSibling {
			if c.DataAtom == atom.Td || c.DataAtom == atom.Th {
				redak = append(redak, celija(c))
			}
		}
		if len(redak) > 0 {
			retci = append(retci, redak)
		}
	})
	if len(retci) == 0 {
		return
	}
	s.odlomci = append(s.odlomci, Odlomak{
		Klasa:   klasaOdlomka(atribut(t, "class")),
		Tablica: &models.TablicaPropisa{Retci: retci},
	})
}

// hodajRetke zove f za svaki redak tablice, i u thead, tbody i tfoot, ali
// ne i za retke tablice umetnute u ćeliju.
func hodajRetke(n *html.Node, f func(*html.Node)) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.DataAtom {
		case atom.Tr:
			f(c)
		case atom.Thead, atom.Tbody, atom.Tfoot:
			hodajRetke(c, f)
		}
	}
}

// celija čita tekst ćelije, raspon i je li podebljana.
func celija(td *html.Node) models.CelijaTablice {
	var b strings.Builder
	for c := td.FirstChild; c != nil; c = c.NextSibling {
		pisiTekst(&b, c)
	}
	return models.CelijaTablice{
		Tekst:      ocisti(b.String()),
		Redaka:     rasponCelije(atribut(td, "rowspan")),
		Stupaca:    rasponCelije(atribut(td, "colspan")),
		Podebljano: td.DataAtom == atom.Th || nadjiCvor(td, podebljan) != nil,
	}
}

func podebljan(n *html.Node) bool {
	return n.DataAtom == atom.B || n.DataAtom == atom.Strong || imaKlasu(n, "bold")
}

// rasponCelije je rowspan ili colspan veći od 1; inače 0, pa se u zapisu ne piše.
func rasponCelije(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 2 {
		return 0
	}
	return n
}

// pisiTekst piše tekst čvora: <br> je novi redak, <sup> s brojem je
// eksponent, a prijelom retka u izvoru stranice samo razmak.
func pisiTekst(b *strings.Builder, n *html.Node) {
	switch {
	case n.Type == html.TextNode:
		b.WriteString(strings.NewReplacer("\r", " ", "\n", " ").Replace(n.Data))
	case n.Type != html.ElementNode, n.DataAtom == atom.Style, n.DataAtom == atom.Script:
	case n.DataAtom == atom.Br:
		b.WriteByte('\n')
	case n.DataAtom == atom.Sup:
		var u strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			pisiTekst(&u, c)
		}
		b.WriteString(nadznak(u.String()))
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			pisiTekst(b, c)
		}
	}
}

// nadjiCvor vraća prvi element (redom kroz dokument) za koji vrijedi uvjet.
func nadjiCvor(n *html.Node, uvjet func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode && uvjet(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if nadjen := nadjiCvor(c, uvjet); nadjen != nil {
			return nadjen
		}
	}
	return nil
}

// hodajSve zove f za svaki element dokumenta.
func hodajSve(n *html.Node, f func(*html.Node)) {
	if n.Type == html.ElementNode {
		f(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		hodajSve(c, f)
	}
}

func atribut(n *html.Node, ime string) string {
	for _, a := range n.Attr {
		if a.Key == ime {
			return a.Val
		}
	}
	return ""
}

// imaKlasu javlja ima li element klasu, bez razlike velikih i malih slova.
func imaKlasu(n *html.Node, klasa string) bool {
	for _, k := range strings.Fields(atribut(n, "class")) {
		if strings.EqualFold(k, klasa) {
			return true
		}
	}
	return false
}

// tekstCvora je očišćen tekst cijelog čvora.
func tekstCvora(n *html.Node) string {
	var b strings.Builder
	pisiTekst(&b, n)
	return ocisti(b.String())
}
