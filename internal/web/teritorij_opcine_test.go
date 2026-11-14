package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Popis gradova i općina: tablica bez čelnika, e-pošte i telefona; filtri, pager i
// brisanje s potvrdom ostaju, a Uredi i Obriši vidi samo globalni administrator.
func TestTeritorijPopisOpcinaKaoTablica(t *testing.T) {
	opcine := []models.Municipality{{ID: 302, CountyID: 14, CountyName: "Primjerska županija", Name: "Primjerovo", Type: "OPCINA",
		PostalCode: "31300", AreaSqKm: 61.7, Population: 1446, HeadTitle: "načelnica", HeadName: "Iva Izmišljenić", Email: "opcina@primjer.hr", Phone: "031/000-003"}}
	for _, admin := range []bool{true, false} {
		html := iscrtajStranicu(t, "territories.html", TerritoriesPageData{
			Permissions: &models.UserPermissions{IsGlobalAdmin: admin}, ActiveTab: "municipalities",
			Municipalities: opcine, Pager: Pager{Total: 1}, SearchQuery: "Prim",
		})
		for _, mora := range []string{
			`data-listanje="popis"`, `<a href="/territories/municipalities/302" class="dl-poveznica">Primjerovo</a>`,
			`<td data-stupac="Vrsta">općina</td>`, `<a href="/territories/counties/14">Primjerska županija</a>`,
			`<td data-stupac="Poštanski broj">31300</td>`, `<td data-stupac="km²" class="text-right">61,7</td>`,
			`name="county_id"`, `name="type"`, `name="q" value="Prim"`, `href="/territories?tab=municipalities" class="btn btn-link">Poništi</a>`,
		} {
			if !strings.Contains(html, mora) {
				t.Errorf("admin=%v: nema %q", admin, mora)
			}
		}
		for _, s := range []string{`href="/territories/municipalities/302/edit"`, `action="/territories/municipality/delete"`, `name="county_id" value="14"`} {
			if strings.Contains(html, s) != admin {
				t.Errorf("admin=%v: %q vidljivo %v", admin, s, !admin)
			}
		}
		for _, osobno := range []string{"Izmišljenić", "opcina@primjer.hr", "031/000-003"} {
			if strings.Contains(html, osobno) {
				t.Errorf("admin=%v: popis pokazuje %q", admin, osobno)
			}
		}
	}
}
