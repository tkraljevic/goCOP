package models

import "math"

// Granice nagiba umjerenja: uređaj koji za centimetar letve pokaže manje od
// petine ili više od pet centimetara nije krivo umjeren nego pokvaren, i to
// se ne preračunava nego popravlja na terenu.
const (
	NajmanjiNagibUmjerenja = 0.2
	NajveciNagibUmjerenja  = 5
)

// ImaUmjerenje javlja preračunavaju li se preuzeta očitanja letve
// (UmjerenjeOdsjecak, UmjerenjeNagib).
func (s Station) ImaUmjerenje() bool {
	return s.UmjerenjeOdsjecak != nil && s.UmjerenjeNagib != nil &&
		*s.UmjerenjeNagib >= NajmanjiNagibUmjerenja && *s.UmjerenjeNagib <= NajveciNagibUmjerenja
}

// Umjeri preračunava vrijednost mjernog uređaja na letvu:
// letva = (uređaj − odsječak) / nagib. Bez umjerenja vraća vrijednost kakva jest.
func (s Station) Umjeri(uredjaj int) int {
	if !s.ImaUmjerenje() {
		return uredjaj
	}
	return int(math.Round((float64(uredjaj) - *s.UmjerenjeOdsjecak) / *s.UmjerenjeNagib))
}
