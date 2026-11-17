package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Rečenice obrasca zahvata (crtež 19a, 3): stanje u skladištu po oblicima,
// što stoji na mjestu i upozorenje uz dan prije zaključene inventure.
func TestReceniceZahvata(t *testing.T) {
	bez := func(s string) string { return strings.ReplaceAll(s, " ", " ") }
	vrece := models.VrstaSredstva{ID: "vrece", Naziv: "Vreće 50x80 cm", Jedinica: "kom", Oblici: []string{models.OblikPrazno, models.OblikPunjeno}}
	lopata := models.VrstaSredstva{ID: "lopata", Naziv: "Lopata", Jedinica: "kom"}
	stanje := []models.StanjeVrste{
		{Vrsta: vrece, PoOblicima: map[string]float64{models.OblikPrazno: 263550, models.OblikPunjeno: 1400}},
		{Vrsta: lopata, PoOblicima: map[string]float64{models.OblikOsnovni: 12}},
		{Vrsta: models.VrstaSredstva{ID: "prazna"}, PoOblicima: map[string]float64{}},
	}
	r := stanjaZaRecenicu(stanje)
	if bez(r["vrece"]) != "263 550 praznih i 1 400 napunjenih" || r["lopata"] != "12 kom" || len(r) != 2 {
		t.Errorf("stanje u skladištu: %q", r)
	}
	if spojiRijecima([]string{"a", "b", "c"}) != "a, b i c" || spojiRijecima(nil) != "" {
		t.Error("spajanje riječima")
	}

	teren := []models.Stanje{
		{AreaID: 16, SectionCode: "B.16.5", VrstaID: "vrece", Oblik: models.OblikPunjeno, Kolicina: 800},
		{AreaID: 16, SectionCode: "B.16.5", VrstaID: "agregat", Kolicina: 1},
		{AreaID: 16, Mjesto: "kod ustave", VrstaID: "lopata", Kolicina: 4},
	}
	stoji := stojiNaMjestima(teren, []models.VrstaSredstva{vrece, lopata})
	if bez(stoji[teren[0].Teren().Kljuc()]) != "Vreće 50x80 cm 800 napunjenih, agregat 1" || stoji[teren[2].Teren().Kljuc()] != "Lopata 4 kom" {
		t.Errorf("na mjestu: %q", stoji)
	}

	dan := time.Date(2025, 12, 31, 0, 0, 0, 0, models.Zagreb)
	zakljuceno := dan.Add(48 * time.Hour)
	popisi := []models.Popis{{Dan: dan.AddDate(-1, 0, 0), ZakljucenoAt: &zakljuceno}, {Dan: dan, ZakljucenoAt: &zakljuceno}, {Dan: dan.AddDate(0, 1, 0)}}
	if got := zadnjaZakljucena(popisi); !got.Equal(dan) {
		t.Errorf("zadnja zaključena: %v", got)
	}
	if u := upozorenjeInventure(dan.Add(15*time.Hour), dan); !strings.Contains(u, "31. 12. 2025.") {
		t.Errorf("isti dan kao inventura: %q", u)
	}
	if upozorenjeInventure(dan.AddDate(0, 0, 1), dan) != "" || upozorenjeInventure(dan, time.Time{}) != "" {
		t.Error("upozorenje poslije inventure ili bez nje")
	}
}
