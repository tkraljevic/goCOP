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
	ZeroDatum             *float64 `json:"zero_datum,omitempty"`
	ZeroDatumSystem       string   `json:"zero_datum_system"`
	ZeroDatumNew          *float64 `json:"zero_datum_new,omitempty"`
	ZeroDatumNewSystem    string   `json:"zero_datum_new_system"`
	ZeroDatumSource       string   `json:"zero_datum_source,omitempty"`
	ZeroDatumMethod       string   `json:"zero_datum_method,omitempty"`
	ZeroDatumSurveyDate   string   `json:"zero_datum_survey_date,omitempty"`
	ZeroDatumDocumentDate string   `json:"zero_datum_document_date,omitempty"`

	// Extremes su zabilježeni ekstremi letve: najviši i najniži vodostaj s
	// datumom. Vode se odvojeno od pragova jer nisu svi izmjereni na ovoj
	// letvi — Batina najviši vodostaj iz 1965. nema izmjeren nego preračunat iz
	// Bezdana, a prikazan kao mjerenje tvrdio bi nešto što se nije dogodilo.
	Extremes []StationExtreme `json:"extremes,omitempty"`

	// ZeroDatumHistory su promjene kote nule kroz vrijeme, od najstarije.
	// Vodostaji u bazi svi su svedeni na zadnju kotu, pa se ne preračunavaju;
	// povijest služi da se zna što je koja stara evidencija zapravo mjerila i
	// kad je letva premještena ili obnovljena.
	ZeroDatumHistory []ZeroDatumChange `json:"zero_datum_history,omitempty"`

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

// ZeroDatumChange je jedna kota nule s datumom od kojeg vrijedi.
type ZeroDatumChange struct {
	ValidFrom string   `json:"valid_from"`       // datum, YYYY-MM-DD; prazno = od početka mjerenja
	Datum     *float64 `json:"datum,omitempty"`  // kota nule u metrima
	System    string   `json:"system,omitempty"` // TRST, HVRS71 …
	Note      string   `json:"note,omitempty"`   // razlog: premještaj letve, obnova, novi elaborat
}

// ZeroDatumAt vraća kotu koja je vrijedila na dan; nil kad povijest za taj
// dan ne zna ništa. Promjene se čitaju od najstarije, pa pobjeđuje zadnja
// koja je počela vrijediti prije ili na taj dan.
func (s Station) ZeroDatumAt(day string) *ZeroDatumChange {
	var out *ZeroDatumChange
	for i := range s.ZeroDatumHistory {
		c := &s.ZeroDatumHistory[i]
		if c.ValidFrom == "" || c.ValidFrom <= day {
			out = c
		}
	}
	return out
}

// Kvaliteta zabilježene vrijednosti: je li izmjerena na ovoj letvi ili
// dobivena računom iz druge postaje.
const (
	QualityMeasured      = "IZMJERENO"
	QualityReconstructed = "REKONSTRUIRANO"
	QualityUncertain     = "SUMNJIVO"
)

// StationExtreme je zabilježeni najviši ili najniži vodostaj letve.
type StationExtreme struct {
	Kind    string `json:"kind"` // MAX ili MIN
	LevelCm *int   `json:"level_cm,omitempty"`
	OnDate  string `json:"on_date,omitempty"` // YYYY-MM-DD ili YYYY kad se zna samo godina
	Quality string `json:"quality,omitempty"` // Quality*
	Source  string `json:"source,omitempty"`  // odakle podatak: DHMZ, postaja Bezdan …
	Method  string `json:"method,omitempty"`  // kako je dobiven, kad nije izmjeren
	Note    string `json:"note,omitempty"`
}

const (
	ExtremeMax = "MAX"
	ExtremeMin = "MIN"
)

// IsMeasured govori smije li se vrijednost predstaviti kao mjerenje ove letve.
// Prazna kvaliteta znači izmjereno: takvi su zapisi zatečeni prije nego što se
// razlika počela bilježiti.
func (e StationExtreme) IsMeasured() bool {
	return e.Quality == "" || e.Quality == QualityMeasured
}

// Label je vrijednost s predznakom, kako se vodostaj i inače piše.
func (e StationExtreme) Label() string {
	if e.LevelCm == nil {
		return "—"
	}
	return fmt.Sprintf("%+d cm", *e.LevelCm)
}

// QualityLabel je kratko objašnjenje odakle vrijednost dolazi.
func QualityLabel(q string) string {
	switch q {
	case QualityReconstructed:
		return "rekonstruirano"
	case QualityUncertain:
		return "sumnjivo"
	default:
		return "izmjereno"
	}
}

// ExtremesOf vraća ekstreme zadane vrste, redom kojim su upisani.
func (s Station) ExtremesOf(kind string) []StationExtreme {
	var out []StationExtreme
	for _, e := range s.Extremes {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
