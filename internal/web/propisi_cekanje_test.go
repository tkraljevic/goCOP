package web

import (
	"regexp"
	"testing"
	"time"
)

// cekaonicaSaSatom je cekaonica s podmetnutim satom
func cekaonicaSaSatom(sada *time.Time) *cekaonica[string] {
	c := novaCekaonica[string]()
	c.sada = func() time.Time { return *sada }
	return c
}

// Pregled čeka 30 minuta; poslije se ne može uzeti, a sljedeće stavljanje
// ga počisti (Odluke (25. 11. 2026.))
func TestCekaonicaIstjece(t *testing.T) {
	sada := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	c := cekaonicaSaSatom(&sada)
	k := c.stavi("pperic", "zakon")
	sada = sada.Add(29 * time.Minute)
	if v, ok := c.uzmi(k, "pperic"); !ok || v != "zakon" {
		t.Fatalf("nakon 29 minuta: %q %v", v, ok)
	}
	sada = sada.Add(2 * time.Minute)
	if _, ok := c.uzmi(k, "pperic"); ok {
		t.Fatal("pregled nije istekao nakon 31 minute")
	}
	c.stavi("pperic", "plan")
	if len(c.stavke) != 1 {
		t.Errorf("istekli pregled nije počišćen: %d", len(c.stavke))
	}
}

// Ključ je 16 slučajnih bajtova u heksu i vrijedi samo za osobu koja je
// učitala propis: drugi administrator s istim ključem ne dobije ništa
func TestCekaonicaVezanaUzOsobu(t *testing.T) {
	sada := time.Now()
	c := cekaonicaSaSatom(&sada)
	k1, k2 := c.stavi("pperic", "zakon"), c.stavi("pperic", "plan")
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(k1) || k1 == k2 {
		t.Errorf("ključevi %q i %q", k1, k2)
	}
	if _, ok := c.uzmi(k1, "mmaric"); ok {
		t.Error("tuđi pregled je dan drugoj osobi")
	}
	if _, ok := c.uzmi("nepostojeci", "pperic"); ok {
		t.Error("nepostojeći ključ je dao pregled")
	}
}

// Na čekanju je najviše osam pregleda; deveti istisne najstariji
func TestCekaonicaNajviseOsam(t *testing.T) {
	sada := time.Now()
	c := cekaonicaSaSatom(&sada)
	var kljucevi []string
	for i := 0; i < 9; i++ {
		sada = sada.Add(time.Second)
		kljucevi = append(kljucevi, c.stavi("pperic", "pregled"))
	}
	if len(c.stavke) != 8 {
		t.Errorf("na čekanju %d", len(c.stavke))
	}
	if _, ok := c.uzmi(kljucevi[0], "pperic"); ok {
		t.Error("najstariji pregled nije ispao")
	}
	if _, ok := c.uzmi(kljucevi[8], "pperic"); !ok {
		t.Error("najnoviji pregled je ispao")
	}
}

// Uzimanje ne briše pregled (provjera se može otvoriti ponovno), a
// micanje nakon spremanja briše
func TestUzmiNeBriseMakniBrise(t *testing.T) {
	sada := time.Now()
	c := cekaonicaSaSatom(&sada)
	k := c.stavi("pperic", "zakon")
	for i := 0; i < 2; i++ {
		if _, ok := c.uzmi(k, "pperic"); !ok {
			t.Fatalf("%d. uzimanje nije dalo pregled", i+1)
		}
	}
	c.makni(k)
	if _, ok := c.uzmi(k, "pperic"); ok {
		t.Error("maknut pregled se još može uzeti")
	}
}
