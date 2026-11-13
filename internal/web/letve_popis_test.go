package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Popis letava po crtežu 17c, 4a: tablica koja je na mobitelu kartica, redak
// vodi na kartu letve, „Čeka pregled” kao oznaka upozorenja, prve tri dionice
// pa „i još N”, čipovi Sve i Čeka pregled s brojem i Nova postaja samo za
// globalnog administratora.
func TestPopisLetavaPoCrtezu(t *testing.T) {
	kota := 82.064
	batina := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000011"), Name: "Batina",
		Watercourse: "Dunav", Stationing: "rkm 1424+850", ZeroDatum: &kota,
		Prep: models.Threshold{Cm: cm(500)}, Regular: models.Threshold{Cm: cm(650)},
		SectionCodes: []string{"B.34.1", "B.34.2", "B.34.3", "B.34.4", "B.34.5"}, TelemetrijaUvoz: true}
	proba := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000012"), Name: "Proba",
		NeedsReview: true, ReviewNote: "nema praga u cm"}
	d := StationsPageData{
		CurrentUser:   &models.User{FullName: "P"},
		Permissions:   &models.UserPermissions{},
		Stations:      []models.Station{batina, proba},
		TotalStations: 40, NeedsReview: 3, SearchQuery: "Bat",
	}
	izgledPopisaLetava(&d, url.Values{"q": {"Bat"}, "stranica": {"2"}})
	html := iscrtaj(t, "stations.html", d)
	for _, want := range []string{
		`class="povratna-poveznica">← Registri`,
		`class="data-table kartice-na-mobitelu dio-tablica let-tablica"`, `data-listanje="popis"`,
		`href="/stations/` + batina.ID.String() + `">Batina</a>`,
		"82,064 <span", "500 · 650 · — · —", "B.34.1, B.34.2, B.34.3 i još 2", "telemetrija",
		`<span class="oznaka-stanja pozor" title="nema praga u cm">Čeka pregled</span>`, "vodotok nije određen",
		`href="/stations?q=Bat" aria-pressed="true">`, "Sve (40)",
		`href="/stations?q=Bat&amp;review=1" aria-pressed="false">Čeka pregled (3)`,
		`href="/karta#letve=` + batina.ID.String() + "," + proba.ID.String() + `"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na popisu nema %q", want)
		}
	}
	if strings.Contains(html, "/stations/new") || strings.Contains(html, "table-responsive") {
		t.Error("popis nudi Novu postaju bez prava ili je tablica u table-responsive")
	}

	d.Stations, d.Permissions = nil, &models.UserPermissions{IsGlobalAdmin: true}
	izgledPopisaLetava(&d, url.Values{})
	html = iscrtaj(t, "stations.html", d)
	for _, want := range []string{"Nijedna postaja ne odgovara zadanim filtrima.", `href="/stations/new"`, `<a href="/karta" class="btn btn-link">Karta</a>`} {
		if !strings.Contains(html, want) {
			t.Errorf("prazan popis nema %q", want)
		}
	}
}
