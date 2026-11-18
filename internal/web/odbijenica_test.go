package web

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/models"
)

// zahtjevOdbijenice je zahtjev prijavljenog djelatnika koji vidi zadane module
func zahtjevOdbijenice(put string, zaglavlja map[string]string, mods models.Visibility) *http.Request {
	r := httptest.NewRequest(http.MethodGet, put, nil)
	for k, v := range zaglavlja {
		r.Header.Set(k, v)
	}
	u := &models.User{Username: "ana", FullName: "Ana Primjer", IsActive: true}
	ctx := context.WithValue(r.Context(), contextKeyUser, u)
	ctx = context.WithValue(ctx, contextKeyRealUsr, u)
	ctx = context.WithValue(ctx, contextKeyPerms, &models.UserPermissions{})
	ctx = context.WithValue(ctx, contextKeyModules, mods)
	return r.WithContext(ctx)
}

func TestOdbijenicaJeStranicaSIzbornikom(t *testing.T) {
	s := &Server{templates: map[string]*template.Template{"odbijenica.html": predlozakStranice(t, "odbijenica.html")}}
	mods := models.Visibility{"administracija": true}

	w := httptest.NewRecorder()
	r := zahtjevOdbijenice("/administracija/izvori", map[string]string{"Sec-Fetch-Mode": "navigate"}, mods)
	trebaAdmina(s.odbij, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("prošlo bez prava") })).ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, želim 403", w.Code)
	}
	tijelo := w.Body.String()
	for _, treba := range []string{porukaSamoAdmin, `class="izbornik"`, `<h1 class="vrh-naslov">goCOP</h1>`, "Ako vam treba, javite se administratoru.", `href="/administracija">`, "Na Naslovnu", "Nemate pristup"} {
		if !strings.Contains(tijelo, treba) {
			t.Errorf("na odbijenici nema %q", treba)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type %q", ct)
	}
}

func TestOdbijenicaBezAdministracijeNemaPutaTamo(t *testing.T) {
	s := &Server{templates: map[string]*template.Template{"odbijenica.html": predlozakStranice(t, "odbijenica.html")}}
	w := httptest.NewRecorder()
	r := zahtjevOdbijenice("/administracija/izvori", map[string]string{"Accept": "text/html,application/xhtml+xml"}, models.Visibility{})
	s.odbij(w, r, Odbijenica{Poruka: porukaSamoAdmin})

	tijelo := w.Body.String()
	if strings.Contains(tijelo, "Na Administraciju") || strings.Contains(tijelo, "adm-natrag") {
		t.Error("račun bez Administracije ne smije dobiti put na nju")
	}
	if !strings.Contains(tijelo, "Na Naslovnu") {
		t.Error("nema puta na Naslovnu")
	}
}

func TestOdbijenicaSkriptiSamoRecenica(t *testing.T) {
	s := &Server{templates: map[string]*template.Template{"odbijenica.html": predlozakStranice(t, "odbijenica.html")}}
	for ime, zaglavlja := range map[string]map[string]string{
		"fetch":            {"Sec-Fetch-Mode": "cors", "Accept": "text/html"},
		"bez zaglavlja":    {},
		"stari preglednik": {"Accept": "application/json"},
	} {
		w := httptest.NewRecorder()
		s.odbij(w, zahtjevOdbijenice("/api/nesto", zaglavlja, nil), Odbijenica{Poruka: porukaSamoAdmin})
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != porukaSamoAdmin {
			t.Errorf("%s: %d %q, želim 403 i samo rečenicu", ime, w.Code, w.Body.String())
		}
	}
}

// Na pravom poslužitelju odbijenica ima izbornik, ime administratora kojeg
// treba pitati i ime modula koji se htio otvoriti
func TestOdbijenicaNaPoslužiteljuImenujeAdministratora(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	stranica := map[string]string{"Sec-Fetch-Mode": "navigate"}

	w := o.zahtjev(http.MethodGet, "/administracija/izvori", o.mara, nil, stranica)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, želim 403", w.Code)
	}
	tijelo := w.Body.String()
	for _, treba := range []string{"Ako vam treba, javite se administratoru: iivic.", `class="izbornik"`, "<title>goCOP — Administracija</title>"} {
		if !strings.Contains(tijelo, treba) {
			t.Errorf("na odbijenici nema %q", treba)
		}
	}

	// tuđim očima upis se odbija istom stranicom
	if err := o.gledajMaru(); err != nil {
		t.Fatal(err)
	}
	w = o.zahtjev(http.MethodPost, "/profile/update", o.ivo, nil, stranica)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Dok gledaš tuđim očima program samo čita.") || !strings.Contains(w.Body.String(), `class="izbornik"`) {
		t.Errorf("upis tuđim očima: %d, želim odbijenicu s izbornikom", w.Code)
	}
}

func TestOdbijenicaBezPredloskaSamoRecenica(t *testing.T) {
	w := httptest.NewRecorder()
	r := zahtjevOdbijenice("/administracija/izvori", map[string]string{"Sec-Fetch-Mode": "navigate"}, nil)
	(&Server{}).odbij(w, r, Odbijenica{Poruka: porukaSamoAdmin})
	if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != porukaSamoAdmin {
		t.Errorf("%d %q", w.Code, w.Body.String())
	}
}
