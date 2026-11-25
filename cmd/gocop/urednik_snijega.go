package main

import (
	"fmt"
	"time"

	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
)

// Postavke modela snijega (Postavke prognoze › Model snijega) mijenja isti
// urednik kao i model letve: samo na računalu koje izdaje prognozu, nacrt pa
// proba pa primjena, a primijenjene putuju s modelom.

// SpremiSnijeg sprema nacrt postavki snijega.
func (u *urednikModela) SpremiSnijeg(p prognoza.PostavkeSnijega, tko string) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	return prognoza.SpremiNacrtSnijega(u.pb, prognoza.NacrtSnijega{Postavke: p, Tko: tko, Kad: sadaZaZapis()})
}

// ObrisiNacrtSnijega odbacuje nacrt postavki snijega.
func (u *urednikModela) ObrisiNacrtSnijega() error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	return prognoza.ObrisiNacrtSnijega(u.pb)
}

// probajNacrtSnijega proba nacrt postavki snijega; testovi ga zamjenjuju, jer
// pravi traži arhivu s dnevnim nizovima.
var probajNacrtSnijega = prognoza.ProbajNacrtSnijega

// ProbajSnijeg proba nacrt postavki snijega na povijesti. Prognoza i
// primijenjene postavke se ne mijenjaju.
func (u *urednikModela) ProbajSnijeg(p *poslovi.Posao) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	tocke, err := u.tockeRegistra()
	if err != nil {
		return err
	}
	p.Korak("učenje i mjerenje dnevnog modela s primijenjenim postavkama i s nacrtom", 0, 0)
	n, err := probajNacrtSnijega(u.arhiva, u.pb, tocke, sadaZaZapis())
	if err != nil {
		return err
	}
	p.Zavrsi(opisProbeSnijega(n), nil)
	return nil
}

// opisProbeSnijega je rečenica na kraju probe postavki snijega.
func opisProbeSnijega(n prognoza.NacrtSnijega) string {
	if n.Proba == nil {
		return "Proba je gotova."
	}
	bolje, mjereno := 0, 0
	for _, l := range n.Proba.Letve {
		if l.Ocjena != prognoza.MjeraNijeProvjereno {
			mjereno++
		}
		if l.Ocjena == prognoza.SnijegBolje {
			bolje++
		}
	}
	return fmt.Sprintf("Proba je gotova: nacrt je bolji nego sada na %d od %d %s. Prognoza se ne mijenja dok se postavke ne primijene.",
		bolje, mjereno, models.UzBrojHR(mjereno, "letve", "letve", "letava"))
}

// PrimijeniSnijeg primijeni probani nacrt postavki snijega, s imenom, vremenom
// i razlogom.
func (u *urednikModela) PrimijeniSnijeg(tko, razlog string) error {
	if err := u.smijeMijenjatiModel(); err != nil {
		return err
	}
	return prognoza.PrimijeniNacrtSnijega(u.pb, u.promjena(tko, razlog))
}

// sadaZaZapis je sadašnje vrijeme kako stoji uz nacrt i promjenu.
func sadaZaZapis() string { return time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04") }
