package repository

import (
	"context"
	"database/sql"
	"time"

	"gocop/internal/models"
)

// Stanje ispravka telemetrije po ručnom očitanju, za stranicu letve i popis.
// Računa se na svakom čvoru iz očitanja koja ima: ispravak je onaj kojim je
// ispravljeno zadnje preuzeto očitanje (level_cm − sirovo_cm).

// IspravciTelemetrije vraća, po postaji, ispravak zadnjeg preuzetog očitanja
// za letve s uključenim preuzimanjem; letve bez ispravka nisu u mapi.
func (r *ReadingRepository) IspravciTelemetrije(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT s.id, (SELECT z.level_cm - COALESCE(z.sirovo_cm, z.level_cm) FROM readings z
			WHERE z.station_id = s.id AND z.source = ? AND z.level_cm IS NOT NULL
			ORDER BY z.measured_at DESC LIMIT 1)
		FROM stations s WHERE `+uvjetPreuzimanja, models.ReadingSourceImport)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var pomak sql.NullInt64
		if err := rows.Scan(&id, &pomak); err != nil {
			return nil, err
		}
		if pomak.Int64 != 0 {
			out[id] = int(pomak.Int64)
		}
	}
	return out, rows.Err()
}

// StanjeIspravka vraća stanje ispravka jedne letve: ispravak zadnjeg
// preuzetog očitanja i zadnje ručno očitanje iz zadnjih deset dana uz
// vrijednost mjernog uređaja najbližu njemu (unutar sat i pol).
func (r *ReadingRepository) StanjeIspravka(ctx context.Context, stationID string, sada time.Time) (models.StanjeIspravka, error) {
	var s models.StanjeIspravka
	var pomak sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT level_cm - COALESCE(sirovo_cm, level_cm) FROM readings
		WHERE station_id = ? AND source = ? AND level_cm IS NOT NULL ORDER BY measured_at DESC LIMIT 1`,
		stationID, models.ReadingSourceImport).Scan(&pomak)
	if err != nil && err != sql.ErrNoRows {
		return s, err
	}
	s.PomakCm = int(pomak.Int64)
	err = r.db.QueryRowContext(ctx, `SELECT measured_at, level_cm FROM readings
		WHERE station_id = ? AND source = ? AND level_cm IS NOT NULL AND measured_at >= ?
		ORDER BY measured_at DESC LIMIT 1`, stationID, models.ReadingSourceManual, sada.AddDate(0, 0, -10).UTC()).Scan(&s.RucnoKad, &s.RucnoCm)
	if err == sql.ErrNoRows {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.SirovoCm, s.ImaRucno, err = r.uredjajUz(ctx, stationID, s.RucnoKad)
	return s, err
}

// uredjajUz je vrijednost mjernog uređaja najbliža zadanom trenutku, unutar
// sat i pol; ok je netočno kad takve nema.
func (r *ReadingRepository) uredjajUz(ctx context.Context, stationID string, kad time.Time) (int, bool, error) {
	const blizu = 90 * time.Minute
	rows, err := r.db.QueryContext(ctx, `SELECT measured_at, COALESCE(sirovo_cm, level_cm) FROM readings
		WHERE station_id = ? AND source = ? AND level_cm IS NOT NULL AND measured_at BETWEEN ? AND ?`,
		stationID, models.ReadingSourceImport, kad.Add(-blizu).UTC(), kad.Add(blizu).UTC())
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	najblize, vrijednost, ima := blizu+1, 0, false
	for rows.Next() {
		var t time.Time
		var cm int
		if err := rows.Scan(&t, &cm); err != nil {
			return 0, false, err
		}
		if d := t.Sub(kad).Abs(); d < najblize {
			najblize, vrijednost, ima = d, cm, true
		}
	}
	return vrijednost, ima, rows.Err()
}
