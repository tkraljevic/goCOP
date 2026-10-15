package models

import "testing"

// Zaključava boje kojima se stupanj obrane prikazuje u tekstu (značka) i u
// tablici (pilula). Nepoznat stupanj ima vlastitu značku, da postaja bez
// pragova ne izgleda kao mirna, a sve što stupanj nije ispisuje se kao stanje
// bez mjera obrane.
func TestBojeStupnjaObrane(t *testing.T) {
	for _, s := range []struct {
		faza   DefensePhase
		znacka string
		pilula string
	}{
		{PhaseState, "badge-state", "crit"},
		{PhaseEmergency, "badge-emergency", "emerg"},
		{PhaseRegular, "badge-regular", "regular"},
		{PhasePrep, "badge-prep", "prep"},
		{PhaseNormal, "badge-normal", "none"},
		{PhaseUnknown, "badge-unknown", "none"},
		{DefensePhase(""), "badge-normal", "none"},
		{DefensePhase("BILO_STO"), "badge-normal", "none"},
	} {
		if got := s.faza.BadgeClass(); got != s.znacka {
			t.Errorf("%q: značka %q, očekivano %q", s.faza, got, s.znacka)
		}
		if got := s.faza.PillClass(); got != s.pilula {
			t.Errorf("%q: pilula %q, očekivano %q", s.faza, got, s.pilula)
		}
	}
}
