package web

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Karta položaja letve kao slika, za izvješće. Wordov dokument ne može
// prikazati živu kartu, a koordinate bez podloge ne kažu gdje je letva.
//
// Pločice se slažu iz istog izvora koji koristi i kartica. To znači da
// sastavljanje izvješća s kartom traži mrežu — program inače radi bez nje, pa
// se bez pločica dokument sastavlja bez karte, a ne odbija sastaviti.

const (
	velicinaPlocice = 256
	zoomKarte       = 14  // letva i okolno korito stanu, a pločica treba malo
	sirinaKarte     = 768 // stane preko širine stranice u Wordu
	visinaKarte     = 480
)

// KartaSlika je složena karta s označenim položajem.
type KartaSlika struct {
	PNG     []byte
	Sirina  int
	Visina  int
	Zasluge string
}

// slozKartu skida pločice oko zadanog položaja i slaže ih u jednu sliku.
// Vraća nil bez greške kad karta nije postavljena ili mreža ne odgovara:
// izvješće se tada sastavlja bez nje.
func slozKartu(ctx context.Context, k KartaPostavke, lat, lon float64, odakle string) *KartaSlika {
	if !k.Ima() || k.Plocice == "" {
		return nil
	}
	z := zoomKarte
	if k.NajviseZ > 0 && z > k.NajviseZ {
		z = k.NajviseZ
	}
	// Letva mora pasti u sredinu slike, ne u sredinu neke pločice. Zato se
	// prvo odredi njezin položaj u slikovnim točkama cijelog svijeta, pa se oko
	// njega izreže prozor — pločice služe samo da se taj prozor popuni.
	fx, fy := plocicaZa(lat, lon, z)
	px, py := fx*velicinaPlocice, fy*velicinaPlocice
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
				return nil // nepotpuna karta je gora od nikakve
			}
			draw.Draw(platno, image.Rect((x-x0)*velicinaPlocice, (y-y0)*velicinaPlocice,
				(x-x0+1)*velicinaPlocice, (y-y0+1)*velicinaPlocice), p, image.Point{}, draw.Src)
		}
	}
	// izrez prozora oko letve
	ix := int(lijevo) - x0*velicinaPlocice
	iy := int(gore) - y0*velicinaPlocice
	slika := image.NewRGBA(image.Rect(0, 0, sirinaKarte, visinaKarte))
	draw.Draw(slika, slika.Bounds(), platno, image.Point{ix, iy}, draw.Src)

	// Oznaka položaja: križić s prstenom, dovoljno vidljiv i u crno-bijelom ispisu.
	oznaci(slika, sirinaKarte/2, visinaKarte/2)

	var b strings.Builder
	if err := png.Encode(&pisac{&b}, slika); err != nil {
		return nil
	}
	return &KartaSlika{PNG: []byte(b.String()), Sirina: slika.Bounds().Dx(),
		Visina: slika.Bounds().Dy(), Zasluge: k.Zasluge}
}

type pisac struct{ b *strings.Builder }

func (p *pisac) Write(b []byte) (int, error) { return p.b.Write(b) }

// plocicaZa pretvara zemljopisne koordinate u položaj na mreži pločica.
// Standardni Mercatorov izračun, isti koji koristi i preglednik.
func plocicaZa(lat, lon float64, z int) (float64, float64) {
	n := math.Exp2(float64(z))
	x := (lon + 180) / 360 * n
	rad := lat * math.Pi / 180
	y := (1 - math.Log(math.Tan(rad)+1/math.Cos(rad))/math.Pi) / 2 * n
	return x, y
}

func dohvatiPlocicu(ctx context.Context, k *http.Client, predlozak string, z, x, y int, odakle string) image.Image {
	url := strings.NewReplacer("{z}", fmt.Sprint(z), "{x}", fmt.Sprint(x), "{y}", fmt.Sprint(y)).
		Replace(predlozak)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	// Poslužitelji pločica traže da se klijent predstavi i da kažu s koje
	// stranice zahtjev dolazi — Wikimedijin bez Referera odgovara s 403.
	// Šalje se stranica koja je kartu doista zatražila, ne izmišljena adresa.
	req.Header.Set("User-Agent", "goCOP/1.0 (obrana od poplava; Hrvatske vode)")
	if odakle != "" {
		req.Header.Set("Referer", odakle)
	}
	res, err := k.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	img, err := png.Decode(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil
	}
	return img
}

// oznaci crta prsten s križićem na položaju letve.
func oznaci(s *image.RGBA, cx, cy int) {
	crna := image.NewUniform(crnaBoja)
	bijela := image.NewUniform(bijelaBoja)
	for r := 9; r <= 11; r++ {
		krug(s, cx, cy, r, crna)
	}
	krug(s, cx, cy, 12, bijela)
	krug(s, cx, cy, 8, bijela)
	for d := -6; d <= 6; d++ {
		tocka(s, cx+d, cy, crna)
		tocka(s, cx, cy+d, crna)
	}
}

var (
	crnaBoja   = colorRGBA{20, 20, 20, 255}
	bijelaBoja = colorRGBA{255, 255, 255, 255}
)

type colorRGBA struct{ R, G, B, A uint8 }

func (c colorRGBA) RGBA() (r, g, b, a uint32) {
	return uint32(c.R) * 257, uint32(c.G) * 257, uint32(c.B) * 257, uint32(c.A) * 257
}

func krug(s *image.RGBA, cx, cy, r int, boja image.Image) {
	for kut := 0; kut < 360; kut++ {
		rad := float64(kut) * math.Pi / 180
		tocka(s, cx+int(math.Round(float64(r)*math.Cos(rad))),
			cy+int(math.Round(float64(r)*math.Sin(rad))), boja)
	}
}

func tocka(s *image.RGBA, x, y int, boja image.Image) {
	if !(image.Point{x, y}).In(s.Bounds()) {
		return
	}
	s.Set(x, y, boja.At(0, 0))
}
