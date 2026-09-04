package service

import (
	"errors"
	"fmt"
	"strings"

	"gocop/internal/models"
	"gocop/internal/repository"
)

var (
	ErrSectionNotFound = errors.New("dionica nije pronađena")
	ErrInvalidSection  = errors.New("neispravni podaci dionice")
)

type SectionService struct {
	sectionRepo *repository.SectionRepository
	sse         *SSEBroker
}

func NewSectionService(sectionRepo *repository.SectionRepository, sse *SSEBroker) *SectionService {
	return &SectionService{
		sectionRepo: sectionRepo,
		sse:         sse,
	}
}

// ListSections vraća filtrirane dionice
func (s *SectionService) ListSections(sectorID string, areaID int, search string) ([]models.Section, error) {
	return s.sectionRepo.ListSections(sectorID, areaID, search)
}

// GetSectionWithDetails dohvaća dionicu i sve pripadajuće djelatnike na dionici i branjenom području
func (s *SectionService) GetSectionWithDetails(code string) (*models.Section, error) {
	sec, err := s.sectionRepo.GetSectionByCode(code)
	if err != nil {
		return nil, err
	}
	if sec == nil {
		return nil, ErrSectionNotFound
	}

	personnel, err := s.sectionRepo.GetSectionPersonnel(sec.Code, sec.AreaID, sec.SectorID)
	if err == nil {
		sec.Personnel = personnel
	}

	return sec, nil
}

// CanEditSection provjerava ima li prijavljeni korisnik ovlasti mijenjati zadanu dionicu
func (s *SectionService) CanEditSection(perms *models.UserPermissions, sec *models.Section) bool {
	if perms == nil || sec == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	// Voditelji/zamjenici sektora (npr. Mario Spajić, Tomislav Kraljević na Sektoru B)
	if perms.AdminSectors[sec.SectorID] || perms.AllowedSectors[sec.SectorID] {
		return true
	}
	// Rukovoditelji branjenog područja
	if perms.AdminAreas[sec.AreaID] || perms.AllowedAreas[sec.AreaID] {
		return true
	}
	// Specifično dodijeljene dionice (rukovoditelj dionice)
	if perms.AllowedSections[sec.Code] {
		return true
	}

	return false
}

// CanCreateSectionInArea provjerava može li korisnik dodavati novu dionicu u zadano branjeno područje
func (s *SectionService) CanCreateSectionInArea(perms *models.UserPermissions, sectorID string, areaID int) bool {
	if perms == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	if sectorID != "" && (perms.AdminSectors[sectorID] || perms.AllowedSectors[sectorID]) {
		return true
	}
	if areaID > 0 && (perms.AdminAreas[areaID] || perms.AllowedAreas[areaID]) {
		return true
	}
	return false
}

// CreateSection stvara novu dionicu ako korisnik ima odgovarajuće ovlasti
func (s *SectionService) CreateSection(perms *models.UserPermissions, sec *models.Section) error {
	if sec == nil || strings.TrimSpace(sec.Code) == "" || sec.AreaID <= 0 || strings.TrimSpace(sec.SectorID) == "" {
		return ErrInvalidSection
	}

	if !s.CanCreateSectionInArea(perms, sec.SectorID, sec.AreaID) {
		return ErrUnauthorized
	}

	// Provjeri postoji li već dionica s istom šifrom
	existing, _ := s.sectionRepo.GetSectionByCode(sec.Code)
	if existing != nil {
		return fmt.Errorf("dionica sa šifrom '%s' već postoji", sec.Code)
	}

	if err := s.sectionRepo.CreateSection(sec); err != nil {
		return err
	}

	s.sse.Broadcast("section_created", fmt.Sprintf("Dodana nova dionica: %s", sec.Code), sec.Code)
	return nil
}

// UpdateSection ažurira dionicu ako korisnik ima odgovarajuće ovlasti
func (s *SectionService) UpdateSection(perms *models.UserPermissions, sec *models.Section) error {
	if sec == nil || strings.TrimSpace(sec.Code) == "" {
		return ErrInvalidSection
	}

	existing, err := s.sectionRepo.GetSectionByCode(sec.Code)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrSectionNotFound
	}

	// Provjera ovlasti na postojećoj dionici
	if !s.CanEditSection(perms, existing) {
		return ErrUnauthorized
	}

	if err := s.sectionRepo.UpdateSection(sec); err != nil {
		return err
	}

	s.sse.Broadcast("section_updated", fmt.Sprintf("Ažurirana dionica: %s", sec.Code), sec.Code)
	return nil
}

// UpdateProtectedArea ažurira tekst ugroženog područja za dionicu
func (s *SectionService) UpdateProtectedArea(code string, text string) error {
	if err := s.sectionRepo.UpdateProtectedArea(code, text); err != nil {
		return err
	}
	s.sse.Broadcast("section_updated", fmt.Sprintf("Ažurirano ugroženo područje za dionicu: %s", code), code)
	return nil
}
