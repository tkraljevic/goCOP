package models

// Sudionici obrane: tko sve u obrani od poplava ima ulogu i što mu ona daje.
// Šifre uloga nose ovlasti i ne mijenjaju se; nazive organizacija smije
// zvati po svome (OrgTerms.RoleLabels), jer druga vodna organizacija
// vodočuvara ili rukovoditelja zove drukčije.

// RoleDef je jedna uloga u katalogu sudionika obrane
type RoleDef struct {
	Role  Role
	Group string    // razina ustroja ili mjesto u obrani kojem uloga pripada
	Name  string    // zadani naziv (Hrvatske vode)
	Desc  string    // što uloga smije u programu
	Scope ScopeType // prirodni doseg: što dužnost s tom ulogom mora imati
}

// Skupine sudionika, redom od vrha ustroja prema terenu
const (
	RoleGroupLevel1   = "Razina 1"
	RoleGroupLevel2   = "Razina 2"
	RoleGroupLevel3   = "Razina 3"
	RoleGroupLevel4   = "Razina 4"
	RoleGroupField    = "Teren"
	RoleGroupExternal = "Vanjski suradnici"
	RoleGroupGuest    = "Gost"
	RoleGroupProgram  = "Program"
)

// RoleCatalog su sve uloge koje program poznaje, redom kojim se nude u
// obrascima: od najviše prema terenu
var RoleCatalog = []RoleDef{
	{RoleNationalLeader, RoleGroupLevel1, "Glavni rukovoditelj (za cijelu RH)", "vodi obranu cijele organizacije; puna prava u programu", ScopeAll},
	{RoleNationalDeputy, RoleGroupLevel1, "Zamjenik Glavnog rukovoditelja (za cijelu RH)", "zamjenjuje glavnog rukovoditelja; puna prava", ScopeAll},
	{RoleMainCenterLeader, RoleGroupLevel1, "Voditelj Glavnog centra obrane od poplava", "vodi glavni centar obrane po nalogu glavnog rukovoditelja: vodi evidenciju u programu, piše izvješća i koordinira centre sektora; puna prava", ScopeAll},
	{RoleMainCenterDeputy, RoleGroupLevel1, "Zamjenik voditelja Glavnog centra obrane od poplava", "zamjenjuje voditelja glavnog centra u istom poslu; puna prava", ScopeAll},
	{RoleSectorMainDeputy, RoleGroupLevel1, "Zamjenik Glavnog rukovoditelja za sektor", "imenovanje s razine 1: prava glavnog rukovoditelja, ali u okviru svog sektora; u pravilu ista osoba kao rukovoditelj sektora", ScopeSector},
	{RoleSectorLeader, RoleGroupLevel2, "Rukovoditelj sektora", "vodi obranu sektora; upravlja sektorom i njegovim područjima", ScopeSector},
	{RoleSectorDeputy, RoleGroupLevel2, "Zamjenik rukovoditelja sektora", "zamjenjuje rukovoditelja sektora; ista prava", ScopeSector},
	{RoleSectorAreaDeputy, RoleGroupLevel2, "Zamjenik rukovoditelja sektora za branjeno područje", "imenovanje s razine 2: prava rukovoditelja sektora, ali u okviru svog branjenog područja; u pravilu ista osoba kao rukovoditelj branjenog područja", ScopeArea},
	{RoleCopLeader, RoleGroupLevel2, "Voditelj Centra obrane od poplava", "vodi centar obrane po nalogu rukovoditelja sektora: vodi evidenciju u programu, piše izvješća, koordinira područja i djelatnike; upravlja sektorom", ScopeSector},
	{RoleCopDeputy, RoleGroupLevel2, "Zamjenik voditelja Centra obrane od poplava", "zamjenjuje voditelja centra u istom poslu; ista prava", ScopeSector},
	{RoleOperator, RoleGroupLevel2, "Dežurni operater u COP-u", "privremena dužnost za vrijeme aktivne obrane: dežura u centru, prima očitanja i vodi dnevnik u dosegu zaduženja; upisuje se kao privremena, s rokom", ScopeSector},
	{RoleAreaLeader, RoleGroupLevel3, "Rukovoditelj branjenog područja", "vodi obranu jednog branjenog područja; upravlja područjem i njegovim dionicama", ScopeArea},
	{RoleAreaDeputy, RoleGroupLevel3, "Zamjenik rukovoditelja branjenog područja", "zamjenjuje rukovoditelja područja; ista prava", ScopeArea},
	{RoleContractOfficerA2, RoleGroupLevel3, "Ovlaštenik za praćenje ugovora programa usluga A2", "prati ugovor održavanja u području; upravlja područjem", ScopeArea},
	{RoleContractDeputyA2, RoleGroupLevel3, "Zamjenik ovlaštenika za praćenje ugovora programa usluga A2", "zamjenjuje ovlaštenika A2; ista prava", ScopeArea},
	{RoleContractOfficerA3, RoleGroupLevel3, "Ovlaštenik za praćenje ugovora programa usluga A3", "prati ugovor obrane u području; upravlja područjem", ScopeArea},
	{RoleContractDeputyA3, RoleGroupLevel3, "Zamjenik ovlaštenika za praćenje ugovora programa usluga A3", "zamjenjuje ovlaštenika A3; ista prava", ScopeArea},
	{RoleSectionLeader, RoleGroupLevel4, "Rukovoditelj dionice", "vodi obranu na svojim dionicama (razina 4: dionica); upisuje očitanja i dnevnike za njih", ScopeSection},
	{RoleSectionDeputy, RoleGroupLevel4, "Zamjenik rukovoditelja dionice", "zamjenjuje rukovoditelja dionice; ista prava na njegovim dionicama", ScopeSection},
	{RoleWaterGuard, RoleGroupField, "Vodočuvar", "obilazi dionice i očitava letve; terenski pogled", ScopeSection},
	{RoleMachinist, RoleGroupField, "Strojar", "rukuje crpnim stanicama i ustavama; terenski pogled", ScopeSection},
	{RoleFacilityOperator, RoleGroupField, "Rukovatelj", "rukuje objektom obrane; terenski pogled", ScopeSection},
	{RoleCrewLeader, RoleGroupField, "Voditelj posade objekta", "vodi posadu na objektu; terenski pogled", ScopeSection},
	{RoleServiceLeaderForeman, RoleGroupExternal, "Voditelj usluga / Poslovođa", "vodi radove ugovornog izvođača; vidi teren i vodi vlastite dnevnike", ScopeArea},
	{RoleGuest, RoleGroupGuest, "Gost", "račun za posjetitelja obrane (civilna zaštita, uprava, mediji): gleda teren i vodostaje, ne upisuje", ScopeAll},
	{RoleViewer, RoleGroupProgram, "Preglednik (samo čitanje)", "gleda, ne upisuje", ScopeAll},
}

// Rank je razina s koje se uloga dodjeljuje: 1 uprava organizacije, 2 sektor,
// 3 područje, 4 dionica, 5 teren i ostali. Dužnost smije dati samo tko je na
// toj razini ili iznad nje.
func (r Role) Rank() int {
	switch r {
	case RoleGlobalAdmin:
		return 1
	case RoleAreaAdmin:
		return 2
	case RoleFieldWorker:
		return 5
	}
	for _, d := range RoleCatalog {
		if d.Role == r {
			switch d.Group {
			case RoleGroupLevel1:
				return 1
			case RoleGroupLevel2:
				return 2
			case RoleGroupLevel3:
				return 3
			case RoleGroupLevel4:
				return 4
			}
			return 5
		}
	}
	return 5
}

// GroupLabel je naziv razine s koje uloga dolazi ("Razina 2", "Teren"), onako
// kako uloge razvrstava katalog. Rank daje isti poredak brojem; ovo je za
// ispis, da se na ekranu vidi zašto netko stoji prije nekoga.
func (r Role) GroupLabel() string {
	for _, d := range RoleCatalog {
		if d.Role == r {
			return d.Group
		}
	}
	return ""
}

// NaturalScope je doseg koji uloga sama određuje
func (r Role) NaturalScope() ScopeType {
	switch r {
	case RoleGlobalAdmin:
		return ScopeAll
	case RoleAreaAdmin:
		return ScopeSector
	case RoleFieldWorker:
		return ScopeSection
	}
	for _, d := range RoleCatalog {
		if d.Role == r {
			return d.Scope
		}
	}
	return ScopeSection
}

// Writes javlja daje li uloga pravo upisa; gost i preglednik samo gledaju
func (r Role) Writes() bool { return r != RoleViewer && r != RoleGuest }

// RoleGroups su skupine kataloga redom pojavljivanja
func RoleGroups() []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range RoleCatalog {
		if !seen[d.Group] {
			seen[d.Group] = true
			out = append(out, d.Group)
		}
	}
	return out
}

// DefaultLabel je zadani naziv uloge, bez obzira na postavke organizacije
func (r Role) DefaultLabel() string {
	switch r {
	case RoleGlobalAdmin:
		return "Globalni administrator"
	case RoleAreaAdmin:
		return "Voditelj / Zamjenik COP-a"
	case RoleFieldWorker:
		return RoleWaterGuard.DefaultLabel()
	}
	for _, d := range RoleCatalog {
		if d.Role == r {
			return d.Name
		}
	}
	return string(r)
}

// Label je naziv uloge kako je zove organizacija; bez vlastitog naziva zadani
func (r Role) Label() string {
	key := r
	if r == RoleFieldWorker {
		key = RoleWaterGuard
	}
	if l := Terms().RoleLabels[string(key)]; l != "" {
		return l
	}
	return r.DefaultLabel()
}
