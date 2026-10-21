package repository

import "testing"

// Popis nizova letve ide od ovjerenog prema sirovom i preračunatom.
// Mađarska vodoprivreda stoji uz vituki, ispred preračunatih nizova i
// izvora kojih na popisu nema.
func TestRangIzvora(t *testing.T) {
	red := []string{"his2000", "cop", "letva-dhmz", "letva-hv", "vituki", "preracun-hq"}
	for i := 1; i < len(red); i++ {
		if rangIzvora(red[i-1]) >= rangIzvora(red[i]) {
			t.Errorf("%s nije ispred %s", red[i-1], red[i])
		}
	}
	if rangIzvora("vizugy-ddvizig") != rangIzvora("vituki") {
		t.Errorf("vizugy-ddvizig ima rang %d, a vituki %d", rangIzvora("vizugy-ddvizig"), rangIzvora("vituki"))
	}
}
