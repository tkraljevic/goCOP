package web

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
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
	var sst []string
	var listXML []byte
	najniziList := ""
	for _, f := range zr.File {
		switch {
		case f.Name == "xl/sharedStrings.xml":
			b, err := citajIzZipa(f)
			if err != nil {
				return nil, err
			}
			sst = nizoviIzSST(b)
		case strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml"):
			// prvi list po nazivu: sheet1.xml prije sheet2.xml
			if najniziList == "" || f.Name < najniziList {
				b, err := citajIzZipa(f)
				if err != nil {
					return nil, err
				}
				najniziList, listXML = f.Name, b
			}
		}
	}
	if listXML == nil {
		return nil, fmt.Errorf("u datoteci nema nijednog lista")
	}
	return redciIzLista(listXML, sst)
}

func citajIzZipa(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 64<<20))
}

// nizoviIzSST čita zajedničku tablicu nizova. Tekst je u <t>, a u obojenom
// tekstu razlomljen na više <r><t> — pa se dijelovi spajaju.
func nizoviIzSST(b []byte) []string {
	type t struct {
		Text string `xml:",chardata"`
	}
	type si struct {
		T *t  `xml:"t"`
		R []t `xml:"r>t"`
	}
	var sst struct {
		SI []si `xml:"si"`
	}
	if err := xml.Unmarshal(b, &sst); err != nil {
		return nil
	}
	out := make([]string, 0, len(sst.SI))
	for _, s := range sst.SI {
		if s.T != nil {
			out = append(out, s.T.Text)
			continue
		}
		var sb strings.Builder
		for _, r := range s.R {
			sb.WriteString(r.Text)
		}
		out = append(out, sb.String())
	}
	return out
}

var reCelija = regexp.MustCompile(`^([A-Z]+)(\d+)$`)

// redciIzLista pretvara list u retke teksta, poštujući prazne ćelije: stupac
// se određuje iz oznake ćelije (A1, C7), pa prazna ćelija ostaje prazna
// umjesto da pomakne ostatak retka.
func redciIzLista(b []byte, sst []string) ([][]string, error) {
	type c struct {
		R  string `xml:"r,attr"`
		T  string `xml:"t,attr"`
		V  string `xml:"v"`
		IS string `xml:"is>t"`
	}
	type row struct {
		C []c `xml:"c"`
	}
	var list struct {
		Redci []row `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("list nije čitljiv: %w", err)
	}
	var out [][]string
	for _, r := range list.Redci {
		var redak []string
		for _, cel := range r.C {
			i := stupacIz(cel.R)
			for len(redak) <= i {
				redak = append(redak, "")
			}
			redak[i] = vrijednostCelije(cel.T, cel.V, cel.IS, sst)
		}
		out = append(out, redak)
	}
	return out, nil
}

// stupacIz pretvara slovnu oznaku stupca u redni broj: A→0, B→1, AA→26.
func stupacIz(oznaka string) int {
	m := reCelija.FindStringSubmatch(oznaka)
	if m == nil {
		return 0
	}
	n := 0
	for _, z := range m[1] {
		n = n*26 + int(z-'A') + 1
	}
	return n - 1
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
