// Paket pdfw piše jednostavne PDF dokumente: tekst u dva reza (obični i
// podebljani), s hrvatskim znakovima, slika znaka organizacije i crte.
// Dovoljno za akt na jednoj do dvije stranice A4, bez vanjskih ovisnosti.
//
// Slova su Go Regular i Go Bold (Bigelow & Holmes, slobodna licenca), s
// hrvatskim znakovima, ugrađena u dokument kao TrueType s Identity-H
// kodiranjem: ispis je isti u svakom čitaču i na svakom pisaču, a tekst se
// iz PDF-a može i kopirati jer dokument nosi tablicu prema Unicodeu.
package pdfw

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// pismo je jedan ugrađeni font s tablicom glifova koje dokument koristi
type pismo struct {
	ttf   []byte
	f     *sfnt.Font
	upem  int
	mu    sync.Mutex
	gid   map[rune]sfnt.GlyphIndex
	sirin map[sfnt.GlyphIndex]int // širina u tisućinkama em-a
}

var (
	pismaJednom sync.Once
	obicno      *pismo
	podebljano  *pismo
)

func ucitajPisma() {
	pismaJednom.Do(func() {
		obicno = noviPismo(goregular.TTF)
		podebljano = noviPismo(gobold.TTF)
	})
}

func noviPismo(ttf []byte) *pismo {
	f, err := sfnt.Parse(ttf)
	if err != nil {
		panic("pdfw: font: " + err.Error())
	}
	return &pismo{ttf: ttf, f: f, upem: int(f.UnitsPerEm()), gid: map[rune]sfnt.GlyphIndex{}, sirin: map[sfnt.GlyphIndex]int{}}
}

// glif vraća indeks glifa za znak i pamti ga; nepoznat znak je upitnik
func (p *pismo) glif(r rune) sfnt.GlyphIndex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if g, ok := p.gid[r]; ok {
		return g
	}
	var buf sfnt.Buffer
	g, err := p.f.GlyphIndex(&buf, r)
	if err != nil || g == 0 {
		if r == '?' {
			g = 0
		} else {
			p.mu.Unlock()
			g = p.glif('?')
			p.mu.Lock()
		}
	}
	p.gid[r] = g
	if _, ok := p.sirin[g]; !ok {
		adv, err := p.f.GlyphAdvance(&buf, g, fixed.Int26_6(p.upem<<6), font.HintingNone)
		if err != nil {
			adv = fixed.Int26_6(p.upem / 2 << 6)
		}
		p.sirin[g] = int(adv) * 1000 / (p.upem << 6)
	}
	return g
}

func (p *pismo) sirina(r rune) int {
	g := p.glif(r)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sirin[g]
}

// A4 u točkama
const (
	A4W = 595.28
	A4H = 841.89
)

// Doc je dokument koji se gradi odozgo prema dolje
type Doc struct {
	W, H          float64
	Lijevo, Desno float64 // margine
	Gore, Dolje   float64
	Y             float64 // trenutni položaj od vrha, na aktivnoj stranici
	stranice      []*bytes.Buffer
	slike         []slika
	naslov, autor string
	// Predmet ide u metapodatke (/Subject); služi i kao oznaka po kojoj se
	// dokument prepozna kad se vrati potpisan
	Predmet       string
	rednaStranica int
	// glifovi koje ovaj dokument koristi, po rezu; font u dokumentu nosi
	// samo njih, poredane, pa isti sadržaj daje isti PDF bajt za bajt
	koristeno [2]map[rune]bool
	Podnozje  func(d *Doc, stranica, ukupno int) // crta se na kraju, na svakoj stranici
}

type slika struct {
	w, h int
	rgb  []byte
	jpeg []byte // JPEG ide u PDF kakav jest (DCTDecode)
	komp int    // broj komponenti boje JPEG-a: 1 siva, 3 RGB, 4 CMYK
}

// Novi otvara A4 dokument s uobičajenim marginama
func Novi(naslov, autor string) *Doc {
	d := &Doc{W: A4W, H: A4H, Lijevo: 56, Desno: 56, Gore: 48, Dolje: 56, naslov: naslov, autor: autor}
	d.NovaStranica()
	return d
}

// NovaStranica otvara novu stranicu i vraća kursor na vrh
func (d *Doc) NovaStranica() {
	d.stranice = append(d.stranice, &bytes.Buffer{})
	d.Y = d.Gore
}

func (d *Doc) tok() *bytes.Buffer { return d.stranice[len(d.stranice)-1] }

// Stranica je redni broj tekuće stranice, od 1
func (d *Doc) Stranica() int { return len(d.stranice) }

// Sirina je širina prostora za tekst između margina
func (d *Doc) Sirina() float64 { return d.W - d.Lijevo - d.Desno }

// pdfY pretvara položaj od vrha u PDF koordinatu od dna
func (d *Doc) pdfY(y float64) float64 { return d.H - y }

// Osiguraj otvara novu stranicu ako do dna nema mjesta za visinu h
func (d *Doc) Osiguraj(h float64) {
	if d.Y+h > d.H-d.Dolje {
		d.NovaStranica()
	}
}

// Tekst ispisuje jedan redak na zadanom mjestu (x od lijevog ruba, y od
// vrha je osnovna linija), ne mičući kursor
func (d *Doc) Tekst(x, y, size float64, bold bool, s string) {
	font := "/F1"
	if bold {
		font = "/F2"
	}
	fmt.Fprintf(d.tok(), "BT %s %.1f Tf %.2f %.2f Td <%s> Tj ET\n", font, size, x, d.pdfY(y), d.glifovi(s, bold))
}

// glifovi kodira tekst kao niz dvobajtnih indeksa glifova (Identity-H) i
// bilježi koje znakove dokument koristi
func (d *Doc) glifovi(s string, bold bool) string {
	ucitajPisma()
	p, rez := obicno, 0
	if bold {
		p, rez = podebljano, 1
	}
	if d.koristeno[rez] == nil {
		d.koristeno[rez] = map[rune]bool{}
	}
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == '\u00a0' {
			r = ' '
		}
		d.koristeno[rez][r] = true
		fmt.Fprintf(&b, "%04X", uint16(p.glif(r)))
	}
	return b.String()
}

// TekstDesno ispisuje redak poravnat udesno na x
func (d *Doc) TekstDesno(xDesno, y, size float64, bold bool, s string) {
	d.Tekst(xDesno-SirinaTeksta(s, size, bold), y, size, bold, s)
}

// TekstSredina ispisuje redak centriran na x
func (d *Doc) TekstSredina(xSredina, y, size float64, bold bool, s string) {
	d.Tekst(xSredina-SirinaTeksta(s, size, bold)/2, y, size, bold, s)
}

// Poravnanje odlomka
const (
	Lijevo  = 0
	Sredina = 1
	Desno   = 2
)

// Odlomak ispisuje tekst prelomljen na širinu, od kursora nadolje, i miče
// kursor. Prelama i na nove stranice.
func (d *Doc) Odlomak(s string, size float64, bold bool, poravnanje int) {
	d.OdlomakU(d.Lijevo, d.Sirina(), s, size, bold, poravnanje)
}

// OdlomakU je Odlomak u stupcu zadanog lijevog ruba i širine
func (d *Doc) OdlomakU(x, sirina float64, s string, size float64, bold bool, poravnanje int) {
	visina := size * 1.32
	for _, redak := range Prelomi(s, sirina, size, bold) {
		d.Osiguraj(visina)
		d.Y += size
		switch poravnanje {
		case Sredina:
			d.TekstSredina(x+sirina/2, d.Y, size, bold, redak)
		case Desno:
			d.TekstDesno(x+sirina, d.Y, size, bold, redak)
		default:
			d.Tekst(x, d.Y, size, bold, redak)
		}
		d.Y += visina - size
	}
}

// Razmak spušta kursor
func (d *Doc) Razmak(h float64) { d.Y += h }

// Crta povlači tanku crtu; koordinate od vrha
func (d *Doc) Crta(x1, y1, x2, y2 float64) {
	fmt.Fprintf(d.tok(), "0.5 w %.2f %.2f m %.2f %.2f l S\n", x1, d.pdfY(y1), x2, d.pdfY(y2))
}

// Boja je RGB boja od 0 do 1
type Boja struct{ R, G, B float64 }

// CrtaBoja povlači crtu zadane debljine i boje
func (d *Doc) CrtaBoja(x1, y1, x2, y2, debljina float64, c Boja) {
	fmt.Fprintf(d.tok(), "q %.2f w %.3f %.3f %.3f RG 1 J %.2f %.2f m %.2f %.2f l S Q\n", debljina, c.R, c.G, c.B, x1, d.pdfY(y1), x2, d.pdfY(y2))
}

// Okvir crta pravokutnik s gornjim lijevim kutom (x, y od vrha), s ispunom
// i rubom zadanih boja
func (d *Doc) Okvir(x, y, w, h float64, ispuna, rub Boja) {
	fmt.Fprintf(d.tok(), "q 0.8 w %.3f %.3f %.3f rg %.3f %.3f %.3f RG %.2f %.2f %.2f %.2f re B Q\n",
		ispuna.R, ispuna.G, ispuna.B, rub.R, rub.G, rub.B, x, d.pdfY(y+h), w, h)
}

// TekstBoja ispisuje redak u boji
func (d *Doc) TekstBoja(x, y, size float64, bold bool, s string, c Boja) {
	font := "/F1"
	if bold {
		font = "/F2"
	}
	fmt.Fprintf(d.tok(), "q BT %.3f %.3f %.3f rg %s %.1f Tf %.2f %.2f Td <%s> Tj ET Q\n", c.R, c.G, c.B, font, size, x, d.pdfY(y), d.glifovi(s, bold))
}

// SlikaPNG smješta PNG sliku; x i y od vrha su gornji lijevi kut. Prozirnost
// se stapa s bijelom, jer akt ide na papir.
func (d *Doc) SlikaPNG(podaci []byte, x, y, w, h float64) error {
	img, err := png.Decode(bytes.NewReader(podaci))
	if err != nil {
		return err
	}
	b := img.Bounds()
	rgb := make([]byte, 0, b.Dx()*b.Dy()*3)
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			r, g, bl, a := img.At(px, py).RGBA()
			// stapanje s bijelom: c = c*a + 65535*(1-a)
			rgb = append(rgb, stopi(r, a), stopi(g, a), stopi(bl, a))
		}
	}
	d.slike = append(d.slike, slika{w: b.Dx(), h: b.Dy(), rgb: rgb})
	fmt.Fprintf(d.tok(), "q %.2f 0 0 %.2f %.2f %.2f cm /Im%d Do Q\n", w, h, x, d.pdfY(y+h), len(d.slike))
	return nil
}

func stopi(c, a uint32) byte {
	v := (float64(c)*float64(a) + 65535*(65535-float64(a))) / 65535
	return byte(v / 257)
}

// Bajtovi sastavlja PDF
func (d *Doc) Bajtovi() []byte {
	if d.Podnozje != nil {
		ukupno := len(d.stranice)
		for i := range d.stranice {
			d.rednaStranica = i
			d.Podnozje(d, i+1, ukupno)
		}
	}
	var out bytes.Buffer
	var offsets []int
	var zamjene []zamjena
	obj := func(sadrzaj string) int {
		offsets = append(offsets, out.Len())
		n := len(offsets)
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, sadrzaj)
		return n
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	// 1 katalog, 2 stranice, 3 F1, 4 F2 (svaki sa svojim opisnikom, datotekom i
	// tablicom prema Unicodeu), pa slike, pa stranice sa sadržajem
	ucitajPisma()
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("@@PAGES@@")
	// F1 i F2 moraju biti 3 i 4: prvo rezervirano mjesto, pa pomoćni objekti
	obj("@@FONT1@@")
	obj("@@FONT2@@")
	fontObj := func(rezervirano string, p *pismo, naziv string) {
		rez := 0
		if p == podebljano {
			rez = 1
		}
		desc, toUni := fontDijelovi(p, naziv, d.koristeno[rez], obj)
		font := fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [ %d 0 R ] /ToUnicode %d 0 R >>", naziv, desc, toUni)
		zamjene = append(zamjene, zamjena{rezervirano, font})
	}
	fontObj("@@FONT1@@", obicno, "GoRegular")
	fontObj("@@FONT2@@", podebljano, "GoBold")
	slikeIdx := make([]int, len(d.slike))
	for i, s := range d.slike {
		if s.jpeg != nil {
			prostor := map[int]string{1: "/DeviceGray", 3: "/DeviceRGB", 4: "/DeviceCMYK"}[s.komp]
			if prostor == "" {
				prostor = "/DeviceRGB"
			}
			slikeIdx[i] = obj(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", s.w, s.h, prostor, len(s.jpeg), s.jpeg))
			continue
		}
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		_, _ = zw.Write(s.rgb)
		_ = zw.Close()
		slikeIdx[i] = obj(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", s.w, s.h, z.Len(), z.Bytes()))
	}
	xobj := ""
	for i, idx := range slikeIdx {
		xobj += fmt.Sprintf("/Im%d %d 0 R ", i+1, idx)
	}
	resursi := fmt.Sprintf("<< /Font << /F1 3 0 R /F2 4 0 R >> /XObject << %s>> >>", xobj)
	var pageIdx []int
	for _, tok := range d.stranice {
		c := obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", tok.Len(), tok.String()))
		p := obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources %s /Contents %d 0 R >>", d.W, d.H, resursi, c))
		pageIdx = append(pageIdx, p)
	}
	kids := ""
	for _, p := range pageIdx {
		kids += fmt.Sprintf("%d 0 R ", p)
	}
	info := obj(fmt.Sprintf("<< /Title (%s) /Author (%s) /Subject (%s) /Producer (goCOP) >>", kodiraj(d.naslov), kodiraj(d.autor), kodiraj(d.Predmet)))

	// rezervirana mjesta (stranice, fontovi) zamjenjuju se pravim sadržajem;
	// svaka zamjena pomiče pomake objekata iza sebe za razliku duljine
	pages := fmt.Sprintf("<< /Type /Pages /Kids [ %s] /Count %d >>", kids, len(pageIdx))
	zamjene = append([]zamjena{{"@@PAGES@@", pages}}, zamjene...)
	final := out.Bytes()
	for _, z := range zamjene {
		poz := bytes.Index(final, []byte(z.od))
		if poz < 0 {
			continue
		}
		pomak := len(z.na) - len(z.od)
		for i := range offsets {
			if offsets[i] > poz {
				offsets[i] += pomak
			}
		}
		final = bytes.Replace(final, []byte(z.od), []byte(z.na), 1)
	}
	var res bytes.Buffer
	res.Write(final)
	xref := res.Len()
	fmt.Fprintf(&res, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&res, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&res, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, info, xref)
	return res.Bytes()
}

// ---- fontovi ----

type zamjena struct{ od, na string }

// fontDijelovi upisuje opisnik fonta, datoteku i tablicu prema Unicodeu i
// vraća njihove brojeve; širine i tablica nose samo glifove koje dokument
// koristi
func fontDijelovi(p *pismo, naziv string, znakovi map[rune]bool, obj func(string) int) (desc, toUni int) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(p.ttf)
	_ = zw.Close()
	file := obj(fmt.Sprintf("<< /Length %d /Length1 %d /Filter /FlateDecode >>\nstream\n%s\nendstream", z.Len(), len(p.ttf), z.Bytes()))

	var buf sfnt.Buffer
	m, _ := p.f.Metrics(&buf, fixed.Int26_6(p.upem<<6), font.HintingNone)
	skala := func(v fixed.Int26_6) int { return int(v) * 1000 / (p.upem << 6) }
	bbox, _ := p.f.Bounds(&buf, fixed.Int26_6(p.upem<<6), font.HintingNone)
	fd := obj(fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags 32 /FontBBox [ %d %d %d %d ] /ItalicAngle 0 /Ascent %d /Descent %d /CapHeight %d /StemV 80 /FontFile2 %d 0 R >>",
		naziv, skala(bbox.Min.X), -skala(bbox.Max.Y), skala(bbox.Max.X), -skala(bbox.Min.Y), skala(m.Ascent), -skala(m.Descent), skala(m.CapHeight), file))

	redom := make([]rune, 0, len(znakovi))
	for r := range znakovi {
		redom = append(redom, r)
	}
	sort.Slice(redom, func(i, j int) bool { return redom[i] < redom[j] })
	var w strings.Builder
	var cmap strings.Builder
	n := 0
	for _, r := range redom {
		g := p.glif(r)
		fmt.Fprintf(&w, "%d [ %d ] ", g, p.sirina(r))
		fmt.Fprintf(&cmap, "<%04X> <%04X>\n", uint16(g), uint16(r))
		n++
	}
	desc = obj(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /DW 500 /W [ %s] /CIDToGIDMap /Identity >>", naziv, fd, w.String()))
	tu := "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		fmt.Sprintf("%d beginbfchar\n%sendbfchar\n", n, cmap.String()) + "endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend"
	toUni = obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tu), tu))
	return desc, toUni
}

// kodiraj pretvara tekst u PDF niz za metapodatke, sa znakovima izvan
// ASCII-ja zamijenjenima
func kodiraj(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteByte(byte(r))
		case r < 0x80:
			b.WriteByte(byte(r))
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// SirinaTeksta je širina teksta u točkama za zadanu veličinu
func SirinaTeksta(s string, size float64, bold bool) float64 {
	ucitajPisma()
	p := obicno
	if bold {
		p = podebljano
	}
	w := 0
	for _, r := range s {
		if r == '\u00a0' {
			r = ' '
		}
		w += p.sirina(r)
	}
	return float64(w) * size / 1000
}

// Prelomi dijeli tekst na retke koji stanu u širinu; postojeći prijelomi
// redaka se poštuju
func Prelomi(s string, sirina, size float64, bold bool) []string {
	var out []string
	for _, odlomak := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		rijeci := strings.Fields(odlomak)
		if len(rijeci) == 0 {
			out = append(out, "")
			continue
		}
		redak := ""
		for _, r := range rijeci {
			proba := r
			if redak != "" {
				proba = redak + " " + r
			}
			if SirinaTeksta(proba, size, bold) <= sirina || redak == "" {
				redak = proba
				continue
			}
			out = append(out, redak)
			redak = r
		}
		out = append(out, redak)
	}
	return out
}

// Duljina je broj znakova, za grubu procjenu
func Duljina(s string) int { return utf8.RuneCountInString(s) }

var _ = image.Rect

// SlikaJPEG smješta JPEG sliku kakva jest, bez preračunavanja; x i y od vrha
// su gornji lijevi kut
func (d *Doc) SlikaJPEG(podaci []byte, x, y, w, h float64) error {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(podaci))
	if err != nil {
		return err
	}
	komp := 3
	switch cfg.ColorModel {
	case color.GrayModel:
		komp = 1
	case color.CMYKModel:
		komp = 4
	}
	d.slike = append(d.slike, slika{w: cfg.Width, h: cfg.Height, jpeg: podaci, komp: komp})
	fmt.Fprintf(d.tok(), "q %.2f 0 0 %.2f %.2f %.2f cm /Im%d Do Q\n", w, h, x, d.pdfY(y+h), len(d.slike))
	return nil
}

// PDFIzSlike slaže PDF od jedne skenirane stranice (JPEG ili PNG), položene
// na A4 s očuvanim omjerom, za sken potpisanog akta poslan kao slika
func PDFIzSlike(podaci []byte, naslov string) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(podaci))
	if err != nil {
		return nil, fmt.Errorf("slika nije čitljiva: %w", err)
	}
	d := Novi(naslov, "goCOP")
	w, h := d.W, d.H
	if float64(cfg.Width)/float64(cfg.Height) > w/h {
		h = w * float64(cfg.Height) / float64(cfg.Width)
	} else {
		w = h * float64(cfg.Width) / float64(cfg.Height)
	}
	x, y := (d.W-w)/2, (d.H-h)/2
	switch format {
	case "jpeg":
		err = d.SlikaJPEG(podaci, x, y, w, h)
	case "png":
		err = d.SlikaPNG(podaci, x, y, w, h)
	default:
		err = fmt.Errorf("slika mora biti JPEG ili PNG, a ne %s", format)
	}
	if err != nil {
		return nil, err
	}
	return d.Bajtovi(), nil
}
