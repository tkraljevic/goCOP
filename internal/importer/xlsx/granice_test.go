package xlsx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// knjigaSListovima slaže knjigu sa zadanim sadržajem sheetData svakog lista
func knjigaSListovima(t *testing.T, listovi ...string) *bytes.Reader {
	t.Helper()
	var popis, veze strings.Builder
	d := map[string]string{}
	for i, sd := range listovi {
		fmt.Fprintf(&popis, `<sheet name="List%d" r:id="rId%d"/>`, i+1, i+1)
		fmt.Fprintf(&veze, `<Relationship Id="rId%d" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		d[fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)] = `<worksheet><sheetData>` + sd + `</sheetData></worksheet>`
	}
	d["xl/workbook.xml"] = `<workbook xmlns:r="r"><sheets>` + popis.String() + `</sheets></workbook>`
	d["xl/_rels/workbook.xml.rels"] = `<Relationships>` + veze.String() + `</Relationships>`
	return bytes.NewReader(knjiga(t, d))
}

func citaj(r *bytes.Reader) (*Workbook, error) { return Read(r, r.Size()) }

// alociranoZa vraća koliko je bajtova memorije zauzeo poziv
func alociranoZa(f func()) uint64 {
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	f()
	runtime.ReadMemStats(&m1)
	return m1.TotalAlloc - m0.TotalAlloc
}

// Stupac iza XFD i redak iza 1.048.576 odbijaju se porukom, bez slaganja
// lista do njih; zadnji stupac i zadnji redak i dalje se čitaju.
func TestCelijaIzvanGranicaSeOdbija(t *testing.T) {
	for _, sd := range []string{
		`<row r="1"><c r="XFE1"><v>1</v></c></row>`,
		`<row r="1"><c r="ZZZZZZZ1"><v>1</v></c></row>`,
		`<row r="1"><c r="ZZZZZZZZZZZZZZZZZZZZ1"><v>1</v></c></row>`,
		`<row r="2000000000"><c r="A2000000000"><v>1</v></c></row>`,
		`<row r="1048577"><c><v>1</v></c></row>`,
	} {
		var err error
		zauzeto := alociranoZa(func() { _, err = citaj(knjigaSListovima(t, sd)) })
		if err == nil || !strings.Contains(err.Error(), "izvan granica Excela") || !strings.Contains(err.Error(), `list "List1"`) {
			t.Errorf("%s: %v", sd, err)
		}
		if zauzeto > 8<<20 {
			t.Errorf("%s: odbijanje je zauzelo %d MB", sd, zauzeto>>20)
		}
	}
	wb, err := citaj(knjigaSListovima(t, `<row r="1048576"><c r="XFD1048576"><v>kraj</v></c></row>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := wb.Sheets[0].Cell(najviseRedaka-1, najviseStupaca-1); got != "kraj" {
		t.Errorf("zadnja ćelija lista: %q", got)
	}
}

// Ćelije bez adrese idu jedna iza druge; kad prijeđu zadnji stupac, list se
// odbija.
func TestCelijeBezAdreseDoZadnjegStupca(t *testing.T) {
	pun := strings.Repeat(`<c><v>1</v></c>`, najviseStupaca)
	wb, err := citaj(knjigaSListovima(t, `<row>`+pun+`</row>`))
	if err != nil || len(wb.Sheets[0].Rows[0]) != najviseStupaca {
		t.Fatalf("pun redak: %v", err)
	}
	if _, err := citaj(knjigaSListovima(t, `<row>`+pun+`<c><v>1</v></c></row>`)); err == nil || !strings.Contains(err.Error(), "stupcu 16385") {
		t.Errorf("stupac iza zadnjeg: %v", err)
	}
}

// Knjiga koja bi s praznim ćelijama imala više od četiri milijuna ćelija
// odbija se, i kad je razdijeljena na više listova; svaki list zasebno
// i dalje se čita.
func TestPrevelikaKnjigaSeOdbija(t *testing.T) {
	var sd strings.Builder
	for i := range 150 {
		fmt.Fprintf(&sd, `<row r="%d"><c r="XFD%d"><v>1</v></c></row>`, i+1, i+1)
	}
	if _, err := citaj(knjigaSListovima(t, sd.String())); err != nil {
		t.Fatalf("jedan list: %v", err)
	}
	_, err := citaj(knjigaSListovima(t, sd.String(), sd.String()))
	if !errors.Is(err, errPrevelikaKnjiga) || !strings.Contains(err.Error(), `list "List2"`) {
		t.Errorf("dva lista: %v", err)
	}
}

// Redak bez ispravnog broja ide iza prethodnog.
func TestRedakBezBroja(t *testing.T) {
	wb, err := citaj(knjigaSListovima(t, `<row r="3"><c><v>a</v></c></row><row spans="1:1" r="x"><c><v>b</v></c></row>`))
	if err != nil || wb.Sheets[0].Cell(2, 0) != "a" || wb.Sheets[0].Cell(3, 0) != "b" {
		t.Fatalf("redci: %v %q", err, wb.Sheets[0].Rows)
	}
}

// knjigaSTablicom slaže knjigu sa zajedničkom tablicom tekstova i zadanim
// sadržajem sheetData svakog lista
func knjigaSTablicom(t *testing.T, sst string, listovi ...string) *bytes.Reader {
	t.Helper()
	k := knjigaSListovima(t, listovi...)
	d := map[string]string{"xl/sharedStrings.xml": sst}
	zr, err := zip.NewReader(k, k.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		d[f.Name] = string(b)
	}
	return bytes.NewReader(knjiga(t, d))
}

// Dio knjige veći od 64 MB nakon raspakiranja odbija se, a ne skraćuje, i
// ne zauzima memoriju za cijeli dio.
func TestPrevelikDioKnjigeSeOdbija(t *testing.T) {
	list := `<row r="1"><c r="A1" t="inlineStr"><is><t>a</t></is></c></row>` + strings.Repeat(" ", najviseDijela)
	k := knjigaSListovima(t, list)
	var err error
	zauzeto := alociranoZa(func() { _, err = citaj(k) })
	if !errors.Is(err, errPrevelikDio) || !strings.Contains(err.Error(), "64 MB") {
		t.Errorf("prevelik list: %v", err)
	}
	// pročita se najviše 64 MB, koliko je i granica; čitač XML-a taj tekst
	// drži u međuspremniku koji raste udvostručavanjem, pa ukupno zauzme
	// nekoliko puta toliko, i više uz provjeru utrka
	if zauzeto > 6*najviseDijela {
		t.Errorf("odbijanje je zauzelo %d MB", zauzeto>>20)
	}
	sst := `<sst><si><t>a</t></si>` + strings.Repeat(" ", najviseDijela) + `</sst>`
	if _, err := citaj(knjigaSTablicom(t, sst, `<row r="1"><c r="A1" t="s"><v>0</v></c></row>`)); !errors.Is(err, errPrevelikDio) {
		t.Errorf("prevelika tablica: %v", err)
	}
	// dio od točno 64 MB i dalje se čita
	list = `<row r="1"><c r="A1"><v>1</v></c></row>`
	list += strings.Repeat(" ", najviseDijela-len(`<worksheet><sheetData>`+list+`</sheetData></worksheet>`))
	if wb, err := citaj(knjigaSListovima(t, list)); err != nil || wb.Sheets[0].Cell(0, 0) != "1" {
		t.Errorf("dio od 64 MB: %v", err)
	}
}

// Tekst ćelije smije imati najviše 32.767 znakova, koliko dopušta Excel, i
// u ćeliji i u zajedničkoj tablici.
func TestPredugaCelijaUKnjizi(t *testing.T) {
	dug := strings.Repeat("ž", najviseZnakova)
	if _, err := citaj(knjigaSListovima(t, `<row r="1"><c r="A1" t="inlineStr"><is><t>`+dug+`x</t></is></c></row>`)); !errors.Is(err, errPredugaCelija) {
		t.Errorf("u ćeliji: %v", err)
	}
	if _, err := citaj(knjigaSTablicom(t, `<sst><si><r><t>`+dug+`</t></r><r><t>x</t></r></si></sst>`)); !errors.Is(err, errPredugaCelija) {
		t.Errorf("u tablici: %v", err)
	}
	wb, err := citaj(knjigaSTablicom(t, `<sst><si><t>`+dug+`</t></si></sst>`, `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="inlineStr"><is><t>`+dug+`</t></is></c></row>`))
	if err != nil || wb.Sheets[0].Cell(0, 0) != dug || wb.Sheets[0].Cell(0, 1) != dug {
		t.Errorf("ćelije od 32.767 znakova: %v", err)
	}
}

// Zajednička tablica s više zapisa nego što knjiga smije imati ćelija
// odbija se.
func TestZajednickaTablicaSPreviseZapisa(t *testing.T) {
	sst := `<sst>` + strings.Repeat(`<si/>`, najviseCelija+1) + `</sst>`
	if _, err := citaj(knjigaSTablicom(t, sst)); !errors.Is(err, errPrevelikaKnjiga) {
		t.Errorf("previše zapisa: %v", err)
	}
}

// Ćelije koje pokazuju na isti dugi zapis broje se svaka posebno: cijela
// knjiga smije imati najviše 64 MB teksta, i kad je razdijeljen na listove.
func TestPreviseTekstaUKnjizi(t *testing.T) {
	sst := `<sst><si><t>` + strings.Repeat("A", najviseZnakova) + `</t></si></sst>`
	pola := najviseTeksta / najviseZnakova / 2
	var list strings.Builder
	for i := range pola {
		fmt.Fprintf(&list, `<row r="%d"><c r="A%d" t="s"><v>0</v></c></row>`, i+1, i+1)
	}
	if _, err := citaj(knjigaSTablicom(t, sst, list.String(), list.String())); err != nil {
		t.Fatalf("do granice: %v", err)
	}
	fmt.Fprintf(&list, `<row r="%d"><c r="A%d" t="s"><v>0</v></c></row>`, pola+1, pola+1)
	fmt.Fprintf(&list, `<row r="%d"><c r="A%d" t="s"><v>0</v></c></row>`, pola+2, pola+2)
	_, err := citaj(knjigaSTablicom(t, sst, list.String(), list.String()))
	if !errors.Is(err, errPreviseTeksta) || !strings.Contains(err.Error(), `list "List2"`) {
		t.Errorf("preko granice: %v", err)
	}
}

// Predložak ugovora iz dokumentacije i dalje se čita.
func TestPredlozakUGranicama(t *testing.T) {
	wb, err := Open(filepath.Join("..", "..", "..", "docs", "predlosci", "odrzavanje", "ugovor-a02.xlsx"))
	if err != nil || len(wb.Sheets) == 0 {
		t.Fatalf("predložak: %v", err)
	}
}
