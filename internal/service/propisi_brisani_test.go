package service

import (
	"testing"

	"gocop/internal/models"
)

// „Naslovi iznad članaka i članci 98. i 99. brišu se.” (NN 47/23, članak 8.)
// briše i naslove i članke, pa članci dobivaju oznaku „brisan”, a ne
// „mijenjan”. Promjena samog naslova iznad članka ostaje „mijenjan”.
func TestBrisanjeSNaslovomIznadJeBrisan(t *testing.T) {
	slucajevi := []struct {
		ime  string
		u    models.UputaIzmjene
		zeli string
	}{
		{"članci s naslovima", models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-98", "clanak-99"}, NaslovIznad: true}}, OznakaBrisan},
		{"članak bez naslova", models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-3"}}}, OznakaBrisan},
		{"stavak u članku", models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-100"}, Stavci: []string{"4"}}}, OznakaMijenjan},
		{"novi naslov iznad članka", models.UputaIzmjene{Vrsta: models.UputaNoviTekst,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-5"}, NaslovIznad: true}}, OznakaMijenjan},
	}
	for _, s := range slucajevi {
		if got := vrstaOznake(s.u); got != s.zeli {
			t.Errorf("%s: %q, a treba %q", s.ime, got, s.zeli)
		}
	}
}
