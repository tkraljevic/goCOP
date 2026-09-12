package xlsxw

import (
	"archive/zip"
	"bytes"
	"io"
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
