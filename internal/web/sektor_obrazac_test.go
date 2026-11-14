package web

import (
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Obrazac sektora po crtežu (17a, 2c) šalje ista polja kao prije (sectorFromForm), svako
// točno jednom; na izmjeni je oznaka skrivena i onemogućena, a Obriši je zaseban obrazac
// na istoj adresi. Na izmjeni krovne jedinice odabrana je razina 1, pa spremanje ne
// spušta Direkciju na razinu 2.
func TestObrazacSektoraPoCrtezu(t *testing.T) {
	ime := regexp.MustCompile(`name="([a-z_]+)"`)
	polja := func(html string) map[string]int {
		n := map[string]int{}
		glavni := html[strings.Index(html, `action="/organizacija/sektori/save"`):]
		glavni = glavni[:strings.Index(glavni, "</form>")]
		for _, m := range ime.FindAllStringSubmatch(glavni, -1) {
			n[m[1]]++
		}
		return n
	}
	perms := &models.UserPermissions{IsGlobalAdmin: true}
	sektor := models.Sector{ID: "P", Name: "Sektor P — Primjerica", Level: 2, VgoName: "VGO Primjerovo", Email: "cop@primjer.hr"}

	html := iscrtaj(t, "sector_form.html", OrgPageData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: perms, Terms: models.Terms(), Sector: sektor, IsEdit: true})
	n := polja(html)
	for _, p := range []string{"is_new", "level", "id", "name", "vgo_name", "vgo_phone", "center_cop", "address", "phone", "email"} {
		if n[p] != 1 {
			t.Errorf("izmjena: polje %s %d puta", p, n[p])
		}
	}
	for _, mora := range []string{
		`name="is_new" value="0"`, `<input type="hidden" name="id" value="P">`, `value="cop@primjer.hr"`,
		`<form id="obrisi-sektor" method="POST" action="/organizacija/sektori/delete" hidden`, `form="obrisi-sektor"`,
		`href="/organizacija/sektori/P" class="povratna-poveznica"`, `<option value="2" selected>`,
	} {
		if !strings.Contains(html, mora) {
			t.Errorf("izmjena: nema %q", mora)
		}
	}

	html = iscrtaj(t, "sector_form.html", OrgPageData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: perms, Terms: models.Terms(),
		Sector: models.Sector{ID: "DIREKCIJA", Name: "Direkcija, Zagreb", Level: 1}, IsEdit: true})
	if !strings.Contains(html, `<option value="1" selected>`) || strings.Contains(html, `<option value="2" selected>`) {
		t.Error("krovna jedinica: razina 1 nije odabrana")
	}

	html = iscrtaj(t, "sector_form.html", OrgPageData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: perms, Terms: models.Terms()})
	n = polja(html)
	if n["id"] != 1 || n["is_new"] != 1 || !strings.Contains(html, `name="is_new" value="1"`) || strings.Contains(html, "obrisi-sektor") {
		t.Errorf("upis: krivo polje oznake, is_new ili brisanje: %v", n)
	}
}
