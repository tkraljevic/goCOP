package web

import (
	"testing"

	"gocop/internal/models"
)

// Kartice idu redom kojim skupine stoje na aktu, a primatelj skupine koju
// ovaj program ne zna dobije svoju karticu na kraju, umjesto da nestane.
func TestPrimateljiPoSkupinama(t *testing.T) {
	d := AktiPageData{Skupine: models.SkupinePrimatelja, Primatelji: []models.Primatelj{
		{Naziv: "PU Primjer", Skupina: models.SkupinaSluzbe},
		{Naziv: "Grad Primjer", Skupina: models.SkupinaSamouprava},
		{Naziv: "Direkcija", Skupina: models.SkupinaUprava},
		{Naziv: "Lučka kapetanija", Skupina: models.SkupinaSluzbe},
		{Naziv: "Novo", Skupina: "NOVA"},
	}}
	got := d.PrimateljiPoSkupinama()
	want := []struct {
		naziv string
		broj  int
	}{{"Uprava organizacije", 1}, {"Službe", 2}, {"Županije, gradovi i općine", 1}, {"NOVA", 1}}
	if len(got) != len(want) {
		t.Fatalf("kartica %d, želim %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Naziv != w.naziv || len(got[i].Primatelji) != w.broj {
			t.Errorf("kartica %d: %q s %d, želim %q s %d", i, got[i].Naziv, len(got[i].Primatelji), w.naziv, w.broj)
		}
	}
	if got[1].Primatelji[0].Naziv != "PU Primjer" || got[1].Primatelji[1].Naziv != "Lučka kapetanija" {
		t.Errorf("unutar skupine promijenjen redoslijed: %+v", got[1].Primatelji)
	}
	if (AktiPageData{Skupine: models.SkupinePrimatelja}).PrimateljiPoSkupinama() != nil {
		t.Error("prazan registar daje kartice")
	}
}
