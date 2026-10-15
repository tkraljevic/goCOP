package db

import (
	"database/sql"
	"slices"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Brisanje naselja ili općine miče njihove veze iz kazala dionice, a dionica
// ih i dalje navodi. Do 13. 10. 2026. obnova kazala takvu vezu je htjela
// upisati i padala na stranom ključu: na novom čvoru i pri svakom pokretanju.

// bazaTeritorija je baza kazala sa županijom 901, općinom 801 i naseljem 1 na
// površini. U knjizi su obrisani naselje 2, općina 802 i županija 902, a
// naselje 3 obrisano je pa vraćeno, ali na površinu još nije došlo.
func bazaTeritorija(t *testing.T) *sql.DB {
	t.Helper()
	database, rec := bazaKazala(t)
	for _, stmt := range []string{
		`INSERT INTO counties (id, code, name, seat) VALUES (901, 'PRB', 'Probna', 'Sjedište')`,
		`INSERT INTO municipalities (id, county_id, name, type) VALUES (801, 901, 'Probna Općina', 'OPĆINA')`,
		`INSERT INTO settlements (id, municipality_id, county_id, name) VALUES (1, 801, 901, 'Probno Selo')`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	povijest(t, rec, database, "settlements", "2", false, true)
	povijest(t, rec, database, "municipalities", "802", false, true)
	povijest(t, rec, database, "counties", "902", true)
	povijest(t, rec, database, "settlements", "3", false, true, false)
	// naselje na površini ostaje u kazalu i kad mu je neka starija verzija spomenik
	povijest(t, rec, database, "settlements", "1", true, false)
	return database
}

func dionicaNaTeritoriju(teritorij ...models.PartTerritory) *models.Section {
	sec := dionicaKazala(nil, nil)
	sec.Parts[0].Territories = teritorij
	return sec
}

func naselje(id int) *int { return &id }

func TestKazaloDionicePreskaceObrisaniTeritorij(t *testing.T) {
	database := bazaTeritorija(t)
	err := upisiDionicu(database, dionicaNaTeritoriju(
		models.PartTerritory{CountyID: 901, MunicipalityID: 801, SettlementID: naselje(1)},
		models.PartTerritory{CountyID: 901, MunicipalityID: 801, SettlementID: naselje(2)},
		models.PartTerritory{CountyID: 901, MunicipalityID: 802},
		models.PartTerritory{CountyID: 902, MunicipalityID: 801},
		models.PartTerritory{CountyID: 901, MunicipalityID: 801},
	))
	if err != nil {
		t.Fatalf("dionica s obrisanim naseljem, općinom i županijom: %v", err)
	}
	got := kazalo(t, database, `SELECT county_id || '/' || municipality_id || COALESCE('/' || settlement_id, '') FROM section_territories`)
	if want := []string{"901/801", "901/801/1"}; !slices.Equal(got, want) {
		t.Errorf("kazalo teritorija: %v, a treba %v", got, want)
	}
}

func TestKazaloDioniceCekaTeritorijKojiNijeObrisan(t *testing.T) {
	for _, c := range []struct {
		ime       string
		teritorij models.PartTerritory
	}{
		{"naselje koje još nije stiglo", models.PartTerritory{CountyID: 901, MunicipalityID: 801, SettlementID: naselje(7)}},
		{"općina koja još nije stigla", models.PartTerritory{CountyID: 901, MunicipalityID: 803}},
		{"naselje obrisano pa vraćeno", models.PartTerritory{CountyID: 901, MunicipalityID: 801, SettlementID: naselje(3)}},
	} {
		t.Run(c.ime, func(t *testing.T) {
			err := upisiDionicu(bazaTeritorija(t), dionicaNaTeritoriju(c.teritorij))
			if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
				t.Errorf("upis dionice javlja %v, a treba pasti na stranom ključu", err)
			}
		})
	}
}
