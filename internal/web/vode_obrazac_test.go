package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Obrazac vodnog tijela po crtežu 17e, 6c: sva polja u pločama, „Uredi na
// Karti” vodi na Kartu, a Obriši je zaseban obrazac samo za vodu bez dionica
// i letava.
func TestObrazacVodePoCrtezu(t *testing.T) {
	polja := []string{"official_name", "name", "wiki_slug", "kind", "category", "source", "mouth", "flows_into",
		"length_km", "basin_km2", "avg_flow_m3s", "notes", "napomena_toka", "extra_station_ids", "link_station_ids", "code", "subcategory", "red"}
	d := WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, CanEdit: true,
		Water: models.Watercourse{Code: "potok-proba", Name: "Proba", OfficialName: "potok Proba"}, IsEdit: true, NapomenaTokaUredljiva: true,
	}
	html := iscrtaj(t, "watercourse_form.html", d)
	glavni := izmedju(html, `<form class="form-page form-wide"`, "</form>")
	for _, p := range polja {
		if n := strings.Count(glavni, `name="`+p+`"`); n != 1 {
			t.Errorf("polje %s je u obrascu %d puta", p, n)
		}
	}
	for _, want := range []string{`class="povratna-poveznica">← Proba`, "Uredi vodno tijelo", "<h3>Naziv i vrsta</h3>", "<h3>Tok</h3>",
		"<h3>Brojke</h3>", "<h3>Na karti i letve</h3>", `href="/karta#uredi=vodotok:potok-proba"`,
		`<form id="obrisi-vodu" method="POST" action="/api/watercourses/delete"`, `form="obrisi-vodu"`} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac vode nema %q", want)
		}
	}
	d.Water.SectionCount = 2
	if html = iscrtaj(t, "watercourse_form.html", d); strings.Contains(html, "obrisi-vodu") {
		t.Error("voda s dionicama nudi brisanje")
	}
}

// Red i skupina u obrascu: zapis iz uvoza sa skupinom „kanal II. reda” ima
// odabran II. red i zadržava svoju skupinu uz skupine iz ugovora.
func TestObrazacVodeRedISkupina(t *testing.T) {
	d := WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, CanEdit: true,
		Water: models.Watercourse{Code: "kanal-proba", Name: "Proba", OfficialName: "kanal Proba", Subcategory: "kanal II. reda"}, IsEdit: true,
	}
	html := iscrtaj(t, "watercourse_form.html", d)
	for _, want := range []string{`<option value="II" selected>II. red</option>`, `<option value="IV" >IV. red</option>`,
		`<option value="kanal II. reda" selected>kanal II. reda</option>`, `<option value="bujični tokovi" >bujični tokovi</option>`,
		"Pravna kategorija (samo I. red)"} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac nema %q", want)
		}
	}
	d.Water.Subcategory = "vodotoci"
	if n := len(d.SkupineObrasca()); n != len(models.SkupineVoda) {
		t.Errorf("skupina iz ugovora se ponavlja: %d", n)
	}
}
