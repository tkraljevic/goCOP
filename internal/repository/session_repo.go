package repository

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(database *sql.DB) *SessionRepository {
	return &SessionRepository{db: database}
}

// OtisakLozinke je otisak sažetka lozinke računa koji sesija pamti. Lozinka
// putuje među računalima, a sesije ne: kad se lozinka promijeni ili poništi
// na drugom računalu, ovdje stigne samo novi sažetak, pa se po otisku vidi da
// sesija pripada staroj lozinki. Otisak nikad nije prazan: prazan stupac ima
// samo sesija otvorena prije nego što se otisak pamtio.
func OtisakLozinke(sazetak string) string {
	h := sha256.Sum256([]byte("gocop:sesija:" + sazetak))
	return hex.EncodeToString(h[:16])
}

// otisakRacuna je otisak trenutne lozinke računa u bazi
func (r *SessionRepository) otisakRacuna(userID uuid.UUID) (string, error) {
	var sazetak string
	err := r.db.QueryRow("SELECT password_hash FROM users WHERE id = ?", userID.String()).Scan(&sazetak)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("greška pri čitanju lozinke računa: %w", err)
	}
	return OtisakLozinke(sazetak), nil
}

// CreateSession sprema novu sesiju. Sesija pamti otisak lozinke s kojim je
// otvorena (s.LozinkaOtisak, od sažetka koji je prijava upravo provjerila);
// bez njega uzima otisak lozinke koju račun ima u tom trenutku.
func (r *SessionRepository) CreateSession(s *models.Session) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New() // token sesije: UUIDv4, 122 slučajna bita
	}
	s.CreatedAt = time.Now().UTC()
	if s.LozinkaOtisak == "" {
		otisak, err := r.otisakRacuna(s.UserID)
		if err != nil {
			return err
		}
		s.LozinkaOtisak = otisak
	}

	_, err := r.db.Exec(`
		INSERT INTO sessions (id, user_id, ip_address, user_agent, expires_at, created_at, lozinka_otisak)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, s.ID.String(), s.UserID.String(), s.IPAddress, s.UserAgent, s.ExpiresAt, s.CreatedAt, s.LozinkaOtisak)
	if err != nil {
		return fmt.Errorf("greška pri kreiranju sesije: %w", err)
	}
	return nil
}

// ZadrziUzNovuLozinku: sesija iz koje je osoba sama promijenila lozinku
// pamti otisak nove lozinke (sazetak je sažetak koji je ta promjena
// upisala), pa i dalje vrijedi
func (r *SessionRepository) ZadrziUzNovuLozinku(sessionID, userID uuid.UUID, sazetak string) error {
	_, err := r.db.Exec("UPDATE sessions SET lozinka_otisak = ? WHERE id = ? AND user_id = ?",
		OtisakLozinke(sazetak), sessionID.String(), userID.String())
	if err != nil {
		return fmt.Errorf("greška pri zadržavanju prijave uz novu lozinku: %w", err)
	}
	return nil
}

// GetSession dohvaća aktivnu sesiju po tokenu
func (r *SessionRepository) GetSession(id uuid.UUID) (*models.Session, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, viewing_as, ip_address, user_agent, expires_at, created_at, lozinka_otisak
		FROM sessions
		WHERE id = ? AND expires_at > CURRENT_TIMESTAMP
	`, id.String())

	var s models.Session
	var idStr, userStr string
	var viewingAs sql.NullString
	err := row.Scan(&idStr, &userStr, &viewingAs, &s.IPAddress, &s.UserAgent, &s.ExpiresAt, &s.CreatedAt, &s.LozinkaOtisak)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("greška pri dohvatu sesije: %w", err)
	}

	s.ID, _ = uuid.Parse(idStr)
	s.UserID, _ = uuid.Parse(userStr)
	if viewingAs.Valid {
		if target, err := uuid.Parse(viewingAs.String); err == nil {
			s.ViewingAs = &target
		}
	}
	return &s, nil
}

// SetViewingAs upisuje čijim očima sesija gleda; nil vraća administratora sebi
func (r *SessionRepository) SetViewingAs(sessionID uuid.UUID, target *uuid.UUID) error {
	var value any
	if target != nil {
		value = target.String()
	}
	_, err := r.db.Exec("UPDATE sessions SET viewing_as = ? WHERE id = ?", value, sessionID.String())
	if err != nil {
		return fmt.Errorf("greška pri postavljanju pregleda tuđim očima: %w", err)
	}
	return nil
}

// DeleteSession briše sesiju (odjava)
func (r *SessionRepository) DeleteSession(id uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE id = ?", id.String())
	return err
}

// DeleteSessionsForUser gasi sve sesije jedne osobe (poništena lozinka,
// izgubljen uređaj). Sesija je jedini trag prijave, pa se briše lokalno na
// svakom čvoru — ne putuje razmjenom.
func (r *SessionRepository) DeleteSessionsForUser(userID uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE user_id = ?", userID.String())
	if err != nil {
		return fmt.Errorf("greška pri gašenju sesija korisnika: %w", err)
	}
	return nil
}

// DeleteOtherSessionsForUser gasi sve sesije osobe osim zadane (promjena
// lozinke: ostaje samo prijava iz koje je lozinka promijenjena)
func (r *SessionRepository) DeleteOtherSessionsForUser(userID, zadrzi uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE user_id = ? AND id <> ?", userID.String(), zadrzi.String())
	if err != nil {
		return fmt.Errorf("greška pri gašenju ostalih sesija korisnika: %w", err)
	}
	return nil
}

// CleanExpiredSessions čisti istekle sesije
func (r *SessionRepository) CleanExpiredSessions() error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP")
	return err
}
