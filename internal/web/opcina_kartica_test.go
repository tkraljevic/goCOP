package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Kartica grada ili općine po crtežu (7c): naselja kao tablica (za administratora s
// poljima), Podaci desno, prve tri službe županije i „i još N”; služba vezana baš za
// općinu vidi se uvijek.
func TestKarticaOpcinePoCrtezu(t *testing.T) {
	var sluzbe []models.Sluzba
	for i, v := range []string{models.SluzbaCivilnaZastita, models.SluzbaPrevencija, models.SluzbaCentar112, models.SluzbaPolicija, models.SluzbaLuckaKapetanija} {
		sluzbe = append(sluzbe, models.Sluzba{ID: string(rune('a' + i)), Vrsta: v, Naziv: "Služba " + string(rune('A'+i))})
	}
	sluzbe = append(sluzbe, models.Sluzba{ID: "pp", Vrsta: models.SluzbaPolicijskaPost, Naziv: "PP Primjerovo", MunicipalityID: 302, MunicipalityName: "Primjerovo"})
	data := func(admin bool) TerritoryPageData {
		return TerritoryPageData{
			Permissions:  &models.UserPermissions{IsGlobalAdmin: admin},
			County:       models.County{ID: 14, Name: "Primjerska županija"},
			Municipality: models.Municipality{ID: 302, CountyID: 14, Name: "Primjerovo", Type: "OPCINA", PostalCode: "31300", AreaSqKm: 61.7, Population: 1446, HeadTitle: "načelnica", HeadName: "Ana Izmišljenić"},
			Settlements:  []models.Settlement{{ID: 4801, Name: "Branjin Vrh", PostalCode: "31300", Population: 812}},
			Sluzbe:       sluzbe,
		}
	}
	html := iscrtajStranicu(t, "municipality_detail.html", data(false))
	for _, mora := range []string{
		`Općina · Primjerska županija · 31300 · 61,7 km² · 1.446 stanovnika`,
		`<td>Branjin Vrh</td>`, `<td data-stupac="Stanovnika">812</td>`,
		`<dt>Čelna osoba</dt><dd>načelnica Ana Izmišljenić</dd>`,
		`Služba A`, `Služba C`, `PP Primjerovo`, `i još 2 na kartici županije`,
	} {
		if !strings.Contains(html, mora) {
			t.Errorf("bez prava: nema %q", mora)
		}
	}
	for _, ne := range []string{`Služba D`, `/api/settlements/`, `/edit"`, `obrisi-opcinu`} {
		if strings.Contains(html, ne) {
			t.Errorf("bez prava: vidi %q", ne)
		}
	}
	html = iscrtajStranicu(t, "municipality_detail.html", data(true))
	for _, mora := range []string{`href="/territories/municipalities/302/edit"`, `href="/territories/municipalities/302/edit#naselja"`} {
		if !strings.Contains(html, mora) {
			t.Errorf("administrator: nema %q", mora)
		}
	}
	// naselja i brisanje općine su u obrascu, ne na kartici
	if strings.Contains(html, "/api/settlements/") || strings.Contains(html, "/territories/municipality/delete") {
		t.Error("administrator: kartica i dalje uređuje naselja ili briše općinu")
	}
}
