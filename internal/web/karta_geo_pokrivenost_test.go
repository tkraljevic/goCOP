package web

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// geoMapa pretvara GeoJSON tekst u mapu, kako ga čita karta obuhvata
func geoMapa(t *testing.T, s string) map[string]any {
	t.Helper()
	var g map[string]any
	if err := json.Unmarshal([]byte(s), &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// Svaka vrsta geometrije daje svoje nizove točaka; točka (0, 0) i krivo
// zapisane koordinate se preskaču, a nepoznata vrsta ne daje ništa.
func TestGeoTockePoVrstiGeometrije(t *testing.T) {
	for _, s := range []struct {
		opis, geo string
		duljine   []int // broj točaka u svakom nizu
	}{
		{"točka", `{"type":"Point","coordinates":[18.85,45.84]}`, []int{1}},
		{"točka u ishodištu", `{"type":"Point","coordinates":[0,0]}`, nil},
		{"točka bez širine", `{"type":"Point","coordinates":[18.85]}`, nil},
		{"točka s tekstom", `{"type":"Point","coordinates":["18.85","45.84"]}`, nil},
		{"crta", `{"type":"LineString","coordinates":[[18.8,45.8],[18.9,45.8],[0,0]]}`, []int{2}},
		{"više točaka", `{"type":"MultiPoint","coordinates":[[18.8,45.8],[18.9,45.9]]}`, []int{2}},
		{"crta bez valjanih točaka", `{"type":"LineString","coordinates":[[0,0]]}`, nil},
		{"crta koja nije niz", `{"type":"LineString","coordinates":"18.8,45.8"}`, nil},
		{"poligon s rupom", `{"type":"Polygon","coordinates":[[[18.8,45.8],[18.9,45.8],[18.9,45.9],[18.8,45.8]],[[18.85,45.85],[18.86,45.85],[18.85,45.85]]]}`, []int{4, 3}},
		{"više crta", `{"type":"MultiLineString","coordinates":[[[18.8,45.8],[18.9,45.8]],[[0,0]],[[18.7,45.7],[18.6,45.6],[18.5,45.5]]]}`, []int{2, 3}},
		{"više poligona", `{"type":"MultiPolygon","coordinates":[[[[18.8,45.8],[18.9,45.8],[18.8,45.8]]],[[[18.1,45.1],[18.2,45.2],[18.1,45.1]],[[18.15,45.15],[18.16,45.16]]]]}`, []int{3, 3, 2}},
		{"zbirka", `{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[18.85,45.84]},{"type":"LineString","coordinates":[[18.8,45.8],[18.9,45.8]]},"krivo"]}`, []int{1, 2}},
		{"zbirka bez geometrija", `{"type":"GeometryCollection"}`, nil},
		{"nepoznata vrsta", `{"type":"Feature","geometry":{"type":"Point","coordinates":[18.85,45.84]}}`, nil},
		{"bez vrste", `{"coordinates":[18.85,45.84]}`, nil},
	} {
		t.Run(s.opis, func(t *testing.T) {
			nizovi := geoTocke(geoMapa(t, s.geo))
			if len(nizovi) != len(s.duljine) {
				t.Fatalf("nizova %d, očekivano %d: %v", len(nizovi), len(s.duljine), nizovi)
			}
			for i, n := range nizovi {
				if len(n) != s.duljine[i] {
					t.Errorf("niz %d ima %d točaka, očekivano %d", i, len(n), s.duljine[i])
				}
				for _, tc := range n {
					if tc == [2]float64{} {
						t.Errorf("niz %d nosi točku (0, 0)", i)
					}
				}
			}
		})
	}
}

// Redoslijed u paru je [duljina, širina], kao u GeoJSON-u; zamjena bi
// obuhvat preselila na drugi kraj svijeta.
func TestGeoTockeCuvajuRedoslijedDuljinaSirina(t *testing.T) {
	n := geoTocke(geoMapa(t, `{"type":"Point","coordinates":[18.85,45.84,90]}`))
	if len(n) != 1 || len(n[0]) != 1 || n[0][0] != [2]float64{18.85, 45.84} {
		t.Errorf("točka: %v, očekivano jedan niz s [18.85 45.84]", n)
	}
}

// pločicePoZumu je poslužitelj jednobojnih pločica koji pamti na kojem su
// zumu tražene
type plocicePoZumu struct {
	srv    *httptest.Server
	mu     sync.Mutex
	zumovi map[int]int
}

var bojaPlocice = color.RGBA{200, 210, 220, 255}

func novePlocicePoZumu(t *testing.T, status int) *plocicePoZumu {
	t.Helper()
	p := &plocicePoZumu{zumovi: map[int]int{}}
	var slika bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, velicinaPlocice, velicinaPlocice))
	for y := 0; y < velicinaPlocice; y++ {
		for x := 0; x < velicinaPlocice; x++ {
			img.Set(x, y, bojaPlocice)
		}
	}
	if err := png.Encode(&slika, img); err != nil {
		t.Fatal(err)
	}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// put je /{z}/{x}/{y}.png
		dijelovi := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if z, err := strconv.Atoi(dijelovi[0]); err == nil {
			p.mu.Lock()
			p.zumovi[z]++
			p.mu.Unlock()
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(slika.Bytes())
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *plocicePoZumu) predlozak() string { return p.srv.URL + "/{z}/{x}/{y}.png" }

// zum vraća jedini zum na kojem su pločice tražene
func (p *plocicePoZumu) zum(t *testing.T) int {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.zumovi) != 1 {
		t.Fatalf("pločice tražene na zumovima %v, očekivan jedan", p.zumovi)
	}
	for z := range p.zumovi {
		return z
	}
	return 0
}

func dekodirajKartu(t *testing.T, k *KartaSlika) *image.RGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(k.PNG))
	if err != nil {
		t.Fatalf("karta nije PNG: %v", err)
	}
	rgba := image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	return rgba
}

func jeBoja(img *image.RGBA, x, y int, c color.RGBA) bool {
	return img.RGBAAt(x, y) == c
}

// Bez izvora pločica, bez geometrije ili s geometrijom bez ijedne točke nema
// karte — i ne traži se nijedna pločica.
func TestKartaObuhvataBezCegaNema(t *testing.T) {
	p := novePlocicePoZumu(t, http.StatusOK)
	k := KartaPostavke{Plocice: p.predlozak()}
	ctx := context.Background()
	for _, s := range []struct {
		opis string
		k    KartaPostavke
		geo  string
	}{
		{"bez izvora pločica", KartaPostavke{}, `{"type":"Point","coordinates":[18.85,45.84]}`},
		{"prazna geometrija", k, "  \n"},
		{"pokvaren JSON", k, `{"type":"Point",`},
		{"bez točaka", k, `{"type":"LineString","coordinates":[]}`},
		{"samo ishodište", k, `{"type":"Point","coordinates":[0,0]}`},
	} {
		if karta := slozKartuObuhvata(ctx, s.k, s.geo, "http://localhost/"); karta != nil {
			t.Errorf("%s: složena je karta", s.opis)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.zumovi) != 0 {
		t.Errorf("tražene su pločice iako nema što nacrtati: %v", p.zumovi)
	}
}

// Kad poslužitelj pločica ne odgovara, karte nema: pola karte s praznim
// pločicama izgleda kao da obuhvata ondje nema.
func TestKartaObuhvataBezPlocicaNema(t *testing.T) {
	p := novePlocicePoZumu(t, http.StatusNotFound)
	k := KartaPostavke{Plocice: p.predlozak()}
	if karta := slozKartuObuhvata(context.Background(), k,
		`{"type":"LineString","coordinates":[[18.80,45.80],[18.81,45.80]]}`, "http://localhost/"); karta != nil {
		t.Error("karta je složena iako pločica nema")
	}
}

// Vodoravna crta po dionici: sredina obuhvata pada u sredinu slike, crta je
// tamna na bijeloj podlozi, a daleko od crte ostaje karta.
func TestKartaObuhvataCrtaJeUSredini(t *testing.T) {
	p := novePlocicePoZumu(t, http.StatusOK)
	k := KartaPostavke{Plocice: p.predlozak(), Zasluge: "© Probni izvor"}
	karta := slozKartuObuhvata(context.Background(), k,
		`{"type":"LineString","coordinates":[[18.80,45.80],[18.90,45.80]]}`, "http://localhost/")
	if karta == nil {
		t.Fatal("karta nije složena")
	}
	if karta.Sirina != sirinaKarte || karta.Visina != visinaKarte {
		t.Errorf("veličina %d×%d, očekivano %d×%d", karta.Sirina, karta.Visina, sirinaKarte, visinaKarte)
	}
	if karta.Zasluge != "© Probni izvor" {
		t.Errorf("zasluge %q", karta.Zasluge)
	}
	// 0,1° duljine na zumu 14 je oko 1165 točaka, šire od slike; na 13 stane
	if z := p.zum(t); z != 13 {
		t.Errorf("zum %d, očekivano 13", z)
	}
	img := dekodirajKartu(t, karta)
	cx, cy := sirinaKarte/2, visinaKarte/2
	tamnih, bijelih := 0, 0
	for y := cy - 6; y <= cy+6; y++ {
		switch img.RGBAAt(cx, y) {
		case color.RGBA(crnaBoja):
			tamnih++
		case color.RGBA(bijelaBoja):
			bijelih++
		}
	}
	// tamna crta debljine 3 točke na bijeloj podlozi debljine 7
	if tamnih != 3 || bijelih != 4 {
		t.Errorf("u stupcu sredine %d tamnih i %d bijelih točaka, očekivano 3 i 4", tamnih, bijelih)
	}
	for _, xy := range [][2]int{{5, 5}, {cx, 20}, {cx, visinaKarte - 20}, {sirinaKarte - 5, visinaKarte - 5}} {
		if !jeBoja(img, xy[0], xy[1], bojaPlocice) {
			t.Errorf("točka %v nije boje karte: %v", xy, img.RGBAAt(xy[0], xy[1]))
		}
	}
	// crta ne seže do rubova: rub je 24 točke
	if !jeBoja(img, 10, cy, bojaPlocice) || !jeBoja(img, sirinaKarte-10, cy, bojaPlocice) {
		t.Error("crta seže do ruba slike")
	}
}

// Jedna točka dobiva oznaku u sredini slike, na najvećem zumu: križić s
// krakovima od 6 točaka u tamnom prstenu s bijelim rubovima.
func TestKartaObuhvataTockaJeOznaka(t *testing.T) {
	p := novePlocicePoZumu(t, http.StatusOK)
	karta := slozKartuObuhvata(context.Background(), KartaPostavke{Plocice: p.predlozak()},
		`{"type":"Point","coordinates":[18.854722,45.845833]}`, "http://localhost/")
	if karta == nil {
		t.Fatal("karta nije složena")
	}
	if z := p.zum(t); z != zoomKarte {
		t.Errorf("zum %d, očekivano %d", z, zoomKarte)
	}
	img := dekodirajKartu(t, karta)
	cx, cy := sirinaKarte/2, visinaKarte/2
	crna, bijela := color.RGBA(crnaBoja), color.RGBA(bijelaBoja)
	// križić: vodoravni i okomiti krak od -6 do +6 kroz sredinu
	for d := -6; d <= 6; d++ {
		if !jeBoja(img, cx+d, cy, crna) {
			t.Errorf("vodoravni krak: točka %+d nije tamna: %v", d, img.RGBAAt(cx+d, cy))
		}
		if !jeBoja(img, cx, cy+d, crna) {
			t.Errorf("okomiti krak: točka %+d nije tamna: %v", d, img.RGBAAt(cx, cy+d))
		}
	}
	// između krakova i iza njihova kraja vidi se karta, ne oznaka
	for _, xy := range [][2]int{{cx + 3, cy + 3}, {cx - 3, cy - 3}, {cx + 7, cy}, {cx, cy - 7}} {
		if !jeBoja(img, xy[0], xy[1], bojaPlocice) {
			t.Errorf("točka %v nije boje karte: %v", xy, img.RGBAAt(xy[0], xy[1]))
		}
	}
	// prsten oko križića: bijeli rub na 8 i 12, tamno od 9 do 11
	for _, smjer := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		for r, boja := range map[int]color.RGBA{8: bijela, 9: crna, 10: crna, 11: crna, 12: bijela, 13: bojaPlocice} {
			x, y := cx+smjer[0]*r, cy+smjer[1]*r
			if !jeBoja(img, x, y, boja) {
				t.Errorf("prsten u smjeru %v na %d: %v, očekivano %v", smjer, r, img.RGBAAt(x, y), boja)
			}
		}
	}
	if !jeBoja(img, cx+40, cy+40, bojaPlocice) {
		t.Error("oznaka je veća nego što treba")
	}
}

// Najveći zum iz postavki ograničava kartu i kad bi obuhvat stao na većem;
// obuhvat preko pola Hrvatske spušta zum do 7.
func TestKartaObuhvataZumPoObuhvatu(t *testing.T) {
	p := novePlocicePoZumu(t, http.StatusOK)
	if slozKartuObuhvata(context.Background(), KartaPostavke{Plocice: p.predlozak(), NajviseZ: 11},
		`{"type":"Point","coordinates":[18.85,45.84]}`, "") == nil {
		t.Fatal("karta nije složena")
	}
	if z := p.zum(t); z != 11 {
		t.Errorf("zum %d, očekivano 11 iz postavki", z)
	}

	siroko := novePlocicePoZumu(t, http.StatusOK)
	if slozKartuObuhvata(context.Background(), KartaPostavke{Plocice: siroko.predlozak()},
		`{"type":"Polygon","coordinates":[[[13.5,45.0],[19.5,45.0],[19.5,46.5],[13.5,45.0]]]}`, "") == nil {
		t.Fatal("široka karta nije složena")
	}
	if z := siroko.zum(t); z != 7 {
		t.Errorf("zum %d, očekivano 7", z)
	}
}

// Crta zadane debljine: kvadrat oko svake točke puta, i ništa izvan njega.
func TestCrtaDebljina(t *testing.T) {
	s := image.NewRGBA(image.Rect(0, 0, 20, 20))
	crna := image.NewUniform(color.RGBA(crnaBoja))
	crta(s, 5, 10, 15, 10, crna, 1)
	for x := 0; x < 20; x++ {
		for y := 0; y < 20; y++ {
			trebaBiti := x >= 4 && x <= 16 && y >= 9 && y <= 11
			if jeBoja(s, x, y, color.RGBA(crnaBoja)) != trebaBiti {
				t.Errorf("točka %d,%d: obojena=%v, očekivano %v", x, y, !trebaBiti, trebaBiti)
			}
		}
	}

	// crta duljine nula je jedna točka sa svojom debljinom
	t2 := image.NewRGBA(image.Rect(0, 0, 10, 10))
	crta(t2, 5, 5, 5, 5, crna, 0)
	obojenih := 0
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			if jeBoja(t2, x, y, color.RGBA(crnaBoja)) {
				obojenih++
			}
		}
	}
	if obojenih != 1 || !jeBoja(t2, 5, 5, color.RGBA(crnaBoja)) {
		t.Errorf("crta duljine nula obojila je %d točaka", obojenih)
	}

	// kosa crta dolazi do obje krajnje točke, a rub slike je ne ruši
	t3 := image.NewRGBA(image.Rect(0, 0, 10, 10))
	crta(t3, -3, -3, 9, 6, crna, 1)
	if !jeBoja(t3, 9, 6, color.RGBA(crnaBoja)) || !jeBoja(t3, 0, 0, color.RGBA(crnaBoja)) {
		t.Error("kosa crta ne dolazi do krajeva")
	}
}
