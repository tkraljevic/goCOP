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

// CanWrite: pravo pisanja u dosegu dnevnika (izvođač ga ima kroz svoju
// dužnost). Doseg, ne područje: dnevnik sektorskog COP-a prima upis od
// svakoga tko vodi sektor ili bilo koje područje u njemu.
func (s *JournalService) CanWrite(perms *models.UserPermissions, o models.Opseg) bool {
	if perms == nil || o.Prazan() {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	if o.Sektor != "" && (perms.AdminSectors[o.Sektor] || perms.AllowedSectors[o.Sektor]) {
		return true
	}
	for _, id := range o.Podrucja {
		if perms.AdminAreas[id] || perms.AllowedAreas[id] {
			return true
		}
	}
	return false
}

// CanSupervise: nadzor piše nalog i ocjenu i potvrđuje list za nadzor — HV
// strana s pravom pisanja, ne izvođač
func (s *JournalService) CanSupervise(u *models.User, perms *models.UserPermissions, o models.Opseg) bool {
	return s.CanWrite(perms, o) && !IsContractor(u)
}

// CanManage: naslovnicu uređuje nadzor ili administrator područja
func (s *JournalService) CanManage(u *models.User, perms *models.UserPermissions, o models.Opseg) bool {
	return s.CanSupervise(u, perms, o)
}

// AllowedKinds vraća vrste upisa koje osoba smije pisati u dnevnik j.
// Dnevnik COP-a ima svoje vrste: u zapisnik dežurstva ne ulazi rad
// izvođača ni nalog, a dojava i obavijest nemaju što tražiti na listu usluge.
func (s *JournalService) AllowedKinds(u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal) []string {
	if !s.CanWrite(perms, o) {
		return nil
	}
	if j != nil && j.CentarSektor != "" {
		return models.EntryKindsCOP
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
	if !s.CanManage(u, perms, models.OpsegPodrucja(area)) {
		return errors.New("naslovnicu dnevnika uređuje ovlaštenik ili rukovoditelj područja")
	}
	if !models.IsJournalKind(j.Kind) {
		return errors.New("nepoznata vrsta dnevnika")
	}
	// Obrana nema naslovnicu s izvođačem: njezin je dnevnik zapisnik
	// dežurstva centra i otvara se u COP-u, ne po području.
	if j.IsDefense() {
		return errors.New("dnevnik obrane vodi se u centru obrane: otvorite dnevnik COP-a")
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
func (s *JournalService) NewSheet(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal, day time.Time, label string) (*models.JournalSheet, error) {
	if !s.CanWrite(perms, o) {
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
func (s *JournalService) RefreshWeather(ctx context.Context, perms *models.UserPermissions, o models.Opseg, j *models.Journal, sh *models.JournalSheet) error {
	if !s.CanWrite(perms, o) {
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
func (s *JournalService) UpdateSheet(ctx context.Context, perms *models.UserPermissions, o models.Opseg, sh *models.JournalSheet) error {
	if !s.CanWrite(perms, o) {
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
func (s *JournalService) ConfirmSheet(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, sheetID string) error {
	if u == nil || !s.CanWrite(perms, o) {
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
func (s *JournalService) AddEntry(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal, sh *models.JournalSheet, e *models.JournalEntry) (*models.JournalSheet, error) {
	if u == nil {
		return nil, errors.New("upis zahtijeva prijavu")
	}
	allowed := false
	for _, k := range s.AllowedKinds(u, perms, o, j) {
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
func (s *JournalService) VoidEntry(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, id, reason string) error {
	if u == nil || !s.CanWrite(perms, o) {
		return errors.New("nemate pravo storniranja")
	}
	e, err := s.repo.GetEntry(ctx, id)
	if err != nil {
		return err
	}
	if e == nil {
		return errors.New("upis nije pronađen")
	}
	if e.UserID != u.ID.String() && !s.CanSupervise(u, perms, o) {
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
func (s *JournalService) SetTaskStatus(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, id, status string) error {
	if u == nil || !s.CanWrite(perms, o) {
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
		if !s.CanSupervise(u, perms, o) {
			return errors.New("nalog otkazuje ili ponovno otvara samo nadzor")
		}
	default:
		return errors.New("nepoznato stanje naloga")
	}
	e.Status = status
	return s.repo.SaveEntry(ctx, e)
}

// BrojPoVrstama broji dnevnike po vrsti.
func (s *JournalService) BrojPoVrstama(ctx context.Context) (map[string]int, error) {
	return s.repo.BrojPoVrstama(ctx)
}

// ListCOPJournals vraća dnevnike centara obrane.
func (s *JournalService) ListCOPJournals(ctx context.Context, sektor string) ([]models.Journal, error) {
	return s.repo.ListCOPJournals(ctx, sektor)
}

// CentriSDnevnicima vraća centre koji imaju barem jedan dnevnik.
func (s *JournalService) CentriSDnevnicima(ctx context.Context) ([]models.Centar, error) {
	return s.repo.CentriSDnevnicima(ctx)
}

// EntriesForJournal vraća zapise dežurstva jednog dnevnika.
func (s *JournalService) EntriesForJournal(ctx context.Context, journalID string) ([]models.JournalEntry, error) {
	return s.repo.EntriesForJournal(ctx, journalID)
}

// MozeOtvoritiCOP: dnevnik COP-a otvara voditelj ili zamjenik centra — uprava
// sektora — ne svatko tko u njega piše. Dežurni s područja piše, ali ne
// otvara: dnevnik je centra, a ne njegov.
func (s *JournalService) MozeOtvoritiCOP(perms *models.UserPermissions, sektor string) bool {
	return perms != nil && sektor != "" && perms.CanAdminister(sektor, 0)
}

// SpremiCOPDnevnik otvara dnevnik centra ili mu mijenja zaglavlje. Bez
// izvođača i nadzora: centar, naziv, početak dežurstva, i kraj kad obrana
// prestane. Centar se poslije otvaranja ne mijenja — dnevnik je njegov.
func (s *JournalService) SpremiCOPDnevnik(ctx context.Context, u *models.User, perms *models.UserPermissions, j *models.Journal) error {
	if j.ID != "" {
		cur, err := s.repo.GetJournal(ctx, j.ID)
		if err != nil {
			return err
		}
		if cur == nil || cur.CentarSektor == "" {
			return errors.New("dnevnik COP-a nije pronađen")
		}
		if !s.MozeOtvoritiCOP(perms, cur.CentarSektor) {
			return errors.New("zaglavlje dnevnika COP-a mijenja voditelj ili zamjenik centra")
		}
		cur.Title, cur.StartedAt, cur.EndedAt, cur.Notes = j.Title, j.StartedAt, j.EndedAt, j.Notes
		*j = *cur
	} else {
		if !s.MozeOtvoritiCOP(perms, j.CentarSektor) {
			return errors.New("dnevnik COP-a otvara voditelj ili zamjenik centra")
		}
		j.Kind, j.AreaID, j.CentarPodrucje, j.Reconstruction = models.JournalKindDefense, 0, nil, false
		if u != nil {
			j.CreatedBy = u.ID.String()
		}
	}
	if j.StartedAt == nil {
		return errors.New("upišite početak dežurstva")
	}
	if j.EndedAt != nil && j.EndedAt.Before(*j.StartedAt) {
		return errors.New("kraj dežurstva je prije početka")
	}
	j.Year = j.StartedAt.In(models.Zagreb).Year()
	j.Title = strings.TrimSpace(j.Title)
	if j.Title == "" {
		j.Title = fmt.Sprintf("Dnevnik COP-a, %d.", j.Year)
	}
	return s.repo.SaveJournal(ctx, j)
}

// CentriZaOtvaranje vraća centre u kojima osoba smije otvoriti dnevnik.
func (s *JournalService) CentriZaOtvaranje(perms *models.UserPermissions, sektori []models.Sector) []models.Centar {
	var out []models.Centar
	for _, sk := range sektori {
		if sk.Level != 2 || sk.CenterCop == "" || !s.MozeOtvoritiCOP(perms, sk.ID) {
			continue
		}
		out = append(out, models.Centar{Sektor: sk.ID, Naziv: sk.CenterCop})
	}
	return out
}

// DodajZapisCOP upisuje u zapisnik dežurstva. Zapis se veže izravno na dnevnik,
// bez lista: nosi dan, vrijeme kad se dogodilo (kad se zna), tko je javio i
// tekst. Tko je upisao dolazi iz prijave, ne iz obrasca — to je onaj koji
// odgovara za zapis, dok za sadržaj odgovara onaj tko je javio.
func (s *JournalService) DodajZapisCOP(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal, e *models.JournalEntry) error {
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	if j == nil || j.CentarSektor == "" {
		return errors.New("ovo nije dnevnik COP-a")
	}
	dopustena := false
	for _, k := range s.AllowedKinds(u, perms, o, j) {
		if k == e.Kind {
			dopustena = true
		}
	}
	if !dopustena {
		return errors.New("nemate pravo na tu vrstu zapisa u ovaj dnevnik")
	}
	e.Text = strings.TrimSpace(e.Text)
	if e.Text == "" {
		return errors.New("zapis mora imati tekst")
	}
	if e.Date.IsZero() {
		return errors.New("zapis mora imati dan")
	}
	// Zaključen dnevnik ne prima zapise poslije kraja: što se dogodilo poslije
	// obrane ide u sljedeći dnevnik, a ne u onaj koji je već zapečaćen.
	if j.EndedAt != nil && e.Date.After(*j.EndedAt) {
		return fmt.Errorf("dnevnik je zaključen %s: zapis poslije toga ide u novi dnevnik", j.EndedAt.In(models.Zagreb).Format("2.1.2006."))
	}
	if j.StartedAt != nil && e.Date.Before(*j.StartedAt) {
		return fmt.Errorf("dnevnik počinje %s: zapis prije toga u njega ne ide", j.StartedAt.In(models.Zagreb).Format("2.1.2006."))
	}
	if err := podrucjeUSektoru(e.Podrucje, o); err != nil {
		return err
	}
	e.ID, e.Number, e.SheetID, e.Side = "", 0, "", ""
	e.JournalID = j.ID
	e.ReportedBy = strings.TrimSpace(e.ReportedBy)
	e.UserID, e.UserName = u.ID.String(), u.FullName
	e.DueDate, e.Status, e.WorkItemID = nil, "", ""
	e.Voided, e.VoidReason, e.VoidedBy = false, "", ""
	return s.repo.SaveEntry(ctx, e)
}

func (s *JournalService) GetEntry(ctx context.Context, id string) (*models.JournalEntry, error) {
	return s.repo.GetEntry(ctx, id)
}

// IspraviPrijepis ispravlja krivo pročitan zapis prijepisa na mjestu.
//
// Samo u prijepisu: ondje je zapis preslika uveza, a dokument je papir, pa
// ispravak presliku približava izvorniku. U živom dnevniku zapis JEST
// dokument i ne prepravlja se — ispravak je novi zapis uz stari. Ni ovdje
// prijašnje čitanje ne nestaje: knjiga verzija ga zadrži kao stariju verziju.
//
// Dan zapisa se ne mijenja: dan je granica dnevnika i redoslijeda, a krivo
// pročitan dan se rješava novim zapisom u pravom danu i stornom krivog.
func (s *JournalService) IspraviPrijepis(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal, id string, ispravak models.JournalEntry) error {
	if u == nil {
		return errors.New("ispravak zahtijeva prijavu")
	}
	if j == nil || !j.SmijePrepravakZapisa() {
		return errors.New("u živom dnevniku zapis se ne prepravlja: ispravak je novi zapis uz stari")
	}
	if !s.CanWrite(perms, o) {
		return errors.New("nemate pravo ispravljati ovaj dnevnik")
	}
	e, err := s.repo.GetEntry(ctx, id)
	if err != nil {
		return err
	}
	if e == nil || e.JournalID != j.ID {
		return errors.New("zapis nije pronađen u ovom dnevniku")
	}
	dopustena := false
	for _, k := range s.AllowedKinds(u, perms, o, j) {
		if k == ispravak.Kind {
			dopustena = true
		}
	}
	if !dopustena {
		return errors.New("nepoznata vrsta zapisa za dnevnik COP-a")
	}
	ispravak.Text = strings.TrimSpace(ispravak.Text)
	if ispravak.Text == "" {
		return errors.New("zapis mora imati tekst")
	}
	if err := podrucjeUSektoru(ispravak.Podrucje, o); err != nil {
		return err
	}
	e.Kind, e.Text, e.ReportedBy, e.HappenedAt, e.Podrucje = ispravak.Kind, ispravak.Text, strings.TrimSpace(ispravak.ReportedBy), ispravak.HappenedAt, ispravak.Podrucje
	return s.repo.SaveEntry(ctx, e)
}

// podrucjeUSektoru provjerava da područje zapisa pripada sektoru centra;
// prazno je cijeli sektor i uvijek prolazi
func podrucjeUSektoru(podrucje *int, o models.Opseg) error {
	if podrucje == nil || *podrucje <= 0 {
		return nil
	}
	for _, id := range o.Podrucja {
		if id == *podrucje {
			return nil
		}
	}
	return errors.New("branjeno područje nije u sektoru ovog centra")
}
