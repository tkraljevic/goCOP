package pdfw

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"runtime"
	"testing"

	"gocop/internal/slike"
)

// pngSMjerama vraća PNG od jedne točke kojem zaglavlje kaže zadane mjere,
// sa 16 bita po boji kad je dubok
func pngSMjerama(t *testing.T, w, h uint32, dubok bool) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	p := b.Bytes()
	// zaglavlje IHDR: širina na 16, visina na 20, dubina na 24, vrsta boje
	// na 25, zbroj provjere na 29
	binary.BigEndian.PutUint32(p[16:], w)
	binary.BigEndian.PutUint32(p[20:], h)
	if dubok {
		p[24], p[25] = 16, 6
	}
	binary.BigEndian.PutUint32(p[29:], crc32.ChecksumIEEE(p[12:29]))
	return p
}

// PNG kojem bi za čitanje trebalo previše memorije ne smješta se u PDF i ne
// zauzima memoriju za cijelu sliku; obična slika i dalje se smješta, a sken
// A4 sa 600 dpi prolazi provjeru.
func TestSlikaPNGPrevelika(t *testing.T) {
	d := Novi("Akt", "goCOP")
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	err := d.SlikaPNG(pngSMjerama(t, 7071, 7071, true), d.Lijevo, d.Y, 24, 24)
	runtime.ReadMemStats(&m1)
	if !errors.Is(err, slike.ErrPrevelikaSlika) || len(d.slike) != 0 {
		t.Errorf("velika slika: %v, smješteno %d", err, len(d.slike))
	}
	if z := m1.TotalAlloc - m0.TotalAlloc; z > 8<<20 {
		t.Errorf("odbijanje je zauzelo %d MB", z>>20)
	}
	if _, err := PDFIzSlike(pngSMjerama(t, 50000, 50000, false), "Sken"); !errors.Is(err, slike.ErrPrevelikaSlika) {
		t.Errorf("sken: %v", err)
	}
	// sken A4 sa 600 dpi prolazi provjeru, a ovdje pada tek na čitanju točaka
	if err := d.SlikaPNG(pngSMjerama(t, 4961, 7016, false), d.Lijevo, d.Y, 24, 24); err == nil || errors.Is(err, slike.ErrPrevelikaSlika) {
		t.Errorf("sken A4: %v", err)
	}
	var j bytes.Buffer
	if err := jpeg.Encode(&j, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	for _, los := range [][]byte{[]byte("nije slika"), pngSMjerama(t, 10, 10, false), j.Bytes()} {
		if err := d.SlikaPNG(los, d.Lijevo, d.Y, 24, 24); err == nil || len(d.slike) != 0 {
			t.Errorf("nečitljiva slika smještena: %v", err)
		}
	}
	var mala bytes.Buffer
	if err := png.Encode(&mala, image.NewGray(image.Rect(0, 0, 40, 40))); err != nil {
		t.Fatal(err)
	}
	if err := d.SlikaPNG(mala.Bytes(), d.Lijevo, d.Y, 24, 24); err != nil || len(d.slike) != 1 {
		t.Errorf("obična slika: %v", err)
	}
}
