package models

import (
	"testing"
	"time"
)

// Zapis upisan nakon ovjere dnevnika nosi oznaku; zapis iz vremena prije
// ovjere i zapis u neovjerenom dnevniku ne nose (Odluke (30. 10. 2026.))
func TestZapisNakonOvjere(t *testing.T) {
	ovjera := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	zapis := func(kad time.Time) JournalEntry { return JournalEntry{CreatedAt: kad} }
	ovjeren := Journal{ZakljucenoAt: &ovjera}
	for _, s := range []struct {
		opis   string
		j      Journal
		e      JournalEntry
		oznaka bool
	}{
		{"upisan poslije ovjere", ovjeren, zapis(ovjera.Add(time.Minute)), true},
		{"upisan prije ovjere", ovjeren, zapis(ovjera.Add(-time.Minute)), false},
		{"upisan u trenutku ovjere", ovjeren, zapis(ovjera), false},
		{"dnevnik nije ovjeren", Journal{}, zapis(ovjera.Add(time.Hour)), false},
	} {
		if got := s.j.ZapisNakonOvjere(s.e); got != s.oznaka {
			t.Errorf("%s: oznaka %v, očekivano %v", s.opis, got, s.oznaka)
		}
	}
}

// Zapis iz vremena prije ovjere koji je izmijenjen ili storniran nakon nje
// nosi oznaku izmjene; neizmijenjen zapis, novi zapis i zapis neovjerenog
// dnevnika je ne nose
func TestZapisIzmijenjenNakonOvjere(t *testing.T) {
	ovjera := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	zapis := func(upisan, izmijenjen time.Time) JournalEntry {
		return JournalEntry{CreatedAt: upisan, UpdatedAt: izmijenjen}
	}
	ovjeren := Journal{ZakljucenoAt: &ovjera}
	prije, poslije := ovjera.Add(-time.Hour), ovjera.Add(time.Hour)
	for _, s := range []struct {
		opis   string
		j      Journal
		e      JournalEntry
		oznaka bool
	}{
		{"izmijenjen poslije ovjere", ovjeren, zapis(prije, poslije), true},
		{"izmijenjen prije ovjere", ovjeren, zapis(prije, prije.Add(time.Minute)), false},
		{"novi zapis poslije ovjere", ovjeren, zapis(poslije, poslije.Add(time.Minute)), false},
		{"dnevnik nije ovjeren", Journal{}, zapis(prije, poslije), false},
	} {
		if got := s.j.ZapisIzmijenjenNakonOvjere(s.e); got != s.oznaka {
			t.Errorf("%s: oznaka %v, očekivano %v", s.opis, got, s.oznaka)
		}
	}
}
