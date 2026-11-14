package web

import (
	gohtml "html"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
)

// poljaTeritorija čita ime i vrijednost svakog polja i odabranu vrijednost svakog izbora,
// onako kako bi ih preglednik poslao.
func poljaTeritorija(html string) map[string]string {
	polja := map[string]string{}
	for _, m := range regexp.MustCompile(`<input[^>]*name="([^"]+)"[^>]*value="([^"]*)"`).FindAllStringSubmatch(html, -1) {
		polja[m[1]] = gohtml.UnescapeString(m[2])
	}
	for _, m := range regexp.MustCompile(`(?s)<select[^>]*name="([^"]+)"[^>]*>(.*?)</select>`).FindAllStringSubmatch(html, -1) {
		if o := regexp.MustCompile(`<option value="([^"]*)" selected`).FindStringSubmatch(m[2]); o != nil {
			polja[m[1]] = o[1]
		}
	}
	return polja
}

// Obrasci županije i općine (7d, 7e): ista imena polja i odredišta, a obrazac
// spremljen bez izmjena šalje iste vrijednosti.
func TestObrasciTeritorijaCuvajuPolja(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	z := models.County{ID: 14, Code: "PR", Name: "Primjerska županija", Seat: "Primjerovo", Prefect: "Ivo Izmišljenić",
		AreaSqKm: 4155, Population: 305032, Email: "z@primjer.hr", Phone: "031/000-001", Website: "www.primjer.hr"}
	html := iscrtajStranicu(t, "county_form.html", TerritoryPageData{Permissions: admin, County: z, IsEdit: true})
	if !strings.Contains(html, `action="/territories/county/update"`) {
		t.Error("obrazac županije ne šalje na /territories/county/update")
	}
	zeli := map[string]string{"id": "14", "code": "PR", "name": z.Name, "seat": z.Seat, "prefect": z.Prefect, "area_sqkm": "4155",
		"population": "305032", "email": z.Email, "phone": z.Phone, "website": z.Website}
	if dobio := poljaTeritorija(html); !reflect.DeepEqual(dobio, zeli) {
		t.Errorf("županija: polja %v, želi %v", dobio, zeli)
	}

	m := models.Municipality{ID: 302, CountyID: 14, Name: "Primjerovo", Type: "OPCINA", PostalCode: "31300", AreaSqKm: 61.7, Population: 1446,
		HeadTitle: "načelnica", HeadName: "Ana Izmišljenić", Email: "o@primjer.hr", Phone: "031/000-002", Website: "www.opcina.hr"}
	html = iscrtajStranicu(t, "municipality_form.html", TerritoryPageData{Permissions: admin, Municipality: m, IsEdit: true, Counties: []models.County{z}})
	if !strings.Contains(html, `action="/territories/municipality/update"`) {
		t.Error("obrazac općine ne šalje na /territories/municipality/update")
	}
	zeli = map[string]string{"id": "302", "county_id": "14", "type": "OPCINA", "name": m.Name, "postal_code": m.PostalCode, "area_sqkm": "61,7",
		"population": "1446", "head_title": m.HeadTitle, "head_name": m.HeadName, "email": m.Email, "phone": m.Phone, "website": m.Website}
	if dobio := poljaTeritorija(html); !reflect.DeepEqual(dobio, zeli) {
		t.Errorf("općina: polja %v, želi %v", dobio, zeli)
	}
	if atof("61,7") != m.AreaSqKm {
		t.Errorf("površina 61,7 se čita kao %v", atof("61,7"))
	}
}
