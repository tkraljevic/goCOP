package web

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// okolinaAdministracije je ulazna stranica Administracije nad pravom bazom
type okolinaAdministracije struct {
	h        *AdminHandler
	baza     *sql.DB
	peers    *peers.Service
	org      *repository.OrgRepository
	korisnci *repository.UserRepository
}

func novaOkolinaAdministracije(t *testing.T) *okolinaAdministracije {
	t.Helper()
	p, baza := cvorIBazaZaWeb(t, "cop-osijek")
	rec := ledger.New(baza, "cop-osijek")
	orgRepo := repository.NewOrgRepository(baza, rec)
	userRepo := repository.NewUserRepository(baza, rec)
	auth := service.NewAuthService(userRepo, repository.NewSessionRepository(baza))
	users := service.NewUserService(userRepo, auth, service.NewSSEBroker())
	h := NewAdminHandler(service.NewOrgService(orgRepo, service.NewSSEBroker()), users, p, predlozakStranice(t, "administracija.html"), nil)
	return &okolinaAdministracije{h: h, baza: baza, peers: p, org: orgRepo, korisnci: userRepo}
}

// prikazi iscrtava ulaznu stranicu za osobu s tim pravima i modulima
func (o *okolinaAdministracije) prikazi(t *testing.T, perms *models.UserPermissions, moduli models.Visibility) string {
	t.Helper()
	u := perms.User
	r := zahtjevSKorisnikom(httptest.NewRequest(http.MethodGet, "/administracija", nil), &u, perms)
	r = r.WithContext(context.WithValue(r.Context(), contextKeyModules, moduli))
	w := httptest.NewRecorder()
	o.h.ShowAdmin(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("ulazna stranica: %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// skupinaNaStranici je odjeljak skupine; prazno kad ga nema
func skupinaNaStranici(html, id string) string {
	i := strings.Index(html, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	kraj := strings.Index(html[i:], "</section>")
	return html[i : i+kraj]
}

var (
	administrator = &models.UserPermissions{User: models.User{FullName: "Pero Perić", IsGlobalAdmin: true}, IsGlobalAdmin: true}
	samoModulAdm  = models.Visibility{models.ModuleAdmin: true}
)

// Odluke (30. 10. 2026.), 1: šest skupina, tim redom, s karticama iz analize
// administracije; Postavke prognoze u skupini vodostaja, žig u aktima.
func TestAdministracijaSkupineRedom(t *testing.T) {
	o := novaOkolinaAdministracije(t)
	o.h.SetIzvoriStanja(func(context.Context) models.Opcije {
		return models.Opcije{UpisTudjimOcima: true, SimulacijaKljuca: true, SlikeOdmah: true}
	}, nil, nil)
	html := o.prikazi(t, administrator, nil)

	redom := []string{"prvi-koraci", "skupina-ustroj", "skupina-mreza", "skupina-vodostaji", "skupina-akti", "skupina-izgled", "skupina-testni"}
	prije := -1
	for _, id := range redom {
		i := strings.Index(html, `id="`+id+`"`)
		if i < 0 || i < prije {
			t.Fatalf("%s nije na svom mjestu (%d, prethodna %d)", id, i, prije)
		}
		prije = i
	}
	kartice := map[string][]string{
		"skupina-ustroj":    {"/organizacija", "/users", "/administracija/sudionici", "/moduli", "/administracija/nazivi"},
		"skupina-mreza":     {"/settings", "/sinkronizacija", "/pretplate", "/administracija/baza"},
		"skupina-vodostaji": {"/administracija/telemetrija", "/administracija/izvori", "/administracija/uvoz-niza", "/prognoze/postavke"},
		"skupina-akti":      {"/administracija/posta", "/administracija/zig", "/administracija/potpisi", "/administracija/obracun"},
		"skupina-izgled":    {"/administracija/tema", "/administracija/karte"},
		"skupina-testni":    {"/administracija/opcije"},
	}
	for id, hrefovi := range kartice {
		s := skupinaNaStranici(html, id)
		if n := strings.Count(s, `class="dash-card"`); n != len(hrefovi) {
			t.Errorf("%s: %d kartica, a treba %d", id, n, len(hrefovi))
		}
		for _, href := range hrefovi {
			if !strings.Contains(s, `href="`+href+`" class="btn btn-secondary dash-btn"`) {
				t.Errorf("%s nema kartice %s", id, href)
			}
		}
	}
	mreza := skupinaNaStranici(html, "skupina-mreza")
	for _, naslov := range []string{">Ovo računalo i mreža</h2>", ">Stanje razmjene</h2>"} {
		if !strings.Contains(mreza, naslov) {
			t.Errorf("skupina mreže nema naslova %q", naslov)
		}
	}
	if !strings.Contains(skupinaNaStranici(html, "skupina-testni"), "2 uključena") {
		t.Error("Testni prekidači ne kažu koliko ih je uključeno")
	}
	vodostaji := skupinaNaStranici(html, "skupina-vodostaji")
	if !strings.Contains(vodostaji, `class="dash-doseg" title="Vrijedi samo na ovom računalu i ne ide razmjenom.">ovo računalo<`) {
		t.Error("Telemetrija nema oznake da vrijedi samo za ovo računalo")
	}
	if !strings.Contains(skupinaNaStranici(html, "skupina-ustroj"), ">cijela mreža<") {
		t.Error("Nazivi nemaju oznake da vrijede u cijeloj mreži")
	}
}

// Oznaka dosega stoji samo gdje to sigurno slijedi iz spremanja.
func TestDosegKartica(t *testing.T) {
	_, _, doseg := rasporedKartica(pravaKartice{perms: administrator}, false)
	for kljuc, zeli := range map[string]DosegKartice{
		"telemetrija": DosegRacunalo, "izvori": DosegRacunalo, "pretplate": DosegRacunalo,
		"nazivi": DosegMreza, "organizacija": DosegMreza, "opcije": DosegMreza, "zig": DosegMreza,
		"racunalo": DosegOboje, "karte": DosegOboje, "posta": DosegOboje, "baza": DosegOboje,
		"razmjena": "", "unos": "", "prognoza": "",
	} {
		if doseg[kljuc] != zeli {
			t.Errorf("%s: %q, a treba %q", kljuc, doseg[kljuc], zeli)
		}
	}
	for _, d := range []DosegKartice{DosegRacunalo, DosegMreza, DosegOboje} {
		if d.Opis() == "" {
			t.Errorf("%s nema objašnjenja", d)
		}
	}
	if DosegKartice("").Opis() != "" {
		t.Error("prazna oznaka ima objašnjenje")
	}
}

// Tko ima modul Administracija, a nije globalni administrator, ne vidi
// kartice stranica koje ne smije otvoriti, ni skupine bez ijedne kartice.
func TestAdministracijaKarticePratePrava(t *testing.T) {
	o := novaOkolinaAdministracije(t)
	ana := &models.UserPermissions{User: models.User{FullName: "Ana Anić"}}
	html := o.prikazi(t, ana, samoModulAdm)
	for _, href := range []string{"/administracija/telemetrija", "/administracija/izvori", "/administracija/uvoz-niza", "/moduli",
		"/administracija/nazivi", "/administracija/sudionici", "/administracija/opcije", "/administracija/tema", "/administracija/karte",
		"/administracija/posta", "/administracija/obracun", "/administracija/potpisi", "/administracija/zig", "/administracija/uvozi",
		"/prognoze/postavke", "/organizacija", "/users"} {
		if strings.Contains(html, `href="`+href+`"`) {
			t.Errorf("Ana vidi karticu %s", href)
		}
	}
	for _, id := range []string{"prvi-koraci", "skupina-ustroj", "skupina-vodostaji", "skupina-akti", "skupina-izgled", "skupina-testni"} {
		if strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("Ana vidi skupinu %s bez ijedne kartice", id)
		}
	}
	if n := strings.Count(skupinaNaStranici(html, "skupina-mreza"), `class="dash-card"`); n != 4 {
		t.Errorf("Ana u skupini mreže vidi %d kartica, a treba 4", n)
	}

	// s modulom Registri vidi Administrativnu organizaciju, i samo nju u Ustroju
	ustroj := skupinaNaStranici(o.prikazi(t, ana, models.Visibility{models.ModuleAdmin: true, models.ModuleRegisters: true}), "skupina-ustroj")
	if !strings.Contains(ustroj, `href="/organizacija"`) || strings.Count(ustroj, `class="dash-card"`) != 1 {
		t.Errorf("Ana s modulom Registri:\n%s", ustroj)
	}

	// dežurni vidi Postavke prognoze, uprava sektora žig
	sektor := "P"
	dezurni := &models.UserPermissions{User: models.User{FullName: "Iva Ivić", Duties: []models.Duty{{Role: models.RoleOperator,
		ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}}}}
	if v := skupinaNaStranici(o.prikazi(t, dezurni, samoModulAdm), "skupina-vodostaji"); !strings.Contains(v, `href="/prognoze/postavke"`) || strings.Count(v, `class="dash-card"`) != 1 {
		t.Errorf("dežurni u skupini vodostaja:\n%s", v)
	}
	uprava := &models.UserPermissions{User: models.User{FullName: "Petra Pavić"}, AdminSectors: map[string]bool{"P": true}}
	if a := skupinaNaStranici(o.prikazi(t, uprava, samoModulAdm), "skupina-akti"); !strings.Contains(a, `href="/administracija/zig"`) || strings.Count(a, `class="dash-card"`) != 1 {
		t.Errorf("uprava sektora u skupini akata:\n%s", a)
	}
}

// Prvi koraci stoje iznad skupina dok ustroj nije gotov, s kvačicom uz ono
// što je napravljeno, a nestanu kad je gotovo sve što se da provjeriti.
// Tada Uvozi podataka ostaju kao poveznica u Ustroju i ljudima.
func TestPrviKoraciDokUstrojNijeGotov(t *testing.T) {
	o := novaOkolinaAdministracije(t)
	ctx := context.Background()
	telemetrija, posta := false, false
	o.h.SetIzvoriStanja(nil, func(context.Context) bool { return telemetrija }, func(context.Context) bool { return posta })

	koraci := skupinaNaStranici(o.prikazi(t, administrator, nil), "prvi-koraci")
	if strings.Count(koraci, `<li class="dash-korak`) != 8 || strings.Contains(koraci, "dash-korak gotov") {
		t.Fatalf("svjež čvor, Prvi koraci:\n%s", koraci)
	}
	for _, href := range []string{"/administracija/nazivi", "/organizacija", "/users", "/administracija/uvozi", "/settings#uloge",
		"/administracija/telemetrija", "/administracija/posta", "/uparivanje"} {
		if !strings.Contains(koraci, `href="`+href+`"`) {
			t.Errorf("Prvi koraci nemaju %s", href)
		}
	}
	if strings.Count(koraci, "bez-provjere") != 2 {
		t.Error("Uvozi i Uloge moraju stajati bez okvira")
	}

	// nazivi spremljeni: kvačica samo uz njih
	if err := o.org.SaveTerms(ctx, models.DefaultTerms()); err != nil {
		t.Fatal(err)
	}
	html := o.prikazi(t, administrator, nil)
	koraci = skupinaNaStranici(html, "prvi-koraci")
	if strings.Count(koraci, "dash-korak gotov") != 1 || !strings.Contains(koraci, `<li class="dash-korak gotov">
        <span class="dash-korak-oznaka">`) {
		t.Errorf("nakon spremanja naziva:\n%s", koraci)
	}
	if strings.Contains(html, `id="poveznica-uvozi"`) {
		t.Error("Uvozi stoje i u Prvim koracima i u Ustroju")
	}

	// sve ostalo: sektor, djelatnik, telemetrija, e-pošta, upareno računalo
	if _, err := o.baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`); err != nil {
		t.Fatal(err)
	}
	sektor := "P"
	if err := o.korisnci.CreateUser(&models.User{ID: uuid.New(), Username: "iivic", FullName: "Iva Ivić", MustChangePassword: true}, nil); err != nil {
		t.Fatal(err)
	}
	if err := o.korisnci.CreateUser(&models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", IsActive: true},
		&models.Duty{Title: "Dežurni operater u COP-u", Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	kljuc, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.peers.SavePeer(ctx, peers.Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: razmjena.PublicKeyString(kljuc)}); err != nil {
		t.Fatal(err)
	}
	telemetrija = true
	if strings.Contains(o.prikazi(t, administrator, nil), `id="poveznica-uvozi"`) {
		t.Error("Prvi koraci su nestali prije nego što je upisana e-pošta")
	}
	posta = true
	html = o.prikazi(t, administrator, nil)
	if !strings.Contains(html, "1 račun čeka promjenu lozinke") || !strings.Contains(html, "1 je deaktiviran") {
		t.Error("kartica Djelatnici ne broji račune koji čekaju lozinku i deaktivirane")
	}
	if strings.Contains(html, `id="prvi-koraci"`) {
		t.Errorf("ustroj je gotov, a Prvi koraci stoje:\n%s", skupinaNaStranici(html, "prvi-koraci"))
	}
	if !strings.Contains(skupinaNaStranici(html, "skupina-ustroj"), `id="poveznica-uvozi"`) {
		t.Error("nakon Prvih koraka Uvozi podataka nisu u Ustroju i ljudima")
	}
}

// Stavka čiji se izvor ne zna ostaje bez kvačice i ne drži popis otvorenim.
func TestPrviKoraciBezIzvora(t *testing.T) {
	k := prviKoraci(stanjeUstroja{nazivi: true, sektori: true, djelatnici: true, uparen: true})
	if !ustrojGotov(k) {
		t.Error("bez izvora telemetrije i e-pošte popis ostaje otvoren")
	}
	for _, x := range k {
		if (x.Naslov == "Telemetrija" || x.Naslov == "E-pošta") && (x.Zna || x.Gotov) {
			t.Errorf("%s: zna %v, gotov %v", x.Naslov, x.Zna, x.Gotov)
		}
	}
	ne := false
	if ustrojGotov(prviKoraci(stanjeUstroja{nazivi: true, sektori: true, djelatnici: true, uparen: true, posta: &ne})) {
		t.Error("neupisana e-pošta, a ustroj je gotov")
	}
	if izvorStanja(context.Background(), nil) != nil {
		t.Error("izvor bez funkcije nije nil")
	}
}

func TestUkljucenihTestnih(t *testing.T) {
	if n := ukljucenihTestnih(models.Opcije{}); n != 0 {
		t.Errorf("sve isključeno: %d", n)
	}
	sve := models.Opcije{BrisanjeOvjerenihAkata: true, BrisanjePovijestiVerzija: true, BrisanjeSOglasnePloce: true,
		UpisTudjimOcima: true, SimulacijaKljuca: true, SlikeOdmah: true, CuvanjeSlikaDana: 30}
	if n := ukljucenihTestnih(sve); n != 5 {
		t.Errorf("svih pet uključeno: %d", n)
	}
}

func TestImaRacunTelemetrije(t *testing.T) {
	ctx := context.Background()
	if imaRacunTelemetrije(ctx, nil) {
		t.Error("bez baze ima račun")
	}
	_, baza := cvorIBazaZaWeb(t, "cop-osijek")
	if imaRacunTelemetrije(ctx, baza) {
		t.Error("prazna baza ima račun")
	}
	if err := repository.NewHidroViewRepository(baza).Spremi(ctx, &repository.RacunHidroView{Korisnik: "cop", Lozinka: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	if !imaRacunTelemetrije(ctx, baza) {
		t.Error("upisan račun HydroView se ne vidi")
	}
	if imaRacunTelemetrije(ctx, bazaUpita(t)) {
		t.Error("baza bez tablica ima račun")
	}
}

// Odluke (30. 10. 2026.), 3: Sinkroniziraj sa svima, Pretraži lokalnu mrežu
// i razmjena s jednim računalom stoje samo na Stanju razmjene; Ovo računalo i
// mreža do njih vodi poveznicom.
func TestGumbiRazmjeneSamoNaStanjuRazmjene(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	var b strings.Builder
	if err := predlozakStranice(t, "settings.html").ExecuteTemplate(&b, "settings.html", SettingsPageData{CurrentUser: adminIvic, Permissions: admin,
		Peers: []peers.Peer{{NodeID: "laptop-baranja", Name: "Laptop Baranja"}}}); err != nil {
		t.Fatal(err)
	}
	racunalo := b.String()
	for _, nema := range []string{"Sinkroniziraj sa svima", "Pretraži lokalnu mrežu", "syncNow(", "discover(", "syncOne("} {
		if strings.Contains(racunalo, nema) {
			t.Errorf("Ovo računalo i mreža ima %q", nema)
		}
	}
	for _, ima := range []string{"<title>goCOP — Ovo računalo i mreža", "› Ovo računalo i mreža</div>", `id="poveznica-stanje-razmjene"`} {
		if !strings.Contains(racunalo, ima) {
			t.Errorf("Ovo računalo i mreža nema %q", ima)
		}
	}

	b.Reset()
	if err := predlozakStranice(t, "sinkronizacija.html").ExecuteTemplate(&b, "sinkronizacija.html", SyncPageData{CurrentUser: adminIvic, Permissions: admin}); err != nil {
		t.Fatal(err)
	}
	stanje := b.String()
	for _, ima := range []string{"<title>goCOP — Stanje razmjene", "› Stanje razmjene</div>", "Sinkroniziraj sa svima", "Pretraži lokalnu mrežu", "syncOne(", `href="/settings"`} {
		if !strings.Contains(stanje, ima) {
			t.Errorf("Stanje razmjene nema %q", ima)
		}
	}
}

// S5: WEB sync ima jedan odjeljak, s postavkama iz gocop.toml, ponovnim
// uključivanjem i provjerom veze, a ne više u karticama Ovaj čvor i Mreža.
func TestWebSyncImaSvojOdjeljak(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	s.SetProvjeraVeze(func(context.Context) ProvjeraVeze { return ProvjeraVeze{} }, func() *ProvjeraVeze { return nil })
	html := iscrtajPostavke(t, s, "/settings", kaoAdmin)
	odjeljak := strings.Index(html, `id="web-sync"`)
	if odjeljak < 0 {
		t.Fatal("nema odjeljka WEB sync i dohvatljivost")
	}
	for _, id := range []string{`id="postavke-websync"`, `id="provjera-veze"`, `data-postavka="povezivost.web_sync"`} {
		if i := strings.Index(html, id); i < odjeljak {
			t.Errorf("%s nije u odjeljku WEB sync (%d < %d)", id, i, odjeljak)
		}
	}
	uloge := strings.Index(html, `id="uloge"`)
	if i := strings.Index(html, `data-postavka="povezivost.web_sync"`); i > uloge {
		t.Error("WEB sync nije u svom odjeljku, odmah ispod kartice Ovaj čvor")
	}
	i := strings.Index(html, `id="postavke-mreza"`)
	if i < 0 {
		t.Fatal("nema bloka postavki mreže")
	}
	if blok := html[i : i+strings.Index(html[i:], "</details>")]; strings.Contains(blok, `data-postavka="povezivost.`) {
		t.Error("postavke WEB synca stoje i u kartici Mreža")
	}
}
