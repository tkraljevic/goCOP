package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zid ne pokazuje verziju kojoj oznaka zapisa ne odgovara sadržaju: takva
// verzija ništa ne mijenja, pa storniran zapis pod drugom oznakom nije
// događaj. Zapis upisan na ovom računalu zid pokazuje kao i dosad.
func TestZidBezVerzijeDrugeOznake(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	journals := repository.NewJournalRepository(baza, rec)
	pocetak := time.Now().In(models.Zagreb).AddDate(0, 0, -1)
	j := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, proba", Year: pocetak.Year(), StartedAt: &pocetak}
	if err := journals.SaveJournal(ctx, j); err != nil {
		t.Fatal(err)
	}
	h := time.Now().In(models.Zagreb)
	e := &models.JournalEntry{JournalID: j.ID, Date: h, HappenedAt: &h, Kind: models.EntryKindReport, Text: "Procjeđivanje na rkm 1425+100.", UserName: "Ana Anić"}
	if err := journals.SaveEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	top, err := rec.Latest(ctx, repository.EntityJournalEntries, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	storniran := *e
	storniran.Voided = true
	sadrzaj, err := json.Marshal(storniran)
	if err != nil {
		t.Fatal(err)
	}
	druga := *top
	druga.VersionID, druga.EntityID, druga.NodeID, druga.Payload = uuid.Must(uuid.NewV7()).String(), "drugi-zapis", "ured", sadrzaj
	if _, err := rec.Apply(ctx, []ledger.Version{druga}); err != nil {
		t.Fatal(err)
	}

	zid := NewZidService(rec, journals, repository.NewSectionRepository(baza, rec), repository.NewMtsRepository(baza, rec),
		repository.NewUserRepository(baza, rec), nil, repository.NewEpisodeRepository(baza, rec))
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	dog, _, err := zid.Zadnji(ctx, uprava, FiltarZida{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	dojava := 0
	for _, d := range dog {
		if d.VersionID == druga.VersionID || d.Naslov == "Storniran zapis" {
			t.Errorf("zid pokazuje verziju druge oznake: %+v", d)
		}
		if d.VersionID == top.VersionID && d.Naslov == "Dojava" {
			dojava++
		}
	}
	if dojava != 1 {
		t.Errorf("zapis upisan na ovom računalu na zidu %d puta: %+v", dojava, dog)
	}
}
