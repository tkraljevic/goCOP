package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

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

// uvjetPreuzimanja izdvaja postaje (alias s) s uključenim preuzimanjem.
const uvjetPreuzimanja = `(s.javni_uvoz = 1 AND s.javni_url <> '')
		   OR (s.telemetrija_uvoz = 1 AND s.telemetrija_site <> '')`

// LetveZaPreuzimanje su postaje s uključenim preuzimanjem: one koje imaju
// javnu stranicu i one prebačene na telemetriju.
func (s *JavniSpremiste) LetveZaPreuzimanje(ctx context.Context) ([]models.Station, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stationColumns+` FROM stations s
		WHERE `+uvjetPreuzimanja+`
		ORDER BY s.name`)
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

// Razine vraća vodostaje (cm) koje letva već ima u razdoblju i zadnji prije
// njega, kako god davno bio, po trenutku (UTC, unix), iz bilo kojeg izvora:
// od njih preuzimanje mjeri skok nove vrijednosti (paket skok). Zadnji
// prije razdoblja kaže da je ispred novih rupa, a ne nova letva: prvo
// očitanje nakon ispada mjeri se prema idućima (skok.PoRupi).
func (s *JavniSpremiste) Razine(ctx context.Context, stationID string, od, do time.Time) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT measured_at, level_cm FROM readings
		WHERE station_id = ? AND measured_at BETWEEN ? AND ? AND level_cm IS NOT NULL
		UNION ALL SELECT * FROM (SELECT measured_at, level_cm FROM readings
		WHERE station_id = ? AND measured_at < ? AND level_cm IS NOT NULL ORDER BY measured_at DESC LIMIT 1)`,
		stationID, od.UTC(), do.UTC(), stationID, od.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var t time.Time
		var cm int
		if err := rows.Scan(&t, &cm); err != nil {
			return nil, err
		}
		out[t.UTC().Unix()] = cm
	}
	return out, rows.Err()
}

// Vodostaji vraća vodostaje koje letva ima u razdoblju, redom mjerenja, i
// zadnji preuzeti prije njega: po njemu se zna koliki je ispravak vrijedio
// na početku razdoblja (javnivodostaji, ispravak telemetrije).
func (s *JavniSpremiste) Vodostaji(ctx context.Context, stationID string, od, do time.Time) ([]models.UpisanoOcitanje, error) {
	const stupci = `id, measured_at, level_cm, sirovo_cm, source, origin`
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM (SELECT `+stupci+` FROM readings
		WHERE station_id = ? AND measured_at < ? AND level_cm IS NOT NULL AND source = ?
		ORDER BY measured_at DESC LIMIT 1)
		UNION ALL SELECT `+stupci+` FROM readings
		WHERE station_id = ? AND measured_at BETWEEN ? AND ? AND level_cm IS NOT NULL
		ORDER BY measured_at`,
		stationID, od.UTC(), models.ReadingSourceImport, stationID, od.UTC(), do.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.UpisanoOcitanje
	for rows.Next() {
		var u models.UpisanoOcitanje
		var id, izvor string
		var sirovo sql.NullInt64
		if err := rows.Scan(&id, &u.Kad, &u.Cm, &sirovo, &izvor, &u.Podrijetlo); err != nil {
			return nil, err
		}
		u.ID, _ = uuid.Parse(id) // oznaka koju je upisao program uvijek je valjana
		u.Kad, u.SirovoCm, u.Rucno = u.Kad.UTC(), nullInt(sirovo), izvor == models.ReadingSourceManual
		out = append(out, u)
	}
	return out, rows.Err()
}

// Ispravi mijenja vodostaj već upisanih preuzetih očitanja, s verzijom u knjizi
func (s *JavniSpremiste) Ispravi(ctx context.Context, ispravci []models.IspravljenoOcitanje) error {
	return s.readings.IspraviTelemetriju(ctx, ispravci)
}

// ZabiljeziOdbacene bilježi vrijednosti koje preuzimanje nije upisalo jer
// su lažni skok, s verzijom u knjizi, za stranicu letve
func (s *JavniSpremiste) ZabiljeziOdbacene(ctx context.Context, odbacene []models.OdbacenaVrijednost) error {
	return s.readings.ZabiljeziOdbacene(ctx, odbacene)
}

// Upisi upisuje nova očitanja s verzijom u knjizi
func (s *JavniSpremiste) Upisi(ctx context.Context, ocitanja []models.Reading) (int, error) {
	return s.readings.ImportBatch(ctx, ocitanja)
}
