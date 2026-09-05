package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/weather"
)

// JournalService vodi građevinske dnevnike. Tko smije što:
//   - izvođač (voditelj usluga / poslovođa) piše rad i napomene i potvrđuje
//     list za izvođača;
//   - ovlaštenik za praćenje ugovora i rukovoditelji (dionice, područja,
//     sektora) pišu napomene, naloge i ocjene i potvrđuju list za nadzor;
//   - svi navedeni mogu pisati i rad, jer se u obrani upisuje i vlastiti rad.
type JournalService struct {
	repo     *repository.JournalRepository
	stations *repository.StationRepository
	readings *repository.ReadingRepository
	weather  *weather.Client
}

func NewJournalService(repo *repository.JournalRepository, stations *repository.StationRepository, readings *repository.ReadingRepository) *JournalService {
	return &JournalService{repo: repo, stations: stations, readings: readings, weather: &weather.Client{}}
}

// IsContractor govori je li osoba na strani izvođača
func IsContractor(u *models.User) bool {
	if u == nil {
		return false
	}
	for _, d := range u.Duties {
		if d.IsActive && d.Role == models.RoleServiceLeaderForeman {
			return true
		}
	}
	return false
}

// CanWrite: pravo pisanja u području dnevnika (izvođač ga ima kroz svoju dužnost)
func (s *JournalService) CanWrite(perms *models.UserPermissions, area models.Area) bool {
	if perms == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	return perms.AdminSectors[area.SectorID] || perms.AdminAreas[area.ID] ||
		perms.AllowedSectors[area.SectorID] || perms.AllowedAreas[area.ID]
}

// CanSupervise: nadzor piše nalog i ocjenu i potvrđuje list za nadzor — HV
// strana s pravom pisanja, ne izvođač
func (s *JournalService) CanSupervise(u *models.User, perms *models.UserPermissions, area models.Area) bool {
	return s.CanWrite(perms, area) && !IsContractor(u)
}

// CanManage: naslovnicu uređuje nadzor ili administrator područja
func (s *JournalService) CanManage(u *models.User, perms *models.UserPermissions, area models.Area) bool {
	return s.CanSupervise(u, perms, area)
}

// AllowedKinds vraća vrste upisa koje osoba smije pisati
func (s *JournalService) AllowedKinds(u *models.User, perms *models.UserPermissions, area models.Area) []string {
	if !s.CanWrite(perms, area) {
		return nil
	}
	if IsContractor(u) {
		return []string{models.EntryKindWork, models.EntryKindNote}
	}
	return models.EntryKinds
}

func (s *JournalService) ListJournals(ctx context.Context, areaID int) ([]models.Journal, error) {
	return s.repo.ListJournals(ctx, areaID)
}

func (s *JournalService) GetJournal(ctx context.Context, id string) (*models.Journal, error) {
	return s.repo.GetJournal(ctx, id)
}

// SaveJournal upisuje ili mijenja naslovnicu
func (s *JournalService) SaveJournal(ctx context.Context, u *models.User, perms *models.UserPermissions, area models.Area, j *models.Journal) error {
	if !s.CanManage(u, perms, area) {
		return errors.New("naslovnicu dnevnika uređuje ovlaštenik ili rukovoditelj područja")
	}
	if !models.IsJournalKind(j.Kind) {
		return errors.New("nepoznata vrsta dnevnika")
	}
	if j.IsDefense() && j.SectionCode == "" {
		return errors.New("dnevnik obrane vodi se po dionici: upišite šifru dionice")
	}
	if j.Year == 0 {
		j.Year = time.Now().In(models.Zagreb).Year()
	}
	j.AreaID = area.ID
	if j.Title == "" {
		j.Title = models.JournalKindLabel(j.Kind)
	}
	if j.Investor == "" {
		j.Investor = "Hrvatske vode, Ulica grada Vukovara 220, 10000 Zagreb"
	}
	if j.ID != "" {
		cur, err := s.repo.GetJournal(ctx, j.ID)
		if err != nil {
			return err
		}
		if cur == nil || cur.AreaID != area.ID {
			return errors.New("dnevnik nije pronađen")
		}
		j.CreatedAt, j.CreatedBy = cur.CreatedAt, cur.CreatedBy
	} else if u != nil {
		j.CreatedBy = u.ID.String()
	}
	return s.repo.SaveJournal(ctx, j)
}

func (s *JournalService) ListSheets(ctx context.Context, journalID string) ([]models.JournalSheet, error) {
	return s.repo.ListSheets(ctx, journalID)
}

func (s *JournalService) GetSheet(ctx context.Context, id string) (*models.JournalSheet, error) {
	return s.repo.GetSheet(ctx, id)
}

// NewSheet otvara novi list za dan: s vodostajima iz očitanja, osobljem i
// strojevima s prethodnog lista i, kad ima interneta, vremenskim prilikama.
// U danu može biti više listova — po ekipi, kao u tiskanom dnevniku.
func (s *JournalService) NewSheet(ctx context.Context, u *models.User, perms *models.UserPermissions, area models.Area, j *models.Journal, day time.Time, label string) (*models.JournalSheet, error) {
	if !s.CanWrite(perms, area) {
		return nil, errors.New("nemate pravo pisati u ovaj dnevnik")
	}
	sh := &models.JournalSheet{JournalID: j.ID, Date: day, Label: strings.TrimSpace(label)}
	if u != nil {
		sh.CreatedBy = u.ID.String()
	}
	// osoblje i strojevi se prepisuju s prethodnog lista: posada je iz dana u
	// dan uglavnom ista, a svaki izvođač ima svoje strojeve i alate
	if prev, err := s.repo.ListSheets(ctx, j.ID); err == nil && len(prev) > 0 {
		sh.Staff, sh.Machines = prev[0].Staff, prev[0].Machines
	}
	sh.WaterLevels = s.waterLevels(ctx, j, day)
	if j.Latitude != nil && j.Longitude != nil {
		s.fillWeather(ctx, sh, *j.Latitude, *j.Longitude)
	}
	if err := s.repo.SaveSheet(ctx, sh); err != nil {
		return nil, err
	}
	return sh, nil
}

// fillWeather puni prilike s Open-Meteo; bez interneta ostaje prazno, bez greške
func (s *JournalService) fillWeather(ctx context.Context, sh *models.JournalSheet, lat, lon float64) error {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d, err := s.weather.Fetch(wctx, lat, lon, sh.Date, 12)
	if err != nil {
		return err
	}
	t, wf, wt, p, pr := d.Temperature, d.WindFrom, d.WindTo, d.Pressure, d.Precipitation
	sh.Temperature, sh.WindFrom, sh.WindTo, sh.Pressure, sh.Precipitation = &t, &wf, &wt, &p, &pr
	sh.WeatherSource = d.Source
	if sh.Conditions == "" {
		sh.Conditions = d.Description
	}
	return nil
}

// RefreshWeather ponovno povlači prilike za list, na zahtjev
func (s *JournalService) RefreshWeather(ctx context.Context, perms *models.UserPermissions, area models.Area, j *models.Journal, sh *models.JournalSheet) error {
	if !s.CanWrite(perms, area) {
		return errors.New("nemate pravo pisati u ovaj dnevnik")
	}
	if j.Latitude == nil || j.Longitude == nil {
		return errors.New("na naslovnici dnevnika nema koordinata za vremenske prilike")
	}
	if err := s.fillWeather(ctx, sh, *j.Latitude, *j.Longitude); err != nil {
		return err
	}
	sh.WaterLevels = s.waterLevels(ctx, j, sh.Date)
	return s.repo.SaveSheet(ctx, sh)
}

// waterLevels slaže vodostaje postaja s naslovnice za dan lista: zadnje
// očitanje tog dana, a kad ga nema, zadnje prije njega, s datumom da se vidi
// da je starije
func (s *JournalService) waterLevels(ctx context.Context, j *models.Journal, day time.Time) string {
	var parts []string
	dayStart := time.Date(day.In(models.Zagreb).Year(), day.In(models.Zagreb).Month(), day.In(models.Zagreb).Day(), 0, 0, 0, 0, models.Zagreb)
	from := dayStart.Add(-400 * 24 * time.Hour)
	to := dayStart.Add(24 * time.Hour)
	for _, code := range j.GaugeCodes() {
		st, err := s.stations.GetStationByCode(ctx, code)
		if err != nil || st == nil {
			continue
		}
		rds, err := s.readings.ListForGauges(ctx, []string{st.ID.String()}, nil, from.UTC(), to.UTC())
		if err != nil || len(rds) == 0 {
			continue
		}
		var best *models.Reading
		for i := range rds {
			r := &rds[i]
			if r.LevelCm == nil || r.MeasuredAt.After(to.UTC()) {
				continue
			}
			if best == nil || r.MeasuredAt.After(best.MeasuredAt) {
				best = r
			}
		}
		if best == nil {
			continue
		}
		name := st.Name
		if st.Watercourse != "" {
			name = st.Watercourse + " - " + st.Name
		}
		when := ""
		if best.MeasuredAt.Before(dayStart.UTC()) {
			when = " (" + best.MeasuredAt.In(models.Zagreb).Format("02.01.") + ")"
		}
		parts = append(parts, fmt.Sprintf("%s: %d cm%s", name, *best.LevelCm, when))
	}
	return strings.Join(parts, ", ")
}

// UpdateSheet mijenja uvjete, osoblje i strojeve na listu; potvrde se ne diraju
func (s *JournalService) UpdateSheet(ctx context.Context, perms *models.UserPermissions, area models.Area, sh *models.JournalSheet) error {
	if !s.CanWrite(perms, area) {
		return errors.New("nemate pravo pisati u ovaj dnevnik")
	}
	cur, err := s.repo.GetSheet(ctx, sh.ID)
	if err != nil {
		return err
	}
	if cur == nil {
		return errors.New("list nije pronađen")
	}
	if cur.IsConfirmed() {
		return errors.New("list je potvrđen s obje strane i više se ne mijenja")
	}
	if sh.Rating == 1 && strings.TrimSpace(sh.RatingNote) == "" {
		return errors.New("kad su uvjeti nemogući, obrazloženje je obvezno")
	}
	cur.Label = sh.Label
	cur.Conditions, cur.Temperature, cur.WindFrom, cur.WindTo = sh.Conditions, sh.Temperature, sh.WindFrom, sh.WindTo
	cur.Pressure, cur.Precipitation, cur.WaterLevels = sh.Pressure, sh.Precipitation, sh.WaterLevels
	cur.Rating, cur.RatingNote, cur.Staff, cur.Machines = sh.Rating, sh.RatingNote, sh.Staff, sh.Machines
	if sh.WeatherSource != "" {
		cur.WeatherSource = sh.WeatherSource
	}
	*sh = *cur
	return s.repo.SaveSheet(ctx, cur)
}

// ConfirmSheet potvrđuje list za izvođača ili za nadzor, prema tome tko potvrđuje
func (s *JournalService) ConfirmSheet(ctx context.Context, u *models.User, perms *models.UserPermissions, area models.Area, sheetID string) error {
	if u == nil || !s.CanWrite(perms, area) {
		return errors.New("nemate pravo potvrditi list")
	}
	sh, err := s.repo.GetSheet(ctx, sheetID)
	if err != nil {
		return err
	}
	if sh == nil {
		return errors.New("list nije pronađen")
	}
	now := time.Now().UTC()
	if IsContractor(u) {
		sh.ContractorConfirmedBy, sh.ContractorConfirmedAt = u.FullName, &now
	} else {
		sh.SupervisorConfirmedBy, sh.SupervisorConfirmedAt = u.FullName, &now
	}
	return s.repo.SaveSheet(ctx, sh)
}

func (s *JournalService) EntriesForSheet(ctx context.Context, sheetID string) ([]models.JournalEntry, error) {
	return s.repo.EntriesForSheet(ctx, sheetID)
}

func (s *JournalService) OpenTasks(ctx context.Context, journalID string) ([]models.JournalEntry, error) {
	return s.repo.OpenTasks(ctx, journalID)
}

func (s *JournalService) NumberGaps(ctx context.Context, journalID string) ([]int, error) {
	return s.repo.NumberGaps(ctx, journalID)
}

// SheetCapacity je koliko izvođačevih upisa stane na jednu stranicu
// obrasca. Svaka stranica dnevnika nosi svoj broj lista: kad je list pun,
// izvođač otvara novi, istog dana, s istom ili drugom ekipom. Upisi nadzora
// se ne broje: za njih na svakom listu ostaje prostor, kao na obrascu.
const SheetCapacity = 6

// ContractorEntries broji izvođačeve upise na listu (i stornirane, jer
// zauzimaju redak na papiru)
func ContractorEntries(entries []models.JournalEntry) int {
	n := 0
	for _, e := range entries {
		if !e.IsSupervisor() {
			n++
		}
	}
	return n
}

// AddEntry upisuje na list; vrsta mora biti dopuštena osobi
func (s *JournalService) AddEntry(ctx context.Context, u *models.User, perms *models.UserPermissions, area models.Area, j *models.Journal, sh *models.JournalSheet, e *models.JournalEntry) (*models.JournalSheet, error) {
	if u == nil {
		return nil, errors.New("upis zahtijeva prijavu")
	}
	allowed := false
	for _, k := range s.AllowedKinds(u, perms, area) {
		if k == e.Kind {
			allowed = true
		}
	}
	if !allowed {
		return nil, errors.New("nemate pravo na tu vrstu upisa u ovaj dnevnik")
	}
	if sh.IsConfirmed() {
		return nil, errors.New("list je potvrđen s obje strane; upis ide na novi list")
	}
	e.Text = strings.TrimSpace(e.Text)
	if e.Text == "" && e.WorkItemID == "" {
		return nil, errors.New("upis mora imati opis rada ili stavku")
	}
	if e.Kind == models.EntryKindTask {
		e.Status = models.TaskOpen
	} else {
		e.DueDate, e.Status = nil, ""
	}

	e.Side = models.EntrySideContractor
	if !IsContractor(u) {
		e.Side = models.EntrySideSupervisor
	}
	// Pun list se ne nastavlja sam: izvođač otvara novi list, istog dana,
	// s istom ili drugom ekipom. Nadzor uvijek ima mjesta.
	if e.Side == models.EntrySideContractor {
		existing, err := s.repo.EntriesForSheet(ctx, sh.ID)
		if err != nil {
			return nil, err
		}
		if ContractorEntries(existing) >= SheetCapacity {
			return nil, fmt.Errorf("list %d je pun (%d upisa): otvorite novi list za ovaj dan", sh.Number, SheetCapacity)
		}
	}

	e.ID, e.Number = "", 0
	e.JournalID, e.SheetID, e.Date = j.ID, sh.ID, sh.Date
	e.UserID, e.UserName = u.ID.String(), u.FullName
	e.Voided, e.VoidReason, e.VoidedBy = false, "", ""
	if err := s.repo.SaveEntry(ctx, e); err != nil {
		return nil, err
	}
	return sh, nil
}

// VoidEntry stornira upis: ostaje na listu s brojem i razlogom
func (s *JournalService) VoidEntry(ctx context.Context, u *models.User, perms *models.UserPermissions, area models.Area, id, reason string) error {
	if u == nil || !s.CanWrite(perms, area) {
		return errors.New("nemate pravo storniranja")
	}
	e, err := s.repo.GetEntry(ctx, id)
	if err != nil {
		return err
	}
	if e == nil {
		return errors.New("upis nije pronađen")
	}
	if e.UserID != u.ID.String() && !s.CanSupervise(u, perms, area) {
		return errors.New("tuđi upis stornira samo nadzor")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("storniranje traži razlog")
	}
	e.Voided, e.VoidReason, e.VoidedBy = true, strings.TrimSpace(reason), u.FullName
	return s.repo.SaveEntry(ctx, e)
}

// SetTaskStatus mijenja stanje naloga: izvođač ga označava izvedenim,
// nadzor ga može i otkazati ili vratiti u otvoren
func (s *JournalService) SetTaskStatus(ctx context.Context, u *models.User, perms *models.UserPermissions, area models.Area, id, status string) error {
	if u == nil || !s.CanWrite(perms, area) {
		return errors.New("nemate pravo mijenjati nalog")
	}
	e, err := s.repo.GetEntry(ctx, id)
	if err != nil {
		return err
	}
	if e == nil || !e.IsTask() {
		return errors.New("nalog nije pronađen")
	}
	if e.Voided {
		return errors.New("storniran nalog ne mijenja stanje")
	}
	switch status {
	case models.TaskDone:
	case models.TaskOpen, models.TaskCancelled:
		if !s.CanSupervise(u, perms, area) {
			return errors.New("nalog otkazuje ili ponovno otvara samo nadzor")
		}
	default:
		return errors.New("nepoznato stanje naloga")
	}
	e.Status = status
	return s.repo.SaveEntry(ctx, e)
}
