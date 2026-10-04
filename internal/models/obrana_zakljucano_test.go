package models

import (
	"testing"
	"time"
)

// Zaključava izračun stupnja obrane iz pragova za rubne i neispravne
// pragove, kakvi mogu stići iz prijepisa dokumentacije ili razmjenom, te
// pomoćne funkcije stupnja i epizode.

func pragCm(v int) Threshold { return Threshold{Cm: &v} }

func TestStupanjObraneNaRubnimPragovima(t *testing.T) {
	slucajevi := []struct {
		ime   string
		letva Station
		cm    int
		ocek  DefensePhase
	}{
		// prag 0 i negativni pragovi su valjani pragovi
		{"prag 0, vodostaj 0", Station{Prep: pragCm(0)}, 0, PhasePrep},
		{"prag 0, vodostaj -1", Station{Prep: pragCm(0)}, -1, PhaseNormal},
		{"negativni prag", Station{Prep: pragCm(-20), Regular: pragCm(-5)}, -10, PhasePrep},
		// jednaki pragovi: vrijedi viši stupanj (provjera ide odozgo)
		{"jednaki pragovi", Station{Prep: pragCm(400), Regular: pragCm(400)}, 400, PhaseRegular},
		// krivo poredani pragovi se ne provjeravaju: prvi zadovoljen odozgo
		{"krivi poredak", Station{Prep: pragCm(600), Regular: pragCm(500), Emergency: pragCm(450)}, 550, PhaseEmergency},
		{"krivi poredak, ispod svih", Station{Prep: pragCm(600), Regular: pragCm(500), Emergency: pragCm(450)}, 440, PhaseNormal},
		// samo rekordni vodostaj nije prag obrane
		{"samo rekord", Station{Record: pragCm(900)}, 950, PhaseUnknown},
		// prag zapisan samo tekstom ne broji se
		{"prag samo tekstom", Station{Prep: Threshold{Raw: "+300"}}, 400, PhaseUnknown},
		{"samo izvanredno stanje", Station{State: pragCm(800)}, 799, PhaseNormal},
		{"samo izvanredno stanje, na pragu", Station{State: pragCm(800)}, 800, PhaseState},
	}
	for _, s := range slucajevi {
		if got := s.letva.CalculateDefensePhase(s.cm); got != s.ocek {
			t.Errorf("%s: %d cm → %s, očekivano %s", s.ime, s.cm, got, s.ocek)
		}
	}
	if (Station{Record: pragCm(900)}).HasUsableThresholds() {
		t.Error("rekordni vodostaj sam nije upotrebljiv prag")
	}
}

func TestStupanjObraneZaNepoznateVrijednosti(t *testing.T) {
	for _, s := range []struct {
		faza    DefensePhase
		tezina  int
		naSnazi bool
	}{
		{PhaseState, 4, true},
		{PhaseEmergency, 3, true},
		{PhaseRegular, 2, true},
		{PhasePrep, 1, true},
		{PhaseNormal, 0, false},
		{PhaseUnknown, -1, false},
		{DefensePhase(""), 0, false},
		{DefensePhase("BILO_STO"), 0, false},
	} {
		if got := s.faza.Severity(); got != s.tezina {
			t.Errorf("%q: Severity %d, očekivano %d", s.faza, got, s.tezina)
		}
		if got := s.faza.InForce(); got != s.naSnazi {
			t.Errorf("%q: InForce %v, očekivano %v", s.faza, got, s.naSnazi)
		}
		if s.faza.Label() == "" {
			t.Errorf("%q: prazan naziv stupnja", s.faza)
		}
	}
	if DefensePhase("BILO_STO").Label() != PhaseNormal.Label() {
		t.Error("nepoznat stupanj se ispisuje kao stanje bez mjera obrane")
	}
	if PhaseUnknown.Label() == PhaseNormal.Label() {
		t.Error("nepoznat stupanj (bez pragova) ne smije izgledati kao mirno stanje")
	}
}

func TestTrajanjeEpizodeUDanima(t *testing.T) {
	pocetak := time.Date(2026, 3, 1, 23, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		kraj time.Time
		dana int
	}{
		// dani se broje kao pune 24 h plus jedan, a ne kalendarski
		{pocetak.Add(2 * time.Hour), 1},
		{pocetak.Add(23*time.Hour + 59*time.Minute), 1},
		{pocetak.Add(24 * time.Hour), 2},
		{pocetak.Add(-time.Hour), 1}, // kraj prije početka: najmanje jedan dan
	} {
		kraj := s.kraj
		e := DefenseEpisode{StartedAt: pocetak, EndedAt: &kraj}
		if got := e.Days(); got != s.dana {
			t.Errorf("od %s do %s: %d dana, očekivano %d", pocetak.Format(time.RFC3339), kraj.Format(time.RFC3339), got, s.dana)
		}
	}
	// proglašenje u istom trenutku kad je prag prijeđen nije „unaprijed”
	prag := pocetak
	e := DefenseEpisode{StartedAt: pocetak, ThresholdAt: &prag}
	if e.DeclaredBeforeThreshold() || e.LeadHours() != 0 {
		t.Errorf("proglašenje na prelasku praga: unaprijed %v, prednost %d h", e.DeclaredBeforeThreshold(), e.LeadHours())
	}
}
