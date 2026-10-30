package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
	"unicode/utf8"

	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("neispravno korisničko ime ili lozinka")
	ErrAccountInactive    = errors.New("korisnički račun je deaktiviran")
	// ErrStaraLozinka: lozinka je točna, ali je to stara zajednička početna
	// lozinka, pa se račun ne prijavljuje dok mu administrator ne poništi
	// lozinku (Odluke (30. 10. 2026.))
	ErrStaraLozinka   = errors.New("račun ima staru zajedničku početnu lozinku")
	ErrSessionExpired = errors.New("sesija je istekla")
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
	// zakljucana javlja zaključava li sažetak račun (SetZakljucanaLozinka)
	zakljucana func(sazetak string) bool
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

// CheckPassword provjerava odgovara li lozinka hashu. Stara zajednička
// početna lozinka ne vrijedi ni kad odgovara: ne mijenja lozinku, ne
// otključava potpisni ključ i ne potvrđuje druge radnje uz ponovni upis
// lozinke (Odluke (30. 10. 2026.)).
func (s *AuthService) CheckPassword(hash, password string) bool {
	return password != StaraPocetnaLozinka && sazetakOdgovara(hash, password)
}

// sazetakOdgovara je sama provjera sažetka (bcrypt), bez ikakvog pravila
func sazetakOdgovara(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// StaraPocetnaLozinka je zajednička početna lozinka koju su računi iz
// imenika i račun admin dobivali do izdanja 0.0.46-alfa. Novi računi je ne
// dobivaju (db.SeedInitialData), a račun koji je još ima ne prijavljuje se.
const StaraPocetnaLozinka = "gocop2026"

// ImaStaruLozinku javlja otvara li stara zajednička početna lozinka račun s
// tim sažetkom. Provjera sažetka traje (bcrypt), pa je ne treba zvati za
// svaki zahtjev.
func (s *AuthService) ImaStaruLozinku(sazetak string) bool {
	return sazetakOdgovara(sazetak, StaraPocetnaLozinka)
}

// SetZakljucanaLozinka daje provjeru sažetka koji zaključava račun (stara
// zajednička početna lozinka; zapamćen ishod, inače ImaStaruLozinku). Uz nju
// sesija takvog računa prestaje pri prvom zahtjevu, a ne tek kad je nađe
// pregled pri pokretanju. Postavlja se prije nego što poslužitelj primi
// prvi zahtjev; nil = sesije se ne provjeravaju tako.
func (s *AuthService) SetZakljucanaLozinka(zakljucana func(sazetak string) bool) {
	s.zakljucana = zakljucana
}

// ProvjeriPrijavu provjerava ime i lozinku bez otvaranja sesije. Odgovor ne
// smije otkriti postoji li račun: nepostojeće ime prolazi istu provjeru
// sažetka, a deaktiviran račun se javlja tek nakon točne lozinke. Točna
// stara zajednička početna lozinka vraća račun uz ErrStaraLozinka: prijava
// se odbija s bilo koje adrese, a pozivatelj račun može označiti.
func (s *AuthService) ProvjeriPrijavu(username, password string) (*models.User, error) {
	u, err := s.userRepo.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}
	if u == nil {
		sazetakOdgovara(lazniSazetak(), password)
		return nil, ErrInvalidCredentials
	}
	if !sazetakOdgovara(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	if !u.IsActive {
		return nil, ErrAccountInactive
	}
	if password == StaraPocetnaLozinka {
		return u, ErrStaraLozinka
	}
	return u, nil
}

// OtvoriSesiju stvara sesiju za provjerenog korisnika. Token je UUIDv4:
// 122 slučajna bita, dok UUIDv7 nosi vrijeme i brojač pa ih ima samo 62.
// Sesija pamti otisak sažetka lozinke iz u, onog koji je prijava provjerila:
// lozinka koja u međuvremenu stigne s drugog računala tu sesiju gasi.
func (s *AuthService) OtvoriSesiju(u *models.User, ip, userAgent string) (*models.Session, error) {
	session := &models.Session{
		ID:            uuid.New(),
		UserID:        u.ID,
		IPAddress:     ip,
		UserAgent:     userAgent,
		ExpiresAt:     time.Now().UTC().Add(24 * time.Hour), // 24 sata trajanja sesije
		LozinkaOtisak: repository.OtisakLozinke(u.PasswordHash),
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
	if err := s.provjeriSesiju(session, &permissions.User); err != nil {
		return nil, nil, err
	}

	return &permissions.User, permissions, nil
}

// provjeriSesiju: sesija vrijedi dok je račun uključen, dok mu je lozinka
// ona s kojom je sesija otvorena i dok ga lozinka ne zaključava. Lozinka
// promijenjena ili poništena na drugom računalu stiže razmjenom, a sesije
// ovdje ne zna, pa se otisak gleda pri svakom zahtjevu; sesija stare
// lozinke briše se kao pri odjavi.
func (s *AuthService) provjeriSesiju(session *models.Session, u *models.User) error {
	if !u.IsActive {
		return s.odbaciSesijuIskljucenog(session.ID)
	}
	if session.LozinkaOtisak != repository.OtisakLozinke(u.PasswordHash) || s.zakljucan(u) {
		_ = s.sessionRepo.DeleteSession(session.ID) // sesija ionako više ne vrijedi
		return ErrSessionExpired
	}
	return nil
}

// zakljucan javlja ima li račun lozinku koja ga zaključava, pa mu ni
// otvorena sesija više ne vrijedi
func (s *AuthService) zakljucan(u *models.User) bool {
	return s.zakljucana != nil && s.zakljucana(u.PasswordHash)
}

// odbaciSesijuIskljucenog briše sesiju računa koji je isključen, kao pri
// odjavi: ponovno uključenje računa ne smije vratiti staru prijavu
func (s *AuthService) odbaciSesijuIskljucenog(sessionID uuid.UUID) error {
	_ = s.sessionRepo.DeleteSession(sessionID)
	return ErrAccountInactive
}

// SessionView je ono što poslužitelj zna o jednom zahtjevu: tko je stvarno
// prijavljen i čijim se očima gleda program.
type SessionView struct {
	User     *models.User            // djelatnik čijim se očima gleda
	Perms    *models.UserPermissions // ovlasti tog djelatnika
	RealUser *models.User            // prijavljeni administrator
	Viewing  bool                    // gleda li se tuđim očima
}

// RacunIskljucen javlja gleda li se tuđim očima račun kojemu je prijava
// isključena. Takav se pregled samo čita, i kad je uprava uključila upise
// tuđim očima (Odluke (8. 10. 2026.), pitanje 2): zapis pod imenom osobe
// koja se ne smije prijaviti ne smije nastati. Gledani račun čita se pri
// svakom zahtjevu, pa pregled prelazi na samo čitanje čim se račun isključi.
func (v *SessionView) RacunIskljucen() bool {
	return v.Viewing && v.User != nil && !v.User.IsActive
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
	if err := s.provjeriSesiju(session, &perms.User); err != nil {
		return nil, err
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

// NajkracaLozinka je najmanji broj znakova lozinke računa, jednako za sve
// račune i na svakom mjestu gdje se lozinka postavlja (Odluke
// (30. 10. 2026.), 9)
const NajkracaLozinka = 10

// ErrKratkaLozinka: lozinka ima manje od NajkracaLozinka znakova. Poruka je
// cijela rečenica jer je osoba čita takvu kakva jest (profil, novi račun).
var ErrKratkaLozinka = fmt.Errorf("Lozinka je prekratka: upišite najmanje %d znakova.", NajkracaLozinka)

// ProvjeriDuljinuLozinke je jedina provjera duljine lozinke računa: prva
// prijava, profil, novi račun i nova lozinka na uređivanju djelatnika. Broje
// se znakovi, ne bajtovi.
func ProvjeriDuljinuLozinke(lozinka string) error {
	if utf8.RuneCountInString(lozinka) < NajkracaLozinka {
		return ErrKratkaLozinka
	}
	return nil
}

// ProvjeriNovuLozinku provjerava pravila za novu lozinku; rukovatelj ih pita
// prije nego što potpisni ključ prekljuca novom lozinkom
func ProvjeriNovuLozinku(trenutna, nova string) error {
	if err := ProvjeriDuljinuLozinke(nova); err != nil {
		return err
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
	return s.nakonNoveLozinke(userID, zadrzi, newHash)
}

// nakonNoveLozinke: prijava zadrzi pamti novu lozinku (sazetak) i ostaje,
// ostale prijave te osobe se brišu, a zapamćena računala zaboravljaju.
// Sesije pamte otisak lozinke, pa ostale ne vrijede ni kad se ne daju
// obrisati.
func (s *AuthService) nakonNoveLozinke(userID, zadrzi uuid.UUID, sazetak string) error {
	if err := s.sessionRepo.ZadrziUzNovuLozinku(zadrzi, userID, sazetak); err != nil {
		return fmt.Errorf("lozinka je promijenjena, ali ova prijava nije zadržana; prijavite se ponovno: %w", err)
	}
	if err := s.sessionRepo.DeleteOtherSessionsForUser(userID, zadrzi); err != nil {
		return fmt.Errorf("lozinka je promijenjena, ali ostale prijave nisu odjavljene: %w", err)
	}
	if err := s.opozoviPrijave(userID); err != nil {
		return fmt.Errorf("lozinka je promijenjena, ali zapamćena računala nisu zaboravljena: %w", err)
	}
	return nil
}
