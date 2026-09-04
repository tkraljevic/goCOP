package service

import (
	"context"
	"fmt"
	"strings"

	"gocop/internal/hydro"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// WatercourseService daje pristup registru vodnih tijela.
//
// Registar je preslika službenog izvora (Odluka o popisu voda I. reda), pa se
// ovdje samo čita — mijenja se osvježavanjem izvora, ne kroz sučelje.
type WatercourseService struct {
	repo *repository.WatercourseRepository
}

func NewWatercourseService(repo *repository.WatercourseRepository) *WatercourseService {
	return &WatercourseService{repo: repo}
}

func (s *WatercourseService) ListWatercourses(ctx context.Context, search, category string, onlyUsed bool) ([]models.Watercourse, error) {
	return s.repo.ListWatercourses(ctx, search, category, onlyUsed)
}

func (s *WatercourseService) GetWatercourse(ctx context.Context, code string) (*models.Watercourse, error) {
	return s.repo.GetWatercourse(ctx, code)
}

func (s *WatercourseService) ListCategories(ctx context.Context) ([]string, error) {
	return s.repo.ListCategories(ctx)
}

func (s *WatercourseService) Counts(ctx context.Context) (total, used, unlinkedSections, unlinkedStations int, err error) {
	return s.repo.CountWatercourses(ctx)
}

// Registar vodnih tijela preslika je službenog izvora, ali se smije dopunjavati
// i ispravljati — Odluka o popisu voda I. reda ne navodi male potoke i kanale
// koji se ipak štite, a poklapanja po nazivu ponegdje treba potvrditi ručno.

// CreateWatercourse upisuje novo vodno tijelo.
// Registar je zajednički svim dionicama, pa ga mijenja globalni administrator.
func (s *WatercourseService) CreateWatercourse(ctx context.Context, perms *models.UserPermissions, w *models.Watercourse) error {
	if err := requireGlobalAdmin(perms, "unos vodnog tijela"); err != nil {
		return err
	}
	if err := validateWatercourse(w); err != nil {
		return err
	}

	if strings.TrimSpace(w.Code) == "" {
		w.Code = hydro.WatercourseCode(w.OfficialName)
	}
	if w.Origin == "" {
		w.Origin = models.WatercourseOriginManual
	}

	existing, err := s.repo.GetWatercourse(ctx, w.Code)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("vodno tijelo sa šifrom %q već postoji (%s)", w.Code, existing.OfficialName)
	}

	return s.repo.CreateWatercourse(ctx, w)
}

// UpdateWatercourse mijenja podatke vodnog tijela
func (s *WatercourseService) UpdateWatercourse(ctx context.Context, perms *models.UserPermissions, w *models.Watercourse) error {
	if err := requireGlobalAdmin(perms, "izmjenu vodnog tijela"); err != nil {
		return err
	}
	if err := validateWatercourse(w); err != nil {
		return err
	}
	return s.repo.UpdateWatercourse(ctx, w)
}

// DeleteWatercourse briše vodno tijelo, ali ne dok je u upotrebi —
// brisanje bi dionice i postaje ostavilo da pokazuju u prazno
func (s *WatercourseService) DeleteWatercourse(ctx context.Context, perms *models.UserPermissions, code string) error {
	if err := requireGlobalAdmin(perms, "brisanje vodnog tijela"); err != nil {
		return err
	}

	sections, stations, err := s.repo.CountUsage(ctx, code)
	if err != nil {
		return err
	}
	if sections+stations > 0 {
		return fmt.Errorf("vodno tijelo se ne može obrisati jer je pridruženo: %d dionica, %d postaja — prvo ih prevežite na drugu vodu",
			sections, stations)
	}

	return s.repo.DeleteWatercourse(ctx, code)
}

// SetSectionWatercourse pridružuje vodno tijelo dionici.
// Smije ga postaviti onaj tko smije uređivati dionicu.
func (s *WatercourseService) SetSectionWatercourse(ctx context.Context, perms *models.UserPermissions, sectionCode, watercourseCode string, sectionService *SectionService) error {
	sec, err := sectionService.GetSectionWithDetails(sectionCode)
	if err != nil {
		return err
	}
	if !sectionService.CanEditSection(perms, sec) {
		return fmt.Errorf("nemate ovlasti za izmjenu vodotoka dionice %s", sectionCode)
	}

	if watercourseCode != "" {
		water, err := s.repo.GetWatercourse(ctx, watercourseCode)
		if err != nil {
			return err
		}
		if water == nil {
			return fmt.Errorf("vodno tijelo %q ne postoji u registru", watercourseCode)
		}
	}

	return s.repo.SetSectionWatercourse(ctx, sectionCode, watercourseCode)
}

// SetStationWatercourse pridružuje vodno tijelo postaji.
//
// Ručno pridruživanje nadjačava strojno utvrđivanje i tako se i bilježi —
// operater zna ono što dokumentacija ne kaže.
func (s *WatercourseService) SetStationWatercourse(ctx context.Context, perms *models.UserPermissions, stationID, watercourseCode string) error {
	if perms == nil {
		return fmt.Errorf("nemate ovlasti za izmjenu vodotoka postaje")
	}

	name, source := "", ""
	if watercourseCode != "" {
		water, err := s.repo.GetWatercourse(ctx, watercourseCode)
		if err != nil {
			return err
		}
		if water == nil {
			return fmt.Errorf("vodno tijelo %q ne postoji u registru", watercourseCode)
		}
		name, source = water.Name, models.WatercourseFromOperator
	}

	return s.repo.SetStationWatercourse(ctx, stationID, watercourseCode, name, source)
}

func (s *WatercourseService) SectionsForWatercourse(ctx context.Context, code string) ([]string, error) {
	return s.repo.ListSectionsForWatercourse(ctx, code)
}

func requireGlobalAdmin(perms *models.UserPermissions, action string) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("%s dopušteno je samo globalnom administratoru", action)
	}
	return nil
}

func validateWatercourse(w *models.Watercourse) error {
	if w == nil {
		return fmt.Errorf("podaci o vodnom tijelu nedostaju")
	}
	if strings.TrimSpace(w.OfficialName) == "" {
		return fmt.Errorf("naziv vodnog tijela je obavezan")
	}
	if strings.TrimSpace(w.Name) == "" {
		w.Name = w.OfficialName
	}
	for _, m := range []struct {
		label string
		value *float64
	}{
		{"duljina", w.LengthKm},
		{"površina porječja", w.BasinKm2},
		{"prosječni protok", w.AvgFlowM3S},
	} {
		if m.value != nil && *m.value < 0 {
			return fmt.Errorf("%s ne može biti negativna", m.label)
		}
	}
	return nil
}
