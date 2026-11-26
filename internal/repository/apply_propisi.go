package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// primjenePropisa su propisi iz Narodnih novina i njihove izmjene. Upis je
// isti kao pri lokalnom spremanju (upisiPropis, upisiIzmjenuPropisa), s
// kazalom dijelova, a kazalo pri brisanju odlazi kaskadom.
var primjenePropisa = map[string]primjena{
	EntityPropisi:        {upis: primijeniPropis, brisanje: obrisi(`DELETE FROM propisi WHERE id = ?`)},
	EntityPropisiIzmjene: {upis: primijeniIzmjenuPropisa, brisanje: obrisi(`DELETE FROM propisi_izmjene WHERE id = ?`)},
}

// primijeniPropis stavlja primljeni propis na površinu, s kazalom
func primijeniPropis(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var p models.Propis
	if err := json.Unmarshal(v.Payload, &p); err != nil {
		return err
	}
	return upisiPropis(ctx, tx, &p)
}

// primijeniIzmjenuPropisa stavlja primljenu izmjenu na površinu, s kazalom.
// Izmjena koja stigne prije svog propisa padne na stranom ključu i čeka ga
// među nedovršenima.
func primijeniIzmjenuPropisa(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var iz models.IzmjenaPropisa
	if err := json.Unmarshal(v.Payload, &iz); err != nil {
		return err
	}
	return upisiIzmjenuPropisa(ctx, tx, &iz)
}
