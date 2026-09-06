package models

import (
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// Nazivi razina ustroja. Hrvatske vode obranu dijele na sektore (VGO s
// centrom obrane) i branjena područja (mali slivovi s ispostavom); druga
// vodnogospodarska organizacija istu podjelu zove drukčije. Šifre dionica i
// ovlasti rade isto, mijenjaju se samo riječi u sučelju. Zapis je jedan i
// putuje razmjenom, pa svi čvorovi jedne mreže govore istim jezikom.
type OrgTerms struct {
	ID           string    `json:"id"`            // uvijek "terms"
	Sector       string    `json:"sector"`        // Sektor
	Sectors      string    `json:"sectors"`       // Sektori
	Area         string    `json:"area"`          // Branjeno područje
	Areas        string    `json:"areas"`         // Branjena područja
	AreaShort    string    `json:"area_short"`    // BP
	SectorOffice string    `json:"sector_office"` // Vodnogospodarski odjel
	AreaOffice   string    `json:"area_office"`   // Vodnogospodarska ispostava
	Center       string    `json:"center"`        // Centar obrane od poplava
	Subcenter    string    `json:"subcenter"`     // Podcentar obrane
	UpdatedAt    time.Time `json:"updated_at"`
}

// TermsID je identitet jedinog zapisa naziva
const TermsID = "terms"

// DefaultTerms su nazivi Hrvatskih voda
func DefaultTerms() OrgTerms {
	return OrgTerms{
		ID: TermsID, Sector: "Sektor", Sectors: "Sektori", Area: "Branjeno područje", Areas: "Branjena područja",
		AreaShort: "BP", SectorOffice: "Vodnogospodarski odjel", AreaOffice: "Vodnogospodarska ispostava",
		Center: "Centar obrane od poplava", Subcenter: "Podcentar obrane",
	}
}

// Filled vraća nazive s praznima popunjenim iz zadanih, da sučelje nikad ne ostane bez riječi
func (t OrgTerms) Filled() OrgTerms {
	d := DefaultTerms()
	pick := func(v, def string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		return strings.TrimSpace(v)
	}
	return OrgTerms{
		ID: TermsID, Sector: pick(t.Sector, d.Sector), Sectors: pick(t.Sectors, d.Sectors),
		Area: pick(t.Area, d.Area), Areas: pick(t.Areas, d.Areas), AreaShort: pick(t.AreaShort, d.AreaShort),
		SectorOffice: pick(t.SectorOffice, d.SectorOffice), AreaOffice: pick(t.AreaOffice, d.AreaOffice),
		Center: pick(t.Center, d.Center), Subcenter: pick(t.Subcenter, d.Subcenter), UpdatedAt: t.UpdatedAt,
	}
}

// Get vraća naziv po ključu kakav se piše u predlošcima
func (t OrgTerms) Get(key string) string {
	switch key {
	case "sektor":
		return t.Sector
	case "sektori":
		return t.Sectors
	case "podrucje":
		return t.Area
	case "podrucja":
		return t.Areas
	case "bp":
		return t.AreaShort
	case "vgo":
		return t.SectorOffice
	case "vgi":
		return t.AreaOffice
	case "cop":
		return t.Center
	case "podcentar":
		return t.Subcenter
	}
	return key
}

// Lower piše naziv s malim početnim slovom, za sredinu rečenice
func (t OrgTerms) Lower(key string) string {
	s := t.Get(key)
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToLower(r)) + s[size:]
}

var currentTerms atomic.Pointer[OrgTerms]

// Terms vraća nazive koji trenutno vrijede na ovom čvoru
func Terms() OrgTerms {
	if t := currentTerms.Load(); t != nil {
		return *t
	}
	return DefaultTerms()
}

// SetTerms postavlja nazive; zove se pri startu, nakon upisa i nakon primitka razmjenom
func SetTerms(t OrgTerms) {
	f := t.Filled()
	currentTerms.Store(&f)
}
