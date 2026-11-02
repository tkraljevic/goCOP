package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
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
		Teren: func(ctx context.Context, perms *models.UserPermissions, u *models.User) (*service.FieldOverview, error) {
			return s.readingService.FieldOverview(ctx, perms, u, 0)
		},
		StanjaSektora: s.stanjaSektoraNaslovne,
		Osobe: func(sektor string, area int, uloga models.Role) ([]models.User, error) {
			return s.userService.ListUsers(sektor, area, string(uloga), "", "")
		},
		Podrucja: func(sektor string) ([]models.Area, error) { return s.userService.ListAreas(sektor) },
		Dezurstva: func(ctx context.Context, dnevnik string) ([]models.Dezurstvo, error) {
			return s.journalService.Dezurstva(ctx, dnevnik)
		},
		Zapisi: func(ctx context.Context, dnevnik string) ([]models.JournalEntry, error) {
			return s.journalService.EntriesForJournal(ctx, dnevnik)
		},
		Listovi:          s.listoviNaslovne,
		Prijave:          s.prijaveNaslovne,
		ZadaciPodrucja:   s.zadaciPodrucjaNaslovne,
		CekajuParafu:     s.cekajuParafuNaslovne,
		AktiNaPotpis:     s.aktiNaPotpisNaslovne,
		Skladista:        s.skladistaNaslovne,
		StanjeSkladista:  s.stanjeSkladistaNaslovne,
		SredstvaNaTerenu: s.sredstvaNaTerenuNaslovne,
		PrometSredstava:  s.prometSredstavaNaslovne,
		PopisiSredstava:  s.popisiSredstavaNaslovne,
		VrsteSredstava:   s.vrsteSredstavaNaslovne,
		DnevniciUsluga:   s.dnevniciUslugaNaslovne,
		ListoviDnevnika:  s.listoviDnevnikaNaslovne,
	}
}

// aktiNaPotpisNaslovne su nacrti akata sektora koje osoba smije ovjeriti
func (s *Server) aktiNaPotpisNaslovne(ctx context.Context, perms *models.UserPermissions, sektor string) ([]models.Akt, error) {
	if s.akti == nil {
		return nil, errors.New("akti još nisu otvoreni")
	}
	return s.akti.NaPotpis(ctx, perms, sektor)
}

// cekajuParafuNaslovne su listovi područja koji čekaju parafu osobe
func (s *Server) cekajuParafuNaslovne(ctx context.Context, perms *models.UserPermissions, area int) ([]models.VodocuvarskiList, error) {
	if s.vodocuvar == nil {
		return nil, errDnevnikZatvoren
	}
	return s.vodocuvar.CekajuParafu(ctx, perms, area)
}

// errDnevnikZatvoren je greška izvora dok vodočuvarski dnevnik ili prijave
// s terena još nisu otvoreni
var errDnevnikZatvoren = errors.New("vodočuvarski dnevnik još nije otvoren")

// listoviNaslovne su dnevni listovi vodočuvara koje osoba smije čitati
func (s *Server) listoviNaslovne(ctx context.Context, perms *models.UserPermissions, f repository.FiltarListova) ([]models.VodocuvarskiList, error) {
	if s.vodocuvar == nil {
		return nil, errDnevnikZatvoren
	}
	return s.vodocuvar.Tudji(ctx, perms, f)
}

// prijaveNaslovne su prijave s terena koje osoba smije vidjeti
func (s *Server) prijaveNaslovne(ctx context.Context, perms *models.UserPermissions, f repository.FiltarPrijava) ([]models.PrijavaSTerena, error) {
	if s.prijave == nil {
		return nil, errDnevnikZatvoren
	}
	return s.prijave.List(ctx, perms, f)
}

// zadaciPodrucjaNaslovne su zadaci vodočuvara područja
func (s *Server) zadaciPodrucjaNaslovne(ctx context.Context, perms *models.UserPermissions, area int, od time.Time) ([]models.Zadatak, map[string]string, error) {
	if s.vodocuvar == nil {
		return nil, nil, errDnevnikZatvoren
	}
	return s.vodocuvar.ZadaciPodrucja(ctx, perms, area, od)
}

// stanjaSektoraNaslovne su stanja obrane dionica sektora po aktima i
// dionice sektora; greška dok akti nisu otvoreni
func (s *Server) stanjaSektoraNaslovne(ctx context.Context, sektor string) (map[string]models.StanjeObrane, []models.Section, error) {
	if s.akti == nil || s.sectionService == nil {
		return nil, nil, errors.New("akti još nisu otvoreni")
	}
	stanja, err := s.akti.StanjaSektora(ctx, sektor, time.Now())
	if err != nil {
		return nil, nil, err
	}
	dionice, err := s.sectionService.ListSections(sektor, 0, "")
	return stanja, dionice, err
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

// errSredstvaZatvorena je greška izvora dok sredstva za obranu nisu otvorena
var errSredstvaZatvorena = errors.New("sredstva još nisu otvorena")

// skladistaNaslovne su aktivna skladišta sektora ili područja
func (s *Server) skladistaNaslovne(ctx context.Context, sektor string, area int) ([]models.Skladiste, error) {
	if s.mtsService == nil {
		return nil, errSredstvaZatvorena
	}
	return s.mtsService.Skladista(ctx, sektor, area, false)
}

// stanjeSkladistaNaslovne je današnje stanje skladišta po vrstama
func (s *Server) stanjeSkladistaNaslovne(ctx context.Context, id string) ([]models.StanjeVrste, error) {
	if s.mtsService == nil {
		return nil, errSredstvaZatvorena
	}
	return s.mtsService.StanjeSkladista(ctx, id, nil)
}

// sredstvaNaTerenuNaslovne su sredstva izdana na teren sektora
func (s *Server) sredstvaNaTerenuNaslovne(ctx context.Context, sektor string) ([]models.Stanje, error) {
	if s.mtsService == nil {
		return nil, errSredstvaZatvorena
	}
	return s.mtsService.NaTerenu(ctx, "", sektor)
}

// prometSredstavaNaslovne je promet po filtru
func (s *Server) prometSredstavaNaslovne(ctx context.Context, f repository.FiltarPrometa) ([]models.Promet, error) {
	if s.mtsService == nil {
		return nil, errSredstvaZatvorena
	}
	return s.mtsService.Promet(ctx, f)
}

// popisiSredstavaNaslovne su godišnji popisi skladišta u godini
func (s *Server) popisiSredstavaNaslovne(ctx context.Context, sektor, skladiste string, godina int) ([]models.Popis, error) {
	if s.mtsService == nil {
		return nil, errSredstvaZatvorena
	}
	return s.mtsService.Popisi(ctx, sektor, skladiste, godina)
}

// vrsteSredstavaNaslovne je katalog sredstava
func (s *Server) vrsteSredstavaNaslovne(ctx context.Context) ([]models.VrstaSredstva, error) {
	if s.mtsService == nil {
		return nil, errSredstvaZatvorena
	}
	return s.mtsService.Vrste(ctx)
}

// errDnevniciZatvoreni je greška izvora dok dnevnici nisu otvoreni
var errDnevniciZatvoreni = errors.New("dnevnici još nisu otvoreni")

// dnevniciUslugaNaslovne su otvoreni dnevnici radova A.02 i A.03 područja
func (s *Server) dnevniciUslugaNaslovne(ctx context.Context, area int) ([]models.Journal, error) {
	if s.journalService == nil {
		return nil, errDnevniciZatvoreni
	}
	return s.journalService.OtvoreniDnevniciOdrzavanja(ctx, area), nil
}

// listoviDnevnikaNaslovne su dnevni listovi dnevnika radova
func (s *Server) listoviDnevnikaNaslovne(ctx context.Context, id string) ([]models.JournalSheet, error) {
	if s.journalService == nil {
		return nil, errDnevniciZatvoreni
	}
	return s.journalService.ListSheets(ctx, id)
}
