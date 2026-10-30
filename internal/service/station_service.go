package service

import (
	"context"
	"errors"
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

func (s *StationService) ListStations(ctx context.Context, search, watercourse, country string, onlyNeedsReview bool) ([]models.Station, error) {
	return s.stationRepo.ListStations(ctx, search, watercourse, country, onlyNeedsReview)
}

// BrojOcitanja govori koliko je očitanja upisano na letvi.
func (s *StationService) BrojOcitanja(ctx context.Context, id uuid.UUID) int {
	return s.stationRepo.BrojOcitanja(ctx, id)
}

func (s *StationService) GetStation(ctx context.Context, id uuid.UUID) (*models.Station, error) {
	return s.stationRepo.GetStationByID(ctx, id)
}

// GetStationByCode vraća postaju po šifri. Arhiva letvu zna samo po šifri —
// stablo vodostaji/ je po njoj i složeno — pa se preko nje dolazi do dionica
// na kojima letva stoji, a time i do toga smije li je netko dirati.
func (s *StationService) GetStationByCode(ctx context.Context, code string) (*models.Station, error) {
	return s.stationRepo.GetStationByCode(ctx, code)
}

func (s *StationService) ListWatercourses(ctx context.Context) ([]string, error) {
	return s.stationRepo.ListWatercourses(ctx)
}

func (s *StationService) ListCountries(ctx context.Context) ([]string, error) {
	return s.stationRepo.ListCountries(ctx)
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
		return greskaOvlasti(fmt.Sprintf("nemate ovlasti za izmjenu mjerodavnih vodomjera na dionici %s", sectionCode))
	}
	return nil
}

// CreateStation upisuje novu vodomjernu postaju u registar i, kad je zadana
// dionica, odmah je proglašava njezinim mjerodavnim vodomjerom
func (s *StationService) CreateStation(ctx context.Context, perms *models.UserPermissions, station *models.Station, sectionCode string) error {
	if err := s.provjeriNovu(ctx, station); err != nil {
		return err
	}
	if err := s.ProvjeriUnos(perms, sectionCode); err != nil {
		return err
	}

	if strings.TrimSpace(station.Code) == "" {
		station.Code = hydro.Slug(station.Name)
	}
	preispitajPregled(station)

	if err := s.stationRepo.CreateStation(ctx, station); err != nil {
		return err
	}

	// Mjerodavnost za dionicu upisuje se u poddionici na obrascu dionice
	return nil
}

// ProvjeriUnos javlja grešku ako korisnik ne smije unijeti novu postaju na
// dionicu. Postaju izvan dionice unosi samo globalni administrator.
func (s *StationService) ProvjeriUnos(perms *models.UserPermissions, sectionCode string) error {
	if sectionCode != "" {
		return s.canEditSection(perms, sectionCode)
	}
	if perms == nil || !perms.IsGlobalAdmin {
		return greskaOvlasti("postaju izvan dionice može unijeti samo globalni administrator")
	}
	return nil
}

// ProvjeriIzmjenu javlja grešku ako korisnik ne smije mijenjati postaju:
// treba mu pravo na barem jednu dionicu na kojoj postaja stoji. Isto pravilo
// vrijedi za svaku izmjenu postaje, i za vodotok.
func (s *StationService) ProvjeriIzmjenu(perms *models.UserPermissions, station *models.Station) error {
	if station == nil {
		return fmt.Errorf("vodomjerna postaja nije pronađena")
	}
	return s.requireAnySectionAccess(perms, station.SectionCodes)
}

// PostajaZaIzmjenu učita postaju po identifikatoru i provjeri smije li je
// korisnik mijenjati. Vraća identifikator onako kako ga baza zapisuje, da se
// upis odnosi baš na postaju čije je pravo provjereno.
func (s *StationService) PostajaZaIzmjenu(ctx context.Context, perms *models.UserPermissions, stationID string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(stationID))
	if err != nil {
		return "", fmt.Errorf("vodomjerna postaja nije pronađena")
	}
	st, err := s.stationRepo.GetStationByID(ctx, id)
	if err != nil {
		return "", err
	}
	if err := s.ProvjeriIzmjenu(perms, st); err != nil {
		return "", err
	}
	return st.ID.String(), nil
}

// ErrNemaOvlasti stoji iza svake greške koja kaže da korisnik nema pravo na
// radnju, da je web može odvojiti od neispravnog unosa (403, a ne 400).
var ErrNemaOvlasti = errors.New("nemate ovlasti")

// greskaOvlasti je poruka korisniku koji nema pravo na radnju
type greskaOvlasti string

func (g greskaOvlasti) Error() string { return string(g) }

func (g greskaOvlasti) Unwrap() error { return ErrNemaOvlasti }

// bezPragovaNapomena je napomena koju program sam upiše kad postaja nema
// pragova u centimetrima. Prepoznaje se po tekstu, pa se poslije zna razlikovati
// od onoga što je napisao čovjek.
const bezPragovaNapomena = "nijedan prag nije zapisan u centimetrima — faza obrane se ne računa automatski"

// preispitajPregled postavlja i SKIDA oznaku da postaja traži pregled.
//
// Skidanje je bilo ono što je nedostajalo: oznaka se postavljala pri otvaranju
// prazne kartice i ostajala zauvijek, pa je Vukovar s upisanih 530/580/630/680
// i dalje javljao da nijedan prag nije zapisan u centimetrima. Ono što je
// napisao čovjek se ne dira — on je mogao označiti nešto sasvim drugo.
func preispitajPregled(station *models.Station) {
	if !station.HasUsableThresholds() {
		station.NeedsReview = true
		if station.ReviewNote == "" {
			station.ReviewNote = bezPragovaNapomena
		}
		return
	}
	if station.ReviewNote == "" || station.ReviewNote == bezPragovaNapomena {
		station.NeedsReview = false
		station.ReviewNote = ""
	}
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
	if err := s.provjeriIzmjenuIPracenje(ctx, perms, station, existing); err != nil {
		return err
	}
	preispitajPregled(station)

	// Ovdje je nekad stajalo vraćanje izvora kote, načina, datuma i naziva iz
	// dokumentacije na stare vrijednosti, uz obrazloženje da ih obrazac ne
	// šalje. Obrazac ih šalje, i to je značilo da se upisano tiho baca: tko bi
	// obrisao napomenu o koti, dobio bi je natrag pri svakom spremanju.
	//
	// Ono što obrazac doista ne uređuje čuva rukovatelj — svaki obrazac prenosi
	// samo svoja polja na postojeći zapis, pa ovdje nema što nadoknađivati.
	// Šifra je identitet letve: po njoj se vode arhiva, račun telemetrije,
	// ispravci i bilješke. Izmjena je ne mijenja, kakva god stigla.
	station.Code = existing.Code

	podrijetloVode(station, existing)

	// Letva bez ijednog praga u centimetrima traži pregled bez obzira na
	// kvačicu: to nije mišljenje operatera nego stanje podatka. Ima li pragove,
	// odlučuje operater — dotad je kvačica bila bez ikakva učinka jer ju je ovaj
	// redak prepisivao.
	if !station.HasUsableThresholds() {
		station.NeedsReview = true
		if strings.TrimSpace(station.ReviewNote) == "" {
			station.ReviewNote = "nijedan prag nije zapisan u centimetrima — faza obrane se ne računa automatski"
		}
	} else if !station.NeedsReview {
		station.ReviewNote = ""
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

// podrijetloVode namješta podrijetlo naziva vode i vezu na registar voda.
// Naziv vodotoka mijenja i podrijetlo podatka: ručni unos je potvrda
// operatera i raskida vezu na registar dok se ne uspostavi iznova. Obrazac
// postaje polazi od zatečenog zapisa, pa uz novi naziv nosi staru šifru vode;
// takva šifra pokazuje na vodu koje na postaji više nema i pada. Nova šifra
// uz novi naziv (veza na vodu iz registra) ostaje.
func podrijetloVode(station, existing *models.Station) {
	if strings.EqualFold(strings.TrimSpace(station.Watercourse), strings.TrimSpace(existing.Watercourse)) {
		station.WatercourseSource = existing.WatercourseSource
		return
	}
	station.WatercourseSource = models.WatercourseFromOperator
	if strings.TrimSpace(station.Watercourse) == "" {
		station.WatercourseSource = models.WatercourseUndetermined
	}
	if station.WatercourseCode == existing.WatercourseCode {
		station.WatercourseCode = ""
	}
}

// DeleteStation briše postaju iz registra. Postaja mjerodavna za neku dionicu
// briše se samo odlukom globalnog administratora, jer nestaje sa svih dionica.
func (s *StationService) DeleteStation(ctx context.Context, perms *models.UserPermissions, id uuid.UUID) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("vodomjernu postaju iz registra može obrisati samo globalni administrator")
	}
	return s.stationRepo.DeleteStation(ctx, id)
}

// provjeriNovu provjerava podatke nove postaje i sektore i područja koji je
// prate
func (s *StationService) provjeriNovu(ctx context.Context, station *models.Station) error {
	if err := s.validate(station); err != nil {
		return err
	}
	return s.provjeriPracenje(ctx, station, nil)
}

// provjeriIzmjenuIPracenje traži ovlast nad postajom i provjerava sektore i
// područja koji je prate
func (s *StationService) provjeriIzmjenuIPracenje(ctx context.Context, perms *models.UserPermissions, station, existing *models.Station) error {
	if err := s.ProvjeriIzmjenu(perms, existing); err != nil {
		return err
	}
	return s.provjeriPracenje(ctx, station, existing)
}

// provjeriPracenje sređuje sektore i branjena područja koji letvu prate i
// traži da su u registru (Odluke (8. 10. 2026.), pitanje 8). Ono što je
// letva već imala ne provjerava se ponovo: područje koje je u međuvremenu
// nestalo iz registra ne smije zaustaviti svaku izmjenu letve.
func (s *StationService) provjeriPracenje(ctx context.Context, station, staro *models.Station) error {
	station.UrediPracenje()
	sektori, podrucja := station.NovoPracenje(staro)
	nepoznato, err := s.stationRepo.NepoznatoPracenje(ctx, sektori, podrucja)
	if err != nil {
		return err
	}
	if nepoznato != "" {
		return fmt.Errorf("%s nije u registru, pa ne može pratiti letvu", nepoznato)
	}
	return nil
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
	return greskaOvlasti("nemate ovlasti za izmjenu ove vodomjerne postaje")
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
