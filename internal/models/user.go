package models

import (
	"sort"
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
	RoleWarehouseKeeper      Role = "WAREHOUSE_KEEPER"       // Skladištar sredstava za obranu
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
	// ExpiresAt je stvarni istek, kako ga provjerava svaki dio programa (i
	// čvor starije inačice): stalna dužnost ga nema, a privremenoj je
	// najraniji od Rok, kraja obrane (IsticeSObranom) i isteka dužnosti iz
	// koje je dodijeljena (OvisiO)
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	IsActive  bool       `json:"is_active"`
	// Rok je zadani prestanak privremene dužnosti: obrazac „Vrijedi zaključno
	// s” 14. 10. daje 15. 10. u 0 h po hrvatskom vremenu (PrestanakNakonDana)
	Rok *time.Time `json:"rok,omitempty"`
	// IsticeSObranom: privremeno imenovanje vrijedi dok na njegovim dionicama,
	// odnosno u branjenom području, traje redovna ili izvanredna obrana ili
	// izvanredno stanje (models.PrestanakRedovneObrane)
	IsticeSObranom bool `json:"istece_s_obranom,omitempty"`
	// OvisiO je dužnost privremene uprave iz koje je ova dodijeljena: ističe
	// zajedno s njom
	OvisiO *uuid.UUID `json:"ovisi_o,omitempty"`
}

// ZadaniRok je prestanak zadan pri dodjeli („Vrijedi zaključno s”): stvarni
// istek može biti raniji (kraj obrane, istek dužnosti iz koje je
// dodijeljena), a zapisi iz vremena prije zadanog roka imaju ga samo u isteku
func (d Duty) ZadaniRok() *time.Time {
	if d.Rok != nil || d.IsticeSObranom || d.OvisiO != nil {
		return d.Rok
	}
	return d.ExpiresAt
}

// PrijasnjeZaduzenje je opozvano ili isteklo zaduženje kako ostaje u povijesti
// profila; OpozvanoAt je prazno kad je zaduženje samo isteklo
type PrijasnjeZaduzenje struct {
	Duty
	OpozvanoAt *time.Time
}

// Isteklo javlja je li zaduženje prestalo istekom roka, a ne opozivom
func (z PrijasnjeZaduzenje) Isteklo() bool { return z.IsActive && z.ExpiresAt != nil }

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
	Phone              string     `json:"phone"`        // Telefon u uredu, s pozivnim brojem (npr. N/A)
	MobilePhone        string     `json:"mobile_phone"` // Broj mobitela (npr. 099-000-0000)
	ShortPhone         string     `json:"short_phone"`  // Lokal: skraćeni broj uredskog telefona u mreži (npr. N/A)
	ShortMobile        string     `json:"short_mobile"` // Skraćeni broj mobitela u zatvorenoj mreži (npr. 5163)
	Email              string     `json:"email"`
	IsActive           bool       `json:"is_active"`               // Smije li se osoba prijaviti — odluka administratora, a ne trag korištenja
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"` // Zadnja prijava, bilježi se s točnošću na dan
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// Adresa e-pošte koju je globalni administrator potvrdio, pa PIN za
	// prijavu izvana smije ići na nju i izvan dopuštene domene (npr.
	// djelatnik tvrtke izvođača), te tko ju je i kada potvrdio. Potvrda
	// vrijedi samo dok je jednaka adresi računa (PotvrdaAdreseVrijedi):
	// promjena adrese bez nove potvrde sama je poništi.
	PINAdresaPotvrdena    string     `json:"pin_adresa_potvrdena,omitempty"`
	PINAdresuPotvrdio     string     `json:"pin_adresa_potvrdio,omitempty"` // ime administratora
	PINAdresaPotvrdenaKad *time.Time `json:"pin_adresa_potvrdena_kad,omitempty"`

	// Sve aktivne funkcije i zaduženja osobe (i stalna i privremena ispomoć)
	Duties []Duty `json:"duties,omitempty"`
}

// PotvrdaAdreseVrijedi javlja vrijedi li potvrda administratora za
// trenutnu adresu: potvrđena adresa jednaka je adresi računa, bez obzira na
// velika slova i razmake. Prazna adresa potvrde nema.
func (u *User) PotvrdaAdreseVrijedi() bool {
	a := strings.TrimSpace(u.Email)
	return a != "" && strings.EqualFold(a, strings.TrimSpace(u.PINAdresaPotvrdena))
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

// vrijedi javlja je li dužnost aktivna i neistekla u trenutku sad
func (d Duty) vrijedi(sad time.Time) bool {
	return d.IsActive && (d.ExpiresAt == nil || !d.ExpiresAt.Before(sad))
}

// PrimaryDuty vraća primarnu funkciju korisnika: aktivnu, neisteklu
// primarnu, a kad je nema, prvu aktivnu i neisteklu
func (u *User) PrimaryDuty() *Duty {
	sad := time.Now()
	var prva *Duty
	for i := range u.Duties {
		d := &u.Duties[i]
		if !d.vrijedi(sad) {
			continue
		}
		if d.IsPrimary {
			return d
		}
		if prva == nil {
			prva = d
		}
	}
	return prva
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

// VidiVodocuvarskiDnevnik javlja smije li korisnik uopće u vodočuvarske
// dnevnike: vodočuvar u svoj, rukovoditelji, ovlaštenici i uprava u tuđe,
// sve s aktivnom i neisteklom dužnošću. Strojari, rukovatelji, terenski
// radnici i skladištari nemaju što ondje tražiti; oni će imati svoje
// dnevnike. Ne vide ih ni operater, poslovođa izvođača, gost, preglednik i
// nepoznata uloga.
func (u *User) VidiVodocuvarskiDnevnik() bool {
	if u == nil {
		return false
	}
	if u.IsGlobalAdmin {
		return true
	}
	for _, d := range u.Duties {
		if !d.IsActive {
			continue
		}
		if d.Role == RoleWaterGuard || (!d.Role.IsField() && d.Role != RoleWarehouseKeeper) {
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

// UserPermissions sadrži zbirne ovlasti izvedene iz svih funkcija korisnika.
//
// Pravo pisanja ide po dosegu dužnosti: dužnost sektora piše u sektoru,
// dužnost područja u području, dužnost dionica na tim dionicama. Dužnost
// područja ili dionice nosi i sektor (normalizeScope ga upiše iz područja),
// ali on joj ne daje pisanje po sektoru; zato SektoriDuznosti i
// PodrucjaDuznosti stoje odvojeno, samo za prikaz i zadani izbor.
type UserPermissions struct {
	User            User
	IsGlobalAdmin   bool
	AdminSectors    map[string]bool // Sektori u kojima je korisnik administrator
	AdminAreas      map[int]bool    // Područja u kojima je korisnik administrator
	AllowedSectors  map[string]bool // Sektori s pravom pisanja (dužnost s dosegom sektora)
	AllowedAreas    map[int]bool    // Branjena područja s pravom pisanja (dužnost područja, ili terenska bez dionica)
	AllowedSections map[string]bool // Pojedinačne dionice s pravom pisanja
	// SektoriDuznosti i PodrucjaDuznosti su sektori i područja svih dužnosti
	// s pravom upisa, bez obzira na doseg: koji sektor ili područje otvoriti,
	// što pokazati. Pravo pisanja ne daju.
	SektoriDuznosti  map[string]bool
	PodrucjaDuznosti map[int]bool
	// PodrucjaDionica su područja u kojima osoba ima dužnost na dionicama
	// (rukovoditelj dionice, vodočuvar dionica). Dnevnik se vodi po
	// području, a ne po dionici, pa tamo piše u dnevnike i upisuje na
	// objekte koji nisu ni uz jednu dionicu; uz objekte drugog područja
	// koji stoje na njezinim dionicama ne piše (RadiNaDionicamaU).
	PodrucjaDionica map[int]bool
}

// RadiNaDionicamaU javlja ima li osoba dužnost na dionicama u području
func (p *UserPermissions) RadiNaDionicamaU(areaID int) bool {
	return p != nil && areaID > 0 && p.PodrucjaDionica[areaID]
}

// RadiUSektoru javlja ima li osoba u sektoru ijednu dužnost s pravom upisa,
// bilo kojeg dosega; za prikaz i izbor, ne za pravo pisanja
func (p *UserPermissions) RadiUSektoru(sektor string) bool {
	return p != nil && sektor != "" && (p.AllowedSectors[sektor] || p.SektoriDuznosti[sektor])
}

// RadiUPodrucju javlja ima li osoba u području ijednu dužnost s pravom
// upisa, bilo kojeg dosega; za prikaz i izbor, ne za pravo pisanja
func (p *UserPermissions) RadiUPodrucju(areaID int) bool {
	return p != nil && areaID > 0 && (p.AllowedAreas[areaID] || p.PodrucjaDuznosti[areaID])
}

// SektoriRada su sektori u kojima osoba radi (RadiUSektoru), poredani
func (p *UserPermissions) SektoriRada() []string {
	if p == nil {
		return nil
	}
	skup := map[string]bool{}
	for s := range p.AllowedSectors {
		skup[s] = true
	}
	for s := range p.SektoriDuznosti {
		skup[s] = true
	}
	out := make([]string, 0, len(skup))
	for s := range skup {
		if s != "" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// VodiSkladista javlja je li osoba skladištar za to branjeno područje (ili
// za cijeli sektor, kad je dužnost bez područja). Skladištar upisuje promet
// i popis, ali ne upravlja ničim drugim — pa mu doseg ne daje upravu.
func (p *UserPermissions) VodiSkladista(sektor string, areaID int) bool {
	if p == nil {
		return false
	}
	for _, d := range p.User.Duties {
		if !d.IsActive || d.Role != RoleWarehouseKeeper {
			continue
		}
		if d.ExpiresAt != nil && d.ExpiresAt.Before(time.Now()) {
			continue
		}
		if d.AreaID != nil && *d.AreaID > 0 {
			if *d.AreaID == areaID {
				return true
			}
			continue
		}
		if d.SectorID != nil && *d.SectorID == sektor {
			return true
		}
	}
	return false
}

// NewUserPermissions izračunava ukupne ovlasti korisnika iz svih njegovih funkcija
func NewUserPermissions(u User) *UserPermissions {
	p := &UserPermissions{
		User:             u,
		IsGlobalAdmin:    u.IsGlobalAdmin,
		AdminSectors:     make(map[string]bool),
		AdminAreas:       make(map[int]bool),
		AllowedSectors:   make(map[string]bool),
		AllowedAreas:     make(map[int]bool),
		AllowedSections:  make(map[string]bool),
		SektoriDuznosti:  make(map[string]bool),
		PodrucjaDuznosti: make(map[int]bool),
		PodrucjaDionica:  make(map[int]bool),
	}

	for _, d := range u.Duties {
		if !d.IsActive {
			continue
		}
		if d.ExpiresAt != nil && d.ExpiresAt.Before(time.Now()) {
			continue
		}

		// Uprava: organizacije, sektora (razina 2) i područja (razina 3)
		switch d.Role.RazinaUprave() {
		case 1:
			p.IsGlobalAdmin = true
		case 2:
			// uprava praznog sektora ili područja 0 nije uprava ničega
			if d.SectorID != nil && *d.SectorID != "" {
				p.AdminSectors[*d.SectorID] = true
			}
		case 3:
			if d.AreaID != nil && *d.AreaID > 0 {
				p.AdminAreas[*d.AreaID] = true
			}
		}

		// Prava upisa prema dosegu; doseg sam po sebi ne daje upravu, a
		// gost i preglednik ne pišu ni u svom dosegu
		if !d.Role.Writes() {
			continue
		}
		sektor := d.SectorID != nil && *d.SectorID != ""
		podrucje := d.AreaID != nil && *d.AreaID > 0
		if sektor {
			p.SektoriDuznosti[*d.SectorID] = true
		}
		if podrucje {
			p.PodrucjaDuznosti[*d.AreaID] = true
		}
		dionice := false
		for _, part := range strings.Split(d.SectionCodes, ",") {
			if code := strings.TrimSpace(part); code != "" {
				// dionice dužnosti su uvijek u njezinu području (normalizeScope)
				p.AllowedSections[code] = true
				dionice = true
			}
		}
		if dionice && podrucje {
			p.PodrucjaDionica[*d.AreaID] = true
		}
		// Dužnost područja ili dionice nosi i sektor, ali piše samo u svom
		// dosegu: rukovoditelj dionice B.16.1 ne piše po cijelom sektoru B
		switch d.Doseg() {
		case ScopeSector, ScopeAll:
			if sektor {
				p.AllowedSectors[*d.SectorID] = true
			}
			if podrucje {
				p.AllowedAreas[*d.AreaID] = true
			}
		case ScopeArea:
			if podrucje {
				p.AllowedAreas[*d.AreaID] = true
			}
		case ScopeSection:
			// terenska dužnost bez dionica pokriva cijelo područje
			if !dionice && podrucje {
				p.AllowedAreas[*d.AreaID] = true
			}
		}
	}

	return p
}

// Doseg je prostorni doseg dužnosti: upisani, a kad ga nema, onaj koji
// određuje uloga
func (d Duty) Doseg() ScopeType {
	if d.ScopeType != "" {
		return d.ScopeType
	}
	return d.Role.NaturalScope()
}

// HasWriteAccess provjerava ima li korisnik pravo unosa za zadani sektor,
// područje ili dionicu; bez ovlasti (nil) nema ga
func (p *UserPermissions) HasWriteAccess(sectorID string, areaID int, sectionCode string) bool {
	if p == nil {
		return false
	}
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

// CanAdminister provjerava može li korisnik administrirati zadanu prostornu
// jedinicu; bez ovlasti (nil) ne može
func (p *UserPermissions) CanAdminister(sectorID string, areaID int) bool {
	if p == nil {
		return false
	}
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
