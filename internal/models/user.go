package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Role definira ulogu/razinu ovlasti korisnika za određeno zaduženje
type Role string

const (
	RoleGlobalAdmin          Role = "GLOBAL_ADMIN"           // Cjelokupno upravljanje sustavom i svim korisnicima
	RoleNationalLeader       Role = "NATIONAL_LEADER"        // Glavni rukovoditelj obrane od poplava (za cijelu RH)
	RoleNationalDeputy       Role = "NATIONAL_DEPUTY"        // Zamjenik Glavnog rukovoditelja (za cijelu RH)
	RoleMainCenterLeader     Role = "MAIN_CENTER_LEADER"     // Voditelj Glavnog centra obrane od poplava
	RoleMainCenterDeputy     Role = "MAIN_CENTER_DEPUTY"     // Zamjenik voditelja Glavnog centra obrane od poplava
	RoleSectorMainDeputy     Role = "SECTOR_MAIN_DEPUTY"     // Zamjenik Glavnog rukovoditelja za sektor
	RoleSectorLeader         Role = "SECTOR_LEADER"          // Rukovoditelj sektora
	RoleSectorDeputy         Role = "SECTOR_DEPUTY"          // Zamjenik rukovoditelja sektora
	RoleSectorAreaDeputy     Role = "SECTOR_AREA_DEPUTY"     // Zamjenik rukovoditelja sektora za branjeno područje
	RoleCopLeader            Role = "COP_LEADER"             // Voditelj Centra obrane od poplava
	RoleCopDeputy            Role = "COP_DEPUTY"             // Zamjenik voditelja Centra obrane od poplava
	RoleAreaAdmin            Role = "AREA_ADMIN"             // Voditelji COP-a i zamjenici (legacy alias)
	RoleAreaLeader           Role = "AREA_LEADER"            // Rukovoditelj branjenog područja
	RoleAreaDeputy           Role = "AREA_DEPUTY"            // Zamjenik rukovoditelja branjenog područja
	RoleSectionLeader        Role = "SECTION_LEADER"         // Rukovoditelj dionice
	RoleSectionDeputy        Role = "SECTION_DEPUTY"         // Zamjenik rukovoditelja dionice
	RoleContractOfficerA2    Role = "CONTRACT_OFFICER_A2"    // Ovlaštenik za praćenje ugovora programa usluga A2
	RoleContractOfficerA3    Role = "CONTRACT_OFFICER_A3"    // Ovlaštenik za praćenje ugovora programa usluga A3
	RoleContractDeputyA2     Role = "CONTRACT_DEPUTY_A2"     // Zamjenik ovlaštenika za praćenje ugovora A2
	RoleContractDeputyA3     Role = "CONTRACT_DEPUTY_A3"     // Zamjenik ovlaštenika za praćenje ugovora A3
	RoleServiceLeaderForeman Role = "SERVICE_LEADER_FOREMAN" // Voditelj usluga / Poslovođa (licencirane firme)
	RoleOperator             Role = "OPERATOR"               // Dežurni operater u COP-u
	RoleWaterGuard           Role = "WATER_GUARD"            // Vodočuvar
	RoleMachinist            Role = "MACHINIST"              // Strojar
	RoleFacilityOperator     Role = "FACILITY_OPERATOR"      // Rukovatelj
	RoleCrewLeader           Role = "CREW_LEADER"            // Voditelj posade objekta
	RoleFieldWorker          Role = "FIELD_WORKER"           // Terenski radnik (legacy alias)
	RoleGuest                Role = "GUEST"                  // Gost: račun za posjetitelja obrane, samo gleda
	RoleViewer               Role = "VIEWER"                 // Preglednik (samo čitanje)
)

// ScopeType definira prostorni doseg zaduženja
type ScopeType string

const (
	ScopeAll     ScopeType = "ALL"     // Cijela Republika Hrvatska
	ScopeSector  ScopeType = "SECTOR"  // Sektor (npr. Sektor B)
	ScopeArea    ScopeType = "AREA"    // Branjeno područje (npr. Područje 15)
	ScopeSection ScopeType = "SECTION" // Specifične dionice (jedna ili više)
)

// OrgType definira tip organizacije korisnika
type OrgType string

const (
	OrgHrvatskeVode OrgType = "HRVATSKE_VODE"
	OrgPravnaOsoba  OrgType = "PRAVNA_OSOBA"
	OrgVanjski      OrgType = "VANJSKI"
)

// Label je vrsta organizacije za prikaz; matična organizacija nosi naziv iz
// postavki razine 1
func (o OrgType) Label() string {
	switch o {
	case OrgHrvatskeVode:
		return Terms().OrgName
	case OrgPravnaOsoba:
		return "Ugovorni izvođač (pravna osoba)"
	case OrgVanjski:
		return "Vanjska služba (CZ / MUP / 112)"
	default:
		return string(o)
	}
}

// Duty predstavlja konkretnu funkciju ili zaduženje osobe.
// Jedna osoba može imati više funkcija istovremeno (npr. voditelj COP-a, rukovoditelj sektora i zadužen za dionice).
type Duty struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	Title        string     `json:"title"`      // Naziv dužnosti (npr. "Voditelj COP Osijek", "Rukovoditelj dionica A.19.1 - A.19.4")
	Role         Role       `json:"role"`       // Uloga u operativnom smislu
	ScopeType    ScopeType  `json:"scope_type"` // SECTOR, AREA, SECTION, ALL
	SectorID     *string    `json:"sector_id,omitempty"`
	AreaID       *int       `json:"area_id,omitempty"`
	SectionCodes string     `json:"section_codes,omitempty"` // Više dionica odvojenih zarezom, npr. "A.19.1, A.19.2, A.19.3"
	IsPrimary    bool       `json:"is_primary"`              // Primarna funkcija za prikaz uz ime
	IsTemporary  bool       `json:"is_temporary"`            // Je li privremena ispomoć ili stalna dužnost
	Reason       string     `json:"reason,omitempty"`        // Razlog (kod ispomoći)
	AssignedBy   *uuid.UUID `json:"assigned_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	IsActive     bool       `json:"is_active"`
}

// User predstavlja matični korisnički račun djelatnika ili vanjskog suradnika
type User struct {
	ID                 uuid.UUID  `json:"id"`
	Username           string     `json:"username"`
	PasswordHash       string     `json:"-"`
	FullName           string     `json:"full_name"`
	Title              string     `json:"title"` // dipl.ing.građ., mag.ing.aedif...
	IsGlobalAdmin      bool       `json:"is_global_admin"`
	MustChangePassword bool       `json:"must_change_password"`
	OrgType            OrgType    `json:"org_type"`
	OrgName            string     `json:"org_name"`     // npr. "VGO Osijek, Splavarska 2a", "Bistra d.o.o."
	Phone              string     `json:"phone"`        // Telefon u uredu, s pozivnim brojem (npr. 031/252-802)
	MobilePhone        string     `json:"mobile_phone"` // Broj mobitela (npr. 099-000-0000)
	ShortPhone         string     `json:"short_phone"`  // Lokal: skraćeni broj uredskog telefona u mreži (npr. 2802)
	ShortMobile        string     `json:"short_mobile"` // Skraćeni broj mobitela u zatvorenoj mreži (npr. 5163)
	Email              string     `json:"email"`
	IsActive           bool       `json:"is_active"`               // Smije li se osoba prijaviti — odluka administratora, a ne trag korištenja
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"` // Zadnja prijava, bilježi se s točnošću na dan
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// Sve aktivne funkcije i zaduženja osobe (i stalna i privremena ispomoć)
	Duties []Duty `json:"duties,omitempty"`
}

// AccountState je stanje računa za prikaz. Zastavica is_active govori samo
// smije li se osoba prijaviti; ona ne razlikuje djelatnika koji program
// koristi od onoga koji svoj račun još nije ni preuzeo, a upravo ta razlika
// zanima administratora prije sezone obrane.
type AccountState string

const (
	AccountActive   AccountState = "ACTIVE"   // Osoba je preuzela račun i prijavljivala se
	AccountPending  AccountState = "PENDING"  // Račun postoji, osoba se još nije prijavila
	AccountDisabled AccountState = "DISABLED" // Administrator je isključio prijavu
)

// AccountState izvodi stanje računa. Mjerilo je zabilježena prijava, a ne
// zadana lozinka: račun kojim se nitko nije prijavio nije aktivan ni onda kad
// mu je lozinka već postavljena, jer ni takav nitko ne čita.
func (u *User) AccountState() AccountState {
	if !u.IsActive {
		return AccountDisabled
	}
	if u.LastLoginAt == nil {
		return AccountPending
	}
	return AccountActive
}

// Label je naziv stanja u sučelju
func (s AccountState) Label() string {
	switch s {
	case AccountDisabled:
		return "Neaktivan"
	case AccountPending:
		return "Nije se prijavio"
	default:
		return "Aktivan"
	}
}

// BadgeClass je CSS razred značke za stanje
func (s AccountState) BadgeClass() string {
	switch s {
	case AccountDisabled:
		return "badge-inactive"
	case AccountPending:
		return "badge-pending"
	default:
		return "badge-active"
	}
}

// PrimaryDuty vraća primarnu funkciju korisnika
func (u *User) PrimaryDuty() *Duty {
	for i := range u.Duties {
		if u.Duties[i].IsPrimary && u.Duties[i].IsActive {
			return &u.Duties[i]
		}
	}
	if len(u.Duties) > 0 && u.Duties[0].IsActive {
		return &u.Duties[0]
	}
	return nil
}

// IsField javlja je li uloga terenska: ti ljudi očitavaju letve i vode
// dnevnik, pa im program nakon prijave otvara terenski pogled
func (r Role) IsField() bool {
	switch r {
	case RoleWaterGuard, RoleMachinist, RoleFacilityOperator, RoleCrewLeader, RoleFieldWorker:
		return true
	}
	return false
}

// IsFieldUser javlja radi li korisnik na terenu (bilo koja aktivna terenska dužnost)
func (u *User) IsFieldUser() bool {
	if u == nil || u.IsGlobalAdmin {
		return false
	}
	for _, d := range u.Duties {
		if d.IsActive && d.Role.IsField() {
			return true
		}
	}
	return false
}

// PrimaryRole je uloga koja se pokazuje uz ime: primarna dužnost, jer je
// administracija programa zastavica na računu, a ne mjesto u obrani. Tko
// nema dužnosti, a administrira, pokazuje se kao administrator.
func (u *User) PrimaryRole() Role {
	if pd := u.PrimaryDuty(); pd != nil {
		return pd.Role
	}
	if u.IsGlobalAdmin {
		return RoleGlobalAdmin
	}
	return RoleViewer
}

// Session predstavlja korisničku sesiju. Sesije su lokalne: ne sinkroniziraju
// se, jer prijava na jednom računalu nije prijava na drugom.
type Session struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
	// ViewingAs je djelatnik čijim očima administrator trenutno gleda program.
	// Stoji na sesiji, a ne u kolačiću, da ga preglednik ne može podmetnuti.
	ViewingAs *uuid.UUID `json:"viewing_as,omitempty"`
	IPAddress string     `json:"ip_address"`
	UserAgent string     `json:"user_agent"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// UserPermissions sadrži zbirne ovlasti izvedene iz svih funkcija korisnika
type UserPermissions struct {
	User            User
	IsGlobalAdmin   bool
	AdminSectors    map[string]bool // Sektori u kojima je korisnik administrator
	AdminAreas      map[int]bool    // Područja u kojima je korisnik administrator
	AllowedSectors  map[string]bool // Sektori s pravom pisanja
	AllowedAreas    map[int]bool    // Branjena područja s pravom pisanja
	AllowedSections map[string]bool // Pojedinačne dionice s pravom pisanja
}

// NewUserPermissions izračunava ukupne ovlasti korisnika iz svih njegovih funkcija
func NewUserPermissions(u User) *UserPermissions {
	p := &UserPermissions{
		User:            u,
		IsGlobalAdmin:   u.IsGlobalAdmin,
		AdminSectors:    make(map[string]bool),
		AdminAreas:      make(map[int]bool),
		AllowedSectors:  make(map[string]bool),
		AllowedAreas:    make(map[int]bool),
		AllowedSections: make(map[string]bool),
	}

	for _, d := range u.Duties {
		if !d.IsActive {
			continue
		}
		if d.ExpiresAt != nil && d.ExpiresAt.Before(time.Now()) {
			continue
		}

		// Provjera admin prava
		if d.Role == RoleGlobalAdmin || d.Role == RoleNationalLeader || d.Role == RoleNationalDeputy ||
			d.Role == RoleMainCenterLeader || d.Role == RoleMainCenterDeputy {
			p.IsGlobalAdmin = true
		}
		// Uprava sektora (razina 2) i uprava područja (razina 3). Voditelj
		// usluga izvođača je vanjska osoba: piše, ali ne upravlja računima.
		if d.Role == RoleCopLeader || d.Role == RoleCopDeputy || d.Role == RoleAreaAdmin || d.Role == RoleSectorMainDeputy || d.Role == RoleSectorLeader || d.Role == RoleSectorDeputy {
			if d.SectorID != nil {
				p.AdminSectors[*d.SectorID] = true
			}
		}
		if d.Role == RoleSectorAreaDeputy || d.Role == RoleAreaLeader || d.Role == RoleAreaDeputy ||
			d.Role == RoleContractOfficerA2 || d.Role == RoleContractOfficerA3 ||
			d.Role == RoleContractDeputyA2 || d.Role == RoleContractDeputyA3 {
			if d.AreaID != nil {
				p.AdminAreas[*d.AreaID] = true
			}
		}

		// Prava upisa prema dosegu; doseg sam po sebi ne daje upravu, a
		// gost i preglednik ne pišu ni u svom dosegu
		if !d.Role.Writes() {
			continue
		}
		if d.SectorID != nil && *d.SectorID != "" {
			p.AllowedSectors[*d.SectorID] = true
		}
		if d.AreaID != nil && *d.AreaID > 0 {
			p.AllowedAreas[*d.AreaID] = true
		}
		if d.SectionCodes != "" {
			parts := strings.Split(d.SectionCodes, ",")
			for _, part := range parts {
				code := strings.TrimSpace(part)
				if code != "" {
					p.AllowedSections[code] = true
				}
			}
		}
	}

	return p
}

// HasWriteAccess provjerava ima li korisnik pravo unosa za zadani sektor, područje ili dionicu
func (p *UserPermissions) HasWriteAccess(sectorID string, areaID int, sectionCode string) bool {
	if p.IsGlobalAdmin {
		return true
	}
	if sectorID != "" && p.AllowedSectors[sectorID] {
		return true
	}
	if areaID > 0 && p.AllowedAreas[areaID] {
		return true
	}
	if sectionCode != "" && p.AllowedSections[sectionCode] {
		return true
	}
	return false
}

// CanAdminister provjerava može li korisnik administrirati zadanu prostornu jedinicu
func (p *UserPermissions) CanAdminister(sectorID string, areaID int) bool {
	if p.IsGlobalAdmin {
		return true
	}
	if sectorID != "" && p.AdminSectors[sectorID] {
		return true
	}
	if areaID > 0 && p.AdminAreas[areaID] {
		return true
	}
	return false
}
