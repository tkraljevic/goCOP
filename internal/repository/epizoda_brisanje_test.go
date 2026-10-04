package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Brisanje epizode (npr. razdoblja iz poništenog akta) bilježi se u knjigu
// kao arhivirana verzija, pa nestaje i na ostalim čvorovima
func TestBrisanjeEpizode(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "epizode.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO', 'COP');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Područje', 'VGI', '');
		INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'a', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	repo := NewEpisodeRepository(baza, ledger.New(baza, "test"))
	ctx := context.Background()
	e := &models.DefenseEpisode{ID: uuid.New(), SectionCode: "P.1.1", StartedAt: time.Now().Add(-time.Hour), Phase: models.PhasePrep, Origin: models.EpisodeFromOperator}
	if err := repo.SaveEpisode(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteEpisode(ctx, e); err != nil {
		t.Fatal(err)
	}
	if sve, err := repo.ListEpisodes(ctx, "P.1.1"); err != nil || len(sve) != 0 {
		t.Errorf("nakon brisanja: %+v %v", sve, err)
	}
	var arhivirano int
	if err := baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ? AND archived = 1`, EntityEpisodes, e.ID.String()).Scan(&arhivirano); err != nil || arhivirano != 1 {
		t.Errorf("arhivirana verzija u knjizi: %d %v", arhivirano, err)
	}
	if _, err := baza.Exec(`CREATE TRIGGER kvar BEFORE DELETE ON defense_episodes BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveEpisode(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteEpisode(ctx, e); err == nil {
		t.Error("brisanje kroz kvar prošlo")
	}
	// brisanje prođe, ali se ne da zabilježiti u knjigu: ništa se ne briše
	if _, err := baza.Exec(`DROP TRIGGER kvar; CREATE TRIGGER kvar_knjige BEFORE INSERT ON record_versions BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteEpisode(ctx, e); err == nil {
		t.Error("brisanje bez zapisa u knjigu prošlo")
	}
	if sve, _ := repo.ListEpisodes(ctx, "P.1.1"); len(sve) != 1 {
		t.Errorf("epizoda obrisana bez zapisa u knjigu: %d", len(sve))
	}
}
