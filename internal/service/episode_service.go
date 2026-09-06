package service

import (
	"context"
	"fmt"
	"sort"
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
				cur = &models.DefenseEpisode{StartedAt: o.at, Phase: faza, PeakCm: &cm, PeakAt: &at}
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
