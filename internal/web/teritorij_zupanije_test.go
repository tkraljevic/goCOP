package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Popis županija po crtežu (7a): tablica koja je na mobitelu kartica, bez imena
// župana; izvoz CSV-a vide svi, a uvoz i nove zapise samo globalni administrator.
func TestTeritorijPopisZupanijaKaoTablica(t *testing.T) {
	zupanije := []models.County{{ID: 14, Code: "OB", Name: "Primjerska županija", Seat: "Primjerovo", Prefect: "Ivo Izmišljenić", AreaSqKm: 4155, Population: 305032}}
	samoAdmin := []string{`href="/territories/counties/new"`, `action="/territories/uvoz"`, `href="/territories/counties/14/edit"`}
	for _, admin := range []bool{true, false} {
		html := iscrtajStranicu(t, "territories.html", TerritoriesPageData{
			Permissions: &models.UserPermissions{IsGlobalAdmin: admin}, ActiveTab: "counties",
			Counties: zupanije, TotalCounties: 1, BrojSluzbi: map[int]int{14: 3},
		})
		for _, mora := range []string{
			`<table class="data-table kartice-na-mobitelu dio-tablica ter-tablica">`,
			`<th>Oznaka</th>`, `<th>Sjedište</th>`, `<td data-stupac="Sjedište">Primjerovo</td>`,
			`<td data-stupac="km²" class="text-right">4.155</td>`, `<td data-stupac="Službe">3</td>`,
			`href="/territories?tab=municipalities&county_id=14"`,
			`href="/territories/zupanije.csv"`, `href="/territories/gradovi-i-opcine.csv"`, `href="/territories/naselja.csv"`,
			`placeholder="Traži: mjesto ili poštanski broj"`,
		} {
			if !strings.Contains(html, mora) {
				t.Errorf("admin=%v: nema %q", admin, mora)
			}
		}
		for _, s := range samoAdmin {
			if strings.Contains(html, s) != admin {
				t.Errorf("admin=%v: %q vidljivo %v", admin, s, !admin)
			}
		}
		if strings.Contains(html, "Izmišljenić") || strings.Contains(html, "table-responsive") {
			t.Errorf("admin=%v: popis pokazuje župana ili je tablica u table-responsive", admin)
		}
	}
}
