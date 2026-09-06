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
	Channel       string          `json:"channel,omitempty"` // kanal: prazan drže svi, inače vrsta/područje/godina
}

// Kanali: što drže svi, i što čvor prati po izboru. Ustroj, registri i
// djelatnici su mali i trebaju svakome, pa idu bez kanala. Očitanja i
// dnevnici rastu godinama i tiču se jednog područja, pa nose kanal
// "ocitanja/16/2026" ili "dnevnici/16/2026": laptop prati svoje područje i
// zadnje dvije godine, uredski čvor sve. Granica razmjene vodi se po
// autoru i kanalu, pa ono što čvor ne prati ne ostavlja rupu u razmjeni.
const (
	ChannelReadings = "ocitanja"
	ChannelJournals = "dnevnici"
)

// ChannelFor slaže kanal iz vrste, područja i godine; bez područja nema
// kanala, jer se takav zapis tiče svih (npr. uzvodna postaja na Dunavu)
func ChannelFor(kind string, areaID, year int) string {
	if areaID <= 0 || year <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%d/%d", kind, areaID, year)
}

// SplitChannel rastavlja kanal na vrstu, područje i godinu; prazan kanal je ("", 0, 0)
func SplitChannel(channel string) (kind string, areaID, year int) {
	if channel == "" {
		return "", 0, 0
	}
	parts := strings.Split(channel, "/")
	if len(parts) != 3 {
		return channel, 0, 0
	}
	fmt.Sscanf(parts[1], "%d", &areaID)
	fmt.Sscanf(parts[2], "%d", &year)
	return parts[0], areaID, year
}

// FrontierKey je ključ granice: autor za zajednički kanal, autor|kanal inače
func FrontierKey(nodeID, channel string) string {
	if channel == "" {
		return nodeID
	}
	return nodeID + "|" + channel
}

// SplitFrontierKey vraća autora i kanal iz ključa granice
func SplitFrontierKey(key string) (nodeID, channel string) {
	if i := strings.IndexByte(key, '|'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
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

// Record upisuje novu verziju zapisa u zajednički kanal i vraća njezin
// identifikator. Poziva se unutar transakcije u kojoj se mijenja i površina.
func (r *Recorder) Record(ctx context.Context, tx Execer, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, "", entity, entityID, payload, false)
}

// RecordIn upisuje novu verziju u zadani kanal
func (r *Recorder) RecordIn(ctx context.Context, tx Execer, channel, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, channel, entity, entityID, payload, false)
}

// Archive upisuje verziju koja zapis uklanja s površine. Sadržaj se čuva —
// arhivirani zapis se može vratiti kao i svaka starija verzija.
func (r *Recorder) Archive(ctx context.Context, tx Execer, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, "", entity, entityID, payload, true)
}

// ArchiveIn uklanja zapis s površine, u zadanom kanalu
func (r *Recorder) ArchiveIn(ctx context.Context, tx Execer, channel, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, channel, entity, entityID, payload, true)
}

func (r *Recorder) write(ctx context.Context, tx Execer, channel, entity, entityID string, payload any, archived bool) (string, error) {
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
			version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), entity, entityID, r.nodeID, supersedes, boolInt(archived), string(body), time.Now().UTC(), SchemaVersion, channel)
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

// LatestOf vraća zadnju verziju svakog zapisa navedenih entiteta, redom
// nastanka; s praznim popisom vraća zadnje verzije svih entiteta
func (r *Recorder) LatestOf(ctx context.Context, entities []string) ([]Version, error) {
	q := `SELECT ` + columns + ` FROM record_versions v
		WHERE version_id = (SELECT MAX(version_id) FROM record_versions w WHERE w.entity = v.entity AND w.entity_id = v.entity_id)`
	var args []any
	if len(entities) > 0 {
		q += ` AND entity IN (?` + strings.Repeat(",?", len(entities)-1) + `)`
		for _, e := range entities {
			args = append(args, e)
		}
	}
	return r.query(ctx, q+` ORDER BY version_id`, args...)
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
			version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			boolInt(v.Archived), string(v.Payload), v.CreatedAt.UTC(), v.SchemaVersion, v.Channel)
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

const columns = `version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel`

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
			&archived, &payload, &v.CreatedAt, &v.SchemaVersion, &v.Channel); err != nil {
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

// Frontier je dokle ovaj čvor zna za svakog autora i kanal: najveći
// version_id koji drži od svakog node_id u svakom kanalu (ključ je
// FrontierKey). Dva čvora razmijene frontiere i svaki pošalje drugome ono
// što drugi od pojedinog autora u tom kanalu još nema — i tuđe verzije koje
// je sam primio, pa promjena stiže i preko posrednika. Kanal koji čvor ne
// prati u granici nema, pa ga ni ne dobiva, a ne ostavlja ni rupu.
func (r *Recorder) Frontier(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT node_id, channel, MAX(version_id) FROM record_versions GROUP BY node_id, channel`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var node, channel, max string
		if err := rows.Scan(&node, &channel, &max); err != nil {
			return nil, err
		}
		out[FrontierKey(node, channel)] = max
	}
	return out, rows.Err()
}

// CountByChannel vraća broj verzija po kanalu — za nadzornu ploču i
// odluku što s ovog računala obrisati
func (r *Recorder) CountByChannel(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT channel, COUNT(*) FROM record_versions GROUP BY channel`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var ch string
		var n int
		if err := rows.Scan(&ch, &n); err != nil {
			return nil, err
		}
		out[ch] = n
	}
	return out, rows.Err()
}

// PurgeChannel briše sve verzije jednog kanala s ovog čvora. To nije
// arhiviranje nego čišćenje mjesta: zapisi ostaju na čvorovima koji kanal
// prate i vraćaju se razmjenom čim ga ovaj čvor opet zatraži.
func (r *Recorder) PurgeChannel(ctx context.Context, tx Execer, channel string) (int64, error) {
	if channel == "" {
		return 0, fmt.Errorf("zajednički kanal se ne briše")
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM record_versions WHERE channel = ?`, channel)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
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

// SinceByNode vraća verzije jednog autora u jednom kanalu novije od zadane, redom nastanka
func (r *Recorder) SinceByNode(ctx context.Context, nodeID, channel, afterVersionID string, limit int) ([]Version, error) {
	if limit <= 0 {
		limit = 1000
	}
	return r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE node_id = ? AND channel = ? AND version_id > ?
		ORDER BY version_id ASC LIMIT ?
	`, nodeID, channel, afterVersionID, limit)
}

// Delta je sve što drugi čvor još nema, prema njegovoj granici: za svakog
// autora i kanal koji ovaj čvor poznaje, verzije iznad onoga što drugi
// drži. wants kaže koje kanale drugi uopće želi; nil znači sve.
func (r *Recorder) Delta(ctx context.Context, theirs map[string]string, wants func(channel string) bool, limitPerNode int) ([]Version, error) {
	mine, err := r.Frontier(ctx)
	if err != nil {
		return nil, err
	}
	var out []Version
	for key, myMax := range mine {
		node, channel := SplitFrontierKey(key)
		if wants != nil && !wants(channel) {
			continue
		}
		theirMax := theirs[key]
		if theirMax >= myMax {
			continue
		}
		batch, err := r.SinceByNode(ctx, node, channel, theirMax, limitPerNode)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// ---------- održavanje knjige ----------

// Compact sažima knjigu: svaki zapis zadržava zadnju verziju, a arhivirani
// zapisi svoj nadgrobni spomenik; starije verzije istog zapisa, nastale
// prije zadanog trenutka, brišu se. Razmjena to ne osjeti: granica je
// najveći version_id po autoru i kanalu, a njega sažimanje ne dira. Čvor
// koji je dugo šutio može staru verziju poslati natrag; ona uđe u knjigu
// kao povijest, ne dira površinu, i nestane pri sljedećem sažimanju.
func (r *Recorder) Compact(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM record_versions
		WHERE created_at < ?
		  AND version_id <> (SELECT MAX(v.version_id) FROM record_versions v
		                     WHERE v.entity = record_versions.entity AND v.entity_id = record_versions.entity_id)
	`, olderThan.UTC())
	if err != nil {
		return 0, fmt.Errorf("sažimanje knjige: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Compactable broji verzije koje bi Compact obrisao, za pregled prije radnje
func (r *Recorder) Compactable(ctx context.Context, olderThan time.Time) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM record_versions
		WHERE created_at < ?
		  AND version_id <> (SELECT MAX(v.version_id) FROM record_versions v
		                     WHERE v.entity = record_versions.entity AND v.entity_id = record_versions.entity_id)
	`, olderThan.UTC()).Scan(&n)
	return n, err
}

// InChannels vraća sve verzije zadanih kanala, redom nastanka — za izvoz u datoteku
func (r *Recorder) InChannels(ctx context.Context, channels []string) ([]Version, error) {
	var out []Version
	for _, ch := range channels {
		batch, err := r.query(ctx, `SELECT `+columns+` FROM record_versions WHERE channel = ? ORDER BY version_id ASC`, ch)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// Stats su brojke knjige za stranicu održavanja
type Stats struct {
	Versions   int            `json:"versions"`
	Records    int            `json:"records"`    // različitih zapisa
	Tombstones int            `json:"tombstones"` // zapisa uklonjenih s površine
	Oldest     *time.Time     `json:"oldest,omitempty"`
	ByEntity   map[string]int `json:"by_entity"`
}

// Stats broji verzije, zapise i spomenike
func (r *Recorder) Stats(ctx context.Context) (Stats, error) {
	st := Stats{ByEntity: map[string]int{}}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT entity || '|' || entity_id) FROM record_versions`).Scan(&st.Versions, &st.Records); err != nil {
		return st, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_versions a WHERE archived = 1
		AND version_id = (SELECT MAX(version_id) FROM record_versions b WHERE b.entity = a.entity AND b.entity_id = a.entity_id)`).Scan(&st.Tombstones); err != nil {
		return st, err
	}
	// MIN() vraća tekst, ne DATETIME, pa se datum čita kao niz i tumači
	var oldest sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MIN(created_at) FROM record_versions`).Scan(&oldest); err == nil && oldest.Valid {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, oldest.String); err == nil {
				t = t.UTC()
				st.Oldest = &t
				break
			}
		}
	}
	counts, err := r.Count(ctx)
	if err != nil {
		return st, err
	}
	st.ByEntity = counts
	return st, nil
}
