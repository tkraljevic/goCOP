package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"gocop/internal/models"
)

// List i upis obrisanog dnevnika ne idu na površinu i ne čekaju, kao što ih
// brisanje dnevnika na čvoru koji ih je imao odnese (ON DELETE CASCADE). To
// pravilo vrijedi za svako dijete obrisanog roditelja i provjerava se prije
// primjene (roditeljObrisan, apply_roditelji.go). Dnevnik koji još nije
// stigao nije obrisan: list i upis padaju na stranom ključu i čekaju ga među
// nedovršenima.

// primijeniListDnevnika stavlja primljeni list dnevnika na površinu
func primijeniListDnevnika(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var s models.JournalSheet
	if err := json.Unmarshal(payload, &s); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, sheetUpsert, sheetArgs(&s)...)
	return err
}

// primijeniUpisDnevnika stavlja primljeni upis dnevnika na površinu
func primijeniUpisDnevnika(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var e models.JournalEntry
	if err := json.Unmarshal(payload, &e); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, entryUpsert, entryArgs(&e)...)
	return err
}
