package peers

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gocop/internal/ledger"

	_ "modernc.org/sqlite"
)

// Arhiva na disku: kanali se izvoze u zasebnu SQLite datoteku i uvoze iz
// nje. Datoteka nosi samo knjigu verzija tih kanala (ista shema kao u
// programu), pa je uvoz isto što i razmjena: verzije koje čvor nema uđu u
// knjigu, a površina se osvježi iz najnovijih. Tako se godina jednog
// područja odloži na disk ili prenese štapićem bez mreže.

// ExportFile piše verzije zadanih kanala u novu SQLite datoteku
func (s *Service) ExportFile(ctx context.Context, path string, channels []string) (int, error) {
	versions, err := s.rec.InChannels(ctx, channels)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, fmt.Errorf("nema nijedne verzije u zadanim kanalima")
	}
	_ = os.Remove(path)
	out, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	if _, err := out.ExecContext(ctx, ledger.Schema); err != nil {
		return 0, fmt.Errorf("shema arhive: %w", err)
	}
	if _, err := out.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS archive_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return 0, err
	}
	for k, v := range map[string]string{
		"program": "goCOP", "node_id": s.node.ID, "exported_at": time.Now().UTC().Format(time.RFC3339),
		"channels": strings.Join(channels, ","), "schema_version": fmt.Sprint(ledger.SchemaVersion),
	} {
		if _, err := out.ExecContext(ctx, `INSERT INTO archive_meta (key, value) VALUES (?, ?)`, k, v); err != nil {
			return 0, err
		}
	}
	tx, err := out.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO record_versions
		(version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	for _, v := range versions {
		if _, err := stmt.ExecContext(ctx, v.VersionID, v.Entity, v.EntityID, v.NodeID, v.Supersedes,
			boolInt(v.Archived), string(v.Payload), v.CreatedAt.UTC(), v.SchemaVersion, v.Channel); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(versions), nil
}

// ImportReport kaže što je uvoz donio
type ImportReport struct {
	Versions int            `json:"versions"` // u datoteci
	Applied  int            `json:"applied"`  // novih u knjizi
	Channels map[string]int `json:"channels"` // po kanalu, u datoteci
	From     string         `json:"from"`     // čvor koji je izvezao
}

// ImportFile čita arhivu i primjenjuje verzije kao da su stigle razmjenom
func (s *Service) ImportFile(ctx context.Context, path string) (ImportReport, error) {
	rep := ImportReport{Channels: map[string]int{}}
	in, err := sql.Open("sqlite", path)
	if err != nil {
		return rep, err
	}
	defer in.Close()
	var program string
	if err := in.QueryRowContext(ctx, `SELECT value FROM archive_meta WHERE key = 'program'`).Scan(&program); err != nil || program != "goCOP" {
		return rep, fmt.Errorf("datoteka nije goCOP arhiva")
	}
	_ = in.QueryRowContext(ctx, `SELECT value FROM archive_meta WHERE key = 'node_id'`).Scan(&rep.From)

	rows, err := in.QueryContext(ctx, `SELECT version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel
		FROM record_versions ORDER BY version_id`)
	if err != nil {
		return rep, fmt.Errorf("čitanje arhive: %w", err)
	}
	var versions []ledger.Version
	for rows.Next() {
		var v ledger.Version
		var archived int
		var payload string
		if err := rows.Scan(&v.VersionID, &v.Entity, &v.EntityID, &v.NodeID, &v.Supersedes, &archived, &payload, &v.CreatedAt, &v.SchemaVersion, &v.Channel); err != nil {
			rows.Close()
			return rep, err
		}
		v.Archived = archived != 0
		v.Payload = []byte(payload)
		versions = append(versions, v)
		rep.Channels[v.Channel]++
	}
	rows.Close()
	rep.Versions = len(versions)
	if len(versions) == 0 {
		return rep, fmt.Errorf("arhiva je prazna")
	}
	// isto kao razmjena: u knjigu, pa na površinu
	for start := 0; start < len(versions); start += 2000 {
		end := start + 2000
		if end > len(versions) {
			end = len(versions)
		}
		batch := versions[start:end]
		applied, err := s.rec.Apply(ctx, batch)
		if err != nil {
			return rep, err
		}
		rep.Applied += applied
		if applied > 0 && s.onApplied != nil {
			if err := s.onApplied(ctx, batch); err != nil {
				return rep, fmt.Errorf("površina nije osvježena: %w", err)
			}
		}
	}
	return rep, nil
}

// ArchiveFileName je naziv datoteke za izvoz: gocop-ocitanja-bp16-2024.db
func ArchiveFileName(kind string, areaID, year int) string {
	if kind == "" {
		kind = "sve"
	}
	return fmt.Sprintf("gocop-%s-bp%d-%d.db", kind, areaID, year)
}

// ChannelsFor vraća kanale ovog čvora koje pokriva vrsta (prazno = obje), područje i godina
func (s *Service) ChannelsFor(ctx context.Context, kind string, areaID, year int) ([]string, error) {
	list, err := s.Channels(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range list {
		if (kind == "" || c.Kind == kind) && (areaID == 0 || c.AreaID == areaID) && (year == 0 || c.Year == year) {
			out = append(out, c.Channel)
		}
	}
	return out, nil
}

// TempArchivePath daje privremenu putanju uz bazu za izvoz ili uvoz
func (s *Service) TempArchivePath(name string) string {
	return filepath.Join(os.TempDir(), "gocop-"+fmt.Sprint(time.Now().UnixNano())+"-"+filepath.Base(name))
}
