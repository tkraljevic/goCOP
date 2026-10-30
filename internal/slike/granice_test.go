package slike

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"runtime"
	"testing"
	"time"
)

// pngSMjerama vraća PNG od jedne točke kojem zaglavlje kaže zadane mjere
func pngSMjerama(t *testing.T, w, h uint32) []byte {
	t.Helper()
	return pngZaglavlje(t, w, h, 8, 0, 0)
}

// pngZaglavlje vraća PNG od jedne točke kojem zaglavlje kaže zadane mjere,
// dubinu boje, vrstu boje i preplitanje
func pngZaglavlje(t *testing.T, w, h uint32, dubina, vrsta, preplitanje byte) []byte {
	t.Helper()
	var izvor image.Image = image.NewGray(image.Rect(0, 0, 1, 1))
	if vrsta == 3 {
		// slika s paletom treba paletu ispred podataka
		izvor = image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, izvor); err != nil {
		t.Fatal(err)
	}
	p := b.Bytes()
	// zaglavlje IHDR: duljina na 8, vrsta na 12, širina na 16, visina na 20,
	// dubina na 24, vrsta boje na 25, preplitanje na 28, zbroj provjere na 29
	binary.BigEndian.PutUint32(p[16:], w)
	binary.BigEndian.PutUint32(p[20:], h)
	p[24], p[25], p[28] = dubina, vrsta, preplitanje
	binary.BigEndian.PutUint32(p[29:], crc32.ChecksumIEEE(p[12:29]))
	return p
}

// jpegZaglavlje vraća JPEG u boji kojem okvir kaže zadane mjere; progresivan
// ima oznaku SOF2, a pun ima sve boje uzorkovane kao svjetlinu (4:4:4)
func jpegZaglavlje(t *testing.T, w, h uint16, progresivan, pun bool) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil); err != nil {
		t.Fatal(err)
	}
	p := b.Bytes()
	i := bytes.Index(p, []byte{0xFF, 0xC0})
	if i < 0 || p[i+9] != 3 {
		t.Fatal("JPEG bez okvira u boji")
	}
	if progresivan {
		p[i+1] = 0xC2
	}
	binary.BigEndian.PutUint16(p[i+5:], h)
	binary.BigEndian.PutUint16(p[i+7:], w)
	if pun {
		p[i+11] = 0x11
	}
	return p
}

// Koliko memorije treba ovisi o vrsti slike: obična fotografija s telefona
// od 50 ili 108 milijuna točaka prolazi, a jednako velik progresivni JPEG ili
// PNG sa 16 bita po boji ne.
func TestVelicinaPoVrstiSlike(t *testing.T) {
	for _, s := range []struct {
		opis    string
		slika   []byte
		bajtova int64
		prevel  bool
	}{
		{"telefon 50 MP", jpegZaglavlje(t, 8192, 6144, false, false), 8192 * 6144 * 3 / 2, false},
		{"telefon 108 MP", jpegZaglavlje(t, 12000, 9000, false, false), 12000 * 9000 * 3 / 2, false},
		{"progresivni 4:4:4", jpegZaglavlje(t, 7071, 7071, true, true), 7071 * 7071 * 15, true},
		{"sivi PNG 50 MP", pngZaglavlje(t, 8192, 6144, 8, 0, 0), 8192 * 6144 * 4, false},
		{"PNG s paletom", pngZaglavlje(t, 15000, 15000, 8, 3, 0), 15000 * 15000, false},
		{"PNG 16 bita isprepleten", pngZaglavlje(t, 7071, 7071, 16, 6, 1), 7071 * 7071 * 16, true},
		{"PNG 50000×50000", pngSMjerama(t, 50000, 50000), 50000 * 50000 * 4, true},
	} {
		_, _, bajtova, err := Memorija(s.slika)
		if err != nil || bajtova != s.bajtova {
			t.Errorf("%s: %d bajtova, %v; želi %d", s.opis, bajtova, err, s.bajtova)
		}
		if _, _, err := Velicina(s.slika); errors.Is(err, ErrPrevelikaSlika) != s.prevel || (err != nil && !s.prevel) {
			t.Errorf("%s: %v", s.opis, err)
		}
	}
	if _, _, err := Velicina([]byte("nije slika")); err == nil || errors.Is(err, ErrPrevelikaSlika) {
		t.Errorf("smeće: %v", err)
	}
}

// Slika kojoj bi za čitanje trebalo previše memorije odbija se prije
// čitanja, bez velikog zauzimanja memorije; obična slika i dalje se smanjuje.
func TestPrevelikaSlikaSeOdbija(t *testing.T) {
	for _, velika := range [][]byte{pngSMjerama(t, 50000, 50000), jpegZaglavlje(t, 7071, 7071, true, true), pngZaglavlje(t, 7071, 7071, 16, 6, 1)} {
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		_, _, _, err := Smanji(velika)
		runtime.ReadMemStats(&m1)
		if !errors.Is(err, ErrPrevelikaSlika) {
			t.Errorf("Smanji: %v", err)
		}
		if z := m1.TotalAlloc - m0.TotalAlloc; z > 8<<20 {
			t.Errorf("odbijanje je zauzelo %d MB", z>>20)
		}
	}

	var mala bytes.Buffer
	if err := png.Encode(&mala, image.NewGray(image.Rect(0, 0, 1200, 300))); err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := Velicina(mala.Bytes()); err != nil || cfg.Width != 1200 {
		t.Errorf("obična slika: %d %v", cfg.Width, err)
	}
	if _, w, h, err := Smanji(mala.Bytes()); err != nil || w != 1000 || h != 250 {
		t.Errorf("obična slika: %d×%d %v", w, h, err)
	}
}

// Prazna i veća od 20 MB datoteka odbijaju se prije čitanja zaglavlja.
func TestUlazSmanjenja(t *testing.T) {
	for opis, x := range map[string]struct {
		podaci []byte
		greska string
	}{
		"prazna":        {nil, "prazna slika"},
		"veća od 20 MB": {make([]byte, NajveciUlaz+1), "slika je veća od 20 MB"},
		"nije slika":    {[]byte("nije slika"), "slika nije čitljiv JPEG ili PNG"},
	} {
		if err := provjeriUlaz(x.podaci); err == nil || err.Error() != x.greska {
			t.Errorf("%s: %v", opis, err)
		}
	}
}

// Okvir JPEG-a čita se i iza bajtova za popunu i oznaka bez duljine; kad se
// ne može pročitati, računa se najgora vrsta.
func TestOkvirJPEGa(t *testing.T) {
	okvir := []byte{0xFF, 0xD8, 0xFF, 0xFF, 0xD0, 0xFF, 0xC2, 0x00, 0x0B, 8, 0, 16, 0, 16, 1, 1, 0x11, 0}
	if o := okvirJPEGa(okvir); !o.progresivan || len(o.slojevi) != 1 || o.bajtova(100) != 500 {
		t.Errorf("sivi progresivni: %+v", o)
	}
	for _, los := range [][]byte{
		{0xFF, 0xD8, 0x00, 0x00, 0x00, 0x00},
		{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x05, 8, 0},
		{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x0B, 8, 0, 16, 0, 16, 1, 1, 0x00, 0},
		{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x02},
	} {
		if o := okvirJPEGa(los); o.progresivan != najgoriJPEG.progresivan || len(o.slojevi) != 4 {
			t.Errorf("% x: %+v", los, o)
		}
	}
	// sloj gušći od prvoga i CMYK računaju se puni
	if b := (okvirJPEG{slojevi: [][2]int{{1, 1}, {2, 2}, {1, 1}}}).bajtova(100); b != 300 {
		t.Errorf("gušći sloj: %d", b)
	}
	if b := najgoriJPEG.bajtova(100); b != 100*4*5+400 {
		t.Errorf("CMYK: %d", b)
	}
}

// probaFormat je vrsta slike koju samo test zna pročitati
func probaFormat(io.Reader) (image.Config, error) {
	return image.Config{ColorModel: color.RGBAModel, Width: 10, Height: 10}, nil
}

// Druga vrsta slike računa se s osam bajtova po točki.
func TestMemorijaDrugeVrste(t *testing.T) {
	image.RegisterFormat("proba", "PROBA", nil, probaFormat)
	if _, format, bajtova, err := Memorija([]byte("PROBA")); err != nil || format != "proba" || bajtova != 800 {
		t.Errorf("%s %d %v", format, bajtova, err)
	}
}

// Kad su sva mjesta za čitanje zauzeta, Smanji čeka da se jedno oslobodi.
func TestCitanjeSlikeCekaMjesto(t *testing.T) {
	var mala bytes.Buffer
	if err := png.Encode(&mala, image.NewGray(image.Rect(0, 0, 20, 20))); err != nil {
		t.Fatal(err)
	}
	prvo, drugo := Zauzmi(), Zauzmi()
	gotovo := make(chan error, 1)
	go func() {
		_, _, _, err := Smanji(mala.Bytes())
		gotovo <- err
	}()
	select {
	case <-gotovo:
		t.Fatal("slika je pročitana dok su sva mjesta bila zauzeta")
	case <-time.After(50 * time.Millisecond):
	}
	prvo()
	if err := <-gotovo; err != nil {
		t.Fatal(err)
	}
	drugo()
}

// Dulja stranica postaje 1000 točaka, a kraća barem jedna.
func TestSmanjeneMjere(t *testing.T) {
	for _, s := range []struct{ w, h, sw, sh int }{
		{4000, 3000, 1000, 750}, {3000, 4000, 750, 1000}, {20000, 5, 1000, 1}, {5, 20000, 1, 1000},
	} {
		if w, h := smanjeneMjere(s.w, s.h); w != s.sw || h != s.sh {
			t.Errorf("%d×%d → %d×%d", s.w, s.h, w, h)
		}
	}
}
