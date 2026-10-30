package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// setupSectionTestServices puni bazu iz izmišljenog skupa (testdata/sektor-p):
// sektor P s dionicama P.1.1–P.1.3 i P.2.1 te sektor E s E.3.1 i E.3.2
func setupSectionTestServices(t *testing.T) (*service.SectionService, *service.UserService, *service.AuthService, *repository.SectionRepository, *repository.UserRepository) {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_sections_gocop.db")

	database, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatalf("Greška pri otvaranju test baze: %v", err)
	}

	if _, err := db.SeedInitialData(database); err != nil {
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

// TestSectionsSeedAndCount provjerava jesu li sve dionice iz datoteke uz bazu unesene u bazu
func TestSectionsSeedAndCount(t *testing.T) {
	sectionService, _, _, _, _ := setupSectionTestServices(t)

	sections, err := sectionService.ListSections("", 0, "")
	if err != nil {
		t.Fatalf("Greška pri dohvatu dionica: %v", err)
	}

	if len(sections) != 6 {
		t.Fatalf("Očekivano točno 6 dionica u bazi, pronađeno: %d", len(sections))
	}
}

// TestSectionsFiltering provjerava filtriranje po sektoru P i branjenom području 1 (Primjerica);
// broj dionica sektora B u stvarnom registru provjerava TestRegistarTeritorijaJeCjelovit u internal/db
func TestSectionsFiltering(t *testing.T) {
	sectionService, _, _, _, _ := setupSectionTestServices(t)

	// Sektor P treba imati 4 dionice
	sektorPSections, err := sectionService.ListSections("P", 0, "")
	if err != nil {
		t.Fatalf("Greška pri dohvatu dionica Sektora P: %v", err)
	}
	if len(sektorPSections) != 4 {
		t.Errorf("Očekivane 4 dionice za Sektor P, pronađeno: %d", len(sektorPSections))
	}

	// Branjeno područje 1 (Mali sliv Primjerica)
	primjericaSections, err := sectionService.ListSections("P", 1, "")
	if err != nil {
		t.Fatalf("Greška pri dohvatu dionica BP 1: %v", err)
	}
	if len(primjericaSections) != 3 {
		t.Fatalf("Očekivane 3 dionice za BP 1, pronađeno %d", len(primjericaSections))
	}

	// Pretraga po objektu
	searchResults, err := sectionService.ListSections("", 0, "CS Probni")
	if err != nil {
		t.Fatalf("Greška pri pretrazi: %v", err)
	}
	if len(searchResults) == 0 {
		t.Errorf("Očekivan barem 1 rezultat za pretragu 'CS Probni'")
	}
}

// TestSectionPermissionsSectorLeaderAndAdmin provjerava da rukovoditelj
// sektora P Pero Perić uređuje dionice svog sektora, ali ne i tuđeg, a
// administrator Ivo Ivić svaku
func TestSectionPermissionsSectorLeaderAndAdmin(t *testing.T) {
	sectionService, _, _, _, userRepo := setupSectionTestServices(t)

	// Pero Perić (rukovoditelj sektora P)
	pero, err := userRepo.GetUserByUsername("pperic")
	if err != nil || pero == nil {
		t.Fatalf("Korisnik pperic nije pronađen: %v", err)
	}
	peroPerms, _ := userRepo.GetUserPermissions(pero.ID)

	// Ivo Ivić (globalni administrator)
	ivo, err := userRepo.GetUserByUsername("iivic")
	if err != nil || ivo == nil {
		t.Fatalf("Korisnik iivic nije pronađen: %v", err)
	}
	ivoPerms, _ := userRepo.GetUserPermissions(ivo.ID)

	// Dionica u Sektoru P (P.1.1)
	secP, err := sectionService.GetSectionWithDetails("P.1.1")
	if err != nil || secP == nil {
		t.Fatalf("Dionica P.1.1 nije pronađena: %v", err)
	}

	// Dionica u Sektoru E (E.3.1)
	secE, err := sectionService.GetSectionWithDetails("E.3.1")
	if err != nil || secE == nil {
		t.Fatalf("Dionica E.3.1 nije pronađena: %v", err)
	}

	// Pero Perić MORA moći uređivati P.1.1
	if !sectionService.CanEditSection(peroPerms, secP) {
		t.Errorf("Pero Perić (rukovoditelj Sektora P) bi MORAO moći uređivati dionicu P.1.1")
	}

	// Pero Perić NE SMIJE moći uređivati dionicu iz Sektora E
	if sectionService.CanEditSection(peroPerms, secE) {
		t.Errorf("Pero Perić NE BI SMIO moći uređivati dionicu E.3.1 iz Sektora E")
	}

	// Ivo Ivić MORA moći uređivati P.1.1
	if !sectionService.CanEditSection(ivoPerms, secP) {
		t.Errorf("Ivo Ivić bi MORAO moći uređivati dionicu P.1.1")
	}
}

// TestCreateAndEditSectionWorkflow provjerava upis nove dionice s poddionicom i njezinu izmjenu
func TestCreateAndEditSectionWorkflow(t *testing.T) {
	sectionService, _, _, _, userRepo := setupSectionTestServices(t)
	ctx := context.Background()

	iva, _ := userRepo.GetUserByUsername("ivaivic")
	ivaPerms, _ := userRepo.GetUserPermissions(iva.ID)

	from, to := 3.2, 7.45
	newSec := &models.Section{
		Code: "E.3.99", AreaID: 3,
		Notes: "Privremena dionica za testiranje",
		Parts: []models.SectionPart{{
			WatercourseCode: "rijeka-rubnica", Bank: "L", StationingKind: "rkm", KmFrom: &from, KmTo: &to,
			Extent:      "most u Rubnom – ušće Oglednice",
			Territories: []models.PartTerritory{{CountyID: 1, MunicipalityID: 3}},
			Objects:     []models.PartObject{{Name: "most u Rubnom", Bank: "L", StationingKind: "rkm", Stationing: &from}},
		}},
	}

	// Iva Ivić, rukovoditeljica područja 3, stvara novu dionicu u Sektoru E
	if err := sectionService.SaveSection(ctx, ivaPerms, newSec, true); err != nil {
		t.Fatalf("Greška pri stvaranju dionice od strane ovlaštenog korisnika: %v", err)
	}

	fetched, err := sectionService.GetSectionWithDetails("E.3.99")
	if err != nil || fetched == nil {
		t.Fatalf("Nova dionica E.3.99 nije pronađena u bazi: %v", err)
	}
	if fetched.SectorID != "E" {
		t.Errorf("sektor slijedi iz područja: očekivan E, dobiveno %q", fetched.SectorID)
	}
	if len(fetched.Parts) != 1 || fetched.Parts[0].WatercourseCode != "rijeka-rubnica" {
		t.Fatalf("poddionica nije sačuvana: %+v", fetched.Parts)
	}
	if fetched.Parts[0].Length() < 4.24 || fetched.Parts[0].Length() > 4.26 {
		t.Errorf("duljina poddionice iz raspona: %.3f, očekivano 4,250", fetched.Parts[0].Length())
	}
	if fetched.EffectiveDescription() == "" || fetched.WatercourseCode != "rijeka-rubnica" {
		t.Errorf("izvedeni opis i voda dionice: %q / %q", fetched.EffectiveDescription(), fetched.WatercourseCode)
	}
	if fetched.ObjectCount() != 1 {
		t.Errorf("objekata na dionici: %d, očekivan 1", fetched.ObjectCount())
	}

	// izmjena: napomena i druga poddionica
	fetched.Notes = "Ažurirana operativna napomena"
	fetched.Parts = append(fetched.Parts, models.SectionPart{WatercourseCode: "rijeka-rubnica", Bank: "D", Extent: "desna obala uz naselje"})
	if err := sectionService.SaveSection(ctx, ivaPerms, fetched, false); err != nil {
		t.Fatalf("Greška pri ažuriranju dionice: %v", err)
	}

	updated, _ := sectionService.GetSectionWithDetails("E.3.99")
	if updated.Notes != "Ažurirana operativna napomena" {
		t.Errorf("Očekivana nova napomena, dobiveno: %s", updated.Notes)
	}
	if len(updated.Parts) != 2 || updated.Parts[1].Seq != 2 {
		t.Errorf("druga poddionica nije upisana: %+v", updated.Parts)
	}

	// bez poddionice nema dionice
	bare := &models.Section{Code: "E.3.98", AreaID: 3}
	if err := sectionService.SaveSection(ctx, ivaPerms, bare, true); err == nil {
		t.Error("dionica bez poddionice je prošla")
	}
}
