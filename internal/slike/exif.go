package slike

import (
	"encoding/binary"
	"strings"
	"time"
)

// Podaci su ono što fotoaparat zapiše uz sliku, a dokumentu koristi: kad je
// snimljena, gdje, čime i kako je okrenuta. Sve ostalo (ekspozicija, žarišna
// duljina) za prijavu ne znači ništa i ne čita se.
type Podaci struct {
	Snimljeno time.Time // lokalno vrijeme s uređaja; nula kad nema
	Lat, Lon  float64   // WGS84; nula kad nema
	Uredjaj   string    // "Xiaomi 2112123AG"
	Okret     int       // EXIF Orientation 1–8; 1 kad nema
}

// ImaPolozaj javlja je li uz sliku zapisan položaj
func (p Podaci) ImaPolozaj() bool { return p.Lat != 0 || p.Lon != 0 }

// Procitaj vraća podatke fotoaparata iz JPEG-a; slika bez EXIF-a daje prazne
// podatke, ne grešku
func Procitaj(b []byte) Podaci {
	p := Podaci{Okret: 1}
	seg := exifSegment(b)
	if seg == nil {
		return p
	}
	t := seg[6:] // TIFF zaglavlje
	var red binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		red = binary.LittleEndian
	case "MM":
		red = binary.BigEndian
	default:
		return p
	}
	if len(t) < 8 || red.Uint16(t[2:]) != 42 {
		return p
	}
	var make, model, datum, datumIzvorni string
	var exifIFD, gpsIFD int
	citajIFD(t, red, int(red.Uint32(t[4:])), func(tag, tip uint16, n int, v []byte) {
		switch tag {
		case 0x010F:
			make = ascii(v)
		case 0x0110:
			model = ascii(v)
		case 0x0112:
			if o := int(kratki(red, v)); o >= 1 && o <= 8 {
				p.Okret = o
			}
		case 0x0132:
			datum = ascii(v)
		case 0x8769:
			exifIFD = int(red.Uint32(v))
		case 0x8825:
			gpsIFD = int(red.Uint32(v))
		}
	})
	if exifIFD > 0 {
		citajIFD(t, red, exifIFD, func(tag, tip uint16, n int, v []byte) {
			if tag == 0x9003 {
				datumIzvorni = ascii(v)
			}
		})
	}
	if datumIzvorni == "" {
		datumIzvorni = datum
	}
	if datumIzvorni != "" {
		if k, err := time.ParseInLocation("2006:01:02 15:04:05", datumIzvorni, time.Local); err == nil {
			p.Snimljeno = k
		}
	}
	if gpsIFD > 0 {
		var latRef, lonRef string
		var lat, lon float64
		var imaLat, imaLon bool
		citajIFD(t, red, gpsIFD, func(tag, tip uint16, n int, v []byte) {
			switch tag {
			case 0x0001:
				latRef = ascii(v)
			case 0x0002:
				lat, imaLat = stupnjevi(red, v, n)
			case 0x0003:
				lonRef = ascii(v)
			case 0x0004:
				lon, imaLon = stupnjevi(red, v, n)
			}
		})
		if imaLat && imaLon && (lat != 0 || lon != 0) {
			if strings.HasPrefix(latRef, "S") {
				lat = -lat
			}
			if strings.HasPrefix(lonRef, "W") {
				lon = -lon
			}
			p.Lat, p.Lon = lat, lon
		}
	}
	p.Uredjaj = strings.TrimSpace(make + " " + model)
	if model != "" && strings.HasPrefix(strings.ToLower(model), strings.ToLower(make)) {
		p.Uredjaj = model
	}
	return p
}

// exifSegment nalazi APP1 "Exif" segment u JPEG-u (bez oznake i duljine)
func exifSegment(b []byte) []byte {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil
	}
	i := 2
	for i+4 <= len(b) && b[i] == 0xFF {
		oznaka := b[i+1]
		if oznaka == 0xDA || oznaka == 0xD9 { // početak podataka ili kraj
			return nil
		}
		if oznaka == 0xFF || (oznaka >= 0xD0 && oznaka <= 0xD7) || oznaka == 0x01 {
			i++
			continue
		}
		dulj := int(binary.BigEndian.Uint16(b[i+2:]))
		if dulj < 2 || i+2+dulj > len(b) {
			return nil
		}
		if oznaka == 0xE1 {
			s := b[i+4 : i+2+dulj]
			if len(s) >= 14 && string(s[:6]) == "Exif\x00\x00" {
				return s
			}
		}
		i += 2 + dulj
	}
	return nil
}

// citajIFD prolazi stavke jednog IFD-a i za svaku daje vrijednost: kratke
// stoje u samoj stavci, dulje na pomaku u TIFF-u
func citajIFD(t []byte, red binary.ByteOrder, ifd int, f func(tag, tip uint16, n int, v []byte)) {
	if ifd < 8 || ifd+2 > len(t) {
		return
	}
	n := int(red.Uint16(t[ifd:]))
	for k := 0; k < n; k++ {
		p := ifd + 2 + k*12
		if p+12 > len(t) {
			return
		}
		tag, tip := red.Uint16(t[p:]), red.Uint16(t[p+2:])
		broj := int(red.Uint32(t[p+4:]))
		vel := map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}[tip]
		if vel == 0 || broj < 0 || broj > 1<<16 {
			continue
		}
		dulj := vel * broj
		v := t[p+8 : p+12]
		if dulj > 4 {
			pom := int(red.Uint32(t[p+8:]))
			if pom < 0 || pom+dulj > len(t) {
				continue
			}
			v = t[pom : pom+dulj]
		} else {
			v = v[:dulj]
		}
		f(tag, tip, broj, v)
	}
}

func ascii(v []byte) string {
	return strings.TrimSpace(strings.TrimRight(string(v), "\x00"))
}

func kratki(red binary.ByteOrder, v []byte) uint16 {
	if len(v) < 2 {
		return 0
	}
	return red.Uint16(v)
}

// stupnjevi zbraja tri racionalna broja (stupnjevi, minute, sekunde)
func stupnjevi(red binary.ByteOrder, v []byte, n int) (float64, bool) {
	if n < 3 || len(v) < 24 {
		return 0, false
	}
	var d float64
	for i, f := range []float64{1, 60, 3600} {
		br := float64(red.Uint32(v[i*8:]))
		naz := float64(red.Uint32(v[i*8+4:]))
		if naz == 0 {
			return 0, false
		}
		d += br / naz / f
	}
	return d, true
}
