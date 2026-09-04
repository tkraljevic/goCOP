package models

import "strings"

import "fmt"

// Watercourse je vodno tijelo iz službenog registra.
//
// Kostur je Odluka o popisu voda I. reda (NN 79/2010) — ona daje pravnu
// kategoriju i razlikuje vode istog imena ("potok Karašica (Baranja)" nije
// "rijeka Karašica (miholjačka)"). Vode koje nisu I. reda ulaze bez kategorije.
type Watercourse struct {
	Code         string `json:"code"`
	OfficialName string `json:"official_name"` // naziv kako stoji u Odluci
	Name         string `json:"name"`          // bez vrste ("Drava", ne "rijeka Drava")
	Kind         string `json:"kind"`          // rijeka, potok, kanal, jezero, akumulacija...
	Category     string `json:"category"`      // MEĐUDRŽAVNE VODE, DRUGE VEĆE VODE I KANALI...
	Subcategory  string `json:"subcategory"`   // vodotoci, kanali, ponornice, akumulacije i retencije...
	WikiSlug     string `json:"wiki_slug"`

	// Origin govori odakle zapis dolazi. Vode koje Odluka ne navodi nisu izmišljene
	// nego preuzete iz dokumentacije dionica — nisu I. reda, ali postoje.
	Origin string `json:"origin"`

	LengthKm   *float64 `json:"length_km,omitempty"`
	BasinKm2   *float64 `json:"basin_km2,omitempty"`
	AvgFlowM3S *float64 `json:"avg_flow_m3s,omitempty"`
	Source     string   `json:"source,omitempty"`     // izvor
	Mouth      string   `json:"mouth,omitempty"`      // ušće
	FlowsInto  string   `json:"flows_into,omitempty"` // ulijeva se u

	// Izvedeno pri čitanju
	SectionCount int `json:"section_count"`
	StationCount int `json:"station_count"`
}

// Oznake podrijetla zapisa u registru vodnih tijela
const (
	WatercourseOriginDecree        = "ODLUKA"        // Odluka o popisu voda I. reda
	WatercourseOriginEncyclopedia  = "ENCIKLOPEDIJA" // enciklopedijski članak
	WatercourseOriginDocumentation = "DOKUMENTACIJA" // dokumentacija štićenih dionica
	WatercourseOriginManual        = "RUČNI_UNOS"    // unio operater
)

// OriginLabel vraća podrijetlo zapisa u obliku za prikaz
func (w Watercourse) OriginLabel() string {
	switch w.Origin {
	case WatercourseOriginDecree:
		return "Odluka o popisu voda I. reda"
	case WatercourseOriginEncyclopedia:
		return "Wikipedija (CC BY-SA 4.0)"
	case WatercourseOriginDocumentation:
		return "Dokumentacija dionica"
	case WatercourseOriginManual:
		return "Ručni unos"
	default:
		return "—"
	}
}

// WikiURL vraća poveznicu na članak hrvatske Wikipedije iz kojeg potječu
// opisni podaci, ako ih ima — obveza navođenja izvora po CC BY-SA 4.0
func (w Watercourse) WikiURL() string {
	if w.WikiSlug == "" {
		return ""
	}
	return "https://hr.wikipedia.org/wiki/" + strings.ReplaceAll(w.OfficialName, " ", "_")
}

// IsFirstOrder govori je li vodno tijelo na popisu voda I. reda
func (w Watercourse) IsFirstOrder() bool {
	return w.Category != ""
}

// CategoryLabel vraća kategoriju u obliku za prikaz
func (w Watercourse) CategoryLabel() string {
	switch w.Category {
	case "":
		return "nije voda I. reda"
	case "MEĐUDRŽAVNE VODE":
		return "Međudržavna voda"
	case "DRUGE VEĆE VODE I KANALI":
		return "Druga veća voda ili kanal"
	case "BUJIČNE VODE VEĆE SNAGE":
		return "Bujična voda veće snage"
	case "PRIOBALNE VODE":
		return "Priobalna voda"
	default:
		return w.Category
	}
}

// Summary sažima mjerne podatke u jedan redak, koliko ih ima
func (w Watercourse) Summary() string {
	var parts []string
	if w.LengthKm != nil {
		parts = append(parts, fmt.Sprintf("%.0f km", *w.LengthKm))
	}
	if w.BasinKm2 != nil {
		parts = append(parts, fmt.Sprintf("porječje %.0f km²", *w.BasinKm2))
	}
	if w.AvgFlowM3S != nil {
		parts = append(parts, fmt.Sprintf("prosječni protok %.0f m³/s", *w.AvgFlowM3S))
	}

	if len(parts) == 0 {
		return ""
	}

	out := parts[0]
	for _, p := range parts[1:] {
		out += " · " + p
	}
	return out
}
