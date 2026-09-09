package docx

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Nepromjenjivi dijelovi .docx paketa. Svaki je propisan standardom OOXML;
// Word odbija otvoriti datoteku kojoj ijedan nedostaje ili mu se vrsta ne
// poklapa s onim što je unutra.

const zaglavljeXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// contentTypesSa prijavljuje vrste slika koje dokument nosi i, ako ga ima,
// dio sa zaglavljem. Bez toga Word javlja da je datoteka oštećena, bez
// naznake koji dio nedostaje.
func contentTypesSa(mediji []slika, imaZaglavlje bool) string {
	s := contentTypes
	var dodatak strings.Builder
	for _, nastavak := range vrsteSlika(mediji) {
		fmt.Fprintf(&dodatak, "\n"+`<Default Extension="%s" ContentType="image/%s"/>`, nastavak, nastavak)
	}
	if dodatak.Len() > 0 {
		s = strings.Replace(s,
			`<Default Extension="xml" ContentType="application/xml"/>`,
			`<Default Extension="xml" ContentType="application/xml"/>`+dodatak.String(), 1)
	}
	if imaZaglavlje {
		s = strings.Replace(s, "</Types>",
			`<Override PartName="/word/header1.xml" `+
				`ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>`+
				"\n</Types>", 1)
	}
	return s
}

// vrsteSlika vraća nastavke koji se u paketu pojavljuju, svaki jednom i uvijek
// istim redom — dva ista Default zapisa Word odbija.
func vrsteSlika(mediji []slika) []string {
	var vrste []string
	vidjeno := map[string]bool{}
	for _, m := range mediji {
		i := strings.LastIndex(m.ime, ".")
		if i < 0 {
			continue
		}
		n := m.ime[i+1:]
		if !vidjeno[n] {
			vidjeno[n] = true
			vrste = append(vrste, n)
		}
	}
	sort.Strings(vrste)
	return vrste
}

// docRelsSa dodaje vezu na svaku sliku; tijelo se na njih poziva po r:embed.
func docRelsSa(slike []slika, imaZaglavlje bool) string {
	if len(slike) == 0 && !imaZaglavlje {
		return docRels
	}
	var b strings.Builder
	if imaZaglavlje {
		b.WriteString(`<Relationship Id="rIdZaglavlje" ` +
			`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" ` +
			`Target="header1.xml"/>`)
	}
	for i, sl := range slike {
		fmt.Fprintf(&b, `<Relationship Id="rIdSlika%d" `+
			`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" `+
			`Target="media/%s"/>`, i+1, sl.ime)
	}
	return strings.Replace(docRels, "</Relationships>", b.String()+"</Relationships>", 1)
}

const contentTypes = zaglavljeXML + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`

const rels = zaglavljeXML + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`

const docRels = zaglavljeXML + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
</Relationships>`

// coreXML su svojstva datoteke: naslov, autor i vrijeme nastanka. Bez njih se
// dokument otvara, ali u popisu datoteka nema ni naslova ni datuma.
func coreXML(naslov, autor string, kad time.Time) string {
	t := kad.UTC().Format("2006-01-02T15:04:05Z")
	return zaglavljeXML + fmt.Sprintf(`<cp:coreProperties `+
		`xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" `+
		`xmlns:dc="http://purl.org/dc/elements/1.1/" `+
		`xmlns:dcterms="http://purl.org/dc/terms/" `+
		`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`+
		`<dc:title>%s</dc:title><dc:creator>%s</dc:creator>`+
		`<cp:lastModifiedBy>%s</cp:lastModifiedBy>`+
		`<dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created>`+
		`<dcterms:modified xsi:type="dcterms:W3CDTF">%s</dcterms:modified>`+
		`</cp:coreProperties>`, escape(naslov), escape(autor), escape(autor), t, t)
}

// documentXML omata tijelo i zatvara ga opisom stranice: A4 uspravno, rubovi
// oko dva centimetra. Tablica valova ima pet stupaca i uz šire rubove se lomi.
func documentXML(tijelo string, imaZaglavlje bool) string {
	// Bez zaglavlja rub je uzak koliko treba tekstu. Sa zaglavljem se gornji
	// rub spušta na mjeru s obrasca: memorandum je visok pola palca i inače bi
	// ušao u prvi redak.
	sekcija := `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="708" w:footer="708" w:gutter="0"/>` +
		`</w:sectPr>`
	if imaZaglavlje {
		sekcija = `<w:sectPr><w:headerReference w:type="first" r:id="rIdZaglavlje"/>` +
			`<w:pgSz w:w="11906" w:h="16838"/>` +
			`<w:pgMar w:top="1417" w:right="1417" w:bottom="1417" w:left="1417" w:header="567" w:footer="708" w:gutter="0"/>` +
			`<w:titlePg/></w:sectPr>`
	}
	return zaglavljeXML + `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<w:body>` + tijelo + sekcija + `</w:body></w:document>`
}

// Stilovi. Nazivi su hrvatski jer ih korisnik vidi u Wordovu popisu stilova
// kad dokument dalje uređuje.
const stilovi = zaglavljeXML + `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr>
  <w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="21"/><w:szCs w:val="21"/>
  <w:lang w:val="hr-HR"/>
</w:rPr></w:rPrDefault><w:pPrDefault><w:pPr>
  <w:spacing w:before="0" w:after="120" w:line="264" w:lineRule="auto"/>
</w:pPr></w:pPrDefault></w:docDefaults>

<w:style w:type="paragraph" w:styleId="Normal" w:default="1">
  <w:name w:val="Normal"/><w:qFormat/></w:style>

<w:style w:type="paragraph" w:styleId="Celija">
  <w:name w:val="Ćelija tablice"/><w:basedOn w:val="Normal"/>
  <w:pPr><w:spacing w:before="20" w:after="20" w:line="240" w:lineRule="auto"/></w:pPr>
  <w:rPr><w:sz w:val="19"/></w:rPr></w:style>

<w:style w:type="paragraph" w:styleId="Naslov">
  <w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:qFormat/>
  <w:pPr><w:spacing w:before="0" w:after="60"/></w:pPr>
  <w:rPr><w:b/><w:sz w:val="44"/><w:color w:val="1F3864"/></w:rPr></w:style>

<w:style w:type="paragraph" w:styleId="Podnaslov">
  <w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:qFormat/>
  <w:pPr><w:spacing w:before="0" w:after="360"/></w:pPr>
  <w:rPr><w:sz w:val="24"/><w:color w:val="595959"/></w:rPr></w:style>

<w:style w:type="paragraph" w:styleId="Naslov1">
  <w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>
  <w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/>
    <w:pBdr><w:bottom w:val="single" w:sz="6" w:color="BFBFBF"/></w:pBdr>
    <w:outlineLvl w:val="0"/></w:pPr>
  <w:rPr><w:b/><w:sz w:val="30"/><w:color w:val="1F3864"/></w:rPr></w:style>

<w:style w:type="paragraph" w:styleId="Naslov2">
  <w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>
  <w:pPr><w:keepNext/><w:spacing w:before="240" w:after="80"/><w:outlineLvl w:val="1"/></w:pPr>
  <w:rPr><w:b/><w:sz w:val="24"/><w:color w:val="2F5496"/></w:rPr></w:style>

<w:style w:type="paragraph" w:styleId="Napomena">
  <w:name w:val="Napomena"/><w:basedOn w:val="Normal"/>
  <w:pPr><w:spacing w:before="40" w:after="160"/></w:pPr>
  <w:rPr><w:i/><w:sz w:val="18"/><w:color w:val="595959"/></w:rPr></w:style>
</w:styles>`

// Numeriranje: jedan popis s natuknicama. Wordu treba i apstraktni i stvarni
// popis, pa ih ima dva iako je popis jedan.
const numeriranje = zaglavljeXML + `<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="0">
  <w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/>
    <w:lvlJc w:val="left"/>
    <w:pPr><w:ind w:left="360" w:hanging="220"/></w:pPr>
    <w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:hint="default"/></w:rPr></w:lvl>
</w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`
