package web

import (
	"context"
	"encoding/json"
	"image"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"strings"
	"time"
)

// Karta onoga što je uz obilazak ucrtano na karti: crta povučena po dionici,
// točka ili poligon. Za razliku od karte prijave, koja ima jednu točku i
// stalno mjerilo, ovdje se mjerilo bira tako da ucrtano stane u sliku.
//
// Ucrtano nije GPS trag: u ranijoj evidenciji to je netko povlačio mišem, pa
// se ni ne prikazuje kao prijeđeni put nego kao označeno područje obilaska.

// geoTocke vadi iz GeoJSON geometrije nizove točaka [lon, lat]
func geoTocke(g map[string]any) [][][2]float64 {
	vrsta, _ := g["type"].(string)
	switch vrsta {
	case "GeometryCollection":
		var out [][][2]float64
		if gg, ok := g["geometries"].([]any); ok {
			for _, x := range gg {
				if m, ok := x.(map[string]any); ok {
					out = append(out, geoTocke(m)...)
				}
			}
		}
		return out
	case "Point":
		if t, ok := paraTocka(g["coordinates"]); ok {
			return [][][2]float64{{t}}
		}
	case "LineString", "MultiPoint":
		if niz, ok := nizTocaka(g["coordinates"]); ok {
			return [][][2]float64{niz}
		}
	case "Polygon", "MultiLineString":
		var out [][][2]float64
		if c, ok := g["coordinates"].([]any); ok {
			for _, x := range c {
				if niz, ok := nizTocaka(x); ok {
					out = append(out, niz)
				}
			}
		}
		return out
	case "MultiPolygon":
		var out [][][2]float64
		if c, ok := g["coordinates"].([]any); ok {
			for _, x := range c {
				if rings, ok := x.([]any); ok {
					for _, r := range rings {
						if niz, ok := nizTocaka(r); ok {
							out = append(out, niz)
						}
					}
				}
			}
		}
		return out
	}
	return nil
}

func paraTocka(v any) ([2]float64, bool) {
	par, ok := v.([]any)
	if !ok || len(par) < 2 {
		return [2]float64{}, false
	}
	lon, ok1 := par[0].(float64)
	lat, ok2 := par[1].(float64)
	if !ok1 || !ok2 || (lon == 0 && lat == 0) {
		return [2]float64{}, false
	}
	return [2]float64{lon, lat}, true
}

func nizTocaka(v any) ([][2]float64, bool) {
	niz, ok := v.([]any)
	if !ok {
		return nil, false
	}
	var out [][2]float64
	for _, x := range niz {
		if t, ok := paraTocka(x); ok {
			out = append(out, t)
		}
	}
	return out, len(out) > 0
}

// KartaObuhvata slaže kartu s ucrtanim obuhvatom obilaska; nil kad nema što
// nacrtati ili mreža ne odgovara
func slozKartuObuhvata(ctx context.Context, k KartaPostavke, geojson, odakle string) *KartaSlika {
	if !k.Ima() || k.Plocice == "" || strings.TrimSpace(geojson) == "" {
		return nil
	}
	var g map[string]any
	if json.Unmarshal([]byte(geojson), &g) != nil {
		return nil
	}
	nizovi := geoTocke(g)
	if len(nizovi) == 0 {
		return nil
	}
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)
	for _, niz := range nizovi {
		for _, t := range niz {
			minLon, maxLon = math.Min(minLon, t[0]), math.Max(maxLon, t[0])
			minLat, maxLat = math.Min(minLat, t[1]), math.Max(maxLat, t[1])
		}
	}
	// mjerilo: najveće pri kojem ucrtano stane u sliku, uz rub
	const rub = 24.0
	z := zoomKarte
	if k.NajviseZ > 0 {
		z = k.NajviseZ
	}
	for ; z > 6; z-- {
		x1, y1 := plocicaZa(maxLat, minLon, z)
		x2, y2 := plocicaZa(minLat, maxLon, z)
		w := math.Abs(x2-x1) * velicinaPlocice
		h := math.Abs(y2-y1) * velicinaPlocice
		if w <= sirinaKarte-2*rub && h <= visinaKarte-2*rub {
			break
		}
	}
	sx, sy := plocicaZa((minLat+maxLat)/2, (minLon+maxLon)/2, z)
	px, py := sx*velicinaPlocice, sy*velicinaPlocice
	lijevo, gore := px-float64(sirinaKarte)/2, py-float64(visinaKarte)/2

	x0 := int(math.Floor(lijevo / velicinaPlocice))
	y0 := int(math.Floor(gore / velicinaPlocice))
	x1 := int(math.Floor((lijevo + float64(sirinaKarte) - 1) / velicinaPlocice))
	y1 := int(math.Floor((gore + float64(visinaKarte) - 1) / velicinaPlocice))

	platno := image.NewRGBA(image.Rect(0, 0, (x1-x0+1)*velicinaPlocice, (y1-y0+1)*velicinaPlocice))
	klijent := &http.Client{Timeout: 5 * time.Second}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			p := dohvatiPlocicu(ctx, klijent, k.Plocice, z, x, y, odakle)
			if p == nil {
				return nil
			}
			draw.Draw(platno, image.Rect((x-x0)*velicinaPlocice, (y-y0)*velicinaPlocice,
				(x-x0+1)*velicinaPlocice, (y-y0+1)*velicinaPlocice), p, image.Point{}, draw.Src)
		}
	}
	ix := int(lijevo) - x0*velicinaPlocice
	iy := int(gore) - y0*velicinaPlocice
	slika := image.NewRGBA(image.Rect(0, 0, sirinaKarte, visinaKarte))
	draw.Draw(slika, slika.Bounds(), platno, image.Point{ix, iy}, draw.Src)

	uSliku := func(t [2]float64) (int, int) {
		tx, ty := plocicaZa(t[1], t[0], z)
		return int(tx*velicinaPlocice - lijevo), int(ty*velicinaPlocice - gore)
	}
	bijela := image.NewUniform(bijelaBoja)
	crna := image.NewUniform(crnaBoja)
	for _, niz := range nizovi {
		if len(niz) == 1 {
			cx, cy := uSliku(niz[0])
			oznaci(slika, cx, cy)
			continue
		}
		// bijela podloga pa tamna crta: vidi se i na svijetloj i na tamnoj karti
		for _, sloj := range []struct {
			boja   image.Image
			debelo int
		}{{bijela, 3}, {crna, 1}} {
			for i := 0; i+1 < len(niz); i++ {
				ax, ay := uSliku(niz[i])
				bx, by := uSliku(niz[i+1])
				crta(slika, ax, ay, bx, by, sloj.boja, sloj.debelo)
			}
		}
	}

	var b strings.Builder
	if err := png.Encode(&pisac{&b}, slika); err != nil {
		return nil
	}
	return &KartaSlika{PNG: []byte(b.String()), Sirina: slika.Bounds().Dx(),
		Visina: slika.Bounds().Dy(), Zasluge: k.Zasluge}
}

// crta povlači ravnu crtu zadane debljine između dviju točaka
func crta(s *image.RGBA, x0, y0, x1, y1 int, boja image.Image, debelo int) {
	dx, dy := x1-x0, y1-y0
	koraka := int(math.Max(math.Abs(float64(dx)), math.Abs(float64(dy))))
	if koraka == 0 {
		koraka = 1
	}
	for i := 0; i <= koraka; i++ {
		x := x0 + dx*i/koraka
		y := y0 + dy*i/koraka
		for ox := -debelo; ox <= debelo; ox++ {
			for oy := -debelo; oy <= debelo; oy++ {
				tocka(s, x+ox, y+oy, boja)
			}
		}
	}
}
