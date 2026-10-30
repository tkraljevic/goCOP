package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// IzvjescaService vodi dnevna izvješća rukovoditelja dionica. Piše ga i
// predaje rukovoditelj dionice ili zamjenik; uprava područja i sektora ga
// čita (Odluke (30. 10. 2026.), 3). Obrazac se popunjava iz onoga što program već zna:
// vodotok iz dionice, vodostaji u 07:00 iz očitanja mjerodavnih vodomjera,
// stadij iz proglašene obrane, obrana iz otvorenog dnevnika COP-a.
type IzvjescaService struct {
	repo      *repository.IzvjescaRepository
	sections  *repository.SectionRepository
	stations  *repository.StationRepository
	readings  *repository.ReadingRepository
	episodes  *repository.EpisodeRepository
	journals  *repository.JournalRepository
	sektorska *repository.SektorskaIzvjescaRepository // sektorska izvješća; nil dok se ne postavi
	// stanjeObrane je stanje obrane dionice iz ovjerenih akata; nil dok se
	// ne postavi (tada vrijedi epizoda)
	stanjeObrane func(ctx context.Context, sektor, dionica string, t time.Time) (models.StanjeObrane, []models.Akt, error)
	// autori čita račun onoga tko je izradio izvješće; nil kad se ne zna
	// (tada se uzima da autor i dalje vodi dionicu)
	autori AutoriIzvjesca
}

// AutoriIzvjesca čita račun onoga tko je izradio izvješće, s dužnostima, i
// javlja je li račun obrisan (UserRepository)
type AutoriIzvjesca interface {
	GetUserByID(id uuid.UUID) (*models.User, error)
	RacunObrisan(id uuid.UUID) (bool, error)
}

// SetStanjeObrane postavlja stanje obrane iz ovjerenih akata (AktService)
func (s *IzvjescaService) SetStanjeObrane(f func(ctx context.Context, sektor, dionica string, t time.Time) (models.StanjeObrane, []models.Akt, error)) {
	s.stanjeObrane = f
}

func NewIzvjescaService(repo *repository.IzvjescaRepository, sections *repository.SectionRepository, stations *repository.StationRepository,
	readings *repository.ReadingRepository, episodes *repository.EpisodeRepository, journals *repository.JournalRepository,
	autori AutoriIzvjesca) *IzvjescaService {
	return &IzvjescaService{repo: repo, sections: sections, stations: stations, readings: readings, episodes: episodes, journals: journals,
		autori: autori}
}

// SmijePisati: dnevno izvješće dionice pišu i predaju samo rukovoditelj
// dionice i zamjenik rukovoditelja dionice (Odluke (30. 10. 2026.), 3)
func (s *IzvjescaService) SmijePisati(perms *models.UserPermissions, sec *models.Section) bool {
	return sec != nil && perms.VodiDionicu(sec.AreaID, sec.Code)
}

// SmijeVidjeti: tko piše na dionici ili je iznad nje, i tko je u sektoru —
// izvješće je interni dokument obrane, a ne tajna unutar sektora
func (s *IzvjescaService) SmijeVidjeti(perms *models.UserPermissions, sec *models.Section) bool {
	if perms == nil || sec == nil {
		return false
	}
	if perms.HasWriteAccess(sec.SectorID, sec.AreaID, sec.Code) || perms.CanAdminister(sec.SectorID, sec.AreaID) ||
		perms.RadiUSektoru(sec.SectorID) || perms.AdminSectors[sec.SectorID] {
		return true
	}
	for code := range perms.AllowedSections {
		if strings.HasPrefix(code, sec.SectorID+".") {
			return true
		}
	}
	return false
}

// Dionica čita dionicu iz registra
func (s *IzvjescaService) Dionica(code string) (*models.Section, error) {
	if s.sections == nil {
		return nil, errors.New("registar dionica nije dostupan")
	}
	return s.sections.GetSectionByCode(code)
}

// stadijDionice je stadij obrane koji sada vrijedi na dionici: iz ovjerenih
// akata, a za dionicu bez akata iz zatečene epizode
func (s *IzvjescaService) stadijDionice(ctx context.Context, sec *models.Section) models.DefensePhase {
	if s.stanjeObrane != nil {
		if st, _, err := s.stanjeObrane(ctx, sec.SectorID, sec.Code, time.Now()); err == nil && st.IzAkata {
			return st.Najvisi()
		}
	}
	if s.episodes != nil {
		if e, err := s.episodes.OpenEpisode(ctx, sec.Code); err == nil && e != nil {
			return e.Phase
		}
	}
	return models.PhaseNormal
}

// Predlozak slaže novo izvješće dionice za dan, popunjeno iz onoga što
// program zna. Kad izvješće za taj dan već postoji, vraća njega.
func (s *IzvjescaService) Predlozak(ctx context.Context, sec *models.Section, dan time.Time) (*models.DnevnoIzvjesce, error) {
	dan = pocetakDana(dan)
	if postojece, err := s.repo.ZaDan(ctx, sec.Code, dan); err != nil {
		return nil, err
	} else if postojece != nil {
		return postojece, nil
	}
	iz := &models.DnevnoIzvjesce{SectionCode: sec.Code, Dan: dan, Stadij: models.PhaseNormal}
	iz.Sadrzaj.Vodotok = vodotokDionice(sec)
	// stadij: proglašena obrana na dionici
	iz.Stadij = s.stadijDionice(ctx, sec)
	// obrana: otvoren dnevnik COP-a sektora
	if s.journals != nil {
		if dnevnici, err := s.journals.ListCOPJournals(ctx, sec.SectorID); err == nil {
			for _, j := range dnevnici {
				if j.EndedAt == nil && !j.Reconstruction {
					iz.JournalID = j.ID
					break
				}
			}
		}
	}
	// vodostaji u 07:00 na mjerodavnim vodomjerima
	iz.Sadrzaj.Vodostaji = s.vodostajiU7(ctx, sec, dan)
	return iz, nil
}

// vodostajiU7 vraća za svaki mjerodavni vodomjer dionice očitanje najbliže
// 07:00 tog dana (između 05:00 i 09:00); bez očitanja ostaje prazan redak
// s imenom letve, da ga rukovoditelj upiše rukom
func (s *IzvjescaService) vodostajiU7(ctx context.Context, sec *models.Section, dan time.Time) []models.VodostajUIzvjescu {
	var out []models.VodostajUIzvjescu
	if s.stations == nil {
		return out
	}
	sedam := dan.Add(7 * time.Hour)
	for _, id := range sec.AllStationIDs() {
		uid, err := uuid.Parse(id)
		if err != nil {
			continue
		}
		st, err := s.stations.GetStationByID(ctx, uid)
		if err != nil || st == nil {
			continue
		}
		v := models.VodostajUIzvjescu{Postaja: st.Name, StationID: id, Sat: "07:00", Jedinica: "cm"}
		if st.Watercourse != "" {
			v.Postaja = st.Watercourse + " – " + st.Name
		}
		if s.readings != nil {
			if rds, err := s.readings.ListForGauges(ctx, []string{id}, nil, sedam.Add(-2*time.Hour).UTC(), sedam.Add(2*time.Hour).UTC()); err == nil {
				var best *models.Reading
				for i := range rds {
					r := &rds[i]
					if r.LevelCm == nil {
						continue
					}
					if best == nil || razmak(r.MeasuredAt, sedam) < razmak(best.MeasuredAt, sedam) {
						best = r
					}
				}
				if best != nil {
					v.Vrijednost = fmt.Sprintf("%+d", *best.LevelCm)
					v.Sat = best.MeasuredAt.In(models.Zagreb).Format("15:04")
					v.Izvor = "očitanja"
				}
			}
		}
		out = append(out, v)
	}
	return out
}

func razmak(a, b time.Time) time.Duration {
	if a.After(b) {
		return a.Sub(b)
	}
	return b.Sub(a)
}

// vodotokDionice slaže naziv vodotoka iz poddionica; više voda odvaja zarezom
func vodotokDionice(sec *models.Section) string {
	var vode []string
	vidjeno := map[string]bool{}
	for _, p := range sec.Parts {
		ime := p.WatercourseName
		if ime == "" {
			ime = p.WatercourseCode
		}
		if ime != "" && !vidjeno[ime] {
			vidjeno[ime] = true
			vode = append(vode, ime)
		}
	}
	return strings.Join(vode, ", ")
}

// Spremi upisuje ili mijenja izvješće. Jedno po dionici i danu — drugog za
// isti dan nema, pa tko smije mijenjati izvješće koje je bilo predano
// odlučuje SmijeMijenjati.
// Spremljeno izvješće je nacrt: izmjena predanog vraća ga u nacrt koji se
// mora ponovo predati, prvo vrijeme predaje ostaje, a pamte se svi koji su
// ga mijenjali. Spremanje bez promjene predano izvješće ostavlja predanim.
func (s *IzvjescaService) Spremi(ctx context.Context, u *models.User, perms *models.UserPermissions, sec *models.Section, iz *models.DnevnoIzvjesce) error {
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	if sec == nil || iz.SectionCode != sec.Code {
		return errors.New("izvješće nema dionicu")
	}
	if !s.SmijePisati(perms, sec) {
		return errors.New("dnevno izvješće dionice piše i predaje rukovoditelj dionice ili njegov zamjenik")
	}
	if iz.Dan.IsZero() {
		return errors.New("izvješće mora imati dan")
	}
	iz.Dan = pocetakDana(iz.Dan)
	if iz.Dan.After(pocetakDana(time.Now().In(models.Zagreb))) {
		return errors.New("izvješće se piše za protekli dan ili za danas, ne unaprijed")
	}
	if iz.Stadij == "" {
		iz.Stadij = models.PhaseNormal
	}
	if iz.Sadrzaj.Tendencija != "" && models.TendencijaNaziv(iz.Sadrzaj.Tendencija) == "" {
		return errors.New("nepoznata tendencija vodostaja")
	}
	postojece, err := s.repo.ZaDan(ctx, sec.Code, iz.Dan)
	if err != nil {
		return err
	}
	if postojece != nil && postojece.ID != iz.ID {
		return fmt.Errorf("izvješće dionice %s za %s već postoji", sec.Code, iz.Dan.Format("2.1.2006."))
	}
	var prije *models.DnevnoIzvjesce // spremljena inačica; nil za novo izvješće
	if iz.ID != "" {
		cur, err := s.repo.Get(ctx, iz.ID)
		if err != nil {
			return err
		}
		// zadani ID mora biti izvješće ove dionice, kao kod predaje i brisanja
		if !cur.IzDionice(sec.Code) {
			return errors.New("izvješće nije pronađeno")
		}
		if !s.SmijeMijenjati(u, perms, sec, cur) {
			return ErrPredanoMijenjaAutor
		}
		zadrziAutora(iz, cur, u)
		prije = cur
	} else {
		iz.IzradioID, iz.Izradio, iz.TragPredaje = u.ID.String(), u.FullName, models.TragPredaje{}
	}
	if iz.Sadrzaj.Vodotok == "" {
		iz.Sadrzaj.Vodotok = vodotokDionice(sec)
	}
	return s.spremiDnevno(ctx, iz, prije, u)
}

// zadrziAutora: izmjena zadržava vrijeme nastanka, autora i trag predaje
// spremljenog izvješća. Nacrt bez autora koji nikad nije predan preuzima
// onaj tko ga mijenja; izvješću koje je bilo predano autor se naknadno ne
// upisuje, a onaj tko ga mijenja pamti se u tragu predaje.
func zadrziAutora(iz, cur *models.DnevnoIzvjesce, u *models.User) {
	iz.CreatedAt, iz.IzradioID, iz.Izradio, iz.TragPredaje = cur.CreatedAt, cur.IzradioID, cur.Izradio, cur.TragPredaje
	if iz.IzradioID == "" && !cur.BiloPredano() {
		iz.IzradioID, iz.Izradio = u.ID.String(), u.FullName
	}
}

// ErrPredanoMijenjaAutor: izvješće koje je bilo predano mijenja onaj tko ga
// je izradio, a drugi rukovoditelj ili zamjenik tek kad on više ne vodi
// dionicu
var ErrPredanoMijenjaAutor = errors.New("izvješće koje je bilo predano mijenja onaj tko ga je izradio; drugi rukovoditelj ili zamjenik dionice može ga mijenjati tek kad on više ne vodi dionicu")

// SmijeMijenjati javlja smije li osoba mijenjati spremljeno izvješće
// dionice: piše ga tko vodi dionicu (SmijePisati), a ono koje je bilo
// predano, i dok čeka ponovnu predaju, onaj tko ga je izradio, ili uprava
// koja ga smije pisati (Odluke (8. 10. 2026.), pitanje 12). Kad autor više
// ne vodi dionicu, mijenja ga svaki rukovoditelj ili zamjenik te dionice
// (Odluke (30. 10. 2026.), 10).
func (s *IzvjescaService) SmijeMijenjati(u *models.User, perms *models.UserPermissions, sec *models.Section, iz *models.DnevnoIzvjesce) bool {
	if u == nil || iz == nil || !s.SmijePisati(perms, sec) {
		return false
	}
	return !iz.BiloPredano() || iz.IzradioID == u.ID.String() || perms.CanAdminister(sec.SectorID, sec.AreaID) ||
		!s.autorVodiDionicu(iz, sec)
}

// autorVodiDionicu javlja vodi li onaj tko je izradio izvješće sada tu
// dionicu, kao rukovoditelj ili zamjenik s važećom dužnošću. Ne vodi je
// autor koji nije upisan, čiji je račun isključen ili obrisan. Račun koji
// se ne može pročitati ili još nije na ovom računalu (stiže razmjenom,
// čeka potvrdu) ne otvara izmjenu tuđeg izvješća: uzima se da autor
// dionicu vodi.
func (s *IzvjescaService) autorVodiDionicu(iz *models.DnevnoIzvjesce, sec *models.Section) bool {
	if s.autori == nil {
		return true
	}
	id, err := uuid.Parse(iz.IzradioID)
	if err != nil {
		return false // izvješće bez upisanog autora
	}
	a, err := s.autori.GetUserByID(id)
	switch {
	case err != nil:
		return true
	case a == nil:
		return !s.racunObrisan(id)
	}
	return a.IsActive && models.NewUserPermissions(*a).VodiDionicu(sec.AreaID, sec.Code)
}

// racunObrisan javlja je li račun autora obrisan; greška znači da nije
func (s *IzvjescaService) racunObrisan(id uuid.UUID) bool {
	obrisan, err := s.autori.RacunObrisan(id)
	return err == nil && obrisan
}

// spremiDnevno upisuje izvješće dionice kao nacrt. Predano izvješće koje
// spremanje ne mijenja ostaje predano i ništa se ne upisuje: u nacrt ga
// vraća samo izmjena.
func (s *IzvjescaService) spremiDnevno(ctx context.Context, iz, prije *models.DnevnoIzvjesce, u *models.User) error {
	if !iz.PripremiSpremanje(prije, u, time.Now().In(models.Zagreb)) {
		return nil
	}
	return s.repo.Save(ctx, iz)
}

// Predaj označava izvješće predanim u podcentar, s vremenom predaje i
// imenom onoga tko predaje; nakon izmjene to je ponovna predaja
func (s *IzvjescaService) Predaj(ctx context.Context, u *models.User, perms *models.UserPermissions, sec *models.Section, id string) error {
	if u == nil || !s.SmijePisati(perms, sec) {
		return errors.New("dnevno izvješće dionice predaje rukovoditelj dionice ili njegov zamjenik")
	}
	iz, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if iz == nil || iz.SectionCode != sec.Code {
		return errors.New("izvješće nije pronađeno")
	}
	if iz.Predano() {
		return nil
	}
	if iz.Sadrzaj.Prazno() {
		return errors.New("prazno izvješće se ne predaje: upišite bar vodostaje i stanje")
	}
	sad := time.Now().In(models.Zagreb)
	iz.PredanoAt = &sad
	iz.ZabiljeziPredaju(sad, u)
	return s.repo.Save(ctx, iz)
}

// SmijeBrisati: nacrt koji nikad nije predan briše tko smije pisati
// izvješće dionice; svako izvješće dionice briše i uprava područja ili
// sektora, a ono koje je bilo predano, i dok čeka ponovnu predaju, samo ona
func (s *IzvjescaService) SmijeBrisati(perms *models.UserPermissions, sec *models.Section, iz *models.DnevnoIzvjesce) bool {
	if sec == nil || iz == nil {
		return false
	}
	return perms.CanAdminister(sec.SectorID, sec.AreaID) || (!iz.BiloPredano() && s.SmijePisati(perms, sec))
}

// Obrisi arhivira izvješće; nacrt briše tko ga smije pisati i uprava, predano
// samo uprava.
// Izvješće koje je bilo predano, a izmjenom vraćeno u nacrt, i dalje briše
// samo uprava: izmjena nije put oko te zaštite.
func (s *IzvjescaService) Obrisi(ctx context.Context, u *models.User, perms *models.UserPermissions, sec *models.Section, id string) error {
	if u == nil || sec == nil {
		return errors.New("brisanje izvješća zahtijeva prijavu")
	}
	iz, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if iz == nil || iz.SectionCode != sec.Code {
		return errors.New("izvješće nije pronađeno")
	}
	if !s.SmijeBrisati(perms, sec, iz) {
		return errors.New(porukaBrisanja(iz))
	}
	return s.repo.Arhiviraj(ctx, iz)
}

// porukaBrisanja kaže tko briše izvješće koje osoba ne smije obrisati
func porukaBrisanja(iz *models.DnevnoIzvjesce) string {
	if iz.BiloPredano() {
		return "izvješće koje je bilo predano briše samo uprava područja ili sektora"
	}
	return "nacrt izvješća briše rukovoditelj dionice ili njegov zamjenik, ili uprava područja ili sektora"
}

func (s *IzvjescaService) Get(ctx context.Context, id string) (*models.DnevnoIzvjesce, error) {
	return s.repo.Get(ctx, id)
}

func (s *IzvjescaService) List(ctx context.Context, journalID, sectionCode, sektor string, dan *time.Time) ([]models.DnevnoIzvjesce, error) {
	return s.repo.List(ctx, journalID, sectionCode, sektor, dan)
}

func (s *IzvjescaService) Broj(ctx context.Context) (int, error) {
	return s.repo.Broj(ctx)
}

// DioniceZaPisanje vraća dionice za koje osoba smije pisati izvješće, redom
// šifre: rukovoditelju dionice i zamjeniku njihove dionice
func (s *IzvjescaService) DioniceZaPisanje(perms *models.UserPermissions) ([]models.Section, error) {
	if s.sections == nil || perms == nil {
		return nil, nil
	}
	sve, err := s.sections.ListSections("", 0, "")
	if err != nil {
		return nil, err
	}
	var out []models.Section
	for i := range sve {
		if s.SmijePisati(perms, &sve[i]) {
			out = append(out, sve[i])
		}
	}
	return out, nil
}
