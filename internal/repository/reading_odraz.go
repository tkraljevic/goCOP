package repository

import (
	"context"
	"database/sql"
	"strconv"

	"gocop/internal/db"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// Očitanje objekta piše vodostaj i na povezane letve (Odluke (15. 11. 2026.)).
// Na objektu ostaje cijelo očitanje (vodostaj, stanje, zapornica, agregati),
// a istim upisom na letvu objekta ide njegov odraz: vodostaj (LevelCm) na
// letvu objekta, kod crpne stanice unutarnju a kod ustave uzvodnu, i drugi
// vodostaj (Level2Cm) na drugu letvu, vanjsku ili nizvodnu. Odraz je obično
// ručno očitanje letve, pa ide u graf, provjeru kvara i prognozu i putuje na
// druga računala kao svoja verzija. Ispravlja se i briše samo s objektom.

// idOdraza je stalni identifikator odraza: isti za isto očitanje objekta i
// istu stranu, pa se odraz pri izmjeni prepiše, a ne udvostruči.
func idOdraza(ocitanjeObjekta string, strana int) uuid.UUID {
	return db.StableID("reading", "objekt|"+ocitanjeObjekta+"|"+strconv.Itoa(strana))
}

// odrazi upisuje, mijenja ili briše odraze očitanja objekta na njegovim
// letvama, u transakciji upisa samog očitanja
func (r *ReadingRepository) odrazi(ctx context.Context, tx *sql.Tx, rd *models.Reading) error {
	if rd.StructureID == "" {
		return nil
	}
	var gore, dolje string
	err := tx.QueryRowContext(ctx, `SELECT station_id, station_down_id FROM structures WHERE id = ?`, rd.StructureID).Scan(&gore, &dolje)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	for i, s := range []struct {
		letva string
		cm    *int
	}{{gore, rd.LevelCm}, {dolje, rd.Level2Cm}} {
		if err := r.odraziStranu(ctx, tx, rd, idOdraza(rd.ID.String(), i+1), s.letva, s.cm); err != nil {
			return err
		}
	}
	return nil
}

// odraziStranu drži jedan odraz u skladu s očitanjem objekta: bez letve ili
// bez vrijednosti odraza nema, a odraz na letvi koja više nije letva objekta
// briše se i upisuje na novu.
func (r *ReadingRepository) odraziStranu(ctx context.Context, tx *sql.Tx, rd *models.Reading, id uuid.UUID, letva string, cm *int) error {
	if err := r.makniStariOdraz(ctx, tx, id, letva, cm); err != nil {
		return err
	}
	if letva == "" || cm == nil {
		return nil
	}
	o := models.Reading{ID: id, StationID: letva, MeasuredAt: rd.MeasuredAt, LevelCm: cm,
		Source: rd.Source, Origin: rd.Origin, SourceRef: models.IzvorOdrazaObjekta + rd.ID.String(),
		Observer: rd.Observer, UserID: rd.UserID, CreatedAt: rd.CreatedAt, UpdatedAt: rd.UpdatedAt}
	if _, err := tx.ExecContext(ctx, `INSERT INTO readings (`+readingColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET measured_at = excluded.measured_at, level_cm = excluded.level_cm,
			source = excluded.source, observer = excluded.observer, updated_at = excluded.updated_at`, readingArgs(&o)...); err != nil {
		return err
	}
	_, err := r.zabiljeziUTx(ctx, tx, id.String())
	return err
}

// makniStariOdraz briše postojeći odraz kad ga više ne treba ili kad je na
// drugoj letvi
func (r *ReadingRepository) makniStariOdraz(ctx context.Context, tx *sql.Tx, id uuid.UUID, letva string, cm *int) error {
	postoji, err := getReadingTx(ctx, tx, id.String())
	if err != nil || postoji == nil {
		return err
	}
	if letva != "" && cm != nil && postoji.StationID == letva {
		return nil
	}
	return r.obrisiUTx(ctx, tx, postoji)
}

// obrisiOdraze briše odraze očitanja objekta koje se briše
func (r *ReadingRepository) obrisiOdraze(ctx context.Context, tx *sql.Tx, rd *models.Reading) error {
	if rd.StructureID == "" {
		return nil
	}
	for strana := 1; strana <= 2; strana++ {
		o, err := getReadingTx(ctx, tx, idOdraza(rd.ID.String(), strana).String())
		if err != nil {
			return err
		}
		if o == nil {
			continue
		}
		if err := r.obrisiUTx(ctx, tx, o); err != nil {
			return err
		}
	}
	return nil
}

// zabiljeziUTx računa kanal upisanog očitanja i bilježi njegovu verziju u
// knjigu, u istoj transakciji; vraća upisano očitanje
func (r *ReadingRepository) zabiljeziUTx(ctx context.Context, tx *sql.Tx, id string) (*models.Reading, error) {
	saved, err := getReadingTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	channel := readingChannel(ctx, tx, saved)
	if _, err := tx.ExecContext(ctx, `UPDATE readings SET channel = ? WHERE id = ?`, channel, id); err != nil {
		return nil, err
	}
	_, err = r.rec.RecordIn(ctx, tx, channel, EntityReadings, id, saved)
	return saved, err
}

// obrisiUTx briše očitanje i bilježi spomenik u njegovu kanalu
func (r *ReadingRepository) obrisiUTx(ctx context.Context, tx *sql.Tx, rd *models.Reading) error {
	channel := readingChannel(ctx, tx, rd)
	if _, err := tx.ExecContext(ctx, `DELETE FROM readings WHERE id = ?`, rd.ID.String()); err != nil {
		return err
	}
	_, err := r.rec.ArchiveIn(ctx, tx, channel, EntityReadings, rd.ID.String(), rd)
	return err
}
