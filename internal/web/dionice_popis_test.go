package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Popis dionica (17b, 3a): čipovi sektora i područja čuvaju pretragu, Excel
// ide po istim filtrima, dionice su tablica s brojem poddionica, nasipa,
// letava i objekata; dionica bez nasipa to kaže.
func TestPopisDionicaPoCrtezu(t *testing.T) {
	duljina, nasip := 19.565, 31.726
	q := url.Values{"sector": {"B"}, "area": {"16"}, "q": {"Travnik"}, "success": {"x"}}
	d := SectionsPageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, CanCreateAny: true,
		Sectors:        []models.Sector{{ID: "B", Name: "Sektor B"}, {ID: "DIREKCIJA", Name: "Direkcija"}},
		Areas:          []models.Area{{ID: 15, SectorID: "B"}, {ID: 16, SectorID: "B"}},
		SelectedSector: "B", SelectedArea: 16, SearchQuery: "Travnik",
		Sections: []SectionItemView{
			{Section: models.Section{Code: "B.16.1", AreaID: 16, SectorID: "B", WatercourseName: "kanal Travnik", LengthKm: &duljina, EmbankmentKm: &nasip,
				Parts: []models.SectionPart{{Seq: 1, Bank: "LD", Extent: "Ušće u p. Karašicu", StationIDs: []string{"a", "b"},
					Embankments: []models.PartEmbankment{{Name: "Nasip 1"}}}, {Seq: 2}}}},
			{Section: models.Section{Code: "B.16.9", AreaID: 16, SectorID: "B", Parts: []models.SectionPart{{Seq: 1}}}},
		},
	}
	izgledPopisaDionica(&d, q)
	if len(d.CipoviSektora) != 2 || d.CipoviSektora[0].Naziv != "Svi" || !d.CipoviSektora[1].Odabran || d.CipoviSektora[1].URL != "/sections?q=Travnik&sector=B" {
		t.Errorf("čipovi sektora: %+v", d.CipoviSektora)
	}
	if len(d.CipoviPodrucja) != 3 || d.CipoviPodrucja[0].URL != "/sections?q=Travnik&sector=B" || !d.CipoviPodrucja[2].Odabran ||
		d.CipoviPodrucja[2].URL != "/sections?area=16&q=Travnik&sector=B" {
		t.Errorf("čipovi područja: %+v", d.CipoviPodrucja)
	}
	if d.IzvozURL != "/sections/dionice.xlsx?area=16&q=Travnik&sector=B" {
		t.Errorf("izvoz: %q", d.IzvozURL)
	}
	html := iscrtaj(t, "sections.html", d)
	for _, zeli := range []string{`<a href="/registri" class="povratna-poveznica">← Registri</a>`, `href="/sections/dionice.xlsx?area=16&amp;q=Travnik&amp;sector=B"`,
		`href="/sections/new"`, `<a class="dl-poveznica" href="/sections/B.16.1">B.16.1</a>`, "kanal Travnik, obje obale", "Ušće u p. Karašicu",
		"i još 1", "19,565 km", "31,726 km", "1 nasip", "2 letve", "Bez nasipa", `name="q" value="Travnik"`} {
		if !strings.Contains(html, zeli) {
			t.Errorf("popis dionica nema %q", zeli)
		}
	}
	// „465” se traži kao samostalan broj: otisak verzije u adresi CSS-a
	// (?v=…) zna sadržavati iste znamenke.
	if regexp.MustCompile(`\b465\b`).MatchString(html) || strings.Contains(html, "breadcrumb") || strings.Contains(html, "section-card") {
		t.Error("popis dionica ima stari izgled ili upisan broj 465")
	}
}
