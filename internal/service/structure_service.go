package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
)

// StructureService je pravilo tko smije što s registrom objekata
type StructureService struct {
	repo *repository.StructureRepository
}

func NewStructureService(repo *repository.StructureRepository) *StructureService {
	return &StructureService{repo: repo}
}

func (s *StructureService) List(ctx context.Context, sectorID string, areaID int, kind, search string) ([]models.Structure, error) {
	return s.repo.ListStructures(ctx, sectorID, areaID, kind, search)
}

func (s *StructureService) Get(ctx context.Context, id uuid.UUID) (*models.Structure, error) {
	return s.repo.GetStructure(ctx, id)
}

// CanEdit: globalni administrator, administrator sektora ili područja u
// kojem objekt stoji, ili tko smije pisati po tom sektoru ili području
func (s *StructureService) CanEdit(perms *models.UserPermissions, st *models.Structure) bool {
	if perms == nil || st == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	return perms.AdminSectors[st.SectorID] || perms.AdminAreas[st.AreaID] ||
		perms.AllowedSectors[st.SectorID] || perms.AllowedAreas[st.AreaID]
}

// CanCreate: tko smije pisati u ijednom sektoru ili području
func (s *StructureService) CanCreate(perms *models.UserPermissions) bool {
	return perms != nil && (perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 || len(perms.AdminAreas) > 0 ||
		len(perms.AllowedSectors) > 0 || len(perms.AllowedAreas) > 0)
}

func (s *StructureService) validate(st *models.Structure) error {
	st.Name = strings.TrimSpace(st.Name)
	if st.Name == "" {
		return errors.New("naziv objekta je obavezan")
	}
	if st.AreaID <= 0 || st.SectorID == "" {
		return errors.New("objekt mora pripadati sektoru i branjenom području")
	}
	known := false
	for _, k := range models.StructureKinds {
		if k == st.Kind {
			known = true
		}
	}
	if !known {
		return fmt.Errorf("nepoznata vrsta objekta: %s", st.Kind)
	}
	if st.Code == "" {
		st.Code = fmt.Sprintf("bp%d-%s", st.AreaID, Slugify(st.Name))
	}
	if st.ZeroDatum != nil && st.ZeroDatumSystem == "" {
		st.ZeroDatumSystem = "TRST"
	}
	if st.Origin == "" {
		st.Origin = "RUČNI_UNOS"
	}
	return nil
}

func (s *StructureService) Create(ctx context.Context, perms *models.UserPermissions, st *models.Structure) error {
	if err := s.validate(st); err != nil {
		return err
	}
	if !s.CanEdit(perms, st) {
		return errors.New("nemate pravo dodavati objekte u to područje")
	}
	if existing, _ := s.repo.GetByCode(ctx, st.Code); existing != nil {
		return fmt.Errorf("objekt sa šifrom %s već postoji", st.Code)
	}
	return s.repo.CreateStructure(ctx, st)
}

func (s *StructureService) Update(ctx context.Context, perms *models.UserPermissions, st *models.Structure) error {
	current, err := s.repo.GetStructure(ctx, st.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return errors.New("objekt nije pronađen")
	}
	if !s.CanEdit(perms, current) {
		return errors.New("nemate pravo uređivati ovaj objekt")
	}
	if st.Code == "" {
		st.Code = current.Code
	}
	if st.Origin == "" {
		st.Origin = current.Origin
	}
	if err := s.validate(st); err != nil {
		return err
	}
	if !s.CanEdit(perms, st) {
		return errors.New("nemate pravo premjestiti objekt u to područje")
	}
	return s.repo.UpdateStructure(ctx, st)
}

func (s *StructureService) Delete(ctx context.Context, perms *models.UserPermissions, id uuid.UUID) error {
	current, err := s.repo.GetStructure(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return errors.New("objekt nije pronađen")
	}
	if !s.CanEdit(perms, current) {
		return errors.New("nemate pravo brisati ovaj objekt")
	}
	return s.repo.DeleteStructure(ctx, id)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

var slugLetters = strings.NewReplacer("č", "c", "ć", "c", "ž", "z", "š", "s", "đ", "d",
	"Č", "c", "Ć", "c", "Ž", "z", "Š", "s", "Đ", "d")

// Slugify pretvara naziv u šifru: bez dijakritika, mala slova, crtice
func Slugify(name string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(slugLetters.Replace(name)), "-"), "-")
}
