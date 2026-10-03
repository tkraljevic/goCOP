package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("neispravno korisničko ime ili lozinka")
	ErrAccountInactive    = errors.New("korisnički račun je deaktiviran")
	ErrSessionExpired     = errors.New("sesija je istekla")
	// ErrKrivaLozinka označava krivu lozinku pri ponovnoj provjeri unutar
	// prijave (promjena lozinke, potpisni ključ): po njoj rukovatelj broji
	// pokušaje, a poruka ostaje ona koju je javilo mjesto provjere
	ErrKrivaLozinka = errors.New("lozinka nije točna")
)

// greskaLozinke je kriva lozinka s porukom mjesta provjere; errors.Is je
// prepoznaje kao ErrKrivaLozinka
type greskaLozinke string

func (e greskaLozinke) Error() string        { return string(e) }
func (e greskaLozinke) Is(target error) bool { return target == ErrKrivaLozinka }

// lazniSazetak je sažetak lozinke koju nitko nema. Prijava nepostojećeg
// imena ga provjerava da odgovor traje jednako kao za postojeće ime: inače
// se iz vremena odgovora vidi tko ima račun.
var lazniSazetak = sync.OnceValue(func() string {
	h, err := bcrypt.GenerateFromPassword([]byte("gocop-nepostojeci-racun"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(h)
})

type AuthService struct {
	userRepo    *repository.UserRepository
	sessionRepo *repository.SessionRepository
	zastita     ZastitaPrijave // drugi korak prijave izvana; nil = nema ga
}

// ZastitaPrijave je ono što prijava i uređivanje računa traže od drugog
// koraka prijave izvana (DrugiKorak); sučelje drži servise neovisnima o
// njegovoj gradnji, a testovima daje zamjenu.
type ZastitaPrijave interface {
	// Opozovi briše osobi zapamćena računala, prijave na čekanju i
	// privremene kodove na ovom čvoru
	Opozovi(ctx context.Context, userID uuid.UUID) error
	// DopustenaAdresa javlja smije li osoba sama upisati tu adresu e-pošte
	DopustenaAdresa(ctx context.Context, adresa string) error
	// JaviPromjenuAdrese javlja na staru adresu da je promijenjena (najbolje
	// što se može, traje do IstekSlanja; zove se iz pozadine); u nosi
	// potvrdu adrese kakva je bila prije promjene
	JaviPromjenuAdrese(ctx context.Context, u *models.User, stara, nova string)
}

// SetZastitaPrijave povezuje drugi korak prijave: promjena, poništavanje i
// administratorska izmjena lozinke tada opozivaju zapamćena računala,
// prijave na čekanju i privremene kodove, a vlastita promjena adrese
// e-pošte prolazi njegova pravila o domeni
func (s *AuthService) SetZastitaPrijave(z ZastitaPrijave) { s.zastita = z }

// opozoviPrijave opoziva drugi korak osobe nakon promjene lozinke
func (s *AuthService) opozoviPrijave(userID uuid.UUID) error {
	if s.zastita == nil {
		return nil
	}
	return s.zastita.Opozovi(context.Background(), userID)
}

func NewAuthService(uRepo *repository.UserRepository, sRepo *repository.SessionRepository) *AuthService {
	return &AuthService{
		userRepo:    uRepo,
		sessionRepo: sRepo,
	}
}

// HashPassword generira bcrypt hash lozinke
func (s *AuthService) HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("greška pri hashiranju: %w", err)
	}
	return string(bytes), nil
}

// CheckPassword provjerava odgovara li lozinka hashu
func (s *AuthService) CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ProvjeriPrijavu provjerava ime i lozinku bez otvaranja sesije. Odgovor ne
// smije otkriti postoji li račun: nepostojeće ime prolazi istu provjeru
// sažetka, a deaktiviran račun se javlja tek nakon točne lozinke.
func (s *AuthService) ProvjeriPrijavu(username, password string) (*models.User, error) {
	u, err := s.userRepo.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}
	if u == nil {
		s.CheckPassword(lazniSazetak(), password)
		return nil, ErrInvalidCredentials
	}
	if !s.CheckPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	if !u.IsActive {
		return nil, ErrAccountInactive
	}
	return u, nil
}

// OtvoriSesiju stvara sesiju za provjerenog korisnika. Token je UUIDv4:
// 122 slučajna bita, dok UUIDv7 nosi vrijeme i brojač pa ih ima samo 62.
func (s *AuthService) OtvoriSesiju(u *models.User, ip, userAgent string) (*models.Session, error) {
	session := &models.Session{
		ID:        uuid.New(),
		UserID:    u.ID,
		IPAddress: ip,
		UserAgent: userAgent,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour), // 24 sata trajanja sesije
	}

	if err := s.sessionRepo.CreateSession(session); err != nil {
		return nil, err
	}

	// Trag prijave ne smije srušiti samu prijavu: račun je ispravan i sesija
	// je stvorena, pa neuspjeh bilježenja ide u zapisnik, a korisnik ulazi.
	now := time.Now().UTC()
	if err := s.userRepo.MarkLogin(u.ID, now); err != nil {
		log.Printf("prijava korisnika %s zabilježena nije: %v", u.Username, err)
	} else {
		u.LastLoginAt = &now
	}
	return session, nil
}

// Login autentificira korisnika i stvara novu sesiju
func (s *AuthService) Login(username, password, ip, userAgent string) (*models.Session, *models.User, error) {
	u, err := s.ProvjeriPrijavu(username, password)
	if err != nil {
		return nil, nil, err
	}
	session, err := s.OtvoriSesiju(u, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}
	return session, u, nil
}

// Logout uklanja sesiju
func (s *AuthService) Logout(sessionID uuid.UUID) error {
	return s.sessionRepo.DeleteSession(sessionID)
}

// AuthenticateSession provjerava sesiju i vraća korisnika i njegove efektivne ovlasti
func (s *AuthService) AuthenticateSession(sessionID uuid.UUID) (*models.User, *models.UserPermissions, error) {
	session, err := s.sessionRepo.GetSession(sessionID)
	if err != nil {
		return nil, nil, err
	}
	if session == nil {
		return nil, nil, ErrSessionExpired
	}

	permissions, err := s.userRepo.GetUserPermissions(session.UserID)
	if err != nil {
		return nil, nil, err
	}

	if !permissions.User.IsActive {
		return nil, nil, ErrAccountInactive
	}

	return &permissions.User, permissions, nil
}

// SessionView je ono što poslužitelj zna o jednom zahtjevu: tko je stvarno
// prijavljen i čijim se očima gleda program.
type SessionView struct {
	User     *models.User            // djelatnik čijim se očima gleda
	Perms    *models.UserPermissions // ovlasti tog djelatnika
	RealUser *models.User            // prijavljeni administrator
	Viewing  bool                    // gleda li se tuđim očima
}

// AuthenticateSessionView vraća sesiju zajedno s pregledom tuđim očima.
//
// Kad administrator gleda tuđim očima, ovlasti su ovlasti tog djelatnika, pa
// se cijeli program ponaša kao kod njega — to je i smisao: prije nego što
// vodočuvar dobije pojednostavljeno sučelje, netko mora vidjeti što on vidi.
// Prijava se pritom ne bilježi, jer se djelatnik nije prijavio.
func (s *AuthService) AuthenticateSessionView(sessionID uuid.UUID) (*SessionView, error) {
	session, err := s.sessionRepo.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrSessionExpired
	}

	perms, err := s.userRepo.GetUserPermissions(session.UserID)
	if err != nil {
		return nil, err
	}
	if !perms.User.IsActive {
		return nil, ErrAccountInactive
	}

	view := &SessionView{User: &perms.User, Perms: perms, RealUser: &perms.User}
	if session.ViewingAs == nil {
		return view, nil
	}

	// Pravo se provjerava pri svakom zahtjevu, ne samo pri ulasku: kome je
	// administratorstvo u međuvremenu oduzeto, tuđi pogled odmah prestaje.
	if !perms.IsGlobalAdmin {
		_ = s.sessionRepo.SetViewingAs(sessionID, nil)
		return view, nil
	}

	targetPerms, err := s.userRepo.GetUserPermissions(*session.ViewingAs)
	if err != nil || targetPerms == nil {
		_ = s.sessionRepo.SetViewingAs(sessionID, nil)
		return view, nil
	}

	view.User, view.Perms, view.Viewing = &targetPerms.User, targetPerms, true
	return view, nil
}

// StartViewingAs postavlja pregled tuđim očima za tekuću sesiju
func (s *AuthService) StartViewingAs(sessionID uuid.UUID, admin *models.UserPermissions, targetID uuid.UUID) error {
	if admin == nil || !admin.IsGlobalAdmin {
		return errors.New("pregled tuđim očima može pokrenuti samo globalni administrator")
	}
	target, err := s.userRepo.GetUserByID(targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return errors.New("djelatnik nije pronađen")
	}
	if target.ID == admin.User.ID {
		return s.sessionRepo.SetViewingAs(sessionID, nil)
	}
	return s.sessionRepo.SetViewingAs(sessionID, &targetID)
}

// PermissionsFor vraća ovlasti djelatnika — za provjere izvan tekućeg pogleda
func (s *AuthService) PermissionsFor(userID uuid.UUID) (*models.UserPermissions, error) {
	return s.userRepo.GetUserPermissions(userID)
}

// StopViewingAs vraća administratora njegovim vlastitim očima
func (s *AuthService) StopViewingAs(sessionID uuid.UUID) error {
	return s.sessionRepo.SetViewingAs(sessionID, nil)
}

// EndAllSessions gasi sve otvorene sesije jedne osobe
func (s *AuthService) EndAllSessions(userID uuid.UUID) error {
	return s.sessionRepo.DeleteSessionsForUser(userID)
}

// ProvjeriLozinku provjerava trenutnu lozinku osobe; kriva lozinka je
// ErrKrivaLozinka
func (s *AuthService) ProvjeriLozinku(userID uuid.UUID, lozinka string) error {
	u, err := s.userRepo.GetUserByID(userID)
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("korisnik nije pronađen")
	}
	if !s.CheckPassword(u.PasswordHash, lozinka) {
		return greskaLozinke("trenutna lozinka nije točna")
	}
	return nil
}

// ProvjeriNovuLozinku provjerava pravila za novu lozinku; rukovatelj ih pita
// prije nego što potpisni ključ prekljuca novom lozinkom
func ProvjeriNovuLozinku(trenutna, nova string) error {
	if len(nova) < 6 {
		return errors.New("nova lozinka mora imati najmanje 6 znakova")
	}
	if trenutna == nova {
		return errors.New("nova lozinka mora se razlikovati od trenutne")
	}
	return nil
}

// ChangePassword provjerava trenutnu lozinku i postavlja novu lozinku. Sve
// druge prijave te osobe se gase — tko je znao staru lozinku, više ne ulazi;
// sesija zadrzi (ona iz koje se lozinka mijenja) ostaje, uuid.Nil gasi sve.
func (s *AuthService) ChangePassword(userID uuid.UUID, currentPassword, newPassword string, zadrzi uuid.UUID) error {
	if err := s.ProvjeriLozinku(userID, currentPassword); err != nil {
		return err
	}

	if err := ProvjeriNovuLozinku(currentPassword, newPassword); err != nil {
		return err
	}

	newHash, err := s.HashPassword(newPassword)
	if err != nil {
		return err
	}

	if err := s.userRepo.ChangePassword(userID, newHash); err != nil {
		return err
	}
	if err := s.sessionRepo.DeleteOtherSessionsForUser(userID, zadrzi); err != nil {
		return fmt.Errorf("lozinka je promijenjena, ali ostale prijave nisu odjavljene: %w", err)
	}
	if err := s.opozoviPrijave(userID); err != nil {
		return fmt.Errorf("lozinka je promijenjena, ali zapamćena računala nisu zaboravljena: %w", err)
	}
	return nil
}
