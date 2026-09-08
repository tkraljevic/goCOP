package docx

import (
	"fmt"
	"time"
)

// Nepromjenjivi dijelovi .docx paketa. Svaki je propisan standardom OOXML;
// Word odbija otvoriti datoteku kojoj ijedan nedostaje ili mu se vrsta ne
// poklapa s onim što je unutra.

const zaglavljeXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

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
func documentXML(tijelo string) string {
	return zaglavljeXML + `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + tijelo +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="708" w:footer="708" w:gutter="0"/>` +
		`</w:sectPr></w:body></w:document>`
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
