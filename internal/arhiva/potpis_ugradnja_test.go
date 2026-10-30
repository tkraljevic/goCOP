package arhiva

import (
	"crypto/ed25519"
	"errors"
	"testing"
)

// Odluke (30. 10. 2026.), 13: paket letve bez potpisa se ne ugrađuje s
// diska. Procitaj ga i dalje čita, a TraziPotpis ga odbija.
func TestTraziPotpis(t *testing.T) {
	javni, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		opis string
		s    *Sadrzaj
		err  error
	}{
		{"bez paketa", nil, ErrNepotpisan},
		{"nepotpisan", &Sadrzaj{}, ErrNepotpisan},
		{"potpisan", &Sadrzaj{PotpisaoKljuc: javni}, nil},
	} {
		if err := TraziPotpis(x.s); !errors.Is(err, x.err) {
			t.Errorf("%s: %v", x.opis, err)
		}
	}
}
