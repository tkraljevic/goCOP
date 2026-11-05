package web

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Tuđa letva (Odluke 5. 11. 2026.): gumb „Upiši očitanje” stoji i kad letva
// nije u sektoru osobe, a obrazac tada traži tko je očitao i kako je
// vrijednost dobivena. Na svojoj letvi polje ostaje neobavezno.
func TestOcitanjeNaTudjuLetvuUzIzvor(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Mohács probni", Code: "mohacs-probni"}
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{},
		Station: st, LetvaStranica: "kartica", UpisUzIzvor: true})
	if dio := dioIzbornika(t, "kartica", html); !strings.Contains(dio, "Upiši očitanje") || !strings.Contains(dio, "upis uz izvor") {
		t.Errorf("gumb za upis uz izvor: %s", dio)
	}
	povijest := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{},
		Station: &st, LetvaStranica: "ocitanja", UpisUzIzvor: true, NewURL: "/readings/new?station=" + st.ID.String()})
	if dio := dioIzbornika(t, "ocitanja", povijest); !strings.Contains(dio, `href="/readings/new?station=`+st.ID.String()+`"`) || !strings.Contains(dio, "Upiši očitanje") {
		t.Error("na stranici očitanja nema gumba za upis uz izvor")
	}

	obrazac := func(trazi bool) string {
		return iscrtaj(t, "reading_form.html", ReadingFormData{TraziIzvor: trazi, CurrentUser: &models.User{FullName: "Pero Perić"},
			Permissions: &models.UserPermissions{}, Reading: &models.Reading{}, GaugeName: st.Name, BackURL: "/readings",
			LocalValue: "2026-11-05T07:00"})
	}
	tudja := obrazac(true)
	for _, zeli := range []string{"Tko je očitao i kako ste dobili vrijednost", `placeholder="npr. vodočuvar Ivan, telefonom"`, "required>", "Letva nije u vašem sektoru"} {
		if !strings.Contains(tudja, zeli) {
			t.Errorf("obrazac za tuđu letvu nema %q", zeli)
		}
	}
	svoja := obrazac(false)
	if strings.Contains(svoja, "Tko je očitao i kako") || !strings.Contains(svoja, "Prazno znači prijavljeni korisnik.") {
		t.Error("na svojoj letvi polje Očitao ostaje neobavezno")
	}
}
