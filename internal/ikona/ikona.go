// Package ikona crta znak goCOP-a, četiri vala iz web/static/img/hv-mark.svg,
// za ikonu programa (.ico) i za ikonu u traci po stanju čvora.
//
// Crta se u čistom Go-u, bez vanjskog alata: svaki val su dvije kvadratne
// Bézierove krivulje (M8 y Q20 y-8 32 y T56 y) debljine 5 na platnu 64×64
// sa zaobljenim krajevima. Rub se izglađuje po udaljenosti od krivulje, pa
// znak ostaje čist i na 16 px. Kao u SVG-u, svaki val ima svoj prijelaz od
// plave na vrhu do zelene na dnu (gradientUnits objectBoundingBox).
package ikona

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Stanje kaže kako je ikona obojena
type Stanje int

const (
	Radi        Stanje = iota // boje znaka: čvor radi (i ikona programa)
	Stoji                     // sivo: čvor je zaustavljen
	Nadogradnja               // boje znaka i narančasta točka: ima novije izdanje
	Greska                    // crveno: čvor je pao ili ne odgovara
)

var (
	plava        = color.NRGBA{0x17, 0x3e, 0x74, 0xff} // #173e74, boja Hrvatskih voda
	zelena       = color.NRGBA{0x20, 0xba, 0x70, 0xff} // #20ba70
	plavaTamna   = color.NRGBA{0x5b, 0x9b, 0xe0, 0xff} // na tamnoj traci #173e74 se ne vidi
	siva         = color.NRGBA{0x8b, 0x93, 0x9c, 0xff}
	crvena       = color.NRGBA{0xd9, 0x36, 0x36, 0xff}
	narancasta   = color.NRGBA{0xf0, 0x8c, 0x00, 0xff}
	debljina     = 5.0
	valovi       = []float64{14, 26, 38, 50}
	odsjecaka    = 24 // ravnih odsječaka po polovici vala
	poluvisina   = 4.0
	sitnaGranica = 32 // do ove veličine potez je deblji, da se valovi ne stope s pozadinom
)

// Slika crta znak u kvadratu velicina×velicina. Tamno je inačica za tamnu
// traku (Windows), sa svjetlijom plavom.
func Slika(velicina int, s Stanje, tamno bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, velicina, velicina))
	mj := float64(velicina) / 64 // platno SVG-a je 64×64
	pot := debljina
	if velicina <= sitnaGranica {
		pot = 6.5
	}
	gornja := plava
	if tamno {
		gornja = plavaTamna
	}
	for _, y0 := range valovi {
		tocke := tockeVala(y0)
		for py := 0; py < velicina; py++ {
			y := (float64(py) + 0.5) / mj
			if y < y0-poluvisina-pot || y > y0+poluvisina+pot {
				continue
			}
			for px := 0; px < velicina; px++ {
				x := (float64(px) + 0.5) / mj
				d := udaljenost(x, y, tocke)
				// pokrivenost po udaljenosti, u pikselima ciljne slike
				pokriveno := (pot/2-d)*mj + 0.5
				if pokriveno <= 0 {
					continue
				}
				if pokriveno > 1 {
					pokriveno = 1
				}
				var c color.NRGBA
				switch s {
				case Stoji:
					c = siva
				case Greska:
					c = crvena
				default:
					t := (y - (y0 - poluvisina)) / (2 * poluvisina)
					c = mijesaj(gornja, zelena, math.Max(0, math.Min(1, t)))
				}
				preko(img, px, py, c, pokriveno)
			}
		}
	}
	if s == Nadogradnja {
		tocka(img, 51*mj, 51*mj, 11*mj, narancasta, mj)
	}
	return img
}

// tockeVala su vrhovi izlomljene crte koja prati val na visini y0
func tockeVala(y0 float64) [][2]float64 {
	var out [][2]float64
	kvad := func(p0, c, p1 [2]float64, prva bool) {
		for i := 0; i <= odsjecaka; i++ {
			if i == 0 && !prva {
				continue
			}
			t := float64(i) / float64(odsjecaka)
			u := 1 - t
			out = append(out, [2]float64{
				u*u*p0[0] + 2*u*t*c[0] + t*t*p1[0],
				u*u*p0[1] + 2*u*t*c[1] + t*t*p1[1],
			})
		}
	}
	// M8 y0 Q20 y0-8 32 y0 T56 y0: T zrcali kontrolnu točku oko (32, y0)
	kvad([2]float64{8, y0}, [2]float64{20, y0 - 8}, [2]float64{32, y0}, true)
	kvad([2]float64{32, y0}, [2]float64{44, y0 + 8}, [2]float64{56, y0}, false)
	return out
}

// udaljenost je najmanja udaljenost točke od izlomljene crte; krajevi su
// zaobljeni jer se udaljenost od krajnje točke računa kao od kruga
func udaljenost(x, y float64, tocke [][2]float64) float64 {
	naj := math.MaxFloat64
	for i := 1; i < len(tocke); i++ {
		ax, ay := tocke[i-1][0], tocke[i-1][1]
		bx, by := tocke[i][0], tocke[i][1]
		dx, dy := bx-ax, by-ay
		t := ((x-ax)*dx + (y-ay)*dy) / (dx*dx + dy*dy)
		t = math.Max(0, math.Min(1, t))
		ex, ey := ax+t*dx-x, ay+t*dy-y
		if d := ex*ex + ey*ey; d < naj {
			naj = d
		}
	}
	return math.Sqrt(naj)
}

// tocka crta pun krug s bijelim obrubom (da se odvoji od valova)
func tocka(img *image.NRGBA, cx, cy, r float64, c color.NRGBA, mj float64) {
	obrub := 2.5 * mj
	b := img.Bounds()
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			d := math.Hypot(float64(px)+0.5-cx, float64(py)+0.5-cy)
			if a := r + obrub - d + 0.5; a > 0 {
				preko(img, px, py, color.NRGBA{0xff, 0xff, 0xff, 0xff}, math.Min(1, a))
			}
			if a := r - d + 0.5; a > 0 {
				preko(img, px, py, c, math.Min(1, a))
			}
		}
	}
}

func mijesaj(a, b color.NRGBA, t float64) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 0xff}
}

// preko slaže boju s pokrivenošću a preko onoga što je već nacrtano
func preko(img *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	i := img.PixOffset(x, y)
	p := img.Pix[i : i+4 : i+4]
	da := float64(p[3]) / 255
	oa := a + da*(1-a)
	if oa <= 0 {
		return
	}
	for k, v := range []uint8{c.R, c.G, c.B} {
		p[k] = uint8(math.Round((float64(v)*a + float64(p[k])*da*(1-a)) / oa))
	}
	p[3] = uint8(math.Round(oa * 255))
}

// PNG kodira sliku; greška bi značila pokvaren image/png, pa vraća prazno
func PNG(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil
	}
	return b.Bytes()
}

// ICO slaže .ico s PNG zapisom svake veličine (Windows Vista i noviji)
func ICO(slike []*image.NRGBA) []byte {
	var podaci [][]byte
	for _, s := range slike {
		podaci = append(podaci, PNG(s))
	}
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(len(slike))})
	pomak := 6 + 16*len(slike)
	for i, s := range slike {
		w := s.Bounds().Dx()
		dim := byte(w)
		if w >= 256 {
			dim = 0 // 0 znači 256
		}
		b.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&b, binary.LittleEndian, [2]uint16{1, 32})
		_ = binary.Write(&b, binary.LittleEndian, [2]uint32{uint32(len(podaci[i])), uint32(pomak)})
		pomak += len(podaci[i])
	}
	for _, p := range podaci {
		b.Write(p)
	}
	return b.Bytes()
}

// VelicineIkone su veličine u .ico programa
var VelicineIkone = []int{16, 20, 24, 32, 40, 48, 64, 256}

// IkonaPrograma je .ico za program i instalacijski program
func IkonaPrograma() []byte {
	var slike []*image.NRGBA
	for _, v := range VelicineIkone {
		slike = append(slike, Slika(v, Radi, false))
	}
	return ICO(slike)
}
