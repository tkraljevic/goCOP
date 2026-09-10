package web

import "testing"

// int(v+0.5) je za negativne brojeve davao broj veći za jedan: -109,0 je
// postajalo -108, pa je kota vode ispadala centimetar previsoka. Batina je
// negativna veći dio godine.
func TestZaokruzivanjeRadiIZaNegativne(t *testing.T) {
	round := templateFuncs()["round"].(func(float64) int)
	for _, s := range []struct {
		ulaz float64
		want int
	}{
		{-109.0, -109}, {-118.1, -118}, {-118.6, -119}, {-0.4, 0}, {-0.6, -1},
		{109.0, 109}, {118.4, 118}, {118.6, 119}, {0.5, 1}, {-1.5, -2},
	} {
		if got := round(s.ulaz); got != s.want {
			t.Errorf("round(%.1f) = %d, očekivano %d", s.ulaz, got, s.want)
		}
	}
}
