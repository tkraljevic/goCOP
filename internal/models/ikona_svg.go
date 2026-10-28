package models

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"regexp"
	"strings"
)

// VlastitaIkona je ikona točke učitana u Administraciji › Karte. Iz SVG-a se
// zadržavaju samo oblici (crte, krugovi, pravokutnici, poligoni) i njihova
// geometrija; skripte, poveznice, slike, stilovi i boje se izbacuju. Boja
// postaje currentColor, pa se ikona na karti crta bijelo na krugu u boji
// vrste, kao ugrađene.
type VlastitaIkona struct {
	Ime     string `json:"ime"` // "v-" i naziv bez kvačica, npr. v-crpka-velika
	Naziv   string `json:"naziv"`
	ViewBox string `json:"viewbox"`
	Sadrzaj string `json:"sadrzaj"` // pročišćeni oblici, za <symbol>
}

// Granice učitane ikone
const (
	NajvecaIkonaSVG    = 32 << 10
	NajviseOblikaIkone = 300
	NajviseVlastitih   = 24
)

var (
	obliciIkone = map[string]bool{"g": true, "path": true, "circle": true, "ellipse": true, "rect": true,
		"line": true, "polyline": true, "polygon": true}
	geometrijaIkone = map[string]bool{"d": true, "cx": true, "cy": true, "r": true, "rx": true, "ry": true,
		"x": true, "y": true, "width": true, "height": true, "x1": true, "y1": true, "x2": true, "y2": true,
		"points": true, "transform": true, "stroke-width": true, "stroke-linecap": true, "stroke-linejoin": true,
		"fill-rule": true, "clip-rule": true}
	// vrijednost geometrije: brojevi, slova naredbi puta i transformacija,
	// razmaci i zarezi; bez navodnika, <, &, url( i dvotočke
	sigurnaVrijednost = regexp.MustCompile(`^[0-9A-Za-z.,\-+\s()]*$`)
	viewBoxIkone      = regexp.MustCompile(`^-?[0-9.]+[ ,]+-?[0-9.]+[ ,]+[0-9.]+[ ,]+[0-9.]+$`)
)

// ProciscenaIkona čita učitani SVG i vraća ikonu s pročišćenim oblicima
func ProciscenaIkona(naziv string, svg []byte) (VlastitaIkona, error) {
	naziv = strings.TrimSpace(naziv)
	ik := VlastitaIkona{Ime: imeIkone(naziv), Naziv: naziv, ViewBox: "0 0 24 24"}
	switch {
	case naziv == "" || ik.Ime == "v-":
		return ik, errors.New("upišite naziv ikone")
	case len([]rune(naziv)) > 40:
		return ik, errors.New("naziv ikone je dulji od 40 znakova")
	case len(svg) > NajvecaIkonaSVG:
		return ik, fmt.Errorf("datoteka ikone je veća od %d KB", NajvecaIkonaSVG>>10)
	}
	vb, sadrzaj, err := prociscenSVG(svg)
	if err != nil {
		return ik, err
	}
	if vb != "" {
		ik.ViewBox = vb
	}
	ik.Sadrzaj = sadrzaj
	return ik, nil
}

// imeIkone: v- i naziv malim slovima bez kvačica, s crticama
func imeIkone(naziv string) string {
	dijelovi := strings.FieldsFunc(bezKvacica.Replace(strings.ToLower(naziv)), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	return "v-" + strings.Join(dijelovi, "-")
}

var bezKvacica = strings.NewReplacer("č", "c", "ć", "c", "š", "s", "ž", "z", "đ", "d")

// prociscivac ispisuje oblike iznova dok čita SVG
type prociscivac struct {
	out      strings.Builder
	viewBox  string
	korijen  bool
	preskoci int // dubina elementa koji se preskače sa svim sadržajem
	oblika   int
}

func (c *prociscivac) pocetak(t xml.StartElement) error {
	switch {
	case c.preskoci > 0:
		c.preskoci++
	case !c.korijen:
		if t.Name.Local != "svg" {
			return errors.New("datoteka nije SVG")
		}
		c.korijen, c.viewBox = true, viewBoxIz(t)
	case obliciIkone[t.Name.Local]:
		if c.oblika++; c.oblika > NajviseOblikaIkone {
			return fmt.Errorf("ikona ima više od %d oblika", NajviseOblikaIkone)
		}
		c.out.WriteString("<" + t.Name.Local + svojstvaOblika(t) + ">")
	default:
		c.preskoci = 1
	}
	return nil
}

func (c *prociscivac) kraj(t xml.EndElement) {
	if c.preskoci > 0 {
		c.preskoci--
	} else if obliciIkone[t.Name.Local] {
		c.out.WriteString("</" + t.Name.Local + ">")
	}
}

// prociscenSVG vraća viewBox korijena i oblike ispisane iznova, samo s
// dopuštenim svojstvima; nepoznati element preskače se sa svim sadržajem
func prociscenSVG(svg []byte) (string, string, error) {
	d := xml.NewDecoder(bytes.NewReader(svg))
	d.Strict = false
	var c prociscivac
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", errors.New("datoteka nije ispravan SVG")
		}
		if t, ok := tok.(xml.StartElement); ok {
			if err := c.pocetak(t); err != nil {
				return "", "", err
			}
		} else if t, ok := tok.(xml.EndElement); ok {
			c.kraj(t)
		}
	}
	if c.oblika == 0 {
		return "", "", errors.New("u ikoni nema nijednog oblika")
	}
	return c.viewBox, c.out.String(), nil
}

func viewBoxIz(t xml.StartElement) string {
	for _, a := range t.Attr {
		if a.Name.Local == "viewBox" && viewBoxIkone.MatchString(strings.TrimSpace(a.Value)) {
			return strings.Join(strings.FieldsFunc(a.Value, func(r rune) bool { return r == ' ' || r == ',' }), " ")
		}
	}
	return ""
}

// svojstvaOblika: geometrija ostaje, a ispuna i crta samo kao none ili
// currentColor (i iz style="fill:…"), da ikona prati boju karte
func svojstvaOblika(t xml.StartElement) string {
	var b strings.Builder
	boje := map[string]string{}
	for _, a := range t.Attr {
		ime, v := a.Name.Local, strings.TrimSpace(a.Value)
		switch {
		case a.Name.Space != "":
		case ime == "fill" || ime == "stroke":
			boje[ime] = v
		case ime == "style":
			bojeIzStila(v, boje)
		case geometrijaIkone[ime] && sigurnaVrijednost.MatchString(v):
			b.WriteString(" " + ime + `="`)
			_ = xml.EscapeText(&b, []byte(v)) // strings.Builder ne vraća grešku
			b.WriteString(`"`)
		}
	}
	for _, ime := range []string{"fill", "stroke"} {
		if v, ok := boje[ime]; ok {
			b.WriteString(" " + ime + `="` + bojaIkone(v) + `"`)
		}
	}
	return b.String()
}

// bojeIzStila uzima fill i stroke iz style="…"
func bojeIzStila(stil string, boje map[string]string) {
	for _, dio := range strings.Split(stil, ";") {
		k, x, ok := strings.Cut(dio, ":")
		if k = strings.TrimSpace(k); ok && (k == "fill" || k == "stroke") {
			boje[k] = strings.TrimSpace(x)
		}
	}
}

func bojaIkone(v string) string {
	if v == "none" {
		return v
	}
	return "currentColor"
}

// SpriteVlastitih je SVG sa simbolom svake vlastite ikone, za <use href>
func (p PostavkeKarte) SpriteVlastitih() string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg">`)
	for _, ik := range p.Ikone {
		b.WriteString(`<symbol id="` + ik.Ime + `" viewBox="` + ik.ViewBox + `">` + ik.Sadrzaj + `</symbol>`)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// VerzijaVlastitih je otisak vlastitih ikona, za adresu koja se mijenja s njima
func (p PostavkeKarte) VerzijaVlastitih() string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(p.SpriteVlastitih())) // hash ne vraća grešku
	return fmt.Sprintf("%08x", h.Sum32())
}
