package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Poveznica iz Pomoći u dio programa koji čitatelj nema vodila je na golu
// stranicu 403. Tko nema Administraciju ili Dokumente, čita isti tekst bez
// poveznice i bez gumba „Otvori…”.
func TestPomocNudiPoveznicuSamoUDioKojiImate(t *testing.T) {
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("pomoc.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewPomocHandler(tmpl)
	stranica := func(moduli models.Visibility, u ...*models.User) string {
		r := httptest.NewRequest("GET", "/pomoc", nil)
		r = r.WithContext(context.WithValue(r.Context(), contextKeyModules, moduli))
		if len(u) > 0 {
			r = r.WithContext(context.WithValue(r.Context(), contextKeyUser, u[0]))
		}
		w := httptest.NewRecorder()
		h.ShowPomoc(w, r)
		return w.Body.String()
	}
	poveznice := []string{`href="/settings#uloge"`, `href="/sinkronizacija"`, `href="/dnevnici/popis?vrsta=DEZURSTVA"`, `href="/dnevnici"`}

	vodocuvar := stranica(models.Visibility{models.ModuleReadings: true})
	for _, p := range poveznice {
		if strings.Contains(vodocuvar, p) {
			t.Errorf("bez Administracije i Dokumenata Pomoć ipak nudi %s", p)
		}
	}
	// Propise čita svaki prijavljeni (Odluke (25. 11. 2026.), O2), pa gumb
	// stoji i vodočuvaru bez modula Dokumenti
	if !strings.Contains(vodocuvar, `href="/propisi"`) {
		t.Error("Pomoć vodočuvaru ne nudi propise")
	}
	for _, tekst := range []string{"Ovo računalo i mreža", "Stanje razmjene"} {
		if !strings.Contains(vodocuvar, tekst) {
			t.Errorf("bez poveznice nestao je i tekst %q", tekst)
		}
	}

	// gumb Otvori Očitanja vidi samo tko ima Očitanja
	const otvoriOcitanja = `href="/readings">`
	if !strings.Contains(vodocuvar, otvoriOcitanja) || !strings.Contains(vodocuvar, "Otvori Očitanja") {
		t.Error("s Očitanjima Pomoć ne nudi gumb Otvori Očitanja")
	}
	if !strings.Contains(vodocuvar, `href="/readings?moje=1">`) {
		t.Error("s Očitanjima Pomoć u Obilasku ne nudi gumb Otvori Moje letve")
	}

	admin := stranica(models.Visibility{models.ModuleAdmin: true, models.ModuleJournals: true})
	// gumb za Vodočuvarski dnevnik vidi samo tko dnevnik vodi ili čita
	if strings.Contains(admin, "Otvori Vodočuvarski dnevnik") {
		t.Error("bez prijavljene osobe Pomoć nudi Vodočuvarski dnevnik")
	}
	if s := stranica(models.Visibility{}, &models.User{IsGlobalAdmin: true}); !strings.Contains(s, `href="/vodocuvar">`) {
		t.Error("tko čita Vodočuvarski dnevnik, ne dobiva gumb Otvori Vodočuvarski dnevnik")
	}
	// gumb za Prijave s terena prati isto pravilo kao Dokumenti, skupina Teren
	if strings.Contains(admin, "Otvori Prijave s terena") {
		t.Error("bez prijavljene osobe Pomoć nudi Prijave s terena")
	}
	if s := stranica(models.Visibility{}, &models.User{IsGlobalAdmin: true}); !strings.Contains(s, `href="/prijave">`) {
		t.Error("tko čita Vodočuvarski dnevnik, ne dobiva gumb Otvori Prijave s terena")
	}
	if strings.Contains(admin, "Otvori Očitanja") || strings.Contains(admin, "Otvori Moje letve") {
		t.Error("bez Očitanja Pomoć ipak nudi gumb Otvori Očitanja")
	}
	for _, p := range poveznice {
		if !strings.Contains(admin, p) {
			t.Errorf("s Administracijom i Dokumentima Pomoć ne nudi %s", p)
		}
	}
}
