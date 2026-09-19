// Package slike smanjuje fotografije s terena na mjeru koja stane u PDF i u
// razmjenu među čvorovima: telefon daje 4–8 MB po slici, a dokumentu treba
// 100–300 KB. Izlaz je uvijek JPEG.
package slike

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png" // PNG s telefona i skenera

	"golang.org/x/image/draw"
)

// NajvecaStranica je najdulja stranica smanjene slike u točkama
const NajvecaStranica = 1400

// Kvaliteta JPEG-a smanjene slike
const Kvaliteta = 75

// NajveciUlaz je najveća ulazna datoteka koju primamo (bajtova)
const NajveciUlaz = 20 << 20

// Smanji dekodira JPEG ili PNG, smanji je tako da dulja stranica ne prelazi
// NajvecaStranica i vrati JPEG s dimenzijama; manja slika se samo prekodira
func Smanji(podaci []byte) (jpg []byte, w, h int, err error) {
	if len(podaci) == 0 {
		return nil, 0, 0, errors.New("prazna slika")
	}
	if len(podaci) > NajveciUlaz {
		return nil, 0, 0, errors.New("slika je veća od 20 MB")
	}
	img, _, err := image.Decode(bytes.NewReader(podaci))
	if err != nil {
		return nil, 0, 0, errors.New("slika nije čitljiv JPEG ili PNG")
	}
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, 0, 0, errors.New("slika bez sadržaja")
	}
	if w > NajvecaStranica || h > NajvecaStranica {
		if w >= h {
			h = h * NajvecaStranica / w
			w = NajvecaStranica
		} else {
			w = w * NajvecaStranica / h
			h = NajvecaStranica
		}
		if h == 0 {
			h = 1
		}
		if w == 0 {
			w = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
		img = dst
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: Kvaliteta}); err != nil {
		return nil, 0, 0, err
	}
	return out.Bytes(), w, h, nil
}
