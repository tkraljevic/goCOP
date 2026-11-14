package web

import (
	"net/url"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Popis vodotoka po crtežu 17e, 6a: tablica koja je na mobitelu kartica unutar
// listanja, čipovi Sve i „S dionicom ili postajom” s brojem, a „Nije ucrtano”
// je oznaka koja vodi na Kartu (administrator ravno na crtanje te vode).
func TestPopisVodotokaPoCrtezu(t *testing.T) {
	geo := `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[18,45],[18.1,45.1]]},"properties":{}}]}`
	d := WatercoursesPageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Watercourses: []models.Watercourse{
			{Code: "rijeka-proba", Name: "Proba", OfficialName: "rijeka Proba", FlowsInto: "Dunav", SectionCount: 3, StationCount: 2, Geometry: geo},
			{Code: "kanal-proba", Name: "kanal Proba", OfficialName: "kanal Proba"},
		},
		Total: 130, Used: 62, SearchQuery: "Pro",
	}
	izgledPopisaVoda(&d, url.Values{"q": {"Pro"}, "page": {"2"}})
	html := iscrtaj(t, "watercourses.html", d)
	for _, want := range []string{
		`class="data-table kartice-na-mobitelu dio-tablica vod-tablica"`, `data-listanje="popis"`,
		`href="/watercourses/rijeka-proba">Proba</a>`, "rijeka Proba", "Dunav", "2 letve", "ucrtano",
		`<a class="oznaka-stanja pozor" href="/karta#uredi=vodotok:kanal-proba">Nije ucrtano</a>`, "vrsta nije upisana",
		`href="/watercourses?q=Pro" aria-pressed="true">`, "Sve (130)",
		`href="/watercourses?q=Pro&amp;used=1" aria-pressed="false">S dionicom ili postajom (62)`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("popis vodotoka nema %q", want)
		}
	}
	if strings.Contains(html, "table-responsive") {
		t.Error("popis je u table-responsive")
	}
	if got := kartaNeucrtaneVode(models.Watercourse{Code: "kanal-proba"}, false); !strings.HasPrefix(got, "/karta#nedostaje=") {
		t.Errorf("bez administratora „Nije ucrtano” vodi na %q", got)
	}
}
