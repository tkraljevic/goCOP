// Package xlsxw piše .xlsx tablice bez vanjske knjižnice: .xlsx je zip s
// XML-om, pa je dovoljno ono što Go već ima. Piše se samo ono što izvoz
// treba — tekst, brojevi, formule, širine stupaca, podebljano i decimale —
// a formule dobivaju i izračunatu vrijednost, da tablica pokazuje brojke i
// u programu koji je ne preračuna pri otvaranju.
package xlsxw

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Stil ćelije; 0 je običan
const (
	Obican    = 0
	Podebljan = 1
	Broj2     = 2 // dvije decimale
	Broj2Pod  = 3 // dvije decimale, podebljano
	Naslov    = 4 // podebljano, veće
)

// Celija je jedna ćelija: tekst, broj ili formula s izračunatom vrijednosti
type Celija struct {
	Tekst   string
	Broj    float64
	JeBroj  bool
	Formula string
	Stil    int
}

// FT je formula čiji je rezultat tekst, s izračunatim tekstom
func FT(formula, tekst string, stil ...int) Celija {
	return Celija{Formula: formula, Tekst: tekst, Stil: prvi(stil)}
}

// T je tekstualna ćelija
func T(s string, stil ...int) Celija { return Celija{Tekst: s, Stil: prvi(stil)} }

// N je brojčana ćelija
func N(v float64, stil ...int) Celija { return Celija{Broj: v, JeBroj: true, Stil: prvi(stil)} }

// F je formula s izračunatom vrijednosti (bez znaka =)
func F(formula string, vrijednost float64, stil ...int) Celija {
	return Celija{Formula: formula, Broj: vrijednost, JeBroj: true, Stil: prvi(stil)}
}

func prvi(stil []int) int {
	if len(stil) > 0 {
		return stil[0]
	}
	return 0
}

// List je jedan radni list: redci ćelija i širine stupaca u znakovima
type List struct {
	Naziv  string
	Redci  [][]Celija
	Sirine []float64
}

// Dodaj dodaje redak
func (l *List) Dodaj(celije ...Celija) { l.Redci = append(l.Redci, celije) }

// Radna knjiga
type Knjiga struct {
	Listovi []*List
}

// NoviList otvara list; naziv se krati na 31 znak i čisti od znakova koje
// Excel ne dopušta u nazivu lista
func (k *Knjiga) NoviList(naziv string) *List {
	zamjene := strings.NewReplacer("/", "-", "\\", "-", "?", "", "*", "", "[", "(", "]", ")", ":", "-")
	naziv = zamjene.Replace(naziv)
	if r := []rune(naziv); len(r) > 31 {
		naziv = string(r[:31])
	}
	l := &List{Naziv: naziv}
	k.Listovi = append(k.Listovi, l)
	return l
}

// Adresa vraća oznaku ćelije: (0,0) → A1
func Adresa(stupac, redak int) string { return Stupac(stupac) + strconv.Itoa(redak+1) }

// Stupac vraća slovnu oznaku stupca: 0 → A, 26 → AA
func Stupac(n int) string {
	s := ""
	for n >= 0 {
		s = string(rune('A'+n%26)) + s
		n = n/26 - 1
	}
	return s
}

// Zapisi piše knjigu kao .xlsx
func (k *Knjiga) Zapisi(w io.Writer) error {
	z := zip.NewWriter(w)
	pisi := func(ime, sadrzaj string) error {
		f, err := z.Create(ime)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, sadrzaj)
		return err
	}
	var ct, rels, sheets bytes.Buffer
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	for i, l := range k.Listovi {
		n := i + 1
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(l.Naziv), n, n)
		if err := pisi(fmt.Sprintf("xl/worksheets/sheet%d.xml", n), l.xml()); err != nil {
			return err
		}
	}
	ct.WriteString(`</Types>`)
	rels.WriteString(`</Relationships>`)
	if err := pisi("[Content_Types].xml", ct.String()); err != nil {
		return err
	}
	if err := pisi("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`); err != nil {
		return err
	}
	if err := pisi("xl/_rels/workbook.xml.rels", rels.String()); err != nil {
		return err
	}
	// fullCalcOnLoad: Excel preračuna formule pri otvaranju, pa satnica koju
	// računovodstvo upiše odmah daje iznose
	if err := pisi("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+sheets.String()+`</sheets><calcPr fullCalcOnLoad="1"/></workbook>`); err != nil {
		return err
	}
	if err := pisi("xl/styles.xml", stilovi); err != nil {
		return err
	}
	return z.Close()
}

// stilovi: 0 običan, 1 podebljan, 2 dvije decimale, 3 dvije decimale
// podebljano, 4 naslov
const stilovi = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts>` +
	`<fonts count="3"><font><sz val="10"/><name val="Arial"/></font><font><b/><sz val="10"/><name val="Arial"/></font><font><b/><sz val="12"/><name val="Arial"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="5">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment wrapText="1" vertical="top"/></xf>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment wrapText="1" vertical="top"/></xf>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="164" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`

func (l *List) xml() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	if len(l.Sirine) > 0 {
		b.WriteString(`<cols>`)
		for i, s := range l.Sirine {
			if s > 0 {
				fmt.Fprintf(&b, `<col min="%d" max="%d" width="%s" customWidth="1"/>`, i+1, i+1, strconv.FormatFloat(s, 'f', 2, 64))
			}
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	for r, redak := range l.Redci {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, cel := range redak {
			if cel.Tekst == "" && !cel.JeBroj && cel.Formula == "" {
				continue
			}
			adresa := Adresa(c, r)
			switch {
			case cel.Formula != "" && !cel.JeBroj:
				fmt.Fprintf(&b, `<c r="%s" s="%d" t="str"><f>%s</f><v>%s</v></c>`, adresa, cel.Stil, esc(cel.Formula), esc(cel.Tekst))
			case cel.Formula != "":
				fmt.Fprintf(&b, `<c r="%s" s="%d"><f>%s</f><v>%s</v></c>`, adresa, cel.Stil, esc(cel.Formula), broj(cel.Broj))
			case cel.JeBroj:
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%s</v></c>`, adresa, cel.Stil, broj(cel.Broj))
			default:
				fmt.Fprintf(&b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, adresa, cel.Stil, esc(cel.Tekst))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func broj(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
