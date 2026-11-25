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
	stranica := func(moduli models.Visibility) string {
		r := httptest.NewRequest("GET", "/pomoc", nil)
		r = r.WithContext(context.WithValue(r.Context(), contextKeyModules, moduli))
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
	if strings.Contains(admin, "Otvori Očitanja") || strings.Contains(admin, "Otvori Moje letve") {
		t.Error("bez Očitanja Pomoć ipak nudi gumb Otvori Očitanja")
	}
	for _, p := range poveznice {
		if !strings.Contains(admin, p) {
			t.Errorf("s Administracijom i Dokumentima Pomoć ne nudi %s", p)
		}
	}
}
