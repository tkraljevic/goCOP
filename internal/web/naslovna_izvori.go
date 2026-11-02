package web

import (
	"context"
	"net/http"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// izvoriNaslovne su izvori sporih kartica Naslovne. Svaki se razrješava tek
// pri pozivu, jer poslužitelj usluge dobije nakon što su rute složene.
func (s *Server) izvoriNaslovne() IzvoriNaslovne {
	return IzvoriNaslovne{
		Kisa:     s.podaciKiseSlivova,
		Razmjena: s.razmjenaNaslovne,
		Brojke:   s.brojkePodataka,
		Vrijeme:  s.vrijemeNaslovne,
		Letve: func(ctx context.Context) ([]models.GaugeSummary, error) {
			return s.readingService.Overview(ctx)
		},
		Prije:      s.vodostajPrijeDan,
		Prava:      s.pravaNaslovne,
		Djelatnici: func() ([]models.User, error) { return s.userService.ListUsers("", 0, "", "", "") },
		Nepredani:  s.nepredaniUSektoru,
	}
}

// razmjenaNaslovne je stanje razmjene; false dok čvor nema razmjenu
func (s *Server) razmjenaNaslovne(ctx context.Context) (RazmjenaStanje, bool) {
	if s.razmjena == nil {
		return RazmjenaStanje{}, false
	}
	return s.stanjeRazmjene(ctx), true
}

// vrijemeNaslovne je vrijeme za mjesto osobe; prazno dok ploča vremena
// nije složena
func (s *Server) vrijemeNaslovne(r *http.Request) PlocaVremena {
	if s.vrijemeH == nil {
		return PlocaVremena{}
	}
	return s.vrijemeH.Ploca(r)
}

// najstarijePrijeDan je koliko staro smije biti očitanje s kojim se računa
// promjena u 24 h: letva koja se očitava jednom na dan ima ga dan ranije
const najstarijePrijeDan = 30 * time.Hour

// vodostajPrijeDan je vodostaj letve ili objekta oko 24 h prije zadnjeg
// očitanja; nil kad ga nema ili je prestaro
func (s *Server) vodostajPrijeDan(ctx context.Context, g models.GaugeSummary) *int {
	if g.Latest == nil {
		return nil
	}
	f := repository.ReadingFilter{StationID: g.StationID, StructureID: g.StructureID, To: g.Latest.MeasuredAt.Add(-24 * time.Hour), Limit: 1}
	r, err := s.readingService.List(ctx, f)
	if err != nil || len(r) == 0 || r[0].LevelCm == nil || g.Latest.MeasuredAt.Sub(r[0].MeasuredAt) > najstarijePrijeDan {
		return nil
	}
	return r[0].LevelCm
}

// pravaNaslovne je broj promjena prava koje čekaju potvrdu i gdje se o
// njima odlučuje
func (s *Server) pravaNaslovne(ctx context.Context) (int, string, error) {
	p := s.potvrdePrava()
	nacin, err := repository.NacinOdlukePrava(ctx, p.baza(), p.potpis)
	if err != nil {
		return 0, "", err
	}
	ceka, _, err := repository.PromjenePrava(ctx, p.baza(), p.rec, 0)
	return len(ceka), nacin, err
}

// nepredaniUSektoru su nepredani dnevni listovi vodočuvara sektora; ništa
// dok vodočuvarski dnevnik nije otvoren
func (s *Server) nepredaniUSektoru(ctx context.Context, perms *models.UserPermissions, sektor string) ([]models.VodocuvarskiList, error) {
	if s.vodocuvar == nil {
		return nil, nil
	}
	return s.vodocuvar.NepredaniUSektoru(ctx, perms, sektor)
}
