package izdanje

import "testing"

// Potpis ovisi o ključu, izdanju, dopuštenom izdanju i roku
func TestPotpisDopustenja(t *testing.T) {
	p := PotpisDopustenja("kljuc", "0.0.47-alfa", "0.0.45-alfa", 100)
	if len(p) != 64 || p != PotpisDopustenja("kljuc", "0.0.47-alfa", "0.0.45-alfa", 100) {
		t.Fatalf("potpis %q", p)
	}
	for _, drugi := range []string{
		PotpisDopustenja("drugi", "0.0.47-alfa", "0.0.45-alfa", 100),
		PotpisDopustenja("kljuc", "0.0.46-alfa", "0.0.45-alfa", 100),
		PotpisDopustenja("kljuc", "0.0.47-alfa", "0.0.44-alfa", 100),
		PotpisDopustenja("kljuc", "0.0.47-alfa", "0.0.45-alfa", 101),
	} {
		if drugi == p {
			t.Error("potpis ne ovisi o svim dijelovima")
		}
	}
}

// Donja granica vraćanja
func TestPrestaroZaVracanje(t *testing.T) {
	if _, ok := ParsirajOznaku(NajstarijeVracanje); !ok {
		t.Fatalf("NajstarijeVracanje %q nije oznaka izdanja", NajstarijeVracanje)
	}
	for oznaka, zelim := range map[string]bool{"0.0.36-alfa": true, "0.0.5-alfa": true, "0.0.37-alfa": false, "0.0.45-alfa": false, "0.1.0-beta": false} {
		v, _ := ParsirajOznaku(oznaka)
		if PrestaroZaVracanje(v) != zelim {
			t.Errorf("%s: %v", oznaka, !zelim)
		}
	}
}
