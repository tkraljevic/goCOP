package models

// Sudionici obrane: tko sve u obrani od poplava ima ulogu i što mu ona daje.
// Šifre uloga nose ovlasti i ne mijenjaju se; nazive organizacija smije
// zvati po svome (OrgTerms.RoleLabels), jer druga vodna organizacija
// vodočuvara ili rukovoditelja zove drukčije.

// RoleDef je jedna uloga u katalogu sudionika obrane
type RoleDef struct {
	Role  Role
	Group string // razina ustroja ili mjesto u obrani kojem uloga pripada
	Name  string // zadani naziv (Hrvatske vode)
	Desc  string // što uloga smije u programu
}

// Skupine sudionika, redom od vrha ustroja prema terenu
const (
	RoleGroupLevel1   = "Razina 1"
	RoleGroupLevel2   = "Razina 2"
	RoleGroupLevel3   = "Razina 3"
	RoleGroupSection  = "Dionica"
	RoleGroupCenter   = "Centar obrane"
	RoleGroupField    = "Teren"
	RoleGroupExternal = "Vanjski suradnici"
	RoleGroupProgram  = "Program"
)

// RoleCatalog su sve uloge koje program poznaje, redom kojim se nude u
// obrascima: od najviše prema terenu
var RoleCatalog = []RoleDef{
	{RoleNationalLeader, RoleGroupLevel1, "Glavni rukovoditelj (za cijelu RH)", "vodi obranu cijele organizacije; puna prava u programu"},
	{RoleNationalDeputy, RoleGroupLevel1, "Zamjenik Glavnog rukovoditelja (za cijelu RH)", "zamjenjuje glavnog rukovoditelja; puna prava"},
	{RoleSectorMainDeputy, RoleGroupLevel2, "Zamjenik Glavnog rukovoditelja za sektor", "vodi obranu jednog sektora u ime glavnog rukovoditelja; upravlja sektorom"},
	{RoleSectorLeader, RoleGroupLevel2, "Rukovoditelj sektora", "vodi obranu sektora; upravlja sektorom i njegovim područjima"},
	{RoleSectorDeputy, RoleGroupLevel2, "Zamjenik rukovoditelja sektora", "zamjenjuje rukovoditelja sektora; ista prava"},
	{RoleSectorAreaDeputy, RoleGroupLevel2, "Zamjenik rukovoditelja sektora za branjeno područje", "iz sektora vodi jedno branjeno područje; upravlja tim područjem"},
	{RoleCopLeader, RoleGroupCenter, "Voditelj Centra obrane od poplava", "vodi centar obrane sektora; upravlja sektorom i djelatnicima"},
	{RoleCopDeputy, RoleGroupCenter, "Zamjenik voditelja Centra obrane od poplava", "zamjenjuje voditelja centra; ista prava"},
	{RoleAreaLeader, RoleGroupLevel3, "Rukovoditelj branjenog područja", "vodi obranu jednog branjenog područja; upravlja područjem i njegovim dionicama"},
	{RoleAreaDeputy, RoleGroupLevel3, "Zamjenik rukovoditelja branjenog područja", "zamjenjuje rukovoditelja područja; ista prava"},
	{RoleSectionLeader, RoleGroupSection, "Rukovoditelj dionice", "vodi obranu na svojim dionicama; upisuje očitanja i dnevnike za njih"},
	{RoleSectionDeputy, RoleGroupSection, "Zamjenik rukovoditelja dionice", "zamjenjuje rukovoditelja dionice; ista prava na njegovim dionicama"},
	{RoleContractOfficerA2, RoleGroupLevel3, "Ovlaštenik za praćenje ugovora programa usluga A2", "prati ugovor održavanja u području; upravlja područjem"},
	{RoleContractOfficerA3, RoleGroupLevel3, "Ovlaštenik za praćenje ugovora programa usluga A3", "prati ugovor obrane u području; upravlja područjem"},
	{RoleServiceLeaderForeman, RoleGroupExternal, "Voditelj usluga / Poslovođa", "vodi radove ugovornog izvođača; vidi teren i vodi vlastite dnevnike"},
	{RoleOperator, RoleGroupCenter, "Dežurni operater u COP-u", "dežura u centru; upisuje očitanja i dnevnike u dosegu zaduženja"},
	{RoleWaterGuard, RoleGroupField, "Vodočuvar", "obilazi dionice i očitava letve; terenski pogled"},
	{RoleMachinist, RoleGroupField, "Strojar", "rukuje crpnim stanicama i ustavama; terenski pogled"},
	{RoleFacilityOperator, RoleGroupField, "Rukovatelj", "rukuje objektom obrane; terenski pogled"},
	{RoleCrewLeader, RoleGroupField, "Voditelj posade objekta", "vodi posadu na objektu; terenski pogled"},
	{RoleViewer, RoleGroupProgram, "Preglednik (samo čitanje)", "gleda, ne upisuje"},
}

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
