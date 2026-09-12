// Package xlsxw piše .xlsx tablice bez vanjske knjižnice: .xlsx je zip s
// XML-om, pa je dovoljno ono što Go već ima. Piše se ono što izvoz treba —
// tekst, brojevi, formule s izračunatom vrijednosti, širine i visine, spojene
// ćelije, obrubi i ispune, logotip u zaglavlju, ispis vodoravno na jednu
// širinu — a ne ono što ne treba.
package xlsxw

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Stilovi ćelija; brojevi su redni u cellXfs
const (
	Obican         = 0  // tekst
	Podebljan      = 1  // podebljan tekst
	Broj2          = 2  // dvije decimale
	Broj2Pod       = 3  // dvije decimale, podebljano
	Naslov         = 4  // podebljano, 14
	Zaglavlje      = 5  // zaglavlje tablice: podebljano, ispuna, obrub, sredina, prelamanje
	Tablica        = 6  // ćelija tablice: obrub
	TablicaBroj    = 7  // ćelija tablice: obrub, dvije decimale
	TablicaBrojPod = 8  // zbroj: obrub, dvije decimale, podebljano, ispuna
	Napomena       = 9  // sitno, kurziv, sivo, prelamanje
	Podnaslov      = 10 // podebljano, 11
	TablicaPod     = 11 // ćelija tablice: obrub, podebljano, ispuna
	TablicaSredina = 12 // ćelija tablice: obrub, sredina
	Desno          = 13 // tekst desno
)

// Celija je jedna ćelija: tekst, broj ili formula s izračunatom vrijednosti
type Celija struct {
	Tekst   string
	Broj    float64
	JeBroj  bool
	Formula string
	Stil    int
}

// T je tekstualna ćelija
func T(s string, stil ...int) Celija { return Celija{Tekst: s, Stil: prvi(stil)} }

// N je brojčana ćelija
func N(v float64, stil ...int) Celija { return Celija{Broj: v, JeBroj: true, Stil: prvi(stil)} }

// F je formula s izračunatom vrijednosti (bez znaka =)
func F(formula string, vrijednost float64, stil ...int) Celija {
	return Celija{Formula: formula, Broj: vrijednost, JeBroj: true, Stil: prvi(stil)}
}

// FT je formula čiji je rezultat tekst, s izračunatim tekstom
func FT(formula, tekst string, stil ...int) Celija {
	return Celija{Formula: formula, Tekst: tekst, Stil: prvi(stil)}
}

func prvi(stil []int) int {
	if len(stil) > 0 {
		return stil[0]
	}
	return 0
}

// List je jedan radni list
type List struct {
	Naziv     string
	Redci     [][]Celija
	Sirine    []float64       // širine stupaca u znakovima
	visine    map[int]float64 // visine redaka u točkama
	spojene   []string        // spojena područja, "A1:D1"
	Logo      bool            // logotip knjige u gornjem lijevom kutu
	Vodoravno bool            // ispis vodoravno, cijela širina na jednu stranicu
	Podnozje  string          // podnožje ispisa; &P i &N su broj stranice i ukupno
	ponovi    [2]int          // redci zaglavlja koji se ponavljaju na svakoj stranici (1-based), 0 = nema
}

// PonoviRetke zadaje retke (0-based, uključivo) koji se pri ispisu ponavljaju
// na vrhu svake stranice
func (l *List) PonoviRetke(od, do int) { l.ponovi = [2]int{od + 1, do + 1} }

// Dodaj dodaje redak
func (l *List) Dodaj(celije ...Celija) { l.Redci = append(l.Redci, celije) }

// Redak vraća broj sljedećeg retka (0-based), za adrese
func (l *List) Redak() int { return len(l.Redci) }

// Spoji spaja ćelije od (c1,r1) do (c2,r2), 0-based
func (l *List) Spoji(c1, r1, c2, r2 int) {
	l.spojene = append(l.spojene, Adresa(c1, r1)+":"+Adresa(c2, r2))
}

// Visina zadaje visinu retka u točkama
func (l *List) Visina(r int, tocaka float64) {
	if l.visine == nil {
		l.visine = map[int]float64{}
	}
	l.visine[r] = tocaka
}

// Radna knjiga
type Knjiga struct {
	Listovi []*List
	LogoPNG []byte // logotip za listove s Logo = true
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
	logo := len(k.LogoPNG) > 0
	var ct, rels, sheets bytes.Buffer
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	for i, l := range k.Listovi {
		n := i + 1
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(l.Naziv), n, n)
		sLogom := logo && l.Logo
		if err := pisi(fmt.Sprintf("xl/worksheets/sheet%d.xml", n), l.xml(sLogom)); err != nil {
			return err
		}
		if sLogom {
			fmt.Fprintf(&ct, `<Override PartName="/xl/drawings/drawing%d.xml" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/>`, n)
			if err := pisi(fmt.Sprintf("xl/worksheets/_rels/sheet%d.xml.rels", n), `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdD" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing`+strconv.Itoa(n)+`.xml"/></Relationships>`); err != nil {
				return err
			}
			if err := pisi(fmt.Sprintf("xl/drawings/drawing%d.xml", n), crtezLogotipa(k.LogoPNG)); err != nil {
				return err
			}
			if err := pisi(fmt.Sprintf("xl/drawings/_rels/drawing%d.xml.rels", n), `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/></Relationships>`); err != nil {
				return err
			}
		}
	}
	if logo {
		f, err := z.Create("xl/media/image1.png")
		if err != nil {
			return err
		}
		if _, err := f.Write(k.LogoPNG); err != nil {
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
	var imena strings.Builder
	for i, l := range k.Listovi {
		if l.ponovi[0] > 0 {
			fmt.Fprintf(&imena, `<definedName name="_xlnm.Print_Titles" localSheetId="%d">'%s'!$%d:$%d</definedName>`, i, esc(strings.ReplaceAll(l.Naziv, "'", "''")), l.ponovi[0], l.ponovi[1])
		}
	}
	definirana := ""
	if imena.Len() > 0 {
		definirana = `<definedNames>` + imena.String() + `</definedNames>`
	}
	if err := pisi("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+sheets.String()+`</sheets>`+definirana+`<calcPr fullCalcOnLoad="1"/></workbook>`); err != nil {
		return err
	}
	if err := pisi("xl/styles.xml", stilovi); err != nil {
		return err
	}
	return z.Close()
}

// crtezLogotipa smješta sliku u gornji lijevi kut, širine oko 3,2 cm, u
// omjeru slike (iz PNG zaglavlja)
func crtezLogotipa(png []byte) string {
	cx := int64(1150000) // EMU
	cy := cx
	if len(png) >= 24 {
		w, h := binary.BigEndian.Uint32(png[16:20]), binary.BigEndian.Uint32(png[20:24])
		if w > 0 && h > 0 {
			cy = cx * int64(h) / int64(w)
		}
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<xdr:oneCellAnchor><xdr:from><xdr:col>0</xdr:col><xdr:colOff>60000</xdr:colOff><xdr:row>0</xdr:row><xdr:rowOff>60000</xdr:rowOff></xdr:from>` +
		fmt.Sprintf(`<xdr:ext cx="%d" cy="%d"/>`, cx, cy) +
		`<xdr:pic><xdr:nvPicPr><xdr:cNvPr id="2" name="Logotip"/><xdr:cNvPicPr><a:picLocks noChangeAspect="1"/></xdr:cNvPicPr></xdr:nvPicPr>` +
		`<xdr:blipFill><a:blip r:embed="rIdL"/><a:stretch><a:fillRect/></a:stretch></xdr:blipFill>` +
		fmt.Sprintf(`<xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></xdr:spPr></xdr:pic><xdr:clientData/></xdr:oneCellAnchor></xdr:wsDr>`, cx, cy)
}

// stilovi, redom kao konstante gore
const stilovi = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts>` +
	`<fonts count="5">` +
	`<font><sz val="10"/><name val="Arial"/></font>` +
	`<font><b/><sz val="10"/><name val="Arial"/></font>` +
	`<font><b/><sz val="14"/><color rgb="FF173E74"/><name val="Arial"/></font>` +
	`<font><i/><sz val="8"/><color rgb="FF666666"/><name val="Arial"/></font>` +
	`<font><b/><sz val="11"/><name val="Arial"/></font>` +
	`</fonts>` +
	`<fills count="4"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFDCE6F2"/><bgColor indexed="64"/></patternFill></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFF2F2F2"/><bgColor indexed="64"/></patternFill></fill></fills>` +
	`<borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border>` +
	`<border><left style="thin"><color rgb="FF999999"/></left><right style="thin"><color rgb="FF999999"/></right><top style="thin"><color rgb="FF999999"/></top><bottom style="thin"><color rgb="FF999999"/></bottom><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="14">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="center"/></xf>` + // 0
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment vertical="center"/></xf>` + // 1
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` + // 2
	`<xf numFmtId="164" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1"/>` + // 3
	`<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment vertical="center"/></xf>` + // 4
	`<xf numFmtId="0" fontId="1" fillId="2" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf>` + // 5
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1" applyAlignment="1"><alignment vertical="center"/></xf>` + // 6
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="1" xfId="0" applyNumberFormat="1" applyBorder="1" applyAlignment="1"><alignment vertical="center"/></xf>` + // 7
	`<xf numFmtId="164" fontId="1" fillId="3" borderId="1" xfId="0" applyNumberFormat="1" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment vertical="center"/></xf>` + // 8
	`<xf numFmtId="0" fontId="3" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf>` + // 9
	`<xf numFmtId="0" fontId="4" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment vertical="center"/></xf>` + // 10
	`<xf numFmtId="0" fontId="1" fillId="3" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment vertical="center"/></xf>` + // 11
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>` + // 12
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment horizontal="right" vertical="center"/></xf>` + // 13
	`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`

func (l *List) xml(logo bool) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	if l.Vodoravno {
		b.WriteString(`<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr>`)
	}
	b.WriteString(`<sheetViews><sheetView workbookViewId="0" showGridLines="0"/></sheetViews>`)
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
		if h, ok := l.visine[r]; ok {
			fmt.Fprintf(&b, `<row r="%d" ht="%s" customHeight="1">`, r+1, strconv.FormatFloat(h, 'f', 1, 64))
		} else {
			fmt.Fprintf(&b, `<row r="%d">`, r+1)
		}
		for c, cel := range redak {
			if cel.Tekst == "" && !cel.JeBroj && cel.Formula == "" && cel.Stil == 0 {
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
			case cel.Tekst == "":
				fmt.Fprintf(&b, `<c r="%s" s="%d"/>`, adresa, cel.Stil)
			default:
				fmt.Fprintf(&b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, adresa, cel.Stil, esc(cel.Tekst))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if len(l.spojene) > 0 {
		fmt.Fprintf(&b, `<mergeCells count="%d">`, len(l.spojene))
		for _, s := range l.spojene {
			fmt.Fprintf(&b, `<mergeCell ref="%s"/>`, s)
		}
		b.WriteString(`</mergeCells>`)
	}
	b.WriteString(`<printOptions horizontalCentered="1"/><pageMargins left="0.5" right="0.5" top="0.6" bottom="0.6" header="0.3" footer="0.3"/>`)
	if l.Vodoravno {
		b.WriteString(`<pageSetup paperSize="9" orientation="landscape" fitToWidth="1" fitToHeight="0"/>`)
	} else {
		b.WriteString(`<pageSetup paperSize="9" orientation="portrait"/>`)
	}
	if l.Podnozje != "" {
		fmt.Fprintf(&b, `<headerFooter><oddFooter>%s</oddFooter></headerFooter>`, esc(l.Podnozje))
	}
	if logo {
		b.WriteString(`<drawing r:id="rIdD"/>`)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

func broj(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
