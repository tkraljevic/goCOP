package web

import (
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Dan se boji fazom obrane koju doseže prognoza, a obrubljuje kad je doseže
// tek gornja granica raspona — to je upozorenje na trend prije nego što ga
// prognoza sama potvrdi.
func TestDnevniPregledBojiPragove(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{Code: "batina", Name: "Batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)}}
	izd := time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC)
	sat := izd.Unix() / 3600
	dnevne := map[string][]prognoza.DnevnaIzdana{"batina": {
		{Letva: "batina", Izdano: sat, Dan: 0, Vrijednost: 250},
		{Letva: "batina", Izdano: sat, Dan: 1, Vrijednost: 280, Dolje: 270, Gore: 310},
		{Letva: "batina", Izdano: sat, Dan: 2, Vrijednost: 520, Dolje: 480, Gore: 560},
	}}
	_, redovi := dnevniPregled(map[string]models.Station{"batina": st}, izd, dnevne)
	if len(redovi) != 1 || len(redovi[0].Dani) != prognoza.DnevniDosezi {
		t.Fatalf("redovi %+v", redovi)
	}
	d1, d2 := redovi[0].Dani[0], redovi[0].Dani[1]
	if d1.Razina != "" || d1.Moguce != "prep" || d1.Trend != "↑" {
		t.Errorf("1. dan %+v: treba bez boje, obrub pripremnog, rast", d1)
	}
	if d2.Razina != "regular" || d2.Moguce != "" {
		t.Errorf("2. dan %+v: treba redovna obrana", d2)
	}
	if redovi[0].Dani[2].Cm != "" {
		t.Errorf("dan bez prognoze mora ostati prazan")
	}
}
