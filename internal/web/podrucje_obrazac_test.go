package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Obrazac branjenog područja po crtežu (17a, 2d) šalje ista polja kao prije, svako točno
// jednom, a ono što ispiše areaFromForm pročita nepromijenjeno: koordinate se ne
// zaokružuju, pa spremanje bez izmjena ne pomiče točku. Prekidač „izravno sektoru”
// samo skriva polja ispostave, a Obriši je zaseban obrazac na istoj adresi.
func TestObrazacPodrucjaPoCrtezu(t *testing.T) {
	podrucje := models.Area{ID: 16, SectorID: "P", Name: "Mali sliv Primjerica", VgiName: "VGI Primjerica, Primjerovo", VgiPhone: "031/000-001",
		Subcenter: "Podcentar Primjerovo", DirectToSector: true, Latitude: 45.5548793, Longitude: 18.6953685}
	stranica := iscrtaj(t, "area_form.html", OrgPageData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Terms: models.Terms(), Area: podrucje, IsEdit: true, SectorOptions: []models.Sector{{ID: "P", Name: "Sektor P", Level: 2}}})

	glavni := stranica[strings.Index(stranica, `action="/organizacija/podrucja/save"`):]
	glavni = glavni[:strings.Index(glavni, "</form>")]
	forma := url.Values{}
	polje := regexp.MustCompile(`<input[^>]*name="([a-z_]+)"[^>]*>`)
	vrijednost := regexp.MustCompile(`value="([^"]*)"`)
	broj := map[string]int{}
	for _, m := range polje.FindAllStringSubmatch(glavni, -1) {
		broj[m[1]]++
		if m[1] == "direct_to_sector" && !strings.Contains(m[0], "checked") {
			continue
		}
		if v := vrijednost.FindStringSubmatch(m[0]); v != nil {
			forma.Set(m[1], html.UnescapeString(v[1]))
		}
	}
	if strings.Contains(glavni, `<option value="P" selected>`) {
		forma.Set("sector_id", "P")
		broj["sector_id"]++
	}
	for _, p := range []string{"is_new", "id", "sector_id", "name", "vgi_name", "vgi_phone", "subcenter", "latitude", "longitude", "direct_to_sector"} {
		if broj[p] != 1 {
			t.Errorf("polje %s %d puta", p, broj[p])
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/organizacija/podrucja/save", strings.NewReader(forma.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if dobio := *areaFromForm(r); dobio != podrucje {
		t.Errorf("spremanje bez izmjena mijenja područje:\n dobio %+v\n  želi %+v", dobio, podrucje)
	}

	for _, mora := range []string{
		`<div id="afo-ispostava" hidden>`, `name="is_new" value="0"`,
		`<form id="obrisi-podrucje" method="POST" action="/organizacija/podrucja/delete" hidden`, `form="obrisi-podrucje"`,
		`id="geokod"`, `id="geokod-nalaz"`, `href="/firme"`,
	} {
		if !strings.Contains(stranica, mora) {
			t.Errorf("obrazac nema %q", mora)
		}
	}

	novo := iscrtaj(t, "area_form.html", OrgPageData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Terms: models.Terms(), Area: models.Area{SectorID: "P"}, SectorOptions: []models.Sector{{ID: "P", Name: "Sektor P", Level: 2}}})
	if strings.Contains(novo, "obrisi-podrucje") || strings.Contains(novo, `<div id="afo-ispostava" hidden>`) || !strings.Contains(novo, `<option value="P" selected>`) {
		t.Error("novo područje: brisanje, skrivena ispostava ili sektor iz adrese")
	}
}
