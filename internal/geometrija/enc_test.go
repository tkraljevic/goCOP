package geometrija

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestENCNaStvarnojOSMLiniji(t *testing.T) {
	for _, code := range []string{"rijeka-drava", "rijeka-dunav"} {
		t.Run(code, func(t *testing.T) {
			data, err := Ucitaj("", code)
			if err != nil {
				t.Fatal(err)
			}
			out, err := PrimijeniStacionazu(code, data)
			if err != nil {
				t.Fatal(err)
			}
			var before, after struct {
				Features []struct {
					Geometry   json.RawMessage
					Properties map[string]any
				}
			}
			if err = json.Unmarshal(data, &before); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(out, &after); err != nil {
				t.Fatal(err)
			}
			var a, b any
			json.Unmarshal(before.Features[0].Geometry, &a)
			json.Unmarshal(after.Features[0].Geometry, &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatal("kalibracija je izmijenila OSM liniju")
			}
			count := 0
			for _, f := range after.Features {
				if f.Properties["status"] == "kalibrirano" {
					count++
				}
			}
			izvor, _ := SluzbenaSidra(code)
			if count != len(izvor.Sidra) {
				t.Fatalf("%d oznaka, očekivano %d", count, len(izvor.Sidra))
			}
			var g struct{ Coordinates [][2]float64 }
			json.Unmarshal(before.Features[0].Geometry, &g)
			k, err := NovaKalibracija(g.Coordinates, izvor.Sidra)
			if err != nil {
				t.Fatal(err)
			}
			maxD := 0.0
			for _, s := range izvor.Sidra {
				p, _ := Projektiraj(g.Coordinates, s.Tocka)
				maxD = math.Max(maxD, p.UdaljenostKM)
				r, ok := k.RKM(p.UzduzKM)
				if !ok || math.Abs(r-s.Rkm) > 1e-8 {
					t.Fatalf("sidro %g: %g %v", s.Rkm, r, ok)
				}
			}
			t.Logf("%d sidara; najveći odmak ENC–OSM %.3f km", count, maxD)
			// Ponovna primjena ne umnožava oznake.
			again, err := PrimijeniStacionazu(code, out)
			if err != nil {
				t.Fatal(err)
			}
			var decoded1, decoded2 any
			json.Unmarshal(out, &decoded1)
			json.Unmarshal(again, &decoded2)
			if !reflect.DeepEqual(decoded1, decoded2) {
				t.Fatal("kalibracija nije idempotentna")
			}
		})
	}
}

func TestKalibracijaNejednakiSegmentiIValidacija(t *testing.T) {
	line := [][2]float64{{0, 0}, {1, 0}, {2, 0}}
	anchors := []Sidro{{Rkm: 100, Tocka: line[0]}, {Rkm: 90, Tocka: line[1]}, {Rkm: 50, Tocka: line[2]}}
	k, err := NovaKalibracija(line, anchors)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ x, r float64 }{{.5, 95}, {1.5, 70}} {
		p, _ := Projektiraj(line, [2]float64{tc.x, 0})
		r, ok := k.RKM(p.UzduzKM)
		if !ok || math.Abs(r-tc.r) > 1e-8 {
			t.Fatalf("rkm %g, očekivano %g", r, tc.r)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), -1, 101} {
		if _, ok := k.Uzduz(v); ok {
			t.Fatalf("prihvaćen %v", v)
		}
	}
	anchors[2].Rkm = 95
	if _, err := NovaKalibracija(line, anchors); err == nil {
		t.Fatal("prihvaćen preokret stacionaže")
	}
	anchors[2].Rkm = math.NaN()
	if _, err := NovaKalibracija(line, anchors); err == nil {
		t.Fatal("prihvaćen NaN")
	}
}
