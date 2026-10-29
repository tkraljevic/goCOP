package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Kvačica „vrijedi i ugroženo područje cijele poddionice” na nasipu ostaje
// pri spremanju dionice, ide u knjigu verzija i preživi primjenu na drugom
// čvoru.
func TestSectionUgrozenoPoddioniceSeCuva(t *testing.T) {
	ctx := context.Background()
	database, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Međudržavne rijeke Drava i Dunav', 'VGI')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(database, "test")
	repo := NewSectionRepository(database, rec)
	sec := &models.Section{Code: "B.34.14", AreaID: 34, SectorID: "B", Description: "probna",
		Parts: []models.SectionPart{{Seq: 1, Description: "r. Drava, l.o.", Embankments: []models.PartEmbankment{
			{Name: "Nasip Zabara-Hobođ", UgrozenoPoddionice: true},
			{Name: "Sekundarni nasip Karaula"},
		}}}}
	if err := repo.SaveSection(ctx, sec); err != nil {
		t.Fatal(err)
	}
	provjeri := func(kad string) {
		t.Helper()
		got, err := repo.GetSectionByCode("B.34.14")
		if err != nil || got == nil {
			t.Fatalf("%s: %v", kad, err)
		}
		e := got.Parts[0].Embankments
		if len(e) != 2 || !e[0].UgrozenoPoddionice || e[1].UgrozenoPoddionice {
			t.Errorf("%s: kvačice nasipa %+v", kad, e)
		}
	}
	provjeri("nakon spremanja")

	h, err := rec.History(ctx, EntitySections, "B.34.14")
	if err != nil || len(h) != 1 {
		t.Fatalf("knjiga verzija: %v %d", err, len(h))
	}
	if !strings.Contains(string(h[0].Payload), `"ugrozeno_poddionice":true`) {
		t.Errorf("kvačica nije u verziji: %s", h[0].Payload)
	}
	if strings.Count(string(h[0].Payload), `"ugrozeno_poddionice":true`) != 1 {
		t.Errorf("nasip bez kvačice ne smije nositi kvačicu: %s", h[0].Payload)
	}

	// isti zapis primijenjen kao da je stigao s drugog čvora
	if _, err := database.Exec(`UPDATE sections SET parts = '[]' WHERE code = 'B.34.14'`); err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := primijeniDionicu(ctx, tx, h[0]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	provjeri("nakon primjene")
}
