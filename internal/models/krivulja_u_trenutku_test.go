package models

import (
	"testing"
	"time"
)

// Krivulja vrijedi po zagrebačkom danu: u ponoć po zagrebačkom vremenu, kad
// je u UTC-u još prethodni dan, već vrijedi nova.
func TestKrivuljaUTrenutku(t *testing.T) {
	krivulje := []HQKrivulja{
		{ID: 2, VrijediOd: "2026-01-01"},
		{ID: 1, VrijediOd: "2020-01-01", VrijediDo: "2025-12-31"},
	}
	slucajevi := []struct {
		naziv string
		kad   time.Time
		id    int64
	}{
		{"nova, bez kraja", time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC), 2},
		{"stara", time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC), 1},
		{"ponoć u Zagrebu", time.Date(2025, 12, 31, 23, 30, 0, 0, time.UTC), 2},
		{"prije svih", time.Date(2019, 6, 1, 12, 0, 0, 0, time.UTC), 0},
	}
	for _, s := range slucajevi {
		k := KrivuljaUTrenutku(krivulje, s.kad)
		if (k == nil && s.id != 0) || (k != nil && k.ID != s.id) {
			t.Errorf("%s: %+v, očekivana %d", s.naziv, k, s.id)
		}
	}
}
