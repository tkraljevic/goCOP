// Paket docx sastavlja Wordov dokument iz standardne biblioteke. Datoteka
// .docx je ZIP s nekoliko XML-ova, pa se za to ne uvodi vanjska ovisnost:
// izvješće mora raditi i na čvoru koji nikad nije vidio internet, a jedna
// knjižnica manje je jedna stvar manje koja može zastarjeti.
//
// Ovdje je samo ono što izvješće treba: naslovi, odlomci, natuknice i
// tablice. Namjerno nema slika ni zaglavlja — dokument se otvara u Wordu i
// dalje uređuje rukom, pa ne mora izgledati dovršeno nego biti točan.
package docx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// Dokument je izvješće u nastajanju.
type Dokument struct {
	tijelo strings.Builder
	naslov string
	autor  string
	kad    time.Time
}

// Novi otvara dokument. Naslov i autor idu u svojstva datoteke.
func Novi(naslov, autor string, kad time.Time) *Dokument {
	return &Dokument{naslov: naslov, autor: autor, kad: kad}
}

func (d *Dokument) odlomak(stil, tekst string) {
	fmt.Fprintf(&d.tijelo, `<w:p><w:pPr><w:pStyle w:val="%s"/></w:pPr>%s</w:p>`, stil, tekstXML(tekst))
}

// Naslov je naslov dokumenta.
func (d *Dokument) Naslov(t string) { d.odlomak("Naslov", t) }

// Podnaslov stoji odmah ispod naslova.
func (d *Dokument) Podnaslov(t string) { d.odlomak("Podnaslov", t) }

// Poglavlje je naslov prve razine.
func (d *Dokument) Poglavlje(t string) { d.odlomak("Naslov1", t) }

// Odjeljak je naslov druge razine.
func (d *Dokument) Odjeljak(t string) { d.odlomak("Naslov2", t) }

// Odlomak je obični tekst.
func (d *Dokument) Odlomak(t string) { d.odlomak("Normal", t) }

// Napomena je sitniji tekst u kurzivu: ograde, podrijetlo, način računanja.
// Bez toga izvješće tvrdi više nego što zna.
func (d *Dokument) Napomena(t string) { d.odlomak("Napomena", t) }

// Stavka je natuknica popisa.
func (d *Dokument) Stavka(t string) {
	fmt.Fprintf(&d.tijelo,
		`<w:p><w:pPr><w:pStyle w:val="Normal"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr>`+
			`<w:spacing w:before="0" w:after="40"/></w:pPr>%s</w:p>`, tekstXML(t))
}

// Par je redak „naziv: vrijednost", kakvih je u izvješću najviše.
func (d *Dokument) Par(naziv, vrijednost string) {
	if strings.TrimSpace(vrijednost) == "" {
		return // prazan podatak se ne ispisuje; prazan redak tvrdi da je mjeren
	}
	fmt.Fprintf(&d.tijelo, `<w:p><w:pPr><w:pStyle w:val="Normal"/><w:spacing w:before="0" w:after="40"/></w:pPr>`+
		`<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">%s: </w:t></w:r>`+
		`<w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, escape(naziv), escape(vrijednost))
}

// Tablica ispisuje zaglavlje i retke. Prazna tablica se preskače: prazan
// okvir u izvješću tvrdi da podataka ima, a nema ih.
func (d *Dokument) Tablica(glave []string, redci [][]string) {
	if len(glave) == 0 || len(redci) == 0 {
		return
	}
	d.tijelo.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/>` +
		`<w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`<w:left w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`<w:bottom w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`<w:right w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`<w:insideH w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`<w:insideV w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`</w:tblBorders></w:tblPr>`)
	d.redak(glave, true)
	for _, r := range redci {
		d.redak(r, false)
	}
	d.tijelo.WriteString(`</w:tbl><w:p><w:pPr><w:spacing w:before="0" w:after="120"/></w:pPr></w:p>`)
}

func (d *Dokument) redak(celije []string, glava bool) {
	d.tijelo.WriteString(`<w:tr>`)
	if glava {
		// Zaglavlje se ponavlja na svakoj stranici; tablica valova ide preko
		// više njih, pa bi se inače brzo izgubilo koji je stupac koji.
		d.tijelo.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
	}
	for _, c := range celije {
		d.tijelo.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/>`)
		if glava {
			d.tijelo.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="EFEFEF"/>`)
		}
		d.tijelo.WriteString(`</w:tcPr>`)
		if glava {
			fmt.Fprintf(&d.tijelo, `<w:p><w:pPr><w:pStyle w:val="Celija"/></w:pPr>`+
				`<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, escape(c))
		} else {
			fmt.Fprintf(&d.tijelo, `<w:p><w:pPr><w:pStyle w:val="Celija"/></w:pPr>%s</w:p>`, tekstXML(c))
		}
		d.tijelo.WriteString(`</w:tc>`)
	}
	d.tijelo.WriteString(`</w:tr>`)
}

// PrijelomStranice počinje novu stranicu.
func (d *Dokument) PrijelomStranice() {
	d.tijelo.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
}

// tekstXML pretvara tekst u niz Wordovih ulomaka, čuvajući prelome retka.
func tekstXML(t string) string {
	if t == "" {
		return `<w:r><w:t xml:space="preserve"></w:t></w:r>`
	}
	var b strings.Builder
	for i, red := range strings.Split(t, "\n") {
		if i > 0 {
			b.WriteString(`<w:r><w:br/></w:r>`)
		}
		fmt.Fprintf(&b, `<w:r><w:t xml:space="preserve">%s</w:t></w:r>`, escape(red))
	}
	return b.String()
}

func escape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return ""
	}
	return b.String()
}

// Zapisi sastavlja .docx i zapisuje ga.
func (d *Dokument) Zapisi(w io.Writer) error {
	z := zip.NewWriter(w)
	for _, dio := range []struct{ ime, sadrzaj string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"docProps/core.xml", coreXML(d.naslov, d.autor, d.kad)},
		{"word/_rels/document.xml.rels", docRels},
		{"word/styles.xml", stilovi},
		{"word/numbering.xml", numeriranje},
		{"word/document.xml", documentXML(d.tijelo.String())},
	} {
		f, err := z.Create(dio.ime)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, dio.sadrzaj); err != nil {
			return err
		}
	}
	return z.Close()
}
