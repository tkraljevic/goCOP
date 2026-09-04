package service_test

import (
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func setupTestServices(t *testing.T) (*service.UserService, *service.AuthService, *service.SSEBroker, *repository.UserRepository) {
	t.Helper()
	if !db.UseRepoImenik() {
		t.Skip("imenik.json nije dostupan — osobni podaci djelatnika stoje izvan repozitorija (data/imenik.json)")
	}
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_gocop.db")

	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("Greška pri otvaranju test baze: %v", err)
	}

	if err := db.InitSchema(database); err != nil {
		t.Fatalf("Greška pri shemi: %v", err)
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

// TestSeedAdminProvjera provjerava profil i višestruke funkcije Tomislava Kraljevića (COP Osijek)
func TestSeedAdminProvjera(t *testing.T) {
	_, authService, _, userRepo := setupTestServices(t)

	admin, err := userRepo.GetUserByUsername("tomislav")
	if err != nil {
		t.Fatalf("Greška pri dohvatu admina: %v", err)
	}
	if admin == nil {
		t.Fatal("Korisnik 'tomislav' nije pronađen u seed podacima")
	}

	if !admin.IsGlobalAdmin {
		t.Error("Očekivano da Tomislav ima IsGlobalAdmin = true")
	}
	if admin.MobilePhone == "" || admin.ShortPhone == "" {
		t.Errorf("Očekivani mobitel i skraćeni lokal iz imenika, dobiveno: %q / %q", admin.MobilePhone, admin.ShortPhone)
	}

	// Provjera višestrukih funkcija
	if len(admin.Duties) < 2 {
		t.Fatalf("Očekivano barem 2 funkcije za Tomislava, pronađeno: %d", len(admin.Duties))
	}

	// Provjera lozinke
	if !authService.CheckPassword(admin.PasswordHash, "gocop2026") {
		t.Error("Provjera lozinke nije uspjela za zadanu lozinku gocop2026")
	}
}

// TestMultiDutyPersonProvjera provjerava Josipa Fučeka koji ima i branjeno područje i više dionica
func TestMultiDutyPersonProvjera(t *testing.T) {
	_, _, _, userRepo := setupTestServices(t)

	fucek, err := userRepo.GetUserByUsername("jfucek")
	if err != nil {
		t.Fatalf("Greška pri dohvatu korisnika: %v", err)
	}
	if fucek == nil {
		t.Fatal("Korisnik 'jfucek' nije pronađen u seed podacima")
	}

	if len(fucek.Duties) != 2 {
		t.Fatalf("Josip Fuček bi trebao imati točno 2 funkcije, pronađeno: %d", len(fucek.Duties))
	}

	perms, err := userRepo.GetUserPermissions(fucek.ID)
	if err != nil {
		t.Fatalf("Greška pri dohvatu ovlasti: %v", err)
	}

	// Ima pristup branjenom području 19
	if !perms.HasWriteAccess("A", 19, "") {
		t.Error("Korisnik bi trebao imati pravo pisanja za branjeno područje 19")
	}

	// Ima pristup dionicama A.19.8, A.19.9, A.19.10
	if !perms.HasWriteAccess("A", 0, "A.19.8") {
		t.Error("Korisnik bi trebao imati pravo pisanja za dionicu A.19.8")
	}
	if !perms.HasWriteAccess("A", 0, "A.19.10") {
		t.Error("Korisnik bi trebao imati pravo pisanja za dionicu A.19.10")
	}
}

// TestDynamicAddDutyOnTheFly provjerava dinamičko dodavanje funkcije i ispomoći s više dionica
func TestDynamicAddDutyOnTheFly(t *testing.T) {
	userService, _, sseBroker, userRepo := setupTestServices(t)

	sseChan := sseBroker.Subscribe()
	defer sseBroker.Unsubscribe(sseChan)

	admin, _ := userRepo.GetUserByUsername("tomislav")
	adminPerms, _ := userRepo.GetUserPermissions(admin.ID)

	sectorB := "B"
	area15 := 15

	// 1. Kreiranje novog djelatnika u Osijeku
	newUser, err := userService.CreateUser(adminPerms, service.CreateUserRequest{
		Username:     "operater1",
		Password:     "tajna123",
		FullName:     "Petar Perić",
		Title:        "ing.građ.",
		OrgType:      models.OrgHrvatskeVode,
		OrgName:      "VGO Osijek",
		Phone:        "031-252-800",
		MobilePhone:  "098-111-2222",
		ShortPhone:   "2800",
		Email:        "petar.peric@voda.hr",
		DutyTitle:    "Dežurni inženjer COP Osijek",
		Role:         models.RoleOperator,
		ScopeType:    models.ScopeArea,
		SectorID:     &sectorB,
		AreaID:       &area15,
		SectionCodes: "B.15.1, B.15.2",
	})
	if err != nil {
		t.Fatalf("Greška pri kreiranju djelatnika: %v", err)
	}

	permsBefore, _ := userRepo.GetUserPermissions(newUser.ID)
	if !permsBefore.HasWriteAccess("B", 15, "B.15.1") {
		t.Error("Korisnik bi trebao imati pristup dionici B.15.1")
	}
	if permsBefore.HasWriteAccess("D", 10, "D.10.1") {
		t.Error("Korisnik ne bi smio imati pristup dionicama u Sisku")
	}

	// 2. Dodjela dodatnog zaduženja / ispomoći za Sektor D / Područje 10 i specifične dionice D.10.1, D.10.2
	sectorD := "D"
	area10 := 10
	expires := time.Now().Add(72 * time.Hour)

	err = userService.AddDuty(adminPerms, service.AddDutyRequest{
		UserID:       newUser.ID,
		Title:        "Ispomoć — Ophodnja dionica na Savi u Sisku",
		Role:         models.RoleSectionLeader,
		ScopeType:    models.ScopeSection,
		SectorID:     &sectorD,
		AreaID:       &area10,
		SectionCodes: "D.10.1, D.10.2, D.10.3",
		IsPrimary:    false,
		IsTemporary:  true,
		Reason:       "Hitno podizanje nasipa kod Siska",
		ExpiresAt:    &expires,
	})
	if err != nil {
		t.Fatalf("Greška pri dodjeli funkcije: %v", err)
	}

	// 3. Provjera novih ovlasti: sada IMA pristup dionicama D.10.1 i D.10.2
	permsAfter, _ := userRepo.GetUserPermissions(newUser.ID)
	if !permsAfter.HasWriteAccess("D", 0, "D.10.1") {
		t.Error("Korisnik bi sada MORAO imati pristup dionici D.10.1 na Savi")
	}
	if !permsAfter.HasWriteAccess("D", 0, "D.10.3") {
		t.Error("Korisnik bi sada MORAO imati pristup dionici D.10.3 na Savi")
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

	// Pretraga po imenu i prezimenu
	results, err := userService.ListUsers("", 0, "", "Kraljević", "")
	if err != nil {
		t.Fatalf("Greška pri pretrazi: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Pretraga za 'Kraljević' morala je pronaći barem jednog korisnika")
	}
	foundTomislav := false
	for _, u := range results {
		if u.Username == "tomislav" {
			foundTomislav = true
			break
		}
	}
	if !foundTomislav {
		t.Error("Korisnik 'tomislav' nije pronađen u rezultatima pretrage")
	}

	// Pretraga po dionici
	dionicaResults, err := userService.ListUsers("", 0, "", "B.15.1", "")
	if err != nil {
		t.Fatalf("Greška pri pretrazi dionica: %v", err)
	}
	if len(dionicaResults) == 0 {
		t.Fatal("Pretraga po dionici 'B.15.1' morala je vratiti rezultate")
	}

	// Pretraga po broju mobitela
	// pretraga po broju telefona: broj se uzima iz zapisa, ne piše se u test
	tomislavZaBroj, _ := userRepo.GetUserByUsername("tomislav")
	phoneResults, err := userService.ListUsers("", 0, "", tomislavZaBroj.MobilePhone, "")
	if err != nil {
		t.Fatalf("Greška pri pretrazi po mobitelu: %v", err)
	}
	if len(phoneResults) == 0 {
		t.Error("Očekivani rezultati pretrage po broju mobitela, pronađeno 0")
	}
	foundTomislavPhone := false
	for _, u := range phoneResults {
		if u.Username == "tomislav" {
			foundTomislavPhone = true
			break
		}
	}
	if !foundTomislavPhone {
		t.Error("Korisnik 'tomislav' nije pronađen u pretrazi po mobitelu")
	}
}

// TestDeleteUser provjerava brisanje korisnika i zaštitu od brisanja vlastitog računa
func TestDeleteUser(t *testing.T) {
	userService, _, _, userRepo := setupTestServices(t)

	admin, _ := userRepo.GetUserByUsername("tomislav")
	adminPerms, _ := userRepo.GetUserPermissions(admin.ID)

	// Pokušaj brisanja samoga sebe mora vratiti grešku
	err := userService.DeleteUser(adminPerms, admin.ID)
	if err == nil {
		t.Fatal("Očekivana greška pri pokušaju brisanja vlastitog profila")
	}

	// Stvaranje privremenog korisnika za brisanje
	target, err := userService.CreateUser(adminPerms, service.CreateUserRequest{
		Username:  "za_brisanje",
		Password:  "gocop2026",
		FullName:  "Korisnik Za Brisanje",
		Title:     "ing.",
		Role:      models.RoleFieldWorker,
		ScopeType: models.ScopeSector,
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

	// 1. Dohvat korisnika nakon početnog seeda
	user, err := userRepo.GetUserByUsername("tomislav")
	if err != nil || user == nil {
		t.Fatalf("Greška pri dohvatu korisnika: %v", err)
	}

	// Početno mora imati MustChangePassword = true
	if !user.MustChangePassword {
		t.Error("Očekivano da korisnik ima MustChangePassword = true nakon početnog seeda")
	}

	// 2. Prijava s početnom lozinkom mora uspjeti
	_, loggedUser, err := authService.Login("tomislav", "gocop2026", "127.0.0.1", "test-agent")
	if err != nil || loggedUser == nil {
		t.Fatalf("Prijava s početnom lozinkom 'gocop2026' nije uspjela: %v", err)
	}

	// 3. Pokušaj promjene s pogrešnom trenutnom lozinkom mora pasti
	err = authService.ChangePassword(user.ID, "pogresna_lozinka", "novaSifra2026")
	if err == nil {
		t.Error("Očekivana greška pri netočnoj trenutnoj lozinci")
	}

	// 4. Pokušaj postavljanja prekratke lozinke mora pasti
	err = authService.ChangePassword(user.ID, "gocop2026", "123")
	if err == nil {
		t.Error("Očekivana greška za prekratku novu lozinku")
	}

	// 5. Uspješna promjena lozinke
	err = authService.ChangePassword(user.ID, "gocop2026", "mojaNovaSigurnaLozinka")
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
	_, _, err = authService.Login("tomislav", "gocop2026", "127.0.0.1", "test-agent")
	if err == nil {
		t.Error("Stara lozinka ne smije vrijediti nakon promjene")
	}

	// 8. Nova lozinka mora uspješno prijaviti korisnika
	_, _, err = authService.Login("tomislav", "mojaNovaSigurnaLozinka", "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Prijava s novom lozinkom nije uspjela: %v", err)
	}
}

// TestRegularUserCanEditOwnProfileNotOthersOrDuties provjerava da svaki korisnik može mijenjati svoj profil (kontakte, titulu),
// ali ne i tuđe profile, niti može dodavati/brisati funkcije i zaduženja.
func TestRegularUserCanEditOwnProfileNotOthersOrDuties(t *testing.T) {
	userService, _, _, userRepo := setupTestServices(t)

	// Uzmimo redovnog korisnika (terenskog radnika / strojara Zvonka Ciprića)
	user, err := userRepo.GetUserByUsername("zcipric")
	if err != nil || user == nil {
		t.Fatalf("Korisnik 'zcipric' nije pronađen: %v", err)
	}

	userPerms, err := userRepo.GetUserPermissions(user.ID)
	if err != nil {
		t.Fatalf("Greška pri dohvatu ovlasti: %v", err)
	}

	// 1. Korisnik smije ažurirati SVOJ profil (telefon, mobitel, email, titulu)
	updated, err := userService.UpdateUser(userPerms, service.UpdateUserRequest{
		ID:          user.ID,
		Username:    user.Username,
		FullName:    "Zvonko Ciprić (Ažuriran)",
		Title:       "struč.spec.ing.",
		Phone:       "035-123-456",
		MobilePhone: "099-999-8888",
		ShortPhone:  "9999",
		Email:       "zvonko.novi@voda.hr",
	})
	if err != nil {
		t.Fatalf("Redovni korisnik mora moći urediti vlastiti profil, greška: %v", err)
	}
	if updated.MobilePhone != "099-999-8888" || updated.Email != "zvonko.novi@voda.hr" || updated.Title != "struč.spec.ing." {
		t.Errorf("Ažurirani podaci ne odgovaraju: %+v", updated)
	}

	// 2. Korisnik NE SMIJE moći uređivati tuđi profil (npr. Tomislava Kraljevića)
	otherUser, _ := userRepo.GetUserByUsername("tomislav")
	_, err = userService.UpdateUser(userPerms, service.UpdateUserRequest{
		ID:          otherUser.ID,
		Username:    otherUser.Username,
		FullName:    "Hakirani Admin",
		MobilePhone: "091-000-0000",
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
