package models

import (
	"strconv"
	"time"
)

// Licencirana firma: pravna osoba koja po ugovoru radi održavanje i obranu.
// Licencija se dobiva na natječaju, svake četiri godine. Jedna firma radi u
// više sektora ili područja, a jedno područje ima više firmi, pa se veza vodi
// zasebno (ContractorAssignment). Registar i veze putuju razmjenom kao i
// ostatak ustroja.
type Contractor struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`       // puni naziv, npr. Vodogradnja d.d. Osijek
	ShortName string    `json:"short_name"` // za tablice i značke
	OIB       string    `json:"oib"`
	Address   string    `json:"address"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	Contact   string    `json:"contact"` // osoba za obranu: voditelj usluga ili poslovođa
	Notes     string    `json:"notes"`
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updated_at"`

	// Izvedeno pri čitanju
	Assignments []ContractorAssignment `json:"-"`
}

// Label je naziv za tablice: kratki kad postoji
func (c Contractor) Label() string {
	if c.ShortName != "" {
		return c.ShortName
	}
	return c.Name
}

// ContractorAssignment veže firmu na sektor ili jedno branjeno područje.
// AreaID 0 znači cijeli sektor.
type ContractorAssignment struct {
	ID           string    `json:"id"`
	ContractorID string    `json:"contractor_id"`
	SectorID     string    `json:"sector_id"`
	AreaID       int       `json:"area_id"`
	Note         string    `json:"note"` // npr. program A.02, broj ugovora
	UpdatedAt    time.Time `json:"updated_at"`
}

// Key je mjesto rada bez identiteta zapisa; služi usporedbi starih i novih veza
func (a ContractorAssignment) Key() string {
	return a.SectorID + "|" + strconv.Itoa(a.AreaID)
}

// Where je mjesto rada za prikaz
func (a ContractorAssignment) Where() string {
	if a.AreaID > 0 {
		return Terms().AreaShort + " " + strconv.Itoa(a.AreaID)
	}
	return Terms().Sector + " " + a.SectorID
}
