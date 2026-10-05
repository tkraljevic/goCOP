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
	userService    *UserService
}

func NewReadingService(repo *repository.ReadingRepository, stations *repository.StationRepository,
	structures *repository.StructureRepository, sections *SectionService, users *UserService) *ReadingService {
	return &ReadingService{repo: repo, stationRepo: stations, structureRepo: structures, sectionService: sections, userService: users}
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
// PrvoOcitanje vraća vrijeme najstarijeg očitanja vodostaja letve.
func (s *ReadingService) PrvoOcitanje(ctx context.Context, stationID string) (time.Time, bool) {
	if s == nil || s.repo == nil {
		return time.Time{}, false
	}
	return s.repo.PrvoOcitanje(ctx, stationID)
}

// Krajnosti vraća najviše i najniže operativno očitanje letve.
func (s *ReadingService) Krajnosti(ctx context.Context, stationID string) []models.KrajnostIzNiza {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.Krajnosti(ctx, stationID)
}

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

// CanRecordStructure javlja smije li korisnik upisati očitanje na objekt:
// tko piše u sektoru ili području objekta, ili na dionici uz koju objekt
// stoji, kad je objekt iz područja te dionice. Objekt koji nije vezan ni na
// jednu dionicu pripada području, pa na njemu upisuje i tko u tom području
// ima dužnost na dionicama. Objekt drugog područja koji stoji i na dionici
// (CS Budžak područja 16 na B.34.1) vodi njegovo područje.
func (s *ReadingService) CanRecordStructure(perms *models.UserPermissions, st *models.Structure) bool {
	if perms == nil || st == nil {
		return false
	}
	if perms.IsGlobalAdmin || perms.AdminSectors[st.SectorID] || perms.AdminAreas[st.AreaID] ||
		perms.AllowedSectors[st.SectorID] || perms.AllowedAreas[st.AreaID] {
		return true
	}
	if !perms.RadiNaDionicamaU(st.AreaID) {
		return false
	}
	for _, code := range st.SectionCodes {
		if perms.AllowedSections[code] {
			return true
		}
	}
	return len(st.SectionCodes) == 0
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
	if !rd.HasAnyValue() && strings.TrimSpace(rd.Note) == "" && rd.StructureState == "" && rd.Gate == "" {
		return fmt.Errorf("upišite vodostaj, temperaturu ili protok, ili bar napomenu zašto nije očitano")
	}
	for _, v := range []*int{rd.LevelCm, rd.Level2Cm} {
		if v != nil && (*v < -500 || *v > 3000) {
			return fmt.Errorf("vodostaj %d cm je izvan razumnog raspona", *v)
		}
	}
	// uvjet je obrnut da i NaN (koji nije ni manji ni veći) padne
	if rd.TempC != nil && !(*rd.TempC >= -5 && *rd.TempC <= 45) {
		return fmt.Errorf("temperatura vode %.1f °C je izvan razumnog raspona", *rd.TempC)
	}
	if rd.FlowM3s != nil && !(*rd.FlowM3s >= 0 && *rd.FlowM3s <= 100000) {
		return fmt.Errorf("protok %.1f m³/s je izvan razumnog raspona", *rd.FlowM3s)
	}
	switch rd.Source {
	case models.ReadingSourceManual, models.ReadingSourceAutomatic, models.ReadingSourceImport:
	case "":
		rd.Source = models.ReadingSourceManual
	default:
		return fmt.Errorf("nepoznat način očitanja")
	}
	if err := provjeriStanjeObjekta(rd); err != nil {
		return err
	}
	rd.Note = strings.TrimSpace(rd.Note)
	rd.Observer = strings.TrimSpace(rd.Observer)
	return nil
}

// provjeriStanjeObjekta: stanje crpne stanice i položaj zapornice postoje
// samo na objektu, a ne na letvi postaje
func provjeriStanjeObjekta(rd *models.Reading) error {
	if rd.StructureID == "" && (rd.StructureState != "" || rd.Gate != "") {
		return fmt.Errorf("stanje objekta i položaj zapornice upisuju se samo na objektu")
	}
	if rd.StructureState != "" && models.StructureStateLabel(rd.StructureState) == "" {
		return fmt.Errorf("nepoznato stanje crpne stanice")
	}
	if rd.Gate != "" && models.GateLabel(rd.Gate) == "" {
		return fmt.Errorf("nepoznat položaj zapornice")
	}
	return nil
}

// pravoNaObjekt javlja grešku ako osoba ne smije upisivati na objekt
func (s *ReadingService) pravoNaObjekt(ctx context.Context, perms *models.UserPermissions, structureID string) error {
	id, err := uuid.Parse(structureID)
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
	return nil
}

// pravoNaPostaju javlja grešku ako osoba ne smije upisivati na letvu postaje
func (s *ReadingService) pravoNaPostaju(ctx context.Context, perms *models.UserPermissions, stationID string) error {
	id, err := uuid.Parse(stationID)
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
	return nil
}

// Create upisuje ručno očitanje u ime prijavljenog korisnika. Pravo se
// provjerava prije unosa, kao kod izmjene: tko nema pravo, ne dozna pravila
// unosa.
func (s *ReadingService) Create(ctx context.Context, perms *models.UserPermissions, rd *models.Reading) error {
	var err error
	if rd.StructureID != "" {
		err = s.pravoNaObjekt(ctx, perms, rd.StructureID)
	} else {
		err = s.pravoNaPostaju(ctx, perms, rd.StationID)
	}
	if err != nil {
		return err
	}
	if err := s.validate(rd); err != nil {
		return err
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
	stations, err := s.stationRepo.ListStations(ctx, "", "", "", false)
	if err != nil {
		return nil, err
	}
	stationSectors, stationAreas, err := s.stationRepo.StationScopes(ctx)
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
			SectorIDs: stationSectors[st.ID.String()], AreaIDs: stationAreas[st.ID.String()],
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
		if !st.TakesReadings() {
			continue
		}
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

// FieldOverview je terenski pogled jedne osobe: letve koje obično očitava,
// ostale letve njezina područja i što je danas već obavljeno
type FieldOverview struct {
	Area   *models.Area
	Areas  []models.Area // područja koja osoba smije birati (više dužnosti ili administrator)
	Mine   []models.GaugeSummary
	Others []models.GaugeSummary
	Done   int
	Total  int
}

// FieldOverview slaže terenski pogled. Područje: zadano iz upita, inače
// prvo područje s dužnosti osobe. "Moje letve" su one koje je osoba
// očitavala u zadnjih 90 dana, poredane po uobičajenom vremenu obilaska.
func (s *ReadingService) FieldOverview(ctx context.Context, perms *models.UserPermissions, u *models.User, areaID int) (*FieldOverview, error) {
	fo := &FieldOverview{}
	allAreas, err := s.userService.ListAreas("")
	if err != nil {
		return nil, err
	}
	areaByID := map[int]models.Area{}
	for _, a := range allAreas {
		areaByID[a.ID] = a
	}
	if perms != nil && perms.IsGlobalAdmin {
		fo.Areas = allAreas
	} else if perms != nil {
		// Izbor područja: svoja područja s dužnosti (i dužnosti na
		// dionicama); tko vodi sektor, sva područja sektora
		for _, a := range allAreas {
			if perms.RadiUPodrucju(a.ID) || perms.AdminAreas[a.ID] || perms.AdminSectors[a.SectorID] {
				fo.Areas = append(fo.Areas, a)
			}
		}
	}
	if areaID == 0 && u != nil {
		if pd := u.PrimaryDuty(); pd != nil && pd.AreaID != nil {
			areaID = *pd.AreaID
		}
	}
	if areaID == 0 && len(fo.Areas) > 0 {
		areaID = fo.Areas[0].ID
	}
	if a, ok := areaByID[areaID]; ok {
		fo.Area = &a
	}

	all, err := s.Overview(ctx)
	if err != nil {
		return nil, err
	}
	// Letve područja: objekti područja i postaje mjerodavne za njegove dionice
	inArea := map[string]bool{}
	if areaID > 0 {
		secs, _ := s.sectionService.ListSections("", areaID, "")
		for _, sec := range secs {
			sts, _ := s.stationRepo.GetStationsForSection(ctx, sec.Code)
			for _, st := range sts {
				inArea["station:"+st.ID.String()] = true
			}
		}
	}
	var habits map[string]repository.GaugeHabit
	if u != nil {
		habits, _ = s.repo.HabitsFor(ctx, u.ID.String(), u.FullName, time.Now().AddDate(0, 0, -90))
	}
	today := time.Now().In(models.Zagreb).Format("2006-01-02")
	for _, g := range all {
		if g.StructureID != "" && g.AreaID == areaID {
			inArea[g.Key] = true
		}
		h, mine := habits[g.Key]
		if !mine && !inArea[g.Key] {
			continue
		}
		if g.Latest != nil && g.Latest.LocalTime().Format("2006-01-02") == today {
			g.DoneToday = true
		}
		if mine {
			g.Habit, g.UsualMin = h.Count, h.UsualMin
			fo.Mine = append(fo.Mine, g)
		} else {
			fo.Others = append(fo.Others, g)
		}
	}
	sort.SliceStable(fo.Mine, func(i, j int) bool {
		if fo.Mine[i].UsualMin != fo.Mine[j].UsualMin {
			return fo.Mine[i].UsualMin < fo.Mine[j].UsualMin
		}
		return fo.Mine[i].Name < fo.Mine[j].Name
	})
	sort.SliceStable(fo.Others, func(i, j int) bool {
		if (fo.Others[i].Count > 0) != (fo.Others[j].Count > 0) {
			return fo.Others[i].Count > 0
		}
		return fo.Others[i].Name < fo.Others[j].Name
	})
	for _, g := range fo.Mine {
		fo.Total++
		if g.DoneToday {
			fo.Done++
		}
	}
	return fo, nil
}

// UveziZalijepljena upisuje niz očitanja odjednom, kakva se zalijepe s letve
// ili donesu s terena. Identitet je izveden iz postaje i trenutka, pa ponovni
// uvoz istog ispisa ne udvostručuje zapise nego ih preskače.
//
// Ne prolazi kroz Create jer bi svako očitanje tražilo istu provjeru prava i
// isti dohvat postaje; provjera se radi jednom, za cijeli niz.
func (s *ReadingService) UveziZalijepljena(ctx context.Context, perms *models.UserPermissions,
	station *models.Station, ocitanja []models.Reading) (int, error) {
	if station == nil {
		return 0, fmt.Errorf("postaja nije zadana")
	}
	if !s.CanRecordStation(perms, station) {
		return 0, fmt.Errorf("nemate pravo upisivati očitanja na %s", station.Name)
	}
	for i := range ocitanja {
		if err := s.validate(&ocitanja[i]); err != nil {
			return 0, fmt.Errorf("%s: %w", ocitanja[i].MeasuredAt.Format("2.1.2006. 15:04"), err)
		}
	}
	return s.repo.ImportBatch(ctx, ocitanja)
}
