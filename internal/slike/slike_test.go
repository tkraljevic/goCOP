package slike

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// Velika fotografija se smanji na 1600 px po duljoj stranici i postane JPEG
// od stotinjak KB; mala se samo prekodira; smeće se odbija.
func TestSmanjiFotografiju(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	for y := 0; y < 3000; y += 7 {
		for x := 0; x < 4000; x += 5 {
			img.Set(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 90, 255})
		}
	}
	var velika bytes.Buffer
	_ = jpeg.Encode(&velika, img, &jpeg.Options{Quality: 95})
	out, w, h, err := Smanji(velika.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if w != 1000 || h != 750 {
		t.Errorf("mjere: %dx%d", w, h)
	}
	if len(out) >= velika.Len() || len(out) > 600<<10 {
		t.Errorf("smanjena slika: %d B (ulaz %d B)", len(out), velika.Len())
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || format != "jpeg" || cfg.Width != 1000 {
		t.Errorf("izlaz: %s %dx%d %v", format, cfg.Width, cfg.Height, err)
	}
	// uspravna i mala PNG slika
	var mala bytes.Buffer
	_ = png.Encode(&mala, image.NewRGBA(image.Rect(0, 0, 300, 500)))
	if _, w, h, err := Smanji(mala.Bytes()); err != nil || w != 300 || h != 500 {
		t.Errorf("mala: %dx%d %v", w, h, err)
	}
	if _, _, _, err := Smanji([]byte("nije slika")); err == nil {
		t.Error("smeće prošlo kao slika")
	}
}

// sExif umeće APP1 s jedinom EXIF oznakom Orientation u gotov JPEG
func sExif(t *testing.T, jpg []byte, o uint16) []byte {
	t.Helper()
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1,
		0x01, 0x12, 0, 3, 0, 0, 0, 1, byte(o >> 8), byte(o), 0, 0, 0, 0, 0, 0}
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := append([]byte{0xFF, 0xE1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}, app1...)
	out := append([]byte{0xFF, 0xD8}, seg...)
	return append(out, jpg[2:]...)
}

// Telefon sliku spremi položeno s uputom da se okrene: izlaz mora biti
// uspravan, a boje na pravim mjestima.
func TestSmanjiOkreceIzExifa(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			c := color.RGBA{255, 255, 255, 255}
			if x < 20 {
				c = color.RGBA{200, 0, 0, 255} // lijeva polovica crvena
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 95})
	if o := orijentacija(b.Bytes()); o != 1 {
		t.Fatalf("bez EXIF-a: %d", o)
	}
	ulaz := sExif(t, b.Bytes(), 6)
	if o := orijentacija(ulaz); o != 6 {
		t.Fatalf("orijentacija: %d", o)
	}
	out, w, h, err := Smanji(ulaz)
	if err != nil {
		t.Fatal(err)
	}
	if w != 20 || h != 40 {
		t.Fatalf("mjere nakon okreta: %dx%d", w, h)
	}
	dek, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	// 90° u smjeru kazaljke: crvena lijeva polovica dolazi gore
	_, gGore, _, _ := dek.At(10, 5).RGBA()
	_, gDolje, _, _ := dek.At(10, 35).RGBA()
	if gGore>>8 > 80 || gDolje>>8 < 200 {
		t.Errorf("zelena gore %d (crveno polje), dolje %d (bijelo polje)", gGore>>8, gDolje>>8)
	}
	if o := orijentacija(out); o != 1 {
		t.Errorf("izlaz još nosi orijentaciju %d", o)
	}
	// okret za 180° ne mijenja mjere
	if _, w, h, err := Smanji(sExif(t, b.Bytes(), 3)); err != nil || w != 40 || h != 20 {
		t.Errorf("180°: %dx%d %v", w, h, err)
	}
}
