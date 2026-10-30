// Package slike smanjuje fotografije s terena na mjeru koja stane u PDF i u
// razmjenu među čvorovima: telefon daje 4–8 MB po slici, a dokumentu treba
// 100–250 KB. Na A4 slika stoji pola stranice široko, pa je 1 000 px oko
// 300 dpi; travnati teren se loše sažima, pa mjera vrijedi. Izlaz je JPEG.
package slike

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // PNG s telefona i skenera

	"golang.org/x/image/draw"
)

// NajvecaStranica je najdulja stranica smanjene slike u točkama
const NajvecaStranica = 1000

// Kvaliteta JPEG-a smanjene slike
const Kvaliteta = 70

// NajveciUlaz je najveća ulazna datoteka koju primamo (bajtova)
const NajveciUlaz = 20 << 20

// Smanji dekodira JPEG ili PNG, smanji je tako da dulja stranica ne prelazi
// NajvecaStranica i vrati JPEG s dimenzijama; manja slika se samo prekodira.
// Slika kojoj bi za čitanje i smanjenje trebalo više od NajviseMemorije
// odbija se prije čitanja.
func Smanji(podaci []byte) (jpg []byte, w, h int, err error) {
	if err := provjeriUlaz(podaci); err != nil {
		return nil, 0, 0, err
	}
	// mjesto se drži dok se pročitana slika ne smanji
	oslobodi := Zauzmi()
	defer oslobodi()
	img, _, err := image.Decode(bytes.NewReader(podaci))
	if err != nil {
		return nil, 0, 0, errNecitljiva
	}
	o := orijentacija(podaci)
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, 0, 0, errors.New("slika bez sadržaja")
	}
	if w > NajvecaStranica || h > NajvecaStranica {
		img, w, h = umanji(img)
	}
	// okret ide nakon smanjenja, da se ne premještaju milijuni točaka
	img = okreni(img, o)
	if o >= 5 {
		w, h = h, w
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: Kvaliteta}); err != nil {
		return nil, 0, 0, err
	}
	return out.Bytes(), w, h, nil
}

var errNecitljiva = errors.New("slika nije čitljiv JPEG ili PNG")

// provjeriUlaz odbija praznu, preveliku i nečitljivu datoteku, i sliku kojoj
// bi za čitanje i smanjenje trebalo više od NajviseMemorije
func provjeriUlaz(podaci []byte) error {
	if len(podaci) == 0 {
		return errors.New("prazna slika")
	}
	if len(podaci) > NajveciUlaz {
		return errors.New("slika je veća od 20 MB")
	}
	cfg, _, bajtova, err := Memorija(podaci)
	if err != nil {
		return errNecitljiva
	}
	if bajtova+troskoSmanjenja(cfg.Width, cfg.Height) > NajviseMemorije {
		return ErrPrevelikaSlika
	}
	return nil
}

// umanji smanjuje sliku na NajvecaStranica po duljoj stranici. Velika slika
// prvo se svede prosjekom susjednih točaka, jer glatko smanjenje treba 32
// bajta po točki širine izlaza i visine ulaza.
func umanji(img image.Image) (image.Image, int, int) {
	b := img.Bounds()
	w, h := smanjeneMjere(b.Dx(), b.Dy())
	src := predSmanji(img, faktorPredSmanjenja(b.Dx(), b.Dy()))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst, w, h
}

// faktorPredSmanjenja je koliko se puta slika smanji prosjekom, tako da
// ostane barem dvostruko veća od smanjene
func faktorPredSmanjenja(w, h int) int {
	sw, sh := smanjeneMjere(w, h)
	return max(min(w/(2*sw), h/(2*sh)), 1)
}

// troskoSmanjenja je koliko bajtova uz pročitanu sliku treba za smanjenje:
// slika svedena prosjekom, međuspremnik glatkog smanjenja i izlaz
func troskoSmanjenja(w, h int) int64 {
	if w <= NajvecaStranica && h <= NajvecaStranica {
		return 0
	}
	sw, sh := smanjeneMjere(w, h)
	k := faktorPredSmanjenja(w, h)
	pw, ph := int64(w/k), int64(h/k)
	b := int64(sw)*int64(sh)*4 + int64(sw)*ph*32
	if k > 1 {
		b += pw * ph * 4
	}
	return b
}

// predSmanji svodi sliku k puta prosjekom svakog bloka k×k točaka; za k
// manji od 2 vraća sliku kakva jest
func predSmanji(img image.Image, k int) image.Image {
	src, ok := img.(image.RGBA64Image)
	if k < 2 || !ok {
		return img
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()/k, b.Dy()/k))
	for y := range out.Rect.Dy() {
		for x := range out.Rect.Dx() {
			out.SetRGBA64(x, y, prosjekBloka(src, b.Min.X+x*k, b.Min.Y+y*k, k))
		}
	}
	return out
}

// prosjekBloka je prosječna boja bloka k×k točaka s gornjim lijevim kutom
// u (x0, y0)
func prosjekBloka(src image.RGBA64Image, x0, y0, k int) color.RGBA64 {
	var r, g, b, a uint64
	for y := y0; y < y0+k; y++ {
		for x := x0; x < x0+k; x++ {
			c := src.RGBA64At(x, y)
			r, g, b, a = r+uint64(c.R), g+uint64(c.G), b+uint64(c.B), a+uint64(c.A)
		}
	}
	n := uint64(k * k)
	return color.RGBA64{R: uint16(r / n), G: uint16(g / n), B: uint16(b / n), A: uint16(a / n)}
}

// smanjeneMjere svodi mjere slike tako da dulja stranica bude
// NajvecaStranica, uz očuvan omjer i najmanje jednu točku po stranici
func smanjeneMjere(w, h int) (int, int) {
	if w >= h {
		return NajvecaStranica, max(h*NajvecaStranica/w, 1)
	}
	return max(w*NajvecaStranica/h, 1), NajvecaStranica
}
