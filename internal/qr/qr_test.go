package qr

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// slikaKoda crta kod kao PNG s tihom zonom, za dekoder
func slikaKoda(t *testing.T, k *Kod, put string) {
	t.Helper()
	const modul, rub = 8, 4
	n := k.Velicina + 2*rub
	img := image.NewGray(image.Rect(0, 0, n*modul, n*modul))
	for y := 0; y < n*modul; y++ {
		for x := 0; x < n*modul; x++ {
			img.SetGray(x, y, color.Gray{255})
		}
	}
	for y := 0; y < k.Velicina; y++ {
		for x := 0; x < k.Velicina; x++ {
			if !k.Moduli[y][x] {
				continue
			}
			for dy := 0; dy < modul; dy++ {
				for dx := 0; dx < modul; dx++ {
					img.SetGray((x+rub)*modul+dx, (y+rub)*modul+dy, color.Gray{0})
				}
			}
		}
	}
	f, err := os.Create(put)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// dekoder je Python s OpenCV-om iz varijable GOCOP_QR_PYTHON. Bez nje se
// provjerava samo oblik koda. Kad je zadana, a OpenCV se ne može učitati,
// to je pogreška okruženja koju test javlja, ne prešućuje.
func dekoder(t *testing.T) string {
	t.Helper()
	py := os.Getenv("GOCOP_QR_PYTHON")
	if py == "" {
		return ""
	}
	if out, err := exec.Command(py, "-c", "import cv2").CombinedOutput(); err != nil {
		t.Fatalf("GOCOP_QR_PYTHON=%s ne učitava OpenCV: %v\n%s", py, err, out)
	}
	return py
}

// Kodovi raznih duljina moraju biti pravilne veličine i, kad je dekoder
// dostupan, dekodirati se natrag u isti tekst.
func TestKodiranjeIDekodiranje(t *testing.T) {
	py := dekoder(t)
	if py == "" {
		t.Log("GOCOP_QR_PYTHON nije zadan; provjerava se samo oblik koda")
	}
	for i, tekst := range []string{
		"gocop",
		"HELLO WORLD 123",
		"gocop://prijave/019945a1-7b1e-7c8a-9b0e-0f1e2d3c4b5a?kod=6CD63C1E5D",
		"https://gocop.voda.hr/prijave/019945a1-7b1e-7c8a-9b0e-0f1e2d3c4b5a?kod=6CD63C1E5D&v=2",
		strings.Repeat("ABCDEFGHIJ", 10) + "KLMNOP",
	} {
		k, err := Kodiraj(tekst)
		if err != nil {
			t.Fatalf("%q: %v", tekst, err)
		}
		if (k.Velicina-17)%4 != 0 || k.Velicina < 21 || k.Velicina > 41 {
			t.Errorf("%q: veličina %d", tekst, k.Velicina)
		}
		// tražila u kutovima
		if !k.Moduli[0][0] || !k.Moduli[0][k.Velicina-1] || !k.Moduli[k.Velicina-1][0] || !k.Moduli[3][3] {
			t.Errorf("%q: nema tražila", tekst)
		}
		if py == "" {
			continue
		}
		put := filepath.Join(t.TempDir(), "kod.png")
		slikaKoda(t, k, put)
		out, err := exec.Command(py, "-c", "import cv2,sys; d=cv2.QRCodeDetector(); s,_,_=d.detectAndDecode(cv2.imread(sys.argv[1])); print(s)", put).Output()
		if err != nil {
			t.Fatalf("dekoder: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != tekst {
			t.Errorf("kod %d: dekodirano %q, očekivano %q", i, got, tekst)
		}
	}
	if _, err := Kodiraj(strings.Repeat("x", 120)); err == nil {
		t.Error("predug tekst prošao")
	}
}
