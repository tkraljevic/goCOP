package models

import (
	"strings"
	"time"
)

// County predstavlja županiju (20 županija + Grad Zagreb)
type County struct {
	ID         int    `json:"id"`
	Code       string `json:"code"`       // npr. "OBZ", "VSZ", "ZG"
	Name       string `json:"name"`       // npr. "Osječko-baranjska županija"
	Seat       string `json:"seat"`       // npr. "Osijek"
	Prefect    string `json:"prefect"`    // Župan / Županica, npr. "Nataša Tramišak"
	AreaSqKm   int    `json:"area_sqkm"`  // Površina u km2, npr. 4155
	Population int    `json:"population"` // Broj stanovnika
	Email      string `json:"email"`
	Phone      string `json:"phone"`
}

// Municipality predstavlja jedinicu lokalne samouprave (Grad ili Općina)
type Municipality struct {
	ID         int     `json:"id"`
	CountyID   int     `json:"county_id"`
	CountyName string  `json:"county_name,omitempty"`
	Name       string  `json:"name"`       // npr. "Vukovar", "Trpinja", "Ernestinovo"
	Type       string  `json:"type"`       // "GRAD" ili "OPCINA"
	HeadTitle  string  `json:"head_title"` // "Gradonačelnik" ili "Općinski načelnik"
	HeadName   string  `json:"head_name"`  // Ime i prezime čelnika ako je poznato
	PostalCode string  `json:"postal_code,omitempty"`
	AreaSqKm   float64 `json:"area_sqkm,omitempty"`
	Population int     `json:"population,omitempty"`
}

// Settlement predstavlja pojedino naselje u sastavu grada ili općine
type Settlement struct {
	ID             int    `json:"id"`
	MunicipalityID int    `json:"municipality_id"`
	CountyID       int    `json:"county_id"`
	Name           string `json:"name"` // npr. "Bršadin", "Pačetin", "Divoš"
	PostalCode     string `json:"postal_code,omitempty"`
	Population     int    `json:"population,omitempty"`
}

// SectionTerritory predstavlja vezu između štićene dionice i ugroženih naselja / općina
type SectionTerritory struct {
	ID               string    `json:"id"`
	SectionCode      string    `json:"section_code"`
	CountyID         int       `json:"county_id"`
	MunicipalityID   int       `json:"municipality_id"`
	SettlementID     *int      `json:"settlement_id,omitempty"`
	CountyName       string    `json:"county_name,omitempty"`
	MunicipalityName string    `json:"municipality_name,omitempty"`
	MunicipalityType string    `json:"municipality_type,omitempty"`
	SettlementName   string    `json:"settlement_name,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// MunicipalityTypeLabel piše vrstu jedinice kako se piše u tekstu: Grad, Općina
func MunicipalityTypeLabel(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "GRAD":
		return "Grad"
	case "OPCINA", "OPĆINA":
		return "Općina"
	}
	return t
}
