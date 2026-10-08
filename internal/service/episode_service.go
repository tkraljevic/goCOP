package service

import (
	"context"
	"sort"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
)

// EpisodeService vodi epizode obrane od poplava. Epizode nastaju i mijenjaju
// se samo iz ovjerenih akata o obrani (UskladiIzAkata); ručnog proglašenja,
// podizanja i prekida mimo akta nema, ni u hitnom slučaju (Odluke
// (8. 10. 2026.), pitanje 14).
type EpisodeService struct {
	repo     *repository.EpisodeRepository
	readings *repository.ReadingRepository
	stations *repository.StationRepository
}

func NewEpisodeService(repo *repository.EpisodeRepository, readings *repository.ReadingRepository,
	stations *repository.StationRepository) *EpisodeService {
	return &EpisodeService{repo: repo, readings: readings, stations: stations}
}

func (s *EpisodeService) List(ctx context.Context, sectionCode string) ([]models.DefenseEpisode, error) {
	return s.repo.ListEpisodes(ctx, sectionCode)
}

// ByStation vraća epizode svih dionica koje se vode po jednoj letvi.
func (s *EpisodeService) ByStation(ctx context.Context, stationID string, limit int) ([]models.DefenseEpisode, error) {
	return s.repo.EpisodesByStation(ctx, stationID, limit)
}

// Open vraća epizodu koja na dionici traje, ako je ima.
func (s *EpisodeService) Open(ctx context.Context, sectionCode string) (*models.DefenseEpisode, error) {
	return s.repo.OpenEpisode(ctx, sectionCode)
}

// UskladiIzAkata zapisuje povijest obrane dionice izvedenu iz ovjerenih
// akata: razdoblja dolaze sa stalnim identitetom (models.IDEpizodeIzAkta),
// pa se zatečeno ažurira, a vrhovi i prag iz očitanja ostaju. Izvedena
// epizoda kojoj razdoblja više nema (npr. akt je poništen) briše se; moguci
// su identiteti svih epizoda koje su akti dionice ikad mogli otvoriti, pa
// ručno upisane i računate epizode iz ranijih izdanja ostaju netaknute. Epizoda koja je već
// onakva kakvu akti daju ne upisuje se ponovo, pa izvođenje koje se ponavlja
// (krug čvora, drugi čvor s istim aktima) ne dodaje verzije u knjigu.
func (s *EpisodeService) UskladiIzAkata(ctx context.Context, dionica string, zeljene []models.DefenseEpisode, moguci []uuid.UUID) error {
	zatecene, err := s.repo.ListEpisodes(ctx, dionica)
	if err != nil {
		return err
	}
	poID := map[uuid.UUID]models.DefenseEpisode{}
	for _, e := range zatecene {
		poID[e.ID] = e
	}
	ostaje := map[uuid.UUID]bool{}
	for i := range zeljene {
		ostaje[zeljene[i].ID] = true
		if err := s.upisiIzvedenu(ctx, zeljene[i], poID); err != nil {
			return err
		}
	}
	for _, id := range moguci {
		if z, ima := poID[id]; ima && !ostaje[id] {
			if err := s.repo.DeleteEpisode(ctx, &z); err != nil {
				return err
			}
		}
	}
	return nil
}

// upisiIzvedenu upisuje epizodu izvedenu iz akata: zatečenoj ostaju vrhovi i
// prag iz očitanja, a nova dobiva prag s letve; nepromijenjena se ne upisuje
func (s *EpisodeService) upisiIzvedenu(ctx context.Context, e models.DefenseEpisode, poID map[uuid.UUID]models.DefenseEpisode) error {
	if z, ima := poID[e.ID]; ima {
		if istaIzvedena(z, e) {
			return nil
		}
		e.ThresholdAt, e.PeakCm, e.PeakAt, e.CreatedAt = z.ThresholdAt, z.PeakCm, z.PeakAt, z.CreatedAt
	} else {
		s.dopuniPragLetve(ctx, &e)
	}
	return s.repo.SaveEpisode(ctx, &e)
}

// istaIzvedena javlja je li zatečena epizoda jednaka izvedenoj iz akata u
// svemu što izvođenje određuje (vrhovi i prag iz očitanja se ne gledaju)
func istaIzvedena(z, e models.DefenseEpisode) bool {
	return sazetakIzvedene(z) == sazetakIzvedene(e)
}

// izvedenaEpizoda su polja epizode koja određuju akti; vremena kao trenuci,
// da zona iz baze ne čini razliku
type izvedenaEpizoda struct {
	dionica, letva, stupanj, proglasio, prekinuo, osnova, izvor, biljeska string
	od, do                                                                int64
	traje                                                                 bool
}

func sazetakIzvedene(e models.DefenseEpisode) izvedenaEpizoda {
	s := izvedenaEpizoda{dionica: e.SectionCode, letva: e.StationID, stupanj: string(e.Phase), proglasio: e.DeclaredBy,
		prekinuo: e.EndedBy, osnova: e.Basis, izvor: e.Origin, biljeska: e.Note, od: e.StartedAt.UnixNano(), traje: e.EndedAt == nil}
	if e.EndedAt != nil {
		s.do = e.EndedAt.UnixNano()
	}
	return s
}

// dopuniPragLetve upisuje kad je vodostaj na letvi epizode prešao prag, kad
// je letva poznata
func (s *EpisodeService) dopuniPragLetve(ctx context.Context, e *models.DefenseEpisode) {
	id, err := uuid.Parse(e.StationID)
	if err != nil || s.stations == nil || s.readings == nil {
		return
	}
	if st, _ := s.stations.GetStationByID(ctx, id); st != nil {
		s.dopuniPrag(ctx, e, *st)
	}
}

// dopuniPrag upisuje na epizodu trenutak u kojem je vodostaj prešao prag
// pripremnog stanja, ako ga je već prešao. Otud se poslije vidi je li obrana
// proglašena unaprijed ili tek kad je voda već bila gore.
func (s *EpisodeService) dopuniPrag(ctx context.Context, e *models.DefenseEpisode, st models.Station) {
	if !st.Prep.IsUsable() {
		return
	}
	od := e.StartedAt.Add(-14 * 24 * time.Hour)
	sve, err := s.readings.ListForGauges(ctx, []string{st.ID.String()}, nil, od, time.Now())
	if err != nil {
		return
	}
	var niz []ocitanje
	for _, r := range sve {
		if r.LevelCm != nil {
			niz = append(niz, ocitanje{r.MeasuredAt, *r.LevelCm})
		}
	}
	sort.Slice(niz, func(i, j int) bool { return niz[i].at.Before(niz[j].at) })
	for _, o := range niz {
		f := st.CalculateDefensePhase(o.cm)
		if f != models.PhaseNormal && f != models.PhaseUnknown {
			at := o.at
			e.ThresholdAt = &at
			return
		}
	}
}

// Očitanje jedne letve svedeno na ono što epizoda treba
type ocitanje struct {
	at time.Time
	cm int
}
