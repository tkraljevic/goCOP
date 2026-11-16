package web

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Odluke (16. 11. 2026.), Djelatnici 3 i 5: popis je tablica s recima koji
// vode na osobu, korisničko ime je samo na stranici osobe, a filtri su
// čipovi koji pišu odabrano i čuvaju ostale filtre.
func TestPopisDjelatnikaTablicaICipovi(t *testing.T) {
	sektor, podrucje := "B", 16
	ana := models.User{ID: uuid.New(), Username: "aprimjer", FullName: "Ana Primjer", MobilePhone: "099 555 0110", IsActive: true,
		Duties: []models.Duty{{Title: "Vodočuvar", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, IsTemporary: true}}}
	f := filtriDjelatnika{Sektor: "B", Trazi: "ana"}
	podaci := UsersPageData{CurrentUser: &models.User{FullName: "Uprava"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Users: []models.User{ana}, SelectedSector: "B", SearchQuery: "ana",
		Cipovi:     cipoviDjelatnika(f, []models.Sector{{ID: "B", Name: "Sektor B"}}, []models.Area{{ID: 16, Name: "Primjer"}}),
		URLPonisti: "/users"}
	html := iscrtaj(t, "users.html", podaci)
	for _, want := range []string{
		`<div class="dj-ime" role="cell"><a href="/users/` + ana.ID.String() + `">Ana Primjer</a>`,
		`<a href="tel:099%20555%200110">`,
		`BP 16 · sve dionice`,
		`>Privremeno</span>`,
		`Sektor: B`,
		`href="/users?area=16&amp;q=ana&amp;sector=B"`,
		`<a class="dj-ponisti" href="/users">Poništi filtre</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("popis nema %s", want)
		}
	}
	if strings.Contains(html, "@aprimjer") || strings.Contains(html, ">aprimjer<") {
		t.Error("korisničko ime ne ide u popis")
	}
}

// Čipovi pišu odabrano, izbor čuva ostale filtre, a izbor sektora briše
// područje (područje pripada sektoru).
func TestCipoviDjelatnika(t *testing.T) {
	if (filtriDjelatnika{}).imaFiltar() || !(filtriDjelatnika{Stanje: "PENDING"}).imaFiltar() {
		t.Error("imaFiltar")
	}
	if (filtriDjelatnika{}).urlPonisti() != "" || (filtriDjelatnika{Trazi: "x"}).urlPonisti() != "/users" {
		t.Error("urlPonisti")
	}
	if got := (filtriDjelatnika{}).url(); got != "/users" {
		t.Errorf("prazni filtri: %s", got)
	}
	f := filtriDjelatnika{Sektor: "B", Podrucje: 16, Uloga: string(models.RoleWaterGuard), Stanje: "PENDING"}
	c := cipoviDjelatnika(f, []models.Sector{{ID: "B", Name: "Sektor B"}, {ID: "C", Name: "Sektor C"}}, []models.Area{{ID: 16, Name: "Primjer"}})
	if len(c) != 4 || !strings.HasSuffix(c[0].Natpis, ": B") || !strings.HasSuffix(c[1].Natpis, ": 16") ||
		c[2].Natpis != "Uloga: "+models.RoleWaterGuard.Label() || c[3].Natpis != "Stanje računa: Nije se prijavio" {
		t.Fatalf("natpisi: %+v", c)
	}
	for _, iz := range c[0].Izbor {
		if iz.Naziv == "Sektor C" && strings.Contains(iz.URL, "area=") {
			t.Errorf("izbor drugog sektora zadržao je područje: %s", iz.URL)
		}
	}
	for _, iz := range c[3].Izbor {
		if iz.Naziv == "Isključen" && (!strings.Contains(iz.URL, "status=DISABLED") || !strings.Contains(iz.URL, "sector=B")) {
			t.Errorf("izbor stanja: %s", iz.URL)
		}
	}
}

// Stranica osobe po crtežu 20, 2: kartica „Prijava i lozinka” s objema
// radnjama i rečenicom što rade, moduli kao tri izbora istog imena polja
// (mod_*), pa obrazac šalje isto kao dosadašnji izbornik.
func TestStranicaOsobePoCrtezu(t *testing.T) {
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	b, err := fs.ReadFile(templatesFS, "user_detail.html")
	if err != nil {
		t.Fatal(err)
	}
	h := string(b)
	for _, want := range []string{
		`<h2 id="os-prijava">Prijava i lozinka</h2>`,
		`action="/users/{{.User.ID}}/reset-password"`, `action="/users/{{.User.ID}}/kod-prijave"`,
		`Za osobu koja je zaboravila lozinku.`, `Kad PIN ne stiže:`,
		`<input type="radio" name="mod_{{.ID}}" value=""`, `value="show"`, `value="hide"`,
		`<summary>{{icon "history"}} Prijašnja zaduženja`,
		`Pregled tuđim očima`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("stranica osobe nema %s", want)
		}
	}
}

// Crtež 20, 2a: privremena lozinka i kod su okviri u boji odabranog, ne
// zelena poruka o uspjehu, a vrijednost se označi jednim dodirom.
func TestOkviriPrivremeneLozinkeIKoda(t *testing.T) {
	ivan := &models.User{ID: uuid.New(), FullName: "Ivan Primjer", Username: "iprimjer", IsActive: true}
	html := iscrtaj(t, "user_detail.html", UserPageData{CurrentUser: &models.User{FullName: "Uprava"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		User: ivan, CanManage: true, TempPassword: "vrba-nasip-camac-274", TempKod: "P-7K2M-Q4XD"})
	for _, want := range []string{
		`<h2 id="os-tajna-lozinka">`, `<p class="os-tajna-vrijednost">vrba-nasip-camac-274</p>`,
		`<h2 id="os-tajna-kod">`, `<p class="os-tajna-vrijednost">P-7K2M-Q4XD</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica osobe nema %s", want)
		}
	}
	if strings.Contains(html, "alert-success d-block") {
		t.Error("lozinka je još u zelenoj poruci")
	}
}

// Crtež 20, 3: obrazac djelatnika u karticama Račun, Osoba, Kontakti,
// Početno zaduženje i Prava, s pravima kao prekidačima; polja su ista.
func TestObrazacDjelatnikaUKarticama(t *testing.T) {
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	b, err := fs.ReadFile(templatesFS, "user_form.html")
	if err != nil {
		t.Fatal(err)
	}
	h := string(b)
	prije := -1
	for _, kartica := range []string{">Račun</h2>", ">Osoba</h2>", ">Kontakti</h2>", ">Početno zaduženje</h2>", ">Prava</h2>"} {
		i := strings.Index(h, kartica)
		if i < prije {
			t.Errorf("kartica %s nije na mjestu (%d)", kartica, i)
		}
		prije = i
	}
	for _, polje := range []string{`name="username"`, `name="password"`, `name="full_name"`, `name="org_type"`, `name="mobile_phone"`, `name="email"`, `name="duty_title"`, `name="role"`, `name="section_codes"`,
		`class="prekidac" role="switch" id="is_active" name="is_active"`, `class="prekidac" role="switch" id="is_global_admin" name="is_global_admin"`} {
		if !strings.Contains(h, polje) {
			t.Errorf("obrazac nema %s", polje)
		}
	}
}

// Crtež 20, 4: obrazac zaduženja pokazuje zaduženja koja osoba već ima, a
// primarno i privremena ispomoć su prekidači istih polja.
func TestObrazacZaduzenjaPoCrtezu(t *testing.T) {
	sektor := "B"
	ivan := &models.User{ID: uuid.New(), FullName: "Ivan Primjer", Username: "iprimjer", IsActive: true,
		Duties: []models.Duty{{ID: uuid.New(), Title: "Rukovoditelj dionice", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, SectorID: &sektor, IsPrimary: true}}}
	html := iscrtaj(t, "duty_form.html", UserPageData{CurrentUser: &models.User{FullName: "Uprava"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, User: ivan, CanManage: true})
	for _, want := range []string{
		`<h2 id="ob-postojeca">Zaduženja koja već ima</h2>`, `<strong>Rukovoditelj dionice</strong>`,
		`<h2 id="ob-zaduzenje">Zaduženje</h2>`, `<h2 id="ob-privremeno">Privremena ispomoć</h2>`,
		`class="prekidac" role="switch" name="is_primary"`, `class="prekidac" role="switch" name="is_temporary"`,
		`name="istece_s_obranom"`, `name="reason"`, `name="expires_at"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac zaduženja nema %s", want)
		}
	}
}

// Crtež 20, 5: Adresar tvrtke ima pretragu, karticu usporedbe po sektoru i
// rezultat s naslovom sektora i gumbom „Preuzmi označeno”; polja su ista.
func TestAdresarTvrtkePoCrtezu(t *testing.T) {
	html := iscrtaj(t, "imenik_exchange.html", ImenikData{CurrentUser: &models.User{FullName: "Uprava"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		SmijeUskladiti: true, Usporedio: true, Sektor: "B", SRazlikom: 0, Sektori: []models.Sector{{ID: "B", Name: "Sektor B"}}})
	for _, want := range []string{
		`<input type="search" name="trazi"`, `<h2 id="ad-usporedba">Usporedba imenika s adresarom</h2>`,
		`action="/users/exchange/usporedi"`, `name="sektor"`, `<h2 id="ad-rezultat">Sektor B · usporedba</h2>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Adresar nema %s", want)
		}
	}
}
