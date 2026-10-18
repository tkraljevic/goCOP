package service_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// setupTestServices puni bazu iz izmišljenog skupa sektora P
// (testdata/sektor-p): administrator Ivo Ivić, rukovoditelj sektora Pero
// Perić, operaterka Ana Anić, rukovoditeljica dionica Iva Ivić i vodočuvar
// Marko Markić
func setupTestServices(t *testing.T) (*service.UserService, *service.AuthService, *service.SSEBroker, *repository.UserRepository) {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_gocop.db")

	database, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatalf("Greška pri otvaranju test baze: %v", err)
	}

	if err := db.SeedInitialData(database); err != nil {
		t.Fatalf("Greška pri seed podacima: %v", err)
	}

	nodeID := "test-node"
	userRepo := repository.NewUserRepository(database, ledger.New(database, nodeID))
	sessionRepo := repository.NewSessionRepository(database)
	authService := service.NewAuthService(userRepo, sessionRepo)
	sseBroker := service.NewSSEBroker()
	userService := service.NewUserService(userRepo, authService, sseBroker)

	return userService, authService, sseBroker, userRepo
}

// TestSeedAdminProvjera provjerava profil i višestruke funkcije administratora Ive Ivića
func TestSeedAdminProvjera(t *testing.T) {
	_, authService, _, userRepo := setupTestServices(t)

	admin, err := userRepo.GetUserByUsername("iivic")
	if err != nil {
		t.Fatalf("Greška pri dohvatu admina: %v", err)
	}
	if admin == nil {
		t.Fatal("Korisnik 'iivic' nije pronađen u seed podacima")
	}

	if !admin.IsGlobalAdmin {
		t.Error("Očekivano da Ivo Ivić ima IsGlobalAdmin = true")
	}
	if admin.MobilePhone == "" || admin.ShortPhone == "" {
		t.Errorf("Očekivani mobitel i skraćeni lokal iz imenika, dobiveno: %q / %q", admin.MobilePhone, admin.ShortPhone)
	}

	// Provjera višestrukih funkcija
	if len(admin.Duties) < 2 {
		t.Fatalf("Očekivano barem 2 funkcije za Ivu Ivića, pronađeno: %d", len(admin.Duties))
	}

	// Provjera lozinke
	if !authService.CheckPassword(admin.PasswordHash, "gocop2026") {
		t.Error("Provjera lozinke nije uspjela za zadanu lozinku gocop2026")
	}
}

// TestMultiDutyPersonProvjera provjerava Ivu Ivić koja ima i branjeno područje i više dionica
func TestMultiDutyPersonProvjera(t *testing.T) {
	_, _, _, userRepo := setupTestServices(t)

	iva, err := userRepo.GetUserByUsername("ivaivic")
	if err != nil {
		t.Fatalf("Greška pri dohvatu korisnika: %v", err)
	}
	if iva == nil {
		t.Fatal("Korisnik 'ivaivic' nije pronađen u seed podacima")
	}

	if len(iva.Duties) != 2 {
		t.Fatalf("Iva Ivić bi trebala imati točno 2 funkcije, pronađeno: %d", len(iva.Duties))
	}

	perms, err := userRepo.GetUserPermissions(iva.ID)
	if err != nil {
		t.Fatalf("Greška pri dohvatu ovlasti: %v", err)
	}

	// Ima pristup branjenom području 3 (sektor E)
	if !perms.HasWriteAccess("E", 3, "") {
		t.Error("Korisnik bi trebao imati pravo pisanja za branjeno područje 3")
	}

	// Ima pristup dionicama P.1.1 i P.1.2, ali ne i ostatku područja 1
	if !perms.HasWriteAccess("P", 0, "P.1.1") {
		t.Error("Korisnik bi trebao imati pravo pisanja za dionicu P.1.1")
	}
	if !perms.HasWriteAccess("P", 0, "P.1.2") {
		t.Error("Korisnik bi trebao imati pravo pisanja za dionicu P.1.2")
	}
	if perms.HasWriteAccess("P", 1, "P.1.3") {
		t.Error("Rukovoditeljica dionica ne smije pisati po cijelom području 1")
	}
}

// TestDynamicAddDutyOnTheFly provjerava dinamičko dodavanje funkcije i ispomoći s više dionica
func TestDynamicAddDutyOnTheFly(t *testing.T) {
	userService, _, sseBroker, userRepo := setupTestServices(t)

	sseChan := sseBroker.Subscribe()
	defer sseBroker.Unsubscribe(sseChan)

	admin, _ := userRepo.GetUserByUsername("iivic")
	adminPerms, _ := userRepo.GetUserPermissions(admin.ID)

	sectorP := "P"
	area1 := 1

	// 1. Kreiranje novog djelatnika u Primjerovu
	newUser, err := userService.CreateUser(adminPerms, service.CreateUserRequest{
		Username:     "operater1",
		Password:     "tajna123",
		FullName:     "Novi Operater",
		Title:        "ing.građ.",
		OrgType:      models.OrgHrvatskeVode,
		OrgName:      "VGO za Primjerski sliv, Primjerovo",
		Phone:        "000 000 090",
		MobilePhone:  "000 000 091",
		ShortPhone:   "9090",
		Email:        "operater1@example.com",
		DutyTitle:    "Dežurni operater u COP-u Primjerovo",
		Role:         models.RoleOperator,
		ScopeType:    models.ScopeArea,
		SectorID:     &sectorP,
		AreaID:       &area1,
		SectionCodes: "P.1.1, P.1.2",
	})
	if err != nil {
		t.Fatalf("Greška pri kreiranju djelatnika: %v", err)
	}

	permsBefore, _ := userRepo.GetUserPermissions(newUser.ID)
	if !permsBefore.HasWriteAccess("P", 1, "P.1.1") {
		t.Error("Korisnik bi trebao imati pristup dionici P.1.1")
	}
	if permsBefore.HasWriteAccess("E", 3, "E.3.1") {
		t.Error("Korisnik ne bi smio imati pristup dionicama sektora E")
	}

	// 2. Dodjela dodatnog zaduženja / ispomoći za Sektor E / Područje 3 i specifične dionice E.3.1, E.3.2
	sectorE := "E"
	area3 := 3
	expires := time.Now().Add(72 * time.Hour)

	err = userService.AddDuty(adminPerms, service.AddDutyRequest{
		UserID:       newUser.ID,
		Title:        "Ispomoć — Ophodnja dionica na Rubnici",
		Role:         models.RoleSectionLeader,
		ScopeType:    models.ScopeSection,
		SectorID:     &sectorE,
		AreaID:       &area3,
		SectionCodes: "E.3.1, E.3.2",
		IsPrimary:    false,
		IsTemporary:  true,
		Reason:       "Hitno podizanje nasipa kod Rubnog",
		ExpiresAt:    &expires,
	})
	if err != nil {
		t.Fatalf("Greška pri dodjeli funkcije: %v", err)
	}

	// 3. Provjera novih ovlasti: sada IMA pristup dionicama E.3.1 i E.3.2
	permsAfter, _ := userRepo.GetUserPermissions(newUser.ID)
	if !permsAfter.HasWriteAccess("E", 0, "E.3.1") {
		t.Error("Korisnik bi sada MORAO imati pristup dionici E.3.1 na Rubnici")
	}
	if !permsAfter.HasWriteAccess("E", 0, "E.3.2") {
		t.Error("Korisnik bi sada MORAO imati pristup dionici E.3.2 na Rubnici")
	}

	// 4. Provjera primitka SSE poruke
	select {
	case msg := <-sseChan:
		if msg == "" {
			t.Error("Prazna SSE poruka")
		}
	case <-time.After(2 * time.Second):
		t.Error("Isteklo vrijeme čekanja na SSE poruku")
	}
}

// TestSearchUsers provjerava pretragu po imenu, prezimenu, dionicama i kontaktima
func TestSearchUsers(t *testing.T) {
	userService, _, _, userRepo := setupTestServices(t)

	// Pretraga po prezimenu: Ivić su i Ivo i Iva
	results, err := userService.ListUsers("", 0, "", "Ivić", "")
	if err != nil {
		t.Fatalf("Greška pri pretrazi: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Pretraga za 'Ivić' morala je pronaći Ivu Ivića i Ivu Ivić, pronađeno: %d", len(results))
	}
	foundIvo := false
	for _, u := range results {
		if u.Username == "iivic" {
			foundIvo = true
			break
		}
	}
	if !foundIvo {
		t.Error("Korisnik 'iivic' nije pronađen u rezultatima pretrage")
	}

	// Pretraga po dionici
	dionicaResults, err := userService.ListUsers("", 0, "", "P.1.1", "")
	if err != nil {
		t.Fatalf("Greška pri pretrazi dionica: %v", err)
	}
	if len(dionicaResults) == 0 {
		t.Fatal("Pretraga po dionici 'P.1.1' morala je vratiti rezultate")
	}

	// Pretraga po broju mobitela
	// pretraga po broju telefona: broj se uzima iz zapisa, ne piše se u test
	ivoZaBroj, _ := userRepo.GetUserByUsername("iivic")
	phoneResults, err := userService.ListUsers("", 0, "", ivoZaBroj.MobilePhone, "")
	if err != nil {
		t.Fatalf("Greška pri pretrazi po mobitelu: %v", err)
	}
	if len(phoneResults) == 0 {
		t.Error("Očekivani rezultati pretrage po broju mobitela, pronađeno 0")
	}
	foundIvoPhone := false
	for _, u := range phoneResults {
		if u.Username == "iivic" {
			foundIvoPhone = true
			break
		}
	}
	if !foundIvoPhone {
		t.Error("Korisnik 'iivic' nije pronađen u pretrazi po mobitelu")
	}
}

// TestDeleteUser provjerava brisanje korisnika i zaštitu od brisanja vlastitog računa
func TestDeleteUser(t *testing.T) {
	userService, _, _, userRepo := setupTestServices(t)

	admin, _ := userRepo.GetUserByUsername("iivic")
	adminPerms, _ := userRepo.GetUserPermissions(admin.ID)

	// Pokušaj brisanja samoga sebe mora vratiti grešku
	err := userService.DeleteUser(adminPerms, admin.ID)
	if err == nil {
		t.Fatal("Očekivana greška pri pokušaju brisanja vlastitog profila")
	}

	// Stvaranje privremenog korisnika za brisanje; terenska uloga traži
	// branjeno područje
	area1 := 1
	target, err := userService.CreateUser(adminPerms, service.CreateUserRequest{
		Username:  "za_brisanje",
		Password:  "gocop2026",
		FullName:  "Korisnik Za Brisanje",
		Title:     "ing.",
		Role:      models.RoleFieldWorker,
		ScopeType: models.ScopeSector,
		AreaID:    &area1,
		DutyTitle: "Pripravnik",
	})
	if err != nil {
		t.Fatalf("Greška pri stvaranju privremenog korisnika: %v", err)
	}

	// Brisanje korisnika
	err = userService.DeleteUser(adminPerms, target.ID)
	if err != nil {
		t.Fatalf("Greška pri brisanju korisnika: %v", err)
	}

	// Provjera da korisnik više ne postoji
	deleted, err := userRepo.GetUserByID(target.ID)
	if err != nil {
		t.Fatalf("Greška pri provjeri: %v", err)
	}
	if deleted != nil {
		t.Error("Korisnik je i dalje pronađen nakon brisanja")
	}

	// Provjera da su i dužnosti obrisane
	duties, _ := userRepo.GetDutiesForUser(target.ID)
	if len(duties) != 0 {
		t.Errorf("Očekivano 0 zaduženja za obrisanog korisnika, pronađeno: %d", len(duties))
	}
}

// TestChangePassword provjerava rad s početnom lozinkom, upozorenjem i promjenom lozinke
func TestChangePassword(t *testing.T) {
	_, authService, _, userRepo := setupTestServices(t)

	// 1. Dohvat korisnice nakon početnog seeda: Ana Anić
	user, err := userRepo.GetUserByUsername("aanic")
	if err != nil || user == nil {
		t.Fatalf("Greška pri dohvatu korisnika: %v", err)
	}

	// Početno mora imati MustChangePassword = true
	if !user.MustChangePassword {
		t.Error("Očekivano da korisnik ima MustChangePassword = true nakon početnog seeda")
	}

	// 2. Prijava s početnom lozinkom mora uspjeti
	_, loggedUser, err := authService.Login("aanic", "gocop2026", "127.0.0.1", "test-agent")
	if err != nil || loggedUser == nil {
		t.Fatalf("Prijava s početnom lozinkom 'gocop2026' nije uspjela: %v", err)
	}

	// 3. Pokušaj promjene s pogrešnom trenutnom lozinkom mora pasti
	err = authService.ChangePassword(user.ID, "pogresna_lozinka", "novaSifra2026", uuid.Nil)
	if err == nil {
		t.Error("Očekivana greška pri netočnoj trenutnoj lozinci")
	}

	// 4. Pokušaj postavljanja prekratke lozinke mora pasti
	err = authService.ChangePassword(user.ID, "gocop2026", "123", uuid.Nil)
	if err == nil {
		t.Error("Očekivana greška za prekratku novu lozinku")
	}

	// 5. Uspješna promjena lozinke
	err = authService.ChangePassword(user.ID, "gocop2026", "mojaNovaSigurnaLozinka", uuid.Nil)
	if err != nil {
		t.Fatalf("Greška pri promjeni lozinke: %v", err)
	}

	// 6. Provjera da je MustChangePassword sada false
	updatedUser, err := userRepo.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("Greška pri provjeri: %v", err)
	}
	if updatedUser.MustChangePassword {
		t.Error("MustChangePassword mora biti false nakon što korisnik postavi vlastitu lozinku")
	}

	// 7. Stara lozinka više ne smije prolaziti
	_, _, err = authService.Login("aanic", "gocop2026", "127.0.0.1", "test-agent")
	if err == nil {
		t.Error("Stara lozinka ne smije vrijediti nakon promjene")
	}

	// 8. Nova lozinka mora uspješno prijaviti korisnika
	_, _, err = authService.Login("aanic", "mojaNovaSigurnaLozinka", "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Prijava s novom lozinkom nije uspjela: %v", err)
	}
}

// TestRegularUserCanEditOwnProfileNotOthersOrDuties provjerava da svaki korisnik može mijenjati svoj profil (kontakte, titulu),
// ali ne i tuđe profile, niti može dodavati/brisati funkcije i zaduženja.
func TestRegularUserCanEditOwnProfileNotOthersOrDuties(t *testing.T) {
	userService, _, _, userRepo := setupTestServices(t)

	// Uzmimo redovnog korisnika (vodočuvara Marka Markića)
	user, err := userRepo.GetUserByUsername("mmarkic")
	if err != nil || user == nil {
		t.Fatalf("Korisnik 'mmarkic' nije pronađen: %v", err)
	}

	userPerms, err := userRepo.GetUserPermissions(user.ID)
	if err != nil {
		t.Fatalf("Greška pri dohvatu ovlasti: %v", err)
	}

	// 1. Korisnik smije ažurirati SVOJ profil (telefon, mobitel, titulu);
	// adresa e-pošte ima svoja pravila (TestVlastitaPromjenaAdrese), a
	// obrazac šalje i zatečenu uključenost računa, koju sam ne mijenja
	updated, err := userService.UpdateUser(userPerms, service.UpdateUserRequest{
		ID:          user.ID,
		Username:    user.Username,
		FullName:    "Marko Markić (Ažuriran)",
		Title:       "struč.spec.ing.",
		Phone:       "000 000 050",
		MobilePhone: "000 000 059",
		ShortPhone:  "5059",
		Email:       user.Email,
		IsActive:    user.IsActive,
	})
	if err != nil {
		t.Fatalf("Redovni korisnik mora moći urediti vlastiti profil, greška: %v", err)
	}
	if updated.MobilePhone != "000 000 059" || updated.Email != user.Email || updated.Title != "struč.spec.ing." {
		t.Errorf("Ažurirani podaci ne odgovaraju: %+v", updated)
	}

	// 2. Korisnik NE SMIJE moći uređivati tuđi profil (npr. administratora Ive Ivića)
	otherUser, _ := userRepo.GetUserByUsername("iivic")
	_, err = userService.UpdateUser(userPerms, service.UpdateUserRequest{
		ID:          otherUser.ID,
		Username:    otherUser.Username,
		FullName:    "Hakirani Admin",
		MobilePhone: "000 000 000",
	})
	if err != service.ErrUnauthorized {
		t.Errorf("Očekivano ErrUnauthorized pri pokušaju uređivanja tuđeg profila, dobiveno: %v", err)
	}

	// 3. Korisnik NE SMIJE moći dodijeliti sebi ili drugome zaduženje/funkciju
	err = userService.AddDuty(userPerms, service.AddDutyRequest{
		UserID:    user.ID,
		Title:     "Samoprozvani Globalni Admin",
		Role:      models.RoleGlobalAdmin,
		ScopeType: models.ScopeAll,
	})
	if err != service.ErrUnauthorized {
		t.Errorf("Očekivano ErrUnauthorized pri pokušaju dodavanja zaduženja od strane redovnog korisnika, dobiveno: %v", err)
	}

	// 4. Korisnik NE SMIJE moći obrisati/opozvati dužnost
	if len(user.Duties) > 0 {
		err = userService.RevokeDuty(userPerms, user.Duties[0].ID)
		if err != service.ErrUnauthorized {
			t.Errorf("Očekivano ErrUnauthorized pri pokušaju opoziva zaduženja, dobiveno: %v", err)
		}
	}

	// 5. Korisnik NE SMIJE moći obrisati profil drugog korisnika
	err = userService.DeleteUser(userPerms, otherUser.ID)
	if err != service.ErrUnauthorized {
		t.Errorf("Očekivano ErrUnauthorized pri pokušaju brisanja korisnika, dobiveno: %v", err)
	}
}

// Izmjena zaduženja: uprava mijenja dionice u mjestu, redovni korisnik ne smije
func TestIzmjenaZaduzenja(t *testing.T) {
	userService, _, _, userRepo := setupTestServices(t)
	admin, _ := userRepo.GetUserByUsername("iivic")
	adminPerms, _ := userRepo.GetUserPermissions(admin.ID)

	osoba, _ := userRepo.GetUserByUsername("ivaivic")
	if osoba == nil || len(osoba.Duties) == 0 {
		t.Fatal("u izmišljenom skupu nema Ive Ivić s dužnostima")
	}
	var zad *models.Duty
	for i := range osoba.Duties {
		if osoba.Duties[i].SectionCodes != "" {
			zad = &osoba.Duties[i]
			break
		}
	}
	if zad == nil {
		t.Fatal("Iva Ivić nema zaduženje s dionicama")
	}
	req := service.AddDutyRequest{UserID: osoba.ID, Title: zad.Title + " (izmjena)", Role: zad.Role, SectorID: zad.SectorID, AreaID: zad.AreaID, SectionCodes: zad.SectionCodes + ", P.1.3", IsPrimary: zad.IsPrimary}
	if err := userService.UpdateDuty(adminPerms, zad.ID, req); err != nil {
		t.Fatalf("uprava ne može izmijeniti zaduženje: %v", err)
	}
	poslije, _ := userRepo.GetDuty(zad.ID)
	if poslije == nil || !strings.HasSuffix(poslije.SectionCodes, "P.1.3") || !strings.HasSuffix(poslije.Title, "(izmjena)") {
		t.Fatalf("izmjena nije spremljena: %+v", poslije)
	}

	// redovni korisnik ne smije mijenjati zaduženja
	osobaPerms, _ := userRepo.GetUserPermissions(osoba.ID)
	if err := userService.UpdateDuty(osobaPerms, zad.ID, req); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("očekivano ErrUnauthorized, dobiveno: %v", err)
	}

	// opozvano se ne mijenja, a vidi se u povijesti
	if err := userService.RevokeDuty(adminPerms, zad.ID); err != nil {
		t.Fatal(err)
	}
	if err := userService.UpdateDuty(adminPerms, zad.ID, req); err == nil {
		t.Error("izmjena opozvanog zaduženja bi trebala biti odbijena")
	}
	proslost, _ := userService.PastDuties(osoba.ID)
	if len(proslost) != 1 || proslost[0].ID != zad.ID || proslost[0].OpozvanoAt == nil {
		t.Errorf("očekivano jedno opozvano zaduženje s vremenom opoziva, dobiveno: %+v", proslost)
	}
}
