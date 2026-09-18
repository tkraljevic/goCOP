package repository

import (
	"context"
	"database/sql"
	"time"

	"gocop/internal/models"
)

// JavniSpremiste je ono što uvoznik javnih vodostaja treba od baze: letve
// označene za preuzimanje, što na njima već ima i upis novoga kroz istu
// stazu kao zalijepljeni uvoz, s verzijom u knjizi.
type JavniSpremiste struct {
	db       *sql.DB
	readings *ReadingRepository
}

func NewJavniSpremiste(db *sql.DB, readings *ReadingRepository) *JavniSpremiste {
	return &JavniSpremiste{db: db, readings: readings}
}

// LetveZaPreuzimanje su postaje s javnim ID-om i uključenim preuzimanjem
func (s *JavniSpremiste) LetveZaPreuzimanje(ctx context.Context) ([]models.Station, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stationColumns+` FROM stations s
		WHERE s.javni_uvoz = 1 AND s.javni_id > 0 ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Station
	for rows.Next() {
		st, err := scanStation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Postojeca vraća trenutke na koje letva već ima očitanje, iz bilo kojeg izvora
func (s *JavniSpremiste) Postojeca(ctx context.Context, stationID string, od, do time.Time) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT measured_at FROM readings
		WHERE station_id = ? AND measured_at BETWEEN ? AND ?`, stationID, od.UTC(), do.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out[t.UTC().Unix()] = true
	}
	return out, rows.Err()
}

// Upisi upisuje nova očitanja s verzijom u knjizi
func (s *JavniSpremiste) Upisi(ctx context.Context, ocitanja []models.Reading) (int, error) {
	return s.readings.ImportBatch(ctx, ocitanja)
}
