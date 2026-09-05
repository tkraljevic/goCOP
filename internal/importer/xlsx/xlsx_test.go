package xlsx

import (
	"archive/zip"
	"bytes"
	"testing"
)

// gradi malu radnu knjigu s dva lista: zajednički tekstovi, upisani tekst,
// broj, logička vrijednost i ćelije koje preskaču stupce
func uzorak(t *testing.T) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="TROŠKOVNIK" sheetId="1" r:id="rId1"/><sheet name="LOKACIJE_BP_16" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="x" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="x" Target="/xl/worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>#P</t></si><si><r><t>Potok </t></r><r><t>Karašica</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="2"><c r="A2" t="s"><v>0</v></c><c r="I2" t="str"><v>A.02.01.16.01.01.01.</v></c><c r="K2" t="inlineStr"><is><t>1.1.</t></is></c></row>
<row r="3"><c r="A3" t="s"><v>1</v></c><c r="B3"><v>170806.7</v></c><c r="C3" t="b"><v>1</v></c></row>
</sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row><c t="inlineStr"><is><t>VODE II. REDA</t></is></c></row>
</sheetData></worksheet>`,
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestCitanjeRadneKnjige(t *testing.T) {
	r := uzorak(t)
	wb, err := Read(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	if len(wb.Sheets) != 2 || wb.Sheets[0].Name != "TROŠKOVNIK" {
		t.Fatalf("listovi: %+v", wb.Sheets)
	}
	s := wb.Sheet("TROŠKOVNIK")
	if got := s.Cell(1, 0); got != "#P" {
		t.Errorf("A2 = %q, očekivano #P", got)
	}
	if got := s.Cell(1, 8); got != "A.02.01.16.01.01.01." {
		t.Errorf("I2 = %q", got)
	}
	if got := s.Cell(1, 10); got != "1.1." {
		t.Errorf("K2 = %q", got)
	}
	if got := s.Cell(2, 0); got != "Potok Karašica" {
		t.Errorf("bogati tekst A3 = %q", got)
	}
	if v, ok := Number(s.Cell(2, 1)); !ok || v != 170806.7 {
		t.Errorf("B3 = %q", s.Cell(2, 1))
	}
	if got := s.Cell(2, 2); got != "TRUE" {
		t.Errorf("C3 = %q", got)
	}
	if got := s.Cell(0, 0); got != "" {
		t.Errorf("prazan redak 1 daje %q", got)
	}
	if l := wb.SheetPrefix("LOKACIJE"); l == nil || l.Cell(0, 0) != "VODE II. REDA" {
		t.Errorf("list bez adresa ćelija: %+v", l)
	}
}

func TestStupci(t *testing.T) {
	for ref, want := range map[string]int{"A1": 0, "K7": 10, "Z3": 25, "AA1": 26, "AB10": 27} {
		if got, ok := columnIndex(ref); !ok || got != want {
			t.Errorf("%s → %d (%v), očekivano %d", ref, got, ok, want)
		}
	}
	if v, ok := Number("1.234,5"); !ok || v != 1234.5 {
		t.Errorf("zarez kao decimalni znak: %v %v", v, ok)
	}
}
