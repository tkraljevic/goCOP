package pdfw

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// Potpisnik daje CMS (PKCS#7) potpis nad zadanim bajtovima; sažetak računa
// sam, u DER obliku, odvojen od sadržaja
type Potpisnik func(podaci []byte) ([]byte, error)

// Dodatak je ono što se dokumentu dodaje kao inkrementalna izmjena: polje
// potpisa (kad Potpisnik nije nil) ili pečat, s izgledom nacrtanim na
// zadanom mjestu stranice. Izgled se crta kao na maloj stranici širine W i
// visine H, s ishodištem u gornjem lijevom kutu; fontovi su isti kao u
// dokumentu, pa dokument mora biti iz ovog paketa i pripremljen sa
// SviZnakovi.
type Dodatak struct {
	Stranica   int     // od 1
	X, Y, W, H float64 // mjesto na stranici, od vrha
	Crtaj      func(d *Doc)
	Potpisnik  Potpisnik
	Ime        string // potpisnik, u polje /Name
	Razlog     string
	Mjesto     string
	Kad        time.Time
}

// rezervirano za CMS potpis u dokumentu, u bajtovima; ECDSA P-256 s dva
// certifikata stane u četvrtinu
const rezerva = 8192

var (
	reRoot     = regexp.MustCompile(`/Root (\d+) 0 R`)
	reSize     = regexp.MustCompile(`/Size (\d+)`)
	reInfo     = regexp.MustCompile(`/Info (\d+) 0 R`)
	rePages    = regexp.MustCompile(`/Pages (\d+) 0 R`)
	reKids     = regexp.MustCompile(`/Kids \[([^\]]*)\]`)
	reRef      = regexp.MustCompile(`(\d+) 0 R`)
	reMediaBox = regexp.MustCompile(`/MediaBox \[\s*[\d.]+\s+[\d.]+\s+([\d.]+)\s+([\d.]+)\s*\]`)
	reAnnots   = regexp.MustCompile(`/Annots \[([^\]]*)\]`)
	reAcro     = regexp.MustCompile(`/AcroForm << /Fields \[([^\]]*)\] /SigFlags 3 >>`)
	reXref     = regexp.MustCompile(`startxref\s+(\d+)\s+%%EOF\s*$`)
)

// SviZnakovi bilježi u dokumentu sav uobičajeni skup znakova (ASCII,
// hrvatska slova, crtice i navodnici), pa naknadni dodaci mogu ispisivati
// bilo koje ime istim fontom, sa širinama koje dokument nosi
func (d *Doc) SviZnakovi() {
	for rez := range d.koristeno {
		if d.koristeno[rez] == nil {
			d.koristeno[rez] = map[rune]bool{}
		}
		for r := rune(32); r < 127; r++ {
			d.koristeno[rez][r] = true
		}
		for _, r := range "čćđšžČĆĐŠŽ·–—„”“’°€" {
			d.koristeno[rez][r] = true
		}
	}
}

// objekt vraća zadnju definiciju objekta n (tijelo između "obj" i "endobj")
func objekt(pdf []byte, n int) (string, error) {
	oznaka := []byte(fmt.Sprintf("\n%d 0 obj\n", n))
	i := bytes.LastIndex(pdf, oznaka)
	if i < 0 {
		return "", fmt.Errorf("pdf: nema objekta %d", n)
	}
	od := i + len(oznaka)
	kraj := bytes.Index(pdf[od:], []byte("\nendobj"))
	if kraj < 0 {
		return "", fmt.Errorf("pdf: objekt %d nema kraja", n)
	}
	return string(pdf[od : od+kraj]), nil
}

// tekstUTF16 kodira niz kao PDF tekst u UTF-16BE s oznakom redoslijeda
func tekstUTF16(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, "%04X", u)
	}
	b.WriteString(">")
	return b.String()
}

// Dodaj dopisuje dokumentu dodatak kao inkrementalnu izmjenu: stari sadržaj
// ostaje bajt za bajt, pa raniji potpisi vrijede i dalje. Kad je Potpisnik
// zadan, dodatak je polje potpisa (PAdES, odvojeni CMS) s ByteRange preko
// cijelog dokumenta; inače je pečat s izgledom.
func Dodaj(pdf []byte, dod Dodatak) ([]byte, error) {
	if len(pdf) < 16 || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		return nil, errors.New("pdf: nije PDF")
	}
	rep := pdf
	if len(rep) > 2048 {
		rep = rep[len(rep)-2048:]
	}
	// zadnji trailer je mjerodavan: kratka ranija izmjena stane u isti rep
	// s prethodnim trailerom, pa se uzima zadnje podudaranje
	zadnji := func(re *regexp.Regexp) []byte {
		sve := re.FindAllSubmatch(rep, -1)
		if len(sve) == 0 {
			return nil
		}
		return sve[len(sve)-1][1]
	}
	m, ms, mx := zadnji(reRoot), zadnji(reSize), reXref.FindSubmatch(rep)
	if m == nil || ms == nil || mx == nil {
		return nil, errors.New("pdf: nema zaglavlja tablice objekata")
	}
	root, _ := strconv.Atoi(string(m))
	size, _ := strconv.Atoi(string(ms))
	prev, _ := strconv.Atoi(string(mx[1]))
	info := ""
	if mi := zadnji(reInfo); mi != nil {
		info = " /Info " + string(mi) + " 0 R"
	}
	katalog, err := objekt(pdf, root)
	if err != nil {
		return nil, err
	}
	mp := rePages.FindStringSubmatch(katalog)
	if mp == nil {
		return nil, errors.New("pdf: katalog bez stranica")
	}
	pagesN, _ := strconv.Atoi(mp[1])
	pages, err := objekt(pdf, pagesN)
	if err != nil {
		return nil, err
	}
	mk := reKids.FindStringSubmatch(pages)
	if mk == nil {
		return nil, errors.New("pdf: nema popisa stranica")
	}
	kids := reRef.FindAllStringSubmatch(mk[1], -1)
	if dod.Stranica < 1 || dod.Stranica > len(kids) {
		return nil, fmt.Errorf("pdf: nema stranice %d", dod.Stranica)
	}
	pageN, _ := strconv.Atoi(kids[dod.Stranica-1][1])
	page, err := objekt(pdf, pageN)
	if err != nil {
		return nil, err
	}
	visina := A4H
	if mb := reMediaBox.FindStringSubmatch(page); mb != nil {
		visina, _ = strconv.ParseFloat(mb[2], 64)
	}

	// novi objekti dobivaju brojeve od /Size nadalje
	var out bytes.Buffer
	out.Write(pdf)
	if pdf[len(pdf)-1] != '\n' {
		out.WriteByte('\n')
	}
	sljedeci := size
	pomaci := map[int]int{}
	obj := func(n int, sadrzaj string) {
		pomaci[n] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, sadrzaj)
	}
	novi := func(sadrzaj string) int {
		n := sljedeci
		sljedeci++
		obj(n, sadrzaj)
		return n
	}

	// izgled: mala stranica nacrtana istim alatima, kao obrazac XObject
	ap := 0
	if dod.Crtaj != nil && dod.W > 0 && dod.H > 0 {
		t := &Doc{W: dod.W, H: dod.H}
		t.NovaStranica()
		dod.Crtaj(t)
		xobj := ""
		for i, s := range t.slike {
			var k int
			if s.jpeg != nil {
				prostor := map[int]string{1: "/DeviceGray", 3: "/DeviceRGB", 4: "/DeviceCMYK"}[s.komp]
				if prostor == "" {
					prostor = "/DeviceRGB"
				}
				k = novi(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", s.w, s.h, prostor, len(s.jpeg), s.jpeg))
			} else {
				var z bytes.Buffer
				zw := zlib.NewWriter(&z)
				_, _ = zw.Write(s.rgb)
				_ = zw.Close()
				k = novi(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", s.w, s.h, z.Len(), z.Bytes()))
			}
			xobj += fmt.Sprintf("/Im%d %d 0 R ", i+1, k)
		}
		tok := t.tok().String()
		ap = novi(fmt.Sprintf("<< /Type /XObject /Subtype /Form /FormType 1 /BBox [0 0 %.2f %.2f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> /XObject << %s>> >> /Length %d >>\nstream\n%s\nendstream", dod.W, dod.H, xobj, len(tok), tok))
	}

	// polje potpisa ili pečat
	rect := fmt.Sprintf("[%.2f %.2f %.2f %.2f]", dod.X, visina-dod.Y-dod.H, dod.X+dod.W, visina-dod.Y)
	if ap == 0 {
		rect = "[0 0 0 0]"
	}
	apDio := ""
	if ap > 0 {
		apDio = fmt.Sprintf(" /AP << /N %d 0 R >>", ap)
	}
	kad := dod.Kad
	if kad.IsZero() {
		kad = time.Now()
	}
	datum := kad.Format("D:20060102150405-07'00'")
	polja := ""
	if ma := reAcro.FindStringSubmatch(katalog); ma != nil {
		polja = strings.TrimSpace(ma[1])
	}
	brojPolja := len(reRef.FindAllString(polja, -1))
	sig := 0
	var widget int
	if dod.Potpisnik != nil {
		sig = sljedeci
		sljedeci++
		widget = sljedeci
		sljedeci++
		obj(sig, fmt.Sprintf("<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /ETSI.CAdES.detached /ByteRange [0000000000 0000000000 0000000000 0000000000] /Contents <%s> /M (%s) /Name %s /Reason %s /Location %s >>",
			strings.Repeat("0", rezerva*2), datum, tekstUTF16(dod.Ime), tekstUTF16(dod.Razlog), tekstUTF16(dod.Mjesto)))
		obj(widget, fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Sig /T (Potpis%d) /V %d 0 R /Rect %s /F 132 /P %d 0 R%s >>", brojPolja+1, sig, rect, pageN, apDio))
	} else {
		widget = novi(fmt.Sprintf("<< /Type /Annot /Subtype /Stamp /Name /goCOP /Rect %s /F 132 /P %d 0 R /M (%s) /T %s /Contents %s%s >>", rect, pageN, datum, tekstUTF16(dod.Ime), tekstUTF16(dod.Razlog), apDio))
	}

	// stranica s dodanom bilješkom, katalog s poljem obrasca
	ref := fmt.Sprintf("%d 0 R", widget)
	if man := reAnnots.FindStringSubmatch(page); man != nil {
		page = reAnnots.ReplaceAllLiteralString(page, "/Annots [ "+strings.TrimSpace(man[1])+" "+ref+" ]")
	} else {
		page = strings.TrimSuffix(strings.TrimSpace(page), ">>") + "/Annots [ " + ref + " ] >>"
	}
	obj(pageN, page)
	if sig > 0 {
		if polja != "" {
			polja += " "
		}
		polja += ref
		acro := "/AcroForm << /Fields [ " + polja + " ] /SigFlags 3 >>"
		if reAcro.MatchString(katalog) {
			katalog = reAcro.ReplaceAllLiteralString(katalog, acro)
		} else {
			katalog = strings.TrimSuffix(strings.TrimSpace(katalog), ">>") + acro + " >>"
		}
		obj(root, katalog)
	}

	// tablica objekata za izmijenjene i nove, po odsječcima uzastopnih brojeva
	brojevi := make([]int, 0, len(pomaci))
	for n := range pomaci {
		brojevi = append(brojevi, n)
	}
	for i := range brojevi {
		for j := i + 1; j < len(brojevi); j++ {
			if brojevi[j] < brojevi[i] {
				brojevi[i], brojevi[j] = brojevi[j], brojevi[i]
			}
		}
	}
	xref := out.Len()
	out.WriteString("xref\n")
	for i := 0; i < len(brojevi); {
		j := i
		for j+1 < len(brojevi) && brojevi[j+1] == brojevi[j]+1 {
			j++
		}
		fmt.Fprintf(&out, "%d %d\n", brojevi[i], j-i+1)
		for k := i; k <= j; k++ {
			fmt.Fprintf(&out, "%010d 00000 n \n", pomaci[brojevi[k]])
		}
		i = j + 1
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R%s /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", sljedeci, root, info, prev, xref)
	rez := out.Bytes()
	if sig == 0 {
		return rez, nil
	}

	// raspon bajtova: sve osim heksadecimalnog sadržaja potpisa
	od := pomaci[sig]
	c := bytes.Index(rez[od:], []byte("/Contents <"))
	if c < 0 {
		return nil, errors.New("pdf: potpis bez sadržaja")
	}
	pocetak := od + c + len("/Contents <")
	krajHex := pocetak + rezerva*2
	br := fmt.Sprintf("[0 %d %d %d]", pocetak-1, krajHex+1, len(rez)-krajHex-1)
	stari := "[0000000000 0000000000 0000000000 0000000000]"
	br += strings.Repeat(" ", len(stari)-len(br))
	b := bytes.Index(rez[od:], []byte(stari))
	if b < 0 {
		return nil, errors.New("pdf: potpis bez raspona")
	}
	copy(rez[od+b:], br)
	podaci := make([]byte, 0, len(rez))
	podaci = append(podaci, rez[:pocetak-1]...)
	podaci = append(podaci, rez[krajHex+1:]...)
	cms, err := dod.Potpisnik(podaci)
	if err != nil {
		return nil, err
	}
	if len(cms) > rezerva {
		return nil, fmt.Errorf("pdf: potpis od %d bajtova ne stane u rezervirano mjesto", len(cms))
	}
	hexCMS := strings.ToUpper(hex.EncodeToString(cms))
	copy(rez[pocetak:], hexCMS)
	return rez, nil
}

// Potpis je jedan potpis nađen u dokumentu: raspon, CMS i podaci iz rječnika
type Potpis struct {
	CMS       []byte
	Podaci    []byte // bajtovi koje potpis pokriva
	Cijeli    bool   // raspon pokriva cijeli dokument (zadnji potpis)
	Ime       string
	Razlog    string
	Kad       time.Time
	SubFilter string
}

var reByteRange = regexp.MustCompile(`/ByteRange \[\s*(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s*\]`)

// Potpisi vadi sve potpise iz dokumenta, redom kako su dodani
func Potpisi(pdf []byte) []Potpis {
	var out []Potpis
	for _, m := range reByteRange.FindAllSubmatchIndex(pdf, -1) {
		n := func(i int) int { v, _ := strconv.Atoi(string(pdf[m[2*i]:m[2*i+1]])); return v }
		a, b, c, d := n(1), n(2), n(3), n(4)
		if a != 0 || b < 0 || c < b || c+d > len(pdf) {
			continue
		}
		rj := pdf[m[0]:min(len(pdf), m[1]+512)]
		i := bytes.Index(rj, []byte("/Contents <"))
		if i < 0 {
			continue
		}
		hexOd := m[0] + i + len("/Contents <")
		hexDo := bytes.IndexByte(pdf[hexOd:], '>')
		if hexDo < 0 {
			continue
		}
		cms, err := hex.DecodeString(string(pdf[hexOd : hexOd+hexDo]))
		if err != nil {
			continue
		}
		cms = bytes.TrimRight(cms, "\x00")
		p := Potpis{CMS: cms, Cijeli: c+d == len(pdf)}
		p.Podaci = append(append([]byte{}, pdf[a:a+b]...), pdf[c:c+d]...)
		// rječnik potpisa počinje pri zadnjem "<<" prije raspona
		od := bytes.LastIndex(pdf[:m[0]], []byte("<<"))
		if od < 0 {
			od = m[0]
		}
		do := bytes.Index(pdf[hexOd+hexDo:], []byte(">>"))
		if do < 0 {
			do = 0
		}
		rjecnik := string(pdf[od : hexOd+hexDo+do])
		p.Ime = tekstIzRjecnika(rjecnik, "/Name")
		p.Razlog = tekstIzRjecnika(rjecnik, "/Reason")
		p.SubFilter = tekstIzRjecnika(rjecnik, "/SubFilter")
		if mm := regexp.MustCompile(`/M \(D:(\d{14})([-+Z])?(\d{2})?'?(\d{2})?`).FindStringSubmatch(rjecnik); mm != nil {
			pomak := "Z"
			if mm[2] == "-" || mm[2] == "+" {
				pomak = mm[2] + mm[3] + mm[4]
			}
			if t, err := time.Parse("20060102150405Z", mm[1]+"Z"); err == nil && pomak == "Z" {
				p.Kad = t
			} else if t, err := time.Parse("20060102150405-0700", mm[1]+pomak); err == nil {
				p.Kad = t
			}
		}
		out = append(out, p)
	}
	return out
}

// tekstIzRjecnika čita vrijednost ključa iz rječnika potpisa: ime,
// UTF-16 heksadecimalni niz ili obični niz u zagradama
func tekstIzRjecnika(rjecnik, kljuc string) string {
	i := strings.Index(rjecnik, kljuc+" ")
	if i < 0 {
		return ""
	}
	s := rjecnik[i+len(kljuc)+1:]
	switch {
	case strings.HasPrefix(s, "<FEFF"):
		kraj := strings.IndexByte(s, '>')
		if kraj < 0 {
			return ""
		}
		raw, err := hex.DecodeString(s[5:kraj])
		if err != nil {
			return ""
		}
		u := make([]uint16, 0, len(raw)/2)
		for j := 0; j+1 < len(raw); j += 2 {
			u = append(u, uint16(raw[j])<<8|uint16(raw[j+1]))
		}
		return string(utf16.Decode(u))
	case strings.HasPrefix(s, "("):
		kraj := strings.IndexByte(s, ')')
		if kraj < 0 {
			return ""
		}
		return s[1:kraj]
	case strings.HasPrefix(s, "/"):
		kraj := strings.IndexAny(s[1:], " /]>")
		if kraj < 0 {
			return s[1:]
		}
		return s[1 : kraj+1]
	}
	return ""
}
