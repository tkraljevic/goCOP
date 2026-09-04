package service

import (
	"context"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func setupTestTerritoryService(t *testing.T) (*TerritoryService, *SectionService, *models.UserPermissions) {
	database, err := db.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("Greška pri otvaranju in-memory baze: %v", err)
	}

	if err := db.InitSchema(database); err != nil {
		t.Fatalf("Greška pri inicijalizaciji sheme: %v", err)
	}

	if err := db.SeedInitialData(database); err != nil {
		t.Fatalf("Greška pri unosu početnih podataka: %v", err)
	}

	sectionRepo := repository.NewSectionRepository(database, ledger.New(database, "test-node"))
	territoryRepo := repository.NewTerritoryRepository(database, ledger.New(database, "test-node"))
	sseBroker := NewSSEBroker()
	sectionService := NewSectionService(sectionRepo, sseBroker)
	territoryService := NewTerritoryService(territoryRepo, sectionService)

	// Admin permissions
	adminPerms := &models.UserPermissions{
		IsGlobalAdmin: true,
	}

	return territoryService, sectionService, adminPerms
}

func TestTerritorySeedingAndCounts(t *testing.T) {
	ts, _, _ := setupTestTerritoryService(t)
	ctx := context.Background()

	counties, munis, settlements, err := ts.GetTerritoryCounts(ctx)
	if err != nil {
		t.Fatalf("GetTerritoryCounts error: %v", err)
	}

	if counties != 21 {
		t.Errorf("Očekivano 21 županija, dobiveno: %d", counties)
	}
	if munis < 500 {
		t.Errorf("Očekivano više od 500 gradova i općina, dobiveno: %d", munis)
	}
	if settlements < 6000 {
		t.Errorf("Očekivano više od 6000 naselja, dobiveno: %d", settlements)
	}

	list, err := ts.ListCounties(ctx)
	if err != nil {
		t.Fatalf("ListCounties error: %v", err)
	}
	if len(list) != 21 {
		t.Fatalf("ListCounties dužina: očekivano 21, dobiveno %d", len(list))
	}

	// Provjera Osječko-baranjske županije
	var obz *models.County
	for _, c := range list {
		if c.Code == "OB" {
			obz = &c
			break
		}
	}
	if obz == nil {
		t.Fatalf("Nije pronađena Osječko-baranjska županija (kod OB)")
	}
	if obz.Seat != "Osijek" {
		t.Errorf("Sjedište OBŽ: očekivano Osijek, dobiveno %s", obz.Seat)
	}
	if obz.Prefect != "Nataša Tramišak (županica)" {
		t.Errorf("Županica OBŽ: očekivano Nataša Tramišak (županica), dobiveno %s", obz.Prefect)
	}
}

func TestAddAndRemoveSectionTerritoryWorkflow(t *testing.T) {
	ts, _, adminPerms := setupTestTerritoryService(t)
	ctx := context.Background()

	// 1. Dohvati županiju Vukovarsko-srijemsku
	counties, err := ts.ListCounties(ctx)
	if err != nil {
		t.Fatalf("ListCounties error: %v", err)
	}
	var vszID int
	for _, c := range counties {
		if c.Code == "VS" {
			vszID = c.ID
			break
		}
	}
	if vszID == 0 {
		t.Fatalf("Vukovarsko-srijemska županija nije pronađena")
	}

	// 2. Dohvati općinu Trpinja u VSŽ
	munis, err := ts.ListMunicipalities(ctx, vszID, "OPCINA", "Trpinja")
	if err != nil {
		t.Fatalf("ListMunicipalities error: %v", err)
	}
	if len(munis) == 0 {
		t.Fatalf("Općina Trpinja nije pronađena")
	}
	trpinjaID := munis[0].ID

	// 3. Dohvati naselja općine Trpinja (Bršadin, Bobota, Pačetin, Vera...)
	settlements, err := ts.ListSettlements(ctx, trpinjaID, vszID, "")
	if err != nil {
		t.Fatalf("ListSettlements error: %v", err)
	}
	if len(settlements) == 0 {
		t.Fatalf("Općina Trpinja nema naselja u bazi")
	}

	var brsadinID, pacetinID int
	for _, s := range settlements {
		if s.Name == "Bršadin" {
			brsadinID = s.ID
		} else if s.Name == "Pačetin" {
			pacetinID = s.ID
		}
	}
	if brsadinID == 0 {
		t.Fatalf("Naselje Bršadin nije pronađeno u općini Trpinja")
	}

	// 4. Provjeri početni broj pridruženih relacija iz seeda
	initialAssigned, err := ts.GetSectionTerritories(ctx, "B.15.1")
	if err != nil {
		t.Fatalf("GetSectionTerritories error: %v", err)
	}
	initialCount := len(initialAssigned)

	// 5. Dodaj novu vezu (naselje Bršadin i Pačetin)
	err = ts.AddSectionTerritories(ctx, adminPerms, "B.15.1", vszID, trpinjaID, []int{brsadinID, pacetinID})
	if err != nil {
		t.Fatalf("Greška pri dodavanju teritorija na dionicu B.15.1: %v", err)
	}

	assigned, err := ts.GetSectionTerritories(ctx, "B.15.1")
	if err != nil {
		t.Fatalf("GetSectionTerritories error: %v", err)
	}
	if len(assigned) != initialCount+2 {
		t.Fatalf("Očekivano %d pridruženih naselja na B.15.1, dobiveno: %d", initialCount+2, len(assigned))
	}

	// 6. Ukloni jedno dodano naselje
	err = ts.RemoveSectionTerritory(ctx, adminPerms, assigned[len(assigned)-1].ID, "B.15.1")
	if err != nil {
		t.Fatalf("RemoveSectionTerritory error: %v", err)
	}

	// 7. Provjeri da je preostalo initialCount + 1
	assignedAfter, err := ts.GetSectionTerritories(ctx, "B.15.1")
	if err != nil {
		t.Fatalf("GetSectionTerritories after remove error: %v", err)
	}
	if len(assignedAfter) != initialCount+1 {
		t.Errorf("Očekivano %d preostalih naselja, dobiveno: %d", initialCount+1, len(assignedAfter))
	}
}

func TestGenerateProtectedAreaText(t *testing.T) {
	ts, _, _ := setupTestTerritoryService(t)
	ctx := context.Background()

	// Dohvati B.15.1 relacije
	territories, err := ts.GetSectionTerritories(ctx, "B.15.1")
	if err != nil {
		t.Fatalf("GetSectionTerritories error: %v", err)
	}

	text := GenerateProtectedAreaText(territories)
	if text == "" {
		t.Fatalf("Generirani tekst je prazan")
	}

	// Provjeri da sadrži Vukovarsko-srijemska, Bogdanovci, Vukovar, Trpinja
	expectedSubstrings := []string{
		"**Vukovarsko-srijemska**",
		"Bogdanovci",
		"Vukovar",
		"Trpinja",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(text, sub) {
			t.Errorf("Generirani tekst ne sadrži očekivani podstring %q: %s", sub, text)
		}
	}
}

func TestCreateAndUpdateMunicipality(t *testing.T) {
	ts, _, adminPerms := setupTestTerritoryService(t)
	ctx := context.Background()

	// 1. Dodaj novi grad u županiju 14 (OBŽ)
	newMuni := &models.Municipality{
		CountyID:   14,
		Name:       "Novi Grad Test",
		Type:       "GRAD",
		HeadTitle:  "Gradonačelnik",
		HeadName:   "Ivan Horvat",
		PostalCode: "31999",
	}

	err := ts.CreateMunicipality(ctx, adminPerms, newMuni)
	if err != nil {
		t.Fatalf("CreateMunicipality error: %v", err)
	}
	if newMuni.ID <= 0 {
		t.Fatalf("Očekivan generirani ID za novi grad, dobiveno: %d", newMuni.ID)
	}

	// 2. Ažuriraj / preimenuj
	newMuni.Name = "Ažurirani Grad Test"
	err = ts.UpdateMunicipality(ctx, adminPerms, newMuni)
	if err != nil {
		t.Fatalf("UpdateMunicipality error: %v", err)
	}

	// 3. Provjeri dohvatom
	list, err := ts.ListMunicipalities(ctx, 14, "GRAD", "Ažurirani Grad Test")
	if err != nil {
		t.Fatalf("ListMunicipalities error: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Ažurirani Grad Test" {
		t.Fatalf("Grad nije pronađen nakon ažuriranja")
	}

	// 4. Obriši
	err = ts.DeleteMunicipality(ctx, adminPerms, newMuni.ID)
	if err != nil {
		t.Fatalf("DeleteMunicipality error: %v", err)
	}
}

func TestCreateRenameAndDeleteSettlement(t *testing.T) {
	ts, _, adminPerms := setupTestTerritoryService(t)
	ctx := context.Background()

	// 1. Dodaj novo naselje u općinu Trpinja (ID 408)
	newSett := &models.Settlement{
		MunicipalityID: 408,
		CountyID:       16,
		Name:           "Novo Test Selo",
	}

	err := ts.CreateSettlement(ctx, adminPerms, newSett)
	if err != nil {
		t.Fatalf("CreateSettlement error: %v", err)
	}
	if newSett.ID <= 0 {
		t.Fatalf("Očekivan generirani ID za novo naselje, dobiveno: %d", newSett.ID)
	}

	// 2. Preimenuj naselje
	newSett.Name = "Preimenovano Test Selo"
	err = ts.UpdateSettlement(ctx, adminPerms, newSett)
	if err != nil {
		t.Fatalf("UpdateSettlement error: %v", err)
	}

	// 3. Provjeri dohvatom
	settlements, err := ts.ListSettlements(ctx, 408, 16, "Preimenovano Test Selo")
	if err != nil {
		t.Fatalf("ListSettlements error: %v", err)
	}
	if len(settlements) != 1 || settlements[0].Name != "Preimenovano Test Selo" {
		t.Fatalf("Naselje nije pronađeno nakon preimenovanja")
	}

	// 4. Obriši naselje
	err = ts.DeleteSettlement(ctx, adminPerms, newSett.ID)
	if err != nil {
		t.Fatalf("DeleteSettlement error: %v", err)
	}

	settlementsAfter, _ := ts.ListSettlements(ctx, 408, 16, "Preimenovano Test Selo")
	if len(settlementsAfter) != 0 {
		t.Fatalf("Naselje nije obrisano iz baze")
	}
}
