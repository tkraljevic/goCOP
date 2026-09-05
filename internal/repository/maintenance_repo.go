package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Nazivi entiteta u knjizi verzija
const (
	EntityMaintainedWaters = "maintained_waters"
	EntityWorkItems        = "work_items"
)

// MaintenanceRepository čuva popis održavanih voda i stavke radova po
// branjenom području. Sve što piše bilježi verziju, pa se dijeli među čvorovima.
type MaintenanceRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewMaintenanceRepository(db *sql.DB, rec *ledger.Recorder) *MaintenanceRepository {
	return &MaintenanceRepository{db: db, rec: rec}
}

const maintainedWaterColumns = `
	m.id, m.area_id, m.program, m.watercourse_code, m.structure_id, m.name, m.seq, m.water_order, m.water_group,
	m.kind, m.source, m.created_at, m.updated_at,
	COALESCE(w.official_name, s.name, ''), COALESCE(s.kind, '')`

const maintainedWaterFrom = `
	FROM maintained_waters m
	LEFT JOIN watercourses w ON w.code = m.watercourse_code AND m.watercourse_code <> ''
	LEFT JOIN structures s ON s.id = m.structure_id AND m.structure_id <> ''`

func scanMaintainedWater(row rowScanner) (models.MaintainedWater, error) {
	var m models.MaintainedWater
	err := row.Scan(&m.ID, &m.AreaID, &m.Program, &m.WatercourseCode, &m.StructureID, &m.Name, &m.Seq, &m.Order, &m.Group,
		&m.Kind, &m.Source, &m.CreatedAt, &m.UpdatedAt, &m.WaterName, &m.StructureKind)
	return m, err
}

// ListWaters vraća popis lokacija područja redom kojim ih plan navodi:
// I. red međudržavne, I. red ostale, II. red; unutar toga po vrsti i rednom broju.
func (r *MaintenanceRepository) ListWaters(ctx context.Context, areaID int) ([]models.MaintainedWater, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+maintainedWaterColumns+maintainedWaterFrom+`
		WHERE m.area_id = ?`, areaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MaintainedWater
	for rows.Next() {
		m, err := scanMaintainedWater(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ka, kb := planRank(a), planRank(b); ka != kb {
			return ka < kb
		}
		if sa, sb := seqParts(a.Seq), seqParts(b.Seq); sa != sb {
			return sa < sb
		}
		return a.Name < b.Name
	})
	return out, nil
}

// planRank slaže lokacije redom kojim ih plan navodi: I. red međudržavne,
// I. red ostale, II. red; unutar toga vodotoci, akumulacije, bujice, melioracijske
func planRank(m models.MaintainedWater) string {
	order := map[string]string{models.WaterOrderFirst: "1", models.WaterOrderSecond: "2", models.WaterOrderThird: "3", models.WaterOrderFourth: "4"}[m.Order]
	group := map[string]string{models.WaterGroupInterstate: "1", models.WaterGroupOtherState: "2"}[m.Group]
	kind := "9"
	for i, k := range models.MaintenanceKinds {
		if k == m.Kind {
			kind = fmt.Sprint(i + 1)
		}
	}
	if order == "" {
		order = "9"
	}
	return m.ProgramOf() + order + group + kind
}

// seqParts pretvara "4.12." u ključ koji se slaže brojčano, ne slovno
func seqParts(seq string) string {
	var out string
	for _, p := range strings.Split(strings.Trim(seq, ". "), ".") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return seq
		}
		out += fmt.Sprintf("%04d.", n)
	}
	return out
}

// WatersFor vraća sve popise u kojima se voda ili objekt vodi — obično jedan,
// ali voda na granici područja može biti u dva ugovora.
func (r *MaintenanceRepository) WatersFor(ctx context.Context, watercourseCode, structureID string) ([]models.MaintainedWater, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+maintainedWaterColumns+maintainedWaterFrom+`
		WHERE (m.watercourse_code = ? AND ? <> '') OR (m.structure_id = ? AND ? <> '')
		ORDER BY m.area_id`, watercourseCode, watercourseCode, structureID, structureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MaintainedWater
	for rows.Next() {
		m, err := scanMaintainedWater(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetWater vraća jednu lokaciju
func (r *MaintenanceRepository) GetWater(ctx context.Context, id string) (*models.MaintainedWater, error) {
	m, err := scanMaintainedWater(r.db.QueryRowContext(ctx,
		`SELECT `+maintainedWaterColumns+maintainedWaterFrom+` WHERE m.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// UpsertWater upisuje ili obnavlja lokaciju. Identitet je stabilan (područje +
// naziv), pa ponovni uvoz istog ugovora ne stvara dvostruke zapise.
func (r *MaintenanceRepository) UpsertWater(ctx context.Context, m *models.MaintainedWater) error {
	if m.ID == "" {
		m.ID = MaintainedWaterID(m.AreaID, m.Name)
	}
	now := time.Now().UTC()
	m.UpdatedAt = now
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO maintained_waters (id, area_id, program, watercourse_code, structure_id, name, seq, water_order, water_group,
			kind, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			area_id = excluded.area_id, program = excluded.program, watercourse_code = excluded.watercourse_code, structure_id = excluded.structure_id,
			name = excluded.name, seq = excluded.seq, water_order = excluded.water_order, water_group = excluded.water_group,
			kind = excluded.kind, source = excluded.source, updated_at = excluded.updated_at`,
		m.ID, m.AreaID, m.ProgramOf(), m.WatercourseCode, m.StructureID, m.Name, m.Seq, m.Order, m.Group,
		m.Kind, m.Source, m.CreatedAt, m.UpdatedAt); err != nil {
		return fmt.Errorf("greška pri upisu lokacije %q: %w", m.Name, err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityMaintainedWaters, m.ID, m); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteWater uklanja lokaciju s površine; u knjizi ostaje arhivirana
func (r *MaintenanceRepository) DeleteWater(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanMaintainedWater(tx.QueryRowContext(ctx,
		`SELECT `+maintainedWaterColumns+maintainedWaterFrom+` WHERE m.id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("lokacija nije pronađena")
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM maintained_waters WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityMaintainedWaters, id, current); err != nil {
		return err
	}
	return tx.Commit()
}

// MaintainedWaterID je stabilan identitet lokacije: isto područje i isti
// naziv iz popisa daju isti zapis na svakom čvoru.
func MaintainedWaterID(areaID int, name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("gocop:maintained-water:%d:%s", areaID, name))).String()
}

// --- stavke radova ---

const workItemColumns = `id, area_id, number, description, unit, active, sort_order, origin, source, created_at, updated_at`

func scanWorkItem(row rowScanner) (models.WorkItem, error) {
	var w models.WorkItem
	var active int
	err := row.Scan(&w.ID, &w.AreaID, &w.Number, &w.Description, &w.Unit, &active, &w.SortOrder, &w.Origin, &w.Source,
		&w.CreatedAt, &w.UpdatedAt)
	w.Active = active != 0
	return w, err
}

// ListItems vraća stavke područja; s all i one koje su isključene
func (r *MaintenanceRepository) ListItems(ctx context.Context, areaID int, all bool) ([]models.WorkItem, error) {
	q := `SELECT ` + workItemColumns + ` FROM work_items WHERE area_id = ?`
	if !all {
		q += ` AND active = 1`
	}
	q += ` ORDER BY sort_order, CAST(number AS INTEGER), number, description`
	rows, err := r.db.QueryContext(ctx, q, areaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.WorkItem
	for rows.Next() {
		w, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetItem vraća jednu stavku
func (r *MaintenanceRepository) GetItem(ctx context.Context, id string) (*models.WorkItem, error) {
	w, err := scanWorkItem(r.db.QueryRowContext(ctx, `SELECT `+workItemColumns+` FROM work_items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// SaveItem upisuje novu stavku ili mijenja postojeću (po identitetu)
func (r *MaintenanceRepository) SaveItem(ctx context.Context, w *models.WorkItem) error {
	now := time.Now().UTC()
	if w.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		w.ID = id.String()
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	w.UpdatedAt = now

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO work_items (`+workItemColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			area_id = excluded.area_id, number = excluded.number, description = excluded.description, unit = excluded.unit,
			active = excluded.active, sort_order = excluded.sort_order, origin = excluded.origin, source = excluded.source,
			updated_at = excluded.updated_at`,
		w.ID, w.AreaID, w.Number, w.Description, w.Unit, boolInt(w.Active), w.SortOrder, w.Origin, w.Source,
		w.CreatedAt, w.UpdatedAt); err != nil {
		return fmt.Errorf("greška pri upisu stavke: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityWorkItems, w.ID, w); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteItem briše stavku s površine; u knjizi ostaje arhivirana
func (r *MaintenanceRepository) DeleteItem(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanWorkItem(tx.QueryRowContext(ctx, `SELECT `+workItemColumns+` FROM work_items WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("stavka nije pronađena")
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM work_items WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityWorkItems, id, current); err != nil {
		return err
	}
	return tx.Commit()
}

// WorkItemID je stabilan identitet stavke uvezene iz ugovora: isto područje
// i ista oznaka s jedinicom daju isti zapis, pa ugovori sljedećih godina samo
// obnavljaju opis.
func WorkItemID(areaID int, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("gocop:work-item:%d:%s", areaID, key))).String()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
