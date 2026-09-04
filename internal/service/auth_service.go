package service

import (
	"errors"
	"fmt"
	"log"
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
)

type AuthService struct {
	userRepo    *repository.UserRepository
	sessionRepo *repository.SessionRepository
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

// Login autentificira korisnika i stvara novu sesiju s UUIDv7
func (s *AuthService) Login(username, password, ip, userAgent string) (*models.Session, *models.User, error) {
	u, err := s.userRepo.GetUserByUsername(username)
	if err != nil {
		return nil, nil, err
	}
	if u == nil {
		return nil, nil, ErrInvalidCredentials
	}

	if !u.IsActive {
		return nil, nil, ErrAccountInactive
	}

	if !s.CheckPassword(u.PasswordHash, password) {
		return nil, nil, ErrInvalidCredentials
	}

	sessionID, err := uuid.NewV7()
	if err != nil {
		return nil, nil, fmt.Errorf("greška pri generiranju tokena sesije: %w", err)
	}

	session := &models.Session{
		ID:        sessionID,
		UserID:    u.ID,
		IPAddress: ip,
		UserAgent: userAgent,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour), // 24 sata trajanja sesije
	}

	if err := s.sessionRepo.CreateSession(session); err != nil {
		return nil, nil, err
	}

	// Trag prijave ne smije srušiti samu prijavu: račun je ispravan i sesija
	// je stvorena, pa neuspjeh bilježenja ide u zapisnik, a korisnik ulazi.
	now := time.Now().UTC()
	if err := s.userRepo.MarkLogin(u.ID, now); err != nil {
		log.Printf("prijava korisnika %s zabilježena nije: %v", u.Username, err)
	} else {
		u.LastLoginAt = &now
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

// ChangePassword provjerava trenutnu lozinku i postavlja novu lozinku
func (s *AuthService) ChangePassword(userID uuid.UUID, currentPassword, newPassword string) error {
	u, err := s.userRepo.GetUserByID(userID)
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("korisnik nije pronađen")
	}

	if !s.CheckPassword(u.PasswordHash, currentPassword) {
		return errors.New("trenutna lozinka nije točna")
	}

	if len(newPassword) < 6 {
		return errors.New("nova lozinka mora imati najmanje 6 znakova")
	}

	if currentPassword == newPassword {
		return errors.New("nova lozinka mora se razlikovati od trenutne")
	}

	newHash, err := s.HashPassword(newPassword)
	if err != nil {
		return err
	}

	return s.userRepo.ChangePassword(userID, newHash)
}
