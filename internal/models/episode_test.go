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

// Zaključava preklapanje epizode s razdobljem pregleda: dodir na rubu
// razdoblja računa se kao preklapanje, epizoda koja je završila prije ili
// počela poslije razdoblja se ne preklapa, a otvorena epizoda traje do sada
// pa se preklapa sa svakim razdobljem nakon svog početka.
func TestEpizodaURazdoblju(t *testing.T) {
	poc := time.Date(2024, 9, 17, 6, 0, 0, 0, Zagreb)
	kraj := time.Date(2024, 9, 30, 18, 0, 0, 0, Zagreb)
	zatvorena := DefenseEpisode{StartedAt: poc, EndedAt: &kraj}
	otvorena := DefenseEpisode{StartedAt: poc}
	dan := func(g int, m time.Month, d int) time.Time { return time.Date(g, m, d, 0, 0, 0, 0, Zagreb) }
	for _, s := range []struct {
		ime      string
		e        DefenseEpisode
		od, do   time.Time
		preklapa bool
	}{
		{"razdoblje unutar epizode", zatvorena, dan(2024, 9, 20), dan(2024, 9, 21), true},
		{"epizoda unutar razdoblja", zatvorena, dan(2024, 9, 1), dan(2024, 10, 31), true},
		{"razdoblje završava na početku", zatvorena, dan(2024, 9, 1), poc, true},
		{"razdoblje počinje na kraju", zatvorena, kraj, dan(2024, 10, 31), true},
		{"razdoblje prije epizode", zatvorena, dan(2024, 9, 1), poc.Add(-time.Minute), false},
		{"razdoblje nakon epizode", zatvorena, kraj.Add(time.Minute), dan(2024, 10, 31), false},
		{"otvorena, razdoblje godinu kasnije", otvorena, dan(2025, 9, 1), dan(2025, 9, 30), true},
		{"otvorena, razdoblje prije početka", otvorena, dan(2024, 8, 1), dan(2024, 8, 31), false},
	} {
		if got := s.e.Overlaps(s.od, s.do); got != s.preklapa {
			t.Errorf("%s: preklapanje %v, očekivano %v", s.ime, got, s.preklapa)
		}
	}
	if zatvorena.IsOpen() || !otvorena.IsOpen() {
		t.Errorf("otvorena je samo epizoda bez kraja: zatvorena %v, otvorena %v", zatvorena.IsOpen(), otvorena.IsOpen())
	}
}

// Zaključava ispis vrha vala uz epizodu: vodostaj s predznakom, kako se
// piše, a crtica kad vrh nije zabilježen.
func TestVrhEpizodeZaIspis(t *testing.T) {
	plus, minus, nula := 812, -15, 0
	for _, s := range []struct {
		vrh  *int
		want string
	}{
		{&plus, "+812 cm"},
		{&minus, "-15 cm"},
		{&nula, "+0 cm"},
		{nil, "—"},
	} {
		if got := (DefenseEpisode{PeakCm: s.vrh}).PeakLabel(); got != s.want {
			t.Errorf("vrh %q, očekivano %q", got, s.want)
		}
	}
}

// Zaključava najmanje trajanje: i kraj zapisan dva dana prije početka (pogrešan
// upis) daje jedan dan, a ne nulu ili negativan broj.
func TestEpizodaSKrajemPrijePocetkaTrajeDan(t *testing.T) {
	poc := time.Date(2026, 3, 10, 8, 0, 0, 0, Zagreb)
	kraj := poc.Add(-48 * time.Hour)
	if got := (DefenseEpisode{StartedAt: poc, EndedAt: &kraj}).Days(); got != 1 {
		t.Errorf("trajanje %d dana, očekivan 1", got)
	}
}
