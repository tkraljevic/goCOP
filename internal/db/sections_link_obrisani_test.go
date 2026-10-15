package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Brisanje letve ili objekta miče vezu iz kazala dionice, a dionica ga i dalje
// navodi. Obnova kazala takvu poveznicu preskače, i samo nju: zapis koji još
// nije stigao, i zapis obrisan pa vraćen, i dalje padaju na stranom ključu,
// da dionica pričeka dok ne stignu.

func bazaKazala(t *testing.T) (*sql.DB, *ledger.Recorder) {
	t.Helper()
	database, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO P', 'COP P')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (61, 'P', 'Područje 61', 'VGI 61')`,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('letva-ziva', 'P-1', 'Probna letva', '2026-01-01', '2026-01-01')`,
		`INSERT INTO structures (id, code, name, kind, sector_id, area_id, created_at, updated_at)
			VALUES ('objekt-zivi', 'P-CS', 'CS Probna', 'CS', 'P', 61, '2026-01-01', '2026-01-01')`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	return database, ledger.New(database, "ured")
}

// povijest upisuje verzije zapisa u knjigu, redom; true je spomenik
func povijest(t *testing.T, rec *ledger.Recorder, database *sql.DB, entitet, id string, arhivirano ...bool) {
	t.Helper()
	for _, a := range arhivirano {
		upisi := rec.Record
		if a {
			upisi = rec.Archive
		}
		if _, err := upisi(context.Background(), database, entitet, id, map[string]string{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
}

func dionicaKazala(letve, objekti []string) *models.Section {
	p := models.SectionPart{StationIDs: letve}
	for i, id := range objekti {
		if i%2 == 0 {
			p.Objects = append(p.Objects, models.PartObject{StructureID: id, Name: "objekt " + id})
		} else {
			p.Embankments = append(p.Embankments, models.PartEmbankment{StructureID: id, Name: "nasip " + id})
		}
	}
	return &models.Section{Code: "P.61.1", AreaID: 61, SectorID: "P", Description: "probna", DescriptionCustom: true,
		Parts: []models.SectionPart{p}}
}

func upisiDionicu(database *sql.DB, sec *models.Section) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := WriteSection(context.Background(), tx, sec); err != nil {
		return err
	}
	return tx.Commit()
}

func kazalo(t *testing.T, database *sql.DB, upit string) []string {
	t.Helper()
	rows, err := database.Query(upit)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func TestKazaloDionicePreskaceObrisaneZapise(t *testing.T) {
	database, rec := bazaKazala(t)
	povijest(t, rec, database, "stations", "letva-obrisana", false, true)
	povijest(t, rec, database, "structures", "objekt-obrisan", false, false, true)
	povijest(t, rec, database, "structures", "nasip-obrisan", true)
	// zapis na površini ostaje u kazalu i kad mu je neka starija verzija spomenik
	povijest(t, rec, database, "stations", "letva-ziva", true, false)

	sec := dionicaKazala([]string{"letva-obrisana", "letva-ziva"}, []string{"objekt-obrisan", "nasip-obrisan", "objekt-zivi"})
	if err := upisiDionicu(database, sec); err != nil {
		t.Fatalf("dionica s obrisanom letvom i objektom: %v", err)
	}
	if got := kazalo(t, database, `SELECT station_id FROM section_stations`); !slices.Equal(got, []string{"letva-ziva"}) {
		t.Errorf("kazalo letvi: %v", got)
	}
	if got := kazalo(t, database, `SELECT structure_id FROM section_structures`); !slices.Equal(got, []string{"objekt-zivi"}) {
		t.Errorf("kazalo objekata: %v", got)
	}
	if err := LinkAllSections(context.Background(), database); err != nil {
		t.Errorf("vezanje svih dionica: %v", err)
	}
}

func TestKazaloDioniceCekaZapisKojiNijeObrisan(t *testing.T) {
	database, rec := bazaKazala(t)
	povijest(t, rec, database, "stations", "letva-vracena", false, true, false)
	povijest(t, rec, database, "structures", "objekt-vracen", true, false)
	povijest(t, rec, database, "stations", "letva-u-knjizi", false)

	for _, c := range []struct {
		ime            string
		letve, objekti []string
	}{
		{"letva koja još nije stigla", []string{"letva-nepoznata"}, nil},
		{"letva u knjizi koja nije na površini", []string{"letva-u-knjizi"}, nil},
		{"letva obrisana pa vraćena", []string{"letva-vracena"}, nil},
		{"objekt koji još nije stigao", nil, []string{"objekt-nepoznat"}},
		{"nasip koji još nije stigao", nil, []string{"objekt-zivi", "nasip-nepoznat"}},
		{"objekt obrisan pa vraćen", nil, []string{"objekt-vracen"}},
	} {
		t.Run(c.ime, func(t *testing.T) {
			err := upisiDionicu(database, dionicaKazala(append([]string{"letva-ziva"}, c.letve...), c.objekti))
			if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
				t.Errorf("upis dionice javlja %v, a treba pasti na stranom ključu", err)
			}
		})
	}
	if n := kazalo(t, database, `SELECT code FROM sections`); len(n) != 0 {
		t.Errorf("dionica je upisana iako zapis nije stigao: %v", n)
	}
}
