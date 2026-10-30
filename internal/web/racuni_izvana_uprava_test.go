package web

// Pravo globalnog administratora daje i dužnost uprave organizacije
// (razina 1), i privremena, jer zamjenik ima ista prava kao rukovoditelj
// (Odluke (30. 10. 2026.), 12 i 14). Izvana zato lozinku vlastitog računa
// traže i dodjela, izmjena i opoziv takve dužnosti, novi račun s takvom
// ulogom, lozinka, uključenost i brisanje takvog računa, a i nova potvrda
// adrese za PIN. S ovog računala i iz lokalne mreže lozinka ne treba.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// duznost upiše osobi u aktivnu dužnost s ulogom za cijelu organizaciju
func (o *okolinaRacuna) duznost(u *models.User, uloga models.Role, privremena bool) *models.Duty {
	o.t.Helper()
	d := &models.Duty{UserID: u.ID, Title: uloga.Label(), Role: uloga, ScopeType: models.ScopeAll, IsTemporary: privremena}
	if err := o.repo.AddDuty(d); err != nil {
		o.t.Fatal(err)
	}
	return d
}

// bezLozinke javlja je li zahtjev odbijen zbog lozinke računa izvana
func bezLozinke(t *testing.T, w interface{ Header() http.Header }) bool {
	t.Helper()
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	g := u.Query().Get("error")
	return strings.Contains(g, "uz lozinku vašeg računa") || strings.Contains(g, "lozinka nije točna")
}

// slucaj je jedan zahtjev obrasca i treba li ga izvana odbiti bez lozinke
type slucaj struct {
	ime       string
	porijeklo func(*http.Request) *http.Request
	putanja   string
	forma     url.Values
	odbijeno  bool
}

func (o *okolinaRacuna) provjeri(t *testing.T, slucajevi []slucaj) {
	t.Helper()
	for _, c := range slucajevi {
		w := o.salji(c.porijeklo, c.putanja, c.forma)
		if got := bezLozinke(t, w); got != c.odbijeno {
			t.Errorf("%s: odbijeno %v (%s)", c.ime, got, w.Header().Get("Location"))
		}
	}
}

// uzLozinku je kopija obrasca s Aninom lozinkom računa
func uzLozinku(f url.Values) url.Values {
	k := url.Values{}
	for ime, v := range f {
		k[ime] = append([]string(nil), v...)
	}
	k.Set("lozinka_racuna", "anina-lozinka")
	return k
}

func TestDuznostUpraveIzvanaUzLozinku(t *testing.T) {
	dnevnik := dnevnikPrograma(t)
	o := novaOkolinaRacuna(t)
	tunel := krozTunel("198.51.100.7")
	mara := o.osoba("mmaric", false)
	dodaj := func(uloga models.Role) url.Values {
		return url.Values{"user_id": {mara.ID.String()}, "role": {string(uloga)}, "title": {uloga.Label()}}
	}
	o.provjeri(t, []slucaj{
		{"dodjela razine 1 izvana", tunel, "/users/duty/add", dodaj(models.RoleGlobalAdmin), true},
		{"dodjela zamjenika izvana", tunel, "/users/duty/add", dodaj(models.RoleMainCenterDeputy), true},
		{"dodjela razine 1 izvana s lozinkom", tunel, "/users/duty/add", uzLozinku(dodaj(models.RoleMainCenterDeputy)), false},
		{"dodjela razine 1 lokalno", izravno, "/users/duty/add", dodaj(models.RoleNationalDeputy), false},
		{"dodjela niže uloge izvana", tunel, "/users/duty/add", dodaj(models.RoleViewer), false},
	})
	if sad, err := o.repo.GetUserByID(mara.ID); err != nil || len(sad.Duties) != 3 {
		t.Fatalf("Mara treba tri dužnosti: %+v %v", sad, err)
	}

	pero := o.osoba("pperic", false)
	uprava := o.duznost(pero, models.RoleMainCenterLeader, false)
	nize := o.duznost(pero, models.RoleViewer, false)
	izmijeni := func(d *models.Duty, uloga models.Role) (string, url.Values) {
		return "/users/duties/" + d.ID.String() + "/update",
			url.Values{"user_id": {pero.ID.String()}, "role": {string(uloga)}, "title": {d.Title}, "is_temporary": {"1"}}
	}
	upravaPut, upravaForma := izmijeni(uprava, models.RoleMainCenterLeader)
	uzdignutPut, uzdignutForma := izmijeni(nize, models.RoleGlobalAdmin)
	nizePut, nizeForma := izmijeni(nize, models.RoleViewer)
	o.provjeri(t, []slucaj{
		{"izmjena dužnosti razine 1 izvana", tunel, upravaPut, upravaForma, true},
		{"niža dužnost u razinu 1 izvana", tunel, uzdignutPut, uzdignutForma, true},
		{"izmjena niže dužnosti izvana", tunel, nizePut, nizeForma, false},
		{"izmjena dužnosti razine 1 lokalno", izravno, upravaPut, upravaForma, false},
		{"izmjena dužnosti razine 1 izvana s lozinkom", tunel, upravaPut, uzLozinku(upravaForma), false},
	})

	opozovi := func(d *models.Duty) url.Values {
		return url.Values{"duty_id": {d.ID.String()}, "user_id": {pero.ID.String()}}
	}
	o.provjeri(t, []slucaj{
		{"opoziv dužnosti razine 1 izvana", tunel, "/users/duty/revoke", opozovi(uprava), true},
		{"opoziv niže dužnosti izvana", tunel, "/users/duty/revoke", opozovi(nize), false},
		{"opoziv dužnosti razine 1 izvana s lozinkom", tunel, "/users/duty/revoke", uzLozinku(opozovi(uprava)), false},
	})
	if d, err := o.repo.GetDuty(uprava.ID); err != nil || d.IsActive {
		t.Errorf("dužnost razine 1 nije opozvana: %+v %v", d, err)
	}
	if !strings.Contains(dnevnik.String(), `POST "/users/duty/add"`) || !strings.Contains(dnevnik.String(), `POST "/users/duty/revoke"`) ||
		!strings.Contains(dnevnik.String(), `/update", s adrese 198.51.100.7`) {
		t.Errorf("dnevnik programa nema odbijene dužnosti:\n%s", dnevnik.String())
	}
	if strings.Contains(dnevnik.String(), "anina-lozinka") {
		t.Error("lozinka je u dnevniku programa")
	}
}

func TestRacunUpravePoDuznostiIzvana(t *testing.T) {
	o := novaOkolinaRacuna(t)
	tunel := krozTunel("198.51.100.7")
	zamjenik := o.osoba("zzamjenik", false)
	o.duznost(zamjenik, models.RoleMainCenterDeputy, true)
	mara := o.osoba("mmaric", false)
	sef := o.osoba("sef", true)
	ponisti := func(u *models.User) string { return "/users/" + u.ID.String() + "/reset-password" }
	brisi := func(u *models.User) url.Values { return url.Values{"user_id": {u.ID.String()}} }
	iskljuci := func(u *models.User, admin bool) url.Values {
		f := izmjena(u, admin, "", "")
		f.Del("is_active")
		return f
	}
	novi := func(ime string, uloga models.Role) url.Values {
		return url.Values{"username": {ime}, "password": {"pocetna-lozinka"}, "full_name": {"Novi Novak"},
			"org_type": {string(models.OrgHrvatskeVode)}, "role": {string(uloga)}}
	}
	o.provjeri(t, []slucaj{
		{"poništenje lozinke zamjenika izvana", tunel, ponisti(zamjenik), url.Values{}, true},
		{"poništenje lozinke zamjenika s lozinkom", tunel, ponisti(zamjenik), uzLozinku(url.Values{}), false},
		{"lozinka zamjenika u obrascu izvana", tunel, "/users/update", izmjena(zamjenik, false, "nova-lozinka-12", ""), true},
		{"lozinka zamjenika u obrascu lokalno", izravno, "/users/update", izmjena(zamjenik, false, "nova-lozinka-12", ""), false},
		{"isključenje zamjenika izvana", tunel, "/users/update", iskljuci(zamjenik, false), true},
		{"isključenje administratora izvana", tunel, "/users/update", iskljuci(sef, true), true},
		{"isključenje administratora s lozinkom", tunel, "/users/update", uzLozinku(iskljuci(sef, true)), false},
		{"isključenje djelatnika izvana", tunel, "/users/update", iskljuci(mara, false), false},
		{"novi račun s ulogom razine 1 izvana", tunel, "/users/create", novi("nnovak", models.RoleGlobalAdmin), true},
		{"novi račun s ulogom razine 1 s lozinkom", tunel, "/users/create", uzLozinku(novi("nnovak", models.RoleGlobalAdmin)), false},
		{"novi račun s nižom ulogom izvana", tunel, "/users/create", novi("vvidic", models.RoleViewer), false},
		{"brisanje zamjenika izvana", tunel, "/users/delete", brisi(zamjenik), true},
		{"brisanje djelatnika izvana", tunel, "/users/delete", brisi(mara), false},
		{"brisanje zamjenika s lozinkom", tunel, "/users/delete", uzLozinku(brisi(zamjenik)), false},
	})
	if sad, err := o.repo.GetUserByID(sef.ID); err != nil || sad.IsActive {
		t.Errorf("administrator nije isključen uz lozinku: %+v %v", sad, err)
	}
	if sad, err := o.repo.GetUserByID(zamjenik.ID); err != nil || sad != nil {
		t.Errorf("zamjenik nije obrisan uz lozinku: %+v %v", sad, err)
	}
	if u, err := o.repo.GetUserByUsername("nnovak"); err != nil || u == nil || !pravoAdministratora(u) {
		t.Errorf("novi račun s ulogom razine 1: %+v %v", u, err)
	}

	// lozinka se ne čita iz adrese stranice
	w := o.salji(tunel, ponisti(sef)+"?lozinka_racuna=anina-lozinka", url.Values{})
	if !bezLozinke(t, w) {
		t.Errorf("lozinka iz adrese stranice prihvaćena: %s", w.Header().Get("Location"))
	}
}

func TestPotvrdaAdreseIzvanaUzLozinku(t *testing.T) {
	o := novaOkolinaRacuna(t)
	tunel := krozTunel("198.51.100.7")
	mara := o.osoba("mmaric", false)
	adresa := func(email string, potvrdi bool) url.Values {
		f := izmjena(mara, false, "", "")
		f.Set("email", email)
		f.Set("pin_adresa_obrazac", "1")
		if potvrdi {
			f.Set("pin_adresa_potvrdena", "1")
		}
		return f
	}
	novi := url.Values{"username": {"vvanjski"}, "password": {"pocetna-lozinka"}, "full_name": {"Vanja Vanjski"},
		"org_type": {string(models.OrgVanjski)}, "email": {"vanja@izvodjac.hr"}, "pin_adresa_potvrdena": {"1"}}
	o.provjeri(t, []slucaj{
		{"potvrda adrese izvana", tunel, "/users/update", adresa("mara@izvodjac.hr", true), true},
		{"potvrda adrese lokalno", izravno, "/users/update", adresa("mara@izvodjac.hr", true), false},
		{"već potvrđena adresa izvana", tunel, "/users/update", adresa("mara@izvodjac.hr", true), false},
		{"druga adresa uz potvrdu izvana", tunel, "/users/update", adresa("mara2@izvodjac.hr", true), true},
		{"skidanje potvrde izvana", tunel, "/users/update", adresa("mara@izvodjac.hr", false), false},
		{"nova potvrda izvana s lozinkom", tunel, "/users/update", uzLozinku(adresa("mara2@izvodjac.hr", true)), false},
		{"novi račun s potvrdom izvana", tunel, "/users/create", novi, true},
		{"novi račun s potvrdom s lozinkom", tunel, "/users/create", uzLozinku(novi), false},
	})
	if sad, err := o.repo.GetUserByID(mara.ID); err != nil || !sad.PotvrdaAdreseVrijedi() || sad.Email != "mara2@izvodjac.hr" {
		t.Errorf("potvrda uz lozinku nije spremljena: %+v %v", sad, err)
	}
}

// Uprava sektora ne mijenja pravo globalnog administratora, pa joj se
// lozinka računa ne traži: izmjenu odbija usluga, s porukom o ovlasti
func TestUpravaSektoraIzvanaBezPitanjaZaLozinku(t *testing.T) {
	o := novaOkolinaRacuna(t)
	sef := o.osoba("sef", true)
	o.perms = &models.UserPermissions{User: models.User{ID: uuid.New(), Username: "usektor"}, AdminSectors: map[string]bool{"B": true}}
	w := o.salji(krozTunel("198.51.100.7"), "/users/update", izmjena(sef, false, "", ""))
	if g := greska(t, w); g == "" || bezLozinke(t, w) {
		t.Errorf("uprava sektora dobiva poruku o lozinci: %q", g)
	}
}

// Polja lozinke računa za dužnost razine 1, brisanje i poništenje lozinke
// osobe koja pravo globalnog administratora ima samo po dužnosti
func TestObrasciUpraveIzvanaPoljeLozinke(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	zamjenik := &models.User{ID: uuid.New(), FullName: "Zamjenik", IsActive: true,
		Duties: []models.Duty{{ID: uuid.New(), Role: models.RoleMainCenterDeputy, Title: "Zamjenik", IsActive: true, ScopeType: models.ScopeAll}}}
	duznost := &zamjenik.Duties[0]
	for _, izvana := range []bool{true, false} {
		for _, p := range []struct {
			stranica string
			data     UserPageData
			polja    int
		}{
			// poništenje, opoziv dužnosti razine 1 i brisanje
			{"user_detail.html", UserPageData{CurrentUser: adminIvic, Permissions: admin, User: zamjenik, CanManage: true, CanDelete: true, PravoAdministratora: true, Izvana: izvana}, 3},
			{"duty_form.html", UserPageData{CurrentUser: adminIvic, Permissions: admin, User: zamjenik, Duty: duznost, Roles: roleOptions(), Izvana: izvana}, 1},
			{"user_form.html", UserPageData{CurrentUser: adminIvic, Permissions: admin, User: &models.User{}, Roles: roleOptions(), Izvana: izvana}, 1},
		} {
			var b bytes.Buffer
			if err := predlozakStranice(t, p.stranica).ExecuteTemplate(&b, p.stranica, p.data); err != nil {
				t.Fatal(err)
			}
			n := strings.Count(b.String(), `name="lozinka_racuna"`)
			if (izvana && n != p.polja) || (!izvana && n != 0) {
				t.Errorf("%s izvana=%v: %d polja", p.stranica, izvana, n)
			}
			if izvana && p.stranica != "user_detail.html" && !strings.Contains(b.String(), `data-uprava="1"`) {
				t.Errorf("%s: uloge razine 1 nisu označene", p.stranica)
			}
		}
	}
}

// Obrasci računa: neispravan zahtjev i neispravan ID vraćaju na popis, a
// novi račun s područjem dobiva dužnost u tom području
func TestObrasciRacunaNeispravniIPodrucje(t *testing.T) {
	o := novaOkolinaRacuna(t)
	for _, putanja := range []string{"/users/create", "/users/update"} {
		r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader("a=%zz"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, o.perms))
		w := httptest.NewRecorder()
		o.srv.ServeHTTP(w, izravno(r))
		if loc := w.Header().Get("Location"); !strings.Contains(loc, "error=Neispravan+zahtjev") {
			t.Errorf("%s s neispravnim tijelom: %q", putanja, loc)
		}
	}
	if loc := o.salji(izravno, "/users/update", url.Values{"id": {"nije-id"}}).Header().Get("Location"); !strings.Contains(loc, "error=Neispravan+ID") {
		t.Errorf("izmjena s neispravnim ID-em: %q", loc)
	}

	for _, upit := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (7, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
	} {
		if _, err := o.baza.Exec(upit); err != nil {
			t.Fatal(err)
		}
	}
	bp := models.Area{ID: 7, SectorID: "P"}
	forma := url.Values{"username": {"rrukovoditelj"}, "password": {"pocetna-lozinka"}, "full_name": {"Rade Rukovoditelj"},
		"org_type": {string(models.OrgHrvatskeVode)}, "role": {string(models.RoleAreaLeader)},
		"sector_id": {bp.SectorID}, "area_id": {strconv.Itoa(bp.ID)}}
	if g := greska(t, o.salji(krozTunel("198.51.100.7"), "/users/create", forma)); g != "" {
		t.Fatalf("novi rukovoditelj područja izvana: %q", g)
	}
	u, err := o.repo.GetUserByUsername("rrukovoditelj")
	if err != nil || u == nil || len(u.Duties) != 1 || u.Duties[0].AreaID == nil || *u.Duties[0].AreaID != bp.ID {
		t.Errorf("dužnost u području %d: %+v %v", bp.ID, u, err)
	}
}
