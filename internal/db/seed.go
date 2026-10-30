package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"gocop/internal/models"
	"log"
	"os"
	"time"
)

// Imenik djelatnika NIJE ugrađen u program: nosi imena, telefone i adrese
// e-pošte zaposlenih, a kod i binarna datoteka su javni. Datoteka stoji uz
// bazu (data/imenik.json), izvan repozitorija, i čita se samo pri prvom
// punjenju. Ostali čvorovi djelatnike dobivaju sinkronizacijom, pa imenik
// treba jedino prvom čvoru u mreži.
var ImenikPath = "data/imenik.json"

// UseRepoImenik traži imenik u mapi data/ repozitorija i javlja je li nađen;
// usput postavlja i mapu registara. Novi testovi ga ne zovu, nego rade na
// izmišljenom skupu (KoristiIzmisljenePodatke); ostaje zbog testova pisanih
// prije tog skupa, koji bez njega ne bi prevodili.
func UseRepoImenik() bool {
	if !UseRepoData() {
		return false
	}
	_, err := os.Stat(ImenikPath)
	return err == nil
}

type seedDuty struct {
	Title        string  `json:"title"`
	Role         string  `json:"role"`
	ScopeType    string  `json:"scope_type"`
	SectorID     *string `json:"sector_id"`
	AreaID       *int    `json:"area_id"`
	SectionCodes string  `json:"section_codes"`
	IsPrimary    bool    `json:"is_primary"`
}

type seedUser struct {
	Username      string     `json:"username"`
	FullName      string     `json:"full_name"`
	Title         string     `json:"title"`
	OrgType       string     `json:"org_type"`
	OrgName       string     `json:"org_name"`
	Phone         string     `json:"phone"`
	MobilePhone   string     `json:"mobile_phone"`
	ShortPhone    string     `json:"short_phone"`
	ShortMobile   string     `json:"short_mobile"`
	Email         string     `json:"email"`
	IsGlobalAdmin bool       `json:"is_global_admin"`
	Duties        []seedDuty `json:"duties"`
}

// SeedInitialData puni praznu bazu: organizaciju i registre iz datoteka uz
// bazu kad ih ima, djelatnike iz imenika i račun admin. Vraća privremene
// lozinke računa iz imenika, koje globalni administrator jednom preuzme i
// preda ljudima; u bazi je samo njihov sažetak.
func SeedInitialData(database *sql.DB) ([]PocetnaLozinka, error) {
	// 1. Organizacija: sektori i branjena područja. Program ih ne zna sam;
	// stvara ih globalni administrator ili se učitaju uz bazu pri prvom punjenju.
	if err := seedOrganization(database); err != nil {
		return nil, err
	}

	// 3. Početni korisnici s višestrukim funkcijama
	lozinke, err := seedUsers(database)
	if err != nil {
		return nil, err
	}

	// 4. Početne štićene dionice iz prijepisa Privitka uz bazu
	var sectionCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM sections").Scan(&sectionCount); err == nil && sectionCount == 0 {
		sections, err := LoadSections()
		if err != nil {
			return nil, err
		}
		if sections == nil {
			log.Printf("Dionice: %s nije nađen — registri stižu sinkronizacijom s drugim čvorom", DataFile("sections.json"))
		}
		tx, err := database.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		ctx := context.Background()
		for i := range sections {
			if err := WriteSection(ctx, tx, &sections[i]); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	// 5. Početne teritorijalne jedinice (21 županija, gradovi, općine, naselja)
	var countyCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM counties").Scan(&countyCount); err == nil && countyCount == 0 {
		var rawCounties []struct {
			ID             int    `json:"id"`
			Code           string `json:"code"`
			Name           string `json:"name"`
			Seat           string `json:"seat"`
			Prefect        string `json:"prefect"`
			AreaSqKm       int    `json:"area_sqkm"`
			Population     int    `json:"population"`
			Email          string `json:"email"`
			Phone          string `json:"phone"`
			Municipalities []struct {
				ID          int     `json:"id"`
				CountyID    int     `json:"county_id"`
				Name        string  `json:"name"`
				Type        string  `json:"type"`
				HeadTitle   string  `json:"head_title"`
				HeadName    string  `json:"head_name"`
				PostalCode  string  `json:"postal_code"`
				AreaSqKm    float64 `json:"area_sqkm"`
				Population  int     `json:"population"`
				Settlements []struct {
					ID             int    `json:"id"`
					MunicipalityID int    `json:"municipality_id"`
					CountyID       int    `json:"county_id"`
					Name           string `json:"name"`
					PostalCode     string `json:"postal_code"`
					Population     int    `json:"population"`
				} `json:"settlements"`
			} `json:"municipalities"`
		}
		raw, err := readDataFile("territories.json")
		if errors.Is(err, ErrNoDataFile) {
			raw = []byte("[]")
		} else if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &rawCounties); err != nil {
			return nil, fmt.Errorf("greška pri čitanju territories.json: %w", err)
		}

		tx, err := database.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()

		insertCountyStmt, err := tx.Prepare(`
			INSERT INTO counties (id, code, name, seat, prefect, area_sqkm, population, email, phone)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return nil, err
		}
		defer insertCountyStmt.Close()

		insertMuniStmt, err := tx.Prepare(`
			INSERT INTO municipalities (id, county_id, name, type, head_title, head_name, postal_code, area_sqkm, population)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return nil, err
		}
		defer insertMuniStmt.Close()

		insertSettlementStmt, err := tx.Prepare(`
			INSERT INTO settlements (id, municipality_id, county_id, name, postal_code, population)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return nil, err
		}
		defer insertSettlementStmt.Close()

		for _, c := range rawCounties {
			_, err := insertCountyStmt.Exec(c.ID, c.Code, c.Name, c.Seat, c.Prefect, c.AreaSqKm, c.Population, c.Email, c.Phone)
			if err != nil {
				return nil, fmt.Errorf("greška pri unosu županije %s: %w", c.Name, err)
			}

			for _, m := range c.Municipalities {
				_, err := insertMuniStmt.Exec(m.ID, m.CountyID, m.Name, m.Type, m.HeadTitle, m.HeadName, m.PostalCode, m.AreaSqKm, m.Population)
				if err != nil {
					return nil, fmt.Errorf("greška pri unosu općine %s: %w", m.Name, err)
				}

				for _, s := range m.Settlements {
					_, err := insertSettlementStmt.Exec(s.ID, s.MunicipalityID, s.CountyID, s.Name, s.PostalCode, s.Population)
					if err != nil {
						return nil, fmt.Errorf("greška pri unosu naselja %s: %w", s.Name, err)
					}
				}
			}
		}

		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	// 6. Početne relacije dionica i teritorijalnih jedinica
	var secTerrCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM section_territories").Scan(&secTerrCount); err == nil && secTerrCount == 0 {
		var rawSecTerrs []struct {
			ID             string `json:"id"`
			SectionCode    string `json:"section_code"`
			CountyID       int    `json:"county_id"`
			MunicipalityID int    `json:"municipality_id"`
			SettlementID   *int   `json:"settlement_id"`
			CreatedAt      string `json:"created_at"`
		}
		raw, err := readDataFile("section_territories.json")
		if err != nil && !errors.Is(err, ErrNoDataFile) {
			return nil, err
		}
		if err := json.Unmarshal(raw, &rawSecTerrs); err == nil && len(rawSecTerrs) > 0 {
			tx, err := database.Begin()
			if err != nil {
				return nil, err
			}
			defer tx.Rollback()

			stmt, err := tx.Prepare(`
				INSERT INTO section_territories (id, section_code, county_id, municipality_id, settlement_id, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`)
			if err != nil {
				return nil, err
			}
			defer stmt.Close()

			now := time.Now()
			for _, st := range rawSecTerrs {
				_, err := stmt.Exec(st.ID, st.SectionCode, st.CountyID, st.MunicipalityID, st.SettlementID, now)
				if err != nil {
					return nil, fmt.Errorf("greška pri unosu veze dionice %s: %w", st.SectionCode, err)
				}
			}

			if err := tx.Commit(); err != nil {
				return nil, err
			}
		}
	}

	// 7. Registar vodomjernih postaja izveden iz vodomjera navedenih na dionicama
	if err := seedStations(database); err != nil {
		return nil, err
	}

	// 8. Registar vodnih tijela
	if err := seedWatercourses(database); err != nil {
		return nil, err
	}

	// 9. Poddionice na registre: vode, postaje, teritorij, nasipi i brane;
	// iz toga se obnavljaju kazala veza
	if err := LinkAllSections(context.Background(), database); err != nil {
		return nil, err
	}

	// 10. Postaje na registar voda: iz naziva, a gdje ga nema, iz dionica
	// kojima je postaja mjerodavna
	if err := linkWatercourses(database); err != nil {
		return nil, err
	}

	// 11. Registar objekata: crpne stanice i ustave, za sad iz evidencije
	// Baranje; vodomjer objekta traži se među postajama dionica područja
	if err := seedStructures(database); err != nil {
		return nil, err
	}

	// 12. Objekti s poddionica na registar objekata, sad kad ga ima
	if err := LinkAllSections(context.Background(), database); err != nil {
		return nil, err
	}

	return lozinke, nil
}

// LoadSections čita prijepis dionica uz bazu (data/sections.json). Služi
// punjenju i popravcima koji postojeće dionice usklađuju s novijim prijepisom.
// Kad datoteke nema, vraća nil bez greške: čvor tada dionice dobiva sinkronizacijom.
func LoadSections() ([]models.Section, error) {
	raw, err := readDataFile("sections.json")
	if errors.Is(err, ErrNoDataFile) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []models.Section
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("greška pri čitanju %s: %w", DataFile("sections.json"), err)
	}
	return out, nil
}

// seedOrganization učita sektore i branjena područja iz data/organizacija.json
// u praznu bazu. Bez datoteke tablice ostaju prazne, a organizaciju upisuje
// globalni administrator u registru Organizacija.
func seedOrganization(database *sql.DB) error {
	var n int
	if err := database.QueryRow("SELECT COUNT(*) FROM sectors").Scan(&n); err != nil || n > 0 {
		return err
	}
	raw, err := readDataFile("organizacija.json")
	if errors.Is(err, ErrNoDataFile) {
		return nil
	}
	if err != nil {
		return err
	}
	var org struct {
		Sectors []models.Sector `json:"sectors"`
		Areas   []models.Area   `json:"areas"`
	}
	if err := json.Unmarshal(raw, &org); err != nil {
		return fmt.Errorf("greška pri čitanju %s: %w", DataFile("organizacija.json"), err)
	}
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, s := range org.Sectors {
		if _, err := tx.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email, level) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING`, s.ID, s.Name, s.VgoName, s.CenterCop, s.Address, s.Phone, s.Email, s.Level); err != nil {
			return fmt.Errorf("sektor %s: %w", s.ID, err)
		}
	}
	for _, a := range org.Areas {
		if _, err := tx.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, contractor_name, direct_to_sector) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING`, a.ID, a.SectorID, a.Name, a.VgiName, a.Subcenter, a.ContractorName, boolInt(a.DirectToSector)); err != nil {
			return fmt.Errorf("branjeno područje %d: %w", a.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("Organizacija: %d sektora i %d branjenih područja iz %s", len(org.Sectors), len(org.Areas), DataFile("organizacija.json"))
	return nil
}
