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

	if !db.UseRepoData() {
		t.Skip("data/ s registrima nije dostupan — registri stoje izvan repozitorija")
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

// Za web stranicu ne trebaju registri iz data/, pa ovaj servis stoji na
// praznoj shemi i test se ne preskače kad podaci nisu uz repozitorij.
func setupEmptyTerritoryService(t *testing.T) (*TerritoryService, *models.UserPermissions) {
	t.Helper()

	database, err := db.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("in-memory baza: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := db.InitSchema(database); err != nil {
		t.Fatalf("shema: %v", err)
	}

	rec := ledger.New(database, "test-node")
	territoryRepo := repository.NewTerritoryRepository(database, rec)
	sectionRepo := repository.NewSectionRepository(database, rec)
	sectionService := NewSectionService(sectionRepo, NewSSEBroker())

	return NewTerritoryService(territoryRepo, sectionService), &models.UserPermissions{IsGlobalAdmin: true}
}

// Adresa web stranice prolazi kroz servis — i iz obrasca i iz CSV uvoza — pa
// se ovdje provjerava da se upisano bez sheme uredno spremi, da se neispravno
// odbije, i da spremljeno dođe natrag iz baze. Isto za županiju i za općinu.
func TestWebsiteNormalizationAndValidation(t *testing.T) {
	ts, perms := setupEmptyTerritoryService(t)
	ctx := context.Background()

	county := &models.County{Name: "Probna županija", Seat: "Probno", Website: "probna-zupanija.hr"}
	if err := ts.CreateCounty(ctx, perms, county); err != nil {
		t.Fatalf("CreateCounty: %v", err)
	}
	if county.Website != "https://probna-zupanija.hr" {
		t.Errorf("županiji je spremljena adresa %q, očekivano https://probna-zupanija.hr", county.Website)
	}

	saved, err := ts.GetCountyByID(ctx, county.ID)
	if err != nil || saved == nil {
		t.Fatalf("GetCountyByID: %v", err)
	}
	if saved.Website != "https://probna-zupanija.hr" {
		t.Errorf("iz baze je pročitana adresa županije %q", saved.Website)
	}

	county.Website = "javascript:alert(1)"
	if err := ts.UpdateCounty(ctx, perms, county); err == nil {
		t.Error("UpdateCounty je prihvatio javascript: adresu")
	}

	muni := &models.Municipality{CountyID: county.ID, Name: "Probna općina", Type: "OPCINA",
		Website: "  www.probna-opcina.hr  "}
	if err := ts.CreateMunicipality(ctx, perms, muni); err != nil {
		t.Fatalf("CreateMunicipality: %v", err)
	}
	if muni.Website != "https://www.probna-opcina.hr" {
		t.Errorf("općini je spremljena adresa %q, očekivano https://www.probna-opcina.hr", muni.Website)
	}

	savedMuni, err := ts.GetMunicipalityByID(ctx, muni.ID)
	if err != nil || savedMuni == nil {
		t.Fatalf("GetMunicipalityByID: %v", err)
	}
	if savedMuni.Website != "https://www.probna-opcina.hr" {
		t.Errorf("iz baze je pročitana adresa općine %q", savedMuni.Website)
	}

	// Adresa koja se predstavlja kao općinska a vodi drugamo
	savedMuni.Website = "https://probna-opcina.hr@tudja-stranica.com"
	if err := ts.UpdateMunicipality(ctx, perms, savedMuni); err == nil {
		t.Error("UpdateMunicipality je prihvatio adresu s korisničkim dijelom")
	}

	// Prazno polje je dopušteno: stranica se ne mora znati
	savedMuni.Website = ""
	if err := ts.UpdateMunicipality(ctx, perms, savedMuni); err != nil {
		t.Errorf("brisanje adrese nije prošlo: %v", err)
	}
}
