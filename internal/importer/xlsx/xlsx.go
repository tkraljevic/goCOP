// Package xlsx čita vrijednosti ćelija iz Excelove radne knjige.
//
// Radna knjiga je zip s XML-om; nama trebaju samo nazivi listova i tekst
// ćelija, pa se ne uvozi vanjska knjižnica. Oblikovanje, formule i stilovi
// se preskaču — čita se ono što je Excel zadnji put izračunao.
package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Workbook je pročitana radna knjiga
type Workbook struct {
	Sheets []*Sheet
}

// Sheet je jedan list: redci i ćelije kao tekst, prazne ćelije su prazni nizovi
type Sheet struct {
	Name string
	Rows [][]string
}

// Cell vraća tekst ćelije ili prazno kad je izvan popunjenog dijela
func (s *Sheet) Cell(row, col int) string {
	if s == nil || row < 0 || row >= len(s.Rows) || col < 0 || col >= len(s.Rows[row]) {
		return ""
	}
	return s.Rows[row][col]
}

// Sheet vraća list točnog naziva, ili nil
func (w *Workbook) Sheet(name string) *Sheet {
	for _, s := range w.Sheets {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// SheetPrefix vraća prvi list čiji naziv počinje zadanim tekstom, ili nil
func (w *Workbook) SheetPrefix(prefix string) *Sheet {
	for _, s := range w.Sheets {
		if strings.HasPrefix(s.Name, prefix) {
			return s
		}
	}
	return nil
}

// Open čita radnu knjigu s diska
func Open(filename string) (*Workbook, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return Read(f, st.Size())
}

// Read čita radnu knjigu iz zip arhive
func Read(r io.ReaderAt, size int64) (*Workbook, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("nije xlsx (zip): %w", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}

	shared, err := readSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return nil, err
	}
	rels, err := readRels(files["xl/_rels/workbook.xml.rels"])
	if err != nil {
		return nil, err
	}
	sheets, err := readWorkbook(files["xl/workbook.xml"])
	if err != nil {
		return nil, err
	}

	wb := &Workbook{}
	ostalo := &ostatak{celija: najviseCelija, teksta: najviseTeksta}
	for _, sh := range sheets {
		target := rels[sh.relID]
		if target == "" {
			continue
		}
		name := resolveTarget(target)
		f := files[name]
		if f == nil {
			return nil, fmt.Errorf("list %q: nema datoteke %s u arhivi", sh.name, name)
		}
		rows, err := readSheet(f, shared, ostalo)
		if err != nil {
			return nil, fmt.Errorf("list %q: %w", sh.name, err)
		}
		wb.Sheets = append(wb.Sheets, &Sheet{Name: sh.name, Rows: rows})
	}
	return wb, nil
}

// resolveTarget svodi cilj veze na putanju u arhivi: "worksheets/sheet1.xml"
// i "/xl/worksheets/sheet1.xml" oba pokazuju na xl/worksheets/sheet1.xml
func resolveTarget(target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return path.Join("xl", target)
}

// najviseDijela je najveći dio knjige nakon raspakiranja; Excelova knjiga
// s nekoliko tisuća redaka ima dijelove od nekoliko MB
const najviseDijela = 64 << 20

// errPrevelikDio znači da je dio knjige nakon raspakiranja veći od
// najviseDijela
var errPrevelikDio = errors.New("tablica je prevelika (dio knjige veći od 64 MB nakon raspakiranja); podijelite je na manje datoteke")

func openXML(f *zip.File) (io.ReadCloser, error) {
	if f == nil {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	return &ograniceniDio{ReadCloser: rc, ostalo: najviseDijela + 1}, nil
}

// ograniceniDio čita dio knjige i vraća grešku kad dio prijeđe granicu,
// umjesto da ga tiho skrati; ostalo je granica uvećana za jedan bajt
type ograniceniDio struct {
	io.ReadCloser
	ostalo int64
}

func (o *ograniceniDio) Read(p []byte) (int, error) {
	if int64(len(p)) > o.ostalo {
		p = p[:o.ostalo]
	}
	n, err := o.ReadCloser.Read(p)
	if o.ostalo -= int64(n); o.ostalo <= 0 {
		return n, errPrevelikDio
	}
	return n, err
}

type sheetRef struct{ name, relID string }

func readWorkbook(f *zip.File) ([]sheetRef, error) {
	rc, err := openXML(f)
	if err != nil {
		return nil, err
	}
	if rc == nil {
		return nil, fmt.Errorf("arhiva nema xl/workbook.xml")
	}
	defer rc.Close()

	var out []sheetRef
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "sheet" {
			continue
		}
		var ref sheetRef
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "name":
				ref.name = a.Value
			case "id": // r:id
				ref.relID = a.Value
			}
		}
		out = append(out, ref)
	}
	return out, nil
}

func readRels(f *zip.File) (map[string]string, error) {
	rc, err := openXML(f)
	if err != nil {
		return nil, err
	}
	if rc == nil {
		return nil, fmt.Errorf("arhiva nema xl/_rels/workbook.xml.rels")
	}
	defer rc.Close()

	out := map[string]string{}
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		var id, target string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			}
		}
		out[id] = target
	}
	return out, nil
}

// readSharedStrings čita zajedničke tekstove; bogato oblikovan tekst ima više
// <t> u jednom <si>, pa se spajaju. Tablica s više zapisa nego što knjiga
// smije imati ćelija, ili zapis dulji nego što Excel dopušta, odbija se.
func readSharedStrings(f *zip.File) ([]string, error) {
	rc, err := openXML(f)
	if err != nil {
		return nil, err
	}
	if rc == nil {
		return nil, nil
	}
	defer rc.Close()

	c := &citacTablice{}
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return c.out, nil
		}
		if err != nil {
			return nil, err
		}
		if err := c.token(tok); err != nil {
			return nil, err
		}
	}
}

// citacTablice slaže zapise zajedničke tablice tekstova dok je čita
type citacTablice struct {
	out       []string
	cur       strings.Builder
	znakova   int
	inSI, inT bool
}

func (c *citacTablice) token(tok xml.Token) error {
	switch t := tok.(type) {
	case xml.StartElement:
		switch t.Name.Local {
		case "si":
			c.inSI, c.znakova = true, 0
			c.cur.Reset()
		case "t":
			c.inT = c.inSI
		}
	case xml.CharData:
		return c.znakovi(t)
	case xml.EndElement:
		return c.end(t.Name.Local)
	}
	return nil
}

func (c *citacTablice) znakovi(t xml.CharData) error {
	if !c.inT {
		return nil
	}
	if c.znakova += utf8.RuneCount(t); c.znakova > najviseZnakova {
		return errPredugaCelija
	}
	c.cur.Write(t)
	return nil
}

func (c *citacTablice) end(name string) error {
	switch name {
	case "t":
		c.inT = false
	case "si":
		if len(c.out) >= najviseCelija {
			return errPrevelikaKnjiga
		}
		c.out = append(c.out, c.cur.String())
		c.inSI = false
	}
	return nil
}

// Granice lista su Excelove: 16.384 stupca (do XFD) i 1.048.576 redaka.
// Ćelija izvan toga ne postoji ni u jednoj pravoj radnoj knjizi.
const (
	najviseStupaca = 16384
	najviseRedaka  = 1 << 20
	// najviseCelija je koliko ćelija čitač smije složiti iz cijele knjige,
	// zajedno s praznima koje se popunjavaju do zadnje ispunjene u retku
	najviseCelija = 4_000_000
	// najviseZnakova je najdulji tekst ćelije koji Excel dopušta
	najviseZnakova = 32767
	// najviseTeksta je koliko teksta svih ćelija knjige zajedno čitač
	// smije složiti, u bajtovima; ćelije koje pokazuju na isti zajednički
	// tekst broje se svaka posebno
	najviseTeksta = 64 << 20
)

var (
	errPredugaCelija = errors.New("ćelija ima više od 32.767 znakova, koliko Excel dopušta")
	errPreviseTeksta = errors.New("tablica ima previše teksta (više od 64 MB); podijelite je na manje datoteke")
)

// ostatak je koliko knjiga još smije složiti: ćelija, s praznima koje se
// popunjavaju do zadnje ispunjene u retku, i bajtova teksta
type ostatak struct {
	celija, teksta int
}

// potrosi oduzima složene ćelije i tekst, ili javlja da bi knjiga prešla
// granicu; tada se ništa ne oduzima
func (o *ostatak) potrosi(celija, teksta int) error {
	if celija > o.celija {
		return errPrevelikaKnjiga
	}
	if teksta > o.teksta {
		return errPreviseTeksta
	}
	o.celija -= celija
	o.teksta -= teksta
	return nil
}

// errPrevelikaKnjiga znači da bi knjiga u memoriji imala više od
// najviseCelija ćelija
var errPrevelikaKnjiga = errors.New("tablica je prevelika (više od 4 milijuna ćelija); podijelite je na manje datoteke")

// readSheet čita ćelije lista u pravokutnik redaka; ostalo je koliko knjiga
// još smije složiti, i smanjuje se za ono što složi ovaj list
func readSheet(f *zip.File, shared []string, ostalo *ostatak) ([][]string, error) {
	rc, err := openXML(f)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	c := &citacLista{shared: shared, ostalo: ostalo, curRow: -1, curCol: -1}
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return c.rows, nil
		}
		if err != nil {
			return nil, err
		}
		if err := c.token(tok); err != nil {
			return nil, err
		}
	}
}

// citacLista prati gdje je čitanje lista: trenutni redak i ćeliju, vrstu i
// tekst ćelije, i u kojem je elementu
type citacLista struct {
	shared  []string
	rows    [][]string
	ostalo  *ostatak
	znakova int // znakova u trenutnoj ćeliji
	curRow  int
	curCol  int
	nextCol int

	cellType       string
	value          strings.Builder
	inV, inIS, inT bool
}

func (c *citacLista) token(tok xml.Token) error {
	switch t := tok.(type) {
	case xml.StartElement:
		c.start(t)
	case xml.CharData:
		return c.znakovi(t)
	case xml.EndElement:
		return c.end(t.Name.Local)
	}
	return nil
}

// znakovi dodaje tekst trenutnoj ćeliji; ćelija dulja nego što Excel
// dopušta odbija se
func (c *citacLista) znakovi(t xml.CharData) error {
	if !c.inV && !c.inT {
		return nil
	}
	if c.znakova += utf8.RuneCount(t); c.znakova > najviseZnakova {
		return errPredugaCelija
	}
	c.value.Write(t)
	return nil
}

func (c *citacLista) start(t xml.StartElement) {
	switch t.Name.Local {
	case "row":
		c.curRow = redakIz(t.Attr, c.curRow+1)
		c.nextCol = 0
	case "c":
		c.startCell(t.Attr)
	case "v":
		c.inV = true
	case "is":
		c.inIS = true
	case "t":
		c.inT = c.inIS
	}
}

// redakIz vraća redni broj retka iz atributa r, ili sljedeci kad ga nema
func redakIz(attrs []xml.Attr, sljedeci int) int {
	for _, a := range attrs {
		if a.Name.Local != "r" {
			continue
		}
		if n, err := strconv.Atoi(a.Value); err == nil {
			sljedeci = n - 1
		}
	}
	return sljedeci
}

// startCell određuje stupac i vrstu ćelije; bez adrese ćelija ide iza
// prethodne u retku
func (c *citacLista) startCell(attrs []xml.Attr) {
	c.cellType = ""
	c.curCol = c.nextCol
	for _, a := range attrs {
		switch a.Name.Local {
		case "r":
			if col, ok := columnIndex(a.Value); ok {
				c.curCol = col
			}
		case "t":
			c.cellType = a.Value
		}
	}
	c.nextCol = c.curCol + 1
	c.value.Reset()
	c.znakova = 0
}

func (c *citacLista) end(name string) error {
	switch name {
	case "v":
		c.inV = false
	case "t":
		c.inT = false
	case "is":
		c.inIS = false
	case "c":
		return c.endCell()
	}
	return nil
}

func (c *citacLista) endCell() error {
	if c.curRow < 0 || c.curCol < 0 {
		return nil
	}
	val := cellValue(c.cellType, c.value.String(), c.shared)
	if val == "" {
		return nil
	}
	return c.set(c.curRow, c.curCol, val)
}

// cellValue pretvara zapisanu vrijednost u tekst: zajednički tekst po
// rednom broju, logičku vrijednost u TRUE ili FALSE
func cellValue(cellType, val string, shared []string) string {
	switch cellType {
	case "s":
		if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && i >= 0 && i < len(shared) {
			return shared[i]
		}
		return ""
	case "b":
		if strings.TrimSpace(val) == "1" {
			return "TRUE"
		}
		return "FALSE"
	}
	return val
}

// set upisuje ćeliju i popunja redak praznima do nje. Ćelija izvan granica
// lista i knjiga s previše ćelija odbijaju se prije nego što se išta složi.
func (c *citacLista) set(row, col int, val string) error {
	if row >= najviseRedaka || col >= najviseStupaca {
		return fmt.Errorf("ćelija u retku %d i stupcu %d je izvan granica Excela (najviše 1.048.576 redaka i 16.384 stupca)", row+1, col+1)
	}
	duljina := 0
	if row < len(c.rows) {
		duljina = len(c.rows[row])
	}
	if err := c.ostalo.potrosi(max(row+1-len(c.rows), 0)+max(col+1-duljina, 0), len(val)); err != nil {
		return err
	}
	for len(c.rows) <= row {
		c.rows = append(c.rows, nil)
	}
	if n := col + 1 - len(c.rows[row]); n > 0 {
		c.rows[row] = append(c.rows[row], make([]string, n)...)
	}
	c.rows[row][col] = val
	return nil
}

// columnIndex pretvara adresu ćelije u redni broj stupca: "A1" → 0, "K7" → 10.
// Za adresu iza zadnjeg stupca Excela vraća broj veći od zadnjeg, bez
// daljnjeg zbrajanja, pa ga set odbije.
func columnIndex(ref string) (int, bool) {
	n := 0
	seen := false
	for _, r := range ref {
		z, ok := slovoStupca(r)
		if !ok {
			break
		}
		seen = true
		if n <= najviseStupaca {
			n = n*26 + z
		}
	}
	if !seen {
		return 0, false
	}
	return n - 1, true
}

// slovoStupca vraća vrijednost slova u adresi stupca: A ili a → 1, Z → 26
func slovoStupca(r rune) (int, bool) {
	switch {
	case r >= 'A' && r <= 'Z':
		return int(r-'A') + 1, true
	case r >= 'a' && r <= 'z':
		return int(r-'a') + 1, true
	}
	return 0, false
}

// Number čita broj iz ćelije; prihvaća i zarez kao decimalni znak
func Number(cell string) (float64, bool) {
	s := strings.TrimSpace(cell)
	if s == "" {
		return 0, false
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v, true
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}
