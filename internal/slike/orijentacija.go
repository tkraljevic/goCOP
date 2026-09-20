package slike

import (
	"encoding/binary"
	"image"
)

// Telefoni fotografiju često spremaju onako kako je senzor vidi, a u EXIF
// zapišu kako je treba okrenuti. Goov dekoder tu oznaku ne čita, pa bi
// uspravna slika u dokumentu legla na bok. Ovdje se pročita samo ta jedna
// oznaka (0x0112) i slika se okrene prije spremanja; smanjena slika više
// nema EXIF, pa je okret trajan i preglednici je vide jednako.

// orijentacija vraća EXIF Orientation (1–8) iz JPEG-a, ili 1 kad je nema
func orijentacija(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) && b[i] == 0xFF {
		oznaka := b[i+1]
		if oznaka == 0xDA || oznaka == 0xD9 { // početak podataka ili kraj
			break
		}
		if oznaka == 0xFF || (oznaka >= 0xD0 && oznaka <= 0xD7) || oznaka == 0x01 {
			i++
			continue
		}
		dulj := int(binary.BigEndian.Uint16(b[i+2:]))
		if dulj < 2 || i+2+dulj > len(b) {
			return 1
		}
		if oznaka == 0xE1 {
			return orijentacijaIzExif(b[i+4 : i+2+dulj])
		}
		i += 2 + dulj
	}
	return 1
}

func orijentacijaIzExif(s []byte) int {
	if len(s) < 14 || string(s[:6]) != "Exif\x00\x00" {
		return 1
	}
	t := s[6:]
	var red binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		red = binary.LittleEndian
	case "MM":
		red = binary.BigEndian
	default:
		return 1
	}
	if red.Uint16(t[2:]) != 42 {
		return 1
	}
	ifd := int(red.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	n := int(red.Uint16(t[ifd:]))
	for k := 0; k < n; k++ {
		p := ifd + 2 + k*12
		if p+12 > len(t) {
			return 1
		}
		if red.Uint16(t[p:]) == 0x0112 && red.Uint16(t[p+2:]) == 3 {
			if o := int(red.Uint16(t[p+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 1
		}
	}
	return 1
}

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
