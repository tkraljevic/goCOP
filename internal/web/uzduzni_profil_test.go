package web

import (
	"strings"
	"testing"
)

func letvaProfila(letva string, rkm, kota, sada float64) LetvaProfila {
	return LetvaProfila{
		Letva: letva, Naziv: letva, Rkm: rkm, KotaNule: kota,
		SadaCm: sada, ImaSada: true,
		Cm:      map[int]float64{24: sada + 20, 48: sada + 40},
		Granice: map[int][2]float64{24: {sada + 10, sada + 30}, 48: {sada + 20, sada + 60}},
		Pragovi: map[string]float64{"prep": 300, "regular": 400, "emerg": 500},
	}
}

// Profil se crta u apsolutnim kotama: letva s višom nulom mora ležati više, i
// kad joj je vodostaj niži. Kroz centimetre bi crta pokazivala razliku nula, a
// ne nagib vodnog lica.
func TestProfilCrtaUApsolutnimKotama(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("uzvodna", 200, 120, 10),  // kota vode 120,10
		letvaProfila("nizvodna", 100, 90, 300), // kota vode 93,00
	})
	if p == nil {
		t.Fatal("nema profila")
	}
	if len(p.Tocke) != 2 {
		t.Fatalf("%d točaka", len(p.Tocke))
	}
	// Nizvodno ide udesno, i leži niže.
	if p.Tocke[0].X >= p.Tocke[1].X {
		t.Error("nizvodna letva nije desno od uzvodne")
	}
	if p.Tocke[0].Y >= p.Tocke[1].Y {
		t.Error("uzvodna letva nije iznad nizvodne, iako joj je kota vode viša")
	}
}

// Letva bez kote nule ili bez stacionaže ne može na profil: ne zna se ni gdje
// je ni koliko visoko. Ispuštanje je poštenije od nagađanja.
func TestProfilIzostavljaLetvuBezKote(t *testing.T) {
	bez := letvaProfila("bez-kote", 150, 0, 50)
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 200, 120, 10), bez, letvaProfila("b", 100, 90, 300),
	})
	if p == nil {
		t.Fatal("nema profila")
	}
	if len(p.Tocke) != 2 {
		t.Errorf("%d točaka, letva bez kote nije ispuštena", len(p.Tocke))
	}
}

// S jednom letvom profila nema — jedna točka ne pokazuje nagib.
func TestProfilTraziBaremDvijeLetve(t *testing.T) {
	if crtajUzduzni("Drava", []LetvaProfila{letvaProfila("a", 200, 120, 10)}) != nil {
		t.Error("profil nacrtan iz jedne letve")
	}
}

// Crte prognoze i pragova moraju doći na crtež, svaka sa svojim razredom.
func TestProfilNosiPrognozuIPragove(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 200, 120, 350), letvaProfila("b", 100, 90, 380),
	})
	if len(p.Crte) != 3 {
		t.Fatalf("%d crta umjesto tri (sad, 24 h, 48 h)", len(p.Crte))
	}
	for _, c := range p.Crte {
		if !strings.HasPrefix(c.Put, "M") {
			t.Errorf("crta %q ne počinje potezom M", c.Naziv)
		}
	}
	// Uz prognozu ide i pojas granica, zatvoren.
	for _, c := range p.Crte[1:] {
		if !strings.HasSuffix(c.Pojas, " Z") {
			t.Errorf("crta %q nema zatvoren pojas", c.Naziv)
		}
	}
	if len(p.Pragovi) != 3 {
		t.Errorf("%d pragova umjesto tri", len(p.Pragovi))
	}
}

// Pojas s rupom ne smije se nacrtati: spojio bi granice preko letve za koju se
// ne zna, i pokazao ogradu koja ne postoji.
func TestPojasSRupomSeNeCrta(t *testing.T) {
	a := letvaProfila("a", 200, 120, 350)
	b := letvaProfila("b", 100, 90, 380)
	delete(b.Granice, 24)
	p := crtajUzduzni("Drava", []LetvaProfila{a, b})
	for _, c := range p.Crte {
		if c.Naziv == "za 24 h" && c.Pojas != "" {
			t.Error("pojas nacrtan preko letve bez granica")
		}
	}
}
