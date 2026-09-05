// Paket ledger je knjiga verzija: svaki upis u sinkroniziranu tablicu
// ostavlja novu verziju zapisa, ništa se ne prepisuje ni ne briše.
//
// Zamisao je kao u gitu. Zapis je niz verzija složenih jedna iznad druge;
// "trenutno stanje" u običnoj tablici (stations, sections…) samo je površina —
// slika najnovije verzije. Starija verzija se vraća tako da se njezin sadržaj
// upiše kao NOVA verzija na vrh. Brisanje je arhiviranje: verzija s oznakom
// archived, zapis nestaje s površine, a ostaje u knjizi.
//
// Zato među čvorovima nema sukoba: dva čvora koja bez mreže mijenjaju isti
// zapis proizvedu dvije verzije. Nakon razmjene obje postoje, na površini je
// ona s većim version_id (UUIDv7 nosi vrijeme nastanka), druga je u povijesti.
// Razmjena je samo dodavanje verzija koje drugi čvor nema — idempotentna.
package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SchemaVersion je verzija sheme s kojom su verzije zapisane; putuje s njima
// da čvor s drugom verzijom binaryja zna što je dobio.
const SchemaVersion = 1

// Version je jedna verzija jednog zapisa
type Version struct {
	VersionID     string          `json:"version_id"` // UUIDv7 — nosi vrijeme nastanka
	Entity        string          `json:"entity"`     // naziv tablice: stations, sections…
	EntityID      string          `json:"entity_id"`  // identifikator zapisa
	NodeID        string          `json:"node_id"`    // čvor na kojem je verzija nastala
	Supersedes    string          `json:"supersedes"` // verzija koja je bila na površini kad je ova nastala
	Archived      bool            `json:"archived"`   // zapis je uklonjen s površine
	Payload       json.RawMessage `json:"payload"`    // cijeli zapis kako ga vidi aplikacija
	CreatedAt     time.Time       `json:"created_at"` // zidni sat čvora — informativno, ne za redoslijed
	SchemaVersion int             `json:"schema_version"`
}

// Execer je ono što Recorderu treba za upis: *sql.DB ili *sql.Tx.
// Repozitorij zapisuje verziju u ISTOJ transakciji u kojoj mijenja površinu,
// pa ne može postojati površina bez verzije ni obrnuto.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Recorder upisuje verzije u ime jednog čvora
type Recorder struct {
	db     *sql.DB
	nodeID string
}

func New(db *sql.DB, nodeID string) *Recorder {
	return &Recorder{db: db, nodeID: nodeID}
}

var ErrNoVersion = errors.New("zapis nema nijednu verziju")

// Record upisuje novu verziju zapisa i vraća njezin identifikator.
// Poziva se unutar transakcije u kojoj se mijenja i površina.
func (r *Recorder) Record(ctx context.Context, tx Execer, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, entity, entityID, payload, false)
}

// Archive upisuje verziju koja zapis uklanja s površine. Sadržaj se čuva —
// arhivirani zapis se može vratiti kao i svaka starija verzija.
func (r *Recorder) Archive(ctx context.Context, tx Execer, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, entity, entityID, payload, true)
}

func (r *Recorder) write(ctx context.Context, tx Execer, entity, entityID string, payload any, archived bool) (string, error) {
	if strings.TrimSpace(entity) == "" || strings.TrimSpace(entityID) == "" {
		return "", fmt.Errorf("verzija bez entiteta ili identifikatora")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("sadržaj verzije se ne može zapisati: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}

	// Verzija koja je bila na površini — bilo čijeg čvora — postaje prethodnica
	var supersedes string
	err = tx.QueryRowContext(ctx, `
		SELECT version_id FROM record_versions
		WHERE entity = ? AND entity_id = ?
		ORDER BY version_id DESC LIMIT 1
	`, entity, entityID).Scan(&supersedes)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("greška pri traženju prethodne verzije: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO record_versions (
			version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), entity, entityID, r.nodeID, supersedes, boolInt(archived), string(body), time.Now().UTC(), SchemaVersion)
	if err != nil {
		return "", fmt.Errorf("greška pri upisu verzije %s/%s: %w", entity, entityID, err)
	}

	return id.String(), nil
}

// Latest vraća verziju koja je na površini za zapis
func (r *Recorder) Latest(ctx context.Context, entity, entityID string) (*Version, error) {
	versions, err := r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE entity = ? AND entity_id = ?
		ORDER BY version_id DESC LIMIT 1
	`, entity, entityID)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, ErrNoVersion
	}
	return &versions[0], nil
}

// History vraća sve verzije zapisa, od najnovije prema najstarijoj
func (r *Recorder) History(ctx context.Context, entity, entityID string) ([]Version, error) {
	return r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE entity = ? AND entity_id = ?
		ORDER BY version_id DESC
	`, entity, entityID)
}

// Apply prima verzije s drugog čvora i upisuje one koje ovaj čvor nema.
// Idempotentno: isti paket primljen dvaput ne mijenja ništa. Površinu ne
// dira — nju osvježava sloj koji zna što je koji entitet.
func (r *Recorder) Apply(ctx context.Context, versions []Version) (applied int, err error) {
	if len(versions) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO record_versions (
			version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(version_id) DO NOTHING
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, v := range versions {
		if v.VersionID == "" || v.Entity == "" || v.EntityID == "" {
			return 0, fmt.Errorf("primljena verzija bez identifikatora ili entiteta")
		}
		res, err := stmt.ExecContext(ctx, v.VersionID, v.Entity, v.EntityID, v.NodeID, v.Supersedes,
			boolInt(v.Archived), string(v.Payload), v.CreatedAt.UTC(), v.SchemaVersion)
		if err != nil {
			return 0, fmt.Errorf("greška pri primanju verzije %s: %w", v.VersionID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			applied++
		}
	}

	return applied, tx.Commit()
}

// Count vraća broj verzija po entitetu — za stranicu Postavke
func (r *Recorder) Count(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT entity, COUNT(*) FROM record_versions GROUP BY entity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var entity string
		var n int
		if err := rows.Scan(&entity, &n); err != nil {
			return nil, err
		}
		out[entity] = n
	}
	return out, rows.Err()
}

const columns = `version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version`

func (r *Recorder) query(ctx context.Context, query string, args ...any) ([]Version, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri čitanju verzija: %w", err)
	}
	defer rows.Close()

	var out []Version
	for rows.Next() {
		var v Version
		var archived int
		var payload string
		if err := rows.Scan(&v.VersionID, &v.Entity, &v.EntityID, &v.NodeID, &v.Supersedes,
			&archived, &payload, &v.CreatedAt, &v.SchemaVersion); err != nil {
			return nil, err
		}
		v.Archived = archived != 0
		v.Payload = json.RawMessage(payload)
		out = append(out, v)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Frontier je dokle ovaj čvor zna za svakog autora: najveći version_id
// koji drži od svakog node_id. Dva čvora razmijene frontiere i svaki
// pošalje drugome ono što drugi od pojedinog autora još nema — i tuđe
// verzije koje je sam primio, pa promjena stiže i preko posrednika.
func (r *Recorder) Frontier(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT node_id, MAX(version_id) FROM record_versions GROUP BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var node, max string
		if err := rows.Scan(&node, &max); err != nil {
			return nil, err
		}
		out[node] = max
	}
	return out, rows.Err()
}

// Since vraća verzije novije od zadane, redom nastanka — to je ono što se
// šalje drugom čvoru. Prazan afterVersionID znači sve od početka.
func (r *Recorder) Since(ctx context.Context, afterVersionID string, limit int) ([]Version, error) {
	if limit <= 0 {
		limit = 1000
	}
	return r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE version_id > ?
		ORDER BY version_id ASC LIMIT ?
	`, afterVersionID, limit)
}

// SinceByNode vraća verzije jednog autora novije od zadane, redom nastanka
func (r *Recorder) SinceByNode(ctx context.Context, nodeID, afterVersionID string, limit int) ([]Version, error) {
	if limit <= 0 {
		limit = 1000
	}
	return r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE node_id = ? AND version_id > ?
		ORDER BY version_id ASC LIMIT ?
	`, nodeID, afterVersionID, limit)
}

// Delta je sve što drugi čvor još nema, prema njegovu frontieru:
// za svakog autora kojeg ovaj čvor poznaje, verzije iznad onoga što drugi drži.
func (r *Recorder) Delta(ctx context.Context, theirs map[string]string, limitPerNode int) ([]Version, error) {
	mine, err := r.Frontier(ctx)
	if err != nil {
		return nil, err
	}
	var out []Version
	for node, myMax := range mine {
		theirMax := theirs[node]
		if theirMax >= myMax {
			continue
		}
		batch, err := r.SinceByNode(ctx, node, theirMax, limitPerNode)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}
