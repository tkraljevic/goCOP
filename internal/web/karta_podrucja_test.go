package web

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/models"
)

// granice za probu: županija s imenom župana, općina s dva dijela i
// branjeno područje s točkom natpisa
const (
	probneZupanije = `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":14,"name":"Probna županija","prefect":"Ana Županić","phone":"031 000"},
		"geometry":{"type":"Polygon","coordinates":[[[18,45],[18.5,45.0000001],[19,45],[19,46],[18,46],[18,45]]]}}]}`
	probneOpcine = `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":1,"name":"Proba","type":"GRAD","head_name":"Ivo Načelnik"},
		"geometry":{"type":"MultiPolygon","coordinates":[[[[18,45],[18.1,45],[18.1,45.1],[18,45.1],[18,45]]],[[[18.5,45],[18.52,45],[18.52,45.02],[18.5,45]]]]}},
		{"type":"Feature","properties":{"id":2,"name":"Pustara","type":"OPĆINA"},"geometry":{"type":"Point","coordinates":[18,45]}}]}`
	probnaPodrucja = `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"bp":16,"naziv":"Mali sliv Proba","tocka":[18.7,45.8]},
		"geometry":{"type":"Polygon","coordinates":[[[18.6,45.7],[18.8,45.7],[18.8,45.9],[18.6,45.9],[18.6,45.7]]]}}]}`
)

func kartaSGranicama(t *testing.T) (*KartaHandler, map[string]int) {
	t.Helper()
	pozivi := map[string]int{}
	h := NewKartaHandler(nil, nil, nil, nil, nil, nil)
	h.SetGranice(func(kljuc string) ([]byte, error) {
		pozivi[kljuc]++
		switch kljuc {
		case "zupanije":
			return []byte(probneZupanije), nil
		case "opcine":
			return []byte(probneOpcine), nil
		case "branjena-podrucja":
			return nil, errors.New("disk nije dostupan")
		}
		return nil, nil
	})
	return h, pozivi
}

func dohvatiPodrucje(t *testing.T, h *KartaHandler, datoteka string, gz bool) (*httptest.ResponseRecorder, geoZbirka) {
	t.Helper()
	r := httptest.NewRequest("GET", "/karta/podrucja/"+datoteka, nil)
	r.SetPathValue("datoteka", datoteka)
	if gz {
		r.Header.Set("Accept-Encoding", "gzip")
	}
	w := httptest.NewRecorder()
	h.HandlePodrucje(w, r)
	var z geoZbirka
	tijelo := w.Body.Bytes()
	if w.Header().Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(bytes.NewReader(tijelo))
		if err != nil {
			t.Fatal(err)
		}
		if tijelo, err = io.ReadAll(gr); err != nil {
			t.Fatal(err)
		}
	}
	if w.Code == 200 {
		if err := json.Unmarshal(tijelo, &z); err != nil {
			t.Fatalf("%s: %v", datoteka, err)
		}
	}
	return w, z
}

// Karta dobiva granice samo s nazivom: bez imena župana i načelnika, i
// pojednostavljene; točka bez površine se preskače.
func TestKartaGranicePodrucja(t *testing.T) {
	h, pozivi := kartaSGranicama(t)
	w, z := dohvatiPodrucje(t, h, "zupanije.geojson", true)
	if w.Code != 200 || len(z.Features) != 1 {
		t.Fatalf("županije: %d %+v", w.Code, z)
	}
	if strings.Contains(w.Body.String(), "Županić") {
		t.Error("granice nose ime župana")
	}
	p := z.Features[0].Properties
	if p["naziv"] != "Probna županija" || p["kratko"] != "Probna županija" || p["sloj"] != "zupanije" {
		t.Errorf("svojstva: %v", p)
	}
	var g struct {
		Type        string
		Coordinates [][][][2]float64
	}
	if err := json.Unmarshal(z.Features[0].Geometry, &g); err != nil {
		t.Fatal(err)
	}
	if g.Type != "MultiPolygon" || len(g.Coordinates[0][0]) != 5 {
		t.Errorf("pojednostavljeno: %s %v", g.Type, g.Coordinates)
	}
	if tocka, _ := p["tocka"].([]any); len(tocka) != 2 || tocka[0].(float64) != 18.5 || tocka[1].(float64) != 45.5 {
		t.Errorf("težište: %v", p["tocka"])
	}

	_, o := dohvatiPodrucje(t, h, "opcine.geojson", false)
	if len(o.Features) != 1 || o.Features[0].Properties["naziv"] != "Grad Proba" || o.Features[0].Properties["kratko"] != "Proba" {
		t.Errorf("općine: %+v", o.Features)
	}
	if strings.Contains(string(o.Features[0].Geometry), "18.52") == false {
		t.Error("drugi dio grada je izgubljen")
	}

	dohvatiPodrucje(t, h, "zupanije.geojson", false)
	if pozivi["zupanije"] != 1 {
		t.Errorf("granice se slažu jednom, a izvor je čitan %d puta", pozivi["zupanije"])
	}
	for _, d := range []string{"branjena-podrucja.geojson", "sektori.geojson", "naselja.geojson"} {
		if w, _ := dohvatiPodrucje(t, h, d, false); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", d, w.Code)
		}
	}
	if w, _ := dohvatiPodrucje(t, NewKartaHandler(nil, nil, nil, nil, nil, nil), "zupanije.geojson", false); w.Code != http.StatusNotFound {
		t.Errorf("bez izvora: %d", w.Code)
	}
}

// Branjeno područje dobiva kratki natpis s brojem i točku natpisa iz izvora.
func TestKartaNatpisBranjenogPodrucja(t *testing.T) {
	var bp podrucjeKarte
	for _, p := range podrucjaKarte {
		if p.kljuc == "branjena-podrucja" {
			bp = p
		}
	}
	raw, err := pojednostaviPodrucja([]byte(probnaPodrucja), bp)
	if err != nil {
		t.Fatal(err)
	}
	var z geoZbirka
	if err := json.Unmarshal(raw, &z); err != nil {
		t.Fatal(err)
	}
	p := z.Features[0].Properties
	if p["naziv"] != "BP 16 · Mali sliv Proba" || p["kratko"] != "BP 16" {
		t.Errorf("natpis: %v", p)
	}
	if tocka := p["tocka"].([]any); tocka[0].(float64) != 18.7 || tocka[1].(float64) != 45.8 {
		t.Errorf("točka iz izvora: %v", tocka)
	}
	if _, err := pojednostaviPodrucja([]byte("nije json"), bp); err == nil {
		t.Error("pokvarene granice moraju javiti grešku")
	}
}

// Prsten koji bi pojednostavljenjem spao ispod četiri točke ostaje cijel, a
// premala rupa se izostavi; težište prstena bez površine je prosjek točaka.
func TestPojednostaviPoligon(t *testing.T) {
	vanjski := [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 0}}
	rupa := [][2]float64{{0.2, 0.2}, {0.2000001, 0.2}, {0.2, 0.2000001}, {0.2, 0.2}}
	got := pojednostaviPoligon([][][2]float64{vanjski, rupa, {{0, 0}}}, 10)
	if len(got) != 1 || len(got[0]) != 4 {
		t.Errorf("poligon: %v", got)
	}
	if c := teziste([][2]float64{{1, 1}, {3, 1}, {1, 1}}); c != [2]float64{1.666667, 1} {
		t.Errorf("težište bez površine: %v", c)
	}
}

// Vodotoci idu u slojeve po redu vode; sloj bez upisanog reda postoji samo
// kad takvih vodotoka ima.
func TestSlojeviVodotokaPoRedu(t *testing.T) {
	tok := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[18,45],[18.1,45.1]]}}]}`
	vodotoci := []models.Watercourse{
		{Code: "rijeka-dunav", Name: "Dunav", Category: "MEĐUDRŽAVNE VODE", Geometry: tok},
		{Code: "kanal-a", Name: "A", Subcategory: "kanal I. reda", Geometry: tok},
		{Code: "kanal-b", Name: "B", Subcategory: "kanal II. reda"},
		{Code: "kanal-c", Name: "C", Subcategory: "kanal III. reda", Geometry: tok},
	}
	var z geoZbirka
	slojevi := slojeviVodotoka(vodotoci, &z, pravaKarte{})
	kljucevi := []string{}
	for _, s := range slojevi {
		kljucevi = append(kljucevi, s.Kljuc)
	}
	if strings.Join(kljucevi, ",") != "vodotoci-medudrzavni,vodotoci-i,vodotoci-ii,vodotoci-iii-iv" {
		t.Errorf("slojevi: %v", kljucevi)
	}
	if slojevi[2].Ucrtano != 0 || slojevi[2].Ukupno != 1 || slojevi[0].Naziv != "Međudržavni vodotoci" {
		t.Errorf("II. red: %+v", slojevi[2])
	}
	if len(z.Features) != 3 || z.Features[0].Properties["sloj"] != "vodotoci-medudrzavni" || z.Features[2].Properties["sloj"] != "vodotoci-iii-iv" {
		t.Errorf("značajke: %+v", z.Features)
	}
	vodotoci = append(vodotoci, models.Watercourse{Code: "potok-x", Name: "X"})
	if s := slojeviVodotoka(vodotoci, &geoZbirka{}, pravaKarte{}); len(s) != 5 || s[4].Kljuc != "vodotoci-bez-reda" || s[4].Ukupno != 1 {
		t.Errorf("bez reda: %+v", s)
	}
}

// Branjena područja Karta čita s diska čvora (mapa geometrija uz bazu), a
// županije iz programa.
func TestGranicePodrucjaIzvor(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "geometrija"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geometrija", "branjena-podrucja.geojson"), []byte(probnaPodrucja), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &TerritoriesHandler{podaci: dir}
	if b, err := h.GranicePodrucja("branjena-podrucja"); err != nil || !strings.Contains(string(b), "Mali sliv Proba") {
		t.Errorf("branjena područja: %v %.80s", err, b)
	}
	if b, err := h.GranicePodrucja("nema-ga"); err != nil || len(b) != 0 {
		t.Errorf("nepoznate granice: %v %d", err, len(b))
	}
}

// Jezera, akumulacije i retencije su u svom sloju, kao površine, a ne
// među vodotocima po redu; i u popisu onoga što nedostaje.
func TestKartaPovrsineVoda(t *testing.T) {
	povrsina := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"tip":"poligon"},"geometry":{"type":"Polygon","coordinates":[[[18,45],[18.1,45],[18.1,45.1],[18,45]]]}}]}`
	vodotoci := []models.Watercourse{
		{Code: "akumulacija-borovik", Name: "Borovik", OfficialName: "akumulacija Borovik", Kind: "akumulacija", Geometry: povrsina},
		{Code: "retencija-kusac", Name: "Kusac", OfficialName: "retencija Kusac", Kind: "retencija"},
		{Code: "kanal-a", Name: "A", Subcategory: "kanal I. reda"},
	}
	var z geoZbirka
	sloj := slojPovrsina(vodotoci, &z, pravaKarte{})
	if sloj.Kljuc != "retencije" || sloj.Ucrtano != 1 || sloj.Ukupno != 2 || sloj.Crta {
		t.Errorf("sloj: %+v", sloj)
	}
	if len(z.Features) != 1 || z.Features[0].Properties["vrsta"] != "akumulacija" || !strings.Contains(string(z.Features[0].Geometry), "Polygon") {
		t.Errorf("površina: %+v", z.Features)
	}
	for _, s := range slojeviVodotoka(vodotoci, &geoZbirka{}, pravaKarte{}) {
		if s.Ukupno > 0 && s.Kljuc != "vodotoci-i" {
			t.Errorf("površina vode među vodotocima: %+v", s)
		}
	}
	if slojVodotoka(vodotoci[1]) != "retencije" {
		t.Error("retencija bez površine mora biti u popisu sloja retencije")
	}
	if !vodaUcrtana(vodotoci[0]) || vodaUcrtana(vodotoci[1]) {
		t.Error("akumulacija s površinom je ucrtana, a retencija bez nje nije")
	}
}
