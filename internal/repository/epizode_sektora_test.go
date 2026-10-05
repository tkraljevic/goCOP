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

// Otvorene obrane sektora nose identitet epizode (kao OpenEpisode), a
// zatvorene i one s dionica drugog sektora ne ulaze
func TestOtvoreneObraneSektoraSIdentitetom(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "epizode.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO', 'COP'), ('R', 'Sektor R', 'VGO', 'COP');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Područje', 'VGI', ''), (2, 'R', 'Područje', 'VGI', '');
		INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES
			('P.1.1', 1, 'P', 'a', '2026-01-01', '2026-01-01'), ('P.1.2', 1, 'P', 'b', '2026-01-01', '2026-01-01'),
			('R.2.1', 2, 'R', 'c', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	repo := NewEpisodeRepository(baza, ledger.New(baza, "test"))
	ctx := context.Background()
	sad := time.Now().UTC().Truncate(time.Second)
	otvorena := &models.DefenseEpisode{ID: uuid.New(), SectionCode: "P.1.1", StartedAt: sad.Add(-time.Hour), Phase: models.PhaseRegular, Origin: models.EpisodeFromOperator}
	kraj := sad.Add(-time.Minute)
	for _, e := range []*models.DefenseEpisode{
		otvorena,
		{ID: uuid.New(), SectionCode: "P.1.2", StartedAt: sad.Add(-2 * time.Hour), EndedAt: &kraj, Phase: models.PhasePrep, Origin: models.EpisodeFromOperator},
		{ID: uuid.New(), SectionCode: "R.2.1", StartedAt: sad.Add(-time.Hour), Phase: models.PhasePrep, Origin: models.EpisodeFromOperator},
	} {
		if err := repo.SaveEpisode(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	sve, err := repo.OpenEpisodesInSector(ctx, "P")
	if err != nil || len(sve) != 1 {
		t.Fatalf("otvorene obrane sektora P: %+v (%v)", sve, err)
	}
	if sve[0].ID != otvorena.ID || sve[0].SectionCode != "P.1.1" || sve[0].Phase != models.PhaseRegular || !sve[0].StartedAt.Equal(otvorena.StartedAt) {
		t.Errorf("otvorena obrana sektora: %+v", sve[0])
	}

	// neispravan identitet u bazi je greška, ne epizoda bez identiteta
	if _, err := baza.Exec(`UPDATE defense_episodes SET id = 'nije-uuid' WHERE id = ?`, otvorena.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.OpenEpisodesInSector(ctx, "P"); err == nil {
		t.Error("neispravan identitet epizode prošao")
	}
	baza.Close()
	if _, err := repo.OpenEpisodesInSector(ctx, "P"); err == nil {
		t.Error("zatvorena baza: očekivana greška")
	}
}
