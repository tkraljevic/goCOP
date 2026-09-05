package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// EntityUsers i EntityDuties su nazivi entiteta u knjizi verzija
const (
	EntityUsers  = "users"
	EntityDuties = "duties"
)

type UserRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

// NewUserRepository traži Recorder: korisnici i zaduženja se sinkroniziraju
// između čvorova, pa svaki upis ostavlja verziju
func NewUserRepository(database *sql.DB, rec *ledger.Recorder) *UserRepository {
	return &UserRepository{db: database, rec: rec}
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// userColumns su stupci matičnog računa u stalnom redoslijedu. Jedan popis za
// sva čitanja: stupac dodan tablici ne može promaknuti jednom od njih.
const userColumns = `id, username, password_hash, full_name, title, is_global_admin,
	must_change_password, org_type, org_name, phone, mobile_phone, short_phone, short_mobile, email,
	is_active, last_login_at, created_at, updated_at`

// userColumnsOf vraća isti popis s prefiksom tablice, za upite sa spajanjem
func userColumnsOf(prefix string) string {
	parts := strings.Split(userColumns, ",")
	for i, c := range parts {
		parts[i] = prefix + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

type rowScanner interface {
	Scan(dest ...any) error
}

// scanUser čita jedan red u redoslijedu userColumns
func scanUser(row rowScanner) (models.User, error) {
	var u models.User
	var idStr, orgType string
	var title, orgName, phone, mobile, short, shortMobile, email sql.NullString
	var isAdmin, mustChange, isActive int
	var lastLogin sql.NullTime

	err := row.Scan(
		&idStr, &u.Username, &u.PasswordHash, &u.FullName, &title, &isAdmin,
		&mustChange, &orgType, &orgName, &phone, &mobile, &short, &shortMobile, &email,
		&isActive, &lastLogin, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return u, err
	}
	u.ID, _ = uuid.Parse(idStr)
	u.Title, u.OrgName, u.Phone = title.String, orgName.String, phone.String
	u.MobilePhone, u.ShortPhone, u.Email = mobile.String, short.String, email.String
	u.ShortMobile = shortMobile.String
	u.OrgType = models.OrgType(orgType)
	u.IsGlobalAdmin, u.MustChangePassword, u.IsActive = isAdmin != 0, mustChange != 0, isActive != 0
	if lastLogin.Valid {
		t := lastLogin.Time.UTC()
		u.LastLoginAt = &t
	}
	return u, nil
}

// userVersion je oblik korisnika u knjizi verzija.
//
// Lozinka nikad ne ide u JSON prema pregledniku, pa je models.User skriva.
// Između čvorova ipak mora putovati: svaki je čvor puna kopija i osoba se
// mora moći prijaviti na bilo koji od njih, pa i kad središnji padne. Putuje
// bcrypt sažetak, kroz uzajamno autenticiran kanal između članova mreže.
type userVersion struct {
	models.User
	PasswordHash string `json:"password_hash"`
}

func versionOfUser(u models.User) userVersion {
	return userVersion{User: u, PasswordHash: u.PasswordHash}
}

// getUserTx čita korisnika (bez zaduženja) unutar transakcije — za verziju
func getUserTx(ctx context.Context, q rowQuerier, id string) (models.User, error) {
	return scanUser(q.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id))
}

// getDutyTx čita jedno zaduženje unutar transakcije — za verziju
func getDutyTx(ctx context.Context, q rowQuerier, id string) (models.Duty, error) {
	var d models.Duty
	var idStr, userID, role, scope string
	var sectorID, sectionCodes, reason, assignedBy sql.NullString
	var areaID sql.NullInt64
	var expiresAt sql.NullTime
	var isPrimary, isTemp, isActive int

	err := q.QueryRowContext(ctx, `
		SELECT id, user_id, title, role, scope_type, sector_id, area_id, section_codes,
		       is_primary, is_temporary, reason, assigned_by, created_at, expires_at, is_active
		FROM duties WHERE id = ?`, id).Scan(
		&idStr, &userID, &d.Title, &role, &scope, &sectorID, &areaID, &sectionCodes,
		&isPrimary, &isTemp, &reason, &assignedBy, &d.CreatedAt, &expiresAt, &isActive)
	if err != nil {
		return d, err
	}
	d.ID, _ = uuid.Parse(idStr)
	d.UserID, _ = uuid.Parse(userID)
	d.Role, d.ScopeType = models.Role(role), models.ScopeType(scope)
	if sectorID.Valid {
		v := sectorID.String
		d.SectorID = &v
	}
	if areaID.Valid {
		v := int(areaID.Int64)
		d.AreaID = &v
	}
	d.SectionCodes = sectionCodes.String
	d.Reason = reason.String
	if assignedBy.Valid {
		if parsed, err := uuid.Parse(assignedBy.String); err == nil {
			d.AssignedBy = &parsed
		}
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		d.ExpiresAt = &t
	}
	d.IsPrimary, d.IsTemporary, d.IsActive = isPrimary != 0, isTemp != 0, isActive != 0
	return d, nil
}

// recordDuties ostavlja verziju za svako navedeno zaduženje, kakvo je na površini
func (r *UserRepository) recordDuties(ctx context.Context, tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		d, err := getDutyTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := r.rec.Record(ctx, tx, EntityDuties, id, d); err != nil {
			return err
		}
	}
	return nil
}

func idsOf(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetUserByID dohvaća korisnika po ID-u zajedno sa svim njegovim funkcijama (duties)
func (r *UserRepository) GetUserByID(id uuid.UUID) (*models.User, error) {
	u, err := scanUser(r.db.QueryRow("SELECT "+userColumns+" FROM users WHERE id = ?", id.String()))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("greška pri dohvatu korisnika: %w", err)
	}

	// Učitaj sve funkcije i zaduženja
	duties, err := r.GetDutiesForUser(u.ID)
	if err == nil {
		u.Duties = duties
	}

	return &u, nil
}

// GetUserByUsername dohvaća korisnika po korisničkom imenu
func (r *UserRepository) GetUserByUsername(username string) (*models.User, error) {
	u, err := scanUser(r.db.QueryRow(
		"SELECT "+userColumns+" FROM users WHERE username = ? COLLATE NOCASE", username))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("greška pri dohvatu korisnika: %w", err)
	}

	duties, err := r.GetDutiesForUser(u.ID)
	if err == nil {
		u.Duties = duties
	}

	return &u, nil
}

// ListUsers vraća korisnike, opcionalno filtrirane po sektoru, području,
// ulozi, stanju računa ili tekstu pretrage
func (r *UserRepository) ListUsers(sectorID string, areaID int, role, search, status string) ([]models.User, error) {
	query := `
		SELECT DISTINCT ` + userColumnsOf("u") + `
		FROM users u
		LEFT JOIN duties d ON u.id = d.user_id AND d.is_active = 1
		WHERE 1=1
	`
	var args []any

	if sectorID != "" {
		if sectorID == "DIREKCIJA" {
			query += " AND (d.sector_id = 'DIREKCIJA' OR u.org_name LIKE '%Direkcija%' OR d.role IN ('NATIONAL_LEADER', 'NATIONAL_DEPUTY', 'GLOBAL_ADMIN'))"
		} else {
			query += " AND d.sector_id = ?"
			args = append(args, sectorID)
		}
	}
	if areaID > 0 {
		query += " AND d.area_id = ?"
		args = append(args, areaID)
	}
	if role != "" {
		if role == "GLOBAL_ADMIN" {
			query += " AND (u.is_global_admin = 1 OR d.role = 'GLOBAL_ADMIN')"
		} else {
			query += " AND d.role = ?"
			args = append(args, role)
		}
	}
	// Stanje računa: isti izraz kao models.User.AccountState, samo u SQL-u
	switch models.AccountState(status) {
	case models.AccountDisabled:
		query += " AND u.is_active = 0"
	case models.AccountPending:
		query += " AND u.is_active = 1 AND u.last_login_at IS NULL"
	case models.AccountActive:
		query += " AND u.is_active = 1 AND u.last_login_at IS NOT NULL"
	}
	if s := strings.TrimSpace(search); s != "" {
		searchLike := "%" + s + "%"
		query += ` AND (
			u.full_name LIKE ? OR
			u.username LIKE ? OR
			u.title LIKE ? OR
			u.org_name LIKE ? OR
			u.phone LIKE ? OR
			u.mobile_phone LIKE ? OR
			u.short_phone LIKE ? OR
			u.short_mobile LIKE ? OR
			u.email LIKE ? OR
			d.title LIKE ? OR
			d.section_codes LIKE ?
		)`
		for i := 0; i < 11; i++ {
			args = append(args, searchLike)
		}
	}

	query += " ORDER BY u.full_name ASC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri listanju korisnika: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("greška pri čitanju reda: %w", err)
		}

		duties, _ := r.GetDutiesForUser(u.ID)
		u.Duties = duties

		users = append(users, u)
	}

	return users, nil
}

// CreateUser sprema korisnika i njegovu početnu funkciju
func (r *UserRepository) CreateUser(u *models.User, initialDuty *models.Duty) error {
	if u.ID == uuid.Nil {
		newID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		u.ID = newID
	}
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now

	isActiveInt := 0
	if u.IsActive {
		isActiveInt = 1
	}
	isAdminInt := 0
	if u.IsGlobalAdmin {
		isAdminInt = 1
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	mustChangeInt := 1
	if !u.MustChangePassword {
		mustChangeInt = 0
	}

	_, err = tx.Exec(`
		INSERT INTO users (
			id, username, password_hash, full_name, title, is_global_admin,
			must_change_password, org_type, org_name, phone, mobile_phone, short_phone, short_mobile, email,
			is_active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		u.ID.String(), u.Username, u.PasswordHash, u.FullName, u.Title, isAdminInt,
		mustChangeInt, string(u.OrgType), u.OrgName, u.Phone, u.MobilePhone, u.ShortPhone, u.ShortMobile, u.Email,
		isActiveInt, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("greška pri unosu korisnika: %w", err)
	}

	if initialDuty != nil {
		if initialDuty.ID == uuid.Nil {
			initialDuty.ID, _ = uuid.NewV7()
		}
		initialDuty.UserID = u.ID
		initialDuty.CreatedAt = now
		initialDuty.IsActive = true

		_, err = tx.Exec(`
			INSERT INTO duties (
				id, user_id, title, role, scope_type, sector_id, area_id, section_codes,
				is_primary, is_temporary, reason, assigned_by, created_at, expires_at, is_active
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, ?, ?, 1)
		`,
			initialDuty.ID.String(), initialDuty.UserID.String(), initialDuty.Title,
			string(initialDuty.Role), string(initialDuty.ScopeType), initialDuty.SectorID,
			initialDuty.AreaID, initialDuty.SectionCodes, initialDuty.Reason,
			initialDuty.AssignedBy, initialDuty.CreatedAt, initialDuty.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("greška pri unosu funkcije: %w", err)
		}
	}

	ctx := context.Background()
	if _, err := r.rec.Record(ctx, tx, EntityUsers, u.ID.String(), versionOfUser(*u)); err != nil {
		return err
	}
	if initialDuty != nil {
		if _, err := r.rec.Record(ctx, tx, EntityDuties, initialDuty.ID.String(), initialDuty); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpdateUser ažurira profil korisnika
func (r *UserRepository) UpdateUser(u *models.User) error {
	now := time.Now().UTC()
	u.UpdatedAt = now

	isActiveInt := 0
	if u.IsActive {
		isActiveInt = 1
	}
	isAdminInt := 0
	if u.IsGlobalAdmin {
		isAdminInt = 1
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var query string
	var args []any

	if u.PasswordHash != "" {
		query = `
			UPDATE users SET
				username = ?, password_hash = ?, full_name = ?, title = ?,
				is_global_admin = ?, org_type = ?, org_name = ?, phone = ?,
				mobile_phone = ?, short_phone = ?, short_mobile = ?, email = ?, is_active = ?, updated_at = ?
			WHERE id = ?
		`
		args = []any{
			u.Username, u.PasswordHash, u.FullName, u.Title,
			isAdminInt, string(u.OrgType), u.OrgName, u.Phone,
			u.MobilePhone, u.ShortPhone, u.ShortMobile, u.Email, isActiveInt, u.UpdatedAt, u.ID.String(),
		}
	} else {
		query = `
			UPDATE users SET
				username = ?, full_name = ?, title = ?,
				is_global_admin = ?, org_type = ?, org_name = ?, phone = ?,
				mobile_phone = ?, short_phone = ?, short_mobile = ?, email = ?, is_active = ?, updated_at = ?
			WHERE id = ?
		`
		args = []any{
			u.Username, u.FullName, u.Title,
			isAdminInt, string(u.OrgType), u.OrgName, u.Phone,
			u.MobilePhone, u.ShortPhone, u.ShortMobile, u.Email, isActiveInt, u.UpdatedAt, u.ID.String(),
		}
	}

	_, err = tx.Exec(query, args...)
	if err != nil {
		return err
	}

	// Verzija nosi stanje s površine (uključivo lozinku koja se nije mijenjala)
	ctx := context.Background()
	saved, err := getUserTx(ctx, tx, u.ID.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityUsers, u.ID.String(), versionOfUser(saved)); err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteUser uklanja korisnika i sva zaduženja s površine; u knjizi ostaju
// arhivirani. Sesije su lokalne i ne sinkroniziraju se, pa se samo brišu.
func (r *UserRepository) DeleteUser(id uuid.UUID) error {
	ctx := context.Background()
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := getUserTx(ctx, tx, id.String())
	if err == sql.ErrNoRows {
		return fmt.Errorf("korisnik nije pronađen")
	}
	if err != nil {
		return err
	}

	dutyIDs, err := idsOf(ctx, tx, "SELECT id FROM duties WHERE user_id = ?", id.String())
	if err != nil {
		return err
	}
	for _, dutyID := range dutyIDs {
		d, err := getDutyTx(ctx, tx, dutyID)
		if err != nil {
			return err
		}
		if _, err := r.rec.Archive(ctx, tx, EntityDuties, dutyID, d); err != nil {
			return err
		}
	}

	_, _ = tx.Exec("DELETE FROM duties WHERE user_id = ?", id.String())
	_, _ = tx.Exec("DELETE FROM sessions WHERE user_id = ?", id.String())
	if _, err := tx.Exec("DELETE FROM users WHERE id = ?", id.String()); err != nil {
		return fmt.Errorf("greška pri brisanju korisnika: %w", err)
	}

	if _, err := r.rec.Archive(ctx, tx, EntityUsers, id.String(), versionOfUser(current)); err != nil {
		return err
	}
	return tx.Commit()
}

// AddDuty dodaje novu funkciju ili zaduženje (stalno ili privremena ispomoć)
func (r *UserRepository) AddDuty(d *models.Duty) error {
	if d.ID == uuid.Nil {
		newID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		d.ID = newID
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.IsActive = true

	isPrimaryInt := 0
	if d.IsPrimary {
		isPrimaryInt = 1
	}
	isTempInt := 0
	if d.IsTemporary {
		isTempInt = 1
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ctx := context.Background()

	// Ako je ovo nova primarna dužnost, resetiraj prethodne primarne —
	// i zabilježi im verziju, jer im se stanje promijenilo
	var demoted []string
	if d.IsPrimary {
		demoted, err = idsOf(ctx, tx, "SELECT id FROM duties WHERE user_id = ? AND is_primary = 1", d.UserID.String())
		if err != nil {
			return err
		}
		_, _ = tx.Exec("UPDATE duties SET is_primary = 0 WHERE user_id = ?", d.UserID.String())
	}

	var byStr *string
	if d.AssignedBy != nil {
		s := d.AssignedBy.String()
		byStr = &s
	}

	_, err = tx.Exec(`
		INSERT INTO duties (
			id, user_id, title, role, scope_type, sector_id, area_id, section_codes,
			is_primary, is_temporary, reason, assigned_by, created_at, expires_at, is_active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	`,
		d.ID.String(), d.UserID.String(), d.Title, string(d.Role), string(d.ScopeType),
		d.SectorID, d.AreaID, d.SectionCodes, isPrimaryInt, isTempInt,
		d.Reason, byStr, d.CreatedAt, d.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("greška pri spremanju dužnosti: %w", err)
	}

	if err := r.recordDuties(ctx, tx, demoted); err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityDuties, d.ID.String(), d); err != nil {
		return err
	}

	return tx.Commit()
}

// RevokeDuty opoziva funkciju ili privremenu ispomoć
func (r *UserRepository) RevokeDuty(id uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("UPDATE duties SET is_active = 0 WHERE id = ?", id.String())
	if err != nil {
		return err
	}

	if err := r.recordDuties(context.Background(), tx, []string{id.String()}); err != nil {
		return err
	}

	return tx.Commit()
}

// GetDutiesForUser dohvaća sve aktivne funkcije korisnika
func (r *UserRepository) GetDutiesForUser(userID uuid.UUID) ([]models.Duty, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, title, role, scope_type, sector_id, area_id, section_codes,
		       is_primary, is_temporary, reason, assigned_by, created_at, expires_at, is_active
		FROM duties
		WHERE user_id = ? AND is_active = 1
		  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		ORDER BY is_primary DESC, created_at ASC
	`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var duties []models.Duty
	for rows.Next() {
		var d models.Duty
		var idStr, userStr, sectorStr, sectionStr, byStr, reasonStr sql.NullString
		var areaInt sql.NullInt64
		var isPrimaryInt, isTempInt, isActiveInt int

		err := rows.Scan(
			&idStr, &userStr, &d.Title, &d.Role, &d.ScopeType, &sectorStr, &areaInt,
			&sectionStr, &isPrimaryInt, &isTempInt, &reasonStr, &byStr,
			&d.CreatedAt, &d.ExpiresAt, &isActiveInt,
		)
		if err != nil {
			return nil, err
		}

		d.ID, _ = uuid.Parse(idStr.String)
		d.UserID, _ = uuid.Parse(userStr.String)
		d.IsPrimary = isPrimaryInt == 1
		d.IsTemporary = isTempInt == 1
		d.IsActive = isActiveInt == 1
		if reasonStr.Valid {
			d.Reason = reasonStr.String
		}
		if sectorStr.Valid {
			s := sectorStr.String
			d.SectorID = &s
		}
		if areaInt.Valid {
			a := int(areaInt.Int64)
			d.AreaID = &a
		}
		if sectionStr.Valid {
			d.SectionCodes = sectionStr.String
		}
		if byStr.Valid {
			byID, _ := uuid.Parse(byStr.String)
			d.AssignedBy = &byID
		}

		duties = append(duties, d)
	}

	return duties, nil
}

// GetUserPermissions izračunava ukupne ovlasti korisnika iz svih njegovih dužnosti
func (r *UserRepository) GetUserPermissions(userID uuid.UUID) (*models.UserPermissions, error) {
	u, err := r.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, fmt.Errorf("korisnik ne postoji")
	}

	return models.NewUserPermissions(*u), nil
}

// ListSectors vraća sve sektore
// GlobalAdminContact vraća ime, telefon i e-poštu glavnog administratora
// koji ima bar jedan kontakt; on na stranici prijave pomaže oko prijave i
// početne lozinke. Prednost ima onaj s mobitelom i e-poštom, pa najstariji račun.
func (r *UserRepository) GlobalAdminContact() (name, phone, email string, ok bool) {
	err := r.db.QueryRow(`
		SELECT full_name, CASE WHEN mobile_phone <> '' THEN mobile_phone ELSE phone END, email
		FROM users
		WHERE is_global_admin = 1 AND is_active = 1
		  AND (mobile_phone <> '' OR phone <> '' OR email <> '')
		ORDER BY (mobile_phone <> '') DESC, (email <> '') DESC, created_at ASC
		LIMIT 1`).Scan(&name, &phone, &email)
	if err != nil {
		return "", "", "", false
	}
	return name, phone, email, true
}

func (r *UserRepository) ListSectors() ([]models.Sector, error) {
	rows, err := r.db.Query("SELECT id, name, vgo_name, center_cop, address, phone, email FROM sectors ORDER BY CASE WHEN id = 'DIREKCIJA' THEN 0 ELSE 1 END, id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sectors []models.Sector
	for rows.Next() {
		var s models.Sector
		if err := rows.Scan(&s.ID, &s.Name, &s.VgoName, &s.CenterCop, &s.Address, &s.Phone, &s.Email); err != nil {
			return nil, err
		}
		sectors = append(sectors, s)
	}
	return sectors, nil
}

// ListAreas vraća branjena područja
func (r *UserRepository) ListAreas(sectorID string) ([]models.Area, error) {
	query := "SELECT id, sector_id, name, vgi_name, subcenter, COALESCE(contractor_name, '') FROM areas"
	var args []any
	if sectorID != "" {
		query += " WHERE sector_id = ?"
		args = append(args, sectorID)
	}
	query += " ORDER BY id ASC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var areas []models.Area
	for rows.Next() {
		var a models.Area
		if err := rows.Scan(&a.ID, &a.SectorID, &a.Name, &a.VgiName, &a.Subcenter, &a.ContractorName); err != nil {
			return nil, err
		}
		areas = append(areas, a)
	}
	return areas, nil
}

// ChangePassword ažurira lozinku korisnika i postavlja must_change_password na 0
func (r *UserRepository) ChangePassword(userID uuid.UUID, newPasswordHash string) error {
	ctx := context.Background()
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE users
		SET password_hash = ?, must_change_password = 0, updated_at = ?
		WHERE id = ?
	`, newPasswordHash, time.Now().UTC(), userID.String()); err != nil {
		return fmt.Errorf("greška pri ažuriranju lozinke u bazi: %w", err)
	}

	// Lozinka mora stići i na druge čvorove — korisnik se prijavljuje bilo gdje
	saved, err := getUserTx(ctx, tx, userID.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityUsers, userID.String(), versionOfUser(saved)); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkLogin bilježi prijavu korisnika, ali samo jednom dnevno.
//
// Prijava je česta, a knjiga verzija je zajednička svim čvorovima: kad bi
// svaka prijava pisala verziju, sinkronizacija bi prenosila stotine zapisa
// dnevno bez ijednog novog podatka. Točnost na dan je dovoljna za pitanje
// zbog kojeg se ovo bilježi — tko je preuzeo račun i tko program stvarno
// koristi.
func (r *UserRepository) MarkLogin(userID uuid.UUID, at time.Time) error {
	at = at.UTC()
	ctx := context.Background()

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var last sql.NullTime
	if err := tx.QueryRow("SELECT last_login_at FROM users WHERE id = ?", userID.String()).Scan(&last); err != nil {
		return err
	}
	if last.Valid && sameDay(last.Time.UTC(), at) {
		return nil
	}

	if _, err := tx.Exec("UPDATE users SET last_login_at = ? WHERE id = ?", at, userID.String()); err != nil {
		return fmt.Errorf("greška pri bilježenju prijave: %w", err)
	}

	saved, err := getUserTx(ctx, tx, userID.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityUsers, userID.String(), versionOfUser(saved)); err != nil {
		return err
	}
	return tx.Commit()
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// DashboardStats sadrži zbirne brojače za početni ekran
type DashboardStats struct {
	TotalUsers        int
	TotalDuties       int
	TotalSectors      int
	TotalAreas        int
	TotalSections     int
	TotalStations     int
	TotalWatercourses int
	StationLinks      int
}

// GetDashboardStats dohvaća agregirane operativne podatke za dashboard
func (r *UserRepository) GetDashboardStats() (DashboardStats, error) {
	var s DashboardStats
	err := r.db.QueryRow(`
		SELECT 
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM duties WHERE is_active = 1),
			(SELECT COUNT(*) FROM sectors),
			(SELECT COUNT(*) FROM areas),
			(SELECT COUNT(*) FROM sections),
			(SELECT COUNT(*) FROM stations),
			(SELECT COUNT(*) FROM watercourses),
			(SELECT COUNT(*) FROM section_stations)
	`).Scan(&s.TotalUsers, &s.TotalDuties, &s.TotalSectors, &s.TotalAreas,
		&s.TotalSections, &s.TotalStations, &s.TotalWatercourses, &s.StationLinks)
	return s, err
}
