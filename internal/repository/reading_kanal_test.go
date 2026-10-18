package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// Očitanje iz prvog sata nove godine po hrvatskom vremenu (u UTC-u je to još
// stara godina) ostaje u kanalu svoje godine i kad se mijenja i kad se briše.
// Prije je izmjena, koja vrijeme čita iz baze u UTC-u, očitanje prebacivala
// u kanal prethodne godine, pa ga čvor koji prati samo novu godinu nije
// vidio izmijenjeno ni obrisano.
func TestKanalOcitanjaPoGodiniZagreba(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "kanal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	ctx := context.Background()
	// letva Primjerovo mjerodavna je za dionicu P.1.1 u području 1; letva
	// bez područja nema kanala, pa godina ne bi ništa mijenjala
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('primjerovo', 'PRIM', 'Primjerovo', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.WriteSection(ctx, baza, &models.Section{Code: "P.1.1", AreaID: 1, SectorID: "P"}); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('veza', 'P.1.1', 'primjerovo', ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	repo := NewReadingRepository(baza, ledger.New(baza, "cvor-probni"))
	cm := 120
	rd := &models.Reading{ID: uuid.New(), StationID: "primjerovo", MeasuredAt: time.Date(2026, time.January, 1, 0, 30, 0, 0, models.Zagreb),
		LevelCm: &cm, Source: models.ReadingSourceManual, Origin: models.ReadingOriginGoCOP, Observer: "Pero Perić"}
	if err := repo.Create(ctx, rd); err != nil {
		t.Fatal(err)
	}
	cm2 := 121
	rd.LevelCm = &cm2
	rd.MeasuredAt = rd.MeasuredAt.UTC()
	if err := repo.Update(ctx, rd); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, rd.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := baza.QueryContext(ctx, `SELECT channel FROM record_versions WHERE entity = ? AND entity_id = ? ORDER BY version_id`, EntityReadings, rd.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := ledger.ChannelFor(ledger.ChannelReadings, 1, 2026)
	var kanali []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		kanali = append(kanali, k)
	}
	if len(kanali) != 3 {
		t.Fatalf("upis, izmjena i brisanje trebaju tri verzije, a ima ih %d: %v", len(kanali), kanali)
	}
	for i, k := range kanali {
		if k != want {
			t.Errorf("verzija %d je u kanalu %q, a treba %q", i+1, k, want)
		}
	}
}
