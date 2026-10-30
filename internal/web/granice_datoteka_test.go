package web

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
)

// Granice datoteka koje korisnik šalje: tablica Excela i fotografija se
// odbijaju porukom kad bi u memoriji zauzele previše, a obične prolaze.

// xlsxSListom slaže .xlsx s jednim listom zadanog sadržaja sheetData
func xlsxSListom(t *testing.T, sheetData string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`<worksheet><sheetData>` + sheetData + `</sheetData></worksheet>`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// alociranoZa vraća koliko je bajtova memorije zauzeo poziv
func alociranoZa(f func()) uint64 {
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	f()
	runtime.ReadMemStats(&m1)
	return m1.TotalAlloc - m0.TotalAlloc
}

// Oznaka stupca vrijedi do XFD i retka 1.048.576, kao u Excelu.
func TestStupacIzGranice(t *testing.T) {
	for oznaka, stupac := range map[string]int{"A1": 0, "AA7": 26, "XFD1": 16383, "A1048576": 0, "": 0} {
		if i, ok := stupacIz(oznaka); !ok || i != stupac {
			t.Errorf("%q → %d (%v), očekivano %d", oznaka, i, ok, stupac)
		}
	}
	for _, oznaka := range []string{"XFE1", "ZZZZ1", "ZZZZZZZ1", "A1048577", "A99999999999999999999"} {
		if _, ok := stupacIz(oznaka); ok {
			t.Errorf("%q je prošla kao ćelija unutar lista", oznaka)
		}
	}
}

// Ćelija iza zadnjeg stupca odbija se porukom, bez slaganja retka do nje;
// ćelija u zadnjem stupcu i dalje se čita.
func TestListSCelijomIzaZadnjegStupca(t *testing.T) {
	for _, oznaka := range []string{"XFE1", "ZZZZZ1", "ZZZZZZZ1"} {
		var err error
		zauzeto := alociranoZa(func() {
			_, err = procitajXLSX(xlsxSListom(t, `<row><c r="`+oznaka+`"><v>1</v></c></row>`))
		})
		if err == nil || !strings.Contains(err.Error(), "izvan granica Excela") {
			t.Errorf("%s: %v", oznaka, err)
		}
		if zauzeto > 8<<20 {
			t.Errorf("%s: odbijanje je zauzelo %d MB", oznaka, zauzeto>>20)
		}
	}
	redci, err := procitajXLSX(xlsxSListom(t, `<row><c r="A1"><v>a</v></c><c r="XFD1"><v>z</v></c></row>`))
	if err != nil || len(redci) != 1 || len(redci[0]) != 16384 || redci[0][0] != "a" || redci[0][16383] != "z" {
		t.Fatalf("zadnji stupac: %v", err)
	}
}

// List koji bi s praznim ćelijama imao više od četiri milijuna ćelija
// odbija se, a ne skraćuje.
func TestPrevelikaTablicaSeOdbija(t *testing.T) {
	var sd strings.Builder
	for range najviseCelijaXLSX/najviseStupacaXLSX + 1 {
		sd.WriteString(`<row><c r="XFD1"><v>1</v></c></row>`)
	}
	_, err := procitajXLSX(xlsxSListom(t, sd.String()))
	if !errors.Is(err, errPrevelikaTablica) {
		t.Errorf("prevelika tablica: %v", err)
	}
	if !strings.Contains(errPrevelikaTablica.Error(), "4 milijuna") {
		t.Error("poruka ne kaže granicu")
	}
	// i prazni redci troše granicu: 244 puna retka i 2061 prazan prelaze je
	sd.Reset()
	for range 244 {
		sd.WriteString(`<row><c r="XFD1"><v>1</v></c></row>`)
	}
	sd.WriteString(strings.Repeat(`<row/>`, najviseCelijaXLSX-244*(najviseStupacaXLSX+1)))
	if _, err := procitajXLSX(xlsxSListom(t, sd.String())); err != nil {
		t.Fatalf("točno do granice: %v", err)
	}
	sd.WriteString(`<row/>`)
	if _, err := procitajXLSX(xlsxSListom(t, sd.String())); !errors.Is(err, errPrevelikaTablica) {
		t.Errorf("redak preko granice: %v", err)
	}
}

// List s više redaka nego što Excel dopušta odbija se.
func TestListSPreviseRedaka(t *testing.T) {
	sd := strings.Repeat(`<row/>`, najviseRedakaXLSX+1)
	if _, err := procitajXLSX(xlsxSListom(t, sd)); err == nil || !strings.Contains(err.Error(), "1.048.576") {
		t.Errorf("previše redaka: %v", err)
	}
	redci, err := procitajXLSX(xlsxSListom(t, strings.Repeat(`<row/>`, 3)))
	if err != nil || len(redci) != 3 {
		t.Errorf("tri prazna retka: %d, %v", len(redci), err)
	}
}

// Redci izvan sheetData i ćelije izvan retka ne čitaju se, a neispravan
// XML u ćeliji ili listu kaže da list nije čitljiv.
func TestListCitaSamoSheetData(t *testing.T) {
	list := []byte(`<worksheet><row><c r="A1"><v>izvan</v></c></row><sheetData><c r="A1"><v>bez retka</v></c>` +
		`<row><c r="B1" t="inlineStr"><is><t>u retku</t></is></c></row></sheetData></worksheet>`)
	redci, err := redciIzLista(list, nil)
	if err != nil || len(redci) != 1 || len(redci[0]) != 2 || redci[0][1] != "u retku" {
		t.Fatalf("redci %q, %v", redci, err)
	}
	for _, los := range []string{`<worksheet><sheetData><row><c r="A1"><v>1</c></row>`, `<worksheet><sheetData><row>`} {
		if _, err := redciIzLista([]byte(los), nil); err == nil || !strings.Contains(err.Error(), "list nije čitljiv") {
			t.Errorf("%s: %v", los, err)
		}
	}
}

// Pregled uvoza niza odbija Excel s ćelijom izvan granica lista, a
// ispravan Excel i dalje prolazi.
func TestPregledNizaExcelIzvanGranica(t *testing.T) {
	o := novaOkolinaVrata(t)
	los := xlsxSListom(t, `<row><c r="ZZZZZZZ1"><v>1</v></c></row>`)
	r := viseDijelni(t, "/administracija/uvoz-niza/pregled", nil,
		datotekaObrasca{"datoteka", "primjerovo_probni_vodostaj_dnevni.xlsx", string(los)})
	html := htmlUTekst(odgovor(o.h.PregledUvoza, r, o.pero, o.peroOvl).Body.String())
	if !strings.Contains(porukaGreske(html), "izvan granica Excela") || reIdObrasca.MatchString(html) {
		t.Errorf("Excel izvan granica: %s", porukeStranice(html))
	}

	dobar := napraviXLSX(t, [][]string{{"datum", "vodostaj"}, {"2026-03-01", "131"}, {"2026-03-02", "125"}})
	r = viseDijelni(t, "/administracija/uvoz-niza/pregled", nil,
		datotekaObrasca{"datoteka", "primjerovo_probni_vodostaj_dnevni.xlsx", string(dobar)})
	html = htmlUTekst(odgovor(o.h.PregledUvoza, r, o.pero, o.peroOvl).Body.String())
	if porukaGreske(html) != "" || !reIdObrasca.MatchString(html) {
		t.Errorf("ispravan Excel: %s", porukeStranice(html))
	}
}

// xlsxSDijelovima slaže .xlsx od zadanih dijelova, redom kojim su zadani;
// isti naziv može se pojaviti više puta
func xlsxSDijelovima(t *testing.T, dijelovi ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, d := range dijelovi {
		w, err := zw.Create(d[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(d[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// xlsxSTablicom slaže .xlsx sa zajedničkom tablicom nizova i jednim listom
func xlsxSTablicom(t *testing.T, sst, sheetData string) []byte {
	t.Helper()
	return xlsxSDijelovima(t,
		[2]string{"xl/sharedStrings.xml", sst},
		[2]string{"xl/worksheets/sheet1.xml", `<worksheet><sheetData>` + sheetData + `</sheetData></worksheet>`})
}

// Zajednička tablica s više zapisa nego što list smije imati ćelija odbija
// se, a ne slaže do kraja; tablica do granice i dalje se čita.
func TestZajednickaTablicaSPreviseZapisa(t *testing.T) {
	list := `<row><c r="A1" t="s"><v>0</v></c></row>`
	if _, err := procitajXLSX(xlsxSTablicom(t, `<sst>`+strings.Repeat(`<si/>`, najviseCelijaXLSX+1)+`</sst>`, list)); !errors.Is(err, errPrevelikaTablica) {
		t.Errorf("previše zapisa: %v", err)
	}
	// tablica veća od 64 MB nakon raspakiranja odbija se, a ne skraćuje
	golema := `<sst><si><t>a</t></si>` + strings.Repeat(" ", najviseDijelaXLSX) + `</sst>`
	if _, err := procitajXLSX(xlsxSTablicom(t, golema, list)); !errors.Is(err, errPrevelikDio) || !strings.Contains(err.Error(), "64 MB") {
		t.Errorf("prevelika tablica: %v", err)
	}
	redci, err := procitajXLSX(xlsxSTablicom(t, `<sst>`+strings.Repeat(`<si><t>a</t></si>`, 1000)+`</sst>`, list))
	if err != nil || redci[0][0] != "a" {
		t.Errorf("tablica od 1000 zapisa: %q %v", redci, err)
	}
}

// Tekst ćelije smije imati najviše 32.767 znakova, koliko dopušta Excel, i
// u zajedničkoj tablici i u samoj ćeliji.
func TestPredugaCelijaSeOdbija(t *testing.T) {
	dug := strings.Repeat("ž", najviseZnakovaCelije)
	for opis, x := range map[string][2]string{
		"zajednička tablica": {`<sst><si><t>` + dug + `x</t></si></sst>`, `<row><c r="A1" t="s"><v>0</v></c></row>`},
		"obojen tekst":       {`<sst><si><r><t>` + dug + `</t></r><r><t>x</t></r></si></sst>`, `<row><c r="A1" t="s"><v>0</v></c></row>`},
		"u ćeliji":           {`<sst/>`, `<row><c r="A1" t="inlineStr"><is><t>` + dug + `x</t></is></c></row>`},
	} {
		if _, err := procitajXLSX(xlsxSTablicom(t, x[0], x[1])); !errors.Is(err, errPredugaCelija) {
			t.Errorf("%s: %v", opis, err)
		}
	}
	redci, err := procitajXLSX(xlsxSTablicom(t, `<sst><si><t>`+dug+`</t></si></sst>`, `<row><c r="A1" t="s"><v>0</v></c></row>`))
	if err != nil || redci[0][0] != dug {
		t.Errorf("ćelija od 32.767 znakova: %v", err)
	}
}

// Ćelije koje pokazuju na isti dugi zapis broje se svaka posebno: zajedno
// smiju imati najviše 64 MB teksta.
func TestPreviseTekstaUTablici(t *testing.T) {
	sst := `<sst><si><t>` + strings.Repeat("A", najviseZnakovaCelije) + `</t></si></sst>`
	celija := `<c t="s"><v>0</v></c>`
	stane := najviseTekstaXLSX / najviseZnakovaCelije
	if _, err := procitajXLSX(xlsxSTablicom(t, sst, `<row>`+strings.Repeat(celija, stane)+`</row>`)); err != nil {
		t.Fatalf("do granice: %v", err)
	}
	var redci [][]string
	var err error
	zauzeto := alociranoZa(func() {
		redci, err = procitajXLSX(xlsxSTablicom(t, sst, `<row>`+strings.Repeat(celija, stane+1)+`</row>`))
	})
	if !errors.Is(err, errPreviseTeksta) || redci != nil {
		t.Errorf("preko granice: %v", err)
	}
	if zauzeto > 64<<20 {
		t.Errorf("odbijanje je zauzelo %d MB", zauzeto>>20)
	}
}

// Dio koji se u datoteci ponavlja odbija se; Excel takvu ne sprema.
func TestPonovljeniDioXLSX(t *testing.T) {
	list := [2]string{"xl/worksheets/sheet1.xml", `<worksheet><sheetData><row><c><v>1</v></c></row></sheetData></worksheet>`}
	sst := [2]string{"xl/sharedStrings.xml", `<sst><si><t>a</t></si></sst>`}
	for opis, d := range map[string][][2]string{"tablica": {sst, sst, list}, "list": {list, list}} {
		if _, err := procitajXLSX(xlsxSDijelovima(t, d...)); err == nil || !strings.Contains(err.Error(), "se ponavlja") {
			t.Errorf("%s: %v", opis, err)
		}
	}
	if redci, err := procitajXLSX(xlsxSDijelovima(t, sst, list)); err != nil || redci[0][0] != "1" {
		t.Errorf("ispravna datoteka: %q %v", redci, err)
	}
}

// Dio zapisan sažimanjem koje program ne zna raspakirati kaže da se ne
// može pročitati.
func TestDioXLSXNepoznatogSazimanja(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(99, func(w io.Writer) (io.WriteCloser, error) { return nopZatvarac{w}, nil })
	for _, ime := range []string{"xl/sharedStrings.xml", "xl/worksheets/sheet1.xml"} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: ime, Method: 99})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(`<sst/>`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := procitajXLSX(buf.Bytes()); !errors.Is(err, zip.ErrAlgorithm) {
		t.Errorf("nepoznato sažimanje: %v", err)
	}
}

// nopZatvarac je pisač bez zatvaranja, za sažimanje koje ništa ne sažima
type nopZatvarac struct{ io.Writer }

func (nopZatvarac) Close() error { return nil }

// Zajednička tablica čita izravni tekst zapisa, a obojen tekst spaja iz
// dijelova; izgovor i oblikovanje se preskaču, a neispravna tablica daje
// prazne ćelije, kao i prije.
func TestZajednickaTablicaTekst(t *testing.T) {
	sst := `<sst count="3"><si><t>prvi</t><phoneticPr/></si>` +
		`<si><r><rPr><b/></rPr><t>dru</t></r><r><t>gi</t></r><rPh><t>izgovor</t></rPh></si>` +
		`<si><t xml:space="preserve"> treći </t></si></sst>`
	nizovi, err := nizoviIzSST(strings.NewReader(sst))
	if err != nil || len(nizovi) != 3 || nizovi[0] != "prvi" || nizovi[1] != "drugi" || nizovi[2] != " treći " {
		t.Errorf("nizovi %q, %v", nizovi, err)
	}
	if nizovi, err := nizoviIzSST(strings.NewReader(`<sst><si><t>a</si>`)); err != nil || nizovi != nil {
		t.Errorf("neispravna tablica: %q %v", nizovi, err)
	}
}

// Pregled zalijepljenog Excela odbija tablicu s previše teksta porukom, a
// ispravan ispis s letve i dalje prolazi.
func TestZalijepljeniExcelSPreviseTeksta(t *testing.T) {
	o := novaOkolinaPrava(t)
	o.h.SetUvoz(predlozakStranice(t, "uvoz_ocitanja.html"))
	id := o.st.ID.String()
	sst := `<sst><si><t>` + strings.Repeat("A", najviseZnakovaCelije) + `</t></si></sst>`
	los := xlsxSTablicom(t, sst, `<row>`+strings.Repeat(`<c t="s"><v>0</v></c>`, najviseTekstaXLSX/najviseZnakovaCelije+1)+`</row>`)
	r := viseDijelni(t, "/", nil, datotekaObrasca{"datoteka", "ispis.xlsx", string(los)})
	putanja, poruka := naLetvi(o.h.HandleZalijepiPregled, r, id, o.pero, o.peroOvl).poruka(t, "error")
	if putanja != "/readings/station/"+id || !strings.Contains(poruka, "previše teksta") {
		t.Errorf("previše teksta: %s %q", putanja, poruka)
	}

	dobar := napraviXLSX(t, [][]string{
		{"Vrijeme očitanja", "Rbr.", "Primjerica - Primjerovo (DHMZ)"},
		{"07.09.2026. 00:00 ", "0", "-118"},
	})
	r = viseDijelni(t, "/", nil, datotekaObrasca{"datoteka", "ispis.xlsx", string(dobar)})
	if odg := naLetvi(o.h.HandleZalijepiPregled, r, id, o.pero, o.peroOvl); odg.status != http.StatusOK || !strings.Contains(odg.html, "novih <strong>1</strong>") {
		t.Errorf("ispravan ispis: %d %s", odg.status, odg.location)
	}
}

// pngSMjerama vraća PNG od jedne točke kojem zaglavlje kaže zadane mjere
func pngSMjerama(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	p := b.Bytes()
	// zaglavlje IHDR: širina na 16, visina na 20, zbroj provjere na 29
	binary.BigEndian.PutUint32(p[16:], w)
	binary.BigEndian.PutUint32(p[20:], h)
	binary.BigEndian.PutUint32(p[29:], crc32.ChecksumIEEE(p[12:29]))
	return p
}
