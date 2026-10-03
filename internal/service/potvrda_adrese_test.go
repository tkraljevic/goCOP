package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Adresa koju potvrdi administrator: PIN ide i na adresu izvan dopuštene
// domene kad ju je globalni administrator potvrdio na tuđem računu, i samo
// dok je adresa ista.

func oznaceno(b bool) *bool { return &b }

// izmjena je obrazac djelatnika s podacima računa, novom adresom i okvirom
// potvrde (nil: obrazac bez okvira)
func izmjena(u *models.User, email string, potvrda *bool) service.UpdateUserRequest {
	return service.UpdateUserRequest{ID: u.ID, Username: u.Username, FullName: u.FullName, OrgType: u.OrgType,
		Email: email, IsActive: u.IsActive, IsGlobalAdmin: u.IsGlobalAdmin, PotvrdaAdrese: potvrda}
}

// iz čita račun iz baze
func (o *okolinaPIN) iz(t *testing.T, id uuid.UUID) *models.User {
	t.Helper()
	u, err := o.repo.GetUserByID(id)
	if err != nil || u == nil {
		t.Fatalf("račun %s: %v", id, err)
	}
	return u
}

// potvrdi potvrdi adresu računa kao globalni administrator
func (o *okolinaPIN) potvrdi(t *testing.T, u *models.User) *models.User {
	t.Helper()
	if _, err := o.users.UpdateUser(o.admin, izmjena(o.iz(t, u.ID), u.Email, oznaceno(true))); err != nil {
		t.Fatalf("potvrda adrese %s: %v", u.Email, err)
	}
	return o.iz(t, u.ID)
}

// prijava otvori prijavu izvana i javi je li PIN poslan i zašto nije
func (o *okolinaPIN) prijava(t *testing.T, u *models.User) *service.PocetakPrijave {
	t.Helper()
	p, err := o.dk.ZapocniPrijavu(context.Background(), u, "203.0.113.9", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// (a) PIN ide na potvrđenu adresu izvan domene; (b) na nepotvrđenu ne ide.
func TestPINNaPotvrdenuAdresuIzvanDomene(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	ivo := o.korisnik(t, "ivo", "Ivo.Ivic@Bistra.hr")
	ana := o.korisnik(t, "ana", "ana@bistra.hr")

	prije := o.posta.broj()
	if p := o.prijava(t, ivo); p.PINPoslan || !errors.Is(p.Razlog, service.ErrAdresaNijeDopustena) {
		t.Fatalf("nepotvrđena adresa izvan domene: %+v", p)
	}
	if o.posta.broj() != prije {
		t.Fatal("PIN je poslan na nepotvrđenu adresu izvan domene")
	}

	prijePotvrde := time.Now().Add(-time.Second)
	ivo = o.potvrdi(t, ivo)
	if !ivo.PotvrdaAdreseVrijedi() || ivo.PINAdresaPotvrdena != "Ivo.Ivic@Bistra.hr" || ivo.PINAdresuPotvrdio != "uprava" ||
		ivo.PINAdresaPotvrdenaKad == nil || ivo.PINAdresaPotvrdenaKad.Before(prijePotvrde) {
		t.Fatalf("potvrda nije zapisana: %q %q %v", ivo.PINAdresaPotvrdena, ivo.PINAdresuPotvrdio, ivo.PINAdresaPotvrdenaKad)
	}
	p := o.prijava(t, ivo)
	if !p.PINPoslan || p.Razlog != nil || p.Adresa != "I***@Bistra.hr" {
		t.Fatalf("PIN na potvrđenu adresu: %+v", p)
	}
	if m := o.posta.zadnja(t); m.Za.Address != "Ivo.Ivic@Bistra.hr" || !m.BezKopije {
		t.Fatalf("PIN otišao na %+v", m)
	}
	ishod, err := o.dk.ProvjeriKod(ctx, p.Token, o.posta.pin(t))
	if err != nil || ishod.Korisnik == nil || ishod.Korisnik.ID != ivo.ID || ishod.Vrsta != service.VrstaPIN {
		t.Fatalf("PIN s potvrđene adrese: %+v, %v", ishod, err)
	}
	if s, err := o.dk.StanjeAdrese(ctx, ivo); err != nil || s.Vrsta != service.AdresaPotvrdena || s.Potvrdio != "uprava" || s.PotvrdenoTekst() == "" || !s.IdePIN() {
		t.Fatalf("stanje potvrđene adrese: %+v, %v", s, err)
	}

	// Anina adresa u istoj tvrtki nije potvrđena: potvrda je po računu
	prije = o.posta.broj()
	if p := o.prijava(t, ana); p.PINPoslan || !errors.Is(p.Razlog, service.ErrAdresaNijeDopustena) {
		t.Fatalf("nepotvrđena adresa: %+v", p)
	}
	if o.posta.broj() != prije {
		t.Fatal("PIN je poslan na nepotvrđenu adresu")
	}
	if s, err := o.dk.StanjeAdrese(ctx, ana); err != nil || s.Vrsta != service.AdresaNepotvrdena || s.IdePIN() || s.Potvrdio != "" {
		t.Fatalf("stanje nepotvrđene adrese: %+v, %v", s, err)
	}

	// filtar „bez adrese za PIN” hvata nepotvrđenu, a ne potvrđenu adresu
	popis, err := o.users.ListUsers("", 0, "", "", string(repository.StanjeBezEposte))
	if err != nil {
		t.Fatal(err)
	}
	var imena []string
	for _, u := range popis {
		imena = append(imena, u.Username)
	}
	if got := strings.Join(imena, ","); got != "ana" {
		t.Fatalf("bez adrese za PIN: %s", got)
	}
}

// (c) Svaka promjena adrese bez nove potvrde poništava potvrdu: globalni
// administrator bez okvira, administrator sektora i osoba sama. Vlastita
// promjena potvrdu briše, pa ne oživi ni povratkom na staru adresu, a stara
// potvrđena adresa dobije obavijest.
func TestPromjenaAdresePonistavaPotvrdu(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	if _, err := o.baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`); err != nil {
		t.Fatal(err)
	}
	hash, _ := o.auth.HashPassword("lozinka1")
	sektorB := "B"
	ivo := &models.User{Username: "ivo", PasswordHash: hash, FullName: "Ivo Ivić", Email: "ivo@bistra.hr", IsActive: true, OrgType: models.OrgPravnaOsoba}
	if err := o.repo.CreateUser(ivo, &models.Duty{Title: "Vodočuvar", Role: models.RoleWaterGuard, ScopeType: models.ScopeSector, SectorID: &sektorB}); err != nil {
		t.Fatal(err)
	}
	sektor := o.korisnik(t, "sektor", "sektor@voda.hr")
	upravitelj := &models.UserPermissions{User: *sektor, AdminSectors: map[string]bool{"B": true}}
	nijePoslan := func(opis string) {
		t.Helper()
		if p := o.prijava(t, o.iz(t, ivo.ID)); p.PINPoslan || !errors.Is(p.Razlog, service.ErrAdresaNijeDopustena) {
			t.Fatalf("%s: PIN i dalje ide na adresu: %+v", opis, p)
		}
	}

	// globalni administrator mijenja adresu bez okvira (adresar, stari obrazac)
	o.potvrdi(t, ivo)
	if _, err := o.users.UpdateUser(o.admin, izmjena(o.iz(t, ivo.ID), "ivo.novi@bistra.hr", nil)); err != nil {
		t.Fatal(err)
	}
	if u := o.iz(t, ivo.ID); u.PotvrdaAdreseVrijedi() || u.PINAdresaPotvrdena != "ivo@bistra.hr" {
		t.Fatalf("nova adresa bez potvrde: vrijedi %v, zapisano %q", u.PotvrdaAdreseVrijedi(), u.PINAdresaPotvrdena)
	}
	nijePoslan("administrator bez okvira")

	// globalni administrator s neoznačenim okvirom potvrdu briše
	o.potvrdi(t, o.iz(t, ivo.ID))
	if _, err := o.users.UpdateUser(o.admin, izmjena(o.iz(t, ivo.ID), "ivo.novi@bistra.hr", oznaceno(false))); err != nil {
		t.Fatal(err)
	}
	if u := o.iz(t, ivo.ID); u.PINAdresaPotvrdena != "" || u.PINAdresuPotvrdio != "" || u.PINAdresaPotvrdenaKad != nil {
		t.Fatalf("neoznačen okvir nije obrisao potvrdu: %+v", u)
	}
	nijePoslan("neoznačen okvir")

	// administrator sektora mijenja adresu: potvrda ostaje zapisana, ali ne vrijedi
	o.potvrdi(t, o.iz(t, ivo.ID))
	if _, err := o.users.UpdateUser(upravitelj, izmjena(o.iz(t, ivo.ID), "ivo.treci@bistra.hr", nil)); err != nil {
		t.Fatalf("administrator sektora: %v", err)
	}
	if u := o.iz(t, ivo.ID); u.Email != "ivo.treci@bistra.hr" || u.PotvrdaAdreseVrijedi() {
		t.Fatalf("administrator sektora: adresa %q, potvrda vrijedi %v", u.Email, u.PotvrdaAdreseVrijedi())
	}
	nijePoslan("administrator sektora")

	// administrator sektora bez promjene adrese potvrdu ne dira
	o.potvrdi(t, o.iz(t, ivo.ID))
	x := izmjena(o.iz(t, ivo.ID), "ivo.treci@bistra.hr", nil)
	x.Phone = "031-222"
	if _, err := o.users.UpdateUser(upravitelj, x); err != nil {
		t.Fatal(err)
	}
	if !o.iz(t, ivo.ID).PotvrdaAdreseVrijedi() {
		t.Fatal("izmjena telefona poništila je potvrdu")
	}

	// osoba sama: na adresu izvan domene ne može ni s potvrđenom adresom
	ja := &models.UserPermissions{User: *o.iz(t, ivo.ID)}
	vlastita := func(email string) service.UpdateUserRequest {
		r := izmjena(o.iz(t, ivo.ID), email, nil)
		r.TrenutnaLozinka = "lozinka1"
		return r
	}
	_, err := o.users.UpdateUser(ja, vlastita("ivo@drugafirma.hr"))
	var izvan service.AdresaIzvanDomene
	if !errors.Is(err, service.ErrAdresaNijeDopustena) || !errors.As(err, &izvan) || !izvan.Vlastita || !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("vlastita adresa izvan domene: %v", err)
	}
	// na službenu adresu može, i time briše potvrdu
	if _, err := o.users.UpdateUser(ja, vlastita("ivo.ivic@voda.hr")); err != nil {
		t.Fatal(err)
	}
	if u := o.iz(t, ivo.ID); u.PINAdresaPotvrdena != "" || u.PINAdresuPotvrdio != "" || u.PINAdresaPotvrdenaKad != nil {
		t.Fatalf("vlastita promjena nije obrisala potvrdu: %+v", u)
	}
	ceka(t, o, "ivo.treci@bistra.hr", "ivo.ivic@voda.hr")
	// povratak na staru adresu (administrator bez okvira) potvrdu ne vraća
	if _, err := o.users.UpdateUser(o.admin, izmjena(o.iz(t, ivo.ID), "ivo.treci@bistra.hr", nil)); err != nil {
		t.Fatal(err)
	}
	nijePoslan("povratak nakon vlastite promjene")
	if s, err := o.dk.StanjeAdrese(ctx, o.iz(t, ivo.ID)); err != nil || s.Vrsta != service.AdresaNepotvrdena {
		t.Fatalf("stanje nakon vlastite promjene: %+v, %v", s, err)
	}
}

// ceka da obavijest o promjeni adrese stigne na staru adresu
func ceka(t *testing.T, o *okolinaPIN, stara, nova string) {
	t.Helper()
	rok := time.Now().Add(5 * time.Second)
	for {
		o.posta.mu.Lock()
		var m *posta.Poruka
		for i := range o.posta.poruke {
			if o.posta.poruke[i].Za.Address == stara {
				m = &o.posta.poruke[i]
			}
		}
		o.posta.mu.Unlock()
		if m != nil {
			if !strings.Contains(m.Tekst, nova) {
				t.Fatalf("obavijest: %+v", m)
			}
			return
		}
		if time.Now().After(rok) {
			t.Fatalf("stara potvrđena adresa %s nije dobila obavijest", stara)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// (d) Potvrdu daje i uklanja samo globalni administrator, i to na tuđem
// računu; ni administrator sektora, ni osoba sama, ni administrator sebi.
func TestPotvrduDajeSamoGlobalniAdministrator(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	if _, err := o.baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`); err != nil {
		t.Fatal(err)
	}
	sektorB := "B"
	ivo := &models.User{Username: "ivo", FullName: "Ivo Ivić", Email: "ivo@bistra.hr", IsActive: true, OrgType: models.OrgPravnaOsoba}
	if err := o.repo.CreateUser(ivo, &models.Duty{Title: "Vodočuvar", Role: models.RoleWaterGuard, ScopeType: models.ScopeSector, SectorID: &sektorB}); err != nil {
		t.Fatal(err)
	}
	sektor := o.korisnik(t, "sektor", "sektor@bistra.hr")
	upravitelj := &models.UserPermissions{User: *sektor, AdminSectors: map[string]bool{"B": true}}

	for _, c := range []struct {
		opis   string
		actor  *models.UserPermissions
		target uuid.UUID
		odluka bool
	}{
		{"administrator sektora potvrđuje", upravitelj, ivo.ID, true},
		{"administrator sektora uklanja", upravitelj, ivo.ID, false},
		{"osoba sama sebi", &models.UserPermissions{User: *o.iz(t, ivo.ID)}, ivo.ID, true},
		{"globalni administrator sebi", o.admin, o.admin.User.ID, true},
	} {
		if _, err := o.users.UpdateUser(c.actor, izmjena(o.iz(t, c.target), o.iz(t, c.target).Email, oznaceno(c.odluka))); !errors.Is(err, service.ErrPotvrdaAdrese) || !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: %v", c.opis, err)
		}
	}
	// administrator sektora ne briše ni potvrdu koju je dao globalni
	o.potvrdi(t, ivo)
	if _, err := o.users.UpdateUser(upravitelj, izmjena(o.iz(t, ivo.ID), "ivo@bistra.hr", oznaceno(false))); !errors.Is(err, service.ErrPotvrdaAdrese) {
		t.Fatalf("administrator sektora briše potvrdu: %v", err)
	}
	if u := o.iz(t, ivo.ID); !u.PotvrdaAdreseVrijedi() || u.PINAdresuPotvrdio != "uprava" {
		t.Fatalf("potvrda se promijenila: %+v", u)
	}
	// globalni administrator svoju adresu izvan domene ne potvrđuje
	if u := o.iz(t, o.admin.User.ID); u.PINAdresaPotvrdena != "" {
		t.Fatalf("administrator je potvrdio svoju adresu: %q", u.PINAdresaPotvrdena)
	}

	// novi račun: ista pravila
	novi := func(actor *models.UserPermissions, ime string, uloga models.Role) (*models.User, error) {
		return o.users.CreateUser(actor, service.CreateUserRequest{Username: ime, Password: "pocetna-1", FullName: ime,
			OrgType: models.OrgPravnaOsoba, Email: ime + "@bistra.hr", Role: uloga, SectorID: &sektorB, PotvrdaAdrese: true})
	}
	if _, err := novi(upravitelj, "marko", models.RoleSectorDeputy); !errors.Is(err, service.ErrPotvrdaAdrese) {
		t.Fatalf("administrator sektora potvrđuje novom računu: %v", err)
	}
	if u, _ := o.repo.GetUserByUsername("marko"); u != nil {
		t.Fatal("račun je otvoren uz odbijenu potvrdu")
	}
	stvoren, err := novi(o.admin, "luka", "")
	if err != nil {
		t.Fatal(err)
	}
	if u := o.iz(t, stvoren.ID); !u.PotvrdaAdreseVrijedi() || u.PINAdresuPotvrdio != "uprava" || u.PINAdresaPotvrdenaKad == nil {
		t.Fatalf("novi račun bez potvrde: %+v", u)
	}
	// prazna adresa nema potvrde
	prazan, err := o.users.CreateUser(o.admin, service.CreateUserRequest{Username: "bezadrese", Password: "pocetna-1", FullName: "Bez adrese",
		OrgType: models.OrgPravnaOsoba, PotvrdaAdrese: true})
	if err != nil {
		t.Fatal(err)
	}
	if u := o.iz(t, prazan.ID); u.PINAdresaPotvrdena != "" || u.PINAdresaPotvrdenaKad != nil {
		t.Fatalf("prazna adresa s potvrdom: %+v", u)
	}
}

// Tuđim očima potvrda se ne daje ni ne uklanja, kao ni ostale radnje s
// PIN-om: administrator koji gleda očima drugog globalnog administratora
// tako bi potvrdio svoju adresu, a potvrda bi se pripisala tom drugome.
func TestPotvrdaNeTudjimOcima(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	drugi := o.korisnik(t, "glavni", "glavni@voda.hr")
	ociDrugog := &models.UserPermissions{IsGlobalAdmin: true, User: *drugi}
	ivo := o.potvrdi(t, o.korisnik(t, "ivo", "ivo@bistra.hr"))
	ana := o.korisnik(t, "ana", "ana@bistra.hr")
	tudjimOcima := func(z service.UpdateUserRequest) service.UpdateUserRequest {
		z.TudjimOcima = true
		return z
	}

	// svoja adresa, pod tuđim očima: ni potvrda ni promjena adrese
	svoj := tudjimOcima(izmjena(o.iz(t, o.admin.User.ID), "uprava@privatno.hr", oznaceno(true)))
	if _, err := o.users.UpdateUser(ociDrugog, svoj); !errors.Is(err, service.ErrTudjimOcima) {
		t.Fatalf("svoja adresa tuđim očima: %v", err)
	}
	if u := o.iz(t, o.admin.User.ID); u.Email != "uprava@voda.hr" || u.PINAdresaPotvrdena != "" || u.PINAdresuPotvrdio != "" {
		t.Fatalf("svoja adresa potvrđena tuđim očima: %+v", u)
	}
	// tuđi račun: ni potvrda ni uklanjanje
	if _, err := o.users.UpdateUser(ociDrugog, tudjimOcima(izmjena(o.iz(t, ana.ID), ana.Email, oznaceno(true)))); !errors.Is(err, service.ErrTudjimOcima) {
		t.Fatalf("potvrda tuđim očima: %v", err)
	}
	if u := o.iz(t, ana.ID); u.PINAdresaPotvrdena != "" {
		t.Fatalf("potvrda dana tuđim očima: %+v", u)
	}
	if _, err := o.users.UpdateUser(ociDrugog, tudjimOcima(izmjena(o.iz(t, ivo.ID), ivo.Email, oznaceno(false)))); !errors.Is(err, service.ErrTudjimOcima) {
		t.Fatalf("uklanjanje tuđim očima: %v", err)
	}
	if u := o.iz(t, ivo.ID); !u.PotvrdaAdreseVrijedi() || u.PINAdresuPotvrdio != "uprava" {
		t.Fatalf("potvrda uklonjena tuđim očima: %+v", u)
	}
	// ostale izmjene tuđim očima idu, a potvrda ostaje kakva je
	bez := tudjimOcima(izmjena(o.iz(t, ivo.ID), ivo.Email, nil))
	bez.Phone = "031-222"
	if _, err := o.users.UpdateUser(ociDrugog, bez); err != nil {
		t.Fatalf("izmjena bez okvira tuđim očima: %v", err)
	}
	if u := o.iz(t, ivo.ID); !u.PotvrdaAdreseVrijedi() || u.Phone != "031-222" {
		t.Fatalf("izmjena bez okvira: %+v", u)
	}
	// novi račun tuđim očima ne dobiva potvrdu
	if _, err := o.users.CreateUser(ociDrugog, service.CreateUserRequest{Username: "luka", Password: "pocetna-1", FullName: "Luka",
		OrgType: models.OrgPravnaOsoba, Email: "luka@bistra.hr", PotvrdaAdrese: true, TudjimOcima: true}); !errors.Is(err, service.ErrTudjimOcima) {
		t.Fatalf("novi račun s potvrdom tuđim očima: %v", err)
	}
	if u, _ := o.repo.GetUserByUsername("luka"); u != nil {
		t.Fatal("račun je otvoren uz potvrdu tuđim očima")
	}
	// obrazac okvir nudi samo svojim očima
	if service.SmijePotvrditiAdresu(ociDrugog, o.admin.User.ID, true) || service.SmijePotvrditiAdresu(ociDrugog, ana.ID, true) ||
		!service.SmijePotvrditiAdresu(ociDrugog, ana.ID, false) {
		t.Fatal("okvir potvrde tuđim očima")
	}
}

// (g) Zajednička adresa ne dobiva PIN ni kad je potvrđena.
func TestZajednickaPotvrdenaAdresaBezPINa(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	prvi := o.potvrdi(t, o.korisnik(t, "prvi", "ured@bistra.hr"))
	o.potvrdi(t, o.korisnik(t, "drugi", " Ured@Bistra.hr"))
	prije := o.posta.broj()
	if p := o.prijava(t, prvi); p.PINPoslan || !errors.Is(p.Razlog, service.ErrZajednickaAdresa) {
		t.Fatalf("zajednička potvrđena adresa: %+v", p)
	}
	if o.posta.broj() != prije {
		t.Fatal("PIN je poslan na zajedničku adresu")
	}
	s, err := o.dk.StanjeAdrese(context.Background(), prvi)
	if err != nil || s.Vrsta != service.AdresaZajednicka || s.Potvrdio != "uprava" || s.IdePIN() {
		t.Fatalf("stanje zajedničke potvrđene adrese: %+v, %v", s, err)
	}
}
