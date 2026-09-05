package models

import "strings"

// Sector predstavlja jedan od 6 vodnogospodarskih sektora (VGO)
type Sector struct {
	ID        string `json:"id"`         // A, B, C, D, E, F
	Name      string `json:"name"`       // npr. "Sektor B — Dunav i donja Drava"
	VgoName   string `json:"vgo_name"`   // npr. "VGO za Dunav i donju Dravu, Osijek"
	CenterCop string `json:"center_cop"` // npr. "COP Osijek"
	Address   string `json:"address"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
}

// Area predstavlja branjeno područje (mali sliv / VGI)
type Area struct {
	ID        int    `json:"id"`        // 1 do 34
	SectorID  string `json:"sector_id"` // A do F
	Name      string `json:"name"`      // npr. "Mali sliv Vuka", "Mali sliv Bistra"
	VgiName   string `json:"vgi_name"`  // npr. "VGI Vuka, Osijek"
	Subcenter string `json:"subcenter"` // npr. "Podcentar Osijek"
}

// SectionInfo predstavlja sažeti opis dionice
type SectionInfo struct {
	Code        string `json:"code"`        // npr. "B.15.1"
	AreaID      int    `json:"area_id"`     // 15
	SectorID    string `json:"sector_id"`   // "B"
	Watercourse string `json:"watercourse"` // npr. "Vuka"
	Description string `json:"description"` // opis dionice
}

// EmbankmentItem predstavlja nasip na dionici
type EmbankmentItem struct {
	Name string `json:"name"` // npr. "Usporni nasip uz l.o. r. Vuke"
	Data string `json:"data"` // npr. "rkm 0+235 - 0+855; km 0+000 - 0+620; (0,620 km)"
}

// StructureItem predstavlja hidrotehnički ili infrastrukturni objekt na dionici
type StructureItem struct {
	Station string `json:"station"` // npr. "rkm 1+825", "km 2+650"
	Name    string `json:"name"`    // npr. "l.o., CS Adica; Q = 0,50 m3/s", "most u Vukovaru"
}

// GaugeItem predstavlja mjerodavni vodomjer s pragovima obrane od poplava
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

// Section predstavlja potpunu štićenu dionicu u sustavu obrane od poplava
type Section struct {
	Code     string `json:"code"`      // npr. "B.15.1" (Primarni ključ)
	AreaID   int    `json:"area_id"`   // npr. 15
	SectorID string `json:"sector_id"` // npr. "B"
	// Description je izvorni opis dionice iz dokumentacije — voda, obala,
	// obuhvat i stacionaža u jednom retku. Ostaje kao tekst; ono što je u
	// njemu strukturirano živi u poljima ispod i tamo se čita.
	Description string `json:"description"` // npr. "rijeka Sava, l.o.; granica - most...; rkm 212+080 - 230+700"

	WatercourseCode string   `json:"watercourse_code"`   // veza na registar vodnih tijela
	WatercourseName string   `json:"watercourse_name"`   // naziv vode iz registra, za prikaz
	Bank            string   `json:"bank"`               // L, D, LD ili prazno
	RkmFrom         *float64 `json:"rkm_from,omitempty"` // početak raspona stacionaže, km
	RkmTo           *float64 `json:"rkm_to,omitempty"`   // kraj raspona stacionaže, km

	ProtectedArea string           `json:"protected_area"` // Ugroženo područje (općine i naselja)
	Embankments   []EmbankmentItem `json:"embankments"`
	Structures    []StructureItem  `json:"structures"`
	Gauges        []GaugeItem      `json:"gauges"`
	Notes         string           `json:"notes"`

	// Parts je građa dionice kakva je u Privitku: poddionice, u svakoj redci,
	// u svakom retku nasipi, objekti, ugroženo područje i vodomjeri. Ravna
	// polja iznad su unije preko svih redaka i ostaju radi popisa i pretrage;
	// tko treba znati na kojem je nasipu "km 0+304", čita odavde.
	Parts     []SectionPart `json:"parts,omitempty"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`

	// Dodatna polja za prikaz
	AreaName   string           `json:"area_name,omitempty"`
	SectorName string           `json:"sector_name,omitempty"`
	Personnel  []SectionOfficer `json:"personnel,omitempty"`
}

// SectionPart je poddionica: jedna ćelija stupca "Vodotok" u Privitku, s
// obalom i stacionažom, i redci koji joj pripadaju
type SectionPart struct {
	Description     string       `json:"description"`
	WatercourseCode string       `json:"watercourse_code,omitempty"` // voda ove poddionice; dionica s više poddionica zna imati više voda
	Bank            string       `json:"bank,omitempty"`
	RkmFrom         *float64     `json:"rkm_from,omitempty"`
	RkmTo           *float64     `json:"rkm_to,omitempty"`
	Rows            []SectionRow `json:"rows"`
}

// SectionRow je jedan redak tablice Privitka: nasipi tog retka, objekti tog
// retka (stacionirani po nasipu, rijeci, potoku ili kanalu), ugroženo
// područje tog retka i vodomjeri tog retka
type SectionRow struct {
	Embankments   []EmbankmentItem `json:"embankments,omitempty"`
	Objects       []DocObject      `json:"objects,omitempty"`
	ProtectedArea string           `json:"protected_area,omitempty"`
	Gauges        []GaugeItem      `json:"gauges,omitempty"`
	// Unaligned označava da se stupci izvorne tablice u wikiju nisu poravnali
	// (colspan), pa raspored polja u retku može biti kriv; traži ručnu provjeru
	Unaligned bool `json:"unaligned,omitempty"`
}

// DocObject je objekt iz dokumentacije dionice s vrstom stacionaže
type DocObject struct {
	Kind       string `json:"kind,omitempty"` // rkm (rijeka), km (nasip), pkm (potok), kkm (kanal), prazno
	Stationing string `json:"stationing,omitempty"`
	Name       string `json:"name"`
}

// KindLabel kaže po čemu se objekt stacionira
func (o DocObject) KindLabel() string {
	switch o.Kind {
	case "rkm":
		return "po rijeci"
	case "km":
		return "po nasipu"
	case "pkm":
		return "po potoku"
	case "kkm":
		return "po kanalu"
	}
	return ""
}

// Gauges su mjerodavni vodomjeri poddionice: unija po redcima, bez ponavljanja.
// Redci iste poddionice gotovo uvijek dijele vodomjer, pa se prikazuje jednom.
func (p SectionPart) Gauges() []GaugeItem {
	var out []GaugeItem
	seen := map[string]bool{}
	for _, r := range p.Rows {
		for _, g := range r.Gauges {
			k := g.StationName + "|" + g.PrepCm + "|" + g.RegularCm
			if !seen[k] {
				seen[k] = true
				out = append(out, g)
			}
		}
	}
	return out
}

// Unaligned javlja ima li poddionica redak s neporavnanim stupcima
func (p SectionPart) Unaligned() bool {
	for _, r := range p.Rows {
		if r.Unaligned {
			return true
		}
	}
	return false
}

// HasParts javlja ima li dionica strukturirani prijepis
func (s Section) HasParts() bool { return len(s.Parts) > 0 }

// IsGauge javlja je li zapis pravi vodomjer s pragovima u centimetrima, a ne
// mjerilo druge vrste: kota na mostu u metrima nadmorske visine, pravilnik
// retencije, uputa "prema prognozi". Takva mjerila vrijede za ljude na
// dionici, ali se iz njih ne stvara vodomjerna postaja.
func (g GaugeItem) IsGauge() bool {
	name := strings.TrimSpace(g.StationName)
	if name == "" || len(name) > 70 {
		return false
	}
	for _, p := range []string{"R:", "P =", "P=", "Prema ", "V na brani", "Po pravilniku", "upravljanje"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	// bar jedan prag mora biti u centimetrima: broj s predznakom ili bez, bez "m.n.m"
	for _, v := range []string{g.PrepCm, g.RegularCm, g.EmergCm, g.CriticalCm} {
		v = strings.TrimSpace(v)
		if v == "" || strings.Contains(strings.ToLower(v), "m.n.m") || strings.Contains(strings.ToLower(v), "n.j.m") {
			continue
		}
		return true
	}
	return false
}

// SectionOfficer predstavlja djelatnika zaduženog za dionicu ili pripadajuće branjeno područje
type SectionOfficer struct {
	UserID      string `json:"user_id"`
	FullName    string `json:"full_name"`
	Title       string `json:"title"`
	DutyTitle   string `json:"duty_title"`
	Role        string `json:"role"`
	RoleLabel   string `json:"role_label"`
	Phone       string `json:"phone"`
	MobilePhone string `json:"mobile_phone"`
	Email       string `json:"email"`
	OrgName     string `json:"org_name"`
}
