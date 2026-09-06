package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"gocop/internal/models"
)

// Objekt nosi stacionažu dvaput: kao zapis iz Privitka ("rkm 1428+010") i kao
// broj po kojem se raspoređuje po nasipima. Klijent koji pošalje samo zapis ne
// smije broj izbrisati — upis ga izvodi natrag.
func TestUpisIzvodiStacionazuObjektaIzZapisa(t *testing.T) {
	database, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'BP 34', 'VGI Baranja')`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	sec := &models.Section{
		Code: "B.34.1", AreaID: 34, SectorID: "B", Description: "r. Dunav",
		Parts: []models.SectionPart{{
			Seq: 1,
			Objects: []models.PartObject{
				{Name: "ušće Šarkanjskog Dun.", StationingKind: "rkm", StationingText: "rkm 1428+010"},
				{Name: "CS Budžak", StationingKind: "nkm", StationingText: "km 2+700"},
				{Name: "bez stacionaže"},
			},
		}},
	}
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSection(context.Background(), tx, sec); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	o := sec.Parts[0].Objects
	if o[0].Stationing == nil || *o[0].Stationing != 1428.01 {
		t.Errorf("rkm 1428+010 nije izveden u broj: %v", o[0].Stationing)
	}
	if o[1].Stationing == nil || *o[1].Stationing != 2.7 {
		t.Errorf("km 2+700 nije izveden u broj: %v", o[1].Stationing)
	}
	if o[2].Stationing != nil {
		t.Error("objekt bez zapisa ne smije dobiti izmišljen broj")
	}
}

// Povijest kote nule mora preživjeti krug upis → čitanje; stupac je JSON, pa
// prazna povijest ostaje "[]" i čitanje ne mora nagađati.
func TestPovijestKoteNulePrezivljavaUpis(t *testing.T) {
	database, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	kota := 80.45
	if _, err := database.Exec(`INSERT INTO stations (id, code, name, zero_datum_history, created_at, updated_at)
		VALUES ('01a0-x', 'batina', 'Batina', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`[{"valid_from":"2017-06-16","datum":`+strconv.FormatFloat(kota, 'f', -1, 64)+`,"system":"TRST"}]`); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := database.QueryRow(`SELECT zero_datum_history FROM stations WHERE code='batina'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var hist []models.ZeroDatumChange
	if err := json.Unmarshal([]byte(raw), &hist); err != nil {
		t.Fatalf("povijest se ne čita: %v", err)
	}
	if len(hist) != 1 || hist[0].ValidFrom != "2017-06-16" || hist[0].Datum == nil || *hist[0].Datum != kota {
		t.Errorf("povijest krivo spremljena: %+v", hist)
	}
	var prazna string
	if _, err := database.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at)
		VALUES ('01a0-y', 'nova', 'Nova', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT zero_datum_history FROM stations WHERE code='nova'`).Scan(&prazna); err != nil {
		t.Fatal(err)
	}
	if prazna != "[]" {
		t.Errorf("prazna povijest je %q, očekivano []", prazna)
	}
}
