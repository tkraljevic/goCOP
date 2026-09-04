package models

import (
	"time"

	"github.com/google/uuid"
)

// Reading je jedno očitanje vodostaja na letvi. Letva pripada ili vodomjernoj
// postaji (StationID) ili objektu — crpnoj stanici, ustavi (StructureID);
// nikad oboma. Sva tri oblika iz Baranje (vodostaj na CS, uzvodni/nizvodni na
// ustavi, ostali vodomjeri) idu u isti tok: razlikuje ih samo što je popunjeno.
//
// Source govori KAKO je vrijednost dobivena (ručno očitanje, automatska
// postaja, uvoz bez poznatog načina), Origin ODAKLE zapis potječe (goCOP,
// evidencija VGI Baranja…). Uvezeno ručno očitanje ostaje RUČNO — to je
// informacija koju prognoza treba — a Origin i SourceRef čuvaju trag uvoza.
type Reading struct {
	ID          uuid.UUID `json:"id"`
	StationID   string    `json:"station_id,omitempty"`
	StructureID string    `json:"structure_id,omitempty"`
	MeasuredAt  time.Time `json:"measured_at"` // UTC; prikaz u hrvatskom vremenu

	LevelCm  *int `json:"level_cm,omitempty"`  // vodostaj; na ustavi uzvodni
	Level2Cm *int `json:"level2_cm,omitempty"` // nizvodni vodostaj na ustavi

	Source    string `json:"source"`               // ReadingSource*
	Origin    string `json:"origin,omitempty"`     // GOCOP, DIRECTUS_BP16 …
	SourceRef string `json:"source_ref,omitempty"` // npr. directus:vodostaji_na_crpnim_stanicama:26860
	Observer  string `json:"observer,omitempty"`   // tko je očitao, kako je zapisano
	UserID    string `json:"user_id,omitempty"`    // korisnik goCOP-a koji je upisao

	StructureState string `json:"structure_state,omitempty"` // stanje crpne stanice, StructureState*
	Gate           string `json:"gate,omitempty"`            // zapornica ustave, Gate*
	AgHours1       *int   `json:"ag_hours_1,omitempty"`      // sati rada agregata
	AgHours2       *int   `json:"ag_hours_2,omitempty"`
	AgHours3       *int   `json:"ag_hours_3,omitempty"`

	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju, ne putuje između čvorova
	GaugeName string       `json:"-"`
	Phase     DefensePhase `json:"-"`
	UserName  string       `json:"-"`
}

// Načini dobivanja vrijednosti
const (
	ReadingSourceManual    = "RUČNO"
	ReadingSourceAutomatic = "AUTOMATSKI"
	ReadingSourceImport    = "UVOZ"
)

// ReadingSources su načini redom kojim ih obrazac nudi
var ReadingSources = []string{ReadingSourceManual, ReadingSourceAutomatic, ReadingSourceImport}

// Stanja crpne stanice pri očitanju
const (
	StructureStateIdle      = "MIROVANJE"
	StructureStateStarting  = "POKRETANJE"
	StructureStateStopping  = "ZAUSTAVLJANJE"
	StructureStateSiphoning = "SIFONIRANJE"
	StructureStateFault     = "KVAR"
)

var StructureStates = []string{StructureStateIdle, StructureStateStarting, StructureStateStopping, StructureStateSiphoning, StructureStateFault}

// Položaji zapornice ustave
const (
	GateOpen    = "OTVORENA"
	GateClosed  = "ZATVORENA"
	GatePartial = "DJELOMIČNO"
)

var Gates = []string{GateClosed, GateOpen, GatePartial}

// Podrijetla zapisa
const (
	ReadingOriginGoCOP   = "GOCOP"
	ReadingOriginBP16    = "DIRECTUS_BP16"
	ReadingOriginArchive = "ARHIVA"
)

// Zagreb je vremenska zona u kojoj se očitanja upisuju i prikazuju.
// Baza čuva UTC; tzdata je ugrađen u program da i alpine kontejner zna zonu.
var Zagreb = loadZagreb()

func loadZagreb() *time.Location {
	if loc, err := time.LoadLocation("Europe/Zagreb"); err == nil {
		return loc
	}
	return time.FixedZone("CET", 3600)
}

// LocalTime je trenutak očitanja u hrvatskom vremenu
func (r Reading) LocalTime() time.Time { return r.MeasuredAt.In(Zagreb) }

// OnStructure javlja pripada li letva objektu (a ne vodomjernoj postaji)
func (r Reading) OnStructure() bool { return r.StructureID != "" }

// GaugeKey jednoznačno imenuje letvu radi grupiranja
func (r Reading) GaugeKey() string {
	if r.StructureID != "" {
		return "structure:" + r.StructureID
	}
	return "station:" + r.StationID
}

// GaugeURL vodi na povijest očitanja te letve
func (r Reading) GaugeURL() string {
	if r.StructureID != "" {
		return "/readings/structure/" + r.StructureID
	}
	return "/readings/station/" + r.StationID
}

// HasLevel javlja je li vodostaj uopće očitan (na ustavi bar jedan od dva)
func (r Reading) HasLevel() bool { return r.LevelCm != nil || r.Level2Cm != nil }

func (r Reading) SourceLabel() string { return ReadingSourceLabel(r.Source) }

func ReadingSourceLabel(source string) string {
	switch source {
	case ReadingSourceManual:
		return "ručno"
	case ReadingSourceAutomatic:
		return "automatski"
	case ReadingSourceImport:
		return "uvoz"
	}
	return source
}

// OriginLabel govori odakle zapis potječe; za goCOP je prazan jer je to zadano
func (r Reading) OriginLabel() string {
	switch r.Origin {
	case ReadingOriginBP16:
		return "evidencija VGI Baranja"
	case ReadingOriginArchive:
		return "arhiva"
	case ReadingOriginGoCOP, "":
		return ""
	}
	return r.Origin
}

func (r Reading) StateLabel() string { return StructureStateLabel(r.StructureState) }

func StructureStateLabel(state string) string {
	switch state {
	case StructureStateIdle:
		return "mirovanje"
	case StructureStateStarting:
		return "pokretanje"
	case StructureStateStopping:
		return "zaustavljanje"
	case StructureStateSiphoning:
		return "sifoniranje"
	case StructureStateFault:
		return "kvar"
	}
	return ""
}

func (r Reading) GateLabel() string { return GateLabel(r.Gate) }

func GateLabel(gate string) string {
	switch gate {
	case GateOpen:
		return "otvorena"
	case GateClosed:
		return "zatvorena"
	case GatePartial:
		return "djelomično otvorena"
	}
	return ""
}

// HasAgHours javlja jesu li zapisani sati rada agregata
func (r Reading) HasAgHours() bool {
	return r.AgHours1 != nil || r.AgHours2 != nil || r.AgHours3 != nil
}

// GaugeSummary je sažetak jedne letve za pregled vodostaja: zadnje očitanje,
// promjena prema prethodnom i faza obrane ako postaja ima pragove.
type GaugeSummary struct {
	Key         string
	Name        string
	Sub         string // voda, stacionaža ili vrsta objekta
	URL         string
	NewURL      string
	StationID   string
	StructureID string
	SectorID    string
	AreaID      int
	Kind        string // vrsta objekta ili "POSTAJA"
	Latest      *Reading
	Previous    *Reading
	Phase       DefensePhase
	Count       int
}

// Trend je promjena vodostaja prema prethodnom očitanju u cm; nil kad nema usporedbe
func (g GaugeSummary) Trend() *int {
	if g.Latest == nil || g.Previous == nil || g.Latest.LevelCm == nil || g.Previous.LevelCm == nil {
		return nil
	}
	d := *g.Latest.LevelCm - *g.Previous.LevelCm
	return &d
}

// Age javlja koliko je zadnje očitanje staro u odnosu na sadašnji trenutak
func (g GaugeSummary) Age() time.Duration {
	if g.Latest == nil {
		return 0
	}
	return time.Since(g.Latest.MeasuredAt)
}

// Stale javlja da je zadnje očitanje starije od 36 sati — na terenu se čita svako jutro
func (g GaugeSummary) Stale() bool { return g.Latest != nil && g.Age() > 36*time.Hour }
