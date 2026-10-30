package web

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"gocop/internal/raspakiraj"
)

// xlsxOdStavki slaže .xlsx od zadanih stavki, redom kojim su zadane.
func xlsxOdStavki(t *testing.T, stavke ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, s := range stavke {
		w, err := zw.Create(s[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(s[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const listXLSX = `<worksheet><sheetData><row><c r="A1" t="s"><v>0</v></c><c r="B1"><v>7</v></c></row></sheetData></worksheet>`
const sstXLSX = `<sst><si><t>Batina</t></si></sst>`

// granicaXLSXOd postavlja granicu .xlsx za jedan test.
func granicaXLSXOd(t *testing.T, stavki int, bajtova int64) {
	t.Helper()
	stara := granicaXLSX
	granicaXLSX = func() *raspakiraj.Granica { return raspakiraj.Nova(stavki, bajtova) }
	t.Cleanup(func() { granicaXLSX = stara })
}

// Od .xlsx se čitaju samo zajednička tablica nizova i prvi list; ostali
// listovi, ma koliko ih bilo i kojim god redom stajali, ne troše granicu.
func TestXLSXCitaSamoPrviList(t *testing.T) {
	granicaXLSXOd(t, 20, int64(len(listXLSX)+len(sstXLSX)))
	stavke := [][2]string{}
	for i := 9; i >= 2; i-- {
		stavke = append(stavke, [2]string{fmt.Sprintf("xl/worksheets/sheet%d.xml", i), strings.Repeat("x", 200)})
	}
	stavke = append(stavke, [2]string{"xl/worksheets/sheet1.xml", listXLSX}, [2]string{"xl/sharedStrings.xml", sstXLSX})
	redci, err := procitajXLSX(xlsxOdStavki(t, stavke...))
	if err != nil || len(redci) != 1 || redci[0][0] != "Batina" || redci[0][1] != "7" {
		t.Fatalf("prvi list: %v, %v", redci, err)
	}
}

// .xlsx s previše stavki ili s više raspakiranih bajtova nego što granica
// prima odbija se; ista tablica uz dovoljnu granicu čita se.
func TestXLSXGranica(t *testing.T) {
	x := xlsxOdStavki(t, [2]string{"xl/sharedStrings.xml", sstXLSX}, [2]string{"xl/worksheets/sheet1.xml", listXLSX},
		[2]string{"xl/styles.xml", "<styleSheet/>"})
	granicaXLSXOd(t, 2, 1<<20)
	if _, err := procitajXLSX(x); err == nil || !strings.Contains(err.Error(), "najviše 2") {
		t.Errorf("previše stavki: %v", err)
	}
	granicaXLSXOd(t, 3, int64(len(listXLSX)+len(sstXLSX)-1))
	if _, err := procitajXLSX(x); err == nil || !strings.Contains(err.Error(), "kad se raspakiraju") {
		t.Errorf("raspakirano preveliko: %v", err)
	}
	granicaXLSXOd(t, 1, 1<<20)
	if _, err := procitajXLSX(xlsxOdStavki(t, [2]string{"xl/styles.xml", "<styleSheet/>"})); err == nil ||
		!strings.Contains(err.Error(), "nema nijednog lista") {
		t.Errorf("bez lista: %v", err)
	}
	granicaXLSXOd(t, 3, int64(len(listXLSX)))
	if _, err := procitajXLSX(x); err == nil || !strings.Contains(err.Error(), "kad se raspakiraju") {
		t.Errorf("tablica nizova troši granicu prije lista: %v", err)
	}
	granicaXLSXOd(t, 3, int64(len(listXLSX)+len(sstXLSX)))
	if redci, err := procitajXLSX(x); err != nil || len(redci) != 1 {
		t.Errorf("unutar granice: %v, %v", redci, err)
	}
}

// Zadana granica prima obične tablice, a najviše 64 MB po stavci.
func TestZadanaGranicaXLSX(t *testing.T) {
	x := napraviXLSX(t, [][]string{{"a", "1"}})
	if _, err := procitajXLSX(x); err != nil {
		t.Errorf("obična tablica: %v", err)
	}
	if najviseStavkeXLSX != 64<<20 || granicaXLSX() == nil {
		t.Errorf("granica stavke %d", najviseStavkeXLSX)
	}
}
