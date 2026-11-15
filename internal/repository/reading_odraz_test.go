package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Očitanje objekta piše vodostaj i na letve objekta: prvi vodostaj na
// unutarnju ili uzvodnu, drugi na vanjsku ili nizvodnu. Izmjena i brisanje
// na objektu prenose se na letve, a objekt bez letve piše samo na sebe
// (Odluke (15. 11. 2026.)).
func TestOcitanjeObjektaPiseNaLetve(t *testing.T) {
	ctx := context.Background()
	baza, rec := bazaSPodrucjem(t)
	repo := NewReadingRepository(baza, rec)
	gore, dolje, nova, ustava, bez := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for i, l := range []string{gore, dolje, nova} {
		if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES (?, ?, ?, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
			l, "letva-"+string(rune('a'+i)), "Letva"); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range [][3]string{{ustava, gore, dolje}, {bez, "", ""}} {
		if _, err := baza.Exec(`INSERT INTO structures (id, code, name, kind, sector_id, area_id, station_id, station_down_id, created_at, updated_at)
			VALUES (?, ?, 'Ustava Primjerovo', 'USTAVA', 'B', 34, ?, ?, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`, o[0], "u-"+o[0][:8], o[1], o[2]); err != nil {
			t.Fatal(err)
		}
	}
	cm := func(v int) *int { return &v }
	kad := time.Date(2026, 11, 15, 7, 0, 0, 0, models.Zagreb).UTC()
	rd := models.Reading{StructureID: ustava, MeasuredAt: kad, LevelCm: cm(120), Level2Cm: cm(80), Gate: "ZATVORENA",
		Source: models.ReadingSourceManual, Origin: models.ReadingOriginGoCOP, Observer: "Ivo Ivić", UserID: uuid.NewString()}
	if err := repo.Create(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	odraz := func(strana int) *models.Reading {
		t.Helper()
		o, err := repo.Get(ctx, idOdraza(rd.ID.String(), strana))
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	o1, o2 := odraz(1), odraz(2)
	if o1 == nil || o1.StationID != gore || *o1.LevelCm != 120 || o2 == nil || o2.StationID != dolje || *o2.LevelCm != 80 ||
		!o1.JeOdrazObjekta() || o1.Observer != "Ivo Ivić" || o1.Source != models.ReadingSourceManual || o1.Gate != "" {
		t.Fatalf("odrazi: %+v %+v", o1, o2)
	}

	// izmjena vodostaja ide na letvu; bez drugog vodostaja nizvodnog odraza nema
	rd.LevelCm, rd.Level2Cm = cm(118), nil
	if err := repo.Update(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	if o1, o2 = odraz(1), odraz(2); o1 == nil || *o1.LevelCm != 118 || o2 != nil {
		t.Errorf("nakon izmjene: %+v %+v", o1, o2)
	}

	// letva objekta promijenjena u registru: odraz prelazi na novu letvu
	if _, err := baza.Exec(`UPDATE structures SET station_id = ? WHERE id = ?`, nova, ustava); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	if o1 = odraz(1); o1 == nil || o1.StationID != nova {
		t.Errorf("odraz nije prešao na novu letvu: %+v", o1)
	}

	if err := repo.Delete(ctx, rd.ID); err != nil {
		t.Fatal(err)
	}
	if odraz(1) != nil || odraz(2) != nil {
		t.Error("brisanje na objektu nije obrisalo odraze")
	}

	samo := models.Reading{StructureID: bez, MeasuredAt: kad, LevelCm: cm(50), Source: models.ReadingSourceManual}
	if err := repo.Create(ctx, &samo); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := baza.QueryRow(`SELECT count(*) FROM readings WHERE station_id != ''`).Scan(&n); err != nil || n != 0 {
		t.Errorf("objekt bez letve upisao je %d očitanja letve (%v)", n, err)
	}
}

// Greška baze u odrazu vraća se, pa se cijeli upis na objektu poništi
func TestOdrazGreskeBaze(t *testing.T) {
	ctx := context.Background()
	baza, rec := bazaSPodrucjem(t)
	repo := NewReadingRepository(baza, rec)
	letva := uuid.NewString()
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES (?, 'l', 'L', '2026-01-01', '2026-01-01');
		INSERT INTO structures (id, code, name, kind, sector_id, area_id, station_id, created_at, updated_at)
		VALUES ('cs', 'cs', 'CS', 'CRPNA_STANICA', 'B', 34, ?, '2026-01-01', '2026-01-01')`, letva); err != nil {
		t.Fatal(err)
	}
	cm := 10
	rd := models.Reading{StructureID: "cs", MeasuredAt: time.Now().Add(-time.Hour), LevelCm: &cm, Source: models.ReadingSourceManual}
	if err := repo.Create(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	u := func(priprema string, radnja func(*sql.Tx) error) {
		t.Helper()
		tx, err := baza.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(priprema); err != nil {
			t.Fatal(err)
		}
		if radnja(tx) == nil {
			t.Errorf("bez greške uz: %s", priprema)
		}
	}
	odrazi := func(tx *sql.Tx) error { return repo.odrazi(ctx, tx, &rd) }
	obrisi := func(tx *sql.Tx) error { return repo.obrisiOdraze(ctx, tx, &rd) }
	u(`ALTER TABLE structures RENAME TO s2`, odrazi)
	u(`ALTER TABLE readings RENAME TO r2`, odrazi)
	u(`ALTER TABLE readings RENAME TO r2`, obrisi)
	u(`CREATE TEMP TRIGGER ne_brisi BEFORE DELETE ON readings BEGIN SELECT RAISE(ABORT, 'ne'); END`, obrisi)
}

// Kad odraz ne prođe, ne prolazi ni upis, izmjena ni brisanje na objektu:
// objekt i letva ne smiju se razići
func TestOdrazNeProlaziNiObjekt(t *testing.T) {
	ctx := context.Background()
	baza, rec := bazaSPodrucjem(t)
	baza.SetMaxOpenConns(1) // privremeni okidač vrijedi samo na svojoj vezi
	repo := NewReadingRepository(baza, rec)
	letva := uuid.NewString()
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES (?, 'l', 'L', '2026-01-01', '2026-01-01');
		INSERT INTO structures (id, code, name, kind, sector_id, area_id, station_id, created_at, updated_at)
		VALUES ('cs', 'cs', 'CS', 'CRPNA_STANICA', 'B', 34, ?, '2026-01-01', '2026-01-01')`, letva); err != nil {
		t.Fatal(err)
	}
	cm := 10
	rd := models.Reading{StructureID: "cs", MeasuredAt: time.Now().Add(-time.Hour), LevelCm: &cm, Source: models.ReadingSourceManual}
	if err := repo.Create(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	okidac := func(sql string) {
		t.Helper()
		if _, err := baza.Exec(`DROP TRIGGER IF EXISTS temp.ne_odrazu; ` + sql); err != nil {
			t.Fatal(err)
		}
	}
	okidac(`CREATE TEMP TRIGGER ne_odrazu BEFORE INSERT ON readings WHEN NEW.source_ref LIKE 'objekt:%' BEGIN SELECT RAISE(ABORT, 'ne'); END`)
	novo := models.Reading{StructureID: "cs", MeasuredAt: time.Now().Add(-2 * time.Hour), LevelCm: &cm, Source: models.ReadingSourceManual}
	if err := repo.Create(ctx, &novo); err == nil {
		t.Error("upis na objekt prošao je bez odraza")
	}
	drugi := 11
	rd.LevelCm = &drugi
	if err := repo.Update(ctx, &rd); err == nil {
		t.Error("izmjena na objektu prošla je bez odraza")
	}
	okidac(`CREATE TEMP TRIGGER ne_odrazu BEFORE DELETE ON readings WHEN OLD.source_ref LIKE 'objekt:%' BEGIN SELECT RAISE(ABORT, 'ne'); END`)
	if err := repo.Delete(ctx, rd.ID); err == nil {
		t.Error("brisanje na objektu prošlo je bez odraza")
	}
	// ni kanal ni verzija bez kojih očitanje ne putuje
	okidac(`CREATE TEMP TRIGGER ne_odrazu BEFORE UPDATE OF channel ON readings BEGIN SELECT RAISE(ABORT, 'ne'); END`)
	if err := repo.Create(ctx, &novo); err == nil {
		t.Error("upis bez kanala")
	}
	if err := repo.Update(ctx, &rd); err == nil {
		t.Error("izmjena bez kanala")
	}
	okidac(``)
	if o, err := repo.Get(ctx, rd.ID); err != nil || o == nil || *o.LevelCm != 10 {
		t.Errorf("objekt se razišao s letvom: %+v %v", o, err)
	}
}
