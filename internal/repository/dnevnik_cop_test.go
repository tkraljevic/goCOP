package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	_ "modernc.org/sqlite"
)

func probnaBazaDnevnika(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	// Dnevnik mora postojati prije zapisa, a područje prije dnevnika.
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO areas (id, name, sector_id, vgi_name) VALUES (34, 'BP 34', 'B', 'VGI')`); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO journals (id, area_id, kind, created_at, updated_at)
		VALUES ('dn-1', 34, ?, ?, ?)`, models.JournalKindDefense, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return baza
}

// Dnevnik COP-a nosi dva vremena i dvije osobe.
//
// U zapisniku se u 07:15 upisuje vodostaj od 07:00 — oba su vremena dokazna,
// jer jedno briše razliku između "javio odmah" i "javio sat kasnije". A stupac
// s imenom najčešće je onaj tko je JAVIO, ne dežurni koji piše: "Sa porte
// javljaju", "Javorović iz Glavnog centra javio". Spojeno u jedno polje,
// odgovornost se pripiše onome tko je držao olovku.
func TestZapisNosiDvaVremenaIDvijeOsobe(t *testing.T) {
	db := probnaBazaDnevnika(t)
	defer db.Close()
	r := NewJournalRepository(db, ledger.New(db, "test-cvor"))
	ctx := context.Background()

	dogodilo := time.Date(2009, 6, 29, 7, 0, 0, 0, time.UTC)
	upisano := time.Date(2009, 6, 29, 7, 15, 0, 0, time.UTC)
	e := models.JournalEntry{
		JournalID:  "dn-1",
		Date:       dogodilo,
		Kind:       models.EntryKindReport,
		Text:       "vodostaj Batina u 07:00 +551",
		HappenedAt: &dogodilo,
		ReportedBy: "Marko Marić",
		UserID:     "u-1",
		UserName:   "Dežurni operater",
		CreatedAt:  upisano,
		UpdatedAt:  upisano,
	}
	if err := r.SaveEntry(ctx, &e); err != nil {
		t.Fatal(err)
	}

	natrag, err := r.queryEntries(ctx, `e.journal_id = ?`, "dn-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(natrag) != 1 {
		t.Fatalf("pročitano %d zapisa", len(natrag))
	}
	z := natrag[0]
	if z.HappenedAt == nil {
		t.Fatal("vrijeme događaja nije zapamćeno")
	}
	if !z.HappenedAt.Equal(dogodilo) {
		t.Errorf("dogodilo se u %s, zapamćeno %s", dogodilo, z.HappenedAt)
	}
	if !z.CreatedAt.Equal(upisano) {
		t.Errorf("upisano u %s, zapamćeno %s", upisano, z.CreatedAt)
	}
	if z.HappenedAt.Equal(z.CreatedAt) {
		t.Error("dva vremena su se stopila u jedno")
	}
	if z.ReportedBy != "Marko Marić" {
		t.Errorf("tko je javio: %q", z.ReportedBy)
	}
	if z.UserName != "Dežurni operater" {
		t.Errorf("tko je upisao: %q", z.UserName)
	}
}

// Zapis bez vremena događaja je dopušten: zna se samo dan.
func TestZapisBezVremenaDogadjajaProlazi(t *testing.T) {
	db := probnaBazaDnevnika(t)
	defer db.Close()
	r := NewJournalRepository(db, ledger.New(db, "test-cvor"))
	ctx := context.Background()

	dan := time.Date(2009, 6, 29, 0, 0, 0, 0, time.UTC)
	e := models.JournalEntry{JournalID: "dn-1", Date: dan, Kind: models.EntryKindNote,
		Text: "bez sata", CreatedAt: dan, UpdatedAt: dan}
	if err := r.SaveEntry(ctx, &e); err != nil {
		t.Fatal(err)
	}
	natrag, _ := r.queryEntries(ctx, `e.journal_id = ?`, "dn-1")
	if len(natrag) != 1 || natrag[0].HappenedAt != nil {
		t.Errorf("prazno vrijeme događaja nije ostalo prazno: %+v", natrag)
	}
}
