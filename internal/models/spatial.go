package models

import (
	"fmt"
	"strings"
)

// Sector je ustrojstvena jedinica sa svojim centrom obrane. Razina 1 je
// krovna jedinica organizacije (kod Hrvatskih voda Direkcija s Glavnim
// centrom), razina 2 su sektori (VGO s COP-om). Obje se vode istom tablicom
// jer imaju isti oblik: jedinica, centar, adresa i kontakti.
type Sector struct {
	ID        string `json:"id"`         // A, B, C, D, E, F ili DIREKCIJA
	Name      string `json:"name"`       // npr. "Sektor B — Dunav i donja Drava"
	VgoName   string `json:"vgo_name"`   // npr. "VGO za Dunav i donju Dravu, Osijek"
	CenterCop string `json:"center_cop"` // npr. "COP Osijek"
	Address   string `json:"address"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Level     int    `json:"level"` // 1 krovna jedinica, 2 sektor (zadano)
}

// IsLevel1 javlja je li jedinica krovna (razina 1)
func (s Sector) IsLevel1() bool { return s.Level == 1 }

// Area predstavlja branjeno područje (mali sliv / VGI)
type Area struct {
	ID             int    `json:"id"`                         // 1 do 34
	SectorID       string `json:"sector_id"`                  // A do F
	Name           string `json:"name"`                       // npr. "Mali sliv Vuka", "Mali sliv Bistra"
	VgiName        string `json:"vgi_name"`                   // npr. "VGI Vuka, Osijek"
	Subcenter      string `json:"subcenter"`                  // npr. "Podcentar Osijek"
	ContractorName string `json:"contractor_name,omitempty"`  // ugovorna pravna osoba za obranu
	DirectToSector bool   `json:"direct_to_sector,omitempty"` // bez ispostave: pripada izravno sektoru (npr. B.34)
}

// SectionInfo predstavlja sažeti opis dionice
type SectionInfo struct {
	Code        string `json:"code"`        // npr. "B.15.1"
	AreaID      int    `json:"area_id"`     // 15
	SectorID    string `json:"sector_id"`   // "B"
	Watercourse string `json:"watercourse"` // npr. "Vuka"
	Description string `json:"description"` // opis dionice
}

// GaugeItem je mjerodavni vodomjer kako stoji u dokumentaciji: naziv i pragovi
// kao tekst. Iz njega punjenje izvodi postaju u registru; sam tekst ostaje uz
// poddionicu kao izvorni zapis i kao kriterij koji nije vodomjer (kota na
// mostu, pravilnik retencije).
type GaugeItem struct {
	StationName string `json:"station_name"` // npr. "Vukovar , rkm 1.333,45 (76,19)"
	PrepCm      string `json:"prep_cm"`      // Pripremno stanje (P) npr. "+530"
	RegularCm   string `json:"regular_cm"`   // Redovna obrana (R) npr. "+580"
	EmergCm     string `json:"emerg_cm"`     // Izvanredna obrana (I) npr. "+630"
	CriticalCm  string `json:"critical_cm"`  // Izvanredno stanje (IS) npr. "+680"
	RecordCm    string `json:"record_cm"`    // Najviši ikad (M) npr. "+769 (26.06.1965.)"
	Notes       string `json:"notes"`        // Napomena
	FromText    bool   `json:"-"`            // pročitan iz proznog retka, ne iz tablice (samo pri prijepisu)
}

// Section je štićena dionica: šifra i područje, a sve ostalo živi u
// poddionicama. Dionica s jednim vodotokom ima jednu poddionicu; ista je
// građa za sve, pa nema ravnih polja koja bi ponavljala poddionicu.
//
// Stupci description, watercourse_code, bank, rkm_from i rkm_to u tablici su
// izvedeni iz prve poddionice radi popisa i pretrage; pišu se pri svakom
// upisu i nitko ih ne uređuje zasebno.
type Section struct {
	Code     string `json:"code"`      // npr. "B.15.1" (primarni ključ)
	AreaID   int    `json:"area_id"`   // npr. 15
	SectorID string `json:"sector_id"` // izveden iz područja

	// Opis se slaže iz poddionica; kad ga netko prepiše rukom, ostaje njegov
	Description       string `json:"description"`
	DescriptionCustom bool   `json:"description_custom"`

	LengthKm     *float64 `json:"length_km,omitempty"`     // ukupna duljina dionice
	EmbankmentKm *float64 `json:"embankment_km,omitempty"` // ukupno nasipa

	Parts []SectionPart `json:"parts"`
	Notes string        `json:"notes"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	// Izvedeno pri čitanju
	WatercourseCode string           `json:"watercourse_code,omitempty"` // voda prve poddionice
	WatercourseName string           `json:"watercourse_name,omitempty"`
	AreaName        string           `json:"area_name,omitempty"`
	SectorName      string           `json:"sector_name,omitempty"`
	Personnel       []SectionOfficer `json:"personnel,omitempty"`
}

// SectionPart je poddionica: jedna voda s jednim obuhvatom, i sve što se na
// tom obuhvatu štiti — ugroženo područje, mjerodavni vodomjeri, objekti i
// nasipi. Ista voda u dva retka Privitka (lijevi pa desni nasip) jedna je
// poddionica s dva nasipa.
type SectionPart struct {
	Seq             int      `json:"seq"`                        // redni broj u dionici, od 1
	WatercourseCode string   `json:"watercourse_code,omitempty"` // voda iz registra
	WatercourseName string   `json:"watercourse_name,omitempty"` // izvedeno pri čitanju
	StationingKind  string   `json:"stationing_kind,omitempty"`  // rkm, pkm, bkm, kkm
	KmFrom          *float64 `json:"km_from,omitempty"`
	KmTo            *float64 `json:"km_to,omitempty"`
	Bank            string   `json:"bank,omitempty"` // L, D, LD
	Extent          string   `json:"extent,omitempty"`
	LengthKm        *float64 `json:"length_km,omitempty"`

	// Izvorni tekst iz dokumentacije, kad poddionica iz nje potječe
	Description   string `json:"description,omitempty"`
	ProtectedText string `json:"protected_text,omitempty"`
	Unaligned     bool   `json:"unaligned,omitempty"` // stupci Privitka nisu se poravnali pri prijepisu

	Territories []PartTerritory  `json:"territories,omitempty"`
	StationIDs  []string         `json:"station_ids,omitempty"`
	Gauges      []GaugeItem      `json:"gauges,omitempty"` // izvorni zapis vodomjera i kriterija
	Objects     []PartObject     `json:"objects,omitempty"`
	Embankments []PartEmbankment `json:"embankments,omitempty"`
}

// PartTerritory je ugroženo naselje, općina ili grad poddionice
type PartTerritory struct {
	CountyID       int  `json:"county_id"`
	MunicipalityID int  `json:"municipality_id"`
	SettlementID   *int `json:"settlement_id,omitempty"`
}

// PartObject je objekt na poddionici: naš objekt iz registra (crpna stanica,
// ustava, sifon) vezan je preko StructureID; mostovi i propusti tuđi su i
// ostaju samo redak s nazivom.
type PartObject struct {
	StructureID    string   `json:"structure_id,omitempty"`
	Bank           string   `json:"bank,omitempty"`
	StationingKind string   `json:"stationing_kind,omitempty"` // rkm, pkm, bkm, kkm, nkm
	Stationing     *float64 `json:"stationing,omitempty"`      // km
	StationingText string   `json:"stationing_text,omitempty"` // kako je zapisano: "rkm 1+825"
	Name           string   `json:"name"`
	OnEmbankment   string   `json:"on_embankment,omitempty"` // naziv nasipa po kojem je stacioniran

	// Izvedeno pri čitanju
	StructureName string `json:"-"`
	StructureKind string `json:"-"`
}

// PartEmbankment je nasip ili brana na poddionici: građevina je u registru
// objekata, a ovdje stoji njezin odsjek na ovom obuhvatu
type PartEmbankment struct {
	StructureID string   `json:"structure_id,omitempty"`
	Name        string   `json:"name"` // naziv iz dokumentacije, i kad je vezan
	WaterKind   string   `json:"water_kind,omitempty"`
	WaterFrom   *float64 `json:"water_from,omitempty"` // uz vodu
	WaterTo     *float64 `json:"water_to,omitempty"`
	EmbFrom     *float64 `json:"emb_from,omitempty"` // po nasipu
	EmbTo       *float64 `json:"emb_to,omitempty"`
	LengthKm    *float64 `json:"length_km,omitempty"`
	Data        string   `json:"data,omitempty"` // izvorni zapis

	// Izvedeno pri čitanju
	StructureKind string `json:"-"`
}

// Oznake obale za obrazac
var Banks = []struct{ Code, Label string }{{"L", "lijeva obala"}, {"D", "desna obala"}, {"LD", "obje obale"}}

// FirstPart vraća prvu poddionicu, ili praznu kad ih nema
func (s Section) FirstPart() SectionPart {
	if len(s.Parts) > 0 {
		return s.Parts[0]
	}
	return SectionPart{}
}

// Bank je obala prve poddionice, za popise
func (s Section) Bank() string { return s.FirstPart().Bank }

// RkmFrom i RkmTo su raspon prve poddionice, za popise
func (s Section) RkmFrom() *float64 { return s.FirstPart().KmFrom }
func (s Section) RkmTo() *float64   { return s.FirstPart().KmTo }

// Length je ukupna duljina: upisana, ili zbroj poddionica
func (s Section) Length() float64 {
	if s.LengthKm != nil {
		return *s.LengthKm
	}
	sum := 0.0
	for _, p := range s.Parts {
		sum += p.Length()
	}
	return sum
}

// EmbankmentLength je ukupno nasipa: upisano, ili zbroj odsjeka
func (s Section) EmbankmentLength() float64 {
	if s.EmbankmentKm != nil {
		return *s.EmbankmentKm
	}
	sum := 0.0
	for _, p := range s.Parts {
		for _, e := range p.Embankments {
			if e.LengthKm != nil {
				sum += *e.LengthKm
			}
		}
	}
	return sum
}

// ComposeDescription slaže opis iz poddionica, kako ga i Privitak piše:
// voda, obala; obuhvat; stacionaža; (duljina). Više poddionica odvaja se točkom.
func (s Section) ComposeDescription() string {
	var parts []string
	for _, p := range s.Parts {
		parts = append(parts, p.Compose())
	}
	out := strings.Join(parts, " · ")
	if len(s.Parts) > 1 && s.Length() > 0 {
		out += fmt.Sprintf(" (%s km ukupno)", fmtKm(s.Length()))
	}
	return out
}

// EffectiveDescription je opis kakav se prikazuje: ručni kad postoji, inače
// složeni. Složeni se pri upisu sprema s nazivima voda, pa ima prednost pred
// slaganjem iz poddionica koje nazive još nisu dobile.
func (s Section) EffectiveDescription() string {
	if strings.TrimSpace(s.Description) != "" {
		return s.Description
	}
	return s.ComposeDescription()
}

// AllStationIDs su vodomjeri svih poddionica, bez ponavljanja
func (s Section) AllStationIDs() []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range s.Parts {
		for _, id := range p.StationIDs {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// AllStructureIDs su objekti i nasipi iz registra na svim poddionicama
func (s Section) AllStructureIDs() []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, p := range s.Parts {
		for _, o := range p.Objects {
			add(o.StructureID)
		}
		for _, e := range p.Embankments {
			add(e.StructureID)
		}
	}
	return out
}

// AllTerritories su ugrožena područja svih poddionica, bez ponavljanja
func (s Section) AllTerritories() []PartTerritory {
	var out []PartTerritory
	seen := map[string]bool{}
	for _, p := range s.Parts {
		for _, t := range p.Territories {
			k := t.Key()
			if !seen[k] {
				seen[k] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// Key jednoznačno označava teritorij
func (t PartTerritory) Key() string {
	if t.SettlementID != nil {
		return fmt.Sprintf("%d/%d/%d", t.CountyID, t.MunicipalityID, *t.SettlementID)
	}
	return fmt.Sprintf("%d/%d", t.CountyID, t.MunicipalityID)
}

// ProtectedSummary spaja ugroženo područje svih poddionica, za popise
func (s Section) ProtectedSummary() string {
	var out []string
	seen := map[string]bool{}
	for _, p := range s.Parts {
		if t := strings.TrimSpace(p.ProtectedText); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return strings.Join(out, "; ")
}

// ObjectCount, EmbankmentCount i GaugeCount broje kroz sve poddionice
func (s Section) ObjectCount() int {
	n := 0
	for _, p := range s.Parts {
		n += len(p.Objects)
	}
	return n
}

func (s Section) EmbankmentCount() int {
	n := 0
	for _, p := range s.Parts {
		n += len(p.Embankments)
	}
	return n
}

func (s Section) GaugeCount() int { return len(s.AllStationIDs()) }

// Unaligned javlja ima li dionica poddionicu s neporavnanim stupcima
func (s Section) Unaligned() bool {
	for _, p := range s.Parts {
		if p.Unaligned {
			return true
		}
	}
	return false
}

// Length je duljina poddionice: upisana, ili iz raspona
func (p SectionPart) Length() float64 {
	if p.LengthKm != nil {
		return *p.LengthKm
	}
	if p.KmFrom != nil && p.KmTo != nil {
		d := *p.KmTo - *p.KmFrom
		if d < 0 {
			d = -d
		}
		return d
	}
	return 0
}

// Compose slaže opis poddionice kako ga Privitak piše
func (p SectionPart) Compose() string {
	var segs []string
	head := p.WatercourseName
	if head == "" {
		head = p.WatercourseCode
	}
	if b := BankShort(p.Bank); b != "" {
		head += ", " + b
	}
	if head != "" {
		segs = append(segs, head)
	}
	if p.Extent != "" {
		segs = append(segs, p.Extent)
	}
	if p.KmFrom != nil && p.KmTo != nil {
		segs = append(segs, fmt.Sprintf("%s %s - %s", p.StationingKind, fmtStationing(*p.KmFrom), fmtStationing(*p.KmTo)))
	}
	if l := p.Length(); l > 0 {
		segs = append(segs, fmt.Sprintf("(%s km)", fmtKm(l)))
	}
	if len(segs) == 0 {
		return p.Description
	}
	return strings.Join(segs, "; ")
}

// BankShort vraća obalu kako je Privitak krati
func BankShort(bank string) string {
	switch bank {
	case "L":
		return "l.o."
	case "D":
		return "d.o."
	case "LD":
		return "l.o. i d.o."
	}
	return ""
}

// RangeLabel vraća raspon stacionaže poddionice za prikaz: "pkm 0+000 – 32+490"
func (p SectionPart) RangeLabel() string {
	if p.KmFrom == nil || p.KmTo == nil {
		return ""
	}
	return strings.TrimSpace(p.StationingKind + " " + fmtStationing(*p.KmFrom) + " – " + fmtStationing(*p.KmTo))
}

// fmtStationing piše kilometre kao "12+450"
func fmtStationing(km float64) string {
	whole := int(km)
	m := int((km-float64(whole))*1000 + 0.5)
	if m >= 1000 {
		whole++
		m -= 1000
	}
	return fmt.Sprintf("%d+%03d", whole, m)
}

// FormatKm piše kilometre s tri decimale i zarezom, kako ih dokumentacija piše
func FormatKm(km float64) string { return fmtKm(km) }

// fmtKm piše kilometre s tri decimale i zarezom, kako ih dokumentacija piše
func fmtKm(km float64) string {
	return strings.Replace(fmt.Sprintf("%.3f", km), ".", ",", 1)
}

// Stationing vraća stacionažu objekta za prikaz
func (o PartObject) StationingLabel() string {
	if o.StationingText != "" {
		return o.StationingText
	}
	if o.Stationing != nil {
		return strings.TrimSpace(o.StationingKind + " " + fmtStationing(*o.Stationing))
	}
	return ""
}

// Range vraća odsjek nasipa za prikaz
func (e PartEmbankment) Range() string {
	var segs []string
	if e.WaterFrom != nil && e.WaterTo != nil {
		segs = append(segs, fmt.Sprintf("%s %s - %s", e.WaterKind, fmtStationing(*e.WaterFrom), fmtStationing(*e.WaterTo)))
	}
	if e.EmbFrom != nil && e.EmbTo != nil {
		segs = append(segs, fmt.Sprintf("nkm %s - %s", fmtStationing(*e.EmbFrom), fmtStationing(*e.EmbTo)))
	}
	if e.LengthKm != nil {
		segs = append(segs, fmt.Sprintf("(%s km)", fmtKm(*e.LengthKm)))
	}
	if len(segs) == 0 {
		return e.Data
	}
	return strings.Join(segs, "; ")
}

// IsGauge javlja je li zapis mjerilo vodostaja (letva, kota na mostu ili
// brani), a ne uputa: pravilnik retencije, "prema prognozi", pravilo
// upravljanja. Upute vrijede za ljude na dionici, ali se iz njih ne stvara
// vodomjerna postaja; što je mjerilo, a nema ni stacionažu ni prag, odbacuje
// punjenje registra postaja.
func (g GaugeItem) IsGauge() bool {
	name := strings.TrimSpace(g.StationName)
	if name == "" {
		return false
	}
	for _, p := range []string{"R:", "P =", "P=", "Prema ", "V na brani", "Po pravilniku", "upravljanje"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}

// SectionOfficer predstavlja djelatnika zaduženog za dionicu ili pripadajuće branjeno područje
// OfficerGroup je skupina zaduženih iste razine ovlasti. Kartica ih dijeli jer
// se na njoj traži jedno ime — najčešće rukovoditelj dionice — a ne čita se
// popis od dvadesetak ljudi redom.
type OfficerGroup struct {
	Label   string
	Rank    int
	Members []SectionOfficer
}

// PersonnelByLevel dijeli zadužene po razini, redom kojim su već složeni
// (odozgo prema dionici, pa teren). Naslov skupine uz razinu nosi i ono što ta
// razina znači, nazivima koje organizacija koristi.
func (s Section) PersonnelByLevel() []OfficerGroup {
	t := Terms()
	znacenje := map[int]string{
		1: t.Lower("org"),
		2: t.Lower("sektor"),
		3: t.Lower("podrucje"),
		4: "dionica",
	}
	var out []OfficerGroup
	for _, o := range s.Personnel {
		if n := len(out); n > 0 && out[n-1].Rank == o.Rank {
			out[n-1].Members = append(out[n-1].Members, o)
			continue
		}
		label := o.RoleGroup
		if z := znacenje[o.Rank]; z != "" && label != "" {
			label += " — " + z
		}
		out = append(out, OfficerGroup{Label: label, Rank: o.Rank, Members: []SectionOfficer{o}})
	}
	return out
}

type SectionOfficer struct {
	UserID      string `json:"user_id"`
	FullName    string `json:"full_name"`
	Title       string `json:"title"`
	DutyTitle   string `json:"duty_title"`
	Role        string `json:"role"`
	RoleLabel   string `json:"role_label"`
	RoleGroup   string `json:"role_group"` // "Razina 2", "Teren" — po katalogu uloga
	Rank        int    `json:"rank"`       // 1 uprava … 5 teren i ostali; određuje poredak
	Phone       string `json:"phone"`
	MobilePhone string `json:"mobile_phone"`
	Email       string `json:"email"`
	OrgName     string `json:"org_name"`
}
