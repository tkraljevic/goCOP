package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"gocop/internal/models"
)

// dnevnikObrisan javlja je li dnevnik obrisan: nema ga na površini, a zadnja
// verzija u knjizi ga arhivira. List i upis takvog dnevnika ne idu na
// površinu, kao što ih brisanje dnevnika na čvoru koji ih je imao odnese
// (ON DELETE CASCADE). Dnevnik koji još nije stigao nije obrisan: njegov upis
// pada na stranom ključu i čeka ga među nedovršenima.
func dnevnikObrisan(ctx context.Context, tx *sql.Tx, journalID string) (bool, error) {
	var arhiviran bool
	err := tx.QueryRowContext(ctx, `SELECT archived FROM record_versions
		WHERE entity = ? AND entity_id = ? AND NOT EXISTS (SELECT 1 FROM journals WHERE id = ?)
		ORDER BY version_id DESC LIMIT 1`, EntityJournals, journalID, journalID).Scan(&arhiviran)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return arhiviran, err
}

// primijeniListDnevnika stavlja primljeni list dnevnika na površinu
func primijeniListDnevnika(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var s models.JournalSheet
	if err := json.Unmarshal(payload, &s); err != nil {
		return err
	}
	if obrisan, err := dnevnikObrisan(ctx, tx, s.JournalID); err != nil || obrisan {
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
	if obrisan, err := dnevnikObrisan(ctx, tx, e.JournalID); err != nil || obrisan {
		return err
	}
	_, err := tx.ExecContext(ctx, entryUpsert, entryArgs(&e)...)
	return err
}
