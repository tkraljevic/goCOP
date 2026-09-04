package repository

import (
	"context"
	"database/sql"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Nazivi entiteta vidljivosti modula u knjizi verzija
const (
	EntityRoleModules = "role_modules"
	EntityUserModules = "user_modules"
)

// ModuleRepository čuva pravila vidljivosti po ulozi i iznimke po računu.
// Identitet zapisa je sama uloga odnosno račun, pa su promjene s više
// čvorova zadnja-riječ-vrijedi, kao i svaki drugi zapis.
type ModuleRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewModuleRepository(db *sql.DB, rec *ledger.Recorder) *ModuleRepository {
	return &ModuleRepository{db: db, rec: rec}
}

// RoleRules vraća pravila za uloge koje ih imaju; uloge bez pravila koriste zadano
func (r *ModuleRepository) RoleRules(ctx context.Context) (map[string][]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT role, modules FROM role_modules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var role, mods string
		if err := rows.Scan(&role, &mods); err != nil {
			return nil, err
		}
		out[role] = models.SplitModules(mods)
	}
	return out, rows.Err()
}

// SetRoleRule zapisuje pravilo za ulogu i bilježi verziju
func (r *ModuleRepository) SetRoleRule(ctx context.Context, role string, mods []string) error {
	rm := models.RoleModules{Role: role, Modules: models.SplitModules(models.JoinModules(mods)), UpdatedAt: time.Now().UTC()}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO role_modules (role, modules, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(role) DO UPDATE SET modules = excluded.modules, updated_at = excluded.updated_at`,
		rm.Role, models.JoinModules(rm.Modules), rm.UpdatedAt); err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityRoleModules, rm.Role, rm); err != nil {
		return err
	}
	return tx.Commit()
}

// UserOverride vraća iznimku računa ili nil
func (r *ModuleRepository) UserOverride(ctx context.Context, userID string) (*models.UserModules, error) {
	var um models.UserModules
	var shown, hidden string
	err := r.db.QueryRowContext(ctx, `SELECT user_id, shown, hidden, updated_at FROM user_modules WHERE user_id = ?`, userID).
		Scan(&um.UserID, &shown, &hidden, &um.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	um.Shown, um.Hidden = models.SplitModules(shown), models.SplitModules(hidden)
	return &um, nil
}

// SetUserOverride zapisuje iznimku računa; prazna iznimka se briše
func (r *ModuleRepository) SetUserOverride(ctx context.Context, userID string, shown, hidden []string) error {
	um := models.UserModules{UserID: userID, Shown: models.SplitModules(models.JoinModules(shown)),
		Hidden: models.SplitModules(models.JoinModules(hidden)), UpdatedAt: time.Now().UTC()}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(um.Shown) == 0 && len(um.Hidden) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_modules WHERE user_id = ?`, userID); err != nil {
			return err
		}
		if _, err := r.rec.Archive(ctx, tx, EntityUserModules, userID, um); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_modules (user_id, shown, hidden, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET shown = excluded.shown, hidden = excluded.hidden, updated_at = excluded.updated_at`,
		userID, models.JoinModules(um.Shown), models.JoinModules(um.Hidden), um.UpdatedAt); err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityUserModules, userID, um); err != nil {
		return err
	}
	return tx.Commit()
}
