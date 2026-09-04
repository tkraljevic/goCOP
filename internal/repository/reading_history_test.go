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

// Terenski uređaj drži zadnjih godinu dana: razmjena mu smije donijeti nova
// očitanja, a povijest od stotinjak godina ne smije.
func TestRazmjenaPostujeRazdobljePovijesti(t *testing.T) {
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "teren.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(database, "izvorni-cvor")
	repo := NewReadingRepository(database, rec)
	ctx := context.Background()

	cm := func(v int) *int { return &v }
	novo := models.Reading{
		ID: uuid.New(), StationID: "postaja-1", MeasuredAt: time.Now().Add(-2 * time.Hour),
		LevelCm: cm(120), Source: models.ReadingSourceManual, Origin: models.ReadingOriginGoCOP,
	}
	staro := models.Reading{
		ID: uuid.New(), StationID: "postaja-1", MeasuredAt: time.Now().AddDate(-30, 0, 0),
		LevelCm: cm(300), Source: models.ReadingSourceImport, Origin: models.ReadingOriginArchive,
	}
	if _, err := repo.ImportBatch(ctx, []models.Reading{novo, staro}); err != nil {
		t.Fatal(err)
	}
	verzije, err := rec.Since(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}

	// drugi čvor, terenski: drži zadnjih 12 mjeseci
	teren, err := db.OpenDB(filepath.Join(t.TempDir(), "teren2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer teren.Close()
	if err := db.InitSchema(teren); err != nil {
		t.Fatal(err)
	}
	terenRec := ledger.New(teren, "terenski-cvor")
	SetReadingHistoryPolicy(ReadingHistoryPolicy{Months: 12})
	defer SetReadingHistoryPolicy(ReadingHistoryPolicy{})

	// razmjena prvo pita želi li čvor verziju, pa je tek onda upisuje u knjigu
	var primljene []ledger.Version
	for _, v := range verzije {
		if KeepVersion(v) {
			primljene = append(primljene, v)
		}
	}
	if _, err := terenRec.Apply(ctx, primljene); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, teren, terenRec, primljene); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := teren.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("terenski čvor je preuzeo %d očitanja, očekivano samo jedno (novo)", n)
	}
	var id string
	if err := teren.QueryRow(`SELECT id FROM readings`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != novo.ID.String() {
		t.Errorf("preuzeto je krivo očitanje: %s", id)
	}

	// verzije ostaju u knjizi, pa čvor zna dokle je stigao i ne traži ih ponovno
	var verzijaN int
	if err := teren.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = 'readings'`).Scan(&verzijaN); err != nil {
		t.Fatal(err)
	}
	if verzijaN != 1 {
		t.Errorf("u knjizi je %d verzija očitanja, očekivana 1: staro očitanje ne smije zauzeti mjesto", verzijaN)
	}

	// kad čvor zaprati letvu, njezina povijest prolazi bez obzira na razdoblje
	SetReadingHistoryPolicy(ReadingHistoryPolicy{Months: 12, Followed: map[string]bool{"station:postaja-1": true}})
	if !KeepVersion(verzije[0]) || !KeepVersion(verzije[1]) {
		t.Error("praćena letva mora proći cijelu povijest")
	}

	// bez ograde čvor preuzima sve
	SetReadingHistoryPolicy(ReadingHistoryPolicy{})
	puni, err := db.OpenDB(filepath.Join(t.TempDir(), "cop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer puni.Close()
	if err := db.InitSchema(puni); err != nil {
		t.Fatal(err)
	}
	puniRec := ledger.New(puni, "cop-cvor")
	if _, err := puniRec.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, puni, puniRec, verzije); err != nil {
		t.Fatal(err)
	}
	if err := puni.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("čvor bez ograde je preuzeo %d očitanja, očekivana 2", n)
	}
}
