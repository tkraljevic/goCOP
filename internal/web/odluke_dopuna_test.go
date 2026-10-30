package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Odluke (30. 10. 2026.), 18: obrazac za dodavanje dužnosti kreće bez
// unaprijed odabrane uloge, a uloga je obavezna. Uprava područja u njemu ne
// vidi upravne uloge svoje razine (16). Pri izmjeni stoji dosadašnja uloga,
// i kad je osoba koja uređuje ne bi smjela dati.
func TestObrazacDuznostiBezZadaneUloge(t *testing.T) {
	uprava := &models.UserPermissions{AdminAreas: map[int]bool{16: true}}
	osoba := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	obrazac := func(duty *models.Duty) string {
		var dosad models.Role
		if duty != nil {
			dosad = duty.Role
		}
		return iscrtaj(t, "duty_form.html", UserPageData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: uprava,
			User: osoba, Duty: duty, Roles: roleOptionsZa(uprava, dosad)})
	}

	html := obrazac(nil)
	if strings.Contains(html, " selected>") {
		t.Error("u novom zaduženju uloga je unaprijed odabrana")
	}
	if !strings.Contains(html, `<option value="">— odaberi ulogu —</option>`) || !strings.Contains(html, `name="role" class="form-control" required`) {
		t.Error("obrazac nema praznu, obaveznu ulogu")
	}
	if !strings.Contains(html, `value="WATER_GUARD"`) || strings.Contains(html, `value="AREA_LEADER"`) || strings.Contains(html, `value="AREA_DEPUTY"`) {
		t.Error("uprava područja vidi u obrascu uloge koje ne smije dati, ili ne vidi vodočuvara")
	}

	podrucje := 16
	html = obrazac(&models.Duty{ID: uuid.New(), Role: models.RoleAreaDeputy, AreaID: &podrucje, IsActive: true})
	if !strings.Contains(html, `value="AREA_DEPUTY" selected>`) {
		t.Error("pri izmjeni nema dosadašnje uloge")
	}
}

// Slanje obrasca bez uloge vraća na obrazac s porukom, a dužnost se ne
// dodaje
func TestDodavanjeDuznostiBezUloge(t *testing.T) {
	h := NewUsersHandler(nil, nil)
	osoba := uuid.New()
	r := httptest.NewRequest(http.MethodPost, "/users/duty/add", strings.NewReader(url.Values{"user_id": {osoba.String()}, "role": {""}, "area_id": {"16"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.HandleAddDuty(w, r)
	kamo, _ := url.QueryUnescape(w.Header().Get("Location"))
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(kamo, "/users/"+osoba.String()+"/duties/new?") || !strings.Contains(kamo, "Zaduženje nije dodano: odaberite ulogu.") {
		t.Errorf("slanje bez uloge: %d %s", w.Code, kamo)
	}
}

// Odluke (30. 10. 2026.), 20: naziv sektora na popisu organizacije vodi na
// stranicu sektora, a stranica sektora ima poveznicu na njegove dionice
func TestNazivSektoraVodiNaStranicuSektora(t *testing.T) {
	html := iscrtaj(t, "organizacija.html", OrgPageData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: &models.UserPermissions{},
		Terms: models.Terms(), Sectors: []SectorView{{Sector: models.Sector{ID: "P", Name: "Sektor P", Level: 2}}}})
	if !strings.Contains(html, `<a href="/organizacija/sektori/P" class="c-primary fw-600">Sektor P</a>`) {
		t.Error("naziv sektora ne vodi na stranicu sektora")
	}
	if strings.Contains(html, `href="/sections?sector=P"`) {
		t.Error("popis organizacije i dalje vodi na Dionice")
	}

	html = iscrtaj(t, "sektor.html", SektorData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: &models.UserPermissions{},
		Sektor: models.Sector{ID: "P", Name: "Sektor P", Level: 2}})
	if !strings.Contains(html, `href="/sections?sector=P"`) || !strings.Contains(html, "Dionice sektora") {
		t.Error("stranica sektora nema poveznice Dionice sektora")
	}
}
