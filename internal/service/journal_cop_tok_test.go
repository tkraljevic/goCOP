package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Cijeli put dnevnika COP-a na pravoj bazi: voditelj centra ga otvori,
// dežurni s područja u njemu piše, zapis se stornira i ostaje s razlogom,
// a poslije zaključenja u njega više ništa ne ulazi.
func TestTokDnevnikaCOPa(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "cop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (15, 'B', 'Vuka', 'VGI Vuka', 'Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (20, 'B', 'Karašica-Vučica', 'VGI', 'Virovitica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	repo := repository.NewJournalRepository(baza, ledger.New(baza, "test"))
	s := NewJournalService(repo, nil, nil)

	voditelj := &models.User{ID: uuid.New(), FullName: "Voditelj COP-a"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	dezurni := &models.User{ID: uuid.New(), FullName: "Dežurni iz Virovitice"}
	podrucni := &models.UserPermissions{AllowedAreas: map[int]bool{20: true}}

	podrucja := []models.Area{{ID: 15, SectorID: "B"}, {ID: 20, SectorID: "B"}}
	pocetak := time.Date(2026, 9, 10, 0, 0, 0, 0, models.Zagreb)

	// Dežurni s područja ne otvara dnevnik: nije njegov.
	j := models.Journal{CentarSektor: "B", StartedAt: &pocetak}
	if err := s.SpremiCOPDnevnik(ctx, dezurni, podrucni, &j); err == nil {
		t.Fatal("dežurni s područja otvorio dnevnik centra")
	}
	if err := s.SpremiCOPDnevnik(ctx, voditelj, uprava, &j); err != nil {
		t.Fatal(err)
	}
	if j.ID == "" || j.Kind != models.JournalKindDefense || j.AreaID != 0 || j.Title != "Dnevnik COP-a, 2026." || j.Year != 2026 {
		t.Fatalf("otvoren dnevnik: %+v", j)
	}

	// Popis po centru ga vidi, s imenom centra.
	popis, err := s.ListCOPJournals(ctx, "B")
	if err != nil || len(popis) != 1 || popis[0].CentarNaziv != "COP Osijek" {
		t.Fatalf("popis = %+v, %v", popis, err)
	}

	// Dežurni s područja piše u dnevnik centra — to je i bila poanta.
	o := models.OpsegDnevnika(j, nil, podrucja)
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, models.Zagreb)
	z := models.JournalEntry{Kind: models.EntryKindReport, Date: pocetak.AddDate(0, 0, 1), HappenedAt: &kad,
		ReportedBy: "Sa porte", Text: "  vodostaj Batina u 07:00 +551  "}
	if err := s.DodajZapisCOP(ctx, dezurni, podrucni, o, &j, &z); err != nil {
		t.Fatal(err)
	}
	if z.Number != 1 || z.UserName != "Dežurni iz Virovitice" || z.Text != "vodostaj Batina u 07:00 +551" || z.SheetID != "" {
		t.Fatalf("zapis: %+v", z)
	}
	z2 := models.JournalEntry{Kind: models.EntryKindDuty, Date: pocetak.AddDate(0, 0, 1), Text: "Dežurstvo 07:00 – 15:00"}
	if err := s.DodajZapisCOP(ctx, voditelj, uprava, o, &j, &z2); err != nil {
		t.Fatal(err)
	}

	// Storno: zapis ostaje, s razlogom i brojem; tuđi stornira samo nadzor.
	if err := s.VoidEntry(ctx, dezurni, podrucni, o, z2.ID, "krivi zapis"); err != nil {
		// dežurni nije izvođač, pa je "nadzor" i smije — provjera je da ne padne
		t.Fatal(err)
	}
	zapisi, err := s.EntriesForJournal(ctx, j.ID)
	if err != nil || len(zapisi) != 2 {
		t.Fatalf("zapisi = %d, %v", len(zapisi), err)
	}
	if !zapisi[1].Voided || zapisi[1].VoidReason != "krivi zapis" || zapisi[1].Number != 2 {
		t.Fatalf("storniran zapis: %+v", zapisi[1])
	}

	// Zaključenje: kraj se upisuje, centar ostaje, poslije toga se ne piše.
	kraj := pocetak.AddDate(0, 0, 1)
	izmjena := models.Journal{ID: j.ID, CentarSektor: "D", Title: "Dnevnik COP-a — Dunav, rujan 2026.", StartedAt: &pocetak, EndedAt: &kraj}
	if err := s.SpremiCOPDnevnik(ctx, voditelj, uprava, &izmjena); err != nil {
		t.Fatal(err)
	}
	if izmjena.CentarSektor != "B" || izmjena.EndedAt == nil || izmjena.Title != "Dnevnik COP-a — Dunav, rujan 2026." || izmjena.CreatedBy != voditelj.ID.String() {
		t.Fatalf("zaglavlje poslije izmjene: %+v", izmjena)
	}
	kasno := models.JournalEntry{Kind: models.EntryKindReport, Date: kraj.AddDate(0, 0, 1), Text: "prekasno"}
	if err := s.DodajZapisCOP(ctx, dezurni, podrucni, o, &izmjena, &kasno); err == nil {
		t.Fatal("zaključen dnevnik primio zapis poslije kraja")
	}
}
