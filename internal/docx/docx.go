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
	slike  []slika

	zaglavlje *Zaglavlje // memorandum na prvoj stranici; nil kad ga nema
	znak      *znakSlike // znak ustanove u zaglavlju
}

// ImaZaglavlje javlja nosi li dokument memorandum.
func (d *Dokument) ImaZaglavlje() bool { return d.zaglavlje != nil }

// mediji su sve slike u paketu: one iz tijela i znak iz zaglavlja.
func (d *Dokument) mediji() []slika {
	m := append([]slika(nil), d.slike...)
	if d.znak != nil {
		m = append(m, d.znak.slika)
	}
	return m
}

// slika je PNG ugrađen u dokument. Word sliku ne nosi u tijelu nego kao
// zaseban dio paketa, na koji se tijelo poziva vezom — zato se skupljaju
// ovdje i zapisuju tek pri sastavljanju.
type slika struct {
	ime     string
	sadrzaj []byte
}

// Najveća širina slike u dokumentu: A4 bez rubova, izraženo u EMU
// (914.400 po palcu), koliko OOXML traži za veličine crteža.
const najvecaSirinaEMU = 5943600

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

// Slika ugrađuje PNG u dokument, razmjerno smanjen da stane preko širine
// stranice. Veličina se zadaje u slikovnim točkama izvorne slike.
func (d *Dokument) Slika(png []byte, sirina, visina int, opis string) {
	if len(png) == 0 || sirina <= 0 || visina <= 0 {
		return
	}
	ime := fmt.Sprintf("slika%d.png", len(d.slike)+1)
	d.slike = append(d.slike, slika{ime: ime, sadrzaj: png})
	id := len(d.slike)

	// 96 točaka po palcu je ono što Word pretpostavlja za PNG bez zapisane
	// gustoće; preko toga se razmjerno smanjuje da stane u širinu stranice.
	sirinaEMU := int64(sirina) * 9525
	visinaEMU := int64(visina) * 9525
	if sirinaEMU > najvecaSirinaEMU {
		visinaEMU = visinaEMU * najvecaSirinaEMU / sirinaEMU
		sirinaEMU = najvecaSirinaEMU
	}

	fmt.Fprintf(&d.tijelo, `<w:p><w:pPr><w:spacing w:before="60" w:after="60"/></w:pPr><w:r><w:drawing>`+
		`<wp:inline distT="0" distB="0" distL="0" distR="0" `+
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">`+
		`<wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="%s" descr="%s"/>`+
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">`+
		`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="rIdSlika%d" `+
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"/>`+
		`<a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`,
		sirinaEMU, visinaEMU, id, ime, escape(opis), id, ime, id, sirinaEMU, visinaEMU)
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
		{"[Content_Types].xml", contentTypesSa(d.mediji(), d.ImaZaglavlje())},
		{"_rels/.rels", rels},
		{"docProps/core.xml", coreXML(d.naslov, d.autor, d.kad)},
		{"word/_rels/document.xml.rels", docRelsSa(d.slike, d.ImaZaglavlje())},
		{"word/styles.xml", stilovi},
		{"word/numbering.xml", numeriranje},
		{"word/document.xml", documentXML(d.tijelo.String(), d.ImaZaglavlje())},
	} {
		f, err := z.Create(dio.ime)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, dio.sadrzaj); err != nil {
			return err
		}
	}
	if d.ImaZaglavlje() {
		for _, dio := range []struct{ ime, sadrzaj string }{
			{"word/header1.xml", d.zaglavljeDio()},
			{"word/_rels/header1.xml.rels", d.zaglavljeVeze()},
		} {
			f, err := z.Create(dio.ime)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(f, dio.sadrzaj); err != nil {
				return err
			}
		}
	}
	for _, sl := range d.mediji() {
		f, err := z.Create("word/media/" + sl.ime)
		if err != nil {
			return err
		}
		if _, err := f.Write(sl.sadrzaj); err != nil {
			return err
		}
	}
	return z.Close()
}
