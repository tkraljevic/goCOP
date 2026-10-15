package xlsx

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava otvaranje radne knjige s diska i rubne slučajeve čitanja:
// knjigu bez zajedničkih tekstova, arhivu bez popisa listova, nepostojeći
// list i adrese ćelija malim slovima.

func knjiga(t *testing.T, datoteke map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for ime, sadrzaj := range datoteke {
		w, err := zw.Create(ime)
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

// Radna knjiga se otvara s diska; nepostojeća datoteka i datoteka koja nije
// zip javljaju grešku.
func TestOtvaranjeSDiska(t *testing.T) {
	mapa := t.TempDir()
	sadrzaj, err := io.ReadAll(uzorak(t))
	if err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(mapa, "ugovor.xlsx")
	if err := os.WriteFile(put, sadrzaj, 0o644); err != nil {
		t.Fatal(err)
	}
	wb, err := Open(put)
	if err != nil {
		t.Fatal(err)
	}
	if len(wb.Sheets) != 2 || wb.Sheet("TROŠKOVNIK").Cell(1, 0) != "#P" {
		t.Errorf("listovi: %+v", wb.Sheets)
	}
	if wb.Sheet("NEMA") != nil || wb.SheetPrefix("NEMA") != nil {
		t.Error("nepostojeći list nije nil")
	}

	if _, err := Open(filepath.Join(mapa, "nema.xlsx")); err == nil {
		t.Error("nepostojeća datoteka je otvorena")
	}
	tekst := filepath.Join(mapa, "tekst.xlsx")
	if err := os.WriteFile(tekst, []byte("Datum;Vodostaj\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(tekst); err == nil || !strings.Contains(err.Error(), "nije xlsx") {
		t.Errorf("tekst umjesto xlsx: %v", err)
	}
}

// Knjiga bez zajedničkih tekstova čita se iz upisanih vrijednosti; arhiva
// bez popisa listova nije radna knjiga.
func TestKnjigaBezZajednickihTekstova(t *testing.T) {
	b := knjiga(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="List1" r:id="rId1"/><sheet name="Bez veze" r:id="rId9"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row r="1"><c r="b1" t="inlineStr"><is><t>Batina</t></is></c></row></sheetData></worksheet>`,
	})
	wb, err := Read(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if len(wb.Sheets) != 1 || wb.Sheets[0].Cell(0, 1) != "Batina" {
		t.Errorf("listovi: %+v — list bez veze se preskače, b1 je drugi stupac", wb.Sheets)
	}

	b = knjiga(t, map[string]string{"xl/sharedStrings.xml": `<sst/>`, "xl/_rels/workbook.xml.rels": `<Relationships/>`})
	if _, err := Read(bytes.NewReader(b), int64(len(b))); err == nil || !strings.Contains(err.Error(), "xl/workbook.xml") {
		t.Errorf("arhiva bez popisa listova: %v", err)
	}
}

// Adresa ćelije prima i mala slova; adresa bez slova nije adresa.
func TestStupciMalimSlovima(t *testing.T) {
	for ref, want := range map[string]int{"a1": 0, "k7": 10, "aB10": 27, "Zz": 701} {
		if got, ok := columnIndex(ref); !ok || got != want {
			t.Errorf("%s → %d (%v), očekivano %d", ref, got, ok, want)
		}
	}
	for _, los := range []string{"", "12", "#1"} {
		if _, ok := columnIndex(los); ok {
			t.Errorf("%q je prošlo kao adresa", los)
		}
	}
	for _, s := range []struct {
		celija string
		broj   float64
		ok     bool
	}{
		{"", 0, false},
		{"  ", 0, false},
		{" 12.5 ", 12.5, true},
		{"1.234.567,25", 1234567.25, true},
		{"nije broj", 0, false},
	} {
		if v, ok := Number(s.celija); ok != s.ok || (ok && v != s.broj) {
			t.Errorf("Number(%q) = %v, %v", s.celija, v, ok)
		}
	}
}
