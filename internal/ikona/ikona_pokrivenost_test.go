package ikona

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

func boja(img *image.NRGBA, x, y int) color.NRGBA {
	return img.NRGBAAt(x, y)
}

func TestSlikaVelicinaIProziranRub(t *testing.T) {
	for _, v := range []int{16, 32, 64, 256} {
		img := Slika(v, Radi, false)
		if img.Bounds().Dx() != v || img.Bounds().Dy() != v {
			t.Fatalf("veličina %d: dobio %v", v, img.Bounds())
		}
		// kutovi su izvan valova (valovi idu od x=8 do x=56)
		for _, k := range [][2]int{{0, 0}, {v - 1, 0}, {0, v - 1}, {v - 1, v - 1}} {
			if a := boja(img, k[0], k[1]).A; a != 0 {
				t.Errorf("veličina %d: kut %v nije proziran (A=%d)", v, k, a)
			}
		}
		neprozirnih := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] == 0xff {
				neprozirnih++
			}
		}
		if neprozirnih == 0 {
			t.Errorf("veličina %d: nema nijednog punog piksela", v)
		}
	}
}

func TestSlikaBojePoStanju(t *testing.T) {
	// piksel (32,14) na 64×64 leži točno na sredini prvog vala
	srediste := func(s Stanje, tamno bool) color.NRGBA {
		return boja(Slika(64, s, tamno), 32, 14)
	}
	if c := srediste(Stoji, false); c != siva {
		t.Errorf("zaustavljen: %v, želim sivu %v", c, siva)
	}
	if c := srediste(Greska, false); c != crvena {
		t.Errorf("greška: %v, želim crvenu %v", c, crvena)
	}
	// na sredini vala prijelaz je na pola puta od plave do zelene
	if c, zelim := srediste(Radi, false), mijesaj(plava, zelena, 0.5625); c != zelim {
		t.Errorf("radi: %v, želim %v", c, zelim)
	}
	if srediste(Radi, true) == srediste(Radi, false) {
		t.Error("tamna inačica bi trebala imati drukčiju plavu")
	}
	// zaustavljen i greška se ne mijenjaju na tamnoj traci
	if srediste(Stoji, true) != srediste(Stoji, false) {
		t.Error("siva ne ovisi o tamnoj traci")
	}
}

func TestSlikaNadogradnjaImaNarancastuTocku(t *testing.T) {
	n := Slika(64, Nadogradnja, false)
	if c := boja(n, 51, 51); c != narancasta {
		t.Errorf("središte točke: %v, želim narančastu %v", c, narancasta)
	}
	// obrub točke je bijel: 12 jedinica od središta je između r=11 i r+2,5
	if c := boja(n, 51+12, 51); c.R != 0xff || c.G != 0xff || c.B != 0xff || c.A == 0 {
		t.Errorf("obrub točke: %v, želim bijelu", c)
	}
	r := Slika(64, Radi, false)
	if boja(r, 51, 51) == narancasta {
		t.Error("bez nadogradnje ne smije biti narančaste točke")
	}
	// gornji dio znaka je isti kao kad čvor radi
	if boja(n, 32, 14) != boja(r, 32, 14) {
		t.Error("valovi uz nadogradnju trebaju biti u bojama znaka")
	}
}

func TestSlikaSitnaJeDeblja(t *testing.T) {
	// na 32 px potez je 6,5 umjesto 5, pa udio punih piksela raste
	udio := func(v int) float64 {
		img := Slika(v, Stoji, false)
		n := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] > 0x80 {
				n++
			}
		}
		return float64(n) / float64(v*v)
	}
	if s, k := udio(32), udio(64); s <= k {
		t.Errorf("udio na 32 px %.3f nije veći od udjela na 64 px %.3f", s, k)
	}
}

func TestTockeVala(t *testing.T) {
	tk := tockeVala(14)
	if len(tk) != 2*odsjecaka+1 {
		t.Fatalf("točaka %d, želim %d (spoj se ne ponavlja)", len(tk), 2*odsjecaka+1)
	}
	blizu := func(a, b [2]float64) bool {
		return math.Abs(a[0]-b[0]) < 1e-9 && math.Abs(a[1]-b[1]) < 1e-9
	}
	provjere := map[int][2]float64{
		0:                 {8, 14},
		odsjecaka / 2:     {20, 10}, // vrh brijega: y0-4
		odsjecaka:         {32, 14},
		3 * odsjecaka / 2: {44, 18}, // dno doline: y0+4
		2 * odsjecaka:     {56, 14},
	}
	for i, zelim := range provjere {
		if !blizu(tk[i], zelim) {
			t.Errorf("točka %d: %v, želim %v", i, tk[i], zelim)
		}
	}
}

func TestUdaljenost(t *testing.T) {
	crta := [][2]float64{{0, 0}, {10, 0}}
	slucajevi := []struct {
		x, y, zelim float64
	}{
		{5, 0, 0},   // na crti
		{5, 3, 3},   // iznad sredine
		{-3, 4, 5},  // iza početka: kao od kruga oko kraja
		{13, -4, 5}, // iza kraja
	}
	for _, s := range slucajevi {
		if d := udaljenost(s.x, s.y, crta); math.Abs(d-s.zelim) > 1e-9 {
			t.Errorf("(%v,%v): %v, želim %v", s.x, s.y, d, s.zelim)
		}
	}
	// izlomljena crta: bliži je drugi odsječak
	izlom := [][2]float64{{0, 0}, {10, 0}, {10, 10}}
	if d := udaljenost(12, 5, izlom); math.Abs(d-2) > 1e-9 {
		t.Errorf("izlom: %v, želim 2", d)
	}
}

func TestMijesaj(t *testing.T) {
	a := color.NRGBA{0, 100, 200, 10}
	b := color.NRGBA{200, 0, 100, 20}
	if c := mijesaj(a, b, 0); c != (color.NRGBA{0, 100, 200, 0xff}) {
		t.Errorf("t=0: %v", c)
	}
	if c := mijesaj(a, b, 1); c != (color.NRGBA{200, 0, 100, 0xff}) {
		t.Errorf("t=1: %v", c)
	}
	if c := mijesaj(a, b, 0.5); c != (color.NRGBA{100, 50, 150, 0xff}) {
		t.Errorf("t=0,5: %v", c)
	}
}

func TestPreko(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	c := color.NRGBA{200, 100, 50, 0xff}

	preko(img, 0, 0, c, 0) // ništa na praznom ostaje prazno
	if p := img.NRGBAAt(0, 0); p.A != 0 {
		t.Errorf("pokrivenost 0: %v", p)
	}
	preko(img, 0, 0, c, 1)
	if p := img.NRGBAAt(0, 0); p != c {
		t.Errorf("puna pokrivenost: %v, želim %v", p, c)
	}
	// pola bijelog preko pune boje
	preko(img, 0, 0, color.NRGBA{0xff, 0xff, 0xff, 0xff}, 0.5)
	if p := img.NRGBAAt(0, 0); p != (color.NRGBA{228, 178, 153, 0xff}) {
		t.Errorf("pola preko pune: %v", p)
	}
	// pola na prazno: boja ostaje, prozirnost pola
	preko(img, 1, 0, c, 0.5)
	if p := img.NRGBAAt(1, 0); p != (color.NRGBA{200, 100, 50, 128}) {
		t.Errorf("pola na prazno: %v", p)
	}
}

func TestPNGSeVracaIstiPikseli(t *testing.T) {
	img := Slika(24, Greska, false)
	b := PNG(img)
	if !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("nije PNG")
	}
	d, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if d.Bounds() != img.Bounds() {
		t.Fatalf("granice %v, želim %v", d.Bounds(), img.Bounds())
	}
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			zr, zg, zb, za := img.At(x, y).RGBA()
			dr, dg, db, da := d.At(x, y).RGBA()
			if zr != dr || zg != dg || zb != db || za != da {
				t.Fatalf("piksel (%d,%d) se razlikuje", x, y)
			}
		}
	}
}

type stavkaICO struct {
	Sirina, Visina, Boja, Rezerva uint8
	Ravnine, Bitova               uint16
	Velicina, Pomak               uint32
}

func citajICO(t *testing.T, b []byte) ([3]uint16, []stavkaICO) {
	t.Helper()
	r := bytes.NewReader(b)
	var zag [3]uint16
	if err := binary.Read(r, binary.LittleEndian, &zag); err != nil {
		t.Fatal(err)
	}
	st := make([]stavkaICO, zag[2])
	if err := binary.Read(r, binary.LittleEndian, st); err != nil {
		t.Fatal(err)
	}
	return zag, st
}

func TestICOZaglavljeIStavke(t *testing.T) {
	slike := []*image.NRGBA{Slika(16, Radi, false), Slika(48, Stoji, false), Slika(256, Radi, false)}
	b := ICO(slike)
	zag, st := citajICO(t, b)
	if zag != [3]uint16{0, 1, 3} {
		t.Fatalf("zaglavlje %v", zag)
	}
	pomak := uint32(6 + 16*3)
	for i, s := range st {
		w := slike[i].Bounds().Dx()
		zelim := uint8(w)
		if w == 256 {
			zelim = 0
		}
		if s.Sirina != zelim || s.Visina != zelim {
			t.Errorf("stavka %d: dimenzije %d×%d, želim %d", i, s.Sirina, s.Visina, zelim)
		}
		if s.Ravnine != 1 || s.Bitova != 32 || s.Boja != 0 || s.Rezerva != 0 {
			t.Errorf("stavka %d: %+v", i, s)
		}
		if s.Pomak != pomak {
			t.Errorf("stavka %d: pomak %d, želim %d", i, s.Pomak, pomak)
		}
		d, err := png.Decode(bytes.NewReader(b[s.Pomak : s.Pomak+s.Velicina]))
		if err != nil {
			t.Fatalf("stavka %d: %v", i, err)
		}
		if d.Bounds().Dx() != w {
			t.Errorf("stavka %d: PNG širine %d, želim %d", i, d.Bounds().Dx(), w)
		}
		pomak += s.Velicina
	}
	if int(pomak) != len(b) {
		t.Errorf("datoteka %d bajtova, stavke završavaju na %d", len(b), pomak)
	}
}

func TestICOPrazan(t *testing.T) {
	b := ICO(nil)
	if !bytes.Equal(b, []byte{0, 0, 1, 0, 0, 0}) {
		t.Errorf("prazan .ico: %v", b)
	}
}

func TestIkonaPrograma(t *testing.T) {
	b := IkonaPrograma()
	zag, st := citajICO(t, b)
	if int(zag[2]) != len(VelicineIkone) {
		t.Fatalf("slika %d, želim %d", zag[2], len(VelicineIkone))
	}
	for i, v := range VelicineIkone {
		s := st[i]
		d, err := png.Decode(bytes.NewReader(b[s.Pomak : s.Pomak+s.Velicina]))
		if err != nil {
			t.Fatal(err)
		}
		if d.Bounds().Dx() != v {
			t.Errorf("slika %d: %d px, želim %d", i, d.Bounds().Dx(), v)
		}
	}
}
