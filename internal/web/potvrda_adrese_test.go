package web

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Adresa koju potvrdi administrator, na obrascu djelatnika: okvir je
// unaprijed označen za potvrđenu adresu i spremanje bez promjene je čuva;
// neoznačen briše potvrdu. Okvir ima samo globalni administrator za tuđi
// račun, a stanje adrese za PIN vide osoba i tko njome upravlja.

var okvirPotvrde = regexp.MustCompile(`<input type="checkbox" id="pin_adresa_potvrdena" name="pin_adresa_potvrdena" value="1"( checked)?`)

// obrazacPotvrde su polja obrasca djelatnika kako ih šalje preglednik
func obrazacPotvrde(u *models.User, email string, oznacen bool) url.Values {
	v := url.Values{"id": {u.ID.String()}, "username": {u.Username}, "full_name": {u.FullName}, "email": {email},
		"org_type": {string(u.OrgType)}, "is_active": {"1"}, "pin_adresa_obrazac": {"1"}}
	if oznacen {
		v.Set("pin_adresa_potvrdena", "1")
	}
	return v
}

// (h) Okvir je unaprijed označen za potvrđenu adresu, spremanje bez
// promjene čuva potvrdu (i tko i kada), neoznačen okvir je briše.
func TestObrazacCuvaPotvrduAdrese(t *testing.T) {
	o := novaOkolinaIzvana(t)
	o.mux.HandleFunc("GET /users/{id}/edit", o.usersH.ShowUserForm)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ivo := o.racun("ivo", "ivina-lozinka", "ivo@bistra.hr", false)
	iz := func() *models.User {
		u, _ := o.repo.GetUserByID(ivo.ID)
		return u
	}
	okvir := func(u *models.User) (bool, bool) {
		t.Helper()
		w := o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodGet, "/users/"+u.ID.String()+"/edit", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("obrazac %s: %d", u.Username, w.Code)
		}
		m := okvirPotvrde.FindStringSubmatch(w.Body.String())
		return m != nil, m != nil && m[1] != ""
	}

	if ima, oznacen := okvir(ivo); !ima || oznacen {
		t.Fatalf("nepotvrđena adresa: okvir %v, označen %v", ima, oznacen)
	}
	if w := o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/update", obrazacPotvrde(iz(), "ivo@bistra.hr", true)); greskaPreusmjerenja(t, w) != "" {
		t.Fatalf("potvrda: %s", w.Header().Get("Location"))
	}
	potvrden := iz()
	if !potvrden.PotvrdaAdreseVrijedi() || potvrden.PINAdresuPotvrdio != "Uprava" || potvrden.PINAdresaPotvrdenaKad == nil {
		t.Fatalf("potvrda nije spremljena: %+v", potvrden)
	}
	if ima, oznacen := okvir(potvrden); !ima || !oznacen {
		t.Fatalf("potvrđena adresa: okvir %v, označen %v", ima, oznacen)
	}

	// spremanje bez promjene (okvir ostaje označen) čuva potvrdu, tko i kada
	x := obrazacPotvrde(iz(), "ivo@bistra.hr", true)
	x.Set("phone", "031-111")
	if w := o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/update", x); greskaPreusmjerenja(t, w) != "" {
		t.Fatalf("spremanje bez promjene: %s", w.Header().Get("Location"))
	}
	if u := iz(); !u.PotvrdaAdreseVrijedi() || u.PINAdresuPotvrdio != "Uprava" || !u.PINAdresaPotvrdenaKad.Equal(*potvrden.PINAdresaPotvrdenaKad) || u.Phone != "031-111" {
		t.Fatalf("spremanje bez promjene: %+v", u)
	}

	// obrazac bez okvira (npr. adresar) potvrdu ne dira
	bez := obrazacPotvrde(iz(), "ivo@bistra.hr", false)
	bez.Del("pin_adresa_obrazac")
	o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/update", bez)
	if !iz().PotvrdaAdreseVrijedi() {
		t.Fatal("obrazac bez okvira obrisao je potvrdu")
	}

	// neoznačen okvir briše potvrdu
	if w := o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/update", obrazacPotvrde(iz(), "ivo@bistra.hr", false)); greskaPreusmjerenja(t, w) != "" {
		t.Fatalf("uklanjanje: %s", w.Header().Get("Location"))
	}
	if u := iz(); u.PINAdresaPotvrdena != "" || u.PINAdresaPotvrdenaKad != nil {
		t.Fatalf("neoznačen okvir: %+v", u)
	}

	// svoj obrazac administrator ima bez okvira, a tuđi zahtjev s okvirom se odbija
	if ima, _ := okvir(admin); ima {
		t.Fatal("administrator vidi okvir potvrde na svom računu")
	}
	ana := o.racun("ana", "anina-lozinka", "ana@bistra.hr", false)
	w := o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodPost, "/users/update", obrazacPotvrde(iz(), "ivo@bistra.hr", true))
	if !strings.Contains(greskaPreusmjerenja(t, w), "globalni administrator") || iz().PINAdresaPotvrdena != "" {
		t.Fatalf("potvrda bez ovlasti: %s", w.Header().Get("Location"))
	}
}

// Tuđim očima okvira nema, a poslan se odbija: administrator koji gleda
// očima drugog globalnog administratora tako bi potvrdio svoju adresu, a
// potvrda bi se pripisala tom drugome.
func TestPotvrdaAdreseNeTudjimOcima(t *testing.T) {
	o := novaOkolinaIzvana(t)
	o.mux.HandleFunc("GET /users/{id}", o.usersH.ShowUser)
	o.mux.HandleFunc("GET /users/{id}/edit", o.usersH.ShowUserForm)
	o.mux.HandleFunc("GET /users/new", o.usersH.ShowUserForm)
	o.mux.HandleFunc("POST /users/create", o.usersH.HandleCreateUser)
	ruza := o.racun("ruza", "ruzina-lozinka", "ruza@voda.hr", true)
	glavni := o.racun("glavni", "glavna-lozinka", "glavni@voda.hr", true)
	ivo := o.racun("ivo", "ivina-lozinka", "ivo@bistra.hr", false)
	tudje := zahtjevIzvana{stvarni: ruza, vidi: glavni}
	iz := func(id *models.User) *models.User {
		u, _ := o.repo.GetUserByID(id.ID)
		return u
	}

	for _, put := range []string{"/users/" + ruza.ID.String() + "/edit", "/users/" + ivo.ID.String() + "/edit", "/users/new"} {
		w := o.posalji(tudje, http.MethodGet, put, nil)
		if w.Code != http.StatusOK || okvirPotvrde.MatchString(w.Body.String()) || strings.Contains(w.Body.String(), "pin_adresa_obrazac") {
			t.Errorf("%s tuđim očima: %d, okvir potvrde na obrascu", put, w.Code)
		}
	}
	if b := o.posalji(tudje, http.MethodGet, "/users/"+ivo.ID.String(), nil).Body.String(); strings.Contains(b, "potvrdite je u uređivanju profila") {
		t.Error("stranica djelatnika tuđim očima nudi potvrdu adrese")
	}

	// svoj račun: adresa izvan domene s označenim okvirom
	svoj := obrazacPotvrde(iz(ruza), "ruza@privatno.hr", true)
	svoj.Set("is_global_admin", "1")
	if g := greskaPreusmjerenja(t, o.posalji(tudje, http.MethodPost, "/users/update", svoj)); !strings.Contains(g, "tuđim očima") {
		t.Fatalf("svoja adresa tuđim očima: %q", g)
	}
	if u := iz(ruza); u.Email != "ruza@voda.hr" || u.PINAdresaPotvrdena != "" || u.PINAdresuPotvrdio != "" {
		t.Fatalf("svoja adresa potvrđena tuđim očima: %+v", u)
	}
	// tuđi račun
	if g := greskaPreusmjerenja(t, o.posalji(tudje, http.MethodPost, "/users/update", obrazacPotvrde(iz(ivo), "ivo@bistra.hr", true))); !strings.Contains(g, "tuđim očima") {
		t.Fatalf("potvrda tuđim očima: %q", g)
	}
	if u := iz(ivo); u.PINAdresaPotvrdena != "" {
		t.Fatalf("potvrda dana tuđim očima: %+v", u)
	}
	// novi račun
	w := o.posalji(tudje, http.MethodPost, "/users/create", url.Values{"username": {"luka"}, "password": {"pocetna-lozinka"},
		"full_name": {"Luka Lukić"}, "org_type": {string(models.OrgPravnaOsoba)}, "email": {"luka@bistra.hr"}, "pin_adresa_potvrdena": {"1"}})
	if g := greskaPreusmjerenja(t, w); !strings.Contains(g, "tuđim očima") {
		t.Fatalf("novi račun s potvrdom tuđim očima: %q", g)
	}
	if u, _ := o.repo.GetUserByUsername("luka"); u != nil {
		t.Fatal("račun je otvoren uz potvrdu tuđim očima")
	}

	// svojim očima isti obrazac prolazi: okvir je tu i potvrda se sprema
	if w := o.posalji(zahtjevIzvana{stvarni: ruza}, http.MethodGet, "/users/"+ivo.ID.String()+"/edit", nil); !okvirPotvrde.MatchString(w.Body.String()) {
		t.Fatal("svojim očima nema okvira potvrde")
	}
	if g := greskaPreusmjerenja(t, o.posalji(zahtjevIzvana{stvarni: ruza}, http.MethodPost, "/users/update", obrazacPotvrde(iz(ivo), "ivo@bistra.hr", true))); g != "" {
		t.Fatalf("potvrda svojim očima: %s", g)
	}
	if u := iz(ivo); !u.PotvrdaAdreseVrijedi() || u.PINAdresuPotvrdio != "Ruza" {
		t.Fatalf("potvrda svojim očima: %+v", u)
	}
}

var (
	skriptaOkvira  = regexp.MustCompile(`(?s)<script>([^<]*pin_adresa_potvrdena.*?)</script>`)
	adresaObrasca  = regexp.MustCompile(`<input type="email" id="email" name="email" class="form-control" value="([^"]*)"`)
	potvrdenaOkvir = regexp.MustCompile(`data-potvrdena="([^"]*)"`)
)

// Skripta okvira na obrascu: promijenjena adresa odznači okvir, vraćena na
// potvrđenu opet ga označi (inače bi spremanje obrisalo potvrdu), a okvir
// koji je administrator sam odznačio ostaje odznačen. Skripta se izvodi u
// Nodeu s najmanjim DOM-om; bez Nodea test se preskače.
func TestSkriptaOkviraVracaPotvrdu(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nije dostupan; skripta okvira se ne izvodi")
	}
	o := novaOkolinaIzvana(t)
	o.mux.HandleFunc("GET /users/{id}/edit", o.usersH.ShowUserForm)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ivo := o.racun("ivo", "ivina-lozinka", "ivo@bistra.hr", false)
	potvrdi := true
	if _, err := o.users.UpdateUser(&models.UserPermissions{User: *admin, IsGlobalAdmin: true}, service.UpdateUserRequest{ID: ivo.ID,
		Username: ivo.Username, FullName: ivo.FullName, OrgType: ivo.OrgType, Email: ivo.Email, IsActive: true, PotvrdaAdrese: &potvrdi}); err != nil {
		t.Fatal(err)
	}
	b := o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodGet, "/users/"+ivo.ID.String()+"/edit", nil).Body.String()
	skripta, adresa, potvrdena, okvir := skriptaOkvira.FindStringSubmatch(b), adresaObrasca.FindStringSubmatch(b), potvrdenaOkvir.FindStringSubmatch(b), okvirPotvrde.FindStringSubmatch(b)
	if skripta == nil || adresa == nil || potvrdena == nil || okvir == nil || okvir[1] == "" {
		t.Fatalf("obrazac bez skripte, adrese ili označenog okvira: %v %v %v %v", skripta != nil, adresa, potvrdena, okvir)
	}

	// koraci: upis adrese ili klik na okvir; nakon svakog stanje okvira
	koraci := [][]string{
		{"upis", "ivo@bistra.h"},    // adresa promijenjena: odznačen
		{"upis", " Ivo@Bistra.hr "}, // vraćena na potvrđenu: opet označen
		{"klik"},                    // administrator ga sam odznači
		{"upis", "ivo@bistra.hrx"},  // druga adresa
		{"upis", "ivo@bistra.hr"},   // natrag na potvrđenu: ostaje odznačen
		{"klik"},                    // i sam ga opet označi
	}
	zeli := []bool{false, true, false, false, false, true}
	js := func(v any) string {
		s, _ := json.Marshal(v)
		return string(s)
	}
	kod := `const slusaci = {};
const element = (id, svojstva) => Object.assign({addEventListener: (tip, f) => (slusaci[id + ':' + tip] = slusaci[id + ':' + tip] || []).push(f)}, svojstva);
const polja = {email: element('email', {value: ` + js(html.UnescapeString(adresa[1])) + `}),
  pin_adresa_potvrdena: element('pin_adresa_potvrdena', {checked: true, dataset: {potvrdena: ` + js(html.UnescapeString(potvrdena[1])) + `}})};
globalThis.document = {getElementById: (id) => polja[id] || null};
` + skripta[1] + `
const javi = (id, tip) => (slusaci[id + ':' + tip] || []).forEach((f) => f());
const stanja = [];
for (const [radnja, vrijednost] of ` + js(koraci) + `) {
  if (radnja === 'upis') { polja.email.value = vrijednost; javi('email', 'input'); }
  else { polja.pin_adresa_potvrdena.checked = !polja.pin_adresa_potvrdena.checked; javi('pin_adresa_potvrdena', 'change'); }
  stanja.push(polja.pin_adresa_potvrdena.checked);
}
console.log(JSON.stringify(stanja));
`
	put := filepath.Join(t.TempDir(), "okvir.js")
	if err := os.WriteFile(put, []byte(kod), 0o644); err != nil {
		t.Fatal(err)
	}
	izlaz, err := exec.Command(node, put).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, izlaz)
	}
	var stanja []bool
	if err := json.Unmarshal(izlaz, &stanja); err != nil {
		t.Fatalf("izlaz skripte: %v\n%s", err, izlaz)
	}
	if js(stanja) != js(zeli) {
		t.Fatalf("okvir nakon koraka: %v, želi se %v", stanja, zeli)
	}
}

// Novi račun: okvir je na obrascu globalnog administratora i potvrđuje
// upisanu adresu.
func TestNoviDjelatnikSPotvrdenomAdresom(t *testing.T) {
	o := novaOkolinaIzvana(t)
	o.mux.HandleFunc("GET /users/new", o.usersH.ShowUserForm)
	o.mux.HandleFunc("POST /users/create", o.usersH.HandleCreateUser)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	w := o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodGet, "/users/new", nil)
	if m := okvirPotvrde.FindStringSubmatch(w.Body.String()); m == nil || m[1] != "" {
		t.Fatalf("obrazac novog djelatnika: okvir %v", m)
	}
	w = o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/create", url.Values{"username": {"luka"}, "password": {"pocetna-lozinka"},
		"full_name": {"Luka Lukić"}, "org_type": {string(models.OrgPravnaOsoba)}, "email": {"luka@bistra.hr"}, "pin_adresa_potvrdena": {"1"}})
	if g := greskaPreusmjerenja(t, w); g != "" {
		t.Fatalf("novi djelatnik: %s", g)
	}
	u, _ := o.repo.GetUserByUsername("luka")
	if u == nil || !u.PotvrdaAdreseVrijedi() || u.PINAdresuPotvrdio != "Uprava" {
		t.Fatalf("novi djelatnik bez potvrde: %+v", u)
	}
}

// Stanje adrese za PIN: na stranici djelatnika za administratora (s imenom
// onoga tko je potvrdio), ne i za kolegu koji njome ne upravlja; na profilu
// osoba vidi tko joj je potvrdio adresu ili da je treba potvrditi.
func TestStanjeAdreseNaStraniciIProfilu(t *testing.T) {
	o := novaOkolinaIzvana(t)
	o.mux.HandleFunc("GET /users/{id}", o.usersH.ShowUser)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ivo := o.racun("ivo", "ivina-lozinka", "ivo@bistra.hr", false)
	ana := o.racun("ana", "anina-lozinka", "ana@bistra.hr", false)
	potvrdi := true
	if _, err := o.users.UpdateUser(&models.UserPermissions{User: *admin, IsGlobalAdmin: true}, service.UpdateUserRequest{ID: ivo.ID,
		Username: ivo.Username, FullName: ivo.FullName, OrgType: ivo.OrgType, Email: ivo.Email, IsActive: true, PotvrdaAdrese: &potvrdi}); err != nil {
		t.Fatal(err)
	}

	tijelo := func(z zahtjevIzvana, put string) string {
		t.Helper()
		w := o.posalji(z, http.MethodGet, put, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d", put, w.Code)
		}
		return w.Body.String()
	}
	if b := tijelo(zahtjevIzvana{stvarni: admin}, "/users/"+ivo.ID.String()); !strings.Contains(b, "potvrdio ju je administrator Uprava") {
		t.Fatal("administrator ne vidi tko je potvrdio adresu")
	}
	if b := tijelo(zahtjevIzvana{stvarni: admin}, "/users/"+ana.ID.String()); !strings.Contains(b, "a administrator je nije potvrdio") || !strings.Contains(b, "potvrdite je u uređivanju profila") {
		t.Fatal("administrator ne vidi da adresa nije potvrđena")
	}
	if b := tijelo(zahtjevIzvana{stvarni: admin}, "/users/"+admin.ID.String()); !strings.Contains(b, "ide na ovu adresu (@voda.hr)") {
		t.Fatal("adresa u domeni")
	}
	if b := tijelo(zahtjevIzvana{stvarni: ana}, "/users/"+ivo.ID.String()); strings.Contains(b, `id="pin-adresa"`) || strings.Contains(b, "Uprava") {
		t.Fatal("kolega vidi stanje tuđe adrese za PIN")
	}

	if b := tijelo(zahtjevIzvana{stvarni: ivo}, "/profile"); !strings.Contains(b, "Adresu izvan @voda.hr potvrdio je administrator Uprava") || !strings.Contains(b, "i***@bistra.hr") {
		t.Fatal("profil ne kaže tko je potvrdio adresu")
	}
	if b := tijelo(zahtjevIzvana{stvarni: ana}, "/profile"); !strings.Contains(b, "zamolite administratora da je potvrdi") {
		t.Fatal("profil nepotvrđene adrese ne kaže što učiniti")
	}
	if p, _ := porukaRazloga("domena"); !strings.Contains(p, "zamolite administratora da je potvrdi") {
		t.Fatalf("stranica PIN-a: %q", p)
	}
}
