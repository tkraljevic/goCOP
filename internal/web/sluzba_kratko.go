package web

import "gocop/internal/models"

// sluzbaKratko je kratki natpis vrste službe za retke na kartici županije i općine
// (crtež 17f). models.SluzbaLabel ostaje puni naziv, jer je i zadani naziv službe
// pri spremanju.
func sluzbaKratko(v string) string {
	switch v {
	case models.SluzbaCivilnaZastita:
		return "Civilna zaštita"
	case models.SluzbaPrevencija:
		return "Prevencija i pripravnost"
	case models.SluzbaCentar112:
		return "Centar 112"
	case models.SluzbaPolicija:
		return "Policija"
	case models.SluzbaPolicijskaPost:
		return "Policijska postaja"
	case models.SluzbaStozerCZ:
		return "Stožer civilne zaštite"
	case models.SluzbaVatrogasci:
		return "Vatrogasci"
	}
	return models.SluzbaLabel(v)
}
