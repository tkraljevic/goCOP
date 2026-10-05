package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Obrazac vlastitog računa (uprava sektora uređuje sebe) šalje zatečeno
// korisničko ime i uključenost, pa spremanje bez promjene prolazi; zahtjev
// koji ih ili zastavicu globalnog administratora mijenja odbija se, a
// poruka stiže korisniku.
func TestIzmjenaVlastitogRacunaPorukaUObrascu(t *testing.T) {
	o := novaOkolinaIzvana(t)
	o.mux.HandleFunc("GET /users/{id}/edit", o.usersH.ShowUserForm)
	// obrazac djelatnika otvara uprava: Ana Anić je uprava sektora P
	hash, err := o.auth.HashPassword("anina-lozinka")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica')`); err != nil {
		t.Fatal(err)
	}
	sektor := "P"
	ana := &models.User{Username: "ana", FullName: "Ana Anić", PasswordHash: hash, Email: "ana@voda.hr", IsActive: true, OrgType: models.OrgHrvatskeVode}
	if err := o.repo.CreateUser(ana, &models.Duty{Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}); err != nil {
		t.Fatal(err)
	}

	w := posaljiKaoUprava(o, ana, http.MethodGet, "/users/"+ana.ID.String()+"/edit", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("obrazac: %d", w.Code)
	}
	tijelo := w.Body.String()
	if !strings.Contains(tijelo, `name="username" class="form-control" required value="ana"`) ||
		!strings.Contains(tijelo, `<input type="hidden" name="is_active" value="1">`) || strings.Contains(tijelo, `name="is_global_admin"`) {
		t.Fatal("obrazac vlastitog računa mora poslati zatečeno ime i uključenost, bez zastavice")
	}

	obrazac := func() url.Values {
		return url.Values{"id": {ana.ID.String()}, "username": {"ana"}, "full_name": {"Ana Anić"}, "email": {"ana@voda.hr"},
			"org_type": {"HRVATSKE_VODE"}, "is_active": {"1"}}
	}
	if w := posaljiKaoUprava(o, ana, http.MethodPost, "/users/update", obrazac()); greskaPreusmjerenja(t, w) != "" {
		t.Fatalf("spremanje bez promjene: %s", w.Header().Get("Location"))
	}
	for polje, s := range map[string]struct{ vrijednost, poruka string }{
		"username":        {"ana-nova", "korisničko ime"},
		"is_active":       {"0", "isključujete"},
		"is_global_admin": {"1", "stalna uprava"},
	} {
		x := obrazac()
		x.Set(polje, s.vrijednost)
		w := posaljiKaoUprava(o, ana, http.MethodPost, "/users/update", x)
		if g := greskaPreusmjerenja(t, w); !strings.Contains(g, s.poruka) {
			t.Errorf("%s: poruka %q, očekivano da sadrži %q", polje, g, s.poruka)
		}
	}
	if u, _ := o.repo.GetUserByID(ana.ID); u.Username != "ana" || !u.IsActive || u.IsGlobalAdmin || u.FullName != "Ana Anić" {
		t.Errorf("odbijena izmjena je ipak upisana: %+v", u)
	}
}

// posaljiKaoUprava šalje zahtjev osobe s ovlastima izračunatima iz njezinih
// dužnosti, kao authMiddleware (posalji ih ne računa)
func posaljiKaoUprava(o *okolinaIzvana, u *models.User, metoda, put string, polja url.Values) *httptest.ResponseRecorder {
	o.t.Helper()
	r := httptest.NewRequest(metoda, put, strings.NewReader(polja.Encode()))
	if polja != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.RemoteAddr = "192.168.1.50:40000"
	perms, err := o.repo.GetUserPermissions(u.ID)
	if err != nil {
		o.t.Fatal(err)
	}
	c := context.WithValue(r.Context(), contextKeyUser, &perms.User)
	c = context.WithValue(c, contextKeyPerms, perms)
	c = context.WithValue(c, contextKeyRealUsr, &perms.User)
	c = context.WithValue(c, contextKeyViewing, false)
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, r.WithContext(c))
	return w
}
