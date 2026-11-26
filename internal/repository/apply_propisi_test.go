package repository

import (
	"testing"

	"gocop/internal/ledger"
)

// Primjena propisa i izmjene s drugog računala javlja grešku kad sadržaj
// nije zapis (pokvaren JSON) i kad baza ne primi upis (zatvorena
// transakcija), pa ApplyVersions verziju ostavlja među nedovršenima umjesto
// da je tiho preskoči (Odluke (25. 11. 2026.)).
func TestPrimjenaPropisaJaviGresku(t *testing.T) {
	for _, s := range []struct {
		entitet string
		upis    radnjaNaPovrsini
	}{
		{EntityPropisi, primjenePropisa[EntityPropisi].upis},
		{EntityPropisiIzmjene, primjenePropisa[EntityPropisiIzmjene].upis},
	} {
		t.Run(s.entitet, func(t *testing.T) {
			c := noviCvor(t, "server")
			tx, err := c.db.BeginTx(ctxRaz, nil)
			nuzno(t, err)
			defer tx.Rollback()
			pokvaren := ledger.Version{Entity: s.entitet, EntityID: "nn-2001-1-1", Payload: []byte(`{"id": `)}
			if err := s.upis(ctxRaz, tx, pokvaren); err == nil {
				t.Error("pokvaren sadržaj primijenjen je bez greške")
			}
			nuzno(t, tx.Rollback())
			ispravan := ledger.Version{Entity: s.entitet, EntityID: "nn-2001-1-1",
				Payload: []byte(`{"id":"nn-2001-1-1","propis_id":"` + osnovaPropis + `"}`)}
			if err := s.upis(ctxRaz, tx, ispravan); err == nil {
				t.Error("upis u zatvorenu transakciju prošao je bez greške")
			}
		})
	}
}
