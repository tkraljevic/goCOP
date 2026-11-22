package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// StanjeIzvanSluzbe je filtar popisa djelatnika: računi osoba koje su otišle
// iz službe (models.User.IzvanSluzbe)
const StanjeIzvanSluzbe models.AccountState = "IZVAN_SLUZBE"

// RacuniSaZaduzenjem su oznake računa koji imaju ili su ikad imali
// zaduženje, i opozvano i isteklo; račun bez zaduženja koji nije među njima
// je nov
func (r *UserRepository) RacuniSaZaduzenjem() (map[uuid.UUID]bool, error) {
	ids, err := idsOf(context.Background(), r.db, `SELECT DISTINCT user_id FROM duties`)
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		if u, e := uuid.Parse(id); e == nil {
			out[u] = true
		}
	}
	return out, err
}

// OdlazakIzSluzbe bilježi da je osoba otišla iz službe: u jednoj
// transakciji isključi prijavu, upiše razlog i dan odlaska i opozove sva
// zaduženja koja još vrijede. Račun ostaje, da upisi i očitanja zadrže ime.
// Vraća oznake opozvanih zaduženja.
func (r *UserRepository) OdlazakIzSluzbe(userID uuid.UUID, razlog models.RazlogOdlaska, dan time.Time) ([]string, error) {
	ctx := context.Background()
	tx, err := r.pocniIzmjenu(ctx, EntityUsers, userID.String())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := iskljuciZbogOdlaska(ctx, tx, userID.String(), razlog, dan); err != nil {
		return nil, err
	}
	ids, err := opozoviVazeca(ctx, tx, userID.String())
	if err == nil {
		err = r.recordDuties(ctx, tx, ids)
	}
	if err == nil {
		err = r.zapisiRacun(ctx, tx, userID.String())
	}
	if err != nil {
		return nil, err
	}
	return ids, tx.Commit()
}

// iskljuciZbogOdlaska isključuje prijavu i upisuje razlog i dan odlaska
func iskljuciZbogOdlaska(ctx context.Context, tx *sql.Tx, id string, razlog models.RazlogOdlaska, dan time.Time) error {
	res, err := tx.ExecContext(ctx, `UPDATE users SET is_active = 0, odlazak = ?, odlazak_dan = ?, updated_at = ? WHERE id = ?`,
		string(razlog), dan.UTC(), time.Now().UTC(), id)
	var n int64
	if err == nil {
		n, err = res.RowsAffected()
	}
	if err == nil && n == 0 {
		err = fmt.Errorf("korisnik nije pronađen")
	}
	return err
}

// opozoviVazeca opoziva zaduženja osobe koja još vrijede i vraća njihove
// oznake, redom dodjele
func opozoviVazeca(ctx context.Context, tx *sql.Tx, userID string) ([]string, error) {
	ids, err := idsOf(ctx, tx, `SELECT id FROM duties WHERE user_id = ? AND is_active = 1
		AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP) ORDER BY created_at`, userID)
	for i := 0; err == nil && i < len(ids); i++ {
		_, err = tx.ExecContext(ctx, `UPDATE duties SET is_active = 0 WHERE id = ?`, ids[i])
	}
	return ids, err
}

// zapisiRacun upisuje verziju računa kakav je sad na površini
func (r *UserRepository) zapisiRacun(ctx context.Context, tx *sql.Tx, id string) error {
	saved, err := getUserTx(ctx, tx, id)
	if err == nil {
		_, err = r.zapisi(ctx, tx, EntityUsers, id, versionOfUser(saved))
	}
	return err
}
