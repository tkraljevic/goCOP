package web

import (
	"testing"

	"gocop/internal/models"
)

// Svaka vrsta službe ima kratki natpis; vrsta koje nema u popisu ostaje kakva jest.
func TestSluzbaKratko(t *testing.T) {
	for vrsta, zeli := range map[string]string{
		models.SluzbaCivilnaZastita:  "Civilna zaštita",
		models.SluzbaPrevencija:      "Prevencija i pripravnost",
		models.SluzbaCentar112:       "Centar 112",
		models.SluzbaPolicija:        "Policija",
		models.SluzbaPolicijskaPost:  "Policijska postaja",
		models.SluzbaLuckaKapetanija: "Lučka kapetanija",
		models.SluzbaStozerCZ:        "Stožer civilne zaštite",
		models.SluzbaVatrogasci:      "Vatrogasci",
		models.SluzbaCrveniKriz:      "Crveni križ",
		models.SluzbaOstalo:          "ostalo",
		"NEPOZNATO":                  "NEPOZNATO",
	} {
		if dobio := sluzbaKratko(vrsta); dobio != zeli {
			t.Errorf("%s: %q, želi %q", vrsta, dobio, zeli)
		}
	}
}
