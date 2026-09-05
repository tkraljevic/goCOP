package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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

// SaveSection upisuje novu ili izmijenjenu dionicu s poddionicama. Nova
// traži pravo pisanja u području, postojeća pravo uređivanja te dionice.
func (s *SectionService) SaveSection(ctx context.Context, perms *models.UserPermissions, sec *models.Section, isNew bool) error {
	if sec == nil {
		return ErrInvalidSection
	}
	sec.Code = strings.ToUpper(strings.TrimSpace(sec.Code))
	if sec.Code == "" || sec.AreaID <= 0 {
		return ErrInvalidSection
	}
	if !reSectionCode.MatchString(sec.Code) {
		return fmt.Errorf("šifra dionice ima oblik SEKTOR.PODRUČJE.BROJ, npr. B.15.5")
	}
	existing, err := s.sectionRepo.GetSectionByCode(sec.Code)
	if err != nil {
		return err
	}
	if isNew {
		if existing != nil {
			return fmt.Errorf("dionica sa šifrom '%s' već postoji", sec.Code)
		}
		// sektor nosi prvi dio šifre; područje ga mora potvrditi pri upisu
		if sec.SectorID == "" {
			sec.SectorID = strings.ToUpper(sec.Code[:strings.Index(sec.Code, ".")])
		}
		if !s.CanCreateSectionInArea(perms, sec.SectorID, sec.AreaID) {
			return ErrUnauthorized
		}
	} else {
		if existing == nil {
			return ErrSectionNotFound
		}
		if !s.CanEditSection(perms, existing) {
			return ErrUnauthorized
		}
		sec.AreaID, sec.SectorID = existing.AreaID, existing.SectorID
	}
	if err := validateParts(sec); err != nil {
		return err
	}
	if err := s.sectionRepo.SaveSection(ctx, sec); err != nil {
		return err
	}
	if isNew {
		s.sse.Broadcast("section_created", fmt.Sprintf("Dodana nova dionica: %s", sec.Code), sec.Code)
	} else {
		s.sse.Broadcast("section_updated", fmt.Sprintf("Ažurirana dionica: %s", sec.Code), sec.Code)
	}
	return nil
}

var reSectionCode = regexp.MustCompile(`^[A-F]\.\d{1,2}\.\d{1,3}$`)

// validateParts provjerava poddionice: bar jedna, svaka s vodom ili opisom,
// raspon uređen od manje prema većoj stacionaži
func validateParts(sec *models.Section) error {
	if len(sec.Parts) == 0 {
		return fmt.Errorf("dionica mora imati bar jednu poddionicu")
	}
	for i := range sec.Parts {
		p := &sec.Parts[i]
		p.Seq = i + 1
		if strings.TrimSpace(p.WatercourseCode) == "" && strings.TrimSpace(p.Description) == "" {
			return fmt.Errorf("poddionica %d nema vodotok", i+1)
		}
		if p.KmFrom != nil && p.KmTo != nil && *p.KmFrom > *p.KmTo {
			*p.KmFrom, *p.KmTo = *p.KmTo, *p.KmFrom
		}
		if p.Bank != "" && p.Bank != "L" && p.Bank != "D" && p.Bank != "LD" {
			return fmt.Errorf("poddionica %d: obala je L, D ili LD", i+1)
		}
		for j := range p.Objects {
			if strings.TrimSpace(p.Objects[j].Name) == "" && p.Objects[j].StructureID == "" {
				return fmt.Errorf("poddionica %d: objekt bez naziva", i+1)
			}
		}
		for j := range p.Embankments {
			if strings.TrimSpace(p.Embankments[j].Name) == "" && p.Embankments[j].StructureID == "" {
				return fmt.Errorf("poddionica %d: nasip bez naziva", i+1)
			}
		}
	}
	return nil
}
