package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
)

type TerritoryService struct {
	territoryRepo  *repository.TerritoryRepository
	sectionService *SectionService
}

func NewTerritoryService(territoryRepo *repository.TerritoryRepository, sectionService *SectionService) *TerritoryService {
	return &TerritoryService{
		territoryRepo:  territoryRepo,
		sectionService: sectionService,
	}
}

func (s *TerritoryService) ListCounties(ctx context.Context) ([]models.County, error) {
	return s.territoryRepo.ListCounties(ctx)
}

func (s *TerritoryService) GetCountyByID(ctx context.Context, id int) (*models.County, error) {
	return s.territoryRepo.GetCountyByID(ctx, id)
}

func (s *TerritoryService) ListMunicipalities(ctx context.Context, countyID int, mType string, query string) ([]models.Municipality, error) {
	return s.territoryRepo.ListMunicipalities(ctx, countyID, mType, query)
}

// GetMunicipalityByID vraća jedan grad ili općinu. Popis je malen (oko
// pola tisuće redaka), pa se čita cijeli i traži u memoriji.
func (s *TerritoryService) GetMunicipalityByID(ctx context.Context, id int) (*models.Municipality, error) {
	all, err := s.territoryRepo.ListMunicipalities(ctx, 0, "", "")
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, nil
}

func (s *TerritoryService) ListSettlements(ctx context.Context, municipalityID int, countyID int, query string) ([]models.Settlement, error) {
	return s.territoryRepo.ListSettlements(ctx, municipalityID, countyID, query)
}

func (s *TerritoryService) GetSectionTerritories(ctx context.Context, sectionCode string) ([]models.SectionTerritory, error) {
	return s.territoryRepo.GetSectionTerritories(ctx, sectionCode)
}

func (s *TerritoryService) AddSectionTerritories(
	ctx context.Context,
	perms *models.UserPermissions,
	sectionCode string,
	countyID int,
	municipalityID int,
	settlementIDs []int,
) error {
	sec, err := s.sectionService.GetSectionWithDetails(sectionCode)
	if err != nil {
		return err
	}
	if !s.sectionService.CanEditSection(perms, sec) {
		return fmt.Errorf("nemate ovlasti za dodavanje ugroženih područja na dionicu %s", sectionCode)
	}

	if countyID <= 0 || municipalityID <= 0 {
		return fmt.Errorf("županija i grad/općina su obavezna polja")
	}

	// Ako nije odabrano pojedinačno naselje, dodaje se cijela općina/grad.
	// I tada se tekst ugroženog područja mora osvježiti — raniji povratak
	// ovdje ostavljao je dionicu s pridruženom općinom, a bez teksta.
	if len(settlementIDs) == 0 {
		item := &models.SectionTerritory{
			ID:             uuid.New().String(),
			SectionCode:    sectionCode,
			CountyID:       countyID,
			MunicipalityID: municipalityID,
			SettlementID:   nil,
			CreatedAt:      time.Now(),
		}
		if err := s.territoryRepo.AddSectionTerritory(ctx, item); err != nil {
			return err
		}
		_, _ = s.SyncSectionProtectedArea(ctx, sectionCode)
		return nil
	}

	// Dodavanje odabranih naselja
	for _, sid := range settlementIDs {
		val := sid
		item := &models.SectionTerritory{
			ID:             uuid.New().String(),
			SectionCode:    sectionCode,
			CountyID:       countyID,
			MunicipalityID: municipalityID,
			SettlementID:   &val,
			CreatedAt:      time.Now(),
		}
		if err := s.territoryRepo.AddSectionTerritory(ctx, item); err != nil {
			return err
		}
	}

	// Automatski sinkroniziraj i generiraj ažurirani tekst ugroženog područja
	_, _ = s.SyncSectionProtectedArea(ctx, sectionCode)

	return nil
}

func (s *TerritoryService) RemoveSectionTerritory(ctx context.Context, perms *models.UserPermissions, id string, sectionCode string) error {
	sec, err := s.sectionService.GetSectionWithDetails(sectionCode)
	if err != nil {
		return err
	}
	if !s.sectionService.CanEditSection(perms, sec) {
		return fmt.Errorf("nemate ovlasti za uklanjanje ugroženog područja s dionice %s", sectionCode)
	}

	if err := s.territoryRepo.RemoveSectionTerritory(ctx, id); err != nil {
		return err
	}

	// Automatski sinkroniziraj i generiraj ažurirani tekst ugroženog područja
	_, _ = s.SyncSectionProtectedArea(ctx, sectionCode)

	return nil
}

// GenerateProtectedAreaText generira formatirani tekst ugroženih područja prema povezanim jedinicama
// Format: **Županija**; Općina/Grad 1: Naselje 1, Naselje 2; Općina/Grad 2: Naselje 3...
func GenerateProtectedAreaText(territories []models.SectionTerritory) string {
	if len(territories) == 0 {
		return ""
	}

	type muniGroup struct {
		name        string
		settlements []string
	}
	type countyGroup struct {
		name      string
		munis     map[int]*muniGroup
		muniOrder []int
	}

	countyOrder := []int{}
	counties := make(map[int]*countyGroup)

	for _, t := range territories {
		cg, exists := counties[t.CountyID]
		if !exists {
			cg = &countyGroup{
				name:      t.CountyName,
				munis:     make(map[int]*muniGroup),
				muniOrder: []int{},
			}
			counties[t.CountyID] = cg
			countyOrder = append(countyOrder, t.CountyID)
		}

		mg, mExists := cg.munis[t.MunicipalityID]
		if !mExists {
			mg = &muniGroup{
				name:        t.MunicipalityName,
				settlements: []string{},
			}
			cg.munis[t.MunicipalityID] = mg
			cg.muniOrder = append(cg.muniOrder, t.MunicipalityID)
		}

		if t.SettlementName != "" {
			mg.settlements = append(mg.settlements, t.SettlementName)
		}
	}

	var countyBlocks []string
	for _, cid := range countyOrder {
		cg := counties[cid]
		cName := strings.TrimSuffix(cg.name, " županija")
		if cName == "" {
			cName = cg.name
		}

		var parts []string
		parts = append(parts, fmt.Sprintf("**%s**", cName))

		for _, mid := range cg.muniOrder {
			mg := cg.munis[mid]
			if len(mg.settlements) > 0 {
				parts = append(parts, fmt.Sprintf("%s: %s", mg.name, strings.Join(mg.settlements, ", ")))
			} else {
				parts = append(parts, mg.name)
			}
		}

		countyBlocks = append(countyBlocks, strings.Join(parts, "; "))
	}

	return strings.Join(countyBlocks, "; ")
}

// SyncSectionProtectedArea sinkronizira tekst ugroženog područja dionice prema relacijama iz baze
func (s *TerritoryService) SyncSectionProtectedArea(ctx context.Context, sectionCode string) (string, error) {
	territories, err := s.territoryRepo.GetSectionTerritories(ctx, sectionCode)
	if err != nil {
		return "", err
	}
	text := GenerateProtectedAreaText(territories)
	if err := s.sectionService.UpdateProtectedArea(sectionCode, text); err != nil {
		return "", err
	}
	return text, nil
}

func (s *TerritoryService) GetTerritoryCounts(ctx context.Context) (int, int, int, error) {
	return s.territoryRepo.GetTerritoryCounts(ctx)
}

// Helper to sync multiple affected sections
func (s *TerritoryService) syncAffectedSections(ctx context.Context, codes []string) {
	for _, code := range codes {
		_, _ = s.SyncSectionProtectedArea(ctx, code)
	}
}

// CreateCounty dodaje novu županiju
func (s *TerritoryService) CreateCounty(ctx context.Context, perms *models.UserPermissions, c *models.County) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može dodavati županije")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("naziv županije je obavezan")
	}
	return s.territoryRepo.CreateCounty(ctx, c)
}

// UpdateCounty ažurira županiju
func (s *TerritoryService) UpdateCounty(ctx context.Context, perms *models.UserPermissions, c *models.County) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može uređivati županije")
	}
	if c.ID <= 0 || strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("neispravan unos županije")
	}
	if err := s.territoryRepo.UpdateCounty(ctx, c); err != nil {
		return err
	}
	affected, _ := s.territoryRepo.GetSectionsAffectedByTerritory(ctx, c.ID, 0, 0)
	s.syncAffectedSections(ctx, affected)
	return nil
}

// DeleteCounty briše županiju
func (s *TerritoryService) DeleteCounty(ctx context.Context, perms *models.UserPermissions, id int) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može brisati županije")
	}
	affected, _ := s.territoryRepo.GetSectionsAffectedByTerritory(ctx, id, 0, 0)
	if err := s.territoryRepo.DeleteCounty(ctx, id); err != nil {
		return err
	}
	s.syncAffectedSections(ctx, affected)
	return nil
}

// CreateMunicipality dodaje novi grad ili općinu
func (s *TerritoryService) CreateMunicipality(ctx context.Context, perms *models.UserPermissions, m *models.Municipality) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može dodavati gradove i općine")
	}
	if m.CountyID <= 0 {
		return fmt.Errorf("županija je obavezna")
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("naziv grada/općine je obavezan")
	}
	m.Type = strings.ToUpper(strings.TrimSpace(m.Type))
	if m.Type != "GRAD" && m.Type != "OPCINA" {
		m.Type = "OPCINA"
	}
	return s.territoryRepo.CreateMunicipality(ctx, m)
}

// UpdateMunicipality ažurira/preimenuje grad ili općinu
func (s *TerritoryService) UpdateMunicipality(ctx context.Context, perms *models.UserPermissions, m *models.Municipality) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može uređivati gradove i općine")
	}
	if m.ID <= 0 || strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("neispravan unos grada/općine")
	}
	m.Type = strings.ToUpper(strings.TrimSpace(m.Type))
	if m.Type != "GRAD" && m.Type != "OPCINA" {
		m.Type = "OPCINA"
	}
	if err := s.territoryRepo.UpdateMunicipality(ctx, m); err != nil {
		return err
	}
	affected, _ := s.territoryRepo.GetSectionsAffectedByTerritory(ctx, 0, m.ID, 0)
	s.syncAffectedSections(ctx, affected)
	return nil
}

// DeleteMunicipality briše grad ili općinu
func (s *TerritoryService) DeleteMunicipality(ctx context.Context, perms *models.UserPermissions, id int) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može brisati gradove i općine")
	}
	affected, _ := s.territoryRepo.GetSectionsAffectedByTerritory(ctx, 0, id, 0)
	if err := s.territoryRepo.DeleteMunicipality(ctx, id); err != nil {
		return err
	}
	s.syncAffectedSections(ctx, affected)
	return nil
}

// CreateSettlement dodaje novo naselje
func (s *TerritoryService) CreateSettlement(ctx context.Context, perms *models.UserPermissions, sModel *models.Settlement) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može dodavati naselja")
	}
	if sModel.MunicipalityID <= 0 {
		return fmt.Errorf("grad/općina je obavezna")
	}
	if strings.TrimSpace(sModel.Name) == "" {
		return fmt.Errorf("naziv naselja je obavezan")
	}
	return s.territoryRepo.CreateSettlement(ctx, sModel)
}

// UpdateSettlement ažurira / preimenuje naselje
func (s *TerritoryService) UpdateSettlement(ctx context.Context, perms *models.UserPermissions, sModel *models.Settlement) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može uređivati naselja")
	}
	if sModel.ID <= 0 || strings.TrimSpace(sModel.Name) == "" {
		return fmt.Errorf("neispravan unos naselja")
	}
	if err := s.territoryRepo.UpdateSettlement(ctx, sModel); err != nil {
		return err
	}
	affected, _ := s.territoryRepo.GetSectionsAffectedByTerritory(ctx, 0, 0, sModel.ID)
	s.syncAffectedSections(ctx, affected)
	return nil
}

// DeleteSettlement briše naselje
func (s *TerritoryService) DeleteSettlement(ctx context.Context, perms *models.UserPermissions, id int) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("samo globalni administrator može brisati naselja")
	}
	affected, _ := s.territoryRepo.GetSectionsAffectedByTerritory(ctx, 0, 0, id)
	if err := s.territoryRepo.DeleteSettlement(ctx, id); err != nil {
		return err
	}
	s.syncAffectedSections(ctx, affected)
	return nil
}
