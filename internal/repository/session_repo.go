package repository

import (
	"database/sql"
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

// CreateSession sprema novu sesiju
func (r *SessionRepository) CreateSession(s *models.Session) error {
	if s.ID == uuid.Nil {
		newID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("greška pri generiranju UUIDv7 za sesiju: %w", err)
		}
		s.ID = newID
	}
	s.CreatedAt = time.Now().UTC()

	_, err := r.db.Exec(`
		INSERT INTO sessions (id, user_id, ip_address, user_agent, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, s.ID.String(), s.UserID.String(), s.IPAddress, s.UserAgent, s.ExpiresAt, s.CreatedAt)
	if err != nil {
		return fmt.Errorf("greška pri kreiranju sesije: %w", err)
	}
	return nil
}

// GetSession dohvaća aktivnu sesiju po tokenu (UUIDv7)
func (r *SessionRepository) GetSession(id uuid.UUID) (*models.Session, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, viewing_as, ip_address, user_agent, expires_at, created_at
		FROM sessions
		WHERE id = ? AND expires_at > CURRENT_TIMESTAMP
	`, id.String())

	var s models.Session
	var idStr, userStr string
	var viewingAs sql.NullString
	err := row.Scan(&idStr, &userStr, &viewingAs, &s.IPAddress, &s.UserAgent, &s.ExpiresAt, &s.CreatedAt)
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

// CleanExpiredSessions čisti istekle sesije
func (r *SessionRepository) CleanExpiredSessions() error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP")
	return err
}
