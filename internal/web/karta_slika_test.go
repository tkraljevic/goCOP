package web

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// plocicaZa je standardni Mercatorov izračun; provjerava se na poznatim
// točkama, jer bi pogreška ovdje kartu pomaknula a slika bi i dalje izgledala
// uvjerljivo — samo bi pokazivala krivo mjesto.
func TestPolozajNaMreziPlocica(t *testing.T) {
	// ishodište mreže: 0°, 0° pada točno na sredinu svijeta
	x, y := plocicaZa(0, 0, 1)
	if math.Abs(x-1) > 1e-9 || math.Abs(y-1) > 1e-9 {
		t.Errorf("nulta točka: %.6f, %.6f — očekivano 1, 1", x, y)
	}
	// Batina: poznata pločica na zumu 14
	bx, by := plocicaZa(45.845833, 18.854722, 14)
	if int(bx) != 9050 || int(by) != 5838 {
		t.Errorf("Batina pada na pločicu %d/%d, očekivano 9050/5838", int(bx), int(by))
	}
	// veći zum znači razmjerno više pločica
	x1, _ := plocicaZa(45.845833, 18.854722, 15)
	if math.Abs(x1-2*bx) > 1e-6 {
		t.Errorf("udvostručenjem zuma položaj se nije udvostručio: %.3f vs %.3f", x1, 2*bx)
	}
}

// posluziteljPlocica vraća pločice u kojima svaka nosi svoju boju, pa se iz
// složene slike vidi je li izrez pao na pravo mjesto.
func posluziteljPlocica(t *testing.T, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if r.Header.Get("Referer") == "" {
			t.Error("zahtjev za pločicu nema Referer — pravi poslužitelji ga traže")
		}
		img := image.NewRGBA(image.Rect(0, 0, velicinaPlocice, velicinaPlocice))
		for y := 0; y < velicinaPlocice; y++ {
			for x := 0; x < velicinaPlocice; x++ {
				img.Set(x, y, color.RGBA{200, 210, 220, 255})
			}
		}
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, img)
	}))
}

// Letva mora pasti u sredinu slike, ne u sredinu neke pločice. Pogreška ovdje
// daje kartu koja izgleda ispravno, a oznaka stoji na krivom mjestu.
func TestKartaSeIzrezujeOkoLetve(t *testing.T) {
	srv := posluziteljPlocica(t, http.StatusOK)
	defer srv.Close()
	k := KartaPostavke{Plocice: srv.URL + "/{z}/{x}/{y}.png", Zasluge: "probne pločice", NajviseZ: 17}

	s := slozKartu(context.Background(), k, 45.845833, 18.854722, "http://localhost/")
	if s == nil {
		t.Fatal("karta se nije složila")
	}
	if s.Sirina != sirinaKarte || s.Visina != visinaKarte {
		t.Errorf("veličina %d×%d, očekivano %d×%d", s.Sirina, s.Visina, sirinaKarte, visinaKarte)
	}
	if s.Zasluge != "probne pločice" {
		t.Errorf("zasluge se gube: %q", s.Zasluge)
	}
	img, err := png.Decode(bytes.NewReader(s.PNG))
	if err != nil {
		t.Fatalf("složena karta nije ispravan PNG: %v", err)
	}
	// oznaka je tamna i stoji u sredini; podloga je svijetla
	sredina := img.At(sirinaKarte/2, visinaKarte/2)
	r, g, b, _ := sredina.RGBA()
	if r > 30000 || g > 30000 || b > 30000 {
		t.Errorf("u sredini slike nema oznake: %v", sredina)
	}
	// a rub je podloga
	rub := img.At(4, 4)
	rr, _, _, _ := rub.RGBA()
	if rr < 40000 {
		t.Errorf("rub slike nije podloga: %v", rub)
	}
}

// Bez pločica dokument nastaje bez karte — program radi i offline, pa se
// izvješće zbog nedostupne podloge ne smije odbiti sastaviti.
func TestBezPlocicaNemaKarteAliNemaNiGreske(t *testing.T) {
	srv := posluziteljPlocica(t, http.StatusNotFound)
	defer srv.Close()
	k := KartaPostavke{Plocice: srv.URL + "/{z}/{x}/{y}.png", NajviseZ: 17}
	if s := slozKartu(context.Background(), k, 45.845833, 18.854722, "x"); s != nil {
		t.Error("s nedostupnim pločicama karta se ne smije složiti")
	}
	// karta isključena u postavkama
	if s := slozKartu(context.Background(), KartaPostavke{}, 45.8, 18.8, "x"); s != nil {
		t.Error("bez postavljene karte se ništa ne slaže")
	}
}
