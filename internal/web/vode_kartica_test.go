package web

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Kartica vodotoka po crtežu 17e, 6b: povratak na Vodotoke, službeni naziv kao
// naslov, Podaci, Napomena s „Uredi” samo za administratora, a dionice i letve
// kao retci s poveznicom ravno na karticu (ne na pretragu).
func TestKarticaVodotokaPoCrtezu(t *testing.T) {
	popovac := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000051"), Name: "Popovac", Stationing: "pkm 17,62"}
	batina := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000052"), Name: "Batina", Watercourse: "Dunav"}
	d := WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, CanEdit: true,
		Water: models.Watercourse{Code: "potok-proba", Name: "Proba", OfficialName: "potok Proba", Kind: "potok",
			FlowsInto: "Dunav", Notes: "Tok iz OpenStreetMapa."},
		Sections:     []models.Section{{Code: "B.16.2", SectorName: "Sektor B"}},
		Stations:     []models.Station{popovac},
		DodatneLetve: []models.Station{batina},
	}
	html := iscrtaj(t, "watercourse_detail.html", d)
	for _, want := range []string{
		`class="povratna-poveznica">← Vodotoci`, `data-naziv-stranice>potok Proba</h1>`, "potok · ", "ulijeva se u Dunav",
		"<h2>Podaci</h2>", "<h2>Napomena</h2>", `href="/watercourses/potok-proba/edit"`,
		`href="/sections/B.16.2"`, `href="/stations/` + popovac.ID.String() + `"`, `href="/stations/` + batina.ID.String() + `"`,
		"na drugoj vodi: Dunav", "dodatna letva vodotoka", "/karta#uredi=vodotok:potok-proba",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("kartica vodotoka nema %q", want)
		}
	}
	if strings.Contains(html, "/sections?q=") || strings.Contains(html, "/stations?q=") {
		t.Error("dionica ili letva vodi na pretragu umjesto na karticu")
	}
	d.Permissions, d.CanEdit, d.Water.Notes = &models.UserPermissions{}, false, ""
	if html = iscrtaj(t, "watercourse_detail.html", d); strings.Contains(html, "/edit") || strings.Contains(html, "<h2>Napomena</h2>") {
		t.Error("bez prava kartica nudi uređivanje ili praznu napomenu")
	}
}

// Strana letva bez stacionaže ne počinje razdjelnikom: „Mađarska”, a ne „ · Mađarska”.
func TestLetvaVodotokaBezStacionaze(t *testing.T) {
	strana := models.Station{ID: uuid.New(), Name: "Villány (HU)"}
	html := iscrtaj(t, "watercourse_detail.html", WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{},
		Water: models.Watercourse{Code: "potok-proba", Name: "Proba"}, Stations: []models.Station{strana},
	})
	if !strana.JeInozemna() || !strings.Contains(html, `<span class="redak-sporedno">Mađarska</span>`) {
		t.Errorf("redak strane letve ne glasi samo „Mađarska”")
	}
}
