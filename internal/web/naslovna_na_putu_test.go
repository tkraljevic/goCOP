package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Roba na putu na Naslovnoj: skladištu i terenu „Stiže”, a COP-u pošiljke
// koje su na putu dulje od 24 h, svaka sa zaprimanjem
func TestNaPutuNaNaslovnoj(t *testing.T) {
	sad := time.Date(2026, 11, 17, 10, 0, 0, 0, models.Zagreb)
	staro := models.Posiljka{Slanje: models.Promet{VezaID: "v1", VrstaID: "vrece", Kolicina: -400, SkladisteID: "osijek", SkladisteNaziv: "Osijek", PremaAreaID: 34,
		PremaAreaName: "Drava i Dunav", PremaSectionCode: "B.34.1", Datum: sad.AddDate(0, 0, -2), CreatedAt: sad.Add(-30 * time.Hour)}, Zaprimljeno: 100, PremaSektor: "B"}
	novo := models.Posiljka{Slanje: models.Promet{VezaID: "v2", VrstaID: "vrece", Kolicina: -5, SkladisteNaziv: "Osijek", PremaSkladisteID: "draz",
		PremaSkladisteNaziv: "CS Draž", Datum: sad, CreatedAt: sad.Add(-time.Hour)}, PremaSektor: "B"}
	h := &DashboardHandler{izvori: IzvoriNaslovne{
		Posiljke: func(_ context.Context, f repository.FiltarPosiljki) ([]models.Posiljka, error) {
			switch {
			case f.PremaSkladisteID == "draz":
				return []models.Posiljka{novo}, nil
			case f.PremaAreaID == 34:
				return []models.Posiljka{staro}, nil
			case f.Sektor == "B":
				return []models.Posiljka{staro, novo}, nil
			}
			return nil, nil
		},
		VrsteSredstava: func(context.Context) ([]models.VrstaSredstva, error) {
			return []models.VrstaSredstva{{ID: "vrece", Naziv: "Vreće za pijesak", Jedinica: "kom"}}, nil
		},
	}}
	ctx := context.Background()

	skladista := []models.Skladiste{{ID: "draz", Naziv: "CS Draž"}, {ID: "osijek", Naziv: "Osijek"}}
	if r := h.stizeSkladistima(ctx, skladista); len(r) != 1 || r[0].Glavno != "Stiže: 1 pošiljka" || r[0].URL != "/sredstva/skladista/draz" || !strings.HasPrefix(r[0].Sporedno, "CS Draž") {
		t.Errorf("stiže u skladište: %+v", r)
	}
	if k := h.popisiZaRijesiti(ctx, skladista, sad); k.Broj != 1 || k.Prazno != nil {
		t.Errorf("Za riješiti skladištara: %+v", k)
	}
	if r := h.stizeNaTeren(ctx, "B", 34); len(r) != 1 || r[0].Glavno != "Stiže na teren: 1 pošiljka" || r[0].URL != "/sredstva/na-terenu?sektor=B" {
		t.Errorf("stiže na teren: %+v", r)
	}
	if r := h.stizeNaTeren(ctx, "B", 16); r != nil {
		t.Errorf("ništa ne stiže: %+v", r)
	}
	r := h.dugoNaPutu(ctx, "B", sad)
	if len(r) != 1 || r[0].Glavno != "Na putu dulje od 24 h: 300 kom · Vreće za pijesak" || r[0].URL != "/sredstva/na-putu/v1" ||
		!strings.Contains(r[0].Sporedno, "Osijek → teren · BP 34") {
		t.Errorf("dugo na putu: %+v", r)
	}
	if k := karticaZaRijesiti(stavkeZaRijesiti{NaPutu: r}, sad); k.Broj != 1 {
		t.Errorf("Za riješiti COP-a: %+v", k)
	}
	if r := h.dugoNaPutu(ctx, "", sad); r != nil {
		t.Errorf("bez sektora: %+v", r)
	}
	if r := (&DashboardHandler{}).stizeNaTeren(ctx, "B", 34); r != nil {
		t.Errorf("bez izvora: %+v", r)
	}
	if brojPosiljaka(2) != "2 pošiljke" || brojPosiljaka(5) != "5 pošiljaka" {
		t.Error("broj pošiljaka")
	}
}
