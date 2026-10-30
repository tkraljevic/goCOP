package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// U dnevnik.xlsx ovjerenog dnevnika zapis nastao, izmijenjen ili storniran
// nakon ovjere nosi istu oznaku kao na stranici dnevnika; zapis iz vremena
// prije ovjere i zapis neovjerenog dnevnika je ne nose (Odluke (30. 10. 2026.))
func TestDnevnikXLSXOznacavaZapiseNakonOvjere(t *testing.T) {
	ovjera := time.Date(2026, 10, 30, 14, 0, 0, 0, models.Zagreb)
	prije, poslije := ovjera.Add(-time.Hour), ovjera.Add(25*time.Minute)
	j := &models.Journal{ID: "dn-9", Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, 2026.",
		Zakljucio: "Ana Anić", ZakljucenoAt: &ovjera}
	zapisi := []models.JournalEntry{
		{ID: "z1", Number: 1, Date: prije, Kind: models.EntryKindNote, Text: "prije ovjere", UserName: "Marko Marić", CreatedAt: prije, UpdatedAt: prije},
		{ID: "z2", Number: 2, Date: poslije, Kind: models.EntryKindNote, Text: "novi", UserName: "Iva Ivić", CreatedAt: poslije, UpdatedAt: poslije},
		{ID: "z3", Number: 3, Date: prije, Kind: models.EntryKindNote, Text: "ispravljen", UserName: "Marko Marić", CreatedAt: prije, UpdatedAt: poslije},
		{ID: "z4", Number: 4, Date: prije, Kind: models.EntryKindNote, Text: "storniran", UserName: "Marko Marić", CreatedAt: prije,
			UpdatedAt: poslije, Voided: true, VoidedBy: "Iva Ivić"},
	}
	tekst := func(j *models.Journal) map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, red := range KnjigaDnevnika(j, zapisi, nil, ZaglavljeIzvoza{Potpisnici: []PotpisnikIzvoza{{}}}, nil, "").Listovi[0].Redci {
			for _, c := range red {
				for _, z := range zapisi {
					if strings.HasPrefix(c.Tekst, z.Text) {
						out[z.ID] = c.Tekst
					}
				}
			}
		}
		return out
	}
	got := tekst(j)
	for id, oznaka := range map[string]string{
		"z2": "[nastao nakon ovjere, upisan 30.10.2026. 14:25]",
		"z3": "[izmijenjen nakon ovjere, 30.10.2026. 14:25]",
		"z4": "[storniran nakon ovjere, 30.10.2026. 14:25]",
	} {
		if !strings.Contains(got[id], oznaka) {
			t.Errorf("zapis %s nema oznaku %q: %q", id, oznaka, got[id])
		}
	}
	if strings.Contains(got["z1"], "nakon ovjere") {
		t.Errorf("zapis iz vremena prije ovjere nosi oznaku: %q", got["z1"])
	}
	neovjeren := *j
	neovjeren.ZakljucenoAt, neovjeren.Zakljucio = nil, ""
	for id, s := range tekst(&neovjeren) {
		if strings.Contains(s, "nakon ovjere") {
			t.Errorf("neovjeren dnevnik: zapis %s nosi oznaku: %q", id, s)
		}
	}
}
