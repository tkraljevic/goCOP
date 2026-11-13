package web

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Obrazac postaje po crtežu 17c, 4b: ploče Osnovno, Pragovi, Kota nule i
// Zabilježeni ekstremi. Vodotok iz registra stoji u Osnovnom, ali pripada
// zasebnom obrascu; glavni obrazac i dalje šalje svako polje koje rukovatelj
// prepisuje, jer polje koje izostane pri spremanju se obriše.
func TestObrazacPostajePoCrtezu(t *testing.T) {
	st := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000021"), Name: "Batina",
		SectionCodes: []string{"B.34.1", "B.34.2"}}
	html := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, IsEdit: true, CanEdit: true,
		WaterRegistry: []models.Watercourse{{Code: "rijeka-dunav", OfficialName: "rijeka Dunav"}},
	})
	for _, want := range []string{
		`href="/stations/` + st.ID.String() + `" class="povratna-poveznica">← Batina`,
		"Uredi vodomjernu postaju", "<h3>Osnovno</h3>",
		`name="watercourse_code" class="form-control" form="vodotok-obrazac"`,
		`<button type="submit" class="btn btn-secondary" form="vodotok-obrazac">Spremi vodotok</button>`,
		`<form id="vodotok-obrazac" method="POST" action="/api/stations/watercourse"`,
		`href="/sections/B.34.1"`, "Letva se dodaje dionici u obrascu dionice.",
		"Pripremno stanje (P) · cm",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac nema %q", want)
		}
	}
	glavni := izmedju(html, `id="station-form"`, "</form>")
	if strings.Contains(glavni, `name="watercourse_code" class="form-control">`) {
		t.Error("vodotok iz registra šalje se s glavnim obrascem")
	}
	for _, polje := range []string{"name", "stationing", "watercourse", "water_area", "prep", "regular", "emergency", "state",
		"zero_datum_new", "zero_datum", "zero_datum_baltic", "zero_datum_baltic_system", "zero_datum_baltic_source",
		"zero_datum_source", "zero_datum_method", "zero_datum_survey_date", "zero_datum_document_date", "record", "extremes",
		"latitude", "longitude", "notes", "javni_url", "javni_uvoz", "telemetrija_uvoz",
		"source_name", "review_note", "needs_review", "obrazac", "code", "zero_datum_system", "zero_datum_new_system"} {
		if !strings.Contains(glavni, `name="`+polje+`"`) {
			t.Errorf("glavni obrazac više ne šalje polje %s", polje)
		}
	}
}

// Donji dio obrasca: Postavi na Karti vodi na uređivanje letve na Karti, a
// sklopke su prekidači koji ostaju kvačice s istim imenom i vrijednošću.
func TestObrazacPostajeDonjiDio(t *testing.T) {
	st := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000022"), Name: "Batina",
		NeedsReview: true, JavniUvoz: true}
	html := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, IsEdit: true, CanEdit: true,
	})
	for _, want := range []string{
		`href="/karta#uredi=letva:` + st.ID.String() + `">Postavi na Karti</a>`,
		"<h3>Izvor očitanja</h3>", "<h3>Podrijetlo i provjera</h3>",
		`class="prekidac" role="switch" name="javni_uvoz" value="1" checked`,
		`class="prekidac" role="switch" name="needs_review" value="1" checked`,
		`class="prekidac" role="switch" name="telemetrija_uvoz" value="1"`,
		"lob-sklopka lob-zasivljeno", "Letva bez praga u centimetrima ostaje na pregledu",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac nema %q", want)
		}
	}
	if strings.Contains(izmedju(html, `class="lob-stranica"`, "</form>"), `style="`) {
		t.Error("u obrascu postaje ostao je stil u retku")
	}
}

// Obrazac historijata po crtežu 4c: povratak na historijat, tri ploče s brojem
// u naslovu i praznim stanjem, trenutna kota nule i ista skrivena polja.
func TestObrazacHistorijataPoCrtezu(t *testing.T) {
	kota := 80.45
	st := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000023"), Name: "Batina", ZeroDatum: &kota,
		ReturnLevels: []models.StationReturnLevel{{Years: 100}}}
	html := iscrtaj(t, "station_history_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, CanEdit: true, IsEdit: true,
	})
	for _, want := range []string{
		`href="/stations/` + st.ID.String() + `/historijat" class="povratna-poveznica">← Batina · historijat`,
		"Uredi historijat", "Povratni vodostaji (1)", "Promjene kote nule kroz vrijeme (0)", "Ograde uz nizove (0)",
		"Kota nule nije mijenjana.", "Za ovu letvu nema ograda.", "Sada: HVRS71 nije upisana · Trst 80,450 m",
		`name="return_levels"`, `name="zero_datum_history"`, `name="ograde_niza"`, `name="obrazac" value="historijat"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac historijata nema %q", want)
		}
	}
}
