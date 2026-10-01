package xlsxw

import (
	"archive/zip"
	"bytes"
	"io"
	"math"
	"strings"
	"testing"
)

// Knjiga s tekstom, brojem i formulom mora biti valjan zip s listovima,
// stilovima i formulom koja nosi i izračunatu vrijednost.
func TestZapisiKnjigu(t *testing.T) {
	k := &Knjiga{}
	l := k.NoviList("BP 16 / Baranja: <test>")
	l.Sirine = []float64{20, 10}
	l.Dodaj(T("Ime & prezime", Podebljan), T("sati"))
	l.Dodaj(T("Ana Anić"), N(12.5, Broj2), F("B2*2", 25, Broj2Pod))
	k.NoviList("REKAPITULACIJA").Dodaj(T("x"))

	var buf bytes.Buffer
	if err := k.Zapisi(&buf); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	dijelovi := map[string]string{}
	for _, f := range z.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		dijelovi[f.Name] = string(b)
	}
	for _, ime := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if dijelovi[ime] == "" {
			t.Errorf("nema %s", ime)
		}
	}
	if !strings.Contains(dijelovi["xl/workbook.xml"], `name="BP 16 - Baranja- &lt;test&gt;"`) {
		t.Errorf("naziv lista nije očišćen: %s", dijelovi["xl/workbook.xml"])
	}
	s1 := dijelovi["xl/worksheets/sheet1.xml"]
	for _, zelim := range []string{`<t xml:space="preserve">Ime &amp; prezime</t>`, `<c r="B2" s="2"><v>12.5</v></c>`, `<c r="C2" s="3"><f>B2*2</f><v>25</v></c>`, `<col min="1" max="1" width="20.00"`} {
		if !strings.Contains(s1, zelim) {
			t.Errorf("list 1 nema %s", zelim)
		}
	}
	if Adresa(27, 9) != "AB10" || Stupac(0) != "A" || Stupac(25) != "Z" || Stupac(26) != "AA" {
		t.Error("adrese stupaca")
	}
}

// Graf je pravi Excelov dio knjige: chart u svom dijelu, sidren na list
// kroz crtež, s formulom koja upućuje na podatke i upisanim vrijednostima;
// rupa (NaN) se ne upisuje. List podataka može biti skriven, a list s
// grafovima može zamrznuti zaglavlje.
func TestGrafUKnjizi(t *testing.T) {
	k := &Knjiga{}
	g := k.NoviList("Grafovi")
	g.Zamrzni = [2]int{3, 0}
	g.Grafovi = []Graf{{Naslov: "Batina", OdRetka: 3, DoStupca: 9, DoRetka: 23, XMin: 1, XMax: 3, YMin: 0, YMax: 100, YKorak: 20,
		Nizovi: []NizGrafa{
			{Naziv: "izmjereno", X: []float64{1, 2, 3}, Y: []float64{10, math.NaN(), 30}, XRef: "'Podaci grafova'!$A$2:$A$4", YRef: "'Podaci grafova'!$B$2:$B$4"},
			{Naziv: "pripremna", X: []float64{1, 3}, Y: []float64{80, 80}, Boja: "#f1c232", Crtkano: true, BezLegende: true},
		}}}
	p := k.NoviList("Podaci grafova")
	p.Skriven = true
	p.Dodaj(T("x"))
	var buf bytes.Buffer
	if err := k.Zapisi(&buf); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	d := map[string]string{}
	for _, f := range z.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		d[f.Name] = string(b)
	}
	c := d["xl/charts/chart1.xml"]
	for _, zelim := range []string{"<c:scatterChart>", "<a:t>Batina</a:t>", "<c:f>&#39;Podaci grafova&#39;!$A$2:$A$4</c:f>",
		`<c:pt idx="2"><c:v>30</c:v></c:pt>`, "<c:numLit>", `<a:srgbClr val="F1C232"/>`, `<a:prstDash val="dash"/>`,
		`<c:legendEntry><c:idx val="1"/><c:delete val="1"/></c:legendEntry>`, `<c:max val="100"/><c:min val="0"/>`, `<c:dispBlanksAs val="gap"/>`} {
		if !strings.Contains(c, zelim) && !strings.Contains(c, strings.ReplaceAll(zelim, "&#39;", "'")) {
			t.Errorf("graf nema %s", zelim)
		}
	}
	prvi, _, _ := strings.Cut(c, "</c:ser>")
	if _, y, _ := strings.Cut(prvi, "<c:yVal>"); strings.Contains(y, `<c:pt idx="1">`) {
		t.Errorf("rupa (NaN) ne smije biti upisana kao točka")
	}
	if !strings.Contains(d["xl/drawings/drawing1.xml"], `r:id="rIdG1"`) || !strings.Contains(d["xl/drawings/_rels/drawing1.xml.rels"], "../charts/chart1.xml") {
		t.Errorf("crtež ne upućuje na graf: %s", d["xl/drawings/_rels/drawing1.xml.rels"])
	}
	if !strings.Contains(d["[Content_Types].xml"], `/xl/charts/chart1.xml`) {
		t.Errorf("vrsta dijela grafa nije prijavljena")
	}
	if !strings.Contains(d["xl/workbook.xml"], `name="Podaci grafova" sheetId="2" state="hidden"`) {
		t.Errorf("list podataka nije skriven: %s", d["xl/workbook.xml"])
	}
	if !strings.Contains(d["xl/worksheets/sheet1.xml"], `ySplit="3" topLeftCell="A4" activePane="bottomLeft" state="frozen"`) ||
		!strings.Contains(d["xl/worksheets/sheet1.xml"], `<drawing r:id="rIdD"/>`) {
		t.Errorf("list grafova: %s", d["xl/worksheets/sheet1.xml"])
	}
	if ExcelDatum(2026, 1, 1, 0, 0) != 46023 || ExcelDatum(2026, 9, 27, 12, 0) != 46292.5 || ExcelDatum(1900, 3, 1, 0, 0) != 61 {
		t.Errorf("Excelov datum: %v %v %v", ExcelDatum(2026, 1, 1, 0, 0), ExcelDatum(2026, 9, 27, 12, 0), ExcelDatum(1900, 3, 1, 0, 0))
	}
}

func TestSkriveniRedak(t *testing.T) {
	l := &List{Naziv: "P"}
	l.Dodaj(T("cm", Tablica))
	l.Dodaj(T("model", Tablica))
	l.SakrijRedak(l.Redak() - 1)
	l.Dodaj(T("dalje", Tablica))
	x := l.xml(false)
	for _, s := range []string{`outlineLevelRow="1"`, `<row r="2" hidden="1" outlineLevel="1">`, `<row r="3" collapsed="1">`, `<row r="1">`} {
		if !strings.Contains(x, s) {
			t.Errorf("nema %s", s)
		}
	}
}
