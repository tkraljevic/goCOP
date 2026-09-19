package service

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"time"

	"gocop/internal/models"
	"gocop/internal/potpis"
	"gocop/internal/repository"
)

// PotpisService vodi elektroničke potpise: izdavatelja ovog čvora, osobne
// ključeve i provjeru potpisanih PDF-ova
type PotpisService struct {
	repo     *repository.PotpisRepository
	users    *UserService
	cvor     string
	kljuc    ed25519.PrivateKey
	ca       *potpis.CA
	lozinka  func(hash, lozinka string) bool // provjera lozinke računa
	orgNaziv func() string
}

// NewPotpisService sastavlja servis; Pokreni izdavatelja učitava ili stvara
func NewPotpisService(repo *repository.PotpisRepository, users *UserService, cvor string, kljucCvora ed25519.PrivateKey, lozinka func(hash, lozinka string) bool) *PotpisService {
	return &PotpisService{repo: repo, users: users, cvor: cvor, kljuc: kljucCvora, lozinka: lozinka, orgNaziv: func() string { return models.Terms().OrgName }}
}

// Pokreni učitava izdavatelja ovog čvora iz knjige, ili ga stvara i objavljuje
func (s *PotpisService) Pokreni(ctx context.Context) error {
	if len(s.kljuc) == 0 {
		return errors.New("potpis: čvor nema ključ")
	}
	if i, err := s.repo.GetIzdavatelj(ctx, s.cvor); err == nil && i != nil {
		ca, err := potpis.IzCert(s.cvor, i.Cert, s.kljuc)
		if err == nil {
			s.ca = ca
			return nil
		}
		log.Printf("potpis: spremljeni izdavatelj ne odgovara ključu čvora (%v); izdaje se novi", err)
	}
	ca, err := potpis.NoviCA(s.cvor, s.orgNaziv(), s.kljuc)
	if err != nil {
		return err
	}
	if err := s.repo.SaveIzdavatelj(ctx, &models.IzdavateljPotpisa{Cvor: s.cvor, Cert: ca.Cert.Raw}); err != nil {
		return err
	}
	s.ca = ca
	return nil
}

// Spreman javlja je li izdavatelj čvora učitan
func (s *PotpisService) Spreman() bool { return s != nil && s.ca != nil }

// Izdavatelj vraća certifikat izdavatelja ovog čvora
func (s *PotpisService) Izdavatelj() *x509.Certificate {
	if s == nil || s.ca == nil {
		return nil
	}
	return s.ca.Cert
}

// Izdavatelji vraća certifikate izdavatelja svih čvorova, za provjeru
func (s *PotpisService) Izdavatelji(ctx context.Context) []*x509.Certificate {
	var out []*x509.Certificate
	svi, _ := s.repo.ListIzdavatelji(ctx)
	for _, i := range svi {
		if c, err := x509.ParseCertificate(i.Cert); err == nil {
			out = append(out, c)
		}
	}
	if s.ca != nil {
		out = append(out, s.ca.Cert)
	}
	return out
}

// Zapis vraća ključ osobe; nil kad ga nema
func (s *PotpisService) Zapis(ctx context.Context, userID string) *models.PotpisniKljuc {
	if s == nil {
		return nil
	}
	k, _ := s.repo.GetKljuc(ctx, userID)
	return k
}

// Ima javlja ima li osoba potpisni ključ
func (s *PotpisService) Ima(ctx context.Context, userID string) bool {
	return s.Zapis(ctx, userID) != nil
}

// Svi vraća sve izdane ključeve, za administraciju
func (s *PotpisService) Svi(ctx context.Context) []models.PotpisniKljuc {
	out, _ := s.repo.ListKljucevi(ctx)
	return out
}

func zapisUPotpis(k *models.PotpisniKljuc) *potpis.Zapis {
	return &potpis.Zapis{Cert: k.Cert, Kljuc: k.Kljuc, Sol: k.Sol}
}

// provjeriLozinku traži da lozinka bude lozinka računa: ključ se zaključava
// njome, pa je ona i ključ za potpis
func (s *PotpisService) provjeriLozinku(u *models.User, lozinka string) error {
	if lozinka == "" {
		return errors.New("upišite lozinku")
	}
	cijeli, err := s.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		return ErrUnauthorized
	}
	if s.lozinka != nil && !s.lozinka(cijeli.PasswordHash, lozinka) {
		return errors.New("lozinka nije točna")
	}
	return nil
}

// Novi stvara osobni ključ i certifikat, zaključan lozinkom računa; stari
// ključ, ako ga ima, prestaje vrijediti za nove potpise
func (s *PotpisService) Novi(ctx context.Context, u *models.User, lozinka string) (*models.PotpisniKljuc, error) {
	if s == nil || s.ca == nil {
		return nil, errors.New("izdavatelj potpisa nije spreman")
	}
	if u == nil {
		return nil, ErrUnauthorized
	}
	if err := s.provjeriLozinku(u, lozinka); err != nil {
		return nil, err
	}
	cijeli, _ := s.users.GetUserByID(u.ID)
	o := potpis.Osoba{UserID: u.ID.String(), Ime: cijeli.FullName}
	if d := najvisaDuznost(cijeli); d != nil {
		o.Funkcija = d.Title
		if d.SectorID != nil {
			o.Sektor = *d.SectorID
		}
	}
	z, err := s.ca.Novi(o, lozinka, time.Now())
	if err != nil {
		return nil, err
	}
	k := &models.PotpisniKljuc{UserID: u.ID.String(), Ime: cijeli.FullName, Cert: z.Cert, Kljuc: z.Kljuc, Sol: z.Sol, Izdao: s.cvor}
	return k, s.repo.SaveKljuc(ctx, k)
}

// Obrisi uklanja ključ osobe; već dani potpisi ostaju provjerljivi jer
// certifikat stoji u svakom potpisanom PDF-u
func (s *PotpisService) Obrisi(ctx context.Context, u *models.User) error {
	if u == nil {
		return ErrUnauthorized
	}
	return s.repo.DeleteKljuc(ctx, u.ID.String())
}

// Potpisnik otključava ključ osobe lozinkom; ErrLozinka kad ne odgovara
func (s *PotpisService) Potpisnik(ctx context.Context, u *models.User, lozinka string) (*potpis.Potpisnik, error) {
	if s == nil || u == nil {
		return nil, ErrUnauthorized
	}
	k := s.Zapis(ctx, u.ID.String())
	if k == nil {
		return nil, errors.New("nemate potpisni ključ; napravite ga u profilu")
	}
	var ca *x509.Certificate
	if i, _ := s.repo.GetIzdavatelj(ctx, k.Izdao); i != nil {
		ca, _ = x509.ParseCertificate(i.Cert)
	}
	p, err := potpis.NoviPotpisnik(zapisUPotpis(k), lozinka, ca)
	if errors.Is(err, potpis.ErrLozinka) {
		return nil, fmt.Errorf("lozinka ne otključava vaš potpisni ključ; ako vam je administrator poništio lozinku, napravite novi ključ u profilu")
	}
	return p, err
}

// Simulirani pravi jednokratni simulirani ključ u ime osobe, za testiranje
// tuđim očima uz uključenu opciju; potpis nosi oznaku SIMULACIJA
func (s *PotpisService) Simulirani(ctx context.Context, u *models.User) (*potpis.Potpisnik, error) {
	if s == nil || s.ca == nil {
		return nil, errors.New("izdavatelj potpisa nije spreman")
	}
	if u == nil {
		return nil, ErrUnauthorized
	}
	cijeli, err := s.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		return nil, ErrUnauthorized
	}
	o := potpis.Osoba{UserID: u.ID.String(), Ime: cijeli.FullName}
	if d := najvisaDuznost(cijeli); d != nil {
		o.Funkcija = d.Title
		if d.SectorID != nil {
			o.Sektor = *d.SectorID
		}
	}
	return s.ca.NoviSimulirani(o, time.Now())
}

// Prekljucaj zaključava ključ novom lozinkom kad osoba mijenja lozinku
// računa; bez ključa nema što raditi
func (s *PotpisService) Prekljucaj(ctx context.Context, userID, stara, nova string) error {
	if s == nil {
		return nil
	}
	k := s.Zapis(ctx, userID)
	if k == nil {
		return nil
	}
	z, err := potpis.Prekljucaj(zapisUPotpis(k), stara, nova)
	if err != nil {
		return err
	}
	k.Kljuc, k.Sol = z.Kljuc, z.Sol
	return s.repo.SaveKljuc(ctx, k)
}

// Provjeri nalazi i provjerava potpise u PDF-u prema izdavateljima svih čvorova
func (s *PotpisService) Provjeri(ctx context.Context, pdf []byte) []potpis.Potpis {
	if s == nil {
		return nil
	}
	return potpis.Provjeri(pdf, s.Izdavatelji(ctx))
}
