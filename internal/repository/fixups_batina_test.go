package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// Najniži vodostaj Batine iz 1909. stajao je kao izmjeren, a letva je
// utemeljena tek 2001. Popravak mu vraća podrijetlo: preračun iz Bezdana,
// koji je 740 m uzvodno i utemeljen 1856.
func TestPopravakVracaPodrijetloMinimuma1909(t *testing.T) {
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "popravci.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rec := ledger.New(database, "cvor")

	cm := func(v int) *int { return &v }
	prije, _ := json.Marshal([]models.StationExtreme{
		{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14",
			Quality: models.QualityMeasured, Source: "DHMZ"},
		{Kind: models.ExtremeMin, LevelCm: cm(-127), OnDate: "1909-01-07",
			Quality: models.QualityMeasured, Source: "DHMZ"},
	})
	id := uuid.New().String()
	if _, err := database.ExecContext(ctx, `INSERT INTO stations (id, code, name, extremes, created_at, updated_at)
		VALUES (?,?,?,?,?,?)`, id, "batina", "Batina", string(prije), time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if err := RunFixups(ctx, database, rec); err != nil {
		t.Fatal(err)
	}

	var sirovi string
	if err := database.QueryRowContext(ctx,
		`SELECT coalesce(extremes,'') FROM stations WHERE code='batina'`).Scan(&sirovi); err != nil {
		t.Fatal(err)
	}
	var ex []models.StationExtreme
	if err := json.Unmarshal([]byte(sirovi), &ex); err != nil {
		t.Fatal(err)
	}
	var min, max *models.StationExtreme
	for i := range ex {
		switch {
		case ex[i].Kind == models.ExtremeMin && ex[i].OnDate == "1909-01-07":
			min = &ex[i]
		case ex[i].Kind == models.ExtremeMax && ex[i].OnDate == "2013-06-14":
			max = &ex[i]
		}
	}
	if min == nil {
		t.Fatal("zapisani minimum je nestao")
	}
	if min.Quality != models.QualityReconstructed {
		t.Errorf("minimum se vodi kao %q, a letva je utemeljena 2001.", min.Quality)
	}
	if min.Source != "postaja Bezdan" || min.Method == "" {
		t.Errorf("nije zapisano odakle i kako: izvor %q, način %q", min.Source, min.Method)
	}
	if *min.LevelCm != -127 {
		t.Errorf("vrijednost je promijenjena na %d — popravak dira podrijetlo, ne brojku", *min.LevelCm)
	}
	// izmjereni maksimum iz 2013. mora ostati netaknut
	if max == nil || max.Quality != models.QualityMeasured || max.Source != "DHMZ" {
		t.Error("popravak je dirao izmjereni maksimum")
	}
}
