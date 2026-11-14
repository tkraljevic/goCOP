package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Tablica međuslivova po crtežu 17e, 6d: po međuslivu broj stvarnih kišomjera
// i izvedenih točaka, zbroj težina i letve u čiju prognozu ulazi, a naziv vodi
// na odjeljak međusliva (sidro sliv-{oznaka}, na koje vodi i kartica kišomjera).
func TestTablicaMedjuslivova(t *testing.T) {
	km2 := 812.0
	tocka := KarticaKisomjera{Kisomjer: models.Kisomjer{Code: "g-ravnica-1", Naziv: "G ravnica 1", Aktivan: true}, UPrognozi: true}
	d := SlivoviPageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{},
		Skupine: []SkupinaKisomjera{
			{Sliv: models.Sliv{Oznaka: "G", Naziv: "Karašica do Popovca", Km2: &km2}, Tocke: []KarticaKisomjera{tocka, tocka},
				Stvarni: []KarticaKisomjera{{Kisomjer: models.Kisomjer{Code: "orahovica", Naziv: "Orahovica", Aktivan: true}}},
				Tezina:  0.9, UPrognozi: []string{"Popovac", "Branjin Vrh"}},
			{Sliv: models.Sliv{Naziv: "Bez međusliva"}},
		},
	}
	html := iscrtaj(t, "slivovi.html", d)
	tablica := izmedju(html, `<table class="data-table kartice-na-mobitelu dio-tablica sli-tablica">`, "</table>")
	for _, want := range []string{`<span class="kis-oznaka">G</span>`, `href="#sliv-G">Karašica do Popovca</a>`, "812",
		"Stvarni kišomjeri: </span>1", "Izvedene točke: </span>2", "Σ 0,90", "sl-tezina-pozor", "Popovac, Branjin Vrh", "Bez međusliva"} {
		if !strings.Contains(tablica, want) {
			t.Errorf("tablica međuslivova nema %q", want)
		}
	}
	for _, want := range []string{`class="povratna-poveznica">← Registri`, "Slivovi i kišomjeri", `id="sliv-G"`} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica slivova nema %q", want)
		}
	}
}
