package web

import (
	"fmt"
	"slices"

	"gocop/internal/uvoz/izvori"
)

// stabloCitljivo odbija uvoz kad se stablo ne da pročitati: tada se ne zna u
// kojem slivu letva već stoji, pa bi postojeća letva dobila drugu mapu.
func (d *UvozPageData) stabloCitljivo() error {
	if d.greskaStabla != nil {
		return fmt.Errorf("uvoz nije pokrenut: stablo s podacima ne da se pročitati, pa se ne zna u kojem slivu letve stoje (%w)",
			d.greskaStabla)
	}
	return nil
}

// smijeUvesti: posebni izvor se uvozi samo uz čitljivo stablo, uz pravo na
// svaku letvu na koju piše i, kad otvara novi sliv, samo globalni
// administrator (smijeSliv). Bez sliva iz obrasca nova letva ide u zadani
// sliv formata.
func (h *UvozHandler) smijeUvesti(d *UvozPageData, f izvori.Format, z izvori.Zadatak) error {
	if err := d.stabloCitljivo(); err != nil {
		return err
	}
	letve, err := h.smijeSveLetve(d.Permissions, f, z)
	if err != nil {
		return err
	}
	if z.Sliv != "" {
		return d.smijeSliv(z.Sliv, letve)
	}
	if d.smijeSliv(f.ZadaniSliv, letve) != nil {
		return fmt.Errorf("sliv nije upisan, pa bi nova letva išla u sliv %s, zadan za ovaj izvor. "+
			"Tog sliva u arhivi još nema, a %s", f.ZadaniSliv, posljedicaNovogSliva)
	}
	return nil
}

// posljedicaNovogSliva kaže tko otvara novi sliv i što učiniti
const posljedicaNovogSliva = "novi sliv otvara samo globalni administrator. " +
	"Odaberite sliv koji već postoji ili zamolite globalnog administratora da uveze prvu letvu ovog sliva"

// smijeSliv: novi sliv, mapu u korijenu stabla koje još nema, uvozom otvara
// samo globalni administrator; ostali uvoze u slivove koji već postoje
// (Odluke (30. 10. 2026.)). Sliv se otvara samo za letvu koje u stablu još
// nema, jer postojeća letva ostaje u svom slivu.
func (d *UvozPageData) smijeSliv(sliv string, letve []string) error {
	if d.Administrator() || sliv == "" || slices.Contains(d.Slivovi, sliv) || !imaNovuLetvu(letve, d.SlivZa) {
		return nil
	}
	return fmt.Errorf("sliva %s u arhivi još nema, a %s", sliv, posljedicaNovogSliva)
}

// imaNovuLetvu javlja je li među letvama neka koje u stablu još nema
func imaNovuLetvu(letve []string, slivZa map[string]string) bool {
	return slices.ContainsFunc(letve, func(l string) bool {
		_, ima := slivZa[l]
		return !ima
	})
}

// smijePrijedlog: niz se uvozi samo uz čitljivo stablo i uz pravo na letvu,
// čim se letva zna, a u novi sliv samo kad to radi globalni administrator.
// Prazna letva ovdje prolazi, jer još nema čiju provjeriti.
func (h *UvozHandler) smijePrijedlog(d *UvozPageData) error {
	if err := d.stabloCitljivo(); err != nil {
		return err
	}
	if d.Prijedlg.Letva != "" && !h.smijeZa(d.Permissions, d.Prijedlg.Letva) {
		return fmt.Errorf("letva %s nije na vašim dionicama — njezine podatke uvozi administrator njezina područja",
			d.Prijedlg.Letva)
	}
	return d.smijeSliv(d.Prijedlg.Sliv, []string{d.Prijedlg.Letva})
}
