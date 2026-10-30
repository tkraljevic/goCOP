package web

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// staraPocetnaLozinka je stara zajednička lozinka u testovima prijave
const staraPocetnaLozinka = service.StaraPocetnaLozinka

// racunSaSazetkom je račun za pregled; sažetak je samo oznaka
func racunSaSazetkom(ime, sazetak string, ukljucen bool) models.User {
	return models.User{ID: uuid.New(), Username: ime, PasswordHash: sazetak, IsActive: ukljucen}
}

func imena(racuni []models.User) string {
	var b []string
	for _, u := range racuni {
		b = append(b, u.Username)
	}
	return strings.Join(b, ",")
}

// Svaki sažetak provjerava se jednom, isključeni računi se ne provjeravaju;
// na popisu su samo uključeni računi koje stara lozinka otvara, po
// sadašnjem sažetku
func TestPregledStarihLozinki(t *testing.T) {
	provjera := 0
	s := &stareLozinke{provjeri: func(h string) bool { provjera++; return strings.HasPrefix(h, "stara") }}
	ana := racunSaSazetkom("ana", "stara-1", true)
	racuni := []models.User{ana, racunSaSazetkom("iva", "nova", true), racunSaSazetkom("admin", "stara-2", false),
		racunSaSazetkom("eva", "stara-1", true)}

	if got := imena(s.pregledaj(context.Background(), racuni)); got != "ana,eva" || provjera != 2 {
		t.Fatalf("prvi pregled: %q, provjera %d", got, provjera)
	}
	if got := imena(s.pregledaj(context.Background(), racuni)); got != "ana,eva" || provjera != 2 {
		t.Errorf("drugi pregled ne provjerava ponovno: %q, provjera %d", got, provjera)
	}
	// poništena lozinka: račun više nije na popisu, nov sažetak se nakon
	// pregleda provjeri jednom, a stari se zaboravi
	racuni[0].PasswordHash, racuni[3].PasswordHash = "ponistena", "ponistena"
	if got := imena(s.medju(racuni)); got != "" || provjera != 3 {
		t.Errorf("nakon poništenja: %q, provjera %d", got, provjera)
	}
	s.pregledaj(context.Background(), racuni)
	if _, ok := s.otvara["stara-1"]; ok || provjera != 3 {
		t.Errorf("stari sažetak ostao zapamćen, provjera %d", provjera)
	}
	// isključen račun koji netko uključi nakon pregleda provjeri se za popis
	racuni[2].IsActive = true
	if got := imena(s.medju(racuni)); got != "admin" || provjera != 4 {
		t.Errorf("ponovno uključen račun: %q, provjera %d", got, provjera)
	}
}

// Prije kraja pregleda popis za Djelatnike ne provjerava sažetke (to radi
// pregled), a račun stigao razmjenom nakon pregleda provjeri se pri
// sljedećem otvaranju stranice
func TestPopisStarihLozinkiNakonPregleda(t *testing.T) {
	provjera := 0
	s := &stareLozinke{provjeri: func(h string) bool { provjera++; return h == "stara" }}
	stigao := []models.User{racunSaSazetkom("tudji", "stara", true)}
	if got := s.medju(stigao); got != nil || provjera != 0 {
		t.Errorf("prije pregleda: %v, provjera %d", got, provjera)
	}
	s.pregledaj(context.Background(), nil)
	if got := imena(s.medju(stigao)); got != "tudji" || provjera != 1 {
		t.Errorf("nakon pregleda: %q, provjera %d", got, provjera)
	}
}

// Zahtjev prijavljene osobe provjeri sažetak jednom i zapamti ishod; račun
// zabilježen pri prijavi ne provjerava se ponovno; bez pregleda nema
// zaključanih
func TestStaraLozinkaZakljucavaSesiju(t *testing.T) {
	provjera := 0
	s := &stareLozinke{provjeri: func(h string) bool { provjera++; return h == "stara" }}
	for _, sazetak := range []string{"stara", "nova", "stara", "nova"} {
		if got := s.otvaraLi(sazetak); got != (sazetak == "stara") {
			t.Errorf("%s: otvara %v", sazetak, got)
		}
	}
	if provjera != 2 {
		t.Errorf("svaki sažetak provjerava se jednom: provjera %d", provjera)
	}
	zabiljezen := racunSaSazetkom("pperic", "zabiljezen", true)
	s.zabiljezi(&zabiljezen)
	if !s.otvaraLi("zabiljezen") || provjera != 2 {
		t.Errorf("račun zabilježen pri prijavi: provjera %d", provjera)
	}
	if (&Server{}).StaraLozinkaZakljucava("stara") {
		t.Error("bez pregleda starih lozinki sesija je zaključana")
	}
}

// Račun zabilježen pri prijavi dok pregled traje ostaje zabilježen
func TestPregledStarihLozinkiCuvaZabiljezeno(t *testing.T) {
	s := &stareLozinke{}
	tudji := racunSaSazetkom("tudji", "stigao-razmjenom", true)
	s.provjeri = func(string) bool { s.zabiljezi(&tudji); return false }
	s.pregledaj(context.Background(), []models.User{racunSaSazetkom("ana", "nova", true)})
	if got := imena(s.medju([]models.User{tudji})); got != "tudji" {
		t.Errorf("zabilježen račun: %q", got)
	}
}

// Prekinut pregled ništa ne provjerava; prazan ili nepostojeći popis ne
// pada
func TestPregledStarihLozinkiPrekinut(t *testing.T) {
	ctx, prekini := context.WithCancel(context.Background())
	prekini()
	s := &stareLozinke{provjeri: func(string) bool { t.Error("prekinut pregled provjerava"); return true }}
	if got := s.pregledaj(ctx, []models.User{racunSaSazetkom("ana", "stara", true)}); got != nil {
		t.Errorf("prekinut pregled: %v", got)
	}
	var nema *stareLozinke
	nema.zabiljezi(&models.User{})
	s.zabiljezi(nil)
	if nema.medju([]models.User{racunSaSazetkom("ana", "stara", true)}) != nil {
		t.Error("bez pregleda nema računa")
	}
}

// Pregled pri pokretanju gasi otvorene prijave računa sa starom lozinkom i
// javlja ih u dnevnik samo brojem; stranica Djelatnici ih dobiva po
// sadašnjem sažetku
func TestPregledajStareLozinkeNaPosluzitelju(t *testing.T) {
	o := novaOkolinaPrijave(t)
	stari := o.racun("pperic", staraPocetnaLozinka, false, true)
	o.racun("ana", "anina-lozinka", false, false)
	sesija, err := o.auth.OtvoriSesiju(stari, "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{userService: service.NewUserService(o.repo, o.auth, service.NewSSEBroker()), authService: o.auth,
		stareLozinke: &stareLozinke{provjeri: o.auth.ImaStaruLozinku}}
	dnevnik := dnevnikPrograma(t)

	// prije pregleda: prijava sa starom lozinkom prestaje pri prvom
	// zahtjevu, a prijava s drugom lozinkom ostaje
	ivina, err := o.auth.OtvoriSesiju(o.racun("iva", "ivina-lozinka", false, false), "192.168.1.51", "test")
	if err != nil {
		t.Fatal(err)
	}
	o.auth.SetZakljucanaLozinka(s.StaraLozinkaZakljucava)
	if _, _, err := o.auth.AuthenticateSession(sesija.ID); !errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("prijava sa starom lozinkom prije pregleda: %v", err)
	}
	if _, _, err := o.auth.AuthenticateSession(ivina.ID); err != nil {
		t.Errorf("prijava s drugom lozinkom: %v", err)
	}
	if sesija, err = o.auth.OtvoriSesiju(stari, "192.168.1.50", "test"); err != nil {
		t.Fatal(err)
	}
	o.auth.SetZakljucanaLozinka(nil) // pregled sam gasi prijave

	s.PregledajStareLozinke(context.Background())
	if !strings.Contains(dnevnik.String(), "1 računa još ima staru zajedničku početnu lozinku") || strings.Contains(dnevnik.String(), "pperic") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
	if _, _, err := o.auth.AuthenticateSession(sesija.ID); err == nil {
		t.Error("otvorena prijava računa sa starom lozinkom nije ugašena")
	}
	racuni, err := s.racuniSaStaromLozinkom()
	if err != nil || imena(racuni) != "pperic" {
		t.Errorf("za stranicu Djelatnici: %q %v", imena(racuni), err)
	}

	// bez takvih računa dnevnik šuti; baza koja se ne čita javlja se
	dnevnik.Reset()
	if err := o.repo.ResetPassword(stari.ID, mustHash(t, o.auth, "nova-privremena-1")); err != nil {
		t.Fatal(err)
	}
	s.PregledajStareLozinke(context.Background())
	if strings.Contains(dnevnik.String(), "računa još ima") {
		t.Errorf("nakon poništenja: %q", dnevnik.String())
	}
	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	s.PregledajStareLozinke(context.Background())
	if !strings.Contains(dnevnik.String(), "nisu pregledani") {
		t.Errorf("greška baze: %q", dnevnik.String())
	}
	if _, err := s.racuniSaStaromLozinkom(); err == nil {
		t.Error("greška baze za stranicu Djelatnici se ne javlja")
	}
}

func mustHash(t *testing.T, a *service.AuthService, lozinka string) string {
	t.Helper()
	h, err := a.HashPassword(lozinka)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Zaključane račune na stranici Djelatnici vidi samo globalni
// administrator; greška čitanja ne ruši stranicu
func TestDjelatniciPokazujuZakljucaneRacune(t *testing.T) {
	ana := racunSaSazetkom("aanic", "stara", true)
	ana.FullName = "Ana Anić"
	var greska error
	h := &UsersHandler{stareLozinke: func() ([]models.User, error) { return []models.User{ana}, greska }}
	if got := h.zakljucaniRacuni(&models.UserPermissions{AdminSectors: map[string]bool{"B": true}}); got != nil {
		t.Errorf("uprava sektora vidi zaključane račune: %v", got)
	}
	if got := h.zakljucaniRacuni(nil); got != nil {
		t.Errorf("bez prava: %v", got)
	}
	if got := h.zakljucaniRacuni(&models.UserPermissions{IsGlobalAdmin: true}); imena(got) != "aanic" {
		t.Errorf("globalni administrator: %v", got)
	}
	dnevnik := dnevnikPrograma(t)
	greska = errors.New("baza je zaključana")
	if got := h.zakljucaniRacuni(&models.UserPermissions{IsGlobalAdmin: true}); got != nil || !strings.Contains(dnevnik.String(), "nisu pročitani") {
		t.Errorf("greška čitanja: %v, dnevnik %q", got, dnevnik.String())
	}
	if (&UsersHandler{}).zakljucaniRacuni(&models.UserPermissions{IsGlobalAdmin: true}) != nil {
		t.Error("nespojen rukovatelj")
	}

	podaci := UsersPageData{CurrentUser: &models.User{FullName: "Uprava"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}}
	if html := iscrtaj(t, "users.html", podaci); strings.Contains(html, "staru zajedničku lozinku") {
		t.Error("upozorenje bez zaključanih računa")
	}
	podaci.StareLozinke = []models.User{ana}
	html := iscrtaj(t, "users.html", podaci)
	if !strings.Contains(html, "staru zajedničku lozinku") || !strings.Contains(html, `href="/users/`+ana.ID.String()+`"`) || !strings.Contains(html, "Ana Anić") {
		t.Errorf("upozorenje na stranici Djelatnici nedostaje: %q", html)
	}
}
