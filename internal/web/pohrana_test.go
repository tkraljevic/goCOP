package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Rok čuvanja fotografija s terena stoji na Održavanju baze, u dijelu
// „Pohrana” (Odluke (30. 10. 2026.)): administrator ga ondje mijenja, ostali
// ga samo vide, a stranica Opcije ga više ne nudi.
func TestPohranaNaOdrzavanjuBaze(t *testing.T) {
	baza := predlozakStranice(t, "baza.html")
	iscrtaj := func(perms *models.UserPermissions, o models.Opcije) string {
		t.Helper()
		var b bytes.Buffer
		if err := baza.ExecuteTemplate(&b, "baza.html", DBMaintPageData{CurrentUser: adminIvic, Permissions: perms, Opcije: o}); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	admin := iscrtaj(&models.UserPermissions{IsGlobalAdmin: true}, models.Opcije{CuvanjeSlikaDana: 30})
	for _, mora := range []string{`id="pohrana"`, `action="/administracija/baza/pohrana"`, `name="cuvanje_slika_dana"`, `value="30"`, `name="slike_odmah"`} {
		if !strings.Contains(admin, mora) {
			t.Errorf("Pohrana administratoru nema %q", mora)
		}
	}
	ostali := iscrtaj(&models.UserPermissions{}, models.Opcije{})
	if strings.Contains(ostali, `action="/administracija/baza/pohrana"`) || !strings.Contains(ostali, "Izvorne slike čuvaju se <b>180</b> dana") {
		t.Error("Pohrana bez prava mijenjanja mora samo pokazati rok")
	}
	if !strings.Contains(iscrtaj(&models.UserPermissions{}, models.Opcije{SlikeOdmah: true}), "brišu se odmah po objavi") {
		t.Error("Pohrana ne kaže da se slike brišu odmah")
	}

	var opcije bytes.Buffer
	if err := predlozakStranice(t, "administracija_opcije.html").ExecuteTemplate(&opcije, "administracija_opcije.html",
		OpcijeData{CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: true}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(opcije.String(), "cuvanje_slika_dana") || !strings.Contains(opcije.String(), `href="/administracija/baza#pohrana"`) {
		t.Error("Opcije još nude rok čuvanja fotografija ili ne vode na Pohranu")
	}
}

// Spremanje Pohrane: bez prava ili bez servisa akata ništa se ne sprema
func TestPohranaOdbijaBezPrava(t *testing.T) {
	posalji := func(h *DBMaintHandler, perms *models.UserPermissions) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/administracija/baza/pohrana", strings.NewReader(url.Values{"cuvanje_slika_dana": {"20"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, perms))
		w := httptest.NewRecorder()
		h.HandlePohrana(w, r)
		return mustUnescape(w.Header().Get("Location"))
	}
	if loc := posalji(&DBMaintHandler{}, &models.UserPermissions{IsGlobalAdmin: true}); !strings.HasPrefix(loc, "/administracija/baza?error=") || strings.Contains(loc, "#") {
		t.Errorf("bez spremanja opcija: %s", loc)
	}
	spremljeno := false
	h := &DBMaintHandler{}
	h.SetIzmjenaOpcija(func(_ context.Context, p *models.UserPermissions, izmjena func(*models.Opcije)) error {
		if p == nil || !p.IsGlobalAdmin {
			return service.ErrUnauthorized
		}
		spremljeno = true
		return nil
	})
	if loc := posalji(h, &models.UserPermissions{AdminSectors: map[string]bool{"B": true}}); !strings.Contains(loc, "error=Rok čuvanja fotografija nije spremljen") || spremljeno {
		t.Errorf("uprava sektora: %s", loc)
	}
}

// Održavanje baze čita opće prekidače; bez izvora sve je isključeno
func TestOdrzavanjeBazeCitaOpcije(t *testing.T) {
	h := &DBMaintHandler{}
	if h.citajOpcije(context.Background()) != (models.Opcije{}) || h.brisanjePovijesti(context.Background()) {
		t.Error("bez izvora opcije moraju biti isključene")
	}
	h.SetOpcije(func(context.Context) models.Opcije {
		return models.Opcije{CuvanjeSlikaDana: 20, BrisanjePovijestiVerzija: true}
	})
	r := httptest.NewRequest(http.MethodGet, "/administracija/baza", nil)
	if d := h.pageData(r); d.Opcije.CuvanjeSlika() != 20 || !h.brisanjePovijesti(r.Context()) {
		t.Errorf("stranica nema opcija: %+v", d.Opcije)
	}
}

// Poslužitelj sprema Pohranu kroz servis akata; dok ga nema, ništa se ne
// sprema, a uz njega se mijenja samo zadano polje
func TestPosluziteljIzmjenjujeOpcije(t *testing.T) {
	bez := &Server{}
	if err := bez.izmijeniOpcije(t.Context(), &models.UserPermissions{IsGlobalAdmin: true}, func(*models.Opcije) {}); err == nil {
		t.Error("bez servisa akata opcije su spremljene")
	}
	o := novaOkolinaSastavljaca(t)
	s := &Server{akti: o.akti}
	if err := s.izmijeniOpcije(t.Context(), &models.UserPermissions{IsGlobalAdmin: true}, func(op *models.Opcije) { op.CuvanjeSlikaDana = 20 }); err != nil {
		t.Fatal(err)
	}
	if got := o.akti.Opcije(t.Context()).CuvanjeSlikaDana; got != 20 {
		t.Errorf("rok čuvanja nakon spremanja: %d", got)
	}
}
