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
	if w != 1400 || h != 1050 {
		t.Errorf("mjere: %dx%d", w, h)
	}
	if len(out) >= velika.Len() || len(out) > 600<<10 {
		t.Errorf("smanjena slika: %d B (ulaz %d B)", len(out), velika.Len())
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || format != "jpeg" || cfg.Width != 1400 {
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
