package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/slike"
)

// PrijavaService vodi prijave i obavijesti s terena: vodočuvar ih sastavlja
// s fotografijama i mjestom, objavi i potpiše; objava ide na njegov dnevni
// list, potpisani PDF ostaje trajno, a slike se brišu nakon roka
type PrijavaService struct {
	repo      *repository.PrijavaRepository
	users     *UserService
	vodocuvar *VodocuvarService
	cvor      string
}

// NewPrijavaService sastavlja servis; dnevnik vodočuvara je obvezan jer je
// upis na list dokaz prijave
func NewPrijavaService(repo *repository.PrijavaRepository, users *UserService, vodocuvar *VodocuvarService, cvor string) *PrijavaService {
	return &PrijavaService{repo: repo, users: users, vodocuvar: vodocuvar, cvor: cvor}
}

// ErrNijeNacrt: objavljena prijava se ne mijenja
var ErrNijeNacrt = errors.New("objavljena prijava se ne mijenja; ispravak je nova prijava")

// probni je list kojim se prava na prijavu provjeravaju kao na dnevniku
// vodočuvara: tko vidi i parafira njegov list, vidi i njegove prijave
func probniList(p *models.PrijavaSTerena) *models.VodocuvarskiList {
	return &models.VodocuvarskiList{UserID: p.UserID, Sektor: p.Sektor, AreaID: p.AreaID}
}

// SmijePisati javlja vodi li osoba vodočuvarski dnevnik, pa i prijave
func (s *PrijavaService) SmijePisati(u *models.User) bool { return VodiDnevnik(u) }

// SmijeVidjeti javlja smije li osoba čitati prijavu
func (s *PrijavaService) SmijeVidjeti(perms *models.UserPermissions, p *models.PrijavaSTerena) bool {
	if perms == nil || p == nil {
		return false
	}
	if perms.User.ID.String() == p.UserID {
		return true
	}
	if p.Status == models.PrijavaNacrt {
		return false
	}
	return s.vodocuvar.SmijeVidjeti(perms, probniList(p))
}

// SmijeArhivirati javlja smije li osoba arhivirati objavljenu prijavu:
// rukovoditelji koji ovjeravaju listove tog vodočuvara
func (s *PrijavaService) SmijeArhivirati(perms *models.UserPermissions, p *models.PrijavaSTerena) bool {
	return perms != nil && p != nil && p.Status == models.PrijavaObjavljena && s.vodocuvar.SmijeOvjeriti(perms, probniList(p))
}

// Nova daje praznu prijavu vodočuvara: sektor i područje iz zaduženja, današnji dan
func (s *PrijavaService) Nova(ctx context.Context, u *models.User) (*models.PrijavaSTerena, error) {
	if u == nil {
		return nil, ErrUnauthorized
	}
	cijeli, err := s.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		return nil, ErrUnauthorized
	}
	probni := zaVodocuvara(cijeli)
	if probni == nil {
		return nil, ErrNijeVodocuvar
	}
	return &models.PrijavaSTerena{UserID: cijeli.ID.String(), Ime: cijeli.FullName, Sektor: probni.Sektor, AreaID: probni.AreaID,
		Vrsta: models.PrijavaObavijest, Datum: time.Now().In(models.Zagreb), Status: models.PrijavaNacrt, Cvor: s.cvor}, nil
}

// UnosPrijave je što vodočuvar upiše u obrazac
type UnosPrijave struct {
	Vrsta, Naslov, Opis          string
	Datum                        time.Time
	VodotokCode, Vodotok         string
	DionicaCode                  string
	ObjektID, Objekt, Stacionaza string
	Latitude, Longitude          *float64
}

// Spremi sprema nacrt: novi ili postojeći; samo vodočuvar koji ga je počeo
func (s *PrijavaService) Spremi(ctx context.Context, u *models.User, id string, unos UnosPrijave) (*models.PrijavaSTerena, error) {
	var p *models.PrijavaSTerena
	if id == "" {
		var err error
		if p, err = s.Nova(ctx, u); err != nil {
			return nil, err
		}
	} else {
		var err error
		if p, err = s.nacrtVlasnika(ctx, u, id); err != nil {
			return nil, err
		}
	}
	vrsta := strings.ToUpper(strings.TrimSpace(unos.Vrsta))
	if models.VrstaPrijaveLabel(vrsta) == vrsta {
		return nil, fmt.Errorf("odaberite vrstu: obavijest, prijava, izvješće ili zahtjev")
	}
	p.Vrsta = vrsta
	p.Naslov = strings.TrimSpace(unos.Naslov)
	p.Opis = strings.TrimSpace(unos.Opis)
	if p.Naslov == "" {
		return nil, fmt.Errorf("upišite naslov")
	}
	if p.Opis == "" {
		return nil, fmt.Errorf("upišite opis: što ste zatekli i gdje")
	}
	if !unos.Datum.IsZero() {
		if unos.Datum.After(time.Now().Add(24 * time.Hour)) {
			return nil, fmt.Errorf("dan događaja ne može biti u budućnosti")
		}
		p.Datum = unos.Datum
	}
	p.VodotokCode, p.Vodotok = strings.TrimSpace(unos.VodotokCode), strings.TrimSpace(unos.Vodotok)
	p.DionicaCode = strings.TrimSpace(unos.DionicaCode)
	p.ObjektID, p.Objekt, p.Stacionaza = strings.TrimSpace(unos.ObjektID), strings.TrimSpace(unos.Objekt), strings.TrimSpace(unos.Stacionaza)
	p.Latitude, p.Longitude = unos.Latitude, unos.Longitude
	if (p.Latitude == nil) != (p.Longitude == nil) {
		return nil, fmt.Errorf("mjesto na karti treba obje koordinate")
	}
	return p, s.repo.Save(ctx, p)
}

// nacrtVlasnika čita nacrt i provjerava da ga uređuje onaj tko ga je počeo
func (s *PrijavaService) nacrtVlasnika(ctx context.Context, u *models.User, id string) (*models.PrijavaSTerena, error) {
	if u == nil {
		return nil, ErrUnauthorized
	}
	p, err := s.repo.Get(ctx, id)
	if err != nil || p == nil {
		return nil, fmt.Errorf("prijava ne postoji")
	}
	if p.UserID != u.ID.String() {
		return nil, ErrUnauthorized
	}
	if p.Status != models.PrijavaNacrt {
		return nil, ErrNijeNacrt
	}
	return p, nil
}

// DodajSliku smanjuje fotografiju i sprema je uz nacrt
func (s *PrijavaService) DodajSliku(ctx context.Context, u *models.User, id, naziv string, podaci []byte) (*models.PrijavaSTerena, error) {
	p, err := s.nacrtVlasnika(ctx, u, id)
	if err != nil {
		return nil, err
	}
	if len(p.Slike) >= models.NajviseSlika {
		return nil, fmt.Errorf("prijava nosi najviše %d fotografija", models.NajviseSlika)
	}
	jpg, w, h, err := slike.Smanji(podaci)
	if err != nil {
		return nil, err
	}
	sl := models.SlikaPrijave{ID: uuid.Must(uuid.NewV7()).String(), Naziv: strings.TrimSpace(naziv), Sirina: w, Visina: h, Bajtova: len(jpg)}
	if err := s.repo.SaveSlika(ctx, sl.ID, p.ID, jpg); err != nil {
		return nil, err
	}
	p.Slike = append(p.Slike, sl)
	return p, s.repo.Save(ctx, p)
}

// ObrisiSliku miče fotografiju s nacrta
func (s *PrijavaService) ObrisiSliku(ctx context.Context, u *models.User, id, slikaID string) (*models.PrijavaSTerena, error) {
	p, err := s.nacrtVlasnika(ctx, u, id)
	if err != nil {
		return nil, err
	}
	out := p.Slike[:0]
	for _, sl := range p.Slike {
		if sl.ID != slikaID {
			out = append(out, sl)
		}
	}
	p.Slike = out
	_ = s.repo.DeleteSlika(ctx, slikaID)
	return p, s.repo.Save(ctx, p)
}

// Slika daje fotografiju onome tko prijavu smije vidjeti; nil kad je
// obrisana nakon roka ili je na drugom čvoru
func (s *PrijavaService) Slika(ctx context.Context, perms *models.UserPermissions, id, slikaID string) ([]byte, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil || p == nil || !s.SmijeVidjeti(perms, p) {
		return nil, ErrUnauthorized
	}
	for _, sl := range p.Slike {
		if sl.ID == slikaID {
			return s.repo.Slika(ctx, slikaID)
		}
	}
	return nil, nil
}

// Slike učitava sve fotografije nacrta koje ovaj čvor još ima, za PDF
func (s *PrijavaService) Slike(ctx context.Context, p *models.PrijavaSTerena) map[string][]byte {
	out := map[string][]byte{}
	for _, sl := range p.Slike {
		if b, err := s.repo.Slika(ctx, sl.ID); err == nil && len(b) > 0 {
			out[sl.ID] = b
		}
	}
	return out
}

// Objavi objavljuje nacrt: dodijeli broj, upiše prijavu na dnevni list
// vodočuvara za dan događaja i spremi potpisani PDF kao izvornik; PDF gradi
// pozivatelj (crta i potpisuje) nad prijavom kakva će biti objavljena.
// Objava i izvornik su jedna transakcija; upis na list slijedi, a ako ne
// uspije vraća se kao upozorenje uz objavljenu prijavu.
func (s *PrijavaService) Objavi(ctx context.Context, u *models.User, id string, izradi func(p *models.PrijavaSTerena) ([]byte, error)) (*models.PrijavaSTerena, string, error) {
	p, err := s.nacrtVlasnika(ctx, u, id)
	if err != nil {
		return nil, "", err
	}
	if p.Naslov == "" || p.Opis == "" {
		return nil, "", fmt.Errorf("prijava bez naslova ili opisa se ne objavljuje")
	}
	cijeli, _ := s.users.GetUserByID(u.ID)
	if cijeli == nil {
		return nil, "", ErrUnauthorized
	}
	dan := p.Datum.In(models.Zagreb)
	if Arhivirana(dan.Year()) {
		return nil, "", fmt.Errorf("knjiga za %d. je arhivirana", dan.Year())
	}
	// list dana: postojeći ili novi, s brojem
	l, err := s.vodocuvar.pripremiZa(ctx, cijeli, dan)
	if err != nil {
		return nil, "", err
	}
	if l.Broj == 0 {
		if l.Broj, err = s.vodocuvar.repo.SljedeciBroj(ctx, cijeli.ID.String(), dan.Year()); err != nil {
			return nil, "", err
		}
	}
	sad := time.Now()
	p.Godina = dan.Year()
	if p.Broj, err = s.repo.SljedeciBroj(ctx, p.Sektor, p.Godina); err != nil {
		return nil, "", err
	}
	p.Status, p.ObjavljenoAt, p.Cvor = models.PrijavaObjavljena, &sad, s.cvor
	p.ListID, p.ListBroj = l.ID, l.Broj
	if p.ListID == "" {
		// list još nije spremljen: spremi ga sad da dobije oznaku
		if err := s.vodocuvar.repo.Save(ctx, l); err != nil {
			return nil, "", err
		}
		p.ListID = l.ID
	}
	pdf, err := izradi(p)
	if err != nil {
		return nil, "", err
	}
	h := sha256.Sum256(pdf)
	if err := s.repo.Objavi(ctx, p, pdf, hex.EncodeToString(h[:])); err != nil {
		return nil, "", err
	}
	l.Prijave = append(l.Prijave, models.PrijavaNaListu{ID: p.ID, Oznaka: p.Oznaka(), Vrsta: p.Vrsta, Naslov: p.Naslov, Kad: sad})
	if err := s.vodocuvar.repo.Save(ctx, l); err != nil {
		return p, "prijava je objavljena, ali upis na dnevni list nije uspio: " + err.Error(), nil
	}
	return p, "", nil
}

// Get čita prijavu ako je osoba smije vidjeti
func (s *PrijavaService) Get(ctx context.Context, perms *models.UserPermissions, id string) (*models.PrijavaSTerena, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil || p == nil {
		return nil, err
	}
	if !s.SmijeVidjeti(perms, p) {
		return nil, ErrUnauthorized
	}
	return p, nil
}

// List vraća prijave koje osoba smije vidjeti
func (s *PrijavaService) List(ctx context.Context, perms *models.UserPermissions, f repository.FiltarPrijava) ([]models.PrijavaSTerena, error) {
	sve, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := sve[:0]
	for _, p := range sve {
		if s.SmijeVidjeti(perms, &p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// Izvornik vraća potpisani PDF objavljene prijave
func (s *PrijavaService) Izvornik(ctx context.Context, perms *models.UserPermissions, id string) (*models.IzvornikLista, error) {
	if _, err := s.Get(ctx, perms, id); err != nil {
		return nil, err
	}
	return s.repo.Izvornik(ctx, id)
}

// Arhiviraj zatvara objavljenu prijavu: rukovoditelj je pregledao i riješio
func (s *PrijavaService) Arhiviraj(ctx context.Context, perms *models.UserPermissions, u *models.User, id string) (*models.PrijavaSTerena, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil || p == nil {
		return nil, fmt.Errorf("prijava ne postoji")
	}
	if u == nil || !s.SmijeArhivirati(perms, p) {
		return nil, ErrUnauthorized
	}
	sad := time.Now()
	p.Status, p.ArhiviraoID, p.Arhivirao, p.ArhiviranoAt = models.PrijavaArhivirana, u.ID.String(), u.FullName, &sad
	return p, s.repo.Save(ctx, p)
}

// Obrisi briše nacrt onoga tko ga je počeo
func (s *PrijavaService) Obrisi(ctx context.Context, u *models.User, id string) error {
	if _, err := s.nacrtVlasnika(ctx, u, id); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

// ObrisiStareSlike briše izvorne fotografije objavljenih prijava starijih
// od zadanog broja dana; PDF ih nosi trajno
func (s *PrijavaService) ObrisiStareSlike(ctx context.Context, dani int) (int64, error) {
	if dani <= 0 {
		dani = models.ZadanoCuvanjeSlikaDana
	}
	return s.repo.ObrisiStareSlike(ctx, time.Now().AddDate(0, 0, -dani))
}
