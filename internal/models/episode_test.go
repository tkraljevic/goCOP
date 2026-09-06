package models

import (
	"testing"
	"time"
)

// Obrana se zna proglasiti prije nego što vodostaj dođe do praga — po
// prognozi ili po uzvodnim vodostajima. Epizoda to mora znati pokazati, jer
// bez toga izgleda kao da je počela bez razloga.
func TestObranaProglasenaPrijePraga(t *testing.T) {
	poc := time.Date(2024, 9, 17, 6, 0, 0, 0, time.UTC)
	prag := poc.Add(30 * time.Hour)
	e := DefenseEpisode{StartedAt: poc, ThresholdAt: &prag, Basis: BasisForecast}

	if !e.DeclaredBeforeThreshold() {
		t.Error("obrana proglašena 30 h prije praga ne prepoznaje se kao takva")
	}
	if e.LeadHours() != 30 {
		t.Errorf("prednost %d h, očekivano 30", e.LeadHours())
	}
	if e.BasisLabel() != "prognoza" {
		t.Errorf("osnova %q", e.BasisLabel())
	}
}

// Obrana proglašena nakon što je voda već prešla prag nema prednosti, a nema
// je ni ona kojoj prelazak nije zabilježen.
func TestObranaBezPrednosti(t *testing.T) {
	poc := time.Date(2024, 9, 17, 6, 0, 0, 0, time.UTC)
	prag := poc.Add(-8 * time.Hour)
	kasna := DefenseEpisode{StartedAt: poc, ThresholdAt: &prag}
	if kasna.DeclaredBeforeThreshold() || kasna.LeadHours() != 0 {
		t.Error("obrana proglašena nakon praga prikazuje se kao proglašena unaprijed")
	}
	bezPraga := DefenseEpisode{StartedAt: poc}
	if bezPraga.DeclaredBeforeThreshold() || bezPraga.LeadHours() != 0 {
		t.Error("obrana bez zabilježenog praga prikazuje se kao proglašena unaprijed")
	}
}
