package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Koliko prijava osoba smije poslati u satu. Prijave idu na sva računala u
// mreži, pa zapeo gumb ili skripta ne smije napuniti svačiju bazu.
const (
	najvisePrijavaUSatu = 20
	prozorPrijava       = time.Hour
	najviseMojihPrijava = 50
	// isto slanje iste osobe u ovom roku je ponovljeno slanje, ne nova prijava
	rokPonovljenog = 10 * time.Minute
)

// Greške koje osoba može sama ispraviti; poruke za nju slaže web
var (
	ErrPrijavaBezOpisa     = errors.New("prijava bez opisa")
	ErrPrijavaPreduga      = errors.New("predugačak opis prijave")
	ErrPrevisePrijava      = errors.New("previše prijava u satu")
	ErrKrivoStanje         = errors.New("nepoznato stanje prijave")
	ErrOdgovorPredug       = errors.New("predugačak odgovor na prijavu")
	ErrPrijavaNePostoji    = errors.New("prijave nema")
	ErrPrijavaBezKorisnika = errors.New("prijava bez korisnika")
)

// UlazPrijaveGreske je ono što stiže iz obrasca i preglednika
type UlazPrijaveGreske struct {
	Vrsta, Opis, Stranica, NazivStranice string
	Uloga, Izdanje                       string
	Sustav, Preglednik, Zaslon           string
	Greske                               []string
}

// PrijavaGreskeService prima prijave grešaka i prijedloge, a administratoru
// daje popis i odgovor
type PrijavaGreskeService struct {
	repo *repository.PrijavaGreskeRepository
	cvor string
	sat  func() time.Time

	// primanje drži provjeru broja i upis zajedno, inače istodobna slanja vide
	// isti stari broj i prođu preko granice
	primanje sync.Mutex
}

// NewPrijavaGreskeService sastavlja servis; cvor je ime ovog računala
func NewPrijavaGreskeService(repo *repository.PrijavaGreskeRepository, cvor string) *PrijavaGreskeService {
	return &PrijavaGreskeService{repo: repo, cvor: cvor, sat: time.Now}
}

// Posalji provjeri, očisti i spremi novu prijavu
func (s *PrijavaGreskeService) Posalji(ctx context.Context, u *models.User, ulaz UlazPrijaveGreske) (*models.PrijavaGreske, error) {
	if u == nil {
		return nil, ErrPrijavaBezKorisnika
	}
	opis := models.OcistiTekst(ulaz.Opis)
	switch {
	case opis == "":
		return nil, ErrPrijavaBezOpisa
	case len([]rune(opis)) > models.NajviseOpisaPrijave:
		return nil, ErrPrijavaPreduga
	}
	vrsta := models.VrstaPrijaveGreska
	if ulaz.Vrsta == models.VrstaPrijavePrijedlog {
		vrsta = models.VrstaPrijavePrijedlog
	}
	p := &models.PrijavaGreske{
		UserID: u.ID.String(), Ime: u.FullName, Uloga: ulaz.Uloga, Vrsta: vrsta, Opis: opis,
		Stranica:      models.OcistiStranicu(ulaz.Stranica),
		NazivStranice: models.OcistiNazivStranice(ulaz.NazivStranice),
		Izdanje:       ulaz.Izdanje, Cvor: s.cvor, Sustav: ulaz.Sustav, Preglednik: ulaz.Preglednik,
		Zaslon: models.OcistiZaslon(ulaz.Zaslon), Greske: models.OcistiGreske(ulaz.Greske),
		Stanje: models.StanjePrijaveNova,
	}
	s.primanje.Lock()
	defer s.primanje.Unlock()
	vec, err := s.repo.IstaNedavna(ctx, p.UserID, p.Vrsta, p.Opis, p.Stranica, s.sat().Add(-rokPonovljenog))
	if err != nil {
		return nil, err
	}
	if vec != nil {
		return vec, nil
	}
	if err := s.provjeriBroj(ctx, p.UserID); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// provjeriBroj odbija prijavu kad je osoba u zadnjih sat vremena već poslala
// najviše što smije
func (s *PrijavaGreskeService) provjeriBroj(ctx context.Context, userID string) error {
	vremena, err := s.repo.ZadnjaVremena(ctx, userID, najvisePrijavaUSatu)
	if err != nil {
		return err
	}
	if len(vremena) == najvisePrijavaUSatu && s.sat().Sub(vremena[najvisePrijavaUSatu-1]) < prozorPrijava {
		return ErrPrevisePrijava
	}
	return nil
}

// Odgovori upisuje stanje i odgovor administratora
func (s *PrijavaGreskeService) Odgovori(ctx context.Context, admin *models.User, id, stanje, odgovor string) (*models.PrijavaGreske, error) {
	if admin == nil {
		return nil, ErrPrijavaBezKorisnika
	}
	if !models.JeStanjePrijave(stanje) {
		return nil, ErrKrivoStanje
	}
	odgovor = models.OcistiTekst(odgovor)
	if len([]rune(odgovor)) > models.NajviseOdgovoraPrijave {
		return nil, ErrOdgovorPredug
	}
	p, err := s.repo.Odgovori(ctx, id, stanje, odgovor, admin.FullName)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrPrijavaNePostoji
	}
	return p, nil
}

// Popis daje prijave za Administraciju; prazno stanje znači sve
func (s *PrijavaGreskeService) Popis(ctx context.Context, stanje string) ([]models.PrijavaGreske, error) {
	return s.repo.Popis(ctx, stanje)
}

// Get čita jednu prijavu
func (s *PrijavaGreskeService) Get(ctx context.Context, id string) (*models.PrijavaGreske, error) {
	return s.repo.Get(ctx, id)
}

// MojePrijave daje prijave jedne osobe, za Profil
func (s *PrijavaGreskeService) MojePrijave(ctx context.Context, userID string) ([]models.PrijavaGreske, error) {
	return s.repo.PoKorisniku(ctx, userID, najviseMojihPrijava)
}

// BrojPoStanju broji prijave po stanju
func (s *PrijavaGreskeService) BrojPoStanju(ctx context.Context) (map[string]int, error) {
	return s.repo.BrojPoStanju(ctx)
}

// RedniBroj je redni broj prijave na ovom računalu
func (s *PrijavaGreskeService) RedniBroj(ctx context.Context, id string) (int, error) {
	return s.repo.RedniBroj(ctx, id)
}
