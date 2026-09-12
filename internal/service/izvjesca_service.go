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

// IzvjescaService vodi dnevna izvješća rukovoditelja dionica. Piše ga tko
// vodi dionicu — rukovoditelj dionice ili zamjenik, ili tko je iznad njega
// (područje, sektor). Obrazac se popunjava iz onoga što program već zna:
// vodotok iz dionice, vodostaji u 07:00 iz očitanja mjerodavnih vodomjera,
// stadij iz proglašene obrane, obrana iz otvorenog dnevnika COP-a.
type IzvjescaService struct {
	repo     *repository.IzvjescaRepository
	sections *repository.SectionRepository
	stations *repository.StationRepository
	readings *repository.ReadingRepository
	episodes *repository.EpisodeRepository
	journals *repository.JournalRepository
}

func NewIzvjescaService(repo *repository.IzvjescaRepository, sections *repository.SectionRepository, stations *repository.StationRepository,
	readings *repository.ReadingRepository, episodes *repository.EpisodeRepository, journals *repository.JournalRepository) *IzvjescaService {
	return &IzvjescaService{repo: repo, sections: sections, stations: stations, readings: readings, episodes: episodes, journals: journals}
}

// SmijePisati: tko vodi dionicu ili je iznad nje
func (s *IzvjescaService) SmijePisati(perms *models.UserPermissions, sec *models.Section) bool {
	if perms == nil || sec == nil {
		return false
	}
	return perms.HasWriteAccess(sec.SectorID, sec.AreaID, sec.Code) || perms.CanAdminister(sec.SectorID, sec.AreaID)
}

// SmijeVidjeti: tko piše, i tko je u sektoru — izvješće je interni dokument
// obrane, a ne tajna unutar sektora
func (s *IzvjescaService) SmijeVidjeti(perms *models.UserPermissions, sec *models.Section) bool {
	if perms == nil || sec == nil {
		return false
	}
	if s.SmijePisati(perms, sec) || perms.AllowedSectors[sec.SectorID] || perms.AdminSectors[sec.SectorID] {
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
	if s.episodes != nil {
		if e, err := s.episodes.OpenEpisode(ctx, sec.Code); err == nil && e != nil {
			iz.Stadij = e.Phase
		}
	}
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
// isti dan nema, pa predano mijenja samo tko ga je predao, ili uprava.
func (s *IzvjescaService) Spremi(ctx context.Context, u *models.User, perms *models.UserPermissions, sec *models.Section, iz *models.DnevnoIzvjesce) error {
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	if sec == nil || iz.SectionCode != sec.Code {
		return errors.New("izvješće nema dionicu")
	}
	if !s.SmijePisati(perms, sec) {
		return errors.New("dnevno izvješće piše rukovoditelj dionice ili zamjenik, ili uprava područja i sektora")
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
	if iz.ID != "" {
		cur, err := s.repo.Get(ctx, iz.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			return errors.New("izvješće nije pronađeno")
		}
		if cur.Predano() && !perms.CanAdminister(sec.SectorID, sec.AreaID) && cur.IzradioID != u.ID.String() {
			return errors.New("predano izvješće mijenja tko ga je predao, ili uprava")
		}
		iz.CreatedAt, iz.IzradioID, iz.Izradio, iz.PredanoAt = cur.CreatedAt, cur.IzradioID, cur.Izradio, cur.PredanoAt
		if iz.IzradioID == "" {
			iz.IzradioID, iz.Izradio = u.ID.String(), u.FullName
		}
	} else {
		iz.IzradioID, iz.Izradio = u.ID.String(), u.FullName
	}
	if iz.Sadrzaj.Vodotok == "" {
		iz.Sadrzaj.Vodotok = vodotokDionice(sec)
	}
	iz.IzradenoAt = time.Now().In(models.Zagreb)
	return s.repo.Save(ctx, iz)
}

// Predaj označava izvješće predanim u podcentar, s vremenom predaje
func (s *IzvjescaService) Predaj(ctx context.Context, u *models.User, perms *models.UserPermissions, sec *models.Section, id string) error {
	if u == nil || !s.SmijePisati(perms, sec) {
		return errors.New("izvješće predaje tko ga smije pisati")
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
	return s.repo.Save(ctx, iz)
}

// Obrisi arhivira izvješće; nacrt briše tko ga je pisao, predano samo uprava
func (s *IzvjescaService) Obrisi(ctx context.Context, u *models.User, perms *models.UserPermissions, sec *models.Section, id string) error {
	if u == nil || !s.SmijePisati(perms, sec) {
		return errors.New("izvješće briše tko ga smije pisati")
	}
	iz, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if iz == nil || iz.SectionCode != sec.Code {
		return errors.New("izvješće nije pronađeno")
	}
	if iz.Predano() && !perms.CanAdminister(sec.SectorID, sec.AreaID) {
		return errors.New("predano izvješće briše uprava područja ili sektora")
	}
	return s.repo.Arhiviraj(ctx, iz)
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
// šifre — rukovoditelju dionice njegove, upravi područja sve u području,
// upravi sektora sve u sektoru
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
