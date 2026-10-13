package xlsx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// Zaključava greške radne knjige (dio arhive koji se ne da otvoriti ili nije
// ispravan XML, list bez datoteke, knjiga bez veza listova) i rubne ćelije:
// ćeliju izvan retka, zajednički tekst kojeg nema i logičku laž.

const (
	knjigaListova = `<workbook xmlns:r="r"><sheets><sheet name="List1" r:id="rId1"/></sheets></workbook>`
	vezeListova   = `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`
	prazanList    = `<worksheet><sheetData/></worksheet>`
)

// ispravnaKnjiga su dijelovi najmanje ispravne knjige s jednim listom
func ispravnaKnjiga() map[string]string {
	return map[string]string{
		"xl/workbook.xml":            knjigaListova,
		"xl/_rels/workbook.xml.rels": vezeListova,
		"xl/sharedStrings.xml":       `<sst><si><t>prvi</t></si></sst>`,
		"xl/worksheets/sheet1.xml":   prazanList,
	}
}

// knjigaSNecitljivim slaže knjigu u kojoj je zadani dio spremljen nepoznatim
// sažimanjem, pa ga zip ne može otvoriti
func knjigaSNecitljivim(t *testing.T, datoteke map[string]string, necitljiv string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for ime, sadrzaj := range datoteke {
		var w interface{ Write([]byte) (int, error) }
		var err error
		if ime == necitljiv {
			w, err = zw.CreateRaw(&zip.FileHeader{Name: ime, Method: 99, CompressedSize64: uint64(len(sadrzaj)), UncompressedSize64: uint64(len(sadrzaj))})
		} else {
			w, err = zw.Create(ime)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(sadrzaj)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGreskeRadneKnjige(t *testing.T) {
	for _, dio := range []string{"xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/sharedStrings.xml", "xl/worksheets/sheet1.xml"} {
		// dio koji se ne da otvoriti
		b := knjigaSNecitljivim(t, ispravnaKnjiga(), dio)
		if _, err := Read(bytes.NewReader(b), int64(len(b))); err == nil || !strings.Contains(err.Error(), "algorithm") {
			t.Errorf("nečitljiv %s: %v", dio, err)
		}
		// dio koji nije ispravan XML
		d := ispravnaKnjiga()
		d[dio] = `<a><b></a>`
		b = knjiga(t, d)
		if _, err := Read(bytes.NewReader(b), int64(len(b))); err == nil || !strings.Contains(err.Error(), "XML syntax error") {
			t.Errorf("neispravan XML u %s: %v", dio, err)
		}
	}
	b := knjigaSNecitljivim(t, ispravnaKnjiga(), "xl/worksheets/sheet1.xml")
	if _, err := Read(bytes.NewReader(b), int64(len(b))); err == nil || !strings.HasPrefix(err.Error(), `list "List1": `) {
		t.Errorf("greška lista ne kaže koji je list: %v", err)
	}

	d := ispravnaKnjiga()
	delete(d, "xl/worksheets/sheet1.xml")
	b = knjiga(t, d)
	if _, err := Read(bytes.NewReader(b), int64(len(b))); err == nil || err.Error() != `list "List1": nema datoteke xl/worksheets/sheet1.xml u arhivi` {
		t.Errorf("list bez datoteke: %v", err)
	}
	d = ispravnaKnjiga()
	delete(d, "xl/_rels/workbook.xml.rels")
	b = knjiga(t, d)
	if _, err := Read(bytes.NewReader(b), int64(len(b))); err == nil || err.Error() != "arhiva nema xl/_rels/workbook.xml.rels" {
		t.Errorf("knjiga bez veza: %v", err)
	}
}

func TestRubneCelije(t *testing.T) {
	d := ispravnaKnjiga()
	d["xl/worksheets/sheet1.xml"] = `<worksheet><sheetData>
<c t="inlineStr"><is><t>izvan retka</t></is></c>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>7</v></c><c r="C1" t="s"><v>nije broj</v></c><c r="D1" t="b"><v>0</v></c><c r="E1" t="b"><v> 1 </v></c></row>
</sheetData></worksheet>`
	b := knjiga(t, d)
	wb, err := Read(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	s := wb.Sheet("List1")
	// ćelija izvan retka se ne upisuje; zajednički tekst kojeg nema je prazan
	if len(s.Rows) != 1 || s.Cell(0, 0) != "prvi" || s.Cell(0, 1) != "" || s.Cell(0, 2) != "" || s.Cell(0, 3) != "FALSE" || s.Cell(0, 4) != "TRUE" {
		t.Errorf("redci: %q", s.Rows)
	}
}
