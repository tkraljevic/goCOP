package models

import "testing"

// Položaj zapornice riječima; uz otvorenu i postotak, a stari zapis
// „djelomično” ostaje čitljiv
func TestPolozajZapornice(t *testing.T) {
	pct := 40
	for _, s := range []struct {
		rd   Reading
		zeli string
	}{
		{Reading{Gate: GateOpen, GatePct: &pct}, "otvorena 40 %"},
		{Reading{Gate: GateOpen}, "otvorena"},
		{Reading{Gate: GateClosed}, "zatvorena"},
		{Reading{Gate: GateClosed, GatePct: &pct}, "zatvorena"},
		{Reading{Gate: GatePartial}, "djelomično otvorena"},
		{Reading{}, ""},
	} {
		if got := s.rd.GateLabel(); got != s.zeli {
			t.Errorf("%+v: %q, želi %q", s.rd, got, s.zeli)
		}
	}
}
