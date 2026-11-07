package models

import (
	"fmt"
	"strings"
)

// Metode samo za stranicu prijave. PDF i dnevni list i dalje čitaju Mjesto,
// Podaci i Izvornik, pa se njihov ispis ne mijenja.

// MjestoPuno je mjesto u retku na stranici prijave:
// "kanal Travnik · nasip Draž · B.16.2 · st. 4+300"
func (p PrijavaSTerena) MjestoPuno() string {
	var d []string
	for _, x := range []string{p.Vodotok, p.Objekt, p.DionicaCode} {
		if x != "" {
			d = append(d, x)
		}
	}
	if p.Stacionaza != "" {
		d = append(d, "st. "+p.Stacionaza)
	}
	return join(d, " · ")
}

// ImaZapisFotoaparata javlja je li fotoaparat uz sliku zapisao išta od vremena,
// položaja i uređaja
func (s SlikaPrijave) ImaZapisFotoaparata() bool {
	return (s.Snimljeno != nil && !s.Snimljeno.IsZero()) || s.ImaPolozaj() || s.Uredjaj != ""
}

// ZapisNaStranici je zapis fotoaparata ispod slike, u obliku stranice:
// "26. 10. 2026. 08:52:17 · 45,73120; 18,87410 · mobitel Primjer X1".
// Sekunde i pet decimala ostaju kao u Podaci, jer je to dokaz. Bez zapisa kaže
// isto što i Podaci.
func (s SlikaPrijave) ZapisNaStranici() string {
	if !s.ImaZapisFotoaparata() {
		return s.Podaci()
	}
	var d []string
	if s.Snimljeno != nil && !s.Snimljeno.IsZero() {
		d = append(d, s.Snimljeno.In(Zagreb).Format("2. 1. 2006. 15:04:05"))
	}
	if s.ImaPolozaj() {
		d = append(d, strings.ReplaceAll(fmt.Sprintf("%.5f; %.5f", s.Lat, s.Lon), ".", ","))
	}
	if s.Uredjaj != "" {
		d = append(d, s.Uredjaj)
	}
	return join(d, " · ")
}

// OtisakKratko je otisak skraćen za zaslon: "7f3a…c91e". Cijeli otisak ostaje
// u Izvornik i u PDF-u. Prazno kad otisak nije uzet.
func (s SlikaPrijave) OtisakKratko() string {
	if len(s.Otisak) <= 12 {
		return s.Otisak
	}
	return s.Otisak[:4] + "…" + s.Otisak[len(s.Otisak)-4:]
}
