package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// U ovjerenom dnevniku COP-a zapis upisan nakon ovjere nosi oznaku s
// vremenom upisa, uz ime u retku „upisao”, a zapis storniran nakon ovjere
// oznaku storna; zapis iz vremena prije ovjere je nema, a ni neovjeren
// dnevnik oznaku ne pokazuje (Odluke (30. 10. 2026.))
func TestDnevnikCOPOznacavaZapisNakonOvjere(t *testing.T) {
	dan := time.Date(2026, 10, 30, 0, 0, 0, 0, models.Zagreb)
	ovjera := time.Date(2026, 10, 30, 14, 0, 0, 0, models.Zagreb)
	poslije := ovjera.Add(25 * time.Minute)
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Journal: &models.Journal{ID: "dn-9", Kind: models.JournalKindDefense, CentarSektor: "B",
			Title: "Dnevnik COP-a, 2026.", Zakljucio: "Ana Anić", ZakljucenoAt: &ovjera},
		Dani: []DanZapisa{{Dan: dan, Zapisi: []models.JournalEntry{
			{ID: "z1", Number: 1, Kind: models.EntryKindNote, Text: "prije ovjere", UserName: "Marko Marić", CreatedAt: ovjera.Add(-time.Hour), UpdatedAt: ovjera.Add(-time.Hour)},
			{ID: "z2", Number: 2, Kind: models.EntryKindNote, Text: "s drugog računala", UserName: "Iva Ivić", CreatedAt: poslije, UpdatedAt: poslije},
			{ID: "z3", Number: 3, Kind: models.EntryKindNote, Text: "storniran poslije", UserName: "Marko Marić", CreatedAt: ovjera.Add(-time.Hour),
				UpdatedAt: poslije.Add(5 * time.Minute), Voided: true, VoidReason: "krivo", VoidedBy: "Iva Ivić"},
		}}},
	}
	html := iscrtaj(t, "dnevnik_cop.html", d)
	if n := strings.Count(html, "nastao nakon ovjere"); n != 1 {
		t.Errorf("oznaka nakon ovjere stoji %d puta, a treba uz jedan zapis", n)
	}
	if !strings.Contains(html, "upisao Iva Ivić") || !strings.Contains(html, "nastao nakon ovjere, upisan 30.10.2026. 14:25") {
		t.Error("zapis nakon ovjere nema ime i vrijeme upisa")
	}
	if n := strings.Count(html, "storniran nakon ovjere, 30.10.2026. 14:30"); n != 1 {
		t.Errorf("oznaka storna nakon ovjere stoji %d puta, a treba uz jedan zapis", n)
	}
	if !strings.Contains(html, "s drugog računala") || !strings.Contains(html, "prije ovjere") {
		t.Error("ovjeren dnevnik ne pokazuje oba zapisa")
	}

	d.Journal.ZakljucenoAt, d.Journal.Zakljucio = nil, ""
	if html := iscrtaj(t, "dnevnik_cop.html", d); strings.Contains(html, "nakon ovjere") {
		t.Error("neovjeren dnevnik pokazuje oznaku nakon ovjere")
	}
}
