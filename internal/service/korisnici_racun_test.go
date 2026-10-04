package service_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Testovi UserService.CreateUser i UpdateUser: tko smije otvoriti račun i
// s kojom dužnošću, obavezni podaci, zauzeto ime i adresa, što osoba smije
// mijenjati na sebi, a što uprava na drugima, lozinka i zastavica
// globalnog administratora. Okolina je okolinaOvlasti (sektori A i B,
// područja 5, 16 i 17); jedina osoba je Pero Perić (pperic).

// korUprava otvara račun s jednom upravnom dužnošću i vraća njegove ovlasti
func korUprava(t *testing.T, o *okolinaOvlasti, ime string, d service.AddDutyRequest) *models.UserPermissions {
	t.Helper()
	u := o.osoba(t, ime, d)
	return o.ovlasti(t, u.ID)
}

func korZahtjev(ime string) service.CreateUserRequest {
	return service.CreateUserRequest{Username: ime, Password: "lozinka-" + ime, FullName: "Pero Perić", Email: ime + "@voda.hr",
		Role: models.RoleAreaLeader, AreaID: podrucjeP(16)}
}

func TestNoviRacunOdbijanja(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	upravaA := korUprava(t, o, "uprava-a", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("A")})
	upravaB := korUprava(t, o, "uprava-b", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	sutra := time.Now().Add(24 * time.Hour)
	privremenaDrzavna := korUprava(t, o, "zamjenik-drzave", service.AddDutyRequest{Role: models.RoleNationalDeputy, IsTemporary: true, ExpiresAt: &sutra})
	promatrac := korUprava(t, o, "promatrac", service.AddDutyRequest{Role: models.RoleViewer, AreaID: podrucjeP(16)})

	bezUloge := korZahtjev("pperic-bez")
	bezUloge.Role, bezUloge.AreaID = "", nil
	globalni := korZahtjev("pperic-admin")
	globalni.IsGlobalAdmin = true
	tudjiSektor := korZahtjev("pperic-tudji")
	bezLozinke := korZahtjev("pperic-bez-lozinke")
	bezLozinke.Password = ""
	bezImena := korZahtjev("pperic-bez-imena")
	bezImena.FullName = "   "
	for _, s := range []struct {
		ime    string
		actor  *models.UserPermissions
		req    service.CreateUserRequest
		greska error
		poruka string
	}{
		{"bez ovlasti", nil, korZahtjev("pperic-1"), service.ErrUnauthorized, ""},
		{"promatrač ne upravlja", promatrac, korZahtjev("pperic-2"), service.ErrUnauthorized, ""},
		{"zastavica od uprave sektora", upravaB, globalni, service.ErrUnauthorized, "stalna uprava"},
		{"zastavica od privremene uprave države", privremenaDrzavna, globalni, service.ErrUnauthorized, "stalna uprava"},
		{"račun bez dužnosti od uprave sektora", upravaB, bezUloge, service.ErrUnauthorized, "bez dužnosti"},
		{"dužnost u tuđem sektoru", upravaA, tudjiSektor, service.ErrUnauthorized, ""},
		{"bez lozinke", upravaB, bezLozinke, service.ErrInvalidUserData, "obavezni"},
		{"ime od razmaka", upravaB, bezImena, service.ErrInvalidUserData, "obavezni"},
		// zauzeto korisničko ime ne gleda velika i mala slova
		{"zauzeto ime", upravaB, korZahtjev("UPRAVA-A"), service.ErrUsernameExists, ""},
		{"zauzeta adresa", upravaB, func() service.CreateUserRequest {
			r := korZahtjev("pperic-3")
			r.Email = " Uprava-A@voda.hr "
			return r
		}(), service.ErrAdresaZauzeta, "drugi aktivni račun"},
		{"nepostojeća dionica", upravaB, func() service.CreateUserRequest {
			r := korZahtjev("pperic-4")
			r.Role, r.AreaID, r.SectionCodes = models.RoleSectionLeader, nil, "B.99.9"
			return r
		}(), nil, "B.99.9"},
	} {
		u, err := o.users.CreateUser(s.actor, s.req)
		if u != nil || err == nil || (s.greska != nil && !errors.Is(err, s.greska)) || !strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: %v (%v)", s.ime, err, u)
		}
	}
}

func TestNoviRacun(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	upravaB := korUprava(t, o, "uprava-b", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})

	req := korZahtjev("  pperic  ")
	req.FullName, req.Password = "  Pero Perić ", "lozinka-pperic"
	u, err := o.users.CreateUser(upravaB, req)
	if err != nil {
		t.Fatal(err)
	}
	svjez, _ := o.repo.GetUserByID(u.ID)
	// lozinku je odabrala uprava, pa je osoba mora promijeniti
	if svjez.Username != "pperic" || svjez.FullName != "Pero Perić" || !svjez.IsActive || !svjez.MustChangePassword || svjez.IsGlobalAdmin {
		t.Errorf("račun: %+v", svjez)
	}
	if len(svjez.Duties) != 1 {
		t.Fatalf("dužnosti: %+v", svjez.Duties)
	}
	d := svjez.Duties[0]
	// naziv dužnosti iz uloge, doseg i sektor iz područja, primarna i stalna
	if d.Title != models.RoleAreaLeader.Label() || d.ScopeType != models.ScopeArea || d.SectorID == nil || *d.SectorID != "B" ||
		!d.IsPrimary || d.IsTemporary || d.ExpiresAt != nil {
		t.Errorf("dužnost: %+v", d)
	}
	if _, _, err := o.auth.Login("pperic", "lozinka-pperic", "192.168.1.5", "test"); err != nil {
		t.Errorf("prijava novim računom: %v", err)
	}

	// Privremena uprava sektora daje upravu sektora najdulje do svog isteka.
	za10dana := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Second)
	privremena := korUprava(t, o, "privremena-b", service.AddDutyRequest{Role: models.RoleSectorDeputy, SectorID: sektorP("B"), IsTemporary: true, ExpiresAt: &za10dana})
	sektorska := korZahtjev("pperic-privremeni")
	sektorska.Role, sektorska.AreaID, sektorska.SectorID = models.RoleSectorDeputy, nil, sektorP("B")
	u2, err := o.users.CreateUser(privremena, sektorska)
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := o.repo.GetUserByID(u2.ID)
	if len(s2.Duties) != 1 || !s2.Duties[0].IsTemporary || s2.Duties[0].ExpiresAt == nil || !s2.Duties[0].ExpiresAt.Equal(za10dana) ||
		!strings.Contains(s2.Duties[0].Reason, "privremene uprave") {
		t.Errorf("dužnost od privremene uprave: %+v", s2.Duties)
	}
	// I upravu područja (razinu niže) ista privremena uprava daje najdulje
	// do svog isteka: kad joj istekne ovlast, ne ostaje uprava koju je dala.
	u5, err := o.users.CreateUser(privremena, korZahtjev("pperic-podrucje"))
	if err != nil {
		t.Fatal(err)
	}
	if s5, _ := o.repo.GetUserByID(u5.ID); !s5.Duties[0].IsTemporary || s5.Duties[0].ExpiresAt == nil || !s5.Duties[0].ExpiresAt.Equal(za10dana) {
		t.Errorf("uprava područja od privremene uprave sektora: %+v", s5.Duties[0])
	}
	// terensku dužnost daje kakvu je tražila
	teren := korZahtjev("pperic-vodocuvar")
	teren.Role = models.RoleWaterGuard
	u6, err := o.users.CreateUser(privremena, teren)
	if err != nil {
		t.Fatal(err)
	}
	if s6, _ := o.repo.GetUserByID(u6.ID); s6.Duties[0].IsTemporary || s6.Duties[0].ExpiresAt != nil {
		t.Errorf("vodočuvar od privremene uprave sektora: %+v", s6.Duties[0])
	}

	// globalni administrator otvara račun bez dužnosti i daje zastavicu
	bez := korZahtjev("pperic-admin")
	bez.Role, bez.AreaID, bez.IsGlobalAdmin = "", nil, true
	u3, err := o.users.CreateUser(o.admin, bez)
	if err != nil {
		t.Fatal(err)
	}
	if s3, _ := o.repo.GetUserByID(u3.ID); !s3.IsGlobalAdmin || len(s3.Duties) != 0 {
		t.Errorf("račun bez dužnosti: %+v", s3)
	}
	// zadani naziv dužnosti ostaje
	sNazivom := korZahtjev("pperic-naziv")
	sNazivom.DutyTitle = "Rukovoditelj obrane u Primjerovu"
	u4, err := o.users.CreateUser(o.admin, sNazivom)
	if err != nil {
		t.Fatal(err)
	}
	if s4, _ := o.repo.GetUserByID(u4.ID); s4.Duties[0].Title != "Rukovoditelj obrane u Primjerovu" {
		t.Errorf("naziv dužnosti: %q", s4.Duties[0].Title)
	}
}

func korIzmjena(u *models.User) service.UpdateUserRequest {
	return service.UpdateUserRequest{ID: u.ID, Username: u.Username, FullName: u.FullName, Title: u.Title, IsGlobalAdmin: u.IsGlobalAdmin,
		OrgType: u.OrgType, OrgName: u.OrgName, Phone: u.Phone, Email: u.Email, IsActive: u.IsActive}
}

func TestIzmjenaVlastitogRacuna(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	pero := o.osoba(t, "pperic", service.AddDutyRequest{Role: models.RoleSectionLeader, SectionCodes: "B.16.1"})
	ovl := o.ovlasti(t, pero.ID)
	req := korIzmjena(pero)
	// sam sebi ne mijenja korisničko ime, uključenost ni zastavicu
	req.Username, req.IsActive, req.IsGlobalAdmin = "pero-novi", false, true
	req.FullName, req.Phone, req.OrgType, req.OrgName = " Pero Perić ", "000 000 001", "", ""
	u, err := o.users.UpdateUser(ovl, req)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "pperic" || !u.IsActive || u.IsGlobalAdmin || u.FullName != "Pero Perić" || u.Phone != "000 000 001" || u.OrgType != pero.OrgType {
		t.Errorf("vlastita izmjena: %+v", u)
	}
	// vlastita lozinka ne traži zamjenu pri prijavi
	req = korIzmjena(u)
	req.Password = "nova-perina-lozinka"
	u, err = o.users.UpdateUser(ovl, req)
	if err != nil || u.MustChangePassword {
		t.Errorf("vlastita lozinka: %v, mora mijenjati %v", err, u.MustChangePassword)
	}
	if _, err := o.users.UpdateUser(ovl, service.UpdateUserRequest{ID: uuid.New()}); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("nepostojeći račun: %v", err)
	}
}

func TestIzmjenaTudjegRacuna(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	pero := o.osoba(t, "pperic", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	upravaB := korUprava(t, o, "uprava-b", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	uprava17 := korUprava(t, o, "uprava-17", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(17)})

	// uprava drugog područja ne uređuje
	if _, err := o.users.UpdateUser(uprava17, korIzmjena(pero)); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava drugog područja: %v", err)
	}
	// Uprava sektora uređuje i mijenja korisničko ime, ali zastavicu ne
	// daje, nego zadrži zatečenu.
	req := korIzmjena(pero)
	req.Username, req.IsGlobalAdmin, req.Title = "pperic-16", true, "dipl. ing."
	u, err := o.users.UpdateUser(upravaB, req)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "pperic-16" || u.IsGlobalAdmin || u.Title != "dipl. ing." {
		t.Errorf("izmjena uprave sektora: %+v", u)
	}
	// ime drugoga, i drugim slovima, ne; prazno ne
	req = korIzmjena(u)
	req.Username = "UPRAVA-B"
	if _, err := o.users.UpdateUser(upravaB, req); !errors.Is(err, service.ErrUsernameExists) {
		t.Errorf("tuđe ime: %v", err)
	}
	req.Username = "  "
	if _, err := o.users.UpdateUser(upravaB, req); !errors.Is(err, service.ErrInvalidUserData) {
		t.Errorf("prazno ime: %v", err)
	}
	// tuđa adresa ne
	req = korIzmjena(u)
	req.Email = "uprava-b@voda.hr"
	if _, err := o.users.UpdateUser(upravaB, req); !errors.Is(err, service.ErrAdresaZauzeta) {
		t.Errorf("tuđa adresa: %v", err)
	}

	// Lozinka koju upiše uprava: osoba je mora zamijeniti, a otvorene
	// prijave se gase.
	sesija, _, err := o.auth.Login("pperic-16", "lozinka-pperic", "192.168.1.5", "test")
	if err != nil {
		t.Fatal(err)
	}
	req = korIzmjena(u)
	req.Password = "lozinka-od-uprave"
	if _, err := o.users.UpdateUser(upravaB, req); err != nil {
		t.Fatal(err)
	}
	svjez, _ := o.repo.GetUserByID(u.ID)
	if !svjez.MustChangePassword {
		t.Error("tuđa lozinka ne traži zamjenu")
	}
	if s, _ := repository.NewSessionRepository(o.db).GetSession(sesija.ID); s != nil {
		t.Error("otvorena prijava nije ugašena")
	}
}

func TestUkljucenjeRacunaSTudjomAdresom(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	pero := o.osoba(t, "pperic", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	// isključen račun s adresom koju je u međuvremenu dobio drugi aktivni račun
	if _, err := o.db.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, pero.ID.String()); err != nil {
		t.Fatal(err)
	}
	o.racun(t, "pperic-drugi", "lozinka", "pperic@voda.hr", false, true)
	iskljucen, _ := o.repo.GetUserByID(pero.ID)
	req := korIzmjena(iskljucen)
	req.IsActive = true
	_, err := o.users.UpdateUser(o.admin, req)
	if !errors.Is(err, service.ErrAdresaZauzeta) || !strings.Contains(err.Error(), "račun se ne uključuje") {
		t.Errorf("uključenje s tuđom adresom: %v", err)
	}
	// s drugom adresom se uključi
	req.Email = "pero.peric@voda.hr"
	if u, err := o.users.UpdateUser(o.admin, req); err != nil || !u.IsActive {
		t.Errorf("uključenje s novom adresom: %v", err)
	}
}

func TestZastavicaGlobalnogAdministratoraPriIzmjeni(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	pero := o.osoba(t, "pperic", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	sutra := time.Now().Add(24 * time.Hour)
	privremena := korUprava(t, o, "zamjenik-drzave", service.AddDutyRequest{Role: models.RoleNationalDeputy, IsTemporary: true, ExpiresAt: &sutra})
	if !privremena.IsGlobalAdmin {
		t.Fatal("privremeni zamjenik nije globalni administrator")
	}
	// privremena uprava organizacije zastavicu ne daje
	req := korIzmjena(pero)
	req.IsGlobalAdmin = true
	if _, err := o.users.UpdateUser(privremena, req); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("privremena daje zastavicu: %v", err)
	}
	// stalna daje
	u, err := o.users.UpdateUser(o.admin, req)
	if err != nil || !u.IsGlobalAdmin {
		t.Fatalf("stalna daje zastavicu: %v", err)
	}
	// privremena uprava organizacije zastavicu ni ne skida
	req = korIzmjena(u)
	req.IsGlobalAdmin = false
	if _, err := o.users.UpdateUser(privremena, req); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("privremena skida zastavicu: %v", err)
	}
	if s, _ := o.repo.GetUserByID(u.ID); !s.IsGlobalAdmin {
		t.Error("zastavica je skinuta")
	}
	// stalna je skida
	if u, err = o.users.UpdateUser(o.admin, req); err != nil || u.IsGlobalAdmin {
		t.Errorf("stalna skida zastavicu: %v", err)
	}
}

func TestLozinkaOperateraPriIzmjeni(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	upravaB := korUprava(t, o, "uprava-b", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	operater := o.osoba(t, "pperic", service.AddDutyRequest{Role: models.RoleOperator, SectorID: sektorP("B")})
	// uprava sektora uređuje operatera, ali mu lozinku ne postavlja
	req := korIzmjena(operater)
	req.Phone = "000 000 002"
	if _, err := o.users.UpdateUser(upravaB, req); err != nil {
		t.Fatalf("izmjena operatera: %v", err)
	}
	req.Password = "lozinka-od-uprave"
	if _, err := o.users.UpdateUser(upravaB, req); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("lozinka operatera: %v", err)
	}
	if _, _, err := o.auth.Login("pperic", "lozinka-od-uprave", "192.168.1.5", "test"); err == nil {
		t.Error("odbijena lozinka je ipak upisana")
	}
}
