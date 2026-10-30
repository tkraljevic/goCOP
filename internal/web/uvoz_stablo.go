package web

import (
	"fmt"

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

// smijeUvesti: posebni izvor se uvozi samo uz čitljivo stablo i uz pravo na
// svaku letvu na koju piše.
func (h *UvozHandler) smijeUvesti(d *UvozPageData, f izvori.Format, z izvori.Zadatak) error {
	if err := d.stabloCitljivo(); err != nil {
		return err
	}
	return h.smijeSveLetve(d.Permissions, f, z)
}

// smijePrijedlog: niz se uvozi samo uz čitljivo stablo i uz pravo na letvu,
// čim se letva zna. Prazna letva ovdje prolazi, jer još nema čiju provjeriti.
func (h *UvozHandler) smijePrijedlog(d *UvozPageData) error {
	if err := d.stabloCitljivo(); err != nil {
		return err
	}
	if d.Prijedlg.Letva != "" && !h.smijeZa(d.Permissions, d.Prijedlg.Letva) {
		return fmt.Errorf("letva %s nije na vašim dionicama — njezine podatke uvozi administrator njezina područja",
			d.Prijedlg.Letva)
	}
	return nil
}
