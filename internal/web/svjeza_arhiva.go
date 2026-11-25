package web

import (
	"fmt"
	"time"

	"gocop/internal/models"
)

// SvjezaArhiva je sažetak svježe arhive za karticu u Izvorima arhive: na
// računalu koje preuzima izvore zadnje ulaganje i paketi koje je izdalo, a
// na ostalima zadnji paket arhive koji je stigao.
type SvjezaArhiva struct {
	Ulaze         bool // ovo računalo ulaže svježe podatke
	Ulozeno       time.Time
	Letvi, Tocaka int
	Izdano        time.Time
	Paketa        int

	Primljeno      time.Time // zadnji primljeni paket, na računalu koje ne ulaže
	PrimljenoLetvi int
}

// SetSvjezaArhiva postavlja odakle stranica Izvora arhive čita stanje
// svježe arhive.
func (s *Server) SetSvjezaArhiva(f func() SvjezaArhiva) { s.svjeza = f }

// stanjeSvjezeArhive je stanje za stranicu; nil kad ga program ne daje.
func (s *Server) stanjeSvjezeArhive() *SvjezaArhiva {
	if s.svjeza == nil {
		return nil
	}
	v := s.svjeza()
	return &v
}

// ZadnjeUlaganje je redak „Zadnje ulaganje”: 25. 11. 2026. u 00:15 · 63
// letve, 70 točaka.
func (a SvjezaArhiva) ZadnjeUlaganje() string {
	if a.Ulozeno.IsZero() {
		return "još nije bilo"
	}
	return fmt.Sprintf("%s · %d %s, %d %s", a.Ulozeno.In(models.Zagreb).Format("2. 1. 2006. u 15:04"),
		a.Letvi, models.UzBrojHR(a.Letvi, "letva", "letve", "letvi"),
		a.Tocaka, models.UzBrojHR(a.Tocaka, "točka", "točke", "točaka"))
}

// PaketiOstalima je redak „Paketi ostalima”.
func (a SvjezaArhiva) PaketiOstalima() string {
	pravilo := "najviše jednom tjedno po letvi, odmah kad je promjena velika"
	if a.Izdano.IsZero() {
		return pravilo + "; još nijedan"
	}
	return fmt.Sprintf("%s; zadnji %s (%d %s)", pravilo, a.Izdano.In(models.Zagreb).Format("2. 1. 2006."),
		a.Paketa, models.UzBrojHR(a.Paketa, "paket", "paketa", "paketa"))
}

// ZadnjiPaket je redak „Zadnji paket” na računalu koje ne ulaže.
func (a SvjezaArhiva) ZadnjiPaket() string {
	if a.Primljeno.IsZero() {
		return "još nije stigao"
	}
	return fmt.Sprintf("%s · %d %s", a.Primljeno.In(models.Zagreb).Format("2. 1. 2006."),
		a.PrimljenoLetvi, models.UzBrojHR(a.PrimljenoLetvi, "letva", "letve", "letvi"))
}
