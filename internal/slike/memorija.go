package slike

import (
	"bytes"
	"errors"
	"image"
	"image/color"
)

// NajviseMemorije je koliko memorije smije trebati za čitanje jedne slike.
// Koliko memorije treba po točki ovisi o vrsti slike: obična fotografija s
// telefona treba oko 1,5 bajta po točki, pa i 108 milijuna točaka stane,
// dok PNG sa 16 bita po boji ili progresivni JPEG treba i deset puta više.
const NajviseMemorije = 256 << 20

// ErrPrevelikaSlika znači da bi za čitanje slike trebalo više od
// NajviseMemorije
var ErrPrevelikaSlika = errors.New("slika je prevelika (previše točaka); snimite je ili skenirajte manjom razlučivošću")

// istodobnaCitanja je koliko se slika smije čitati u isto vrijeme, da više
// zahtjeva zajedno ne zauzme višestruku granicu
var istodobnaCitanja = make(chan struct{}, 2)

// Zauzmi čeka dok se ne oslobodi mjesto za čitanje slike; vraćena funkcija
// mjesto oslobađa
func Zauzmi() func() {
	istodobnaCitanja <- struct{}{}
	return func() { <-istodobnaCitanja }
}

// Memorija čita iz zaglavlja mjere i vrstu slike, bez čitanja točaka, i
// procjenjuje koliko bajtova memorije treba da se pročita cijela
func Memorija(podaci []byte) (image.Config, string, int64, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(podaci))
	if err != nil {
		return cfg, format, 0, err
	}
	tocaka := int64(cfg.Width) * int64(cfg.Height)
	switch format {
	case "jpeg":
		return cfg, format, okvirJPEGa(podaci).bajtova(tocaka), nil
	case "png":
		return cfg, format, tocaka * pngPoTocki(cfg.ColorModel, isprepletenPNG(podaci)), nil
	}
	return cfg, format, tocaka * 8, nil
}

// Velicina čita iz zaglavlja mjere i vrstu slike i odbija sliku kojoj bi za
// čitanje trebalo više od NajviseMemorije
func Velicina(podaci []byte) (image.Config, string, error) {
	cfg, format, bajtova, err := Memorija(podaci)
	if err == nil && bajtova > NajviseMemorije {
		err = ErrPrevelikaSlika
	}
	return cfg, format, err
}

// pngPoTocki je koliko bajtova po točki zauzme pročitan PNG. Siva slika s
// prozirnom bojom čita se kao slika u boji, pa se siva računa kao u boji.
// Isprepleten PNG čita se u sedam prolaza, uz još jednu sliku u memoriji.
func pngPoTocki(m color.Model, isprepleten bool) int64 {
	n := int64(4)
	switch m {
	case color.Gray16Model, color.RGBA64Model, color.NRGBA64Model:
		n = 8
	}
	if _, ok := m.(color.Palette); ok {
		n = 1
	}
	if isprepleten {
		n *= 2
	}
	return n
}

// isprepletenPNG čita oznaku preplitanja iz zaglavlja IHDR
func isprepletenPNG(p []byte) bool {
	return len(p) > 28 && p[28] == 1
}

// okvirJPEG je ono iz zaglavlja JPEG-a o čemu ovisi memorija: je li
// progresivan i koliko je gusto uzorkovan svaki sloj boje
type okvirJPEG struct {
	progresivan bool
	slojevi     [][2]int // vodoravno i okomito uzorkovanje sloja
}

// najgoriJPEG je procjena kad se okvir ne može pročitati: progresivan, četiri
// pune boje
var najgoriJPEG = okvirJPEG{progresivan: true, slojevi: [][2]int{{1, 1}, {1, 1}, {1, 1}, {1, 1}}}

// bajtova procjenjuje memoriju za čitanje JPEG-a: bajt po uzorku svakog
// sloja, uz četiri bajta po uzorku kod progresivnog (čuva sve koeficijente
// dok ne pročita zadnji prolaz) i pretvorbu u CMYK kod četiri sloja. Sloj
// koji nije uzorkovan rjeđe od prvoga računa se kao pun.
func (o okvirJPEG) bajtova(tocaka int64) int64 {
	h0, v0 := o.slojevi[0][0], o.slojevi[0][1]
	uzoraka := tocaka
	for _, s := range o.slojevi[1:] {
		if s[0] > 0 && s[1] > 0 && h0%s[0] == 0 && v0%s[1] == 0 {
			uzoraka += tocaka * int64(s[0]*s[1]) / int64(h0*v0)
			continue
		}
		uzoraka += tocaka
	}
	b := uzoraka
	if o.progresivan {
		b += 4 * uzoraka
	}
	if len(o.slojevi) == 4 {
		b += 4 * tocaka
	}
	return b
}

// okvirJPEGa traži u JPEG-u okvir (SOF) i čita ga; kad ga ne nađe, vraća
// najgoru procjenu
func okvirJPEGa(p []byte) okvirJPEG {
	for i := 2; i+4 <= len(p); {
		if p[i] != 0xFF {
			return najgoriJPEG
		}
		m := p[i+1]
		switch {
		case m == 0xFF: // bajt za popunu
			i++
			continue
		case m == 0x01 || (m >= 0xD0 && m <= 0xD7): // oznake bez duljine
			i += 2
			continue
		}
		kraj := i + 2 + (int(p[i+2])<<8 | int(p[i+3]))
		if m >= 0xC0 && m <= 0xC2 {
			return citajOkvir(p[i+4:min(kraj, len(p))], m == 0xC2)
		}
		i = kraj
	}
	return najgoriJPEG
}

// citajOkvir čita okvir: preciznost, visinu, širinu, broj slojeva i za
// svaki sloj oznaku, uzorkovanje i tablicu
func citajOkvir(s []byte, progresivan bool) okvirJPEG {
	if len(s) < 6 || s[5] == 0 || len(s) < 6+3*int(s[5]) {
		return najgoriJPEG
	}
	o := okvirJPEG{progresivan: progresivan}
	for k := range int(s[5]) {
		u := s[6+3*k+1]
		o.slojevi = append(o.slojevi, [2]int{int(u >> 4), int(u & 0x0F)})
	}
	if o.slojevi[0][0] == 0 || o.slojevi[0][1] == 0 {
		return najgoriJPEG
	}
	return o
}
