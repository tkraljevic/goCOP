// Package xlsx čita vrijednosti ćelija iz Excelove radne knjige.
//
// Radna knjiga je zip s XML-om; nama trebaju samo nazivi listova i tekst
// ćelija, pa se ne uvozi vanjska knjižnica. Oblikovanje, formule i stilovi
// se preskaču — čita se ono što je Excel zadnji put izračunao.
package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
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
		rows, err := readSheet(f, shared)
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

func openXML(f *zip.File) (io.ReadCloser, error) {
	if f == nil {
		return nil, nil
	}
	return f.Open()
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
// <t> u jednom <si>, pa se spajaju
func readSharedStrings(f *zip.File) ([]string, error) {
	rc, err := openXML(f)
	if err != nil {
		return nil, err
	}
	if rc == nil {
		return nil, nil
	}
	defer rc.Close()

	var out []string
	var cur strings.Builder
	inSI, inT := false, false
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "t":
				inT = inSI
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "si":
				out = append(out, cur.String())
				inSI = false
			}
		}
	}
	return out, nil
}

// readSheet čita ćelije lista u pravokutnik redaka
func readSheet(f *zip.File, shared []string) ([][]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var rows [][]string
	set := func(row, col int, val string) {
		for len(rows) <= row {
			rows = append(rows, nil)
		}
		for len(rows[row]) <= col {
			rows[row] = append(rows[row], "")
		}
		rows[row][col] = val
	}

	dec := xml.NewDecoder(rc)
	curRow, curCol := -1, -1
	nextCol := 0
	var cellType string
	var value strings.Builder
	inV, inIS, inT := false, false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				curRow++
				for _, a := range t.Attr {
					if a.Name.Local == "r" {
						if n, err := strconv.Atoi(a.Value); err == nil {
							curRow = n - 1
						}
					}
				}
				nextCol = 0
			case "c":
				cellType = ""
				curCol = nextCol
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						if c, ok := columnIndex(a.Value); ok {
							curCol = c
						}
					case "t":
						cellType = a.Value
					}
				}
				nextCol = curCol + 1
				value.Reset()
			case "v":
				inV = true
			case "is":
				inIS = true
			case "t":
				inT = inIS
			}
		case xml.CharData:
			if inV || inT {
				value.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "is":
				inIS = false
			case "c":
				if curRow < 0 || curCol < 0 {
					continue
				}
				val := value.String()
				switch cellType {
				case "s":
					if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && i >= 0 && i < len(shared) {
						val = shared[i]
					} else {
						val = ""
					}
				case "b":
					if strings.TrimSpace(val) == "1" {
						val = "TRUE"
					} else {
						val = "FALSE"
					}
				}
				if val != "" {
					set(curRow, curCol, val)
				}
			}
		}
	}
	return rows, nil
}

// columnIndex pretvara adresu ćelije u redni broj stupca: "A1" → 0, "K7" → 10
func columnIndex(ref string) (int, bool) {
	n := 0
	seen := false
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' {
			n = n*26 + int(r-'A') + 1
			seen = true
			continue
		}
		if r >= 'a' && r <= 'z' {
			n = n*26 + int(r-'a') + 1
			seen = true
			continue
		}
		break
	}
	if !seen {
		return 0, false
	}
	return n - 1, true
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
