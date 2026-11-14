package web

import (
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Naselja u obrascu grada ili općine (17f · 7e): svaki redak je svoj obrazac na istoj
// adresi, a polja u tablici vežu se na nj atributom form=. Obrasci naselja stoje izvan
// glavnog obrasca, a Obriši općinu je na dnu uz istu potvrdu.
func TestNaseljaUObrascuOpcine(t *testing.T) {
	html := iscrtajStranicu(t, "municipality_form.html", TerritoryPageData{
		Permissions:  &models.UserPermissions{IsGlobalAdmin: true},
		Municipality: models.Municipality{ID: 302, CountyID: 14, Name: "Primjerovo", Type: "OPCINA"},
		Settlements:  []models.Settlement{{ID: 4801, Name: "Branjin Vrh", PostalCode: "31300", Population: 812}},
		IsEdit:       true,
	})
	for _, mora := range []string{
		`id="naselja"`, `name="name" value="Branjin Vrh"`, `form="naselje-4801"`, `form="obrisi-naselje-4801"`,
		`<form id="naselje-4801" method="POST" action="/api/settlements/update" hidden>`,
		`<form id="obrisi-naselje-4801" method="POST" action="/api/settlements/delete" hidden`,
		`<form id="novo-naselje" method="POST" action="/api/settlements/create" hidden>`, `form="novo-naselje"`,
		`form="obrisi-opcinu"`, `Zapis ostaje u povijesti.`,
	} {
		if !strings.Contains(html, mora) {
			t.Errorf("nema %q", mora)
		}
	}
	glavni := regexp.MustCompile(`(?s)<form class="form-page form-wide".*?</form>`).FindString(html)
	if strings.Contains(glavni, "<form id=") {
		t.Error("obrazac naselja je unutar glavnog obrasca")
	}
	nova := iscrtajStranicu(t, "municipality_form.html", TerritoryPageData{Permissions: &models.UserPermissions{IsGlobalAdmin: true}})
	if strings.Contains(nova, "/api/settlements/") || strings.Contains(nova, "obrisi-opcinu") {
		t.Error("novi grad ili općina nudi naselja ili brisanje")
	}
}
