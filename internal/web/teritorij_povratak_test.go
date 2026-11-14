package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"strings"
	"testing"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Teritorij po crtežu (17f): svaka od pet stranica ima povratak na roditelja umjesto
// putokaza, a h1 zadržava data-naziv-stranice.
func TestTeritorijPovratakNaRoditelja(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	zupanija := models.County{ID: 14, Name: "Primjerska županija"}
	opcina := models.Municipality{ID: 302, CountyID: 14, Name: "Primjerovo", Type: "OPCINA"}
	slucajevi := []struct {
		predlozak string
		data      any
		povratak  string
		naslov    string
	}{
		{"territories.html", TerritoriesPageData{Permissions: admin, ActiveTab: "counties"},
			`<a href="/registri" class="povratna-poveznica">← Registri</a>`, `data-naziv-stranice>Teritorij</h1>`},
		{"county_detail.html", TerritoryPageData{Permissions: admin, County: zupanija},
			`<a href="/territories" class="povratna-poveznica">← Teritorij</a>`, `data-naziv-stranice>Primjerska županija</h1>`},
		{"county_form.html", TerritoryPageData{Permissions: admin, County: zupanija, IsEdit: true},
			`<a href="/territories/counties/14" class="povratna-poveznica">← Primjerska županija</a>`, `data-naziv-stranice>Uredi županiju</h1>`},
		{"county_form.html", TerritoryPageData{Permissions: admin},
			`<a href="/territories" class="povratna-poveznica">← Teritorij</a>`, `data-naziv-stranice>Nova županija</h1>`},
		{"municipality_detail.html", TerritoryPageData{Permissions: admin, County: zupanija, Municipality: opcina},
			`<a href="/territories/counties/14" class="povratna-poveznica">← Primjerska županija</a>`, `data-naziv-stranice>Općina Primjerovo</h1>`},
		{"municipality_form.html", TerritoryPageData{Permissions: admin, Municipality: opcina, IsEdit: true},
			`<a href="/territories/municipalities/302" class="povratna-poveznica">← Općina Primjerovo</a>`, `data-naziv-stranice>Uredi grad ili općinu</h1>`},
		{"municipality_form.html", TerritoryPageData{Permissions: admin},
			`<a href="/territories?tab=municipalities" class="povratna-poveznica">← Teritorij</a>`, `data-naziv-stranice>Novi grad ili općina</h1>`},
	}
	for _, s := range slucajevi {
		html := iscrtajStranicu(t, s.predlozak, s.data)
		for _, mora := range []string{s.povratak, s.naslov} {
			if !strings.Contains(html, mora) {
				t.Errorf("%s: nema %q", s.predlozak, mora)
			}
		}
		if strings.Contains(html, `class="breadcrumb"`) || strings.Contains(html, "Teritorijalne jedinice") {
			t.Errorf("%s: ostao je stari putokaz", s.predlozak)
		}
	}
}

// iscrtajStranicu iscrta stranicu kroz base.html, kao rukovatelj; territories.html nema
// vlastiti omotač, pa iscrtaj po imenu stranice za nju daje prazno.
func iscrtajStranicu(t *testing.T, stranica string, data any) string {
	t.Helper()
	predlosci, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(predlosci, DijeloviPredloska(stranica)...)
	if err != nil {
		t.Fatalf("%s: %v", stranica, err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		t.Fatalf("%s: %v", stranica, err)
	}
	return buf.String()
}
