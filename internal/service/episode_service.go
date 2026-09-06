package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// EpisodeService vodi epizode obrane od poplava.
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

// Declare otvara epizodu obrane na dionici. Obranu proglašava čovjek, pa se
// bilježi tko, kad i po čemu — vodostaj u tom trenutku ne mora biti preko
// praga, jer se obrana zna proglasiti i po prognozi, uzvodnim vodostajima,
// ledu ili nalogu.
func (s *EpisodeService) Declare(ctx context.Context, perms *models.UserPermissions, userID, sectionCode string,
	st models.Station, at time.Time, phase models.DefensePhase, basis, note string) (*models.DefenseEpisode, error) {
	if perms == nil || !perms.HasWriteAccess("", 0, sectionCode) {
		return nil, ErrUnauthorized
	}
	if !phase.InForce() {
		return nil, fmt.Errorf("odaberi stupanj obrane")
	}
	if at.IsZero() {
		at = time.Now()
	}
	if at.After(time.Now().Add(time.Hour)) {
		return nil, fmt.Errorf("obrana se ne može proglasiti unaprijed")
	}
	otvorena, err := s.repo.OpenEpisode(ctx, sectionCode)
	if err != nil {
		return nil, err
	}
	if otvorena != nil {
		return nil, fmt.Errorf("na dionici %s obrana već traje od %s; podigni stupanj ili je prekini",
			sectionCode, otvorena.StartedAt.Format("2.1.2006."))
	}
	e := models.DefenseEpisode{
		SectionCode: sectionCode,
		StationID:   st.ID.String(),
		StartedAt:   at,
		Phase:       phase,
		DeclaredBy:  userID,
		Basis:       basis,
		Origin:      models.EpisodeFromOperator,
		Note:        note,
	}
	s.dopuniPrag(ctx, &e, st)
	if err := s.repo.SaveEpisode(ctx, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// Raise podiže stupanj obrane na epizodi koja traje. Stupanj se ne spušta —
// epizoda nosi najviši dosegnuti stupanj, a olakšanje se bilježi prekidom.
func (s *EpisodeService) Raise(ctx context.Context, perms *models.UserPermissions, sectionCode string,
	phase models.DefensePhase, note string) error {
	if perms == nil || !perms.HasWriteAccess("", 0, sectionCode) {
		return ErrUnauthorized
	}
	e, err := s.repo.OpenEpisode(ctx, sectionCode)
	if err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("na dionici %s ne traje nijedna obrana", sectionCode)
	}
	if phase.Severity() <= e.Phase.Severity() {
		return fmt.Errorf("obrana je već na stupnju %s", e.Phase.Label())
	}
	e.Phase = phase
	if note != "" {
		e.Note = strings.TrimSpace(e.Note + "\n" + note)
	}
	return s.repo.SaveEpisode(ctx, e)
}

// End prekida obranu na dionici. Prekid je jednako odluka kao i proglašenje,
// pa se bilježi tko ga je donio.
func (s *EpisodeService) End(ctx context.Context, perms *models.UserPermissions, userID, sectionCode string,
	at time.Time, note string) error {
	if perms == nil || !perms.HasWriteAccess("", 0, sectionCode) {
		return ErrUnauthorized
	}
	e, err := s.repo.OpenEpisode(ctx, sectionCode)
	if err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("na dionici %s ne traje nijedna obrana", sectionCode)
	}
	if at.IsZero() {
		at = time.Now()
	}
	if at.Before(e.StartedAt) {
		return fmt.Errorf("obrana se ne može prekinuti prije nego što je proglašena")
	}
	e.EndedAt = &at
	e.EndedBy = userID
	if note != "" {
		e.Note = strings.TrimSpace(e.Note + "\n" + note)
	}
	return s.repo.SaveEpisode(ctx, e)
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

// Rebuild iznova računa epizode dionice iz niza očitanja mjerodavne letve.
// Epizoda počinje kad vodostaj prijeđe pripremno stanje i traje dok se ne
// vrati ispod njega; nosi najviši dosegnuti stupanj i vrh vala s vremenom.
//
// Briše samo epizode utvrđene računom — ono što je operater upisao rukom ostaje,
// jer je to njegovo očitovanje o obrani, a ne izvod iz brojeva.
func (s *EpisodeService) Rebuild(ctx context.Context, perms *models.UserPermissions, sectionCode string, st models.Station) (int, error) {
	if perms == nil || !perms.CanAdminister("", 0) && !perms.IsGlobalAdmin {
		return 0, ErrUnauthorized
	}
	if !st.Prep.IsUsable() {
		return 0, fmt.Errorf("letva %s nema zapisan prag pripremnog stanja, pa se epizode ne mogu izvesti", st.Name)
	}
	sve, err := s.readings.ListForGauges(ctx, []string{st.ID.String()}, nil, time.Time{}, time.Time{})
	if err != nil {
		return 0, err
	}
	var niz []ocitanje
	for _, r := range sve {
		if r.LevelCm != nil {
			niz = append(niz, ocitanje{r.MeasuredAt, *r.LevelCm})
		}
	}
	sort.Slice(niz, func(i, j int) bool { return niz[i].at.Before(niz[j].at) })

	if _, err := s.repo.DeleteEpisodesFrom(ctx, sectionCode, models.EpisodeFromReadings); err != nil {
		return 0, err
	}
	upisano := 0
	for _, e := range izracunaj(niz, st) {
		e.SectionCode = sectionCode
		e.StationID = st.ID.String()
		e.Origin = models.EpisodeFromReadings
		if err := s.repo.SaveEpisode(ctx, &e); err != nil {
			return upisano, err
		}
		upisano++
	}
	return upisano, nil
}

// izracunaj dijeli niz očitanja na epizode. Izdvojeno iz Rebuild da se pravilo
// može provjeriti bez baze.
func izracunaj(niz []ocitanje, st models.Station) []models.DefenseEpisode {
	var out []models.DefenseEpisode
	var cur *models.DefenseEpisode
	for _, o := range niz {
		faza := st.CalculateDefensePhase(o.cm)
		if faza != models.PhaseNormal && faza != models.PhaseUnknown {
			if cur == nil {
				cm, at := o.cm, o.at
				// Epizoda utvrđena računom počinje upravo prelaskom praga, pa
				// su početak i prelazak praga isti trenutak.
				prag := o.at
				cur = &models.DefenseEpisode{StartedAt: o.at, Phase: faza, PeakCm: &cm, PeakAt: &at,
					ThresholdAt: &prag, Basis: models.BasisThreshold}
			} else if o.cm > *cur.PeakCm {
				cm, at := o.cm, o.at
				cur.PeakCm, cur.PeakAt = &cm, &at
				cur.Phase = faza
			}
			kraj := o.at
			cur.EndedAt = &kraj
			continue
		}
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}
