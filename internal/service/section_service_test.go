package service_test

import (
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func setupSectionTestServices(t *testing.T) (*service.SectionService, *service.UserService, *service.AuthService, *repository.SectionRepository, *repository.UserRepository) {
	t.Helper()
	if !db.UseRepoImenik() {
		t.Skip("imenik.json nije dostupan — osobni podaci djelatnika stoje izvan repozitorija (data/imenik.json)")
	}
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_sections_gocop.db")

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
	sectionRepo := repository.NewSectionRepository(database, ledger.New(database, "test-node"))

	authService := service.NewAuthService(userRepo, sessionRepo)
	sseBroker := service.NewSSEBroker()
	userService := service.NewUserService(userRepo, authService, sseBroker)
	sectionService := service.NewSectionService(sectionRepo, sseBroker)

	return sectionService, userService, authService, sectionRepo, userRepo
}

// TestSectionsSeedAndCount provjerava jesu li sve 465 dionice uspješno unesene u bazu
func TestSectionsSeedAndCount(t *testing.T) {
	sectionService, _, _, _, _ := setupSectionTestServices(t)

	sections, err := sectionService.ListSections("", 0, "")
	if err != nil {
		t.Fatalf("Greška pri dohvatu dionica: %v", err)
	}

	if len(sections) != 465 {
		t.Fatalf("Očekivano točno 465 dionica u bazi, pronađeno: %d", len(sections))
	}
}

// TestSectionsFiltering provjerava filtriranje po sektoru B i branjenom području 15 (Vuka)
func TestSectionsFiltering(t *testing.T) {
	sectionService, _, _, _, _ := setupSectionTestServices(t)

	// Sektor B treba imati 65 dionica
	sektorBSections, err := sectionService.ListSections("B", 0, "")
	if err != nil {
		t.Fatalf("Greška pri dohvatu dionica Sektora B: %v", err)
	}
	if len(sektorBSections) != 65 {
		t.Errorf("Očekivano 65 dionica za Sektor B, pronađeno: %d", len(sektorBSections))
	}

	// Branjeno područje 15 (Mali sliv Vuka)
	vukaSections, err := sectionService.ListSections("B", 15, "")
	if err != nil {
		t.Fatalf("Greška pri dohvatu dionica BP 15: %v", err)
	}
	if len(vukaSections) == 0 {
		t.Fatalf("Očekivane dionice za BP 15, pronađeno 0")
	}

	// Pretraga po objektu (npr. CS Adica ili CS Teča)
	searchResults, err := sectionService.ListSections("", 0, "CS Adica")
	if err != nil {
		t.Fatalf("Greška pri pretrazi: %v", err)
	}
	if len(searchResults) == 0 {
		t.Errorf("Očekivan barem 1 rezultat za pretragu 'CS Adica'")
	}
}

// TestSectionPermissionsMarioAndTomislav provjerava da Mario Spajić i Tomislav Kraljević mogu uređivati Sektor B
func TestSectionPermissionsMarioAndTomislav(t *testing.T) {
	sectionService, _, _, _, userRepo := setupSectionTestServices(t)

	// Mario Spajić (zamjenik rukovoditelja Sektora B i voditelj COP Osijek)
	mario, err := userRepo.GetUserByUsername("mspajic")
	if err != nil || mario == nil {
		t.Fatalf("Korisnik mspajic nije pronađen: %v", err)
	}
	marioPerms, _ := userRepo.GetUserPermissions(mario.ID)

	// Tomislav Kraljević (zamjenik voditelja COP Osijek za Sektor B)
	tomislav, err := userRepo.GetUserByUsername("tkraljevic")
	if err != nil || tomislav == nil {
		// Ako je tomislav admin račun
		tomislav, err = userRepo.GetUserByUsername("tkraljevic")
		if err != nil || tomislav == nil {
			t.Fatalf("Korisnik tomislav nije pronađen: %v", err)
		}
	}
	tomislavPerms, _ := userRepo.GetUserPermissions(tomislav.ID)

	// Dionica u Sektoru B (B.15.1)
	secB, err := sectionService.GetSectionWithDetails("B.15.1")
	if err != nil || secB == nil {
		t.Fatalf("Dionica B.15.1 nije pronađena: %v", err)
	}

	// Dionica u Sektoru A (A.19.1)
	secA, err := sectionService.GetSectionWithDetails("A.19.1")
	if err != nil || secA == nil {
		t.Fatalf("Dionica A.19.1 nije pronađena: %v", err)
	}

	// Mario Spajić MORA moći uređivati B.15.1
	if !sectionService.CanEditSection(marioPerms, secB) {
		t.Errorf("Mario Spajić (zamjenik rukovoditelja Sektora B) bi MORAO moći uređivati dionicu B.15.1")
	}

	// Mario Spajić NE SMIJE moći uređivati dionicu iz Sektora A
	if sectionService.CanEditSection(marioPerms, secA) {
		t.Errorf("Mario Spajić NE BI SMIO moći uređivati dionicu A.19.1 iz Sektora A")
	}

	// Tomislav Kraljević MORA moći uređivati B.15.1
	if !sectionService.CanEditSection(tomislavPerms, secB) {
		t.Errorf("Tomislav Kraljević bi MORAO moći uređivati dionicu B.15.1")
	}
}

// TestCreateAndEditSectionWorkflow provjerava kreiranje nove dionice i njezino ažuriranje
func TestCreateAndEditSectionWorkflow(t *testing.T) {
	sectionService, _, _, _, userRepo := setupSectionTestServices(t)

	mario, _ := userRepo.GetUserByUsername("mspajic")
	marioPerms, _ := userRepo.GetUserPermissions(mario.ID)

	newSec := &models.Section{
		Code:          "B.15.99",
		AreaID:        15,
		SectorID:      "B",
		Description:   "rijeka Vuka, nova pokusna dionica",
		ProtectedArea: "Općina Ernestinovo",
		Notes:         "Privremena dionica za testiranje",
	}

	// Mario stvara novu dionicu u Sektoru B
	err := sectionService.CreateSection(marioPerms, newSec)
	if err != nil {
		t.Fatalf("Greška pri stvaranju dionice od strane ovlaštenog korisnika: %v", err)
	}

	// Provjera dohvata
	fetched, err := sectionService.GetSectionWithDetails("B.15.99")
	if err != nil || fetched == nil {
		t.Fatalf("Nova dionica B.15.99 nije pronađena u bazi: %v", err)
	}

	if fetched.Description != "rijeka Vuka, nova pokusna dionica" {
		t.Errorf("Očekivan vodotok 'rijeka Vuka, nova pokusna dionica', dobiveno: %s", fetched.Description)
	}

	// Ažuriranje dionice
	fetched.Notes = "Ažurirana operativna napomena"
	err = sectionService.UpdateSection(marioPerms, fetched)
	if err != nil {
		t.Fatalf("Greška pri ažuriranju dionice: %v", err)
	}

	updated, _ := sectionService.GetSectionWithDetails("B.15.99")
	if updated.Notes != "Ažurirana operativna napomena" {
		t.Errorf("Očekivana nova napomena, dobiveno: %s", updated.Notes)
	}
}
