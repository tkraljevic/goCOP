package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// AktService sastavlja, čuva i ovjerava akte o stupnju obrane. Akt se
// proglašava po vodomjeru i branjenom području, a dionice slijede iz toga:
// one za koje je vodomjer mjerodavan. Primatelji dolaze iz registra
// primatelja, iz ugroženih područja dionica (županije i općine, od
// izvanrednog stanja) i iz zaduženja na dionicama (rukovoditelji).
type AktService struct {
	repo        *repository.AktiRepository
	stations    *repository.StationRepository
	sections    *repository.SectionRepository
	territories *repository.TerritoryRepository
	readings    *repository.ReadingRepository
	users       *UserService
	episodes    *EpisodeService
	cvor        string
}

func NewAktService(repo *repository.AktiRepository, stations *repository.StationRepository, sections *repository.SectionRepository,
	territories *repository.TerritoryRepository, readings *repository.ReadingRepository, users *UserService, episodes *EpisodeService, cvor string) *AktService {
	return &AktService{repo: repo, stations: stations, sections: sections, territories: territories, readings: readings, users: users, episodes: episodes, cvor: cvor}
}

// ZahtjevAkta je što čovjek zada; ostalo se izvede
type ZahtjevAkta struct {
	StationID string
	Radnja    string
	Stupanj   models.DefensePhase
	Vrijedi   time.Time
	Prognoza  string // prazno = po izmjerenom vodostaju
	Napomena  string
	Dionice   []string // prazno = sve za koje je vodomjer mjerodavan
}

// Pripremi sastavlja nacrt akta iz vodomjera: dionice, zadnji vodostaj s
// tendencijom, potpisnik po stupnju i primatelji. Ništa ne sprema.
func (s *AktService) Pripremi(ctx context.Context, perms *models.UserPermissions, u *models.User, z ZahtjevAkta) (*models.Akt, error) {
	if perms == nil || u == nil {
		return nil, ErrUnauthorized
	}
	if z.Radnja != models.AktUspostava && z.Radnja != models.AktPrekid {
		return nil, fmt.Errorf("odaberi uspostavu ili prekid")
	}
	if !z.Stupanj.InForce() {
		return nil, fmt.Errorf("odaberi stupanj obrane")
	}
	id, err := uuid.Parse(z.StationID)
	if err != nil {
		return nil, fmt.Errorf("neispravan vodomjer")
	}
	st, err := s.stations.GetStationByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("vodomjer ne postoji")
	}
	a := &models.Akt{
		Radnja: z.Radnja, Stupanj: z.Stupanj, StationID: st.ID.String(), StationName: st.Name,
		Watercourse: nazivVode(st.Watercourse), Prognoza: strings.TrimSpace(z.Prognoza), Napomena: strings.TrimSpace(z.Napomena),
		Vrijedi: z.Vrijedi, Status: models.AktNacrt,
		IzradioID: u.ID.String(), Izradio: u.FullName, IzradenoAt: time.Now(),
	}
	if a.Vrijedi.IsZero() {
		a.Vrijedi = time.Now().Truncate(time.Minute)
	}

	// dionice za koje je vodomjer mjerodavan, ili uži izbor među njima
	sifre := st.SectionCodes
	if len(sifre) == 0 {
		sifre, _ = s.stations.GetSectionCodesForStation(ctx, st.ID)
	}
	if len(z.Dionice) > 0 {
		dopustene := map[string]bool{}
		for _, c := range sifre {
			dopustene[c] = true
		}
		var izbor []string
		for _, c := range z.Dionice {
			if dopustene[c] {
				izbor = append(izbor, c)
			}
		}
		sifre = izbor
	}
	if len(sifre) == 0 {
		return nil, fmt.Errorf("vodomjer %s nije mjerodavan ni za jednu dionicu; poveži ga s dionicama u registru", st.Name)
	}
	sort.Strings(sifre)
	podrucja := map[int]int{}
	for _, c := range sifre {
		sec, err := s.sections.GetSectionByCode(c)
		if err != nil || sec == nil {
			continue
		}
		a.Dionice = append(a.Dionice, models.AktDionica{Code: sec.Code, Opis: strings.TrimSpace(sec.Description)})
		podrucja[sec.AreaID]++
		if a.Sektor == "" {
			a.Sektor = sec.SectorID
		}
	}
	for areaID, n := range podrucja {
		if n > podrucja[a.AreaID] || a.AreaID == 0 {
			a.AreaID = areaID
		}
	}
	if !perms.HasWriteAccess(a.Sektor, a.AreaID, "") && !s.SmijeOvjeriti(perms, a) {
		return nil, fmt.Errorf("%w: akt za branjeno područje %d sastavlja tko ondje vodi obranu", ErrUnauthorized, a.AreaID)
	}

	// zadnji vodostaj i tendencija iz zadnja dva očitanja
	if a.Prognoza == "" {
		if zadnja, err := s.readings.List(ctx, repository.ReadingFilter{StationID: st.ID.String(), Limit: 2}); err == nil && len(zadnja) > 0 && zadnja[0].LevelCm != nil {
			v := *zadnja[0].LevelCm
			a.VodostajCm, a.VodostajKad = &v, zadnja[0].MeasuredAt
			a.Tendencija = models.TendencijaStagnacija
			if len(zadnja) > 1 && zadnja[1].LevelCm != nil {
				switch d := v - *zadnja[1].LevelCm; {
				case d > 0:
					a.Tendencija = models.TendencijaPorast
				case d < 0:
					a.Tendencija = models.TendencijaOpadanje
				}
			}
		}
	}
	a.Potpisnik = s.potpisnik(a)
	a.Primatelji = s.primatelji(ctx, a)
	return a, nil
}

// nazivVode je voda kako stoji u rečenici akta: "r. Dunav"
func nazivVode(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	l := strings.ToLower(v)
	if strings.HasPrefix(l, "r. ") || strings.HasPrefix(l, "p. ") || strings.HasPrefix(l, "k. ") || strings.HasPrefix(l, "rijeka ") || strings.HasPrefix(l, "potok ") || strings.HasPrefix(l, "kanal ") {
		return v
	}
	return "r. " + v
}

// potpisnik je funkcija koja akt donosi: pripremno i redovnu rukovoditelj
// branjenog područja, izvanrednu i izvanredno stanje rukovoditelj sektora
func (s *AktService) potpisnik(a *models.Akt) string {
	if a.Stupanj == models.PhaseEmergency || a.Stupanj == models.PhaseState {
		return "Rukovoditelj obrane od poplava Sektora " + a.Sektor
	}
	return fmt.Sprintf("Rukovoditelj obrane od poplava za branjeno područje %d", a.AreaID)
}

// SmijeOvjeriti javlja smije li osoba ovjeriti akt: pripremno i redovnu
// tko upravlja branjenim područjem ili sektorom, izvanrednu i izvanredno
// stanje tko upravlja sektorom
func (s *AktService) SmijeOvjeriti(perms *models.UserPermissions, a *models.Akt) bool {
	if perms == nil || a == nil {
		return false
	}
	if a.Stupanj == models.PhaseEmergency || a.Stupanj == models.PhaseState {
		return perms.CanAdminister(a.Sektor, 0)
	}
	return perms.CanAdminister(a.Sektor, a.AreaID)
}

// primatelji slaže popis primatelja akta, po skupinama i bez ponavljanja
func (s *AktService) primatelji(ctx context.Context, a *models.Akt) []models.AktPrimatelj {
	var out []models.AktPrimatelj
	vidjeno := map[string]bool{}
	dodaj := func(skupina, naziv, email string) {
		naziv = strings.TrimSpace(naziv)
		if naziv == "" {
			return
		}
		kljuc := strings.ToLower(naziv + "|" + email)
		if vidjeno[kljuc] {
			return
		}
		vidjeno[kljuc] = true
		out = append(out, models.AktPrimatelj{Naziv: naziv, Email: strings.TrimSpace(email), Skupina: skupina})
	}

	// registar: sektor i područje, od stupnja
	if reg, err := s.repo.ListPrimatelji(ctx, a.Sektor); err == nil {
		sort.SliceStable(reg, func(i, j int) bool {
			if reg[i].Skupina != reg[j].Skupina {
				return redSkupine(reg[i].Skupina) < redSkupine(reg[j].Skupina)
			}
			return reg[i].Redoslijed < reg[j].Redoslijed
		})
		for _, p := range reg {
			if p.Vrijedi(a.AreaID, a.Stupanj) {
				dodaj(p.Skupina, p.Naziv, p.Email)
			}
		}
	}
	// ugovorna pravna osoba branjenog područja
	if areas, err := s.users.ListAreas(a.Sektor); err == nil {
		for _, ar := range areas {
			if ar.ID == a.AreaID && ar.ContractorName != "" {
				dodaj(models.SkupinaIspostava, ar.ContractorName, "")
			}
		}
	}
	// županije i općine ugroženih područja, od izvanrednog stanja
	if a.Stupanj == models.PhaseState && s.territories != nil {
		zupanije := map[int]string{}
		var opcine []models.SectionTerritory
		for _, d := range a.Dionice {
			ter, err := s.territories.GetSectionTerritories(ctx, d.Code)
			if err != nil {
				continue
			}
			for _, t := range ter {
				zupanije[t.CountyID] = t.CountyName
				opcine = append(opcine, t)
			}
		}
		for id, naziv := range zupanije {
			email := ""
			if c, err := s.territories.GetCountyByID(ctx, id); err == nil && c != nil {
				email = c.Email
				naziv = c.Name
			}
			dodaj(models.SkupinaSamouprava, "Župan, "+naziv, email)
		}
		// e-pošta općina iz registra, jednim upitom po županiji
		emailOpcine := map[int]string{}
		for id := range zupanije {
			if ms, err := s.territories.ListMunicipalities(ctx, id, "", ""); err == nil {
				for _, m := range ms {
					emailOpcine[m.ID] = m.Email
				}
			}
		}
		sort.Slice(opcine, func(i, j int) bool { return opcine[i].MunicipalityName < opcine[j].MunicipalityName })
		for _, t := range opcine {
			vrsta := "Općina"
			if strings.EqualFold(t.MunicipalityType, "GRAD") {
				vrsta = "Grad"
			}
			dodaj(models.SkupinaSamouprava, vrsta+" "+t.MunicipalityName, emailOpcine[t.MunicipalityID])
		}
	}
	// rukovoditelji i zamjenici na dionicama
	for _, d := range a.Dionice {
		osobe, err := s.sections.GetSectionPersonnel(d.Code, a.AreaID, a.Sektor)
		if err != nil {
			continue
		}
		for _, o := range osobe {
			if o.Rank > 4 {
				continue // teren i ostali ne primaju akt
			}
			naziv := o.FullName
			if o.Title != "" {
				naziv += ", " + o.Title
			}
			if o.DutyTitle != "" {
				naziv += ", " + o.DutyTitle
			}
			dodaj(models.SkupinaOsobe, naziv, o.Email)
		}
	}
	dodaj(models.SkupinaPismohrana, "Pismohrana", "")
	sort.SliceStable(out, func(i, j int) bool { return redSkupine(out[i].Skupina) < redSkupine(out[j].Skupina) })
	return out
}

// osnovaEpizode je po čemu je obrana proglašena, za epizodu na dionici
func osnovaEpizode(a *models.Akt) string {
	if a.Prognoza != "" {
		return models.BasisForecast
	}
	return models.BasisThreshold
}

func redSkupine(s string) int {
	for i, x := range models.SkupinePrimatelja {
		if x == s {
			return i
		}
	}
	return len(models.SkupinePrimatelja)
}

// Spremi sprema nacrt; ovjeren akt se ne mijenja
func (s *AktService) Spremi(ctx context.Context, perms *models.UserPermissions, a *models.Akt) error {
	if perms == nil {
		return ErrUnauthorized
	}
	if a.ID != "" {
		postojeci, err := s.repo.GetAkt(ctx, a.ID)
		if err != nil {
			return err
		}
		if postojeci != nil && postojeci.Ovjeren() {
			return fmt.Errorf("akt %s je ovjeren i ne mijenja se; ispravak je novi akt", postojeci.Oznaka())
		}
	}
	if !perms.HasWriteAccess(a.Sektor, a.AreaID, "") && !s.SmijeOvjeriti(perms, a) {
		return ErrUnauthorized
	}
	if len(a.Dionice) == 0 {
		return fmt.Errorf("akt bez dionica")
	}
	if a.Vrijedi.IsZero() {
		return fmt.Errorf("upiši dan i sat od kojeg vrijedi")
	}
	a.Status = models.AktNacrt
	a.Godina = a.Vrijedi.In(models.Zagreb).Year()
	return s.repo.SaveAkt(ctx, a)
}

// Ovjeri ovjerava nacrt: dodijeli broj, upiše tko i kad, izračuna kod i
// proglasi ili prekine obranu na dionicama. Vraća upozorenja s dionica na
// kojima se stanje obrane nije dalo uskladiti; akt je svejedno ovjeren, jer
// je odluka donesena, a stanje se može ispraviti na dionici.
func (s *AktService) Ovjeri(ctx context.Context, perms *models.UserPermissions, u *models.User, id string) (*models.Akt, []string, error) {
	if perms == nil || u == nil {
		return nil, nil, ErrUnauthorized
	}
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a == nil {
		return nil, nil, fmt.Errorf("akt ne postoji")
	}
	if a.Ovjeren() {
		return a, nil, fmt.Errorf("akt %s je već ovjeren", a.Oznaka())
	}
	if !s.SmijeOvjeriti(perms, a) {
		return nil, nil, fmt.Errorf("%w: %s ovjerava %s", ErrUnauthorized, a.Naslov(), strings.ToLower(a.Potpisnik))
	}
	sad := time.Now()
	a.Godina = a.Vrijedi.In(models.Zagreb).Year()
	if a.Broj, err = s.repo.SljedeciBroj(ctx, a.Sektor, a.Godina); err != nil {
		return nil, nil, err
	}
	a.Status = models.AktOvjeren
	a.OvjerioID, a.Ovjerio, a.OvjerenoAt, a.Cvor = u.ID.String(), u.FullName, &sad, s.cvor
	a.UZamjeni = !a.NositeljFunkcije(u.Duties)
	a.OvjeraKod = a.KodOvjere(a.OvjerioID, sad)
	if err := s.repo.SaveAkt(ctx, a); err != nil {
		return nil, nil, err
	}

	var upozorenja []string
	if s.episodes != nil {
		st, _ := s.stations.GetStationByID(ctx, uuid.MustParse(a.StationID))
		if st == nil {
			st = &models.Station{Name: a.StationName}
		}
		biljeska := a.Naslov() + " " + a.Oznaka()
		for _, d := range a.Dionice {
			var err error
			if a.Radnja == models.AktUspostava {
				otvorena, _ := s.episodes.Open(ctx, d.Code)
				if otvorena == nil {
					_, err = s.episodes.Declare(ctx, perms, u.ID.String(), d.Code, *st, a.Vrijedi, a.Stupanj, osnovaEpizode(a), biljeska)
				} else if a.Stupanj.Severity() > otvorena.Phase.Severity() {
					err = s.episodes.Raise(ctx, perms, d.Code, a.Stupanj, biljeska)
				}
			} else if a.Stupanj == models.PhasePrep {
				err = s.episodes.End(ctx, perms, u.ID.String(), d.Code, a.Vrijedi, biljeska)
			} else if otvorena, _ := s.episodes.Open(ctx, d.Code); otvorena != nil {
				err = s.episodes.Raise(ctx, perms, d.Code, otvorena.Phase, biljeska)
			}
			if err != nil {
				upozorenja = append(upozorenja, d.Code+": "+err.Error())
			}
		}
	}
	return a, upozorenja, nil
}

// ZadnjeOcitanje je zadnje očitanje letve, za obrazac akta
func (s *AktService) ZadnjeOcitanje(ctx context.Context, stationID string) (*models.Reading, error) {
	zadnja, err := s.readings.List(ctx, repository.ReadingFilter{StationID: stationID, Limit: 1})
	if err != nil || len(zadnja) == 0 {
		return nil, err
	}
	return &zadnja[0], nil
}

// DioniceLetve su dionice za koje je vodomjer mjerodavan, s opisom
func (s *AktService) DioniceLetve(ctx context.Context, st *models.Station) []models.Section {
	sifre := st.SectionCodes
	if len(sifre) == 0 {
		sifre, _ = s.stations.GetSectionCodesForStation(ctx, st.ID)
	}
	var out []models.Section
	for _, c := range sifre {
		if sec, err := s.sections.GetSectionByCode(c); err == nil && sec != nil {
			out = append(out, *sec)
		}
	}
	return out
}

// Get čita akt
func (s *AktService) Get(ctx context.Context, id string) (*models.Akt, error) {
	return s.repo.GetAkt(ctx, id)
}

// List vraća akte po filtru, samo iz sektora koje osoba vidi
func (s *AktService) List(ctx context.Context, perms *models.UserPermissions, f repository.FiltarAkata) ([]models.Akt, error) {
	akti, err := s.repo.ListAkti(ctx, f)
	if err != nil || perms == nil || perms.IsGlobalAdmin {
		return akti, err
	}
	var out []models.Akt
	for _, a := range akti {
		if perms.AllowedSectors[a.Sektor] || perms.AllowedAreas[a.AreaID] || perms.AdminSectors[a.Sektor] || perms.AdminAreas[a.AreaID] || vidiDionicu(perms, a) {
			out = append(out, a)
		}
	}
	return out, nil
}

func vidiDionicu(perms *models.UserPermissions, a models.Akt) bool {
	for _, d := range a.Dionice {
		if perms.AllowedSections[d.Code] {
			return true
		}
	}
	return false
}

// Obrisi briše nacrt; smije tko ga je sastavio ili tko bi ga smio ovjeriti
func (s *AktService) Obrisi(ctx context.Context, perms *models.UserPermissions, u *models.User, id string) error {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil || a == nil {
		return err
	}
	if a.Ovjeren() {
		return fmt.Errorf("ovjeren akt se ne briše")
	}
	if u == nil || (a.IzradioID != u.ID.String() && !s.SmijeOvjeriti(perms, a)) {
		return ErrUnauthorized
	}
	return s.repo.DeleteAkt(ctx, id)
}

// ---- registar primatelja ----

// Primatelji vraća registar primatelja sektora
func (s *AktService) Primatelji(ctx context.Context, sektor string) ([]models.Primatelj, error) {
	return s.repo.ListPrimatelji(ctx, sektor)
}

// SpremiPrimatelja upisuje primatelja; smije uprava sektora
func (s *AktService) SpremiPrimatelja(ctx context.Context, perms *models.UserPermissions, p *models.Primatelj) error {
	if perms == nil || !perms.CanAdminister(p.Sektor, p.AreaID) {
		return ErrUnauthorized
	}
	p.Naziv = strings.TrimSpace(p.Naziv)
	if p.Naziv == "" {
		return fmt.Errorf("upiši naziv primatelja")
	}
	if p.Skupina == "" {
		p.Skupina = models.SkupinaSluzbe
	}
	return s.repo.SavePrimatelj(ctx, p)
}

// ObrisiPrimatelja briše primatelja iz registra
func (s *AktService) ObrisiPrimatelja(ctx context.Context, perms *models.UserPermissions, id string) error {
	p, err := s.repo.GetPrimatelj(ctx, id)
	if err != nil || p == nil {
		return err
	}
	if perms == nil || !perms.CanAdminister(p.Sektor, p.AreaID) {
		return ErrUnauthorized
	}
	return s.repo.DeletePrimatelj(ctx, id)
}
