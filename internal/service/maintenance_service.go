package service

import (
	"context"
	"errors"
	"strings"

	"gocop/internal/importer/ugovor"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// MaintenanceService: popis održavanih voda i stavke radova po branjenom
// području. Piše tko smije pisati u to područje.
type MaintenanceService struct {
	repo       *repository.MaintenanceRepository
	waters     *repository.WatercourseRepository
	structures *repository.StructureRepository
}

func NewMaintenanceService(repo *repository.MaintenanceRepository, waters *repository.WatercourseRepository, structures *repository.StructureRepository) *MaintenanceService {
	return &MaintenanceService{repo: repo, waters: waters, structures: structures}
}

// CanEdit: globalni administrator, ili pravo pisanja u području ili njegovu sektoru
func (s *MaintenanceService) CanEdit(perms *models.UserPermissions, area models.Area) bool {
	if perms == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	return perms.AdminSectors[area.SectorID] || perms.AdminAreas[area.ID] ||
		perms.AllowedSectors[area.SectorID] || perms.AllowedAreas[area.ID]
}

func (s *MaintenanceService) ListWaters(ctx context.Context, areaID int) ([]models.MaintainedWater, error) {
	return s.repo.ListWaters(ctx, areaID)
}

func (s *MaintenanceService) WatersFor(ctx context.Context, watercourseCode, structureID string) ([]models.MaintainedWater, error) {
	return s.repo.WatersFor(ctx, watercourseCode, structureID)
}

// LinkWater veže lokaciju iz popisa na vodu (šifra) ili nasip (identitet objekta)
func (s *MaintenanceService) LinkWater(ctx context.Context, perms *models.UserPermissions, area models.Area, id, target string) error {
	if !s.CanEdit(perms, area) {
		return errors.New("nemate pravo uređivati popis lokacija tog područja")
	}
	m, err := s.repo.GetWater(ctx, id)
	if err != nil {
		return err
	}
	if m == nil || m.AreaID != area.ID {
		return errors.New("lokacija nije pronađena")
	}
	target = strings.TrimSpace(target)
	m.WatercourseCode, m.StructureID = "", ""
	if target != "" {
		if w, err := s.waters.GetWatercourse(ctx, target); err != nil {
			return err
		} else if w != nil {
			m.WatercourseCode = w.Code
		} else if st, err := s.structures.GetByCode(ctx, target); err != nil {
			return err
		} else if st != nil {
			m.StructureID = st.ID.String()
		} else {
			return errors.New("nema vode ni objekta s tom šifrom")
		}
	}
	return s.repo.UpsertWater(ctx, m)
}

// AddWater ručno dodaje lokaciju u popis: naziv s kategorijom, po želji
// odmah vezanu na vodu ili nasip iz registra
func (s *MaintenanceService) AddWater(ctx context.Context, perms *models.UserPermissions, area models.Area, m *models.MaintainedWater, target string) error {
	if !s.CanEdit(perms, area) {
		return errors.New("nemate pravo uređivati popis lokacija tog područja")
	}
	m.Name = strings.Join(strings.Fields(m.Name), " ")
	if m.Name == "" {
		return errors.New("naziv lokacije je obvezan")
	}
	if m.Order != models.WaterOrderFirst && m.Order != models.WaterOrderSecond && m.Order != models.WaterOrderThird && m.Order != models.WaterOrderFourth {
		return errors.New("odaberite red vode")
	}
	if m.Order != models.WaterOrderFirst {
		m.Group = ""
	} else if m.Group != models.WaterGroupInterstate && m.Group != models.WaterGroupOtherState {
		return errors.New("za vode I. reda odaberite skupinu")
	}
	if m.Program == "" {
		m.Program = models.ProgramA02
		if m.Order == models.WaterOrderThird || m.Order == models.WaterOrderFourth {
			m.Program = models.ProgramA03
		}
	}
	if m.Program != models.ProgramA02 && m.Program != models.ProgramA03 {
		return errors.New("program je A.02 ili A.03")
	}
	if models.MaintenanceKindLabel(m.Kind) == m.Kind {
		return errors.New("odaberite vrstu lokacije")
	}
	m.AreaID = area.ID
	m.ID = repository.MaintainedWaterID(area.ID, m.Name)
	if ex, err := s.repo.GetWater(ctx, m.ID); err != nil {
		return err
	} else if ex != nil {
		return errors.New("lokacija s tim nazivom već je u popisu")
	}
	if m.Source == "" {
		m.Source = "ručni unos"
	}
	if target = strings.TrimSpace(target); target != "" {
		if w, err := s.waters.GetWatercourse(ctx, target); err != nil {
			return err
		} else if w != nil {
			m.WatercourseCode = w.Code
		} else if st, err := s.structures.GetByCode(ctx, target); err != nil {
			return err
		} else if st != nil {
			m.StructureID = st.ID.String()
		} else {
			return errors.New("nema vode ni objekta s tom šifrom")
		}
	}
	return s.repo.UpsertWater(ctx, m)
}

// ImportContract čita ugovor o održavanju iz datoteke: bez write samo
// izvješće, s write upisuje popis lokacija i stavke radova. Datoteka mora
// biti ugovor tog područja.
func (s *MaintenanceService) ImportContract(ctx context.Context, perms *models.UserPermissions, area models.Area, areas []models.Area, path string, aliases map[string]string, write bool) (ugovor.Report, error) {
	if !s.CanEdit(perms, area) {
		return ugovor.Report{}, errors.New("nemate pravo uređivati popis lokacija tog područja")
	}
	rep, err := ugovor.Run(ctx, ugovor.Options{
		Path: path, DryRun: !write, Aliases: aliases,
		Deps: ugovor.Deps{Waters: s.waters, Structures: s.structures, Maintenance: s.repo, Areas: areas},
	})
	if err != nil {
		return rep, err
	}
	if rep.Area != area.ID {
		return rep, errors.New("datoteka je ugovor drugog branjenog područja (BP " + itoa(rep.Area) + ")")
	}
	return rep, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func (s *MaintenanceService) DeleteWater(ctx context.Context, perms *models.UserPermissions, area models.Area, id string) error {
	if !s.CanEdit(perms, area) {
		return errors.New("nemate pravo uređivati popis lokacija tog područja")
	}
	m, err := s.repo.GetWater(ctx, id)
	if err != nil {
		return err
	}
	if m == nil || m.AreaID != area.ID {
		return errors.New("lokacija nije pronađena")
	}
	return s.repo.DeleteWater(ctx, id)
}

func (s *MaintenanceService) ListItems(ctx context.Context, areaID int, all bool) ([]models.WorkItem, error) {
	return s.repo.ListItems(ctx, areaID, all)
}

// SaveItem upisuje novu ili mijenja postojeću stavku radova
func (s *MaintenanceService) SaveItem(ctx context.Context, perms *models.UserPermissions, area models.Area, w *models.WorkItem) error {
	if !s.CanEdit(perms, area) {
		return errors.New("nemate pravo uređivati stavke tog područja")
	}
	w.Description = strings.TrimSpace(w.Description)
	w.Unit = strings.TrimSpace(w.Unit)
	w.Number = strings.TrimSpace(w.Number)
	if w.Description == "" {
		return errors.New("opis stavke je obvezan")
	}
	if w.Unit == "" {
		return errors.New("jedinica mjere je obvezna")
	}
	w.AreaID = area.ID
	if w.ID != "" {
		cur, err := s.repo.GetItem(ctx, w.ID)
		if err != nil {
			return err
		}
		if cur == nil || cur.AreaID != area.ID {
			return errors.New("stavka nije pronađena")
		}
		w.Origin, w.Source, w.CreatedAt = cur.Origin, cur.Source, cur.CreatedAt
		if w.SortOrder == 0 {
			w.SortOrder = cur.SortOrder
		}
	} else {
		w.Origin = models.WorkItemOriginManual
		w.Active = true
		if w.SortOrder == 0 {
			items, err := s.repo.ListItems(ctx, area.ID, true)
			if err != nil {
				return err
			}
			for _, it := range items {
				if it.SortOrder >= w.SortOrder {
					w.SortOrder = it.SortOrder + 10
				}
			}
		}
	}
	return s.repo.SaveItem(ctx, w)
}

func (s *MaintenanceService) DeleteItem(ctx context.Context, perms *models.UserPermissions, area models.Area, id string) error {
	if !s.CanEdit(perms, area) {
		return errors.New("nemate pravo uređivati stavke tog područja")
	}
	cur, err := s.repo.GetItem(ctx, id)
	if err != nil {
		return err
	}
	if cur == nil || cur.AreaID != area.ID {
		return errors.New("stavka nije pronađena")
	}
	return s.repo.DeleteItem(ctx, id)
}
