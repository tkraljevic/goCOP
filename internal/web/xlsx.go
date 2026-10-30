package web

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Čitanje .xlsx tablica bez vanjske knjižnice: .xlsx je zip s XML-om, pa je
// dovoljno ono što Go već ima. Stara .xls (BIFF) se ne čita — za nju se
// datoteka spremi kao .xlsx.
//
// Čita se samo ono što treba za uvoz očitanja: prvi list, vrijednosti ćelija
// kao tekst. Oblikovanje, formule i stilovi se ne diraju.

// procitajXLSX vraća retke prvog lista kao tekst.
func procitajXLSX(sadrzaj []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(sadrzaj), int64(len(sadrzaj)))
	if err != nil {
		return nil, fmt.Errorf("datoteka nije .xlsx: %w", err)
	}
	if err := bezPonovljenihDijelova(zr.File); err != nil {
		return nil, err
	}
	var sst []string
	if f := dioZipa(zr.File, "xl/sharedStrings.xml"); f != nil {
		if sst, err = tablicaIzZipa(f); err != nil {
			return nil, err
		}
	}
	list := prviList(zr.File)
	if list == nil {
		return nil, fmt.Errorf("u datoteci nema nijednog lista")
	}
	listXML, err := citajIzZipa(list)
	if err != nil {
		return nil, err
	}
	return redciIzLista(listXML, sst)
}

// dioZipa vraća dio zadanog naziva, ili nil
func dioZipa(dijelovi []*zip.File, ime string) *zip.File {
	for _, f := range dijelovi {
		if f.Name == ime {
			return f
		}
	}
	return nil
}

// prviList vraća prvi list po nazivu (sheet1.xml prije sheet2.xml), ili nil
func prviList(dijelovi []*zip.File) *zip.File {
	var prvi *zip.File
	for _, f := range dijelovi {
		if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		if prvi == nil || f.Name < prvi.Name {
			prvi = f
		}
	}
	return prvi
}

// najviseDijelaXLSX je najveći dio .xlsx-a nakon raspakiranja
const najviseDijelaXLSX = 64 << 20

func citajIzZipa(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return procitajDatoteku(rc, najviseDijelaXLSX)
}

// tablicaIzZipa čita zajedničku tablicu nizova izravno iz zipa, bez
// spremanja cijelog dijela u memoriju
func tablicaIzZipa(f *zip.File) ([]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return nizoviIzSST(&ograniceniDio{r: rc, ostalo: najviseDijelaXLSX + 1})
}

var errPrevelikDio = fmt.Errorf("datoteka je veća od %s nakon raspakiranja", opisVelicine(najviseDijelaXLSX))

// ograniceniDio čita dio zipa i vraća grešku kad dio prijeđe granicu,
// umjesto da ga tiho skrati; ostalo je granica uvećana za jedan bajt
type ograniceniDio struct {
	r      io.Reader
	ostalo int64
}

func (o *ograniceniDio) Read(p []byte) (int, error) {
	if int64(len(p)) > o.ostalo {
		p = p[:o.ostalo]
	}
	n, err := o.r.Read(p)
	if o.ostalo -= int64(n); o.ostalo <= 0 {
		return n, errPrevelikDio
	}
	return n, err
}

// bezPonovljenihDijelova odbija .xlsx u kojem se isti dio pojavljuje više
// puta; Excel takvu datoteku ne sprema
func bezPonovljenihDijelova(dijelovi []*zip.File) error {
	vidjeno := make(map[string]bool, len(dijelovi))
	for _, f := range dijelovi {
		if vidjeno[f.Name] {
			return fmt.Errorf("datoteka nije ispravan .xlsx: dio %s se ponavlja", f.Name)
		}
		vidjeno[f.Name] = true
	}
	return nil
}

// nizoviIzSST čita zajedničku tablicu nizova, zapis po zapis. Tekst je u
// <t>, a u obojenom tekstu razlomljen na više <r><t>, pa se dijelovi
// spajaju. Neispravna tablica daje prazne nizove, kao i prije; tablica s
// više zapisa nego što list smije imati ćelija, zapis dulji nego što Excel
// dopušta ili tablica veća od najviseDijelaXLSX odbija se.
func nizoviIzSST(r io.Reader) ([]string, error) {
	dec := xml.NewDecoder(r)
	c := &citacSST{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return c.out, nil
		}
		if errors.Is(err, errPrevelikDio) {
			return nil, err
		}
		if err != nil {
			return nil, nil
		}
		if err := c.korak(tok); err != nil {
			return nil, err
		}
	}
}

// citacSST slaže zapise zajedničke tablice nizova dok je čita
type citacSST struct {
	out      []string
	dubina   int  // dubina unutar <si>; 0 je izvan zapisa
	uR       bool // unutar <r> izravno u <si>
	uT       bool
	izravno  bool // zapis ima <t> izravno u <si>
	tekst    strings.Builder
	dijelovi strings.Builder
	znakova  int
}

func (c *citacSST) korak(tok xml.Token) error {
	switch t := tok.(type) {
	case xml.StartElement:
		c.pocetak(t.Name.Local)
	case xml.CharData:
		return c.znakovi(t)
	case xml.EndElement:
		return c.kraj(t.Name.Local)
	}
	return nil
}

func (c *citacSST) pocetak(ime string) {
	switch {
	case c.dubina > 0:
		c.dubina++
		c.unutarZapisa(ime)
	case ime == "si":
		c.dubina = 1
		c.tekst.Reset()
		c.dijelovi.Reset()
		c.izravno, c.znakova = false, 0
	}
}

// unutarZapisa prati gdje je tekst: <t> izravno u <si> ili <t> u <r>;
// ostalo (izgovor, oblikovanje) se preskače
func (c *citacSST) unutarZapisa(ime string) {
	switch {
	case ime == "r" && c.dubina == 2:
		c.uR = true
	case ime != "t":
	case c.dubina == 2:
		c.uT, c.izravno = true, true
	case c.dubina == 3 && c.uR:
		c.uT = true
	}
}

func (c *citacSST) znakovi(t xml.CharData) error {
	if !c.uT {
		return nil
	}
	if c.znakova += utf8.RuneCount(t); c.znakova > najviseZnakovaCelije {
		return errPredugaCelija
	}
	if c.uR && c.dubina == 3 {
		c.dijelovi.Write(t)
		return nil
	}
	c.tekst.Write(t)
	return nil
}

func (c *citacSST) kraj(ime string) error {
	if c.dubina == 0 {
		return nil
	}
	c.dubina--
	switch {
	case c.dubina == 0:
		return c.zapis()
	case ime == "t":
		c.uT = false
	case ime == "r" && c.dubina == 1:
		c.uR = false
	}
	return nil
}

// zapis dodaje završeni <si>: izravni tekst kad ga ima, inače spojene
// dijelove obojenog teksta
func (c *citacSST) zapis() error {
	if len(c.out) >= najviseCelijaXLSX {
		return errPrevelikaTablica
	}
	if c.izravno {
		c.out = append(c.out, c.tekst.String())
		return nil
	}
	c.out = append(c.out, c.dijelovi.String())
	return nil
}

var reCelija = regexp.MustCompile(`^([A-Z]+)(\d+)$`)

// Granice lista su Excelove: 16.384 stupca (do XFD) i 1.048.576 redaka.
const (
	najviseStupacaXLSX = 16384
	najviseRedakaXLSX  = 1 << 20
	// najviseCelijaXLSX je koliko ćelija čitač smije složiti iz lista,
	// zajedno s praznima koje se popunjavaju do zadnje ispunjene u retku
	najviseCelijaXLSX = 4_000_000
	// najviseZnakovaCelije je najdulji tekst ćelije koji Excel dopušta
	najviseZnakovaCelije = 32767
	// najviseTekstaXLSX je koliko teksta svih ćelija lista zajedno čitač
	// smije složiti, u bajtovima; ćelije koje pokazuju na isti zapis
	// zajedničke tablice broje se svaka posebno
	najviseTekstaXLSX = 64 << 20
)

var (
	errPrevelikaTablica = errors.New("tablica je prevelika (više od 4 milijuna ćelija); podijelite je na manje datoteke")
	errPredugaCelija    = errors.New("ćelija ima više od 32.767 znakova, koliko Excel dopušta")
	errPreviseTeksta    = errors.New("tablica ima previše teksta (više od 64 MB); podijelite je na manje datoteke")
)

// celijaXLSX je jedna ćelija lista: adresa, vrsta i vrijednost
type celijaXLSX struct {
	R  string `xml:"r,attr"`
	T  string `xml:"t,attr"`
	V  string `xml:"v"`
	IS string `xml:"is>t"`
}

// redciIzLista pretvara list u retke teksta, poštujući prazne ćelije: stupac
// se određuje iz oznake ćelije (A1, C7), pa prazna ćelija ostaje prazna
// umjesto da pomakne ostatak retka. List se čita ćeliju po ćeliju, a
// ćelija izvan granica Excela i list s previše ćelija odbijaju se prije
// nego što se išta složi.
func redciIzLista(b []byte, sst []string) ([][]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	c := &citacListaXLSX{sst: sst}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return c.out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("list nije čitljiv: %w", err)
		}
		if err := c.korak(dec, tok); err != nil {
			return nil, err
		}
	}
}

// citacListaXLSX slaže retke lista dok ga čita
type citacListaXLSX struct {
	sst    []string
	out    [][]string
	celija int // koliko je ćelija složeno, s praznima
	teksta int // koliko je bajtova teksta složeno
	uListu bool
	uRetku bool
}

func (c *citacListaXLSX) korak(dec *xml.Decoder, tok xml.Token) error {
	switch t := tok.(type) {
	case xml.StartElement:
		return c.pocetak(dec, t)
	case xml.EndElement:
		switch t.Name.Local {
		case "sheetData":
			c.uListu = false
		case "row":
			c.uRetku = false
		}
	}
	return nil
}

func (c *citacListaXLSX) pocetak(dec *xml.Decoder, se xml.StartElement) error {
	switch {
	case se.Name.Local == "sheetData":
		c.uListu = true
	case se.Name.Local == "row" && c.uListu:
		return c.noviRedak()
	case se.Name.Local == "c" && c.uRetku:
		var cel celijaXLSX
		if err := dec.DecodeElement(&cel, &se); err != nil {
			return fmt.Errorf("list nije čitljiv: %w", err)
		}
		return c.upisi(cel)
	}
	return nil
}

func (c *citacListaXLSX) noviRedak() error {
	if len(c.out) >= najviseRedakaXLSX {
		return fmt.Errorf("tablica ima više redaka nego što Excel dopušta (1.048.576)")
	}
	if err := c.potrosi(1); err != nil {
		return err
	}
	c.out = append(c.out, nil)
	c.uRetku = true
	return nil
}

// upisi stavlja vrijednost ćelije u njezin stupac zadnjeg retka i popunja
// redak praznima do nje
func (c *citacListaXLSX) upisi(cel celijaXLSX) error {
	i, ok := stupacIz(cel.R)
	if !ok {
		return fmt.Errorf("ćelija %s je izvan granica Excela (najviše 1.048.576 redaka i 16.384 stupca)", cel.R)
	}
	redak := &c.out[len(c.out)-1]
	if n := i + 1 - len(*redak); n > 0 {
		if err := c.potrosi(n); err != nil {
			return err
		}
		*redak = append(*redak, make([]string, n)...)
	}
	v := vrijednostCelije(cel.T, cel.V, cel.IS, c.sst)
	if utf8.RuneCountInString(v) > najviseZnakovaCelije {
		return errPredugaCelija
	}
	if c.teksta += len(v); c.teksta > najviseTekstaXLSX {
		return errPreviseTeksta
	}
	(*redak)[i] = v
	return nil
}

func (c *citacListaXLSX) potrosi(n int) error {
	if c.celija+n > najviseCelijaXLSX {
		return errPrevelikaTablica
	}
	c.celija += n
	return nil
}

// stupacIz pretvara slovnu oznaku stupca u redni broj: A→0, B→1, AA→26.
// Ćelija bez oznake ide u prvi stupac. Oznaka iza stupca XFD ili retka
// 1.048.576 nije ispravna.
func stupacIz(oznaka string) (int, bool) {
	m := reCelija.FindStringSubmatch(oznaka)
	if m == nil {
		return 0, true
	}
	if len(m[1]) > 3 {
		return 0, false
	}
	n := 0
	for _, z := range m[1] {
		n = n*26 + int(z-'A') + 1
	}
	redak, err := strconv.Atoi(m[2])
	if err != nil || n > najviseStupacaXLSX || redak > najviseRedakaXLSX {
		return 0, false
	}
	return n - 1, true
}

func vrijednostCelije(tip, v, is string, sst []string) string {
	switch tip {
	case "s":
		if i, err := strconv.Atoi(v); err == nil && i >= 0 && i < len(sst) {
			return sst[i]
		}
		return ""
	case "inlineStr":
		return is
	}
	return v
}

// xlsxUOcitanja pretvara list u redke koje razumije čitač zalijepljenog
// ispisa. Traži se zaglavlje „Vrijeme očitanja“, pa stupac vrijednosti — prvi
// s ispunjenim naslovom nakon rednog broja. Prazne ćelije se preskaču: to su
// sati koje preuzimanje nije stiglo, a ne greške.
func xlsxUOcitanja(redci [][]string) (string, string, error) {
	zaglavlje, postaja := -1, ""
	for i, r := range redci {
		for _, c := range r {
			if strings.Contains(strings.ToLower(c), "vrijeme očitanja") {
				zaglavlje = i
				break
			}
		}
		if zaglavlje >= 0 {
			for j := len(r) - 1; j >= 0; j-- {
				if s := strings.TrimSpace(r[j]); s != "" && !strings.EqualFold(s, "Rbr.") &&
					!strings.Contains(strings.ToLower(s), "vrijeme") {
					postaja = s
				}
			}
			break
		}
	}
	if zaglavlje < 0 {
		return "", "", fmt.Errorf("u tablici nema stupca „Vrijeme očitanja“ — je li to ispis s letve?")
	}
	stupacVrijednosti := 2
	if len(redci[zaglavlje]) > 2 {
		for j := 2; j < len(redci[zaglavlje]); j++ {
			if strings.TrimSpace(redci[zaglavlje][j]) != "" {
				stupacVrijednosti = j
				break
			}
		}
	}

	var sb strings.Builder
	for _, r := range redci[zaglavlje+1:] {
		if len(r) == 0 || strings.TrimSpace(r[0]) == "" {
			continue
		}
		if len(r) <= stupacVrijednosti {
			continue
		}
		v := strings.TrimSpace(r[stupacVrijednosti])
		if v == "" {
			continue // sat koji preuzimanje nije stiglo
		}
		sb.WriteString(strings.TrimSpace(r[0]))
		sb.WriteByte('\t')
		sb.WriteString(v)
		sb.WriteByte('\n')
	}
	if sb.Len() == 0 {
		return "", postaja, fmt.Errorf("u tablici nema nijedne vrijednosti")
	}
	return sb.String(), postaja, nil
}
