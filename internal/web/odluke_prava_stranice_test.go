package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/poslovi"
)

// Odluke (30. 10. 2026.), 5: na stranici unosa u arhivu izdanja, ulaganje i
// nizove bez datoteke vidi samo globalni administrator. Administratorica
// područja vidi samo obrasce za unos.
func TestUnosUArhivuOdjeljciZaAdministratora(t *testing.T) {
	stranica := func(admin bool) string {
		d := vrataZaTest()
		d.Permissions = &models.UserPermissions{IsGlobalAdmin: admin}
		d.Letve = []string{"batina"}
		d.IzdavanjeRadi, d.UlaganjeRadi = true, true
		d.Sirotani = []arhiva.Sirotan{{Letva: "batina", Izvor: "probni", Velicina: "vodostaj", Vrsta: "dnevni", Zapisa: 3}}
		return iscrtaj(t, "uvoz_niza.html", d)
	}
	samoAdmin := []string{"Izdanja arhive", "/administracija/izdavanje", "Očitanja iz programa", "/administracija/ulaganje",
		"/administracija/uvoz-niza/makni-niz", `<div class="breadcrumb"><a href="/administracija">`}
	ana := stranica(false)
	for _, s := range samoAdmin {
		if strings.Contains(ana, s) {
			t.Errorf("administratorica područja vidi %q", s)
		}
	}
	if !strings.Contains(ana, `action="/administracija/uvoz-niza/pregled"`) || !strings.Contains(ana, `href="/stations"`) {
		t.Error("administratorica područja ne vidi obrazac za unos ili mrvicu prema letvama")
	}
	pero := stranica(true)
	for _, s := range samoAdmin {
		if !strings.Contains(pero, s) {
			t.Errorf("administrator ne vidi %q", s)
		}
	}
}

// Rukovatelj ne puni dijelove nad cijelom arhivom za onoga tko ih ne vidi,
// a stranicu otvara i onome tko smije svoju letvu.
func TestUnosUArhivuOtvaraOnajTkoSmijeLetvu(t *testing.T) {
	o := novaOkolinaVrata(t)
	o.h.SetIzdavanje(func() string { return t.TempDir() },
		func(string, bool, io.Writer) (arhiva.IzvjestajIzdanja, error) { return arhiva.IzvjestajIzdanja{}, nil },
		func() (arhiva.Katalog, error) { return arhiva.Katalog{Nastalo: time.Now()}, nil }, poslovi.NoviRegistar())
	prikazi := func(u *models.User, p *models.UserPermissions) (UvozPageData, int) {
		r := zahtjevSKorisnikom(httptest.NewRequest(http.MethodGet, "/administracija/uvoz-niza", nil), u, p)
		w := httptest.NewRecorder()
		o.h.ShowUvoz(w, r)
		return o.h.pageData(r), w.Code
	}
	d, kod := prikazi(o.ana, o.anaOvl)
	if kod != http.StatusOK || d.IzdavanjeRadi || d.UlaganjeRadi || d.Sirotani != nil || d.KatalogNastalo != "" {
		t.Errorf("Ana: %d, izdavanje %v, ulaganje %v, sirotani %v", kod, d.IzdavanjeRadi, d.UlaganjeRadi, d.Sirotani)
	}
	if d, kod = prikazi(o.marko, o.markoOvl); kod != http.StatusForbidden {
		t.Errorf("Marko bez ijedne letve: %d", kod)
	}
	if d, kod = prikazi(o.pero, o.peroOvl); kod != http.StatusOK || !d.IzdavanjeRadi || d.KatalogNastalo == "" {
		t.Errorf("Pero: %d, izdavanje %v, katalog %q", kod, d.IzdavanjeRadi, d.KatalogNastalo)
	}
}

// Kartica letve nudi „Unos u arhivu” samo onome tko smije uređivati letvu,
// i samo kad ovaj čvor drži njezine izvorne datoteke.
func TestKarticaLetveNudiUnosUArhivu(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"}
	for _, unos := range []bool{true, false} {
		html := iscrtaj(t, "station_detail.html", StationPageData{CurrentUser: &models.User{FullName: "Ana Anić"},
			Permissions: &models.UserPermissions{}, Station: st, CanEdit: true, LetvaStranica: "kartica", UnosUArhivu: unos})
		if ima := strings.Contains(html, `href="/administracija/uvoz-niza?letva=batina"`); ima != unos {
			t.Errorf("UnosUArhivu %v: gumb %v", unos, ima)
		}
	}

	koren := t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "dunav", "batina"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Server{podaciDir: koren}
	if !s.letvaUStablu("batina") || !s.letvaUStablu(" batina ") || s.letvaUStablu("vukovar") || s.letvaUStablu("") {
		t.Error("letva u stablu se krivo prepoznaje")
	}
	if (&Server{}).letvaUStablu("batina") {
		t.Error("čvor bez stabla nudi unos")
	}
	h := &StationsHandler{}
	h.SetUnosUArhivu(s.letvaUStablu)
	if h.uStablu == nil || !h.uStablu("batina") {
		t.Error("provjera stabla nije predana kartici")
	}
}

// Odluke (30. 10. 2026.), 6: tko pretplate ne smije mijenjati, vidi
// stranicu bez obrazaca i jednu rečenicu zašto.
func TestPretplateBezObrazacaZaOstale(t *testing.T) {
	s := novaStranicaPretplata(t)
	if _, err := s.h.peers.AddSubscription(t.Context(), peers.Subscription{Kind: "prijave", AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	obrasci := []string{`action="/pretplate/dodaj"`, "Nova pretplata"}
	if html := s.prikazi(t); !strings.Contains(html, obrasci[0]) || strings.Contains(html, "mijenja samo globalni administrator") {
		t.Error("administrator iz lokalne mreže ne vidi obrazac za novu pretplatu")
	}
	if html := s.prikazi(t); !strings.Contains(html, "možete obrisati s ovog računala") {
		t.Error("administratoru uvod ne kaže da može obrisati")
	}
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	for opis, x := range map[string]struct {
		u      uvjetiZahtjeva
		poruka string
	}{
		"obični korisnik":      {obican, "Što ovo računalo prati mijenja samo globalni administrator, s ovog računala ili iz lokalne mreže."},
		"administrator izvana": {izvana, "Što ovo računalo prati mijenja se samo s ovog računala ili iz lokalne mreže, a ne izvana ni kroz tunel."},
	} {
		html := s.prikaziKao(t, x.u)
		for _, o := range append(obrasci, `action="/pretplate/ukloni"`, `action="/pretplate/obrisi"`) {
			if strings.Contains(html, o) {
				t.Errorf("%s vidi %q", opis, o)
			}
		}
		if !strings.Contains(html, `<div class="alert alert-info">`) || !strings.Contains(html, x.poruka) {
			t.Errorf("%s: nema rečenice %q na vrhu stranice", opis, x.poruka)
		}
		if strings.Contains(html, "<th></th>") || strings.Contains(html, `<td class="nowrap text-right">`) {
			t.Errorf("%s: tablica pretplata ima prazan stupac za gumb", opis)
		}
		if !strings.Contains(html, "koje ovdje označi administrator") || strings.Contains(html, "možete obrisati") {
			t.Errorf("%s: uvod stranice kaže da sam bira i briše", opis)
		}
	}
}

// Odluke (30. 10. 2026.), 7: gumb „Generiraj” vide dežurni i administrator,
// ostali rečenicu tko prognozu pokreće.
func TestGumbGenerirajSamoZaDezurnogIAdministratora(t *testing.T) {
	d := PrognozePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{},
		ActiveNav: "prognoze", MozeGenerirati: true}
	html := iscrtaj(t, "prognoze_postavke.html", d)
	if strings.Contains(html, `action="/prognoze/generiraj"`) || !strings.Contains(html, "Prognozu rukom pokreću dežurni operater, voditelj centra, uprava sektora i globalni administrator. Ako vam treba odmah, javite dežurnom.") {
		t.Error("tko ne smije generirati vidi gumb ili ne vidi rečenicu")
	}
	d.SmijeGenerirati = true
	if html = iscrtaj(t, "prognoze_postavke.html", d); !strings.Contains(html, `action="/prognoze/generiraj"`) {
		t.Error("dežurni ne vidi gumb Generiraj")
	}
}

// Odluke (30. 10. 2026.), 10: uprava branjenog područja vidi gumb „Novi
// djelatnik”, a vodočuvar ne.
func TestGumbNoviDjelatnikZaUpravuPodrucja(t *testing.T) {
	for opis, x := range map[string]struct {
		perms *models.UserPermissions
		vidi  bool
	}{
		"uprava područja":        {&models.UserPermissions{AdminAreas: map[int]bool{16: true}}, true},
		"uprava sektora":         {&models.UserPermissions{AdminSectors: map[string]bool{"B": true}}, true},
		"globalni administrator": {&models.UserPermissions{IsGlobalAdmin: true}, true},
		"vodočuvar":              {&models.UserPermissions{AllowedSections: map[string]bool{"B.16.1": true}}, false},
	} {
		html := iscrtaj(t, "users.html", UsersPageData{CurrentUser: &models.User{FullName: "Ana Anić"}, Permissions: x.perms})
		if ima := strings.Contains(html, `href="/users/new"`); ima != x.vidi {
			t.Errorf("%s: gumb %v", opis, ima)
		}
	}
}

// Odluke (30. 10. 2026.), 15: obrazac novog djelatnika nema unaprijed
// odabranu ulogu. Globalni administrator smije ostaviti račun bez uloge,
// ostali ulogu moraju odabrati.
func TestNoviDjelatnikBezZadaneUloge(t *testing.T) {
	obrazac := func(admin bool) string {
		return iscrtaj(t, "user_form.html", UserPageData{CurrentUser: &models.User{FullName: "Ana Anić"},
			Permissions: &models.UserPermissions{IsGlobalAdmin: admin, AdminAreas: map[int]bool{16: !admin}},
			User:        &models.User{OrgType: models.OrgHrvatskeVode, IsActive: true}, Roles: roleOptions(), Orgs: orgOptions()})
	}
	for _, admin := range []bool{true, false} {
		html := obrazac(admin)
		if strings.Contains(html, `value="OPERATOR" selected`) || strings.Contains(html, `value="OPERATOR"  selected`) {
			t.Errorf("admin %v: dežurni je unaprijed odabran", admin)
		}
		if !strings.Contains(html, `<option value="">`) || !strings.Contains(html, `minlength="10"`) {
			t.Errorf("admin %v: nema prazne uloge ili najmanje duljine lozinke", admin)
		}
	}
	if html := obrazac(true); !strings.Contains(html, "— bez uloge —") || strings.Contains(html, `name="role" class="form-control" required`) {
		t.Error("globalni administrator ne može otvoriti račun bez uloge")
	}
	if html := obrazac(false); !strings.Contains(html, "— odaberi ulogu —") || !strings.Contains(html, `name="role" class="form-control" required`) {
		t.Error("uprava područja ne mora odabrati ulogu")
	}
}

// Račun bez uloge vidi na početnoj stranici jednu rečenicu da čeka dodjelu
// uloge; račun s dužnošću i administrator je ne vide. Kartica i zaglavlje
// pišu isti naziv uloge, bez posebnog „bez uloge”.
func TestPocetnaZaRacunBezUloge(t *testing.T) {
	prije := time.Now().Add(-time.Hour)
	vodocuvar := models.Duty{Role: models.RoleWaterGuard, IsActive: true, IsPrimary: true}
	istekla := models.Duty{Role: models.RoleOperator, IsActive: true, ExpiresAt: &prije}
	for _, x := range []struct {
		opis string
		u    *models.User
		ceka bool
	}{
		{"bez prijave", nil, false},
		{"novi račun", &models.User{FullName: "Ivo Novak"}, true},
		{"isteklo zaduženje", &models.User{FullName: "Ivo Novak", Duties: []models.Duty{istekla}}, true},
		{"vodočuvar", &models.User{FullName: "Ivo Novak", Duties: []models.Duty{vodocuvar}}, false},
		{"globalni administrator", &models.User{FullName: "Ivo Novak", IsGlobalAdmin: true}, false},
	} {
		if got := cekaUlogu(x.u); got != x.ceka {
			t.Errorf("%s: čeka ulogu %v", x.opis, got)
		}
	}
	const recenica = "Vaš račun čeka da mu administrator dodijeli ulogu. Dotad možete samo gledati."
	for _, ceka := range []bool{true, false} {
		html := iscrtaj(t, "dashboard.html", DashboardViewData{CurrentUser: &models.User{FullName: "Ivo Novak"},
			Perms: &models.UserPermissions{}, CekaUlogu: ceka})
		if strings.Contains(html, recenica) != ceka || strings.Contains(html, "bez uloge") {
			t.Errorf("CekaUlogu %v: rečenica %v", ceka, strings.Contains(html, recenica))
		}
	}
}

// Odluke (30. 10. 2026.), 13: isključivanje uloge ovog računala traži
// potvrdu, a Stanje razmjene pokazuje uloge drugih računala.
func TestUlogeRacunalaPotvrdaIPrikaz(t *testing.T) {
	html := iscrtaj(t, "settings.html", SettingsPageData{CurrentUser: adminIvic,
		Permissions: &models.UserPermissions{IsGlobalAdmin: true}})
	if !strings.Contains(html, "data-potvrda-uloga") || !strings.Contains(html, "Isključiti ulogu ovog čvora (") ||
		!strings.Contains(html, "p.defaultChecked") {
		t.Error("isključivanje uloge nema potvrdu")
	}
	sink, err := os.ReadFile(filepath.Join("..", "..", "web", "templates", "sinkronizacija.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"function uloge(s)", "izdaje prognozu", "preuzima vodostaje", "${uloge(s)}"} {
		if !strings.Contains(string(sink), s) {
			t.Errorf("Stanje razmjene nema %q", s)
		}
	}
}

// S kartice letve stiže se na unos sa šifrom letve u adresi: polja „Letva”
// već je imaju, mrvica vodi natrag na karticu, a izbornik ne ističe
// Administraciju onome tko je nema. Letva koju osoba ne smije dirati ili
// koje stablo nema ne upisuje se nikamo.
func TestUnosUArhivuSKarticeLetve(t *testing.T) {
	o := novaOkolinaVrata(t)
	o.h.SetKarticaLetve(func(_ context.Context, letva string) (string, string) {
		if letva == "primjerovo" {
			return "/stations/1", "Primjerovo na Primjerici"
		}
		return "", ""
	})
	stranica := func(u *models.User, p *models.UserPermissions, letva string) (UvozPageData, string) {
		r := zahtjevSKorisnikom(httptest.NewRequest(http.MethodGet, "/administracija/uvoz-niza?letva="+letva, nil), u, p)
		w := httptest.NewRecorder()
		o.h.ShowUvoz(w, r)
		return o.h.pageData(r), w.Body.String()
	}
	d, html := stranica(o.ana, o.anaOvl, "primjerovo")
	if d.Odabrana == nil || d.Odabrana.Kod != "primjerovo" || d.ActiveNav != "stations" {
		t.Fatalf("Ana s kartice: %+v, izbornik %q", d.Odabrana, d.ActiveNav)
	}
	for _, s := range []string{`<a href="/stations/1">Primjerovo na Primjerici</a> › Unos u arhivu`, `value="primjerovo"`} {
		if !strings.Contains(html, s) {
			t.Errorf("Ana s kartice: nema %q", s)
		}
	}
	if d, _ = stranica(o.ana, o.anaOvl, "drugovo"); d.Odabrana != nil {
		t.Errorf("letva koju Ana ne smije: %+v", d.Odabrana)
	}
	if d, _ = stranica(o.pero, o.peroOvl, "nema-je"); d.Odabrana != nil || d.ActiveNav != "admin" {
		t.Errorf("letva koje stablo nema: %+v, izbornik %q", d.Odabrana, d.ActiveNav)
	}
	if d, _ = stranica(o.pero, o.peroOvl, "drugovo"); d.Odabrana == nil || d.Odabrana.Naziv != "drugovo" || d.Odabrana.Kartica != "" {
		t.Errorf("letva bez kartice: %+v", d.Odabrana)
	}
	o.h.SetKarticaLetve(nil)
	if d, _ = stranica(o.pero, o.peroOvl, "primjerovo"); d.Odabrana == nil || d.Odabrana.Kartica != "" {
		t.Errorf("bez puta do kartice: %+v", d.Odabrana)
	}
}

// Tko nije administrator, ne dobiva upute koje ga šalju u Administraciju.
func TestUnosUArhivuUputeBezAdministracije(t *testing.T) {
	for _, admin := range []bool{true, false} {
		d := vrataZaTest()
		d.Permissions = &models.UserPermissions{IsGlobalAdmin: admin}
		d.Cekaizvor = "cekajuci"
		html := iscrtaj(t, "uvoz_niza.html", d)
		if strings.Contains(html, `href="/administracija/izvori"`) != admin || strings.Contains(html, "na stranici Telemetrija") != admin {
			t.Errorf("admin %v: upute u Administraciju", admin)
		}
		if !admin && (!strings.Contains(html, "Uključuje ga globalni administrator.") || !strings.Contains(html, "upisuje ga globalni administrator")) {
			t.Error("ne-administrator ne zna tko uključuje izvor ili upisuje račun")
		}
	}
}
