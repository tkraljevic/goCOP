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

// SazetakSluzbi je dio službi za karticu grada ili općine: prvih n redom, uz njih
// uvijek i služba vezana baš za tu općinu; Jos broji ostale.
type SazetakSluzbi struct {
	Prve []models.Sluzba
	Jos  int
}

func sazetakSluzbi(sluzbe []models.Sluzba, n int) SazetakSluzbi {
	var s SazetakSluzbi
	for _, x := range sluzbe {
		if len(s.Prve) < n || x.MunicipalityID != 0 {
			s.Prve = append(s.Prve, x)
		} else {
			s.Jos++
		}
	}
	return s
}
