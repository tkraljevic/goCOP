package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// dionicaObrisana javlja je li verzija epizode obrane ostala bez dionice jer
// je dionica obrisana. Brisanje dionice odnese njezine epizode s površine
// (ON DELETE CASCADE), pa epizoda ne ide na površinu kad je dionica obrisana
// nakon te verzije epizode, i kad je poslije ponovno upisana pod istom
// šifrom. Ne ide ni kad dionice nema na površini, a zadnja verzija u knjizi
// ju arhivira: epizodu je mijenjao čvor koji za brisanje još nije znao. Kad
// se dionica poslije vrati pod istom šifrom, takva se epizoda vraća s njom
// (vratiDjecu, apply_roditelji.go). Dionica koja još nije stigla nije
// obrisana: epizoda pada na stranom ključu i čeka je među nedovršenima.
func dionicaObrisana(ctx context.Context, tx *sql.Tx, code, versionID string) (bool, error) {
	var obrisana bool
	err := tx.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM record_versions
			WHERE entity = ? AND entity_id = ? AND archived = 1 AND version_id > ?)
		OR (NOT EXISTS (SELECT 1 FROM sections WHERE code = ?)
			AND COALESCE((SELECT archived FROM record_versions WHERE entity = ? AND entity_id = ?
				ORDER BY version_id DESC LIMIT 1), 0) = 1)`,
		EntitySections, code, versionID, code, EntitySections, code).Scan(&obrisana)
	return obrisana, err
}

// primijeniEpizodu stavlja primljenu epizodu obrane na površinu. Epizoda
// obrisane dionice ne ide na površinu i ne čeka; ako je ostala na površini
// jer je brisanje stiglo poslije ponovnog upisa dionice, odlazi s nje.
func primijeniEpizodu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var e models.DefenseEpisode
	if err := json.Unmarshal(v.Payload, &e); err != nil {
		return err
	}
	obrisana, err := dionicaObrisana(ctx, tx, e.SectionCode, v.VersionID)
	if err != nil {
		return err
	}
	if obrisana {
		_, err := tx.ExecContext(ctx, `DELETE FROM defense_episodes WHERE id = ?`, e.ID.String())
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO defense_episodes (id, section_code, station_id, started_at, ended_at,
			phase, peak_cm, peak_at, declared_by, basis, threshold_at, ended_by,
			origin, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			section_code = excluded.section_code, station_id = excluded.station_id,
			started_at = excluded.started_at, ended_at = excluded.ended_at,
			phase = excluded.phase, peak_cm = excluded.peak_cm, peak_at = excluded.peak_at,
			declared_by = excluded.declared_by, basis = excluded.basis,
			threshold_at = excluded.threshold_at, ended_by = excluded.ended_by,
			origin = excluded.origin, note = excluded.note, updated_at = excluded.updated_at`,
		e.ID.String(), e.SectionCode, e.StationID, e.StartedAt.UTC(), nullTime(e.EndedAt),
		string(e.Phase), e.PeakCm, nullTime(e.PeakAt), e.DeclaredBy, e.Basis,
		nullTime(e.ThresholdAt), e.EndedBy,
		e.Origin, e.Note, e.CreatedAt.UTC(), e.UpdatedAt.UTC())
	return err
}
