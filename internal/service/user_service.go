package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
)

var (
	ErrUnauthorized    = errors.New("nemate ovlasti za ovu radnju")
	ErrUsernameExists  = errors.New("korisničko ime već postoji")
	ErrUserNotFound    = errors.New("korisnik nije pronađen")
	ErrInvalidUserData = errors.New("neispravni podaci korisnika")
)

type UserService struct {
	userRepo *repository.UserRepository
	auth     *AuthService
	sse      *SSEBroker
	// ukloniKljuc uklanja osobni potpisni ključ (SetUklanjanjeKljuca)
	ukloniKljuc func(ctx context.Context, userID string) error
	// brisiSanducic briše spremljenu lozinku sandučića e-pošte (SetBrisanjeSanducica)
	brisiSanducic func(ctx context.Context, userID string) (bool, error)
	// prestanakObrane je prestanak redovne i izvanredne obrane na dosegu
	// privremenog imenovanja, s aktom koji ju je ukinuo, iz ovjerenih akata
	// (SetPrestanakObrane); nil dok traje ili je nema
	prestanakObrane func(d models.Duty) *models.PrestanakObrane
}

// SetPrestanakObrane povezuje prestanak redovne i izvanredne obrane iz akata
// (AktService.PrestanakObraneDuznosti): privremeno imenovanje s tim istekom vrijedi
// dok na njegovim dionicama, odnosno u branjenom području, obrana traje
func (s *UserService) SetPrestanakObrane(f func(d models.Duty) *models.PrestanakObrane) {
	s.prestanakObrane = f
}

// SetUklanjanjeKljuca povezuje uklanjanje osobnog potpisnog ključa. Kad
// lozinku osobe postavi netko drugi (poništenje ili administrator u
// obrascu), ključ zaključan starom lozinkom nova ne otvara, a obavezna
// promjena lozinke zapela bi na prekljucavanju ključa; zato se uklanja, a
// osoba na profilu napravi novi. Već dani potpisi ostaju provjerljivi.
func (s *UserService) SetUklanjanjeKljuca(f func(ctx context.Context, userID string) error) {
	s.ukloniKljuc = f
}

// SetBrisanjeSanducica povezuje brisanje spremljene lozinke sandučića
// e-pošte osobe na ovom čvoru (AktService.ZaboraviSanducic). Tko osobi
// postavi lozinku (poništenje ili administrator u obrascu), prijavljuje se
// njome kao ta osoba; lozinka računa domene koju je osoba spremila
// otvarala bi mu i njezinu poštu na poslužitelju tvrtke. Zato se briše, a
// osoba je upiše ponovno. Nova lozinka računa razmjenom stiže i na druge
// čvorove: ondje spremljenu lozinku sandučića gasi otisak lozinke računa
// koji stoji uz nju.
func (s *UserService) SetBrisanjeSanducica(f func(ctx context.Context, userID string) (bool, error)) {
	s.brisiSanducic = f
}

// zaboraviSanducic briše spremljenu lozinku sandučića osobe kojoj je
// lozinku računa postavio drugi. Greška se samo bilježi: lozinka je već
// postavljena, a otisak lozinke računa ionako više ne otključava sandučić.
func (s *UserService) zaboraviSanducic(u *models.User) {
	if s.brisiSanducic == nil || u == nil {
		return
	}
	obrisan, err := s.brisiSanducic(context.Background(), u.ID.String())
	if err != nil {
		log.Printf("e-pošta: spremljena lozinka sandučića računa %s nije obrisana nakon postavljanja lozinke: %v (otisak lozinke računa je svejedno više ne otključava)", u.Username, err)
		return
	}
	if obrisan {
		log.Printf("e-pošta: spremljena lozinka sandučića računa %s obrisana je jer je lozinku računa postavio drugi", u.Username)
	}
}

// ukloniPotpisniKljuc uklanja ključ osobe kojoj je lozinku postavio drugi
func (s *UserService) ukloniPotpisniKljuc(userID uuid.UUID) error {
	if s.ukloniKljuc == nil {
		return nil
	}
	return s.ukloniKljuc(context.Background(), userID.String())
}

func NewUserService(uRepo *repository.UserRepository, auth *AuthService, sse *SSEBroker) *UserService {
	return &UserService{
		userRepo: uRepo,
		auth:     auth,
		sse:      sse,
	}
}

type CreateUserRequest struct {
	Username      string
	Password      string
	FullName      string
	Title         string
	IsGlobalAdmin bool
	OrgType       models.OrgType
	OrgName       string
	Phone         string
	MobilePhone   string
	ShortPhone    string
	ShortMobile   string
	Email         string
	// PotvrdaAdrese: globalni administrator potvrđuje da adresa pripada
	// osobi, pa PIN za prijavu izvana smije ići na nju i izvan dopuštene
	// domene (potvrda_adrese.go); drugima je to ErrPotvrdaAdrese
	PotvrdaAdrese bool
	// TudjimOcima: zahtjev je poslan dok se gleda tuđim očima; potvrda
	// adrese tada se odbija (ErrTudjimOcima)
	TudjimOcima bool
	// Inicijalna funkcija / zaduženje
	DutyTitle    string
	Role         models.Role
	ScopeType    models.ScopeType
	SectorID     *string
	AreaID       *int
	SectionCodes string
}

// areaSectors vraća pretragu sektora po području iz registra
func (s *UserService) areaSectors() areaSector {
	return sektoriPodrucja(s.userRepo.ListAreas)
}

// dionice daje područje i sektor dionice iz registra; kad se registar ne
// da pročitati, nijedna dionica nije poznata (dodjela s dionicama se odbija)
func (s *UserService) dionice() dionicaPodrucja {
	m, _ := s.userRepo.PodrucjaDionica()
	return func(code string) (int, string, bool) {
		d, ok := m[code]
		return d.AreaID, d.SectorID, ok
	}
}

// CreateUser stvara novog korisnika i dodjeljuje mu početnu funkciju
func (s *UserService) CreateUser(actor *models.UserPermissions, req CreateUserRequest) (*models.User, error) {
	if actorRank(actor) == 0 {
		return nil, ErrUnauthorized
	}
	if req.IsGlobalAdmin && !stalnaUpravaOrganizacije(actor) {
		return nil, fmt.Errorf("%w: globalnog administratora postavlja samo stalna uprava organizacije", ErrUnauthorized)
	}
	if req.PotvrdaAdrese {
		if err := dopustenaPotvrda(actor, uuid.Nil, req.TudjimOcima); err != nil {
			return nil, err
		}
	}
	sectors := s.areaSectors()
	if req.Role != "" {
		scope, sectorID, areaID, err := normalizeScope(req.Role, req.SectorID, req.AreaID, req.SectionCodes, sectors, s.dionice())
		if err != nil {
			return nil, err
		}
		req.ScopeType, req.SectorID, req.AreaID = scope, sectorID, areaID
		if err := mayAssign(actor, req.Role, req.SectorID, req.AreaID, sectors); err != nil {
			return nil, err
		}
	} else if !actor.IsGlobalAdmin {
		return nil, fmt.Errorf("%w: račun bez dužnosti otvara samo uprava organizacije", ErrUnauthorized)
	}

	req.Username = strings.TrimSpace(req.Username)
	req.FullName = strings.TrimSpace(req.FullName)
	if req.Username == "" || req.FullName == "" || req.Password == "" {
		return nil, fmt.Errorf("%w: korisničko ime, ime i lozinka su obavezni", ErrInvalidUserData)
	}

	existing, err := s.userRepo.GetUserByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrUsernameExists
	}
	if err := s.provjeriZauzetuAdresu(req.Email, uuid.Nil, false); err != nil {
		return nil, err
	}

	pwHash, err := s.auth.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	// Identifikator ostaje prazan: repozitorij mu u istoj transakciji
	// dodijeli stalni iz korisničkog imena (ili nasumični, kad je stalni
	// već nečiji), pa se korisnik poslije nikad ne prekodira.
	user := &models.User{
		Username:      req.Username,
		PasswordHash:  pwHash,
		FullName:      req.FullName,
		Title:         req.Title,
		IsGlobalAdmin: req.IsGlobalAdmin,
		OrgType:       req.OrgType,
		OrgName:       req.OrgName,
		Phone:         req.Phone,
		MobilePhone:   req.MobilePhone,
		ShortPhone:    req.ShortPhone,
		ShortMobile:   req.ShortMobile,
		Email:         req.Email,
		IsActive:      true,
		// Lozinku je odabrao administrator i zna je: osoba je pri prvoj
		// prijavi mora zamijeniti svojom, kao i nakon poništavanja
		MustChangePassword: true,
	}
	var zapisPotvrde string
	if req.PotvrdaAdrese {
		if zapisPotvrde, err = potvrdiAdresu(actor, user, true, time.Now()); err != nil {
			return nil, err
		}
	}

	var initialDuty *models.Duty
	if req.Role != "" {
		dutyTitle := req.DutyTitle
		if dutyTitle == "" {
			dutyTitle = req.Role.Label()
		}
		dutyID, _ := uuid.NewV7()
		initialDuty = &models.Duty{
			ID:           dutyID,
			Title:        dutyTitle,
			Role:         req.Role,
			ScopeType:    req.ScopeType,
			SectorID:     req.SectorID,
			AreaID:       req.AreaID,
			SectionCodes: req.SectionCodes,
			IsPrimary:    true,
			IsTemporary:  false,
			IsActive:     true,
		}
		ograniciRok(actor, req.Role, req.SectorID, req.AreaID, req.SectionCodes, sectors, privremenost{}, nil).upisi(initialDuty)
		s.istekDuznosti(initialDuty, time.Now())
	}

	if err := s.userRepo.CreateUser(user, initialDuty); err != nil {
		return nil, err
	}
	if zapisPotvrde != "" {
		log.Print(zapisPotvrde)
	}

	s.sse.Broadcast("users_updated", fmt.Sprintf("Kreiran novi djelatnik: %s", user.FullName), user.ID.String())
	return user, nil
}

type UpdateUserRequest struct {
	ID            uuid.UUID
	Username      string
	Password      string
	FullName      string
	Title         string
	IsGlobalAdmin bool
	OrgType       models.OrgType
	OrgName       string
	Phone         string
	MobilePhone   string
	ShortPhone    string
	ShortMobile   string
	Email         string
	IsActive      bool
	// TrenutnaLozinka traži se kad osoba sama sebi mijenja adresu e-pošte:
	// na tu adresu ide PIN za prijavu izvana
	TrenutnaLozinka string
	// Izvana: zahtjev je došao izvana (rukovatelj: service.IzvanaAdresa);
	// vlastita adresa e-pošte tada se ne mijenja
	Izvana bool
	// PotvrdaAdrese je okvir „adresa je provjerena” s obrasca djelatnika
	// (potvrda_adrese.go): nil ne dira potvrdu, true potvrđuje upisanu
	// adresu, false briše potvrdu. Smije samo globalni administrator na
	// tuđem računu; drugima je to ErrPotvrdaAdrese.
	PotvrdaAdrese *bool
	// TudjimOcima: zahtjev je poslan dok se gleda tuđim očima; potvrda
	// adrese tada se ne daje ni ne uklanja (ErrTudjimOcima)
	TudjimOcima bool
	// izAdresara: kontakt upisan iz adresara tvrtke (PrimijeniKontakt), ne
	// rukom; pravila vlastite promjene adrese tada ne vrijede, a potvrda
	// adrese se ne postavlja
	izAdresara bool
}

// istaAdresa javlja jesu li dvije adrese e-pošte iste (bez obzira na velika
// slova i razmake)
func istaAdresa(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// adresaZauzeta: adresu e-pošte koju upisuje uprava već ima drugi aktivni
// račun. errors.Is je ErrAdresaZauzeta, ali poruka je za onoga tko upisuje
// tuđu adresu ili uključuje račun.
type adresaZauzeta struct{ ukljucenje bool }

func (e adresaZauzeta) Error() string {
	if e.ukljucenje {
		return "račun se ne uključuje: njegovu adresu e-pošte već ima drugi aktivni račun, a dvije osobe ne dijele adresu (na nju ide PIN za prijavu izvana); najprije promijenite adresu jednom od njih"
	}
	return "tu adresu e-pošte već ima drugi aktivni račun; dvije osobe ne dijele adresu, jer na nju ide PIN za prijavu izvana"
}

func (adresaZauzeta) Is(cilj error) bool { return cilj == ErrAdresaZauzeta }

// provjeriZauzetuAdresu odbija adresu e-pošte koju već ima drugi aktivni
// račun (bez obzira na velika slova i razmake): zajednička adresa gasi PIN
// za prijavu izvana objema osobama (ErrZajednickaAdresa), pa bi upisom tuđe
// adrese uprava ugasila PIN i onome tko nije u njezinu dosegu. Vrijedi za
// sve, i za globalnog administratora; prazna adresa je dopuštena.
func (s *UserService) provjeriZauzetuAdresu(adresa string, osim uuid.UUID, ukljucenje bool) error {
	if strings.TrimSpace(adresa) == "" {
		return nil
	}
	n, err := s.userRepo.AktivnihSAdresom(adresa, osim)
	if err != nil {
		return err
	}
	if n > 0 {
		return adresaZauzeta{ukljucenje: ukljucenje}
	}
	return nil
}

// provjeriKorisnickoIme odbija novo korisničko ime koje već ima drugi
// račun, bez obzira na velika i mala slova: prijava ime traži tako, pa bi
// preimenovani račun zaključao prijavu onome čije je ime bilo prvo
func (s *UserService) provjeriKorisnickoIme(ime string, osim uuid.UUID) error {
	if ime == "" {
		return fmt.Errorf("%w: korisničko ime je obavezno", ErrInvalidUserData)
	}
	racuni, err := s.userRepo.RacuniPoImenu(ime)
	if err != nil {
		return err
	}
	for _, r := range racuni {
		if r.ID != osim {
			return ErrUsernameExists
		}
	}
	return nil
}

// provjeriVlastituAdresu provjerava pravila kad osoba sama sebi mijenja
// adresu e-pošte: ne dok mora promijeniti lozinku, ne izvana, uz trenutnu
// lozinku i samo na dopuštenu domenu (prazna adresa smije: PIN tada ne ide
// nikamo). Administratorske izmjene tuđih adresa ostaju kakve su bile.
func (s *UserService) provjeriVlastituAdresu(target *models.User, req UpdateUserRequest) error {
	if target.MustChangePassword {
		return ErrAdresaPrijeLozinke
	}
	if req.Izvana {
		return ErrPromjenaAdreseIzvana
	}
	if req.TrenutnaLozinka == "" {
		return greskaLozinke("za promjenu adrese e-pošte upišite trenutnu lozinku")
	}
	if !s.auth.CheckPassword(target.PasswordHash, req.TrenutnaLozinka) {
		return greskaLozinke("trenutna lozinka nije točna")
	}
	if strings.TrimSpace(req.Email) != "" && s.auth.zastita != nil {
		if err := s.auth.zastita.DopustenaAdresa(context.Background(), req.Email); err != nil {
			// adresu izvan domene sebi ne upisuje ni osoba s potvrđenom
			// adresom: potvrda vrijedi za adresu koju je provjerio administrator
			var izvan AdresaIzvanDomene
			if errors.As(err, &izvan) {
				izvan.Vlastita = true
				return izvan
			}
			return err
		}
	}
	if strings.TrimSpace(req.Email) != "" {
		// tuđa adresa bi toj osobi ugasila PIN (zajednička adresa)
		n, err := s.userRepo.AktivnihSAdresom(req.Email, target.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			log.Printf("prijava izvana: %s je pokušao upisati adresu e-pošte koju već ima drugi djelatnik", target.Username)
			return ErrAdresaZauzeta
		}
	}
	return nil
}

// zabranjenoNaSebi odbija izmjenu vlastitog računa koja mijenja korisničko
// ime ili uključenost računa: to osobi mijenja uprava. Nepromijenjene
// vrijednosti iz obrasca ne smetaju.
func zabranjenoNaSebi(target *models.User, req UpdateUserRequest) error {
	if strings.TrimSpace(req.Username) != target.Username {
		return fmt.Errorf("%w: svoje korisničko ime ne mijenjate sami, nego uprava", ErrUnauthorized)
	}
	if req.IsActive != target.IsActive {
		return fmt.Errorf("%w: svoj račun ne uključujete ni isključujete sami", ErrUnauthorized)
	}
	return nil
}

// UpdateUser ažurira matične podatke korisnika. Vlastitu adresu e-pošte
// osoba mijenja samo uz pravila provjeriVlastituAdresu (kriva ili prazna
// trenutna lozinka: errors.Is ErrKrivaLozinka), a stara adresa dobije
// obavijest; time se briše i potvrda adrese od administratora. Potvrdu
// postavlja i uklanja samo globalni administrator na tuđem računu, ne
// tuđim očima (PotvrdaAdrese); kad netko drugi promijeni adresu, potvrda
// ostaje zapisana, ali više ne vrijedi. Nova lozinka (administratorski obrazac)
// opoziva zapamćena računala, prijave na čekanju i privremene kodove osobe
// na ovom čvoru, briše spremljenu lozinku sandučića i, kao poništenje,
// traži zamjenu pri prvoj prijavi. Adresu koju već ima drugi aktivni račun
// ne upisuje nitko (ni pri uključenju računa), a novo korisničko ime ne
// smije biti tuđe ni drugim slovima. Izmjena koju actor ne smije
// (zastavica globalnog administratora bez stalne uprave organizacije, na
// vlastitom računu korisničko ime i uključenost) odbija se porukom, a ne
// zanemaruje.
func (s *UserService) UpdateUser(actor *models.UserPermissions, req UpdateUserRequest) (*models.User, error) {
	target, err := s.userRepo.GetUserByID(req.ID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrUserNotFound
	}
	if req.PotvrdaAdrese != nil {
		if err := dopustenaPotvrda(actor, target.ID, req.TudjimOcima); err != nil {
			return nil, err
		}
	}

	staraAdresa := target.Email
	staraPotvrda := "" // potvrđena adresa koja je dosad vrijedila
	if target.PotvrdaAdreseVrijedi() {
		staraPotvrda = target.PINAdresaPotvrdena
	}
	vlastitaAdresa := actor != nil && actor.User.ID == target.ID && !req.izAdresara && !istaAdresa(req.Email, target.Email)
	if vlastitaAdresa {
		if err := s.provjeriVlastituAdresu(target, req); err != nil {
			return nil, err
		}
	}

	if !actor.IsGlobalAdmin {
		// Tko nije uprava organizacije, uređuje svoj profil ili račune u
		// svom dosegu čije su sve dužnosti na njegovoj razini ili niže
		if actor.User.ID != target.ID {
			if err := mayManage(actor, target, s.areaSectors()); err != nil {
				return nil, err
			}
			// lozinka upisana u obrascu je poništenje: ista pravila
			if req.Password != "" {
				if err := smijePonistiti(actor, target, s.areaSectors()); err != nil {
					return nil, err
				}
			}
			// zastavicu ne mijenja: odbija je provjera stalne uprave niže
		} else {
			// Korisnik uređuje SAM SVOJ profil (može mijenjati ime, titulu,
			// telefone, email, lozinku); korisničko ime, uključenost računa
			// i zastavicu ne mijenja, a zahtjev koji ih mijenja odbija se
			if err := zabranjenoNaSebi(target, req); err != nil {
				return nil, err
			}
			if req.OrgType == "" {
				req.OrgType = target.OrgType
			}
			if req.OrgName == "" {
				req.OrgName = target.OrgName
			}
		}
	}

	if ime := strings.TrimSpace(req.Username); ime != target.Username {
		if err := s.provjeriKorisnickoIme(ime, target.ID); err != nil {
			return nil, err
		}
	}
	// Tuđa adresa (rukom ili iz adresara) i uključenje računa: adresu ne
	// smije imati drugi aktivni račun. Vlastitu je provjerio provjeriVlastituAdresu.
	ukljucenje := !target.IsActive && req.IsActive
	if !vlastitaAdresa && (ukljucenje || !istaAdresa(req.Email, target.Email)) {
		if err := s.provjeriZauzetuAdresu(req.Email, target.ID, ukljucenje && istaAdresa(req.Email, target.Email)); err != nil {
			return nil, err
		}
	}

	target.Username = strings.TrimSpace(req.Username)
	target.FullName = strings.TrimSpace(req.FullName)
	target.Title = req.Title
	// zastavicu daje i skida samo stalna uprava organizacije
	if req.IsGlobalAdmin != target.IsGlobalAdmin && !stalnaUpravaOrganizacije(actor) {
		return nil, fmt.Errorf("%w: globalnog administratora postavlja i skida samo stalna uprava organizacije", ErrUnauthorized)
	}
	target.IsGlobalAdmin = req.IsGlobalAdmin
	target.OrgType = req.OrgType
	target.OrgName = req.OrgName
	target.Phone = req.Phone
	target.MobilePhone = req.MobilePhone
	target.ShortPhone = req.ShortPhone
	target.ShortMobile = req.ShortMobile
	target.Email = req.Email
	target.IsActive = req.IsActive

	var zapisPotvrde string
	switch {
	case vlastitaAdresa:
		// novu adresu izvan domene potvrđuje administrator; stara potvrda
		// ne smije oživjeti ni kad se osoba vrati na staru adresu
		if staraPotvrda != "" {
			zapisPotvrde = fmt.Sprintf("prijava izvana: %s je sam promijenio adresu e-pošte; potvrda administratora za %s prestaje",
				target.Username, MaskirajAdresu(staraPotvrda))
		}
		obrisiPotvrdu(target)
	case req.PotvrdaAdrese != nil:
		if zapisPotvrde, err = potvrdiAdresu(actor, target, *req.PotvrdaAdrese, time.Now()); err != nil {
			return nil, err
		}
	}

	// tuđa lozinka postavljena u obrascu: kao kod poništenja, osoba je pri
	// prvoj prijavi mora zamijeniti svojom, jer ovu zna onaj tko ju je upisao
	tudjaLozinka := req.Password != "" && actor.User.ID != target.ID
	if req.Password != "" {
		pwHash, err := s.auth.HashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		target.PasswordHash = pwHash
		if tudjaLozinka {
			target.MustChangePassword = true
		}
	}

	if err := s.userRepo.UpdateUser(target); err != nil {
		return nil, err
	}
	if zapisPotvrde != "" {
		log.Print(zapisPotvrde)
	}
	if req.Password != "" {
		// Lozinku tuđeg računa postavlja administrator: kao kod poništenja,
		// gase se i otvorene prijave te osobe, ne samo zapamćena računala
		if err := s.auth.EndAllSessions(target.ID); err != nil {
			return nil, fmt.Errorf("lozinka je promijenjena, ali otvorene prijave nisu ugašene: %w", err)
		}
		if err := s.auth.opozoviPrijave(target.ID); err != nil {
			return nil, fmt.Errorf("lozinka je promijenjena, ali zapamćena računala nisu zaboravljena: %w", err)
		}
		if err := s.ukloniPotpisniKljuc(target.ID); err != nil {
			return nil, fmt.Errorf("lozinka je promijenjena, ali osobni potpisni ključ nije uklonjen pa ga nova lozinka ne otvara (postavite lozinku ponovno): %w", err)
		}
		if tudjaLozinka {
			s.zaboraviSanducic(target)
		}
	}
	if vlastitaAdresa && strings.TrimSpace(staraAdresa) != "" && s.auth.zastita != nil {
		osoba := *target
		// obavijest ide i na potvrđenu staru adresu: na nju je dotad išao PIN
		osoba.PINAdresaPotvrdena = staraPotvrda
		go s.auth.zastita.JaviPromjenuAdrese(context.Background(), &osoba, staraAdresa, target.Email)
	}

	s.sse.Broadcast("users_updated", fmt.Sprintf("Ažuriran djelatnik: %s", target.FullName), target.ID.String())
	return target, nil
}

type AddDutyRequest struct {
	UserID       uuid.UUID
	Title        string
	Role         models.Role
	ScopeType    models.ScopeType
	SectorID     *string
	AreaID       *int
	SectionCodes string // npr. "A.19.1, A.19.2, A.19.3"
	IsPrimary    bool
	IsTemporary  bool
	Reason       string
	// ExpiresAt je zadani prestanak privremene dužnosti („Vrijedi zaključno
	// s”: idući dan u 0 h po hrvatskom vremenu), ne stvarni istek: on se
	// računa (istekDuznosti)
	ExpiresAt *time.Time
	// IsticeSObranom: privremeno imenovanje vrijedi dok na dosegu traje
	// redovna ili izvanredna obrana ili izvanredno stanje
	IsticeSObranom bool
}

// AddDuty dodjeljuje korisniku dodatnu funkciju, dionice ili privremenu
// ispomoć. Tko nije globalni administrator, zadužuje samo aktivan račun koji
// već ima aktivnu dužnost (zaduzujeTudji); primarnu dužnost i vlastiti
// naziv daje samo onaj tko smije uređivati cijeli račun (mayManage), a
// drugima je dodana dužnost ispomoć pod nazivom uloge.
func (s *UserService) AddDuty(actor *models.UserPermissions, req AddDutyRequest) error {
	if actorRank(actor) == 0 {
		return ErrUnauthorized
	}
	// kao izmjena i opoziv: vlastito zaduženje dodjeljuje nadređena razina
	if !actor.IsGlobalAdmin && req.UserID == actor.User.ID {
		return fmt.Errorf("%w: vlastito zaduženje dodjeljuje nadređena razina", ErrUnauthorized)
	}
	sectors := s.areaSectors()
	scope, sectorID, areaID, err := normalizeScope(req.Role, req.SectorID, req.AreaID, req.SectionCodes, sectors, s.dionice())
	if err != nil {
		return err
	}
	req.ScopeType, req.SectorID, req.AreaID = scope, sectorID, areaID
	if err := mayAssign(actor, req.Role, req.SectorID, req.AreaID, sectors); err != nil {
		return err
	}
	if !actor.IsGlobalAdmin {
		target, err := s.userRepo.GetUserByID(req.UserID)
		if err != nil {
			return err
		}
		if err := zaduzujeTudji(target); err != nil {
			return err
		}
		if mayManage(actor, target, sectors) != nil {
			req.IsPrimary = false
			req.Title = ""
		}
	}

	if strings.TrimSpace(req.Title) == "" {
		req.Title = req.Role.Label()
	}
	// privremena uprava na svojoj razini dodjeljuje najdulje do svog isteka
	dopusteno := ograniciRok(actor, req.Role, req.SectorID, req.AreaID, req.SectionCodes, sectors, trazenaPrivremenost(req), nil)

	dutyID, err := uuid.NewV7()
	if err != nil {
		return err
	}

	actorID := actor.User.ID
	duty := &models.Duty{
		ID:           dutyID,
		UserID:       req.UserID,
		Title:        req.Title,
		Role:         req.Role,
		ScopeType:    req.ScopeType,
		SectorID:     req.SectorID,
		AreaID:       req.AreaID,
		SectionCodes: req.SectionCodes,
		IsPrimary:    req.IsPrimary,
		Reason:       req.Reason,
		AssignedBy:   &actorID,
		IsActive:     true,
	}
	dopusteno.upisi(duty)
	s.istekDuznosti(duty, time.Now())

	if err := s.userRepo.AddDuty(duty); err != nil {
		return err
	}

	s.sse.Broadcast("duty_added", fmt.Sprintf("Dodijeljena nova funkcija/ispomoć: %s", req.Title), duty.ID.String())
	return nil
}

// SmijeUredjivatiRacun javlja smije li actor uređivati cijeli račun osobe
// (mayManage): samo takav dodjeljuje primarnu dužnost i vlastiti naziv
func (s *UserService) SmijeUredjivatiRacun(actor *models.UserPermissions, userID uuid.UUID) bool {
	if actor == nil {
		return false
	}
	if actor.IsGlobalAdmin {
		return true
	}
	target, err := s.userRepo.GetUserByID(userID)
	if err != nil || target == nil {
		return false
	}
	return mayManage(actor, target, s.areaSectors()) == nil
}

// UpdateDuty mijenja postojeće zaduženje; smije tko bi ga smio dati i tko bi
// smio dati novo takvo. Vlastito zaduženje mijenja nadređena razina.
func (s *UserService) UpdateDuty(actor *models.UserPermissions, dutyID uuid.UUID, req AddDutyRequest) error {
	if actorRank(actor) == 0 {
		return ErrUnauthorized
	}
	duty, err := s.userRepo.GetDuty(dutyID)
	if err != nil {
		return err
	}
	if duty == nil || !duty.IsActive {
		return fmt.Errorf("zaduženje nije pronađeno ili je opozvano")
	}
	if !actor.IsGlobalAdmin && duty.UserID == actor.User.ID {
		return fmt.Errorf("%w: vlastito zaduženje mijenja nadređena razina", ErrUnauthorized)
	}
	sectors := s.areaSectors()
	if err := mayAssign(actor, duty.Role, duty.SectorID, duty.AreaID, sectors); err != nil {
		return err
	}
	scope, sectorID, areaID, err := normalizeScope(req.Role, req.SectorID, req.AreaID, req.SectionCodes, sectors, s.dionice())
	if err != nil {
		return err
	}
	if err := mayAssign(actor, req.Role, sectorID, areaID, sectors); err != nil {
		return err
	}
	if !actor.IsGlobalAdmin {
		// Kao dodjela: istekla dužnost računa koji drugih aktivnih nema ne
		// produljuje se odozdo (target.Duties su samo aktivne i neistekle)
		target, err := s.userRepo.GetUserByID(duty.UserID)
		if err != nil {
			return err
		}
		if err := zaduzujeTudji(target); err != nil {
			return err
		}
		if mayManage(actor, target, sectors) != nil {
			// Primarnu funkciju i vlastiti naziv (ide uz ime i u certifikat
			// potpisa) mijenja samo onaj tko uređuje cijeli račun; ostali ih
			// smiju ostaviti kakvi jesu ili ukloniti. Promjena se odbija, a
			// ne zamjenjuje tiho nazivom uloge.
			t := strings.TrimSpace(req.Title)
			if req.IsPrimary && !duty.IsPrimary {
				return fmt.Errorf("%w: primarnu funkciju daje onaj tko uređuje cijeli račun osobe (viša razina ili globalni administrator)", ErrUnauthorized)
			}
			if t != "" && t != duty.Title && t != req.Role.Label() {
				return fmt.Errorf("%w: vlastiti naziv dužnosti mijenja onaj tko uređuje cijeli račun osobe (viša razina ili globalni administrator); ostavite naziv „%s” ili upišite naziv uloge", ErrUnauthorized, duty.Title)
			}
			// naziv koji je bio samo naziv uloge prati novu ulogu
			if t == duty.Title && duty.Title == duty.Role.Label() {
				req.Title = ""
			}
		}
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = req.Role.Label()
	}
	dosad := *duty
	duty.Title, duty.Role, duty.ScopeType = req.Title, req.Role, scope
	duty.SectorID, duty.AreaID, duty.SectionCodes = sectorID, areaID, req.SectionCodes
	duty.IsPrimary, duty.Reason = req.IsPrimary, req.Reason
	// privremena uprava na svojoj razini ne produljuje ni ne trajno ostavlja
	// dužnost preko svog isteka
	ograniciRok(actor, duty.Role, duty.SectorID, duty.AreaID, duty.SectionCodes, sectors, trazenaPrivremenost(req), &dosad).upisi(duty)
	s.istekDuznosti(duty, time.Now())
	if err := s.userRepo.UpdateDuty(duty); err != nil {
		return err
	}
	s.sse.Broadcast("duty_updated", fmt.Sprintf("Izmijenjeno zaduženje: %s", duty.Title), duty.ID.String())
	return nil
}

// GetDuty čita jedno zaduženje, aktivno ili opozvano
func (s *UserService) GetDuty(id uuid.UUID) (*models.Duty, error) { return s.userRepo.GetDuty(id) }

// PastDuties vraća opozvana i istekla zaduženja osobe, za povijest na profilu
func (s *UserService) PastDuties(userID uuid.UUID) ([]models.PrijasnjeZaduzenje, error) {
	return s.userRepo.GetPastDutiesForUser(userID)
}

// RevokeDuty opoziva funkciju ili privremenu ispomoć; smije tko bi je smio i dati
func (s *UserService) RevokeDuty(actor *models.UserPermissions, dutyID uuid.UUID) error {
	if actorRank(actor) == 0 {
		return ErrUnauthorized
	}
	duty, err := s.userRepo.GetDuty(dutyID)
	if err != nil {
		return err
	}
	if duty == nil {
		return fmt.Errorf("zaduženje nije pronađeno")
	}
	if !actor.IsGlobalAdmin && duty.UserID == actor.User.ID {
		return fmt.Errorf("%w: vlastitu dužnost opoziva nadređena razina", ErrUnauthorized)
	}
	if err := mayAssign(actor, duty.Role, duty.SectorID, duty.AreaID, s.areaSectors()); err != nil {
		return err
	}

	if err := s.userRepo.RevokeDuty(dutyID); err != nil {
		return err
	}
	s.uskladiPoOpozivu(dutyID)

	s.sse.Broadcast("duty_revoked", "Opozvana funkcija / ovlast", dutyID.String())
	return nil
}

// DeleteUser briše račun koji se nitko nikad nije prijavio, sa svim
// dužnostima. Račun koji je radio ne briše se, jer bi očitanja i upisi ostali
// bez imena: njemu se isključi prijava.
func (s *UserService) DeleteUser(actor *models.UserPermissions, targetID uuid.UUID) error {
	if actorRank(actor) == 0 {
		return ErrUnauthorized
	}

	target, err := s.userRepo.GetUserByID(targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrUserNotFound
	}

	// Korisnik ne može obrisati samoga sebe
	if actor.User.ID == target.ID {
		return fmt.Errorf("ne možete obrisati vlastiti korisnički profil")
	}

	if target.LastLoginAt != nil {
		return fmt.Errorf("račun koji se prijavljivao ne briše se: isključite mu prijavu, da upisi zadrže ime")
	}
	if !actor.IsGlobalAdmin {
		if err := mayManage(actor, target, s.areaSectors()); err != nil {
			return err
		}
	}

	if err := s.userRepo.DeleteUser(targetID); err != nil {
		return err
	}

	s.sse.Broadcast("user_deleted", fmt.Sprintf("Obrisan profil djelatnika: %s", target.FullName), targetID.String())
	return nil
}

// ListUsers vraća korisnike po filtrima; uz StanjeBezEposte i one čija
// adresa nije u domeni na koju ide PIN (postavka prijave izvana)
func (s *UserService) ListUsers(sectorID string, areaID int, role, search, status string) ([]models.User, error) {
	if status == string(repository.StanjeBezEposte) {
		return s.userRepo.ListUsersDomena(sectorID, areaID, role, search, status, s.domenaPINa())
	}
	return s.userRepo.ListUsers(sectorID, areaID, role, search, status)
}

// domenaPINa je domena na koju ide PIN (ZadanaDomenaPIN bez postavke)
func (s *UserService) domenaPINa() string {
	if s.auth != nil {
		if o, ok := s.auth.zastita.(interface {
			Opcije(context.Context) OpcijePrijaveIzvana
		}); ok {
			return o.Opcije(context.Background()).Domena
		}
	}
	return ZadanaDomenaPIN
}

func (s *UserService) GetUserByID(id uuid.UUID) (*models.User, error) {
	return s.userRepo.GetUserByID(id)
}

// GlobalAdminContact je kontakt glavnog administratora za stranicu prijave
func (s *UserService) GlobalAdminContact() (name, phone, email string, ok bool) {
	return s.userRepo.GlobalAdminContact()
}

func (s *UserService) ListSectors() ([]models.Sector, error) {
	return s.userRepo.ListSectors()
}

func (s *UserService) ListAreas(sectorID string) ([]models.Area, error) {
	return s.userRepo.ListAreas(sectorID)
}

func (s *UserService) GetDashboardStats() (repository.DashboardStats, error) {
	return s.userRepo.GetDashboardStats()
}
