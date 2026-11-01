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
// kojem objekt stoji, tko smije pisati po tom sektoru ili području, ili na
// dionici uz koju objekt stoji, kad je objekt iz područja te dionice
func (s *StructureService) CanEdit(perms *models.UserPermissions, st *models.Structure) bool {
	if perms == nil || st == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	if perms.AdminSectors[st.SectorID] || perms.AdminAreas[st.AreaID] ||
		perms.AllowedSectors[st.SectorID] || perms.AllowedAreas[st.AreaID] {
		return true
	}
	// po dionici samo objekt njezina područja: objekt drugog područja koji
	// stoji i na njoj (CS Budžak područja 16 na B.34.1) vodi njegovo područje
	if !perms.RadiNaDionicamaU(st.AreaID) {
		return false
	}
	for _, code := range st.SectionCodes {
		if perms.AllowedSections[code] {
			return true
		}
	}
	return false
}

// CanCreate: tko smije pisati u ijednom sektoru ili području
func (s *StructureService) CanCreate(perms *models.UserPermissions) bool {
	return perms != nil && (perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 || len(perms.AdminAreas) > 0 ||
		len(perms.AllowedSectors) > 0 || len(perms.AllowedAreas) > 0)
}

// provjeriVeze: sektor dolazi iz područja, a ustava smije biti u sklopu
// crpne stanice
func (s *StructureService) provjeriVeze(ctx context.Context, st *models.Structure) error {
	if err := s.sektorIzPodrucja(ctx, st); err != nil {
		return err
	}
	return s.provjeriUSklopu(ctx, st)
}

// Sklop su crpna stanica u čijem je sklopu ustava i ustave u sklopu crpne
// stanice; za stranicu objekta
func (s *StructureService) Sklop(ctx context.Context, st *models.Structure) (*models.Structure, []models.Structure) {
	var cs *models.Structure
	if id, err := uuid.Parse(st.USklopuID); err == nil {
		if nadjena, err := s.Get(ctx, id); err == nil {
			cs = nadjena
		}
	}
	ustave, err := s.UstaveUSklopu(ctx, st)
	if err != nil {
		return cs, nil
	}
	return cs, ustave
}

// ZapornicaObjekta: nosi li očitanje objekta položaj zapornice, a kad je
// ustava u sklopu crpne stanice, stanica na kojoj se zapornica upisuje
func (s *StructureService) ZapornicaObjekta(ctx context.Context, st *models.Structure) (bool, *models.Structure) {
	if st.USklopuID != "" {
		cs, _ := s.Sklop(ctx, st) // ustava nema svojih ustava u sklopu
		return false, cs
	}
	if st.ImaZapornicu() {
		return true, nil
	}
	ustave, err := s.UstaveUSklopu(ctx, st)
	return err == nil && len(ustave) > 0, nil
}

// provjeriUSklopu: u sklopu crpne stanice smije biti samo ustava, i to
// postojeće crpne stanice
func (s *StructureService) provjeriUSklopu(ctx context.Context, st *models.Structure) error {
	st.USklopuID = strings.TrimSpace(st.USklopuID)
	if st.USklopuID == "" {
		return nil
	}
	if st.Kind != models.StructureKindSluice {
		return errors.New("u sklopu crpne stanice može biti samo ustava")
	}
	id, err := uuid.Parse(st.USklopuID)
	if err != nil {
		return errors.New("crpna stanica u čijem je sklopu ustava nije pronađena")
	}
	cs, err := s.Get(ctx, id)
	if err != nil || cs == nil || !cs.IsPumpingStation() {
		return errors.New("ustava može biti u sklopu samo crpne stanice")
	}
	return nil
}

// UstaveUSklopu su ustave u sklopu crpne stanice; njihova se zapornica
// upisuje u očitanju stanice
func (s *StructureService) UstaveUSklopu(ctx context.Context, cs *models.Structure) ([]models.Structure, error) {
	if cs == nil || !cs.IsPumpingStation() {
		return nil, nil
	}
	ustave, err := s.List(ctx, "", 0, models.StructureKindSluice, "")
	if err != nil {
		return nil, err
	}
	var out []models.Structure
	for _, u := range ustave {
		if u.USklopuID == cs.ID.String() {
			out = append(out, u)
		}
	}
	return out, nil
}

func (s *StructureService) validate(ctx context.Context, st *models.Structure) error {
	st.Name = strings.TrimSpace(st.Name)
	if st.Name == "" {
		return errors.New("naziv objekta je obavezan")
	}
	if st.AreaID <= 0 {
		return errors.New("objekt mora pripadati sektoru i branjenom području")
	}
	if err := s.provjeriVeze(ctx, st); err != nil {
		return err
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

// sektorIzPodrucja upisuje objektu sektor njegova branjenog područja. Pravo
// se priznaje i po sektoru, pa se zadani sektor koji nije sektor područja
// odbija, a ne prihvaća.
func (s *StructureService) sektorIzPodrucja(ctx context.Context, st *models.Structure) error {
	sektor, err := s.repo.SektorPodrucja(ctx, st.AreaID)
	if err != nil {
		return err
	}
	if zadani := strings.TrimSpace(st.SectorID); zadani != "" && !strings.EqualFold(zadani, sektor) {
		return fmt.Errorf("branjeno područje %d pripada sektoru %s, a za objekt je zadan sektor %s", st.AreaID, sektor, zadani)
	}
	st.SectorID = sektor
	return nil
}

func (s *StructureService) Create(ctx context.Context, perms *models.UserPermissions, st *models.Structure) error {
	if err := s.validate(ctx, st); err != nil {
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
	if err := s.validate(ctx, st); err != nil {
		return err
	}
	// premještaj u drugo područje traži pravo i ondje; tko objekt uređuje po
	// dionici, ne premješta ga
	if (st.AreaID != current.AreaID || st.SectorID != current.SectorID) && !s.CanEdit(perms, st) {
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
