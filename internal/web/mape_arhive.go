package web

import (
	"fmt"

	"gocop/internal/config"
)

// MapaArhive je stablo arhive ili mapa paketa kako ih čvor vidi na disku, uz
// datoteku postavki i zastavicu pokretanja, ako je zadana, za uputu kad
// relativne mape nema. Uputa (config.Mapa.Uputa) je ista u dnevniku pri
// pokretanju, na stranici Unos u arhivu i u poruci odbijenog upisa ili
// izdavanja.
type MapaArhive struct {
	config.Mapa
}

// NovaMapaArhive provjerava mapu iz postavke kljuc (podaci ili pakete).
func NovaMapaArhive(kljuc, zadano string, pc PostavkeCvora) MapaArhive {
	return MapaArhive{config.IzvorMapa{Postavke: pc.Put, Zastavice: pc.Zastavice}.Provjeri(kljuc, zadano)}
}

// mapaArhive je MapaArhive ovog poslužitelja
func (s *Server) mapaArhive(kljuc, zadano string) MapaArhive {
	var pc PostavkeCvora
	if s.postavke != nil {
		pc = s.postavke.snimka()
	}
	return NovaMapaArhive(kljuc, zadano, pc)
}

type rijeciMape struct {
	naziv      string // Stablo arhive
	iskljuceno string // kad je postavka prazna
	dok        string // što se ne radi dok je mapa drugdje
	nastaje    string // kad mape još nema, a nastat će
}

var rijeciMapa = map[string]rijeciMape{
	"podaci": {naziv: "Stablo arhive", iskljuceno: "isključeno, čvor arhivu samo prima",
		dok: "Dok se to ne riješi, uvoz u stablo ne zapisuje ništa.", nastaje: "nastaje pri prvom upisu"},
	"pakete": {naziv: "Mapa paketa", iskljuceno: "isključena, čvor ne izdaje pakete",
		dok: "Dok se to ne riješi, paketi se ne izdaju.", nastaje: "nastaje pri prvom izdavanju"},
}

func (m MapaArhive) rijeci() rijeciMape { return rijeciMapa[m.Kljuc] }

// Naziv je "Stablo arhive" ili "Mapa paketa".
func (m MapaArhive) Naziv() string { return m.rijeci().naziv }

// Dok kaže što čvor ne radi dok je mapa drugdje.
func (m MapaArhive) Dok() string { return m.rijeci().dok }

// Nastaje kaže kad nastaje mapa koje još nema.
func (m MapaArhive) Nastaje() string { return m.rijeci().nastaje }

// Redak je zapis za dnevnik pri pokretanju: puna putanja i, kad mape nema,
// upozorenje; uz relativnu i uputa.
func (m MapaArhive) Redak() string {
	r := m.rijeci()
	switch {
	case m.Zadano == "":
		return r.naziv + ": " + r.iskljuceno
	case m.Postoji:
		return r.naziv + ": " + m.Puna
	case m.Drugdje():
		return fmt.Sprintf("%s: mape %s nema. %s %s", r.naziv, m.Puna, r.dok, m.Uputa())
	case m.Relativna():
		return fmt.Sprintf("%s: %s (mape još nema, %s). %s", r.naziv, m.Puna, r.nastaje, m.Uputa())
	}
	return fmt.Sprintf("%s: %s (mape još nema, %s)", r.naziv, m.Puna, r.nastaje)
}

// SetMape daje vratima pogled na stablo i mapu paketa s uputom iz postavki
// čvora. Bez toga vrata i dalje vide gdje je mapa, samo bez datoteke
// postavki.
func (h *UvozHandler) SetMape(f func(kljuc, zadano string) MapaArhive) { h.mapa = f }

// mapeNaDisku je stablo i mapa paketa ovih vrata
func (h *UvozHandler) mapeNaDisku() (stablo, paketi MapaArhive) {
	mapa := h.mapa
	if mapa == nil {
		mapa = func(kljuc, zadano string) MapaArhive { return NovaMapaArhive(kljuc, zadano, PostavkeCvora{}) }
	}
	paketiDir := ""
	if h.paketiDir != nil {
		paketiDir = h.paketiDir()
	}
	return mapa("podaci", h.podaciDir()), mapa("pakete", paketiDir)
}
