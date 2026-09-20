package slike

import "image"

// Telefoni fotografiju često spremaju onako kako je senzor vidi, a u EXIF
// zapišu kako je treba okrenuti. Goov dekoder tu oznaku ne čita, pa bi
// uspravna slika u dokumentu legla na bok. Ovdje se pročita samo ta jedna
// oznaka (0x0112) i slika se okrene prije spremanja; smanjena slika više
// nema EXIF, pa je okret trajan i preglednici je vide jednako.

// orijentacija vraća EXIF Orientation (1–8) iz JPEG-a, ili 1 kad je nema
func orijentacija(b []byte) int { return Procitaj(b).Okret }

// okreni vraća sliku postavljenu uspravno prema EXIF orijentaciji
func okreni(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2: // zrcalo vodoravno
				nx, ny = w-1-x, y
			case 3: // 180°
				nx, ny = w-1-x, h-1-y
			case 4: // zrcalo okomito
				nx, ny = x, h-1-y
			case 5: // zrcalo + 90°
				nx, ny = y, x
			case 6: // 90° u smjeru kazaljke
				nx, ny = h-1-y, x
			case 7: // zrcalo + 270°
				nx, ny = h-1-y, w-1-x
			case 8: // 270° u smjeru kazaljke
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
