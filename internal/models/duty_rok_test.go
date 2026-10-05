package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Zadani datum: privremena dužnost ga ima u Rok, a stvarni istek može biti
// raniji; zapis iz vremena prije Rok-a (bez isteka s obranom i ovisnosti)
// datum ima samo u isteku
func TestZadaniRok(t *testing.T) {
	rok, kraj := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	izvor := uuid.New()
	for _, tc := range []struct {
		ime  string
		d    Duty
		want *time.Time
	}{
		{"stari zapis", Duty{IsTemporary: true, ExpiresAt: &rok}, &rok},
		{"stalna", Duty{}, nil},
		{"s obranom i datumom", Duty{IsTemporary: true, IsticeSObranom: true, Rok: &rok, ExpiresAt: &kraj}, &rok},
		{"s obranom bez datuma", Duty{IsTemporary: true, IsticeSObranom: true, ExpiresAt: &kraj}, nil},
		{"ovisna bez datuma", Duty{IsTemporary: true, OvisiO: &izvor, ExpiresAt: &kraj}, nil},
	} {
		got := tc.d.ZadaniRok()
		if (got == nil) != (tc.want == nil) || got != nil && !got.Equal(*tc.want) {
			t.Errorf("%s: %v, očekivano %v", tc.ime, got, tc.want)
		}
	}
}
