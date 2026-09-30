package models

import (
	"fmt"
	"net/url"
	"strings"
)

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
	Notes      string   `json:"notes,omitempty"`      // napomena i atribucija izvora
	Geometry   string   `json:"geometry,omitempty"`   // GeoJSON polilinije toka i stacionaže (rkm)

	// ExtraStationIDs su letve s drugih voda mjerodavne i za ovu. Letva ima
	// jednu vodu — Batina stoji na Dunavu — ali uspor Dunava vodi i baranjsku
	// Karašicu, pa se Batina prikazuje i uz nju.
	ExtraStationIDs []string `json:"extra_station_ids,omitempty"`

	// Izvedeno pri čitanju
	SectionCount int `json:"section_count"`
	StationCount int `json:"station_count"`
}

// HasGeometry javlja ima li vodotok definiranu geometriju (tok i stacionaže)
func (w Watercourse) HasGeometry() bool {
	return strings.TrimSpace(w.Geometry) != ""
}

// Oznake podrijetla zapisa u registru vodnih tijela
const (
	WatercourseOriginDecree        = "ODLUKA"        // Odluka o popisu voda I. reda
	WatercourseOriginEncyclopedia  = "ENCIKLOPEDIJA" // enciklopedijski članak
	WatercourseOriginDocumentation = "DOKUMENTACIJA" // dokumentacija štićenih dionica
	WatercourseOriginManual        = "RUČNI_UNOS"    // unio operater
	WatercourseOriginContract      = "UGOVOR"        // popis lokacija iz ugovora o održavanju (A.02)
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
	case WatercourseOriginContract:
		return "Ugovor o održavanju (A.02)"
	default:
		return "—"
	}
}

// WikiURL vraća poveznicu na članak hrvatske Wikipedije iz kojeg potječu
// opisni podaci, ako ih ima — obveza navođenja izvora po CC BY-SA 4.0.
// Članak s druge Wikipedije čuva se kao puna adresa i vraća kakav jest.
func (w Watercourse) WikiURL() string {
	switch {
	case w.WikiSlug == "":
		return ""
	case strings.HasPrefix(w.WikiSlug, "http"):
		return w.WikiSlug
	}
	return "https://hr.wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(w.WikiSlug, " ", "_"))
}

// WikiNaslov je naslov članka za prikaz: „Karašica (Dunav)"
func (w Watercourse) WikiNaslov() string {
	return WikiNaslov(w.WikiSlug)
}

// WikiNaslov svodi upisani članak na naslov. Obrazac traži naslov, ali ljudi
// prirodno zalijepe adresu iz preglednika —
// „https://hr.wikipedia.org/wiki/Kara%C5%A1ica_(Dunav)" je „Karašica (Dunav)".
// Adresa druge Wikipedije ostaje puna, jer bez nje jezik nestaje.
func WikiNaslov(s string) string {
	s = strings.TrimSpace(s)
	i := strings.Index(s, "wikipedia.org/wiki/")
	if i < 0 {
		return strings.ReplaceAll(s, "_", " ")
	}
	if !strings.Contains(s[:i], "hr.") {
		return s
	}
	naslov := s[i+len("wikipedia.org/wiki/"):]
	if j := strings.IndexAny(naslov, "?#"); j >= 0 {
		naslov = naslov[:j]
	}
	if u, err := url.PathUnescape(naslov); err == nil {
		naslov = u
	}
	return strings.ReplaceAll(naslov, "_", " ")
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
		parts = append(parts, fmt.Sprintf("površina sliva %.0f km²", *w.BasinKm2))
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
