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

// Navodi javlja navodi li slobodan upis firme (npr. firma s računa osobe)
// ovu firmu, punim ili kratkim nazivom. Uspoređuje se kao u dnevnicima
// usluga (istiNaziv): naziv do pravnog oblika i prvog zareza.
func (c Contractor) Navodi(naziv string) bool {
	return istiNaziv(c.Name, naziv) || istiNaziv(c.ShortName, naziv)
}

// VezanaNa javlja je li firma vezana na branjeno područje (veza s tim
// područjem, ne cijeli sektor), kao kad joj se šalju rješenja i obavijesti
// područja
func (c Contractor) VezanaNa(areaID int) bool {
	for _, a := range c.Assignments {
		if a.AreaID == areaID {
			return true
		}
	}
	return false
}

// FirmeRacuna su firme iz registra koje navodi firma upisana na računu
// osobe (User.OrgName); prazan upis ne navodi nijednu
func FirmeRacuna(u *User, firme []Contractor) []Contractor {
	var out []Contractor
	for _, c := range firme {
		if u != nil && c.Navodi(u.OrgName) {
			out = append(out, c)
		}
	}
	return out
}
