package service

import (
	"testing"
	"time"

	"gocop/internal/models"
)

func prag(v int) models.Threshold { return models.Threshold{Cm: &v} }

func letvaBatina() models.Station {
	return models.Station{Name: "Batina", Prep: prag(300), Regular: prag(500), Emergency: prag(650), State: prag(800)}
}

func dan(g, m, d int) time.Time { return time.Date(g, time.Month(m), d, 7, 0, 0, 0, time.UTC) }

// Epizoda počinje prelaskom pripremnog stanja i traje dok se vodostaj ne vrati
// ispod njega; nosi najviši dosegnuti stupanj i vrh vala, ne zadnju vrijednost.
func TestEpizodaNosiVrhINajvisiStupanj(t *testing.T) {
	niz := []ocitanje{
		{dan(2024, 9, 15), 280}, // ispod praga
		{dan(2024, 9, 16), 340}, // počinje
		{dan(2024, 9, 17), 520},
		{dan(2024, 9, 18), 708}, // vrh, izvanredna obrana
		{dan(2024, 9, 19), 610},
		{dan(2024, 9, 20), 290}, // završava
		{dan(2024, 9, 21), 250},
	}
	epi := izracunaj(niz, letvaBatina())
	if len(epi) != 1 {
		t.Fatalf("epizoda %d, očekivano 1", len(epi))
	}
	e := epi[0]
	if !e.StartedAt.Equal(dan(2024, 9, 16)) {
		t.Errorf("počinje %s", e.StartedAt)
	}
	if e.EndedAt == nil || !e.EndedAt.Equal(dan(2024, 9, 19)) {
		t.Errorf("završava %v, očekivano 19.09.", e.EndedAt)
	}
	if e.PeakCm == nil || *e.PeakCm != 708 {
		t.Errorf("vrh %v, očekivano 708", e.PeakCm)
	}
	if e.PeakAt == nil || !e.PeakAt.Equal(dan(2024, 9, 18)) {
		t.Errorf("vrh nastupio %v, očekivano 18.09.", e.PeakAt)
	}
	if e.Phase != models.PhaseEmergency {
		t.Errorf("stupanj %q, očekivano izvanrednu obranu", e.Phase)
	}
	if e.Days() != 4 {
		t.Errorf("trajanje %d dana, očekivano 4", e.Days())
	}
}

// Dva odvojena vala daju dvije epizode, a ne jednu dugu.
func TestDvaValaDvijeEpizode(t *testing.T) {
	niz := []ocitanje{
		{dan(2013, 2, 14), 417}, {dan(2013, 2, 18), 310},
		{dan(2013, 2, 19), 180},
		{dan(2013, 3, 2), 332}, {dan(2013, 3, 8), 305},
		{dan(2013, 3, 9), 120},
	}
	epi := izracunaj(niz, letvaBatina())
	if len(epi) != 2 {
		t.Fatalf("epizoda %d, očekivano 2", len(epi))
	}
	if *epi[0].PeakCm != 417 || *epi[1].PeakCm != 332 {
		t.Errorf("vrhovi %d i %d", *epi[0].PeakCm, *epi[1].PeakCm)
	}
}

// Epizoda koja traje na kraju niza ostaje otvorena — obrana još nije gotova.
func TestEpizodaNaKrajuNizaOstajeOtvorena(t *testing.T) {
	niz := []ocitanje{{dan(2023, 12, 30), 520}, {dan(2023, 12, 31), 631}}
	epi := izracunaj(niz, letvaBatina())
	if len(epi) != 1 {
		t.Fatalf("epizoda %d", len(epi))
	}
	// zadnje očitanje zatvara epizodu tek kad vodostaj padne; dotad je kraj
	// zadnji dan iznad praga, a stupanj najviši dosegnuti
	if epi[0].Phase != models.PhaseRegular {
		t.Errorf("stupanj %q, očekivano redovnu obranu", epi[0].Phase)
	}
	if *epi[0].PeakCm != 631 {
		t.Errorf("vrh %d", *epi[0].PeakCm)
	}
}

// Vodostaj koji nikad ne prijeđe prag ne daje epizodu.
func TestBezPrelaskaPragaNemaEpizode(t *testing.T) {
	niz := []ocitanje{{dan(2022, 1, 4), 280}, {dan(2022, 1, 5), 299}, {dan(2022, 1, 6), 150}}
	if epi := izracunaj(niz, letvaBatina()); len(epi) != 0 {
		t.Errorf("epizoda %d, očekivano nijednu", len(epi))
	}
}
