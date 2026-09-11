package arhiva

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func probniManifest() (Manifest, []DioPaketa) {
	return Manifest{
			Inacica: 2, Letva: "batina", Izdanje: 3, Izdao: "cop-osijek-node",
			Otisak: "6e0ed8df", Nizova: 15, Zapisa: 933732, Od: "1901-01-01", Do: "2026-09-11",
		}, []DioPaketa{
			{Ime: "nizovi.json", Otisak: "aa", Bajtova: 10},
			{Ime: "ocitanja.bin", Otisak: "bb", Bajtova: 20},
		}
}

// Otisak dokazuje cjelovitost, potpis dokazuje tko je izdao. Tko promijeni
// sadržaj, izračuna novi otisak i upiše proizvoljno ime u "izdao" — potpis je
// jedino što to razlikuje.
func TestPotpisPrepoznajePromjenuSadrzaja(t *testing.T) {
	javni, tajni, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	m, d := probniManifest()
	p := potpisi(m, d, tajni)
	if p == nil {
		t.Fatal("potpis nije nastao")
	}

	got, err := ProvjeriPotpis(m, d, p)
	if err != nil {
		t.Fatalf("valjan potpis odbijen: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(javni) {
		t.Error("vraćen krivi ključ")
	}

	// Promijenjen otisak — sadržaj je drugi.
	m2 := m
	m2.Otisak = "drugi"
	if _, err := ProvjeriPotpis(m2, d, p); err == nil {
		t.Error("potpis je prošao uz promijenjen otisak")
	}
	// Promijenjen izdavač — netko se predstavlja kao drugi čvor.
	m3 := m
	m3.Izdao = "tudji-cvor"
	if _, err := ProvjeriPotpis(m3, d, p); err == nil {
		t.Error("potpis je prošao uz promijenjenog izdavača")
	}
	// Promijenjen dio — zajednički otisak veže spojene dijelove, ali ne i
	// granicu među njima; otisak po dijelu veže i to.
	d2 := append([]DioPaketa(nil), d...)
	d2[0].Bajtova = 11
	if _, err := ProvjeriPotpis(m, d2, p); err == nil {
		t.Error("potpis je prošao uz promijenjen dio")
	}
}

// Poredak dijelova u ZIP-u ne smije mijenjati potpis: isti sadržaj prepakiran
// drugim redom je isti paket.
func TestPotpisNeOvisiOPoretkuDijelova(t *testing.T) {
	_, tajni, _ := ed25519.GenerateKey(nil)
	m, d := probniManifest()
	obrnuto := []DioPaketa{d[1], d[0]}

	p := potpisi(m, d, tajni)
	if _, err := ProvjeriPotpis(m, obrnuto, p); err != nil {
		t.Errorf("potpis pukao na drugom poretku: %v", err)
	}
}

// Tuđi ključ ne prolazi.
func TestTudjiKljucNeProlazi(t *testing.T) {
	_, tajni, _ := ed25519.GenerateKey(nil)
	_, drugi, _ := ed25519.GenerateKey(nil)
	m, d := probniManifest()

	p := potpisi(m, d, tajni)
	lazni := potpisi(m, d, drugi)
	// Potpis jednog s ključem drugog.
	krivo := &Potpis{Kljuc: p.Kljuc, Vaznost: lazni.Vaznost}
	if _, err := ProvjeriPotpis(m, d, krivo); err == nil {
		t.Error("potpis s tuđim ključem je prošao")
	}
}

// Nepotpisan paket se prepoznaje kao nepotpisan, ne kao neispravan.
func TestNepotpisanSePrepoznaje(t *testing.T) {
	m, d := probniManifest()
	_, err := ProvjeriPotpis(m, d, nil)
	if err == nil || !strings.Contains(err.Error(), "nije potpisan") {
		t.Errorf("nepotpisan paket: %v", err)
	}
	if potpisi(m, d, nil) != nil {
		t.Error("bez ključa je nastao potpis")
	}
}

// Kanonski zapis mora biti isti za isti sadržaj, svaki put.
func TestKanonskiZapisJeStabilan(t *testing.T) {
	m, d := probniManifest()
	a := string(kanonski(m, d))
	b := string(kanonski(m, []DioPaketa{d[1], d[0]}))
	if a != b {
		t.Error("kanonski zapis ovisi o poretku dijelova")
	}
	if !strings.Contains(a, "izdao\tcop-osijek-node\n") {
		t.Errorf("kanonski zapis: %q", a)
	}
	// Vrijeme nastanka NE ulazi: isti sadržaj izdan dvaput mora dati isti
	// potpis, kao što daje i isti otisak.
	if strings.Contains(a, "nastalo") {
		t.Error("vrijeme nastanka ulazi u potpis, pa isti sadržaj daje dva potpisa")
	}
}
