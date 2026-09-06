package models

import (
	"time"

	"github.com/google/uuid"
)

// Structure je hidrotehnički objekt: crpna stanica, ustava, sifon, pregrada,
// nasip. Do sada su objekti živjeli samo kao redci u opisu dionice i kao
// "vodomjeri" nazvani po njima; Baranja je pokazala da je objekt zaseban
// zapis s vlastitim podacima — kapacitetom, kotom nule i vodostajima na
// kojima se pokreće i zaustavlja — na koji se vežu očitanja i dnevnik rada.
type Structure struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"` // stabilna šifra, npr. "bp16-cs-draz"
	Name     string    `json:"name"`
	Kind     string    `json:"kind"` // StructureKind*
	SectorID string    `json:"sector_id"`
	AreaID   int       `json:"area_id"`

	WatercourseCode string `json:"watercourse_code,omitempty"` // voda na kojoj objekt stoji
	StationID       string `json:"station_id,omitempty"`       // vodomjer na objektu, ako postoji

	ZeroDatum       *float64 `json:"zero_datum,omitempty"`        // kota nule letve, m n. m., sustav Trst
	ZeroDatumSystem string   `json:"zero_datum_system,omitempty"` // TRST ili HVRS71
	CapacityText    string   `json:"capacity_text,omitempty"`     // npr. "2x2,5" (m³/s) — kako je zapisano
	StartCm         *int     `json:"start_cm,omitempty"`          // vodostaj uključenja crpne stanice
	StartText       string   `json:"start_text,omitempty"`        // izvorni zapis kad nije jedan broj
	StopCm          *int     `json:"stop_cm,omitempty"`           // vodostaj zaustavljanja
	StopText        string   `json:"stop_text,omitempty"`
	Notes           string   `json:"notes,omitempty"`
	Origin          string   `json:"origin"` // odakle zapis: DIRECTUS_BP16, DOKUMENTACIJA, RUČNI_UNOS

	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	SectionCodes []string `json:"section_codes,omitempty"`
	StationName  string   `json:"station_name,omitempty"`
	AreaName     string   `json:"area_name,omitempty"`
}

// Vrste objekata
const (
	StructureKindPumpingStation = "CRPNA_STANICA"
	StructureKindSluice         = "USTAVA"
	StructureKindSiphon         = "SIFON"
	StructureKindWeir           = "PREGRADA"
	StructureKindEmbankment     = "NASIP"
	StructureKindDam            = "BRANA"
	StructureKindOther          = "OSTALO"
)

// StructureKinds su vrste redom kojim ih obrazac nudi
var StructureKinds = []string{
	StructureKindPumpingStation, StructureKindSluice, StructureKindSiphon,
	StructureKindWeir, StructureKindEmbankment, StructureKindDam, StructureKindOther,
}

// KindLabel je naziv vrste u sučelju
func (s Structure) KindLabel() string { return StructureKindLabel(s.Kind) }

// TakesReadings kaže je li na objektu uopće što očitati. Vodostaj se čita na
// letvi crpne stanice, ustave, sifona ili pregrade; nasip i brana letve nemaju
// — Teren ih je nudio kao "letve područja" s gumbom za upis, uz jedinu pravu
// letvu Batinu.
func (s Structure) TakesReadings() bool {
	switch s.Kind {
	case StructureKindEmbankment, StructureKindDam:
		return false
	}
	return true
}

func StructureKindLabel(kind string) string {
	switch kind {
	case StructureKindPumpingStation:
		return "crpna stanica"
	case StructureKindSluice:
		return "ustava"
	case StructureKindSiphon:
		return "sifon"
	case StructureKindWeir:
		return "pregrada"
	case StructureKindEmbankment:
		return "nasip"
	case StructureKindDam:
		return "brana"
	}
	return "objekt"
}

// OriginLabel govori odakle zapis potječe
func (s Structure) OriginLabel() string {
	switch s.Origin {
	case "DIRECTUS_BP16":
		return "Evidencija VGI Baranja"
	case "DOKUMENTACIJA":
		return "Dokumentacija dionica"
	case "RUČNI_UNOS":
		return "Ručni unos"
	case StructureOriginContract:
		return "Ugovor o održavanju (A.02)"
	}
	return s.Origin
}

// StructureOriginContract označava objekt koji je u registar ušao iz popisa
// lokacija ugovora o održavanju
const StructureOriginContract = "UGOVOR"

// IsPumpingStation javlja nosi li objekt podatke o pogonu (uključenje, zaustavljanje)
func (s Structure) IsPumpingStation() bool { return s.Kind == StructureKindPumpingStation }
