package models

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// Tema su boje koje uprava organizacije prilagođava u Administraciji › Tema.
// Svaka tema (svijetla i tamna) ima tri boje; prazno polje znači zadanu boju
// iz style.css. Iz njih se izvode prateće nijanse (prelazak mišem, svijetle
// podloge, rubovi, traka), pa se cijela aplikacija mijenja zajedno. Tema
// putuje razmjenom kao opća postavka, pa vrijedi na svim čvorovima.
type Tema struct {
	Svijetla BojeTeme `json:"svijetla"`
	Tamna    BojeTeme `json:"tamna"`
}

// BojeTeme su boje jedne teme, kao #rrggbb
type BojeTeme struct {
	Glavna   string `json:"glavna,omitempty"`   // naslovi, poveznice, okviri, značke (--primary)
	Naglasak string `json:"naglasak,omitempty"` // zelena: aktivno, mirno, traka (--accent)
	Gumb     string `json:"gumb,omitempty"`     // pozadina glavnog gumba, tekst je bijel (--gumb)
}

// Zadane boje, iste kao u style.css (TestZadaneBojeTemeKaoUStilu to čuva)
var (
	ZadanaSvijetla = BojeTeme{Glavna: "#173e74", Naglasak: "#20ba70", Gumb: "#173e74"}
	ZadanaTamna    = BojeTeme{Glavna: "#6f9bd9", Naglasak: "#2fd08a", Gumb: "#3a6db5"}
)

// Podloge na kojima se boje teme čitaju (style.css: --surface, --bg)
const (
	svijetlaPovrsina = "#ffffff"
	svijetlaPodloga  = "#f4f4f4"
	tamnaPovrsina    = "#172231"
	tamnaPodloga     = "#0f1720"
)

var trenutnaTema atomic.Pointer[Tema]

// SetTema postavlja temu; zove se pri startu, nakon spremanja i nakon
// primitka razmjenom
func SetTema(t Tema) { trenutnaTema.Store(&t) }

// TemaPrograma vraća trenutnu temu (prazna znači zadane boje)
func TemaPrograma() Tema {
	if t := trenutnaTema.Load(); t != nil {
		return *t
	}
	return Tema{}
}

// CitajTemu čita temu iz spremljenog JSON-a; neispravna boja se zanemaruje
func CitajTemu(s string) Tema {
	var t Tema
	if strings.TrimSpace(s) == "" || json.Unmarshal([]byte(s), &t) != nil {
		return Tema{}
	}
	t.Svijetla, t.Tamna = t.Svijetla.ocisceno(), t.Tamna.ocisceno()
	return t
}

// Prazna javlja ostaju li sve boje zadane
func (t Tema) Prazna() bool { return t.Svijetla == (BojeTeme{}) && t.Tamna == (BojeTeme{}) }

var hexBoja = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// NormalizirajBoju vraća boju kao #rrggbb malim slovima; prazno ostaje prazno
func NormalizirajBoju(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", nil
	}
	if !strings.HasPrefix(s, "#") {
		s = "#" + s
	}
	if len(s) == 4 { // #abc → #aabbcc
		s = "#" + string([]byte{s[1], s[1], s[2], s[2], s[3], s[3]})
	}
	if !hexBoja.MatchString(s) {
		return "", fmt.Errorf("%q nije boja u obliku #rrggbb", s)
	}
	return s, nil
}

func (b BojeTeme) ocisceno() BojeTeme {
	ocisti := func(s string) string {
		if n, err := NormalizirajBoju(s); err == nil {
			return n
		}
		return ""
	}
	return BojeTeme{Glavna: ocisti(b.Glavna), Naglasak: ocisti(b.Naglasak), Gumb: ocisti(b.Gumb)}
}

// Popunjeno vraća boje s popunjenim praznim poljima zadanim bojama. U
// svijetloj temi gumb bez svoje boje prati glavnu (style.css: --gumb:
// var(--primary)), u tamnoj ima vlastitu.
func (b BojeTeme) Popunjeno(zadano BojeTeme, gumbPratiGlavnu bool) BojeTeme {
	if b.Glavna == "" {
		b.Glavna = zadano.Glavna
	}
	if b.Naglasak == "" {
		b.Naglasak = zadano.Naglasak
	}
	if b.Gumb == "" {
		b.Gumb = zadano.Gumb
		if gumbPratiGlavnu {
			b.Gumb = b.Glavna
		}
	}
	return b
}

// Poveznice i tekst „uspjeha” čitaju se u tamnijoj (svijetla tema) odnosno
// svjetlijoj (tamna tema) nijansi naglaska, da budu čitljivi na kartici
func poveznicaSvijetla(naglasak string) string { return miješaj(naglasak, "#000000", 0.3) }
func poveznicaTamna(naglasak string) string    { return miješaj(naglasak, "#ffffff", 0.15) }

// ProvjeraKontrasta je jedna provjera čitljivosti boje teme
type ProvjeraKontrasta struct {
	Tema     string  // "svijetla" ili "tamna"
	Opis     string  // što se čita na čemu
	Omjer    float64 // omjer kontrasta po WCAG-u
	Najmanje float64
}

// Prolazi javlja je li kontrast dovoljan za običan tekst
func (p ProvjeraKontrasta) Prolazi() bool { return p.Omjer >= p.Najmanje }

// Provjere računaju kontrast boja teme na podlogama na kojima se čitaju
func (t Tema) Provjere() []ProvjeraKontrasta {
	sv := t.Svijetla.Popunjeno(ZadanaSvijetla, true)
	ta := t.Tamna.Popunjeno(ZadanaTamna, false)
	return []ProvjeraKontrasta{
		{"svijetla", "glavna boja na podlozi stranice", Kontrast(sv.Glavna, svijetlaPodloga), 4.5},
		{"svijetla", "poveznica (tamnija nijansa naglaska) na kartici", Kontrast(poveznicaSvijetla(sv.Naglasak), svijetlaPovrsina), 4.5},
		{"svijetla", "bijeli tekst na glavnom gumbu", Kontrast("#ffffff", sv.Gumb), 4.5},
		{"tamna", "glavna boja na kartici", Kontrast(ta.Glavna, tamnaPovrsina), 4.5},
		{"tamna", "naglasak na kartici", Kontrast(poveznicaTamna(ta.Naglasak), tamnaPovrsina), 4.5},
		{"tamna", "bijeli tekst na glavnom gumbu", Kontrast("#ffffff", ta.Gumb), 4.5},
	}
}

// NajmanjiDopusteniKontrast je granica ispod koje se tema ne sprema: tekst
// se tada više ne da pouzdano pročitati (WCAG za veliki tekst i sučelje)
const NajmanjiDopusteniKontrast = 3.0

// Verzija je kratki otisak teme za adresu /tema.css, da preglednik ne drži
// staru temu u međuspremniku
func (t Tema) Verzija() string {
	h := fnv.New32a()
	h.Write([]byte(t.CSS()))
	return strconv.FormatUint(uint64(h.Sum32()), 36)
}

// CSS je stil koji nadjačava tokene iz style.css; samo za postavljene boje
func (t Tema) CSS() string {
	if t.Prazna() {
		return ""
	}
	return "/* Tema iz Administracije › Tema; zadane boje su u style.css */\n" +
		t.CSSZa(":root", `:root[data-theme="dark"]`)
}

// CSSZa je isti stil za zadane selektore svijetle i tamne teme; stranica
// teme njime boja pregled prije spremanja
func (t Tema) CSSZa(svijetla, tamna string) string {
	var b strings.Builder
	if blok := t.Svijetla.svijetliTokeni(); blok != "" {
		b.WriteString(svijetla + " {\n" + blok + "}\n")
	}
	if blok := t.Tamna.tamniTokeni(); blok != "" {
		b.WriteString(tamna + " {\n" + blok + "}\n")
	}
	return b.String()
}

func token(b *strings.Builder, ime, vrijednost string) {
	b.WriteString("  " + ime + ": " + vrijednost + ";\n")
}

func (c BojeTeme) svijetliTokeni() string {
	var b strings.Builder
	if p := c.Glavna; p != "" {
		token(&b, "--primary", p)
		token(&b, "--secondary", p)
		token(&b, "--primary-hover", miješaj(p, "#000000", 0.4))
		token(&b, "--primary-light", miješaj(p, "#ffffff", 0.9))
		token(&b, "--primary-muted", miješaj(p, "#ffffff", 0.44))
		token(&b, "--tint-blue-bg", miješaj(p, "#ffffff", 0.88))
		token(&b, "--tint-blue-fg", p)
		token(&b, "--tint-sky-bg", miješaj(p, "#ffffff", 0.94))
		token(&b, "--znacka-rub", miješaj(p, "#ffffff", 0.77))
		token(&b, "--ploca-rub", miješaj(p, "#ffffff", 0.81))
		token(&b, "--rub-hover", miješaj(p, "#ffffff", 0.7))
		token(&b, "--fokus", rgbProzirno(p, 0.28))
		token(&b, "--hero", hero(p))
		token(&b, "--ploca-pozadina", fmt.Sprintf("linear-gradient(145deg, #ffffff 0%%, %s 45%%, %s 100%%)",
			miješaj(p, "#ffffff", 0.97), miješaj(p, "#ffffff", 0.93)))
	}
	if a := c.Naglasak; a != "" {
		tamniji := poveznicaSvijetla(a)
		token(&b, "--accent", a)
		token(&b, "--accent-dark", tamniji)
		token(&b, "--accent-light", miješaj(a, "#ffffff", 0.89))
		token(&b, "--success", tamniji)
		token(&b, "--success-bg", miješaj(a, "#ffffff", 0.89))
		token(&b, "--mirno-rub", miješaj(a, "#ffffff", 0.67))
		token(&b, "--mirno-fg", miješaj(a, "#000000", 0.45))
	}
	if g := c.Gumb; g != "" {
		token(&b, "--gumb", g)
		token(&b, "--gumb-hover", miješaj(g, "#000000", 0.4))
	}
	return b.String()
}

func (c BojeTeme) tamniTokeni() string {
	var b strings.Builder
	if p := c.Glavna; p != "" {
		token(&b, "--primary", p)
		token(&b, "--primary-hover", miješaj(p, "#ffffff", 0.22))
		token(&b, "--primary-light", miješaj(p, tamnaPodloga, 0.875))
		token(&b, "--secondary", miješaj(p, "#ffffff", 0.72))
		token(&b, "--tint-blue-bg", miješaj(p, tamnaPodloga, 0.875))
		token(&b, "--tint-blue-fg", miješaj(p, "#ffffff", 0.4))
		token(&b, "--tint-sky-bg", miješaj(p, tamnaPodloga, 0.9))
		token(&b, "--znacka-rub", rgbProzirno(p, 0.32))
		token(&b, "--ploca-rub", rgbProzirno(p, 0.22))
		token(&b, "--fokus", rgbProzirno(p, 0.45))
		token(&b, "--hero", hero(p))
	}
	if a := c.Naglasak; a != "" {
		svjetliji := poveznicaTamna(a)
		token(&b, "--accent", a)
		token(&b, "--accent-dark", svjetliji)
		token(&b, "--accent-light", miješaj(a, tamnaPodloga, 0.88))
		token(&b, "--success", svjetliji)
		token(&b, "--success-bg", miješaj(a, tamnaPodloga, 0.88))
		token(&b, "--mirno-rub", rgbProzirno(a, 0.28))
		token(&b, "--mirno-fg", miješaj(a, "#ffffff", 0.4))
	}
	if g := c.Gumb; g != "" {
		token(&b, "--gumb", g)
		token(&b, "--gumb-hover", miješaj(g, "#ffffff", 0.06))
	}
	return b.String()
}

// ---- boje ----

func rgb(hex string) (r, g, b float64) {
	v, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	return float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff)
}

// miješaj vraća boju a pomiješanu s bojom b; udio je dio boje b (0 do 1)
func miješaj(a, b string, udio float64) string {
	r1, g1, b1 := rgb(a)
	r2, g2, b2 := rgb(b)
	m := func(x, y float64) int { return int(math.Round(x + (y-x)*udio)) }
	return fmt.Sprintf("#%02x%02x%02x", m(r1, r2), m(g1, g2), m(b1, b2))
}

// hero je gradijent velikog natpisa: ton glavne boje, ali uvijek taman, da
// bijeli tekst na njemu ostane čitljiv i kad je glavna boja svijetla
func hero(glavna string) string {
	h, s, _ := hsl(glavna)
	tamno := func(l float64) string {
		// žuta i zelena su svijetle i pri maloj svjetlini, pa se tamne dalje
		for ; l > 0 && Kontrast("#ffffff", izHSL(h, s, l)) < 4.5; l -= 0.01 {
		}
		return izHSL(h, s, l)
	}
	return fmt.Sprintf("linear-gradient(135deg, %s 0%%, %s 60%%, %s 100%%)",
		tamno(0.17), tamno(0.27), tamno(0.39))
}

// hsl vraća ton (0 do 360), zasićenje i svjetlinu (0 do 1)
func hsl(hex string) (h, s, l float64) {
	r, g, b := rgb(hex)
	r, g, b = r/255, g/255, b/255
	maks, min := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (maks + min) / 2
	d := maks - min
	if d == 0 {
		return 0, 0, l
	}
	s = d / (1 - math.Abs(2*l-1))
	switch maks {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l
}

func izHSL(h, s, l float64) string {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	k := func(v float64) int { return int(math.Round((v + m) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", k(r), k(g), k(b))
}

func rgbProzirno(hex string, alfa float64) string {
	r, g, b := rgb(hex)
	return fmt.Sprintf("rgb(%d %d %d / %.2f)", int(r), int(g), int(b), alfa)
}

func svjetlina(hex string) float64 {
	r, g, b := rgb(hex)
	k := func(c float64) float64 {
		c /= 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*k(r) + 0.7152*k(g) + 0.0722*k(b)
}

// Kontrast je omjer kontrasta dviju boja po WCAG-u (1 do 21)
func Kontrast(a, b string) float64 {
	la, lb := svjetlina(a), svjetlina(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
