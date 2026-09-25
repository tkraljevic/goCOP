package models

import (
	"strings"
	"time"
)

// Kisomjer je točka kvazi-kišomjera: mjesto na kojem se iz reanalize i
// prognoze (Open-Meteo) čitaju oborina, snijeg i temperatura kao da ondje
// stoji kišomjer. Točke stoje po slivovima između letvi i po visinskim
// pojasima, pa jedna točka predstavlja dio sliva određene visine, s
// težinom koja kaže koliki je taj dio. Registar je namjerno rijedak:
// nekoliko točaka po slivu, ne mreža od stotina ćelija.
type Kisomjer struct {
	Code  string `json:"code"`
	Naziv string `json:"naziv"`
	Sliv  string `json:"sliv"`  // oznaka sliva (A, B, C…)
	Pojas string `json:"pojas"` // visinski pojas koji točka predstavlja

	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`

	Visina        *float64 `json:"visina,omitempty"`         // m n. m. na samoj točki
	SrednjaVisina *float64 `json:"srednja_visina,omitempty"` // srednja visina dijela sliva koji predstavlja
	Km2           *float64 `json:"km2,omitempty"`            // površina koju predstavlja
	Tezina        *float64 `json:"tezina,omitempty"`         // udio u slivu, 0–1

	Aktivan  bool   `json:"aktivan"`
	Napomena string `json:"napomena,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KisomjerPojasi su visinski pojasi koje obrazac nudi. Granice prate
// hidrološki smisao: u ravnici pada kiša, iznad 1000 m zimi snijeg koji se
// topi tek u proljeće, iznad 1500 m snijeg drži i dulje.
var KisomjerPojasi = []string{
	"ravnica (<300 m)",
	"pobrđe (300–1000 m)",
	"1000–1500 m",
	">1500 m",
}

// ImaKoordinate javlja stoji li točka negdje na karti
func (k Kisomjer) ImaKoordinate() bool {
	return k.Latitude != 0 && k.Longitude != 0
}

// KratkiPojas vraća pojas bez zagrade, za oznaku na karti
func (k Kisomjer) KratkiPojas() string {
	p, _, _ := strings.Cut(k.Pojas, " (")
	return p
}

// Sliv je dio sliva između susjednih letvi: ono što se slije u rijeku
// nizvodno od gornjih letvi, a uzvodno od donje. Poligon je iz HydroBASINS
// (WWF HydroSHEDS, CC BY 4.0), a služi da se na karti vidi koje područje
// koja točka predstavlja.
type Sliv struct {
	Oznaka   string   `json:"oznaka"`
	Naziv    string   `json:"naziv"`
	Km2      *float64 `json:"km2,omitempty"`
	Geometry string   `json:"geometry,omitempty"` // GeoJSON poligon
	Napomena string   `json:"napomena,omitempty"`
}

// HasGeometry javlja ima li sliv ucrtan poligon
func (m Sliv) HasGeometry() bool {
	return strings.TrimSpace(m.Geometry) != ""
}
