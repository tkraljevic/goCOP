package web

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"gocop/internal/models"
)

// napraviXLSX slaže najmanju datoteku koja liči na letvin ispis: zajednička
// tablica nizova i jedan list.
func napraviXLSX(t *testing.T, redci [][]string) []byte {
	t.Helper()
	var nizovi []string
	indeks := map[string]int{}
	si := func(v string) int {
		if i, ok := indeks[v]; ok {
			return i
		}
		indeks[v] = len(nizovi)
		nizovi = append(nizovi, v)
		return len(nizovi) - 1
	}
	stupac := func(i int) string {
		s := ""
		for i++; i > 0; {
			i--
			s = string(rune('A'+i%26)) + s
			i /= 26
		}
		return s
	}
	var sd strings.Builder
	for r, red := range redci {
		sd.WriteString(`<row r="` + itoa(r+1) + `">`)
		for c, v := range red {
			if v == "" {
				continue
			}
			ref := stupac(c) + itoa(r+1)
			if jeBroj(v) {
				sd.WriteString(`<c r="` + ref + `"><v>` + v + `</v></c>`)
			} else {
				sd.WriteString(`<c r="` + ref + `" t="s"><v>` + itoa(si(v)) + `</v></c>`)
			}
		}
		sd.WriteString(`</row>`)
	}
	var sst strings.Builder
	sst.WriteString(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, n := range nizovi {
		sst.WriteString(`<si><t>` + n + `</t></si>`)
	}
	sst.WriteString(`</sst>`)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	dodaj := func(ime, sadrzaj string) {
		w, err := zw.Create(ime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(sadrzaj)); err != nil {
			t.Fatal(err)
		}
	}
	dodaj("xl/sharedStrings.xml", sst.String())
	dodaj("xl/worksheets/sheet1.xml",
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`+
			sd.String()+`</sheetData></worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func jeBroj(s string) bool {
	if s == "" {
		return false
	}
	for i, z := range s {
		if z == '-' && i == 0 {
			continue
		}
		if z != '.' && (z < '0' || z > '9') {
			return false
		}
	}
	return true
}

// Excel ispis s letve mora se pročitati kao i zalijepljeni tekst: prazni sati
// se preskaču, a ne prijavljuju kao greška — to su sati koje preuzimanje nije
// stiglo.
func TestCitanjeExcelIspisaSLetve(t *testing.T) {
	x := napraviXLSX(t, [][]string{
		{"Vodostaj za razdoblje 08.09.2026. - 08.09.2026."},
		{"Vrijeme očitanja", "Rbr.", "Dunav - Batina (DHMZ)", "", "", "Vodostaj manji od minimuma"},
		{"08.09.2026. 00:00 ", "0", "-123"},
		{"08.09.2026. 01:00 ", "1", "-123"},
		{"08.09.2026. 05:00 ", "5", "-127"},
		{"08.09.2026. 12:00 ", "12", ""}, // sat koji preuzimanje nije stiglo
		{"08.09.2026. 23:00 ", "23", ""},
	})

	redci, err := procitajXLSX(x)
	if err != nil {
		t.Fatalf("čitanje .xlsx: %v", err)
	}
	if len(redci) != 7 {
		t.Fatalf("redaka %d, očekivano 7", len(redci))
	}
	if redci[1][2] != "Dunav - Batina (DHMZ)" {
		t.Errorf("zaglavlje stupca %q", redci[1][2])
	}

	tekst, postaja, err := xlsxUOcitanja(redci)
	if err != nil {
		t.Fatalf("pretvorba: %v", err)
	}
	if !strings.Contains(postaja, "Batina") {
		t.Errorf("postaja %q", postaja)
	}
	if n := strings.Count(strings.TrimSpace(tekst), "\n") + 1; n != 3 {
		t.Errorf("prepoznato %d redaka, očekivano 3 (prazni sati se preskaču)", n)
	}

	ocit, satni, err := citajZalijepljeno(tekst)
	if err != nil {
		t.Fatalf("čitanje očitanja: %v", err)
	}
	if !satni || len(ocit) != 3 {
		t.Fatalf("očitanja %d, satni %v", len(ocit), satni)
	}
	if ocit[0].Vrijedi != -123 || ocit[2].Vrijedi != -127 {
		t.Errorf("vrijednosti %v %v", ocit[0].Vrijedi, ocit[2].Vrijedi)
	}
	// oblik 00:00 iz Excela i 00 h sa zaslona daju isti sat
	if l := ocit[0].Kad.In(models.Zagreb); l.Hour() != 0 || l.Day() != 8 {
		t.Errorf("prvi sat %v", l)
	}

	// tablica bez zaglavlja se odbija, umjesto da vrati prazno
	bez := napraviXLSX(t, [][]string{{"nešto drugo"}, {"1", "2"}})
	r2, _ := procitajXLSX(bez)
	if _, _, err := xlsxUOcitanja(r2); err == nil {
		t.Error("tablica bez „Vrijeme očitanja“ mora biti odbijena")
	}
}

// Excel piše brojeve s decimalnom točkom (-123.0), a zaslon letve bez nje
// (-123). Oboje mora dati isti cijeli broj centimetara.
func TestExcelBrojeviSTockom(t *testing.T) {
	x := napraviXLSX(t, [][]string{
		{"Vrijeme očitanja", "Rbr.", "Dunav - Batina (DHMZ)"},
		{"08.09.2026. 00:00 ", "0", "-123.0"},
		{"08.09.2026. 01:00 ", "1", "-127.0"},
	})
	redci, err := procitajXLSX(x)
	if err != nil {
		t.Fatal(err)
	}
	tekst, _, err := xlsxUOcitanja(redci)
	if err != nil {
		t.Fatal(err)
	}
	ocit, _, err := citajZalijepljeno(tekst)
	if err != nil {
		t.Fatal(err)
	}
	if len(ocit) != 2 {
		t.Fatalf("očitanja %d", len(ocit))
	}
	for i, ocekivano := range []float64{-123, -127} {
		if ocit[i].Greska != "" {
			t.Errorf("redak %d: %s", i, ocit[i].Greska)
		}
		if ocit[i].Vrijedi != ocekivano {
			t.Errorf("redak %d: %v, očekivano %v", i, ocit[i].Vrijedi, ocekivano)
		}
	}
}
