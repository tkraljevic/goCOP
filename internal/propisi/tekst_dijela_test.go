package propisi

import (
	"testing"

	"gocop/internal/models"
)

// Odluke (25. 11. 2026.): pretraga nalazi riječ bilo gdje u dijelu: u
// naslovu, stavku, točki bilo koje razine, rečenici iza nabrajanja i u
// tablici. Oznake stavaka i točaka ne ulaze, a prazni komadi se preskaču.
func TestTekstDijelaSkupljaSveRazine(t *testing.T) {
	d := models.DioPropisa{
		Oznaka: "clanak-3", Vrsta: models.DioClanak, Broj: "3.", Naslov: "Čuvanje nasipa",
		Stavci: []models.StavakPropisa{
			{Oznaka: "(1)", Tekst: "Nasip čuvaju:", Tocke: []models.TockaPropisa{
				{Vrsta: models.TockaBroj, Oznaka: "1.", Tekst: "vodočuvar", Tocke: []models.TockaPropisa{
					{Vrsta: models.TockaSlovo, Oznaka: "a)", Tekst: "danju"},
					{Vrsta: models.TockaPodstavak, Tekst: "  "},
				}},
				{Vrsta: models.TockaBroj, Oznaka: "2.", Tekst: "dežurni"},
			}, Nastavak: "po nalogu rukovoditelja."},
			{Oznaka: "(2)", Tablica: &models.TablicaPropisa{Retci: [][]models.CelijaTablice{
				{{Tekst: "Letva", Podebljano: true}, {Tekst: "Kota"}},
				{{Tekst: "Primjerovo"}, {Tekst: ""}},
			}}},
		},
	}
	zeli := "Čuvanje nasipa\nNasip čuvaju:\nvodočuvar\ndanju\ndežurni\npo nalogu rukovoditelja.\nLetva\nKota\nPrimjerovo"
	if got := TekstDijela(d); got != zeli {
		t.Errorf("TekstDijela:\n%q\nželi\n%q", got, zeli)
	}
	if got := TekstDijela(models.DioPropisa{Oznaka: "uvod", Vrsta: models.DioUvod}); got != "" {
		t.Errorf("prazan dio: %q", got)
	}
}
