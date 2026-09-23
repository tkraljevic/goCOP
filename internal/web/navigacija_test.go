package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/web"
)

func prikaziNavigaciju(t *testing.T, moduli models.Visibility) string {
	t.Helper()
	predlosci, err := fs.Sub(web.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(predlosci, DijeloviPredloska("registri.html")...)
	if err != nil {
		t.Fatal(err)
	}
	var izlaz bytes.Buffer
	data := DashboardViewData{ActiveNav: "users", ViewAsBanner: ViewAsBanner{Modules: moduli}}
	if err := tmpl.ExecuteTemplate(&izlaz, "registri.html", data); err != nil {
		t.Fatal(err)
	}
	html := izlaz.String()
	od := strings.Index(html, `<nav class="main-nav"`)
	if od < 0 {
		t.Fatal("glavna navigacija nije prikazana")
	}
	do := strings.Index(html[od:], `</nav>`)
	if do < 0 {
		t.Fatal("glavna navigacija nije prikazana")
	}
	return html[od : od+do]
}

func TestDjelatniciSuUPadajucemIzbornikuRegistara(t *testing.T) {
	nav := prikaziNavigaciju(t, models.Visibility{
		models.ModuleRegisters: true,
		models.ModuleUsers:     true,
	})
	if strings.Contains(nav, `<a href="/users" class="nav-link`) {
		t.Error("Djelatnici su ostali zasebna stavka glavnog izbornika")
	}
	if !strings.Contains(nav, `<a href="/users" class="nav-dropdown-item active`) {
		t.Error("Djelatnici nisu aktivna stavka padajućeg izbornika Registara")
	}
}

func TestDjelatniciOstajuDostupniBezOstalihRegistara(t *testing.T) {
	nav := prikaziNavigaciju(t, models.Visibility{models.ModuleUsers: true})
	if !strings.Contains(nav, `Registri <span class="nav-caret">`) || !strings.Contains(nav, `href="/users"`) {
		t.Error("korisnik s imenikom nema pristup Djelatnicima kroz Registre")
	}
	if strings.Contains(nav, `href="/organizacija"`) {
		t.Error("korisniku bez modula Registra ponuđena je Administrativna organizacija")
	}
}
