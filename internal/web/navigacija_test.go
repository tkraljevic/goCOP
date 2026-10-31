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

// Djelatnici su vlastita stavka glavnog izbornika, a Administrativna
// organizacija ostaje pod Registrima (Odluke (30. 10. 2026.)). Isti izbornik
// je i na mobitelu, otvoren gumbom „Više”.
func TestDjelatniciSuVlastitaStavkaIzbornika(t *testing.T) {
	nav := prikaziNavigaciju(t, models.Visibility{
		models.ModuleRegisters: true,
		models.ModuleUsers:     true,
	})
	if !strings.Contains(nav, `<a href="/users" class="nav-link active"`) {
		t.Error("Djelatnici nisu istaknuta stavka glavnog izbornika")
	}
	if strings.Contains(nav, `<a href="/users" class="nav-dropdown-item`) {
		t.Error("Djelatnici su ostali i u padajućem izborniku Registara")
	}
	if !strings.Contains(nav, `href="/organizacija" class="nav-dropdown-item`) {
		t.Error("Administrativna organizacija nije pod Registrima")
	}
	if razredi, _, _ := strings.Cut(nav[strings.Index(nav, "nav-group-btn"):], `"`); strings.Contains(razredi, "active") {
		t.Error("Registri su istaknuti dok je otvoren imenik djelatnika")
	}
}

func TestDjelatniciBezRegistaraIRegistriBezDjelatnika(t *testing.T) {
	nav := prikaziNavigaciju(t, models.Visibility{models.ModuleUsers: true})
	if !strings.Contains(nav, `<a href="/users" class="nav-link`) {
		t.Error("korisnik s imenikom nema stavku Djelatnici")
	}
	if strings.Contains(nav, `nav-group-btn`) || strings.Contains(nav, `href="/organizacija"`) {
		t.Error("korisniku bez modula Registri ponuđeni su Registri")
	}
	nav = prikaziNavigaciju(t, models.Visibility{models.ModuleRegisters: true})
	if strings.Contains(nav, `href="/users"`) {
		t.Error("korisniku bez modula Djelatnici ponuđeni su Djelatnici")
	}
	if !strings.Contains(nav, `nav-group-btn`) {
		t.Error("korisnik s Registrima nema Registre")
	}
}

// Sredstva za obranu su vlastita stavka izbornika, a ne dio Registara
// (sustav dizajna, Odluke (31. 10. 2026.)); vidi ih tko vidi registre
func TestSredstvaSuVlastitaStavkaIzbornika(t *testing.T) {
	nav := prikaziNavigaciju(t, models.Visibility{models.ModuleRegisters: true})
	if !strings.Contains(nav, `<a href="/sredstva" class="nav-link`) {
		t.Error("Sredstva nisu stavka glavnog izbornika")
	}
	if strings.Contains(nav, `<a href="/sredstva" class="nav-dropdown-item`) {
		t.Error("Sredstva su ostala i pod Registrima")
	}
	if strings.Contains(prikaziNavigaciju(t, models.Visibility{models.ModuleUsers: true}), `href="/sredstva"`) {
		t.Error("korisniku bez modula Registri ponuđena su Sredstva")
	}
}

// Naslov u gornjoj traci i inicijali osobe
func TestNaslovGornjeTrakeIInicijali(t *testing.T) {
	for nav, zeli := range map[string]string{"": "Naslovna", "dashboard": "Naslovna", "sections": "Registri", "sync": "Administracija", "journals": "Dokumenti", "nepoznato": "goCOP"} {
		if got := navNaslov(nav); got != zeli {
			t.Errorf("navNaslov(%q) = %q, želi %q", nav, got, zeli)
		}
	}
	for ime, zeli := range map[string]string{"Tomislav Kraljević": "TK", "Ana Marija Horvat": "AH", "Đuro": "Đ", "": "?"} {
		if got := inicijali(ime); got != zeli {
			t.Errorf("inicijali(%q) = %q, želi %q", ime, got, zeli)
		}
	}
}
