package web

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
)

// Područja na Karti (Odluke (4. 11. 2026.)): županije, gradovi i općine te
// branjena područja, svako s granicom i imenom. Izvorne granice su velike
// (općine 28 MB) i nose imena čelnika i kontakte, pa Karta dobiva
// pojednostavljene granice samo s nazivom, složene jednom po pokretanju.

// podrucjeKarte je jedan sloj područja: koliko granica smije odstupiti od
// izvorne (u stupnjevima) i kako se iz svojstava slaže naziv
type podrucjeKarte struct {
	kljuc, naziv string
	ukljucen     bool
	tolerancija  float64
	natpis       func(map[string]any) (naziv, kratko string)
}

var podrucjaKarte = []podrucjeKarte{
	{kljuc: "zupanije", naziv: "Županije", ukljucen: true, tolerancija: 0.0008, natpis: func(p map[string]any) (string, string) {
		n := tekstSvojstva(p, "name")
		return n, n
	}},
	{kljuc: "opcine", naziv: "Gradovi i općine", tolerancija: 0.0003, natpis: func(p map[string]any) (string, string) {
		n := tekstSvojstva(p, "name")
		if strings.EqualFold(tekstSvojstva(p, "type"), "GRAD") {
			return "Grad " + n, n
		}
		return "Općina " + n, n
	}},
	{kljuc: "branjena-podrucja", naziv: "Branjena područja", ukljucen: true, tolerancija: 0.0003, natpis: func(p map[string]any) (string, string) {
		bp := fmt.Sprint(p["bp"])
		return "BP " + bp + " · " + tekstSvojstva(p, "naziv"), "BP " + bp
	}},
}

// slojeviPodrucja su retci skupine Područja u ploči slojeva
func slojeviPodrucja() []SlojKarte {
	out := make([]SlojKarte, 0, len(podrucjaKarte))
	for _, p := range podrucjaKarte {
		out = append(out, SlojKarte{Kljuc: p.kljuc, Naziv: p.naziv, Ikona: "map", Granice: true, Ukljucen: p.ukljucen})
	}
	return out
}

func tekstSvojstva(p map[string]any, kljuc string) string {
	s, _ := p[kljuc].(string)
	return strings.TrimSpace(s)
}

// SetGranice daje Karti izvorne granice područja (GeoJSON po ključu sloja)
func (h *KartaHandler) SetGranice(izvor func(kljuc string) ([]byte, error)) {
	h.granice = &graniceKarte{izvor: izvor, slozene: map[string]*slozenaGranica{}}
}

// graniceKarte drže izvor i jednom složene slojeve područja
type graniceKarte struct {
	izvor   func(kljuc string) ([]byte, error)
	mu      sync.Mutex
	slozene map[string]*slozenaGranica
}

// slozenaGranica je pojednostavljeni GeoJSON jednog sloja, i sažet
type slozenaGranica struct {
	raw, gz []byte
	err     error
}

// HandlePodrucje vraća granice jednog sloja područja: GET
// /karta/podrucja/{datoteka}, npr. opcine.geojson
func (h *KartaHandler) HandlePodrucje(w http.ResponseWriter, r *http.Request) {
	kljuc := strings.TrimSuffix(r.PathValue("datoteka"), ".geojson")
	var podrucje *podrucjeKarte
	for i := range podrucjaKarte {
		if podrucjaKarte[i].kljuc == kljuc {
			podrucje = &podrucjaKarte[i]
		}
	}
	if podrucje == nil || h.granice == nil {
		http.Error(w, "Nema takvog sloja područja", http.StatusNotFound)
		return
	}
	g := h.granice.zaKartu(*podrucje)
	if g.err != nil {
		http.Error(w, "Granice nisu učitane: "+g.err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(g.gz) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(g.gz)
		return
	}
	w.Write(g.raw)
}

// zaKartu složi sloj jednom; neuspjeh se pamti dok program radi, kao i
// granice županija na stranici teritorija
func (k *graniceKarte) zaKartu(p podrucjeKarte) *slozenaGranica {
	k.mu.Lock()
	defer k.mu.Unlock()
	if g, ok := k.slozene[p.kljuc]; ok {
		return g
	}
	g := &slozenaGranica{}
	izvor, err := k.izvor(p.kljuc)
	if err == nil && len(izvor) == 0 {
		err = errors.New("nema datoteke " + p.kljuc + ".geojson")
	}
	if err == nil {
		g.raw, err = pojednostaviPodrucja(izvor, p)
	}
	if err == nil {
		g.gz = sazmi(g.raw)
	}
	g.err = err
	k.slozene[p.kljuc] = g
	return g
}

// sazmi: gzip u memoriji; pisanje u međuspremnik ne pada
func sazmi(b []byte) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(b) // u bytes.Buffer pisanje ne pada
	_ = gw.Close()     // isto
	return buf.Bytes()
}

// pojednostaviPodrucja: granice bez suvišnih točaka i svojstva samo s
// nazivom, kratkim nazivom za natpis i točkom natpisa
func pojednostaviPodrucja(izvor []byte, p podrucjeKarte) ([]byte, error) {
	var zbirka struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(izvor, &zbirka); err != nil {
		return nil, errors.New("granice nisu ispravan GeoJSON")
	}
	out := geoZbirka{Type: "FeatureCollection", Features: make([]geoZnacajka, 0, len(zbirka.Features))}
	for _, f := range zbirka.Features {
		poligoni, err := poligoniGranice(f.Geometry.Type, f.Geometry.Coordinates)
		if err != nil || len(poligoni) == 0 {
			continue
		}
		naziv, kratko := p.natpis(f.Properties)
		svojstva := map[string]any{"sloj": p.kljuc, "naziv": naziv, "kratko": kratko, "tocka": tockaNatpisa(f.Properties, poligoni)}
		for i := range poligoni {
			poligoni[i] = pojednostaviPoligon(poligoni[i], p.tolerancija)
		}
		g, _ := json.Marshal(map[string]any{"type": "MultiPolygon", "coordinates": poligoni}) // nizovi brojeva se uvijek pretvore
		out.Features = append(out.Features, geoZnacajka{Type: "Feature", Properties: svojstva, Geometry: g})
	}
	return json.Marshal(out)
}

// poligoniGranice čita Polygon i MultiPolygon kao popis poligona
func poligoniGranice(vrsta string, koordinate json.RawMessage) ([][][][2]float64, error) {
	switch vrsta {
	case "Polygon":
		var p [][][2]float64
		err := json.Unmarshal(koordinate, &p)
		return [][][][2]float64{p}, err
	case "MultiPolygon":
		var mp [][][][2]float64
		err := json.Unmarshal(koordinate, &mp)
		return mp, err
	}
	return nil, nil
}

// pojednostaviPoligon pojednostavi svaki prsten; prsten koji bi spao na
// manje od četiri točke ostaje kakav je bio, a rupa se tada izostavi
func pojednostaviPoligon(prsteni [][][2]float64, tolerancija float64) [][][2]float64 {
	out := make([][][2]float64, 0, len(prsteni))
	for i, prsten := range prsteni {
		if len(prsten) < 4 {
			continue
		}
		if p := pojednostaviCrtu(prsten, tolerancija); len(p) >= 4 {
			out = append(out, p)
		} else if i == 0 {
			out = append(out, pojednostaviCrtu(prsten, 0))
		}
	}
	return out
}

// tockaNatpisa: točka iz izvora (branjena područja je imaju), inače
// težište najvećeg vanjskog prstena
func tockaNatpisa(svojstva map[string]any, poligoni [][][][2]float64) [2]float64 {
	if t, ok := svojstva["tocka"].([]any); ok && len(t) == 2 {
		x, okX := t[0].(float64)
		y, okY := t[1].(float64)
		if okX && okY {
			return [2]float64{x, y}
		}
	}
	var najveci [][2]float64
	najvise := -1.0
	for _, p := range poligoni {
		if len(p) > 0 {
			if a := math.Abs(povrsinaPrstena(p[0])); a > najvise {
				najveci, najvise = p[0], a
			}
		}
	}
	return teziste(najveci)
}

// povrsinaPrstena je površina u kvadratnim stupnjevima, s predznakom
func povrsinaPrstena(r [][2]float64) float64 {
	a := 0.0
	for i := 0; i+1 < len(r); i++ {
		a += r[i][0]*r[i+1][1] - r[i+1][0]*r[i][1]
	}
	return a / 2
}

// teziste prstena; za prsten bez površine prosjek točaka
func teziste(r [][2]float64) [2]float64 {
	a := povrsinaPrstena(r)
	if a == 0 {
		var x, y float64
		for _, t := range r {
			x, y = x+t[0], y+t[1]
		}
		n := math.Max(1, float64(len(r)))
		return [2]float64{zaokruzi6(x / n), zaokruzi6(y / n)}
	}
	var cx, cy float64
	for i := 0; i+1 < len(r); i++ {
		k := r[i][0]*r[i+1][1] - r[i+1][0]*r[i][1]
		cx += (r[i][0] + r[i+1][0]) * k
		cy += (r[i][1] + r[i+1][1]) * k
	}
	return [2]float64{zaokruzi6(cx / (6 * a)), zaokruzi6(cy / (6 * a))}
}
