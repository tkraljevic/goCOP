package models

import (
	"net/url"
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
	Website    string `json:"website,omitempty"`
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
	Email      string  `json:"email,omitempty"` // službena e-pošta, za obavijesti u obrani
	Phone      string  `json:"phone,omitempty"`
	Website    string  `json:"website,omitempty"`
}

// NormalizeWebsite priprema upisanu adresu mrežne stranice za pohranu.
//
// Ljudi upisuju "www.darda.hr" bez sheme, a preglednik bi to u poveznici
// shvatio kao putanju unutar goCOP-a; zato se dodaje https://. Prihvaćaju se
// samo http i https: adresa završava u href atributu, a ondje "javascript:"
// nije poveznica nego rupa. Odbija se i adresa s korisničkim dijelom — tu
// završi zalijepljena e-mail adresa ("mailto:pisarnica@darda.hr"), ali i
// adresa koja se predstavlja kao općinska a vodi drugamo
// ("https://darda.hr@tudja-stranica.com").
//
// Neispravan unos vraća praznu adresu i ok=false, da ga sloj iznad može
// odbiti umjesto da ga tiho proguta.
func NormalizeWebsite(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", false
	}
	if u.Host == "" || !strings.Contains(u.Host, ".") || u.User != nil {
		return "", false
	}
	return u.String(), true
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

// TerritoryLabel je natpis ugroženog naselja na obrascu: naselje, pa općina,
// pa županija bez riječi "županija" — "Batina (Draž, Osječko-baranjska)".
// Bez naselja stoji cijela općina ili grad. Županija se piše jer se isto ime
// naselja ponavlja po Hrvatskoj, a čitatelj obrasca nije nužno s tog terena.
func TerritoryLabel(settlement, municipalityType, municipality, county string) string {
	county = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(county), "županija"))
	if settlement != "" {
		if county != "" {
			return settlement + " (" + municipality + ", " + county + ")"
		}
		return settlement + " (" + municipality + ")"
	}
	label := strings.TrimSpace(MunicipalityTypeLabel(municipalityType) + " " + municipality)
	if county != "" {
		label += " (" + county + ")"
	}
	return label
}
