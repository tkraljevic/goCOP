package pdfw

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
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
	Pecat      string // naziv pečata (/Name) kad nema potpisnika; zadano goCOP
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
	tr, err := citajTrailer(pdf)
	if err != nil {
		return nil, err
	}
	st, err := citajStranicu(pdf, tr.root, dod.Stranica)
	if err != nil {
		return nil, err
	}
	iz := novaIzmjena(pdf, tr.size)
	ap := iz.izgled(dod)
	widget, sig := iz.biljeska(dod, st, ap)
	iz.uStranicuIKatalog(st, tr.root, widget, sig > 0)
	rez := iz.zakljuci(tr)
	if sig == 0 {
		return rez, nil
	}
	return upisiPotpis(rez, iz.pomaci[sig], dod.Potpisnik)
}

// trailer su podaci zadnjeg traillera dokumenta
type trailer struct {
	root, size, prev int
	info             string // " /Info N 0 R" ili prazno
}

// citajTrailer čita zadnji trailer iz repa dokumenta. Zadnji je mjerodavan:
// kratka ranija izmjena stane u isti rep s prethodnim trailerom, pa se uzima
// zadnje podudaranje.
func citajTrailer(pdf []byte) (trailer, error) {
	rep := pdf
	if len(rep) > 2048 {
		rep = rep[len(rep)-2048:]
	}
	zadnji := func(re *regexp.Regexp) []byte {
		sve := re.FindAllSubmatch(rep, -1)
		if len(sve) == 0 {
			return nil
		}
		return sve[len(sve)-1][1]
	}
	m, ms, mx := zadnji(reRoot), zadnji(reSize), reXref.FindSubmatch(rep)
	if m == nil || ms == nil || mx == nil {
		return trailer{}, errors.New("pdf: nema zaglavlja tablice objekata")
	}
	var tr trailer
	tr.root, _ = strconv.Atoi(string(m))     // regex je uzeo samo znamenke
	tr.size, _ = strconv.Atoi(string(ms))    // isto
	tr.prev, _ = strconv.Atoi(string(mx[1])) // isto
	if mi := zadnji(reInfo); mi != nil {
		tr.info = " /Info " + string(mi) + " 0 R"
	}
	return tr, nil
}

// stranica je stranica na koju ide dodatak, uz katalog dokumenta
type stranica struct {
	katalog string
	n       int // broj objekta stranice
	tijelo  string
	visina  float64
}

// citajStranicu nalazi zadanu stranicu (od 1) preko kataloga i popisa stranica
func citajStranicu(pdf []byte, root, redni int) (stranica, error) {
	katalog, err := objekt(pdf, root)
	if err != nil {
		return stranica{}, err
	}
	mp := rePages.FindStringSubmatch(katalog)
	if mp == nil {
		return stranica{}, errors.New("pdf: katalog bez stranica")
	}
	pagesN, _ := strconv.Atoi(mp[1]) // regex je uzeo samo znamenke
	pages, err := objekt(pdf, pagesN)
	if err != nil {
		return stranica{}, err
	}
	mk := reKids.FindStringSubmatch(pages)
	if mk == nil {
		return stranica{}, errors.New("pdf: nema popisa stranica")
	}
	kids := reRef.FindAllStringSubmatch(mk[1], -1)
	if redni < 1 || redni > len(kids) {
		return stranica{}, fmt.Errorf("pdf: nema stranice %d", redni)
	}
	st := stranica{katalog: katalog, visina: A4H}
	st.n, _ = strconv.Atoi(kids[redni-1][1]) // regex je uzeo samo znamenke
	if st.tijelo, err = objekt(pdf, st.n); err != nil {
		return stranica{}, err
	}
	if mb := reMediaBox.FindStringSubmatch(st.tijelo); mb != nil {
		st.visina, _ = strconv.ParseFloat(mb[2], 64) // regex je uzeo samo broj
	}
	return st, nil
}

// izmjena je dokument s dopisanim objektima; novi objekti dobivaju brojeve
// od /Size nadalje, a pomaci svih upisanih idu u novu tablicu objekata
type izmjena struct {
	out      bytes.Buffer
	sljedeci int
	pomaci   map[int]int
}

func novaIzmjena(pdf []byte, size int) *izmjena {
	iz := &izmjena{sljedeci: size, pomaci: map[int]int{}}
	iz.out.Write(pdf)
	if pdf[len(pdf)-1] != '\n' {
		iz.out.WriteByte('\n')
	}
	return iz
}

func (iz *izmjena) obj(n int, sadrzaj string) {
	iz.pomaci[n] = iz.out.Len()
	fmt.Fprintf(&iz.out, "%d 0 obj\n%s\nendobj\n", n, sadrzaj)
}

func (iz *izmjena) rezerviraj() int {
	n := iz.sljedeci
	iz.sljedeci++
	return n
}

func (iz *izmjena) novi(sadrzaj string) int {
	n := iz.rezerviraj()
	iz.obj(n, sadrzaj)
	return n
}

// izgled crta dodatak kao malu stranicu istim alatima i upisuje je kao
// obrazac XObject, sa slikama; 0 kad dodatak nema izgleda
func (iz *izmjena) izgled(dod Dodatak) int {
	if dod.Crtaj == nil || dod.W <= 0 || dod.H <= 0 {
		return 0
	}
	t := &Doc{W: dod.W, H: dod.H}
	t.NovaStranica()
	dod.Crtaj(t)
	xobj := ""
	for i, s := range t.slike {
		xobj += fmt.Sprintf("/Im%d %d 0 R ", i+1, iz.novi(objektSlike(s)))
	}
	tok := t.tok().String()
	return iz.novi(fmt.Sprintf("<< /Type /XObject /Subtype /Form /FormType 1 /BBox [0 0 %.2f %.2f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> /XObject << %s>> >> /Length %d >>\nstream\n%s\nendstream", dod.W, dod.H, xobj, len(tok), tok))
}

// objektSlike: JPEG ide kakav jest, a ostalo kao sažeti RGB
func objektSlike(s slika) string {
	if s.jpeg != nil {
		prostor := map[int]string{1: "/DeviceGray", 3: "/DeviceRGB", 4: "/DeviceCMYK"}[s.komp]
		if prostor == "" {
			prostor = "/DeviceRGB"
		}
		return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", s.w, s.h, prostor, len(s.jpeg), s.jpeg)
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(s.rgb) // pisanje u memoriju ne pada
	_ = zw.Close()         // isto
	return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", s.w, s.h, z.Len(), z.Bytes())
}

// biljeska upisuje polje potpisa (s rječnikom potpisa i rezerviranim
// mjestom za CMS) ili pečat; vraća broj bilješke i rječnika potpisa (0 za
// pečat)
func (iz *izmjena) biljeska(dod Dodatak, st stranica, ap int) (widget, sig int) {
	rect, apDio := "[0 0 0 0]", ""
	if ap > 0 {
		rect = fmt.Sprintf("[%.2f %.2f %.2f %.2f]", dod.X, st.visina-dod.Y-dod.H, dod.X+dod.W, st.visina-dod.Y)
		apDio = fmt.Sprintf(" /AP << /N %d 0 R >>", ap)
	}
	kad := dod.Kad
	if kad.IsZero() {
		kad = time.Now()
	}
	datum := kad.Format("D:20060102150405-07'00'")
	if dod.Potpisnik == nil {
		pecat := dod.Pecat
		if pecat == "" {
			pecat = "goCOP"
		}
		return iz.novi(fmt.Sprintf("<< /Type /Annot /Subtype /Stamp /Name /%s /Rect %s /F 132 /P %d 0 R /M (%s) /T %s /Contents %s%s >>", pecat, rect, st.n, datum, tekstUTF16(dod.Ime), tekstUTF16(dod.Razlog), apDio)), 0
	}
	sig, widget = iz.rezerviraj(), iz.rezerviraj()
	iz.obj(sig, fmt.Sprintf("<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /ETSI.CAdES.detached /ByteRange [0000000000 0000000000 0000000000 0000000000] /Contents <%s> /M (%s) /Name %s /Reason %s /Location %s >>",
		strings.Repeat("0", rezerva*2), datum, tekstUTF16(dod.Ime), tekstUTF16(dod.Razlog), tekstUTF16(dod.Mjesto)))
	iz.obj(widget, fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Sig /T (Potpis%d) /V %d 0 R /Rect %s /F 132 /P %d 0 R%s >>", len(reRef.FindAllString(poljaObrasca(st.katalog), -1))+1, sig, rect, st.n, apDio))
	return widget, sig
}

// poljaObrasca su reference polja iz /AcroForm kataloga, ili prazno
func poljaObrasca(katalog string) string {
	if ma := reAcro.FindStringSubmatch(katalog); ma != nil {
		return strings.TrimSpace(ma[1])
	}
	return ""
}

// uStranicuIKatalog dopisuje stranicu s novom bilješkom, a za potpis i
// katalog s poljem u obrascu
func (iz *izmjena) uStranicuIKatalog(st stranica, root, widget int, potpis bool) {
	ref := fmt.Sprintf("%d 0 R", widget)
	page := st.tijelo
	if man := reAnnots.FindStringSubmatch(page); man != nil {
		page = reAnnots.ReplaceAllLiteralString(page, "/Annots [ "+strings.TrimSpace(man[1])+" "+ref+" ]")
	} else {
		page = strings.TrimSuffix(strings.TrimSpace(page), ">>") + "/Annots [ " + ref + " ] >>"
	}
	iz.obj(st.n, page)
	if !potpis {
		return
	}
	polja := poljaObrasca(st.katalog)
	if polja != "" {
		polja += " "
	}
	acro := "/AcroForm << /Fields [ " + polja + ref + " ] /SigFlags 3 >>"
	katalog := st.katalog
	if reAcro.MatchString(katalog) {
		katalog = reAcro.ReplaceAllLiteralString(katalog, acro)
	} else {
		katalog = strings.TrimSuffix(strings.TrimSpace(katalog), ">>") + acro + " >>"
	}
	iz.obj(root, katalog)
}

// zakljuci dopisuje tablicu objekata za izmijenjene i nove, po odsječcima
// uzastopnih brojeva, i trailer koji preko /Prev pokazuje na stari
func (iz *izmjena) zakljuci(tr trailer) []byte {
	brojevi := make([]int, 0, len(iz.pomaci))
	for n := range iz.pomaci {
		brojevi = append(brojevi, n)
	}
	slices.Sort(brojevi)
	xref := iz.out.Len()
	iz.out.WriteString("xref\n")
	for i := 0; i < len(brojevi); {
		j := i
		for j+1 < len(brojevi) && brojevi[j+1] == brojevi[j]+1 {
			j++
		}
		fmt.Fprintf(&iz.out, "%d %d\n", brojevi[i], j-i+1)
		for k := i; k <= j; k++ {
			fmt.Fprintf(&iz.out, "%010d 00000 n \n", iz.pomaci[brojevi[k]])
		}
		i = j + 1
	}
	fmt.Fprintf(&iz.out, "trailer\n<< /Size %d /Root %d 0 R%s /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", iz.sljedeci, tr.root, tr.info, tr.prev, xref)
	return iz.out.Bytes()
}

// upisiPotpis upisuje raspon bajtova (sve osim heksadecimalnog sadržaja
// potpisa), potpiše ga i upiše CMS u rezervirano mjesto rječnika na pomaku od
func upisiPotpis(rez []byte, od int, potpisnik Potpisnik) ([]byte, error) {
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
	cms, err := potpisnik(podaci)
	if err != nil {
		return nil, err
	}
	if len(cms) > rezerva {
		return nil, fmt.Errorf("pdf: potpis od %d bajtova ne stane u rezervirano mjesto", len(cms))
	}
	copy(rez[pocetak:], strings.ToUpper(hex.EncodeToString(cms)))
	return rez, nil
}

// bezDopune odbacuje dopunu iza CMS-a. Mjesto za potpis je unaprijed
// rezervirano i popunjeno nulama, pa iza potpisa uvijek ostane rep nula.
//
// Rep se NE smije rezati brisanjem nula s kraja: potpis je DER zapis koji i
// sam može završiti nulom — zadnji bajt ECDSA vrijednosti je slučajan, pa se
// to dogodi otprilike jednom u sto potpisa. Takav potpis bi se skratio za
// bajt ili dva i postao nečitljiv. Umjesto toga čita se duljina koju DER
// zapis sam navodi u zaglavlju.
func bezDopune(b []byte) []byte {
	if n := duljinaDER(b); n > 0 && n <= len(b) {
		return b[:n]
	}
	return bytes.TrimRight(b, "\x00")
}

// duljinaDER vraća ukupnu duljinu prvog DER zapisa (zaglavlje i sadržaj);
// 0 kad zapis nije čitljiv ili je BER-a s neodređenom duljinom
func duljinaDER(b []byte) int {
	if len(b) < 2 {
		return 0
	}
	duljina := int(b[1])
	if duljina < 0x80 {
		return 2 + duljina
	}
	bajtova := duljina & 0x7f
	if bajtova == 0 || bajtova > 4 || len(b) < 2+bajtova {
		return 0 // neodređena duljina ili zapis koji ne razumijemo
	}
	duljina = 0
	for _, x := range b[2 : 2+bajtova] {
		duljina = duljina<<8 | int(x)
	}
	return 2 + bajtova + duljina
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
		cms = bezDopune(cms)
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
