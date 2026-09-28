package xlsxw

import (
	"fmt"
	"math"
	"strings"
)

// Graf je Excelov graf s točkama (x, y) povezanima crtom — vrsta „raspršeni
// s ravnim crtama". Vodostaj kroz vrijeme tako stoji na pravom vremenu, a ne
// na jednako razmaknutim kategorijama, pa rupa u mjerenju ostaje rupa.
// Graf je pravi Excelov objekt: pri ispisu je oštar, može se kopirati u Word
// i mijenjati.
type Graf struct {
	Naslov string
	// Sidro na listu, 0-based; Do je prvi stupac i redak iza grafa.
	OdStupca, OdRetka, DoStupca, DoRetka int
	XMin, XMax, XKorak                   float64 // os x; korak glavnih oznaka
	XFormat                              string  // oblik oznaka osi x, npr. "d.m."
	YMin, YMax, YKorak                   float64 // os y; kad je YMax <= YMin, mjerilo bira Excel
	YNaslov                              string
	Nizovi                               []NizGrafa
}

// NizGrafa je jedna crta grafa.
type NizGrafa struct {
	Naziv string
	// Vrijednosti; NaN je rupa. Upisuju se u graf kao zadnje poznate, a
	// Excel ih pri otvaranju obnovi iz ćelija kad je zadana formula.
	X, Y       []float64
	XRef, YRef string  // formule ćelija, npr. 'Podaci'!$A$2:$A$218; prazno = samo vrijednosti
	Boja       string  // RGB, npr. "1F4E9A"
	Debljina   float64 // u točkama; 0 = 1,5
	Crtkano    bool
	BezLegende bool
}

// chartXML piše graf kao DrawingML chartSpace.
func (g Graf) chartXML() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	b.WriteString(`<c:roundedCorners val="0"/><c:chart>`)
	if g.Naslov != "" {
		fmt.Fprintf(&b, `<c:title><c:tx><c:rich><a:bodyPr/><a:lstStyle/><a:p><a:pPr><a:defRPr sz="1100" b="1"/></a:pPr><a:r><a:rPr lang="hr-HR" sz="1100" b="1"/><a:t>%s</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title><c:autoTitleDeleted val="0"/>`, esc(g.Naslov))
	} else {
		b.WriteString(`<c:autoTitleDeleted val="1"/>`)
	}
	b.WriteString(`<c:plotArea><c:layout/><c:scatterChart><c:scatterStyle val="lineMarker"/><c:varyColors val="0"/>`)
	for i, n := range g.Nizovi {
		debljina := n.Debljina
		if debljina <= 0 {
			debljina = 1.5
		}
		fmt.Fprintf(&b, `<c:ser><c:idx val="%d"/><c:order val="%d"/><c:tx><c:v>%s</c:v></c:tx>`, i, i, esc(n.Naziv))
		fmt.Fprintf(&b, `<c:spPr><a:ln w="%d" cap="rnd"><a:solidFill><a:srgbClr val="%s"/></a:solidFill>`, int(debljina*12700), boja(n.Boja))
		if n.Crtkano {
			b.WriteString(`<a:prstDash val="dash"/>`)
		}
		b.WriteString(`<a:round/></a:ln></c:spPr><c:marker><c:symbol val="none"/></c:marker>`)
		b.WriteString(`<c:xVal>` + brojevi(n.XRef, n.X) + `</c:xVal><c:yVal>` + brojevi(n.YRef, n.Y) + `</c:yVal>`)
		b.WriteString(`<c:smooth val="0"/></c:ser>`)
	}
	b.WriteString(`<c:axId val="5001"/><c:axId val="5002"/></c:scatterChart>`)
	// Os x: vrijeme, oznake dolje; os y presijeca je na najmanjoj vrijednosti,
	// pa stoji uz lijevi rub i kad je vodostaj negativan.
	b.WriteString(`<c:valAx><c:axId val="5001"/>` + mjerilo(g.XMin, g.XMax) + `<c:delete val="0"/><c:axPos val="b"/>`)
	b.WriteString(`<c:majorGridlines><c:spPr><a:ln w="6350"><a:solidFill><a:srgbClr val="E3E3E3"/></a:solidFill></a:ln></c:spPr></c:majorGridlines>`)
	format := g.XFormat
	if format == "" {
		format = "General"
	}
	fmt.Fprintf(&b, `<c:numFmt formatCode="%s" sourceLinked="0"/><c:majorTickMark val="out"/><c:minorTickMark val="none"/><c:tickLblPos val="low"/>`, esc(format))
	b.WriteString(osTeksta() + `<c:crossAx val="5002"/><c:crosses val="min"/><c:crossBetween val="midCat"/>`)
	if g.XKorak > 0 {
		fmt.Fprintf(&b, `<c:majorUnit val="%s"/>`, broj(g.XKorak))
	}
	b.WriteString(`</c:valAx>`)
	b.WriteString(`<c:valAx><c:axId val="5002"/>` + mjerilo(g.YMin, g.YMax) + `<c:delete val="0"/><c:axPos val="l"/>`)
	b.WriteString(`<c:majorGridlines><c:spPr><a:ln w="6350"><a:solidFill><a:srgbClr val="E3E3E3"/></a:solidFill></a:ln></c:spPr></c:majorGridlines>`)
	if g.YNaslov != "" {
		fmt.Fprintf(&b, `<c:title><c:tx><c:rich><a:bodyPr rot="-5400000" vert="horz"/><a:lstStyle/><a:p><a:pPr><a:defRPr sz="900" b="0"/></a:pPr><a:r><a:rPr lang="hr-HR" sz="900" b="0"/><a:t>%s</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title>`, esc(g.YNaslov))
	}
	b.WriteString(`<c:numFmt formatCode="#,##0" sourceLinked="0"/><c:majorTickMark val="out"/><c:minorTickMark val="none"/><c:tickLblPos val="low"/>`)
	b.WriteString(osTeksta() + `<c:crossAx val="5001"/><c:crosses val="min"/><c:crossBetween val="midCat"/>`)
	if g.YKorak > 0 && g.YMax > g.YMin {
		fmt.Fprintf(&b, `<c:majorUnit val="%s"/>`, broj(g.YKorak))
	}
	b.WriteString(`</c:valAx></c:plotArea>`)
	b.WriteString(`<c:legend><c:legendPos val="b"/>`)
	for i, n := range g.Nizovi {
		if n.BezLegende {
			fmt.Fprintf(&b, `<c:legendEntry><c:idx val="%d"/><c:delete val="1"/></c:legendEntry>`, i)
		}
	}
	b.WriteString(`<c:overlay val="0"/><c:txPr><a:bodyPr/><a:lstStyle/><a:p><a:pPr><a:defRPr sz="800"/></a:pPr><a:endParaRPr lang="hr-HR"/></a:p></c:txPr></c:legend>`)
	b.WriteString(`<c:plotVisOnly val="1"/><c:dispBlanksAs val="gap"/></c:chart>`)
	b.WriteString(`<c:txPr><a:bodyPr/><a:lstStyle/><a:p><a:pPr><a:defRPr sz="900"><a:latin typeface="Arial"/></a:defRPr></a:pPr><a:endParaRPr lang="hr-HR"/></a:p></c:txPr>`)
	b.WriteString(`</c:chartSpace>`)
	return b.String()
}

// mjerilo je <c:scaling>; bez granica Excel bira sam.
func mjerilo(od, do float64) string {
	s := `<c:scaling><c:orientation val="minMax"/>`
	if do > od {
		s += fmt.Sprintf(`<c:max val="%s"/><c:min val="%s"/>`, broj(do), broj(od))
	}
	return s + `</c:scaling>`
}

// osTeksta je sitniji tekst oznaka osi.
func osTeksta() string {
	return `<c:txPr><a:bodyPr/><a:lstStyle/><a:p><a:pPr><a:defRPr sz="800"/></a:pPr><a:endParaRPr lang="hr-HR"/></a:p></c:txPr>`
}

// brojevi je izvor točaka niza: formula s upisanim vrijednostima, ili samo
// vrijednosti kad formule nema. NaN se preskače — Excel ga čita kao rupu.
func brojevi(ref string, v []float64) string {
	var cache strings.Builder
	fmt.Fprintf(&cache, `<c:formatCode>General</c:formatCode><c:ptCount val="%d"/>`, len(v))
	for i, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			continue
		}
		fmt.Fprintf(&cache, `<c:pt idx="%d"><c:v>%s</c:v></c:pt>`, i, broj(x))
	}
	if ref == "" {
		return `<c:numLit>` + cache.String() + `</c:numLit>`
	}
	return `<c:numRef><c:f>` + esc(ref) + `</c:f><c:numCache>` + cache.String() + `</c:numCache></c:numRef>`
}

// boja čisti oznaku boje; prazno je tamnoplava.
func boja(s string) string {
	s = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "#")
	if len(s) != 6 {
		return "1F4E9A"
	}
	return s
}

// sidroGrafa smješta graf između ćelija i upućuje na njegov dio.
func sidroGrafa(g Graf, id int, rel string) string {
	return fmt.Sprintf(`<xdr:twoCellAnchor editAs="oneCell"><xdr:from><xdr:col>%d</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from>`+
		`<xdr:to><xdr:col>%d</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to>`+
		`<xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="%d" name="Graf %d"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr>`+
		`<xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart">`+
		`<c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="%s"/></a:graphicData></a:graphic></xdr:graphicFrame><xdr:clientData/></xdr:twoCellAnchor>`,
		g.OdStupca, g.OdRetka, g.DoStupca, g.DoRetka, id, id, rel)
}

// Excelov datum: dani od 30. 12. 1899.; sat je dio dana.
func ExcelDatum(godina, mjesec, dan, sat, minuta int) float64 {
	return excelDani(godina, mjesec, dan) + (float64(sat)*60+float64(minuta))/1440
}

// excelDani su dani od 30. 12. 1899. do ponoći zadanog dana.
func excelDani(g, m, d int) float64 {
	// algoritam dana od epohe (Howard Hinnant), pomaknut na Excelovu nulu
	if m <= 2 {
		g--
	}
	era := g / 400
	if g < 0 && g%400 != 0 {
		era = (g - 399) / 400
	}
	yoe := g - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	danaOdUnixa := era*146097 + doe - 719468
	return float64(danaOdUnixa) + 25569
}
