package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
)

// ReadingService: očitanja vodostaja na letvama postaja i objekata.
// Tko smije upisati: za objekt onaj tko smije pisati u njegov sektor ili
// područje; za postaju onaj tko smije uređivati bar jednu dionicu kojoj je
// mjerodavna. Postaja bez dionica (npr. mađarske uzvodne) prima očitanja od
// svakoga tko ima bilo koje pravo pisanja — nema koga drugoga da ih upiše.
type ReadingService struct {
	repo           *repository.ReadingRepository
	stationRepo    *repository.StationRepository
	structureRepo  *repository.StructureRepository
	sectionService *SectionService
}

func NewReadingService(repo *repository.ReadingRepository, stations *repository.StationRepository,
	structures *repository.StructureRepository, sections *SectionService) *ReadingService {
	return &ReadingService{repo: repo, stationRepo: stations, structureRepo: structures, sectionService: sections}
}

func (s *ReadingService) Get(ctx context.Context, id uuid.UUID) (*models.Reading, error) {
	return s.repo.Get(ctx, id)
}

func (s *ReadingService) List(ctx context.Context, f repository.ReadingFilter) ([]models.Reading, error) {
	return s.repo.List(ctx, f)
}

func (s *ReadingService) Stats(ctx context.Context) (int, time.Time, time.Time, error) {
	return s.repo.Stats(ctx)
}

func hasAnyWriteRight(perms *models.UserPermissions) bool {
	if perms == nil {
		return false
	}
	return perms.IsGlobalAdmin || len(perms.AdminSectors) > 0 || len(perms.AdminAreas) > 0 ||
		len(perms.AllowedSectors) > 0 || len(perms.AllowedAreas) > 0 || len(perms.AllowedSections) > 0
}

// CanRecordStation javlja smije li korisnik upisati očitanje na postaju
func (s *ReadingService) CanRecordStation(perms *models.UserPermissions, st *models.Station) bool {
	if perms == nil || st == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	if len(st.SectionCodes) == 0 {
		return hasAnyWriteRight(perms)
	}
	for _, code := range st.SectionCodes {
		if perms.AllowedSections[code] {
			return true
		}
		sec, err := s.sectionService.GetSectionWithDetails(code)
		if err == nil && sec != nil && s.sectionService.CanEditSection(perms, sec) {
			return true
		}
	}
	return false
}

// CanRecordStructure javlja smije li korisnik upisati očitanje na objekt
func (s *ReadingService) CanRecordStructure(perms *models.UserPermissions, st *models.Structure) bool {
	if perms == nil || st == nil {
		return false
	}
	return perms.IsGlobalAdmin || perms.AdminSectors[st.SectorID] || perms.AdminAreas[st.AreaID] ||
		perms.AllowedSectors[st.SectorID] || perms.AllowedAreas[st.AreaID]
}

// CanEdit javlja smije li korisnik mijenjati ili brisati postojeće očitanje:
// tko ga je upisao, ili tko smije upisivati na tu letvu
func (s *ReadingService) CanEdit(ctx context.Context, perms *models.UserPermissions, rd *models.Reading) bool {
	if perms == nil || rd == nil {
		return false
	}
	if perms.IsGlobalAdmin || (rd.UserID != "" && rd.UserID == perms.User.ID.String()) {
		return true
	}
	if rd.StructureID != "" {
		id, err := uuid.Parse(rd.StructureID)
		if err != nil {
			return false
		}
		st, err := s.structureRepo.GetStructure(ctx, id)
		return err == nil && s.CanRecordStructure(perms, st)
	}
	id, err := uuid.Parse(rd.StationID)
	if err != nil {
		return false
	}
	st, err := s.stationRepo.GetStationByID(ctx, id)
	return err == nil && s.CanRecordStation(perms, st)
}

func (s *ReadingService) validate(rd *models.Reading) error {
	if (rd.StationID == "") == (rd.StructureID == "") {
		return fmt.Errorf("očitanje mora pripadati ili postaji ili objektu")
	}
	if rd.MeasuredAt.IsZero() {
		return fmt.Errorf("vrijeme očitanja je obavezno")
	}
	if rd.MeasuredAt.After(time.Now().Add(time.Hour)) {
		return fmt.Errorf("vrijeme očitanja ne može biti u budućnosti")
	}
	if rd.MeasuredAt.Year() < 1900 {
		return fmt.Errorf("vrijeme očitanja nije vjerojatno")
	}
	if !rd.HasLevel() && strings.TrimSpace(rd.Note) == "" && rd.StructureState == "" && rd.Gate == "" {
		return fmt.Errorf("upišite vodostaj ili bar napomenu zašto nije očitan")
	}
	for _, v := range []*int{rd.LevelCm, rd.Level2Cm} {
		if v != nil && (*v < -500 || *v > 3000) {
			return fmt.Errorf("vodostaj %d cm je izvan razumnog raspona", *v)
		}
	}
	switch rd.Source {
	case models.ReadingSourceManual, models.ReadingSourceAutomatic, models.ReadingSourceImport:
	case "":
		rd.Source = models.ReadingSourceManual
	default:
		return fmt.Errorf("nepoznat način očitanja")
	}
	if rd.StructureState != "" && models.StructureStateLabel(rd.StructureState) == "" {
		return fmt.Errorf("nepoznato stanje crpne stanice")
	}
	if rd.Gate != "" && models.GateLabel(rd.Gate) == "" {
		return fmt.Errorf("nepoznat položaj zapornice")
	}
	rd.Note = strings.TrimSpace(rd.Note)
	rd.Observer = strings.TrimSpace(rd.Observer)
	return nil
}

// Create upisuje ručno očitanje u ime prijavljenog korisnika
func (s *ReadingService) Create(ctx context.Context, perms *models.UserPermissions, rd *models.Reading) error {
	if err := s.validate(rd); err != nil {
		return err
	}
	if rd.StructureID != "" {
		id, err := uuid.Parse(rd.StructureID)
		if err != nil {
			return fmt.Errorf("neispravan objekt")
		}
		st, err := s.structureRepo.GetStructure(ctx, id)
		if err != nil || st == nil {
			return fmt.Errorf("objekt ne postoji")
		}
		if !s.CanRecordStructure(perms, st) {
			return fmt.Errorf("nemate pravo upisivati očitanja na %s", st.Name)
		}
	} else {
		id, err := uuid.Parse(rd.StationID)
		if err != nil {
			return fmt.Errorf("neispravna postaja")
		}
		st, err := s.stationRepo.GetStationByID(ctx, id)
		if err != nil || st == nil {
			return fmt.Errorf("postaja ne postoji")
		}
		if !s.CanRecordStation(perms, st) {
			return fmt.Errorf("nemate pravo upisivati očitanja na %s", st.Name)
		}
	}
	if rd.Origin == "" {
		rd.Origin = models.ReadingOriginGoCOP
	}
	if perms != nil {
		rd.UserID = perms.User.ID.String()
		if rd.Observer == "" {
			rd.Observer = perms.User.FullName
		}
	}
	return s.repo.Create(ctx, rd)
}

// Update mijenja postojeće očitanje; letva se ne mijenja
func (s *ReadingService) Update(ctx context.Context, perms *models.UserPermissions, rd *models.Reading) error {
	existing, err := s.repo.Get(ctx, rd.ID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("očitanje ne postoji")
	}
	if !s.CanEdit(ctx, perms, existing) {
		return fmt.Errorf("nemate pravo mijenjati ovo očitanje")
	}
	rd.StationID, rd.StructureID = existing.StationID, existing.StructureID
	rd.Origin, rd.SourceRef, rd.UserID, rd.CreatedAt = existing.Origin, existing.SourceRef, existing.UserID, existing.CreatedAt
	if err := s.validate(rd); err != nil {
		return err
	}
	return s.repo.Update(ctx, rd)
}

func (s *ReadingService) Delete(ctx context.Context, perms *models.UserPermissions, id uuid.UUID) (*models.Reading, error) {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("očitanje ne postoji")
	}
	if !s.CanEdit(ctx, perms, existing) {
		return nil, fmt.Errorf("nemate pravo brisati ovo očitanje")
	}
	return existing, s.repo.Delete(ctx, id)
}

// Overview slaže pregled svih letvi sa zadnjim očitanjem, promjenom i fazom.
// Letve bez ijednog očitanja dolaze s Count 0 — pozivatelj bira hoće li ih pokazati.
func (s *ReadingService) Overview(ctx context.Context) ([]models.GaugeSummary, error) {
	latest, err := s.repo.LatestPerGauge(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountPerGauge(ctx)
	if err != nil {
		return nil, err
	}
	stations, err := s.stationRepo.ListStations(ctx, "", "", false)
	if err != nil {
		return nil, err
	}
	structures, err := s.structureRepo.ListStructures(ctx, "", 0, "", "")
	if err != nil {
		return nil, err
	}
	var out []models.GaugeSummary
	for i := range stations {
		st := stations[i]
		g := models.GaugeSummary{
			Key: "station:" + st.ID.String(), Name: st.Name, URL: "/readings/station/" + st.ID.String(),
			NewURL: "/readings/new?station=" + st.ID.String(), StationID: st.ID.String(), Kind: "POSTAJA",
		}
		g.Sub = strings.TrimSpace(strings.Trim(st.Watercourse+" · "+st.Stationing, " ·"))
		fill(&g, latest, counts)
		if g.Latest != nil && g.Latest.LevelCm != nil {
			g.Phase = st.CalculateDefensePhase(*g.Latest.LevelCm)
		} else {
			g.Phase = models.PhaseUnknown
		}
		out = append(out, g)
	}
	stationByID := map[string]models.Station{}
	for _, st := range stations {
		stationByID[st.ID.String()] = st
	}
	for i := range structures {
		st := structures[i]
		g := models.GaugeSummary{
			Key: "structure:" + st.ID.String(), Name: st.Name, Sub: st.KindLabel(),
			URL: "/readings/structure/" + st.ID.String(), NewURL: "/readings/new?structure=" + st.ID.String(),
			StructureID: st.ID.String(), SectorID: st.SectorID, AreaID: st.AreaID, Kind: st.Kind,
		}
		if st.AreaName != "" {
			g.Sub += " · BP " + fmt.Sprint(st.AreaID)
		}
		fill(&g, latest, counts)
		g.Phase = models.PhaseUnknown
		if gs, ok := stationByID[st.StationID]; ok && g.Latest != nil && g.Latest.LevelCm != nil {
			g.Phase = gs.CalculateDefensePhase(*g.Latest.LevelCm)
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Latest == nil) != (b.Latest == nil) {
			return a.Latest != nil
		}
		if a.Phase.Severity() != b.Phase.Severity() {
			return a.Phase.Severity() > b.Phase.Severity()
		}
		if a.Latest != nil && b.Latest != nil && !a.Latest.MeasuredAt.Equal(b.Latest.MeasuredAt) {
			return a.Latest.MeasuredAt.After(b.Latest.MeasuredAt)
		}
		return a.Name < b.Name
	})
	return out, nil
}

func fill(g *models.GaugeSummary, latest map[string][]models.Reading, counts map[string]int) {
	g.Count = counts[g.Key]
	if rs := latest[g.Key]; len(rs) > 0 {
		r := rs[0]
		g.Latest = &r
		if len(rs) > 1 {
			p := rs[1]
			g.Previous = &p
		}
	}
}

// PhaseFor računa fazu obrane za očitanje: postaja iz svojih pragova, objekt
// iz pragova vodomjera koji mu je pridružen
func (s *ReadingService) PhaseFor(station *models.Station, level *int) models.DefensePhase {
	if station == nil || level == nil {
		return models.PhaseUnknown
	}
	return station.CalculateDefensePhase(*level)
}
