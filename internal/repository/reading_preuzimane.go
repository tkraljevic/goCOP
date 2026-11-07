package repository

import (
	"context"
	"database/sql"
	"time"

	"gocop/internal/models"
)

// MjerenjaVelicine sažima mjerenja jedne veličine letve u nekom razdoblju.
type MjerenjaVelicine struct {
	Broj         int
	Prvo, Zadnje time.Time
}

// citajMjerenja puni sažetak iz stupaca agregata; Broj 0 znači bez mjerenja.
func (m *MjerenjaVelicine) citajMjerenja(prvo, zadnje sql.NullString) {
	m.Prvo, _ = parseDBTime(prvo.String)
	m.Zadnje, _ = parseDBTime(zadnje.String)
}

// MjerenjaLetve je što je letva s uključenim preuzimanjem izmjerila u razdoblju.
type MjerenjaLetve struct {
	Naziv            string
	Vodostaj, Protok MjerenjaVelicine
}

// MjerenjaPreuzimanih sažima vodostaje i protoke letvi s uključenim
// preuzimanjem (uvjetPreuzimanja) od trenutka od do trenutka do. Ručni upisi se
// ne broje: pitanje je što stiže s izvora, a vodočuvar može upisati i kad je
// izvor stao. Letve bez ijednog preuzetog očitanja u razdoblju nema u rezultatu.
func (r *ReadingRepository) MjerenjaPreuzimanih(ctx context.Context, od, do time.Time) ([]MjerenjaLetve, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT s.name,
		COUNT(r.level_cm),
		MIN(CASE WHEN r.level_cm IS NOT NULL THEN r.measured_at END),
		MAX(CASE WHEN r.level_cm IS NOT NULL THEN r.measured_at END),
		COUNT(r.flow_m3s),
		MIN(CASE WHEN r.flow_m3s IS NOT NULL THEN r.measured_at END),
		MAX(CASE WHEN r.flow_m3s IS NOT NULL THEN r.measured_at END)
		FROM stations s JOIN readings r ON r.station_id = s.id
		WHERE (`+uvjetPreuzimanja+`) AND r.measured_at >= ? AND r.measured_at <= ? AND r.source <> ?
		GROUP BY s.id ORDER BY s.name`, od.UTC(), do.UTC(), models.ReadingSourceManual)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MjerenjaLetve
	for rows.Next() {
		var l MjerenjaLetve
		var vPrvo, vZadnje, qPrvo, qZadnje sql.NullString
		if err := rows.Scan(&l.Naziv, &l.Vodostaj.Broj, &vPrvo, &vZadnje, &l.Protok.Broj, &qPrvo, &qZadnje); err != nil {
			return nil, err
		}
		l.Vodostaj.citajMjerenja(vPrvo, vZadnje)
		l.Protok.citajMjerenja(qPrvo, qZadnje)
		out = append(out, l)
	}
	return out, rows.Err()
}
