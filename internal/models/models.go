package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DefensePhase predstavlja fazu obrane od poplava
type DefensePhase string

const (
	PhaseUnknown   DefensePhase = "NEPOZNATO"
	PhaseNormal    DefensePhase = "NORMALNO"
	PhasePrep      DefensePhase = "PRIPREMNO"
	PhaseRegular   DefensePhase = "REDOVNA"
	PhaseEmergency DefensePhase = "IZVANREDNA"
	PhaseState     DefensePhase = "IZVANREDNO_STANJE"
)

// Tendency opisuje smjer kretanja vodostaja
type Tendency string

const (
	TendencyRising   Tendency = "RASTE"
	TendencyFalling  Tendency = "OPADA"
	TendencyStagnant Tendency = "STAGNIRA"
	TendencyUnknown  Tendency = "NEPOZNATO"
)

// Threshold predstavlja jedan prag obrane od poplava.
//
// Cm je popunjen isključivo kad je prag izražen u centimetrima na vodomjeru i
// smije ući u automatski izračun faze obrane. Pragovi zadani kao apsolutna kota
// ("206,30 m n. m.") ili kao uputa ("Prema Pravilniku akumulacije Borovik")
// ostaju sačuvani u Raw, ali se NE preračunavaju — kota nule ponegdje nedostaje
// ili je upisana kao 0,00, pa bi preračun dao krivu fazu obrane na ekranu.
type Threshold struct {
	Cm  *int   `json:"cm,omitempty"`  // vrijednost u cm na vodomjeru
	Raw string `json:"raw,omitempty"` // izvorni zapis iz dokumentacije dionice
}

// IsUsable govori smije li se prag koristiti za automatski izračun faze obrane
func (t Threshold) IsUsable() bool {
	return t.Cm != nil
}

// Label vraća zapis praga za prikaz — brojčanu vrijednost ili izvorni tekst
func (t Threshold) Label() string {
	if t.Cm != nil {
		return fmt.Sprintf("%+d cm", *t.Cm)
	}
	if raw := strings.TrimSpace(t.Raw); raw != "" {
		return raw
	}
	return "—"
}

// Station predstavlja hidrološku mjernu postaju (vodomjer) Hrvatskih voda.
// Jedna postaja mjerodavna je za više štićenih dionica — veza se drži u
// tablici section_stations, a SectionCodes popunjava repozitorij pri čitanju.
type Station struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
	// Watercourse je voda na kojoj vodomjer FIZIČKI STOJI (Batina i Vukovar na
	// Dunavu). To nije isto što i vode za koje je postaja mjerodavna — Batina je
	// mjerodavna i za dionice potoka Karašice, osobito na ušću u Dunav; ta veza
	// se drži u section_stations i ne smije prepisati lokaciju postaje.
	Watercourse       string `json:"watercourse"`        // naziv vode za prikaz; prazno kad nije utvrđena
	WatercourseCode   string `json:"watercourse_code"`   // veza na registar vodnih tijela; prazno kad nije uspostavljena
	WatercourseSource string `json:"watercourse_source"` // odakle je voda utvrđena (models.WatercourseFrom*)
	WaterArea         string `json:"water_area"`         // npr. Srednja Sava, Sliv Drave i Dunava
	Stationing        string `json:"stationing"`         // stacionaža vodomjera, npr. "rkm 271+900"

	// Kota nule vodomjera vodi se u dva visinska sustava. ZeroDatum je kota
	// preuzeta iz dokumentacije dionica i zapisana je u starom visinskom sustavu;
	// ZeroDatumNew je kota u novom sustavu i upisuje se ručno.
	//
	// Jedna se NE izvodi iz druge: razlika visinskih sustava je konstanta koja se
	// upisuje iz službenog izvora, a pogrešna kota nule pomiče cijelu ljestvicu
	// pragova obrane.
	ZeroDatum          *float64 `json:"zero_datum,omitempty"`
	ZeroDatumSystem    string   `json:"zero_datum_system"`
	ZeroDatumNew       *float64 `json:"zero_datum_new,omitempty"`
	ZeroDatumNewSystem string   `json:"zero_datum_new_system"`

	Prep      Threshold `json:"prep"`      // Pripremno stanje
	Regular   Threshold `json:"regular"`   // Redovna obrana od poplava
	Emergency Threshold `json:"emergency"` // Izvanredna obrana od poplava
	State     Threshold `json:"state"`     // Izvanredno stanje
	Record    Threshold `json:"record"`    // Najviši zabilježeni vodostaj

	Notes      string `json:"notes"`
	SourceName string `json:"source_name"` // izvorni zapis naziva iz dokumentacije dionice

	// NeedsReview označava postaju čiji podaci nisu u cijelosti strojno
	// pročitani i traže potvrdu operatera prije oslanjanja na automatiku.
	NeedsReview bool   `json:"needs_review"`
	ReviewNote  string `json:"review_note,omitempty"`

	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	SectionCodes []string `json:"section_codes,omitempty"`
}

// WaterLevel predstavlja zapis očitanja vodostaja i protoka
type WaterLevel struct {
	ID          uuid.UUID `json:"id"`
	StationID   uuid.UUID `json:"station_id"`
	StationName string    `json:"station_name,omitempty"`
	Watercourse string    `json:"watercourse,omitempty"`
	MeasuredAt  time.Time `json:"measured_at"`
	LevelCm     int       `json:"level_cm"`
	FlowM3S     *float64  `json:"flow_m3s,omitempty"`
	Tendency    Tendency  `json:"tendency"`
	Source      string    `json:"source"` // npr. "Terensko očitanje", "Telemetrija", "DHMZ uvoz"
	CreatedAt   time.Time `json:"created_at"`
}

// StationStatus sažima trenutno operativno stanje hidrološke postaje
type StationStatus struct {
	Station           Station      `json:"station"`
	LatestMeasurement *WaterLevel  `json:"latest_measurement,omitempty"`
	CurrentPhase      DefensePhase `json:"current_phase"`
	LevelDifference   int          `json:"level_difference"` // razlika u cm u odnosu na prethodno mjerenje
}

// DiaryEntry predstavlja zapis u operativnom dnevniku Centra obrane od poplava
type DiaryEntry struct {
	ID          uuid.UUID    `json:"id"`
	Timestamp   time.Time    `json:"timestamp"`
	Author      string       `json:"author"`
	Category    string       `json:"category"` // "Mjera", "Zapovijed", "Ophodnja", "Crpna stanica", "Vreće", "Incident", "Opažanje"
	Location    string       `json:"location"` // Dionica, stacionaža ili lokacija
	StationID   *uuid.UUID   `json:"station_id,omitempty"`
	StationName string       `json:"station_name,omitempty"`
	Phase       DefensePhase `json:"phase"`
	Description string       `json:"description"`
	CreatedAt   time.Time    `json:"created_at"`
}

// Visinski sustavi u kojima se vodi kota nule vodomjera.
//
// Kote iz dokumentacije dionica su u starom sustavu (Trst, prema mareografu
// u Trstu). Novi sustav je HVRS71; te kote se tek trebaju izmjeriti i
// upisuju se ručno — ne izvode se preračunom.
const (
	ZeroDatumSystemOld = "TRST"
	ZeroDatumSystemNew = "HVRS71"
)

// Odakle je utvrđena voda na kojoj postaja stoji. Vodotok se upisuje samo kad ga
// dokumentacija tvrdi; kad je neodređen, ostaje prazan umjesto da se pogodi iz
// dionice, jer dionica govori za što je postaja mjerodavna, a ne gdje stoji.
const (
	WatercourseFromName       = "NAZIV"      // rijeka navedena u nazivu vodomjera
	WatercourseFromStationing = "STACIONAŽA" // stacionaža upada u raspon dionica jedne vode
	WatercourseFromSections   = "DIONICE"    // sve dionice postaje su na istoj vodi
	WatercourseFromOperator   = "OPERATER"   // ručno potvrdio operater
	WatercourseUndetermined   = ""           // nije utvrđeno — popunjava operater
)

// HasWatercourse govori je li utvrđeno na kojoj vodi postaja stoji
func (s Station) HasWatercourse() bool {
	return strings.TrimSpace(s.Watercourse) != ""
}

// HasNewZeroDatum govori je li kota nule prenesena u novi visinski sustav
func (s Station) HasNewZeroDatum() bool {
	return s.ZeroDatumNew != nil
}

// HasUsableThresholds govori ima li postaja ijedan prag u centimetrima, tj.
// može li se za nju uopće automatski odrediti faza obrane
func (s Station) HasUsableThresholds() bool {
	return s.Prep.IsUsable() || s.Regular.IsUsable() || s.Emergency.IsUsable() || s.State.IsUsable()
}

// CalculateDefensePhase izračunava fazu obrane za očitani vodostaj.
//
// Vraća PhaseUnknown kad postaja nema nijedan prag izražen u centimetrima —
// radije nego da tišinom prijavi redovno stanje na postaji čiji pragovi nisu
// strojno čitljivi.
func (s Station) CalculateDefensePhase(levelCm int) DefensePhase {
	if !s.HasUsableThresholds() {
		return PhaseUnknown
	}
	if s.State.IsUsable() && levelCm >= *s.State.Cm {
		return PhaseState
	}
	if s.Emergency.IsUsable() && levelCm >= *s.Emergency.Cm {
		return PhaseEmergency
	}
	if s.Regular.IsUsable() && levelCm >= *s.Regular.Cm {
		return PhaseRegular
	}
	if s.Prep.IsUsable() && levelCm >= *s.Prep.Cm {
		return PhasePrep
	}
	return PhaseNormal
}

// Severity vraća težinski rang faze radi sortiranja po kritičnosti.
// Nepoznata faza dobiva -1 kako ne bi bila izjednačena s redovnim stanjem.
func (p DefensePhase) Severity() int {
	switch p {
	case PhaseState:
		return 4
	case PhaseEmergency:
		return 3
	case PhaseRegular:
		return 2
	case PhasePrep:
		return 1
	case PhaseUnknown:
		return -1
	default:
		return 0
	}
}

// BadgeClass vraća CSS klasu stila za fazu
func (p DefensePhase) BadgeClass() string {
	switch p {
	case PhaseUnknown:
		return "badge-unknown"
	case PhaseState:
		return "badge-state"
	case PhaseEmergency:
		return "badge-emergency"
	case PhaseRegular:
		return "badge-regular"
	case PhasePrep:
		return "badge-prep"
	default:
		return "badge-normal"
	}
}

// Label vraća human-readable naziv faze na hrvatskom
func (p DefensePhase) Label() string {
	switch p {
	case PhaseUnknown:
		return "Pragovi nisu određeni"
	case PhaseState:
		return "Izvanredno stanje"
	case PhaseEmergency:
		return "Izvanredna obrana"
	case PhaseRegular:
		return "Redovna obrana"
	case PhasePrep:
		return "Pripremno stanje"
	default:
		// Ispod pripremnog stanja nema mjera obrane. Zakon poznaje četiri
		// faze — pripremno stanje, redovnu i izvanrednu obranu i izvanredno
		// stanje — pa ovo nije peta, nego njihov izostanak.
		return "bez mjera obrane"
	}
}

// InForce javlja je li na snazi neka od četiriju faza obrane. Vodostaj ispod
// pripremnog stanja i postaja bez pragova nisu faza, pa ih sučelje ne ističe.
func (p DefensePhase) InForce() bool { return p.Severity() > 0 }
