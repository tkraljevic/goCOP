package models

import (
	"fmt"
	"strings"
	"time"
)

// MaintainedWater je jedan redak popisa lokacija izvršenja usluga: voda ili
// nasip koji se u branjenom području održava iz programa A.02, s kategorijom
// pod kojom ga ugovor vodi. Popis dolazi iz ugovora o održavanju, a klasifikacija
// je ona koju Hrvatske vode koriste u cijeloj zemlji: red vode, skupina i vrsta.
//
// Pozicija plana se iz toga izvodi i ne pohranjuje: pozicije i cjenici mijenjaju
// se sa svakim okvirnim sporazumom, a kanal ostaje kanal II. reda.
type MaintainedWater struct {
	ID              string `json:"id"`
	AreaID          int    `json:"area_id"`
	Program         string `json:"program"`          // Program*: A.02 vode I. i II. reda, A.03 kanali III. i IV. reda
	WatercourseCode string `json:"watercourse_code"` // voda iz registra, ili
	StructureID     string `json:"structure_id"`     // objekt (nasip) iz registra
	Name            string `json:"name"`             // naziv kako stoji u popisu
	Seq             string `json:"seq"`              // redni broj u popisu, npr. "4.12."
	Order           string `json:"order"`            // WaterOrder*
	Group           string `json:"group"`            // WaterGroup*
	Kind            string `json:"kind"`             // MaintenanceKind*
	Source          string `json:"source"`           // iz kojeg ugovora/datoteke

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	WaterName     string `json:"-"` // naziv iz registra voda ili objekata
	StructureKind string `json:"-"`
}

// Programi održavanja
const (
	ProgramA02 = "A.02"
	ProgramA03 = "A.03"
)

// ProgramOf vraća program lokacije; prazno je A.02 (starije lokacije)
func (m MaintainedWater) ProgramOf() string {
	if m.Program == "" {
		return ProgramA02
	}
	return m.Program
}

// Red vode: I. i II. red su A.02, III. i IV. red su A.03
const (
	WaterOrderFirst  = "I"
	WaterOrderSecond = "II"
	WaterOrderThird  = "III"
	WaterOrderFourth = "IV"
)

// WaterOrders su redovi kojim ih obrazac nudi
var WaterOrders = []string{WaterOrderFirst, WaterOrderSecond, WaterOrderThird, WaterOrderFourth}

// Skupina unutar voda I. reda; vode II. reda nemaju skupinu
const (
	WaterGroupInterstate = "MEĐUDRŽAVNE"    // Međudržavne vode
	WaterGroupOtherState = "OSTALE DRŽAVNE" // Ostale državne vode (u planu "Druge veće vode")
)

// Vrsta lokacije, kako ih plan razvrstava
const (
	MaintenanceKindWatercourse = "VODOTOK"       // 1. Vodotoci
	MaintenanceKindReservoir   = "AKUMULACIJA"   // 2. Akumulacije, retencije i jezera
	MaintenanceKindTorrent     = "BUJICA"        // 3. Bujični tokovi
	MaintenanceKindDrainage    = "MELIORACIJSKA" // 4. Osnovne melioracijske građevine za odvodnju...
)

// MaintenanceKinds su vrste redom kojim ih plan navodi
var MaintenanceKinds = []string{
	MaintenanceKindWatercourse, MaintenanceKindReservoir, MaintenanceKindTorrent, MaintenanceKindDrainage,
}

// MaintenanceKindLabel vraća vrstu kako je plan naziva
func MaintenanceKindLabel(kind string) string {
	switch kind {
	case MaintenanceKindWatercourse:
		return "Vodotoci"
	case MaintenanceKindReservoir:
		return "Akumulacije, retencije i jezera"
	case MaintenanceKindTorrent:
		return "Bujični tokovi"
	case MaintenanceKindDrainage:
		return "Osnovne melioracijske građevine za odvodnju"
	default:
		return kind
	}
}

// KindLabel vraća vrstu za prikaz
func (m MaintainedWater) KindLabel() string { return MaintenanceKindLabel(m.Kind) }

// OrderLabel vraća red i skupinu u jednom retku: "Vode I. reda – Međudržavne vode"
func (m MaintainedWater) OrderLabel() string {
	switch m.Order {
	case WaterOrderFirst:
		switch m.Group {
		case WaterGroupInterstate:
			return "Vode I. reda – Međudržavne vode"
		case WaterGroupOtherState:
			return "Vode I. reda – Ostale državne vode"
		}
		return "Vode I. reda"
	case WaterOrderSecond:
		return "Vode II. reda"
	case WaterOrderThird:
		return "Vode III. reda"
	case WaterOrderFourth:
		return "Vode IV. reda"
	}
	return ""
}

// kindIndex je redni broj vrste u poziciji plana
func kindIndex(kind string) int {
	for i, k := range MaintenanceKinds {
		if k == kind {
			return i + 1
		}
	}
	return 0
}

// PlanPosition izvodi poziciju plana A.02 iz reda, skupine i vrste, npr.
// A.02.01.16.02.04. za melioracijski kanal II. reda u području 16. Prazno kad
// razvrstavanje nije potpuno.
func (m MaintainedWater) PlanPosition() string {
	n := kindIndex(m.Kind)
	if n == 0 || m.AreaID == 0 {
		return ""
	}
	switch m.Order {
	case WaterOrderFirst:
		g := 0
		switch m.Group {
		case WaterGroupInterstate:
			g = 1
		case WaterGroupOtherState:
			g = 2
		}
		if g == 0 {
			return ""
		}
		return fmt.Sprintf("A.02.01.%02d.01.%02d.%02d.", m.AreaID, g, n)
	case WaterOrderSecond:
		return fmt.Sprintf("A.02.01.%02d.02.%02d.", m.AreaID, n)
	}
	return ""
}

// IsStructure govori je li lokacija objekt (nasip), a ne voda
func (m MaintainedWater) IsStructure() bool { return m.StructureID != "" }

// DisplayName vraća naziv iz registra kad je lokacija vezana, inače iz popisa
func (m MaintainedWater) DisplayName() string {
	if m.WaterName != "" {
		return m.WaterName
	}
	return m.Name
}

// WorkItem je stavka radova održavanja u branjenom području: opis usluge i
// jedinica mjere, bez cijene. Popis se puni iz ugovora, a operateri ga
// dopunjuju i mijenjaju, jer dnevnik održavanja bilježi rad po stavkama.
type WorkItem struct {
	ID          string `json:"id"`
	AreaID      int    `json:"area_id"`
	Number      string `json:"number"` // oznaka iz okvirnog sporazuma, npr. "225"; može biti prazna
	Description string `json:"description"`
	Unit        string `json:"unit"` // ha, m2, m3, kom, sat...
	Active      bool   `json:"active"`
	SortOrder   int    `json:"sort_order"`
	Origin      string `json:"origin"` // WorkItemOrigin*
	Source      string `json:"source"` // ugovor iz kojeg je stavka došla

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Podrijetlo stavke radova
const (
	WorkItemOriginContract = "UGOVOR"
	WorkItemOriginManual   = "RUČNI_UNOS"
)

// OriginLabel vraća podrijetlo za prikaz
func (w WorkItem) OriginLabel() string {
	switch w.Origin {
	case WorkItemOriginContract:
		return "iz ugovora"
	case WorkItemOriginManual:
		return "ručni unos"
	}
	return w.Origin
}

// ShortDescription vraća opis skraćen na prvu rečenicu, za popise
func (w WorkItem) ShortDescription() string {
	d := strings.TrimSpace(w.Description)
	if i := strings.Index(d, ". "); i > 0 && i < len(d)-1 {
		return d[:i+1]
	}
	return d
}

// WorkItemKey je ključ po kojem se stavka prepoznaje pri ponovnom uvozu:
// ista oznaka i jedinica su ista stavka, bez obzira na godinu ugovora.
func WorkItemKey(number, description, unit string) string {
	n := strings.TrimSpace(number)
	if n != "" {
		return n + "|" + strings.ToLower(strings.TrimSpace(unit))
	}
	return strings.ToLower(strings.Join(strings.Fields(description), " ")) + "|" + strings.ToLower(strings.TrimSpace(unit))
}
