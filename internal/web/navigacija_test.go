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

// iscrtajSkoljku iscrta stranicu registara s danim podacima školjke
// (izbornik, gornja i donja traka iz base.html)
func iscrtajSkoljku(t *testing.T, data DashboardViewData) string {
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
	if err := tmpl.ExecuteTemplate(&izlaz, "registri.html", data); err != nil {
		t.Fatal(err)
	}
	return izlaz.String()
}

// dio je dio HTML-a od oznake do njezina kraja
func dio(t *testing.T, html, od, do string) string {
	t.Helper()
	i := strings.Index(html, od)
	if i < 0 {
		t.Fatalf("stranica nema %q", od)
	}
	j := strings.Index(html[i:], do)
	if j < 0 {
		t.Fatalf("%q nema kraja %q", od, do)
	}
	return html[i : i+j]
}

// sadrzajStranice je samo glavni sadržaj, bez izbornika i traka: natpisi
// izbornika (Teren, Dokumenti…) ne smiju zadovoljiti provjeru sadržaja
func sadrzajStranice(html string) string {
	i := strings.Index(html, "<main")
	j := strings.LastIndex(html, "</main>")
	if i < 0 || j < i {
		return html
	}
	return html[i:j]
}

func prikaziNavigaciju(t *testing.T, moduli models.Visibility) string {
	t.Helper()
	html := iscrtajSkoljku(t, DashboardViewData{ActiveNav: "users", ViewAsBanner: ViewAsBanner{Modules: moduli}})
	return dio(t, html, `<nav class="main-nav"`, `</nav>`)
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

// Naslov u gornjoj traci je stavka izbornika kojoj stranica pripada; registri
// objekata (objekti-*) pripadaju Registrima. Inicijali su prvo slovo prvog i
// zadnjeg imena.
func TestNaslovGornjeTrakeIInicijali(t *testing.T) {
	for nav, zeli := range map[string]string{
		"": "Naslovna", "dashboard": "Naslovna", "readings": "Očitanja", "prognoze": "Prognoze",
		"journals": "Dokumenti", "teren": "Teren", "sredstva": "Sredstva",
		"sections": "Registri", "firme": "Registri", navObjekta("nasip"): "Registri", navObjekta("crpna-stanica"): "Registri",
		"sync": "Administracija", "moduli": "Administracija", "users": "Djelatnici i dežurstva",
		"pomoc": "Pomoć", "profile": "Profil", "posta": "Sandučić", "nepoznato": "goCOP",
	} {
		if got := navNaslov(nav); got != zeli {
			t.Errorf("navNaslov(%q) = %q, želi %q", nav, got, zeli)
		}
	}
	for ime, zeli := range map[string]string{
		"Marko Marić": "MM", "Ana Marija Horvat": "AH", "ivana kovač": "IK", "  Pero  Perić  ": "PP",
		"Đuro": "Đ", "šime šarić": "ŠŠ", "": "?", "   ": "?",
	} {
		if got := inicijali(ime); got != zeli {
			t.Errorf("inicijali(%q) = %q, želi %q", ime, got, zeli)
		}
	}
}

// Gornja traka: naslov stavke, krug s inicijalima prijavljene osobe i oznaka
// razmjene; bez prijavljene osobe (npr. stranica prijave) nema ni kruga ni
// oznake. Registar objekata ističe Registre u izborniku.
func TestGornjaTraka(t *testing.T) {
	html := iscrtajSkoljku(t, DashboardViewData{
		ActiveNav:    navObjekta("nasip"),
		CurrentUser:  &models.User{FullName: "Ana Marija Horvat"},
		ViewAsBanner: ViewAsBanner{Modules: models.Visibility{models.ModuleRegisters: true}},
	})
	vrh := dio(t, html, `<header class="vrh"`, `</header>`)
	for _, zeli := range []string{`<h1 class="vrh-naslov">Registri</h1>`, `>AH</a>`, `id="vrh-razmjena"`, `data-stanje="nema"`, `href="/dogadjanja"`} {
		if !strings.Contains(vrh, zeli) {
			t.Errorf("gornja traka nema %q:\n%s", zeli, vrh)
		}
	}
	nav := dio(t, html, `<nav class="main-nav"`, `</nav>`)
	if razredi, _, _ := strings.Cut(nav[strings.Index(nav, "nav-group-btn"):], `"`); !strings.Contains(razredi, "active") {
		t.Error("registar objekata ne ističe Registre u izborniku")
	}
	vrh = dio(t, iscrtajSkoljku(t, DashboardViewData{}), `<header class="vrh"`, `</header>`)
	if strings.Contains(vrh, "vrh-osoba") || strings.Contains(vrh, "vrh-razmjena") {
		t.Errorf("bez prijavljene osobe gornja traka ima osobu ili razmjenu:\n%s", vrh)
	}
}

// Donja traka na mobitelu: Naslovna, Očitanja, Prognoze i Dokumenti (ili
// Teren, kome dokumenti nisu vidljivi), pa „Više” koje otvara izbornik
func TestDonjaTrakaNaMobitelu(t *testing.T) {
	traka := dio(t, iscrtajSkoljku(t, DashboardViewData{
		ActiveNav:    "readings",
		ViewAsBanner: ViewAsBanner{Modules: models.Visibility{models.ModuleReadings: true, models.ModuleJournals: true, models.ModuleField: true}},
	}), `<nav class="donja-traka"`, `</nav>`)
	for _, zeli := range []string{`href="/" class="donja-stavka "`, `href="/readings" class="donja-stavka active"`, `href="/prognoze"`, `href="/dnevnici"`, `aria-controls="izbornik"`, "Više"} {
		if !strings.Contains(traka, zeli) {
			t.Errorf("donja traka nema %q:\n%s", zeli, traka)
		}
	}
	if strings.Contains(traka, `href="/teren"`) {
		t.Error("donja traka ima i Dokumente i Teren, pa je stavki više od pet")
	}
	traka = dio(t, iscrtajSkoljku(t, DashboardViewData{ViewAsBanner: ViewAsBanner{Modules: models.Visibility{models.ModuleField: true}}}), `<nav class="donja-traka"`, `</nav>`)
	if !strings.Contains(traka, `href="/teren"`) || strings.Contains(traka, `href="/dnevnici"`) || strings.Contains(traka, `href="/readings"`) {
		t.Errorf("vodočuvaru bez dokumenata donja traka ne vodi na Teren:\n%s", traka)
	}
}
