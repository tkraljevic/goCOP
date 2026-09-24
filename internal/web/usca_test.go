package web

import (
	"fmt"
	"math"
	"testing"
)

// Sintetični tokovi: glavni ide ravno prema istoku uz 45,5° sa oznakama rkm
// svakih pola stupnja, pritoka mu prilazi sa sjevera i završava tik uz njega.
func sintetickiGlavni() geoTok {
	return geoTok{
		linija: [][2]float64{{18.0, 45.5}, {18.5, 45.5}, {19.0, 45.5}},
		oznake: []geoOznaka{{1400, [2]float64{18.0, 45.5}}, {1350, [2]float64{18.5, 45.5}}, {1300, [2]float64{19.0, 45.5}}},
	}
}

func TestUsceNaTokuInterpoliraKilometar(t *testing.T) {
	pritoka := geoTok{linija: [][2]float64{{18.25, 46.0}, {18.25, 45.7}, {18.25, 45.503}}}
	rkm, ok := usceNaToku(pritoka, sintetickiGlavni())
	if !ok {
		t.Fatal("ušće nije nađeno")
	}
	if math.Abs(rkm-1375) > 0.5 {
		t.Errorf("rkm ušća %.1f, očekivano 1375", rkm)
	}
	// Kraj pritoke je onaj bliže toku, ma kojim redom točke bile zapisane.
	obrnuto := geoTok{linija: [][2]float64{{18.25, 45.503}, {18.25, 45.7}, {18.25, 46.0}}}
	if r2, _ := usceNaToku(obrnuto, sintetickiGlavni()); math.Abs(r2-rkm) > 1e-9 {
		t.Errorf("obrnuti redoslijed točaka daje %.1f umjesto %.1f", r2, rkm)
	}
}

func TestUsceDalekoOdTokaNemaKilometra(t *testing.T) {
	pritoka := geoTok{linija: [][2]float64{{18.25, 46.0}, {18.25, 45.8}}} // 33 km sjeverno
	if _, ok := usceNaToku(pritoka, sintetickiGlavni()); ok {
		t.Error("pritoka koja ne dolazi do toka dobila je ušće")
	}
}

func TestCitajGeoTok(t *testing.T) {
	b := []byte(`{"type":"FeatureCollection","features":[
	 {"type":"Feature","properties":{"tip":"vodotok"},"geometry":{"type":"LineString","coordinates":[[18,45.5],[19,45.5]]}},
	 {"type":"Feature","properties":{"tip":"rkm","rkm":1400},"geometry":{"type":"Point","coordinates":[18,45.5]}},
	 {"type":"Feature","properties":{"tip":"rkm","rkm":"1300"},"geometry":{"type":"Point","coordinates":[19,45.5]}}]}`)
	g, ok := citajGeoTok(b)
	if !ok || len(g.linija) != 2 {
		t.Fatalf("linija nije pročitana: %v", ok)
	}
	// Oznaka s rkm kao tekstom se preskače, brojčana ostaje.
	if len(g.oznake) != 1 || g.oznake[0].rkm != 1400 {
		t.Errorf("oznake: %+v", g.oznake)
	}
}

func TestKilometarIzOpisaIPadezi(t *testing.T) {
	if km, ok := kilometarIzOpisa("ušće u Dravu kod Petrijevaca, rkm 22+400"); !ok || math.Abs(km-22.4) > 1e-9 {
		t.Errorf("rkm iz opisa: %v %v", km, ok)
	}
	if _, ok := kilometarIzOpisa("ušće u Dravu kod Legrada"); ok {
		t.Error("opis bez kilometra dao je kilometar")
	}
	for _, c := range [][3]string{{"Drava", "Drave", "Dravu"}, {"Dunav", "Dunav", "Dunav"}, {"Karašica", "Karašice", "Karašicu"}} {
		if g, a := genitiv(c[0]), akuzativ(c[0]); g != c[1] || a != c[2] {
			t.Errorf("%s: %s/%s", c[0], g, a)
		}
	}
	_ = fmt.Sprint
}
