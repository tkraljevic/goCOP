// Paket pdfw piše jednostavne PDF dokumente: tekst u dva reza (obični i
// podebljani), s hrvatskim znakovima, slika znaka organizacije i crte.
// Dovoljno za akt na jednoj do dvije stranice A4, bez vanjskih ovisnosti.
//
// Slova su standardna Helvetica koju svaki čitač ima, s vlastitim kodiranjem
// za č, ć, đ, š, ž: čitač ih uzima iz zamjenskog fonta po imenu glifa. Zato
// se ništa ne ugrađuje, a dokument je malen.
package pdfw

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/png"
	"strings"
	"unicode/utf8"
)

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
	rednaStranica int
	Podnozje      func(d *Doc, stranica, ukupno int) // crta se na kraju, na svakoj stranici
}

type slika struct {
	w, h int
	rgb  []byte
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
	fmt.Fprintf(d.tok(), "BT %s %.1f Tf %.2f %.2f Td (%s) Tj ET\n", font, size, x, d.pdfY(y), kodiraj(s))
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
	obj := func(sadrzaj string) int {
		offsets = append(offsets, out.Len())
		n := len(offsets)
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, sadrzaj)
		return n
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	// 1 katalog, 2 stranice, 3 kodiranje, 4 F1, 5 F2, pa slike, pa stranice sa sadržajem
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	pagesIdx := obj("PLACEHOLDER")
	obj("<< /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [ 129 /Ccaron 141 /Cacute 143 /Dcroat 144 /ccaron 157 /cacute 173 /dcroat ] >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding 3 0 R >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding 3 0 R >>")
	slikeIdx := make([]int, len(d.slike))
	for i, s := range d.slike {
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
	resursi := fmt.Sprintf("<< /Font << /F1 4 0 R /F2 5 0 R >> /XObject << %s>> >>", xobj)
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
	info := obj(fmt.Sprintf("<< /Title (%s) /Author (%s) /Producer (goCOP) >>", kodiraj(d.naslov), kodiraj(d.autor)))

	// stranice: zamijeni rezervirano mjesto pravim sadržajem, s istim rednim brojem
	pages := fmt.Sprintf("<< /Type /Pages /Kids [ %s] /Count %d >>", kids, len(pageIdx))
	final := bytes.Replace(out.Bytes(), []byte("PLACEHOLDER"), []byte(pages), 1)
	// pomaci iza zamjene se pomiču za razliku duljine
	pomak := len(pages) - len("PLACEHOLDER")
	for i := range offsets {
		if i > pagesIdx-1 {
			offsets[i] += pomak
		}
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

// ---- kodiranje i širine ----

// kodovi za znakove izvan ASCII-ja: WinAnsi gdje ih ima, a č ć đ na
// slobodnim mjestima koja kodiranje dokumenta imenuje glifom
var kodovi = map[rune]byte{
	'Š': 0x8A, 'š': 0x9A, 'Ž': 0x8E, 'ž': 0x9E,
	'Č': 0x81, 'č': 0x90, 'Ć': 0x8D, 'ć': 0x9D, 'Đ': 0x8F, 'đ': 0xAD,
	'€': 0x80, '…': 0x85, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '„': 0x84, '•': 0x95, '–': 0x96, '—': 0x97,
	'°': 0xB0, '²': 0xB2, '³': 0xB3, '·': 0xB7, '×': 0xD7, '§': 0xA7, '«': 0xAB, '»': 0xBB, ' ': 0x20,
	'é': 0xE9, 'è': 0xE8, 'ä': 0xE4, 'ö': 0xF6, 'ü': 0xFC, 'Ä': 0xC4, 'Ö': 0xD6, 'Ü': 0xDC, 'ß': 0xDF, 'á': 0xE1, 'í': 0xED, 'ó': 0xF3, 'ú': 0xFA, 'ñ': 0xF1,
	'ő': 'o', 'ű': 'u', 'Ő': 'O', 'Ű': 'U',
}

// kodiraj pretvara tekst u PDF niz u kodiranju dokumenta, s izbjegnutim
// zagradama i kosom crtom
func kodiraj(s string) string {
	var b strings.Builder
	for _, r := range s {
		var c byte
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			c = byte(r)
		case r < 0x80:
			if r == '\n' || r == '\r' || r == '\t' {
				c = ' '
			} else {
				c = byte(r)
			}
		default:
			k, ok := kodovi[r]
			if !ok {
				k = '?'
			}
			c = k
		}
		if c >= 0x80 {
			fmt.Fprintf(&b, "\\%03o", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// SirinaTeksta je širina teksta u točkama za zadanu veličinu
func SirinaTeksta(s string, size float64, bold bool) float64 {
	w := 0
	for _, r := range s {
		w += sirinaZnaka(r, bold)
	}
	return float64(w) * size / 1000
}

// osnovni oblik slova s dijakritikom, za širinu
var osnova = map[rune]rune{'Š': 'S', 'š': 's', 'Ž': 'Z', 'ž': 'z', 'Č': 'C', 'č': 'c', 'Ć': 'C', 'ć': 'c', 'Đ': 'D', 'đ': 'd',
	'é': 'e', 'è': 'e', 'ä': 'a', 'ö': 'o', 'ü': 'u', 'Ä': 'A', 'Ö': 'O', 'Ü': 'U', 'á': 'a', 'í': 'i', 'ó': 'o', 'ú': 'u', 'ñ': 'n', 'ő': 'o', 'ű': 'u', 'Ő': 'O', 'Ű': 'U', ' ': ' '}

func sirinaZnaka(r rune, bold bool) int {
	if o, ok := osnova[r]; ok {
		r = o
	}
	tablica := helvetica
	if bold {
		tablica = helveticaBold
	}
	if r >= 32 && r < 127 {
		return tablica[r-32]
	}
	switch r {
	case '–':
		return 556
	case '—':
		return 1000
	case '…':
		return 1000
	case '„', '“', '”':
		if bold {
			return 500
		}
		return 333
	case '‘', '’':
		if bold {
			return 278
		}
		return 222
	case '°':
		return 400
	case '²', '³':
		return 333
	case '·', '•':
		return 350
	case '€':
		return 556
	case 'ß':
		return 611
	}
	return 556
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

// Širine Helvetice i Helvetice Bold za ASCII 32–126, iz AFM datoteka Adobea
var helvetica = [95]int{278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584}
var helveticaBold = [95]int{278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611, 975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584}

var _ = image.Rect
