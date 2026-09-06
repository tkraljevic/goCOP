package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/hydro"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// StationService upravlja registrom vodomjernih postaja i njihovom vezom s dionicama.
//
// Postaje su zajedničke svim dionicama — isti vodomjer mjerodavan je za više
// dionica — pa se s dionice postaja dodaje i uklanja, a ne briše iz registra.
type StationService struct {
	stationRepo    *repository.StationRepository
	sectionService *SectionService
	sseBroker      *SSEBroker
}

func NewStationService(
	stationRepo *repository.StationRepository,
	sectionService *SectionService,
	sseBroker *SSEBroker,
) *StationService {
	return &StationService{
		stationRepo:    stationRepo,
		sectionService: sectionService,
		sseBroker:      sseBroker,
	}
}

func (s *StationService) ListStations(ctx context.Context, search, watercourse string, onlyNeedsReview bool) ([]models.Station, error) {
	return s.stationRepo.ListStations(ctx, search, watercourse, onlyNeedsReview)
}

func (s *StationService) GetStation(ctx context.Context, id uuid.UUID) (*models.Station, error) {
	return s.stationRepo.GetStationByID(ctx, id)
}

func (s *StationService) ListWatercourses(ctx context.Context) ([]string, error) {
	return s.stationRepo.ListWatercourses(ctx)
}

func (s *StationService) Counts(ctx context.Context) (total, needsReview, links int, err error) {
	return s.stationRepo.CountStations(ctx)
}

// GetSectionStations vraća mjerodavne vodomjere dionice
func (s *StationService) GetSectionStations(ctx context.Context, sectionCode string) ([]models.Station, error) {
	return s.stationRepo.GetStationsForSection(ctx, sectionCode)
}

// GetSectionGaugeCriteria vraća retke iz dokumentacije dionice koji nisu
// vodomjerne postaje nego kriteriji obrane — upute na pravilnik akumulacije,
// kote brana i mjerna mjesta bez usporedivih pragova.
//
// Popis mjerodavnih vodomjera po dionicama je službeni podatak (Privitak 1,
// Hrvatske vode), pa se nijedan redak ne smije izgubiti s ekrana samo zato što
// nije mogao ući u registar postaja.
func (s *StationService) GetSectionGaugeCriteria(ctx context.Context, sectionCode string) ([]models.GaugeItem, error) {
	section, err := s.sectionService.GetSectionWithDetails(sectionCode)
	if err != nil {
		return nil, err
	}
	if section == nil {
		return nil, nil
	}

	stations, err := s.stationRepo.GetStationsForSection(ctx, sectionCode)
	if err != nil {
		return nil, err
	}

	var criteria []models.GaugeItem
	for _, p := range section.Parts {
		for _, gauge := range p.Gauges {
			if !coveredByStation(gauge.StationName, stations) {
				criteria = append(criteria, gauge)
			}
		}
	}

	return criteria, nil
}

// coveredByStation govori je li redak dokumentacije već prikazan kao postaja.
// Usporedba ide istim ključem kojim seed prepoznaje isti vodomjer zapisan na
// više načina — ne izmišlja se druga normalizacija.
func coveredByStation(gaugeName string, stations []models.Station) bool {
	rowName, _ := hydro.ParseStationName(gaugeName)
	rowKey := hydro.StationKey(rowName)
	if rowKey == "" {
		rowKey = hydro.StationKey(gaugeName)
	}

	for _, st := range stations {
		if hydro.StationKey(st.Name) == rowKey {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(st.SourceName), strings.TrimSpace(gaugeName)) {
			return true
		}
	}

	return false
}

// canEditSection provjerava smije li korisnik mijenjati sadržaj dionice
func (s *StationService) canEditSection(perms *models.UserPermissions, sectionCode string) error {
	sec, err := s.sectionService.GetSectionWithDetails(sectionCode)
	if err != nil {
		return err
	}
	if !s.sectionService.CanEditSection(perms, sec) {
		return fmt.Errorf("nemate ovlasti za izmjenu mjerodavnih vodomjera na dionici %s", sectionCode)
	}
	return nil
}

// CreateStation upisuje novu vodomjernu postaju u registar i, kad je zadana
// dionica, odmah je proglašava njezinim mjerodavnim vodomjerom
func (s *StationService) CreateStation(ctx context.Context, perms *models.UserPermissions, station *models.Station, sectionCode string) error {
	if err := s.validate(station); err != nil {
		return err
	}
	if sectionCode != "" {
		if err := s.canEditSection(perms, sectionCode); err != nil {
			return err
		}
	} else if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("postaju izvan dionice može unijeti samo globalni administrator")
	}

	if strings.TrimSpace(station.Code) == "" {
		station.Code = hydro.Slug(station.Name)
	}
	station.NeedsReview = !station.HasUsableThresholds()
	if station.NeedsReview && station.ReviewNote == "" {
		station.ReviewNote = "nijedan prag nije zapisan u centimetrima — faza obrane se ne računa automatski"
	}

	if err := s.stationRepo.CreateStation(ctx, station); err != nil {
		return err
	}

	// Mjerodavnost za dionicu upisuje se u poddionici na obrascu dionice
	return nil
}

// UpdateStation mijenja podatke postaje u registru.
// Izmjena vrijedi za sve dionice kojima je postaja mjerodavna, pa je smije
// napraviti onaj tko smije uređivati barem jednu od njih.
func (s *StationService) UpdateStation(ctx context.Context, perms *models.UserPermissions, station *models.Station) error {
	if err := s.validate(station); err != nil {
		return err
	}

	existing, err := s.stationRepo.GetStationByID(ctx, station.ID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("vodomjerna postaja nije pronađena")
	}
	if err := s.requireAnySectionAccess(perms, existing.SectionCodes); err != nil {
		return err
	}

	// Obrazac ne šalje sva polja — ono što ne uređuje mora preživjeti izmjenu
	station.SourceName = existing.SourceName
	station.ZeroDatumSource = existing.ZeroDatumSource
	station.ZeroDatumMethod = existing.ZeroDatumMethod
	station.ZeroDatumSurveyDate = existing.ZeroDatumSurveyDate
	station.ZeroDatumDocumentDate = existing.ZeroDatumDocumentDate
	if strings.TrimSpace(station.Code) == "" {
		station.Code = existing.Code
	}
	if strings.TrimSpace(station.WaterArea) == "" {
		station.WaterArea = existing.WaterArea
	}

	// Naziv vodotoka mijenja i podrijetlo podatka: ručni unos je potvrda
	// operatera i raskida vezu na registar dok se ne uspostavi iznova
	if strings.EqualFold(strings.TrimSpace(station.Watercourse), strings.TrimSpace(existing.Watercourse)) {
		station.WatercourseSource = existing.WatercourseSource
	} else if strings.TrimSpace(station.Watercourse) == "" {
		station.WatercourseSource = models.WatercourseUndetermined
	} else {
		station.WatercourseSource = models.WatercourseFromOperator
	}

	station.NeedsReview = !station.HasUsableThresholds()
	if !station.NeedsReview {
		station.ReviewNote = ""
	} else if station.ReviewNote == "" {
		station.ReviewNote = "nijedan prag nije zapisan u centimetrima — faza obrane se ne računa automatski"
	}

	if err := s.stationRepo.UpdateStation(ctx, station); err != nil {
		return err
	}

	if s.sseBroker != nil {
		s.sseBroker.Broadcast("stations",
			fmt.Sprintf("Izmijenjena vodomjerna postaja %s", station.Name),
			map[string]any{"station_id": station.ID.String()})
	}
	return nil
}

// DeleteStation briše postaju iz registra. Postaja mjerodavna za neku dionicu
// briše se samo odlukom globalnog administratora, jer nestaje sa svih dionica.
func (s *StationService) DeleteStation(ctx context.Context, perms *models.UserPermissions, id uuid.UUID) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("vodomjernu postaju iz registra može obrisati samo globalni administrator")
	}
	return s.stationRepo.DeleteStation(ctx, id)
}

// requireAnySectionAccess traži ovlast nad barem jednom dionicom postaje
func (s *StationService) requireAnySectionAccess(perms *models.UserPermissions, sectionCodes []string) error {
	if perms != nil && perms.IsGlobalAdmin {
		return nil
	}
	for _, code := range sectionCodes {
		if err := s.canEditSection(perms, code); err == nil {
			return nil
		}
	}
	return fmt.Errorf("nemate ovlasti za izmjenu ove vodomjerne postaje")
}

func (s *StationService) validate(station *models.Station) error {
	if station == nil {
		return fmt.Errorf("podaci o postaji nedostaju")
	}
	if strings.TrimSpace(station.Name) == "" {
		return fmt.Errorf("naziv vodomjerne postaje je obavezan")
	}

	// Pragovi moraju rasti od pripremnog stanja prema izvanrednom stanju —
	// obrnut redoslijed znači da bi postaja prijavila blažu fazu nego što jest.
	ordered := []struct {
		label string
		value *int
	}{
		{"pripremno stanje", station.Prep.Cm},
		{"redovna obrana", station.Regular.Cm},
		{"izvanredna obrana", station.Emergency.Cm},
		{"izvanredno stanje", station.State.Cm},
	}

	var prevLabel string
	var prevValue *int
	for _, t := range ordered {
		if t.value == nil {
			continue
		}
		if prevValue != nil && *t.value <= *prevValue {
			return fmt.Errorf("prag za %s (%d cm) mora biti viši od praga za %s (%d cm)",
				t.label, *t.value, prevLabel, *prevValue)
		}
		prevLabel, prevValue = t.label, t.value
	}

	return nil
}
