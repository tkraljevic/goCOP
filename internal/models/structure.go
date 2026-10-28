package models

import (
	"strings"
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
	StationID       string `json:"station_id,omitempty"`       // vodomjer na objektu, ako postoji; kod ustave uzvodni
	// StationDownID je nizvodna letva ustave. Ustava drži dvije vode i ima
	// letvu s obje strane — Ustava Kopačevo uzvodno na Kanalu Kopačevo,
	// nizvodno na Kopačkom ritu — a očitanje na objektu nosi oba vodostaja.
	StationDownID string `json:"station_down_id,omitempty"`

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
	// Geometry je GeoJSON zbirka u WGS84: kod nasipa crta cijelog nasipa i
	// točke stacionaže (km nasipa), kao tok i rkm kod vodotoka. Nasip
	// podijeljen na dvije dionice ostaje jedan objekt s jednom crtom.
	Geometry string `json:"geometry,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	SectionCodes    []string `json:"section_codes,omitempty"`
	StationName     string   `json:"station_name,omitempty"`
	StationDownName string   `json:"station_down_name,omitempty"`
	AreaName        string   `json:"area_name,omitempty"`
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
	StructureKindPlug           = "CEP"     // automatski ili ručni čep na propustu kroz nasip
	StructureKindCulvert        = "PROPUST" // propust kroz nasip, sa zapornicom ili bez
	StructureKindSill           = "PRAG"    // prag ili stepenica u koritu
)

// StructureKinds su vrste redom kojim ih obrazac nudi
var StructureKinds = []string{
	StructureKindPumpingStation, StructureKindSluice, StructureKindSiphon,
	StructureKindWeir, StructureKindEmbankment, StructureKindDam,
	StructureKindPlug, StructureKindCulvert, StructureKindSill, StructureKindOther,
}

// StructureRegister je zaseban registar objekata: svoj popis, svoja
// pretraga i svoja stavka u izborniku. Nasipi, crpne stanice, ustave i
// brane imaju svaki svoj; sifoni, pregrade i ostalo dijele „Ostale objekte”.
type StructureRegister struct {
	Kind        string   // vrsta novog zapisa iz ovog registra
	Kinds       []string // sve vrste koje registar prikazuje
	Path        string   // npr. "/nasipi"
	Title       string   // množina, naslov popisa
	Icon        string
	Desc        string
	Placeholder string // primjer u polju pretrage
	NewLabel    string // gumb za novi zapis
}

// Ima javlja prikazuje li registar tu vrstu
func (r StructureRegister) Ima(kind string) bool {
	for _, k := range r.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// StructureRegisters su registri redom kojim ih izbornik nudi
var StructureRegisters = []StructureRegister{
	{StructureKindEmbankment, []string{StructureKindEmbankment}, "/nasipi", "Nasipi", "nasip",
		"Nasipi i obaloutvrde: crta nasipa sa stacionažom na karti, dionice koje ga dijele.", "Naziv nasipa (Zmajevac – Kopačevo)…", "Novi nasip"},
	{StructureKindPumpingStation, []string{StructureKindPumpingStation}, "/crpne-stanice", "Crpne stanice", "crpna-stanica",
		"Crpne stanice: kota nule letve, kapacitet i vodostaji uključenja i zaustavljanja.", "Naziv crpne stanice (CS Draž)…", "Nova crpna stanica"},
	{StructureKindSluice, []string{StructureKindSluice}, "/ustave", "Ustave", "ustava",
		"Ustave s letvom uzvodno i nizvodno.", "Naziv ustave (Ustava Kopačevo)…", "Nova ustava"},
	{StructureKindDam, []string{StructureKindDam}, "/brane", "Brane", "brana",
		"Brane i njihove dionice.", "Naziv brane…", "Nova brana"},
	{StructureKindSiphon, []string{StructureKindSiphon, StructureKindPlug, StructureKindCulvert, StructureKindSill, StructureKindWeir, StructureKindOther}, "/ostali-objekti", "Ostali objekti", "construction",
		"Sifoni, čepovi, propusti, pragovi, pregrade i objekti drugih vrsta.", "Naziv objekta…", "Novi objekt"},
}

// StructureRegisterFor vraća registar koji prikazuje tu vrstu
func StructureRegisterFor(kind string) (StructureRegister, bool) {
	for _, r := range StructureRegisters {
		if r.Ima(kind) {
			return r, true
		}
	}
	return StructureRegister{}, false
}

// Path je adresa objekta u registru njegove vrste, npr. /nasipi/<id>
func (s Structure) Path() string {
	if reg, ok := StructureRegisterFor(s.Kind); ok {
		return reg.Path + "/" + s.ID.String()
	}
	return "/structures/" + s.ID.String()
}

// KindLabel je naziv vrste u sučelju
func (s Structure) KindLabel() string { return StructureKindLabel(s.Kind) }

// TakesReadings kaže je li na objektu uopće što očitati. Vodostaj se čita na
// letvi crpne stanice, ustave, sifona ili pregrade; nasip, brana, čep, propust
// i prag letve nemaju
// — Teren ih je nudio kao "letve područja" s gumbom za upis, uz jedinu pravu
// letvu Batinu.
func (s Structure) TakesReadings() bool {
	switch s.Kind {
	case StructureKindEmbankment, StructureKindDam, StructureKindPlug, StructureKindCulvert, StructureKindSill:
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
	case StructureKindPlug:
		return "čep"
	case StructureKindCulvert:
		return "propust"
	case StructureKindSill:
		return "prag"
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

// HasGeometry javlja je li objekt ucrtan
func (s Structure) HasGeometry() bool { return strings.TrimSpace(s.Geometry) != "" }

// IsPumpingStation javlja nosi li objekt podatke o pogonu (uključenje, zaustavljanje)
func (s Structure) IsPumpingStation() bool { return s.Kind == StructureKindPumpingStation }
