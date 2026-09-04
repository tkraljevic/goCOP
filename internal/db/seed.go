package db

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Imenik djelatnika NIJE ugrađen u program: nosi imena, telefone i adrese
// e-pošte zaposlenih, a kod i binarna datoteka su javni. Datoteka stoji uz
// bazu (data/imenik.json), izvan repozitorija, i čita se samo pri prvom
// punjenju. Ostali čvorovi djelatnike dobivaju sinkronizacijom, pa imenik
// treba jedino prvom čvoru u mreži.
var ImenikPath = "data/imenik.json"

// UseRepoImenik traži imenik u mapi data/ repozitorija (za testove koji
// rade na stvarnim ljudima) i javlja je li nađen
func UseRepoImenik() bool {
	for _, up := range []string{"", "..", "../..", "../../.."} {
		candidate := filepath.Join(up, "data", "imenik.json")
		if _, err := os.Stat(candidate); err == nil {
			ImenikPath = candidate
			return true
		}
	}
	return false
}

//go:embed sections.json
var sectionsJSON []byte

//go:embed territories.json
var territoriesJSON []byte

//go:embed section_territories.json
var sectionTerritoriesJSON []byte

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
	Email         string     `json:"email"`
	IsGlobalAdmin bool       `json:"is_global_admin"`
	Duties        []seedDuty `json:"duties"`
}

// SeedInitialData unosi sektore, branjena područja, stvarne voditelje COP-ova i dužnosti
func SeedInitialData(database *sql.DB) error {
	// 1. Sektori i Direkcija
	sectors := []struct {
		id, name, vgoName, centerCop, address, phone, email string
	}{
		{"DIREKCIJA", "Direkcija Zagreb (za područje čitave RH)", "Hrvatske vode, Direkcija Zagreb", "Glavni centar obrane od poplava (GCOP)", "Ulica grada Vukovara 220, 10000 Zagreb", "01/6151-778", "GCOPRH@voda.hr"},
		{"A", "Sektor A — Mura i gornja Drava", "VGO za Muru i gornju Dravu, Varaždin", "COP Varaždin", "Međimurska 26 b, 42000 Varaždin", "042/404-000", "COP.A@voda.hr"},
		{"B", "Sektor B — Dunav i donja Drava", "VGO za Dunav i donju Dravu, Osijek", "COP Osijek", "Splavarska 2a, 31000 Osijek", "031/252-802", "copos@voda.hr"},
		{"C", "Sektor C — Gornja Sava", "VGO za gornju Savu, Zagreb", "COP Zagreb", "Terenski ured Hruščica, Savska 100", "01/2773-002", "GCOPRH@voda.hr"},
		{"D", "Sektor D — Srednja i donja Sava", "VGO za srednju i donju Savu, Slavonski Brod", "COP Slavonski Brod", "Šetalište braće Radića 22, Slavonski Brod", "035/386-304", "copsb@voda.hr"},
		{"E", "Sektor E — Sjeverni Jadran", "VGO za slivove sjevernog Jadrana, Rijeka", "COP Rijeka", "Đure Šporera 3, 51000 Rijeka", "051/317-018", "COP.E@voda.hr"},
		{"F", "Sektor F — Južni Jadran", "VGO za slivove južnog Jadrana, Split", "COP Split", "Vukovarska 35, 21000 Split", "021/309-477", "copst@voda.hr"},
	}

	for _, s := range sectors {
		_, err := database.Exec(`
			INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				vgo_name = excluded.vgo_name,
				center_cop = excluded.center_cop,
				address = excluded.address,
				phone = excluded.phone,
				email = excluded.email;
		`, s.id, s.name, s.vgoName, s.centerCop, s.address, s.phone, s.email)
		if err != nil {
			return fmt.Errorf("greška pri unosu sektora %s: %w", s.id, err)
		}
	}

	// 2. Branjena područja 1-34 s ugovornim pravnim osobama
	areas := []struct {
		id         int
		sectorID   string
		name       string
		vgiName    string
		subcenter  string
		contractor string
	}{
		// Sektor D
		{1, "D", "Mali sliv Biđ-Bosut", "VGI Biđ-Bosut, Vinkovci", "Podcentar Vinkovci", "Vinkovački vodovod i kanalizacija d.o.o."},
		{2, "D", "Mali sliv Brodska Posavina", "VGI Brodska Posavina, Slavonski Brod", "Podcentar Slavonski Brod", "Brodska Posavina d.d."},
		{3, "D", "Mali sliv Orljava-Londža", "VGI Orljava-Londža, Požega", "Podcentar Požega", "Presoflex gradnja d.o.o."},
		{4, "D", "Mali sliv Šumetlica-Crnac", "VGI Šumetlica-Crnac, Nova Gradiška", "Podcentar Nova Gradiška", "Vodoprivreda Nova Gradiška d.d."},
		{5, "D", "Mali sliv Subocka-Strug", "VGI Subocka-Strug, Novska", "Podcentar Novska", "Vodoprivreda Novska d.o.o."},
		{6, "D", "Mali sliv Ilova-Pakra", "VGI Ilova-Pakra, Daruvar", "Podcentar Daruvar", "Vodoprivreda Daruvar d.d."},
		{7, "D", "Mali sliv Česma-Glogovnica", "VGI Česma-Glogovnica, Bjelovar", "Podcentar Bjelovar", "Vodoprivreda Čazma d.d."},
		{9, "D", "Mali sliv Lonja-Trebež", "VGI Lonja-Trebež, Kutina", "Podcentar Kutina", "Vodoprivreda Lonja-Zelina d.d."},
		{10, "D", "Mali sliv Banovina", "VGI Banovina, Sisak", "Podcentar Sisak", "Vodoprivreda Sisak d.d."},
		{11, "D", "Mali sliv Kupa", "VGI Kupa, Karlovac", "Podcentar Karlovac", "Vodoprivreda Karlovac d.d."},
		// Sektor C
		{8, "C", "Mali sliv Zelina-Lonja", "VGI Zelina-Lonja, Dugo Selo", "Podcentar Dugo Selo", "Vodoprivreda Zagreb d.d."},
		{12, "C", "Mali sliv Krapina-Sutla", "VGI Krapina-Sutla, Zabok", "Podcentar Zabok", "Vodoprivreda Zagorje d.o.o."},
		{13, "C", "Zagrebačko Prisavlje — južni dio", "VGI Zagreb", "Podcentar Zagreb Jug", "Vodoprivreda Zagreb d.d."},
		{14, "C", "Zagrebačko Prisavlje — središnji dio", "VGI Zagreb", "Podcentar Zagreb Centar", "Vodoprivreda Zagreb d.d."},
		// Sektor B
		{15, "B", "Mali sliv Vuka", "VGI Vuka, Osijek", "Podcentar Osijek", "Vodogradnja d.d. Osijek"},
		{16, "B", "Mali sliv Baranja", "VGI Baranja, Darda", "Podcentar Darda", "Vodogradnja d.d. Osijek"},
		{17, "B", "Mali sliv Karašica-Vučica", "VGI Karašica-Vučica, Donji Miholjac", "Podcentar Donji Miholjac", "Karašica-Vučica d.d."},
		{18, "B", "Mali sliv Županijski kanal", "VGI Županijski kanal, Virovitica", "Podcentar Virovitica", "Karašica-Vučica d.d."},
		{34, "B", "Međudržavne rijeke Drava i Dunav", "VGO Osijek", "COP Osijek", "Vodogradnja d.d. Osijek"},
		// Sektor A
		{19, "A", "Mali sliv Bistra", "VGI Bistra, Đurđevac", "Podcentar Đurđevac", "Bistra d.o.o. Đurđevac"},
		{20, "A", "Mali sliv Plitvica-Bednja", "VGI Plitvica-Bednja, Varaždin", "Podcentar Varaždin", "Hidroing d.d. Varaždin"},
		{21, "A", "Mali sliv Trnava", "VGI Trnava, Čakovec", "Podcentar Čakovec", "Tegra d.o.o. Čakovec"},
		{33, "A", "Međudržavne rijeke Mura i Drava", "VGO Varaždin", "COP Varaždin", "Hidroing d.d. Varaždin"},
		// Sektor E
		{22, "E", "Mali slivovi Mirna-Dragonja i Raša-Boljunčica", "VGI Istra, Pula", "Podcentar Pula", "Vodoprivreda Pula d.o.o."},
		{23, "E", "Kvarnersko primorje i otoci", "VGI Rijeka", "Podcentar Rijeka", "Vodogradnja Rijeka d.o.o."},
		{24, "E", "Mali sliv Gorski kotar", "VGI Gorski kotar, Delnice", "Podcentar Delnice", "Vodogradnja Rijeka d.o.o."},
		{25, "E", "Mali sliv Lika", "VGI Lika, Gospić", "Podcentar Gospić", "Vodoprivreda Gospić d.o.o."},
		// Sektor F
		{26, "F", "Zrmanja - Zadarsko primorje", "VGI Zadar", "Podcentar Zadar", "Vodoinstalacija Zadar d.o.o."},
		{27, "F", "Krka - Šibensko primorje", "VGI Šibenik", "Podcentar Šibenik", "Vodoprivreda Šibenik d.o.o."},
		{28, "F", "Mali sliv Cetina", "VGI Cetina, Sinj", "Podcentar Sinj", "Vodoprivreda Split d.d."},
		{29, "F", "Srednjodalmatinsko primorje i otoci", "VGI Split", "Podcentar Split", "Vodoprivreda Split d.d."},
		{30, "F", "Mali sliv Matica", "VGI Vrgorac", "Podcentar Vrgorac", "Vodoprivreda Vrgorac d.o.o."},
		{31, "F", "Mali sliv Vrljika", "VGI Imotski", "Podcentar Imotski", "Vodoprivreda Vrgorac d.o.o."},
		{32, "F", "Neretva - Korčula i Dubrovačko primorje", "VGI Opuzen", "Podcentar Opuzen", "Neretvanski sliv d.o.o."},
	}

	for _, a := range areas {
		_, err := database.Exec(`
			INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, contractor_name)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				sector_id = excluded.sector_id,
				name = excluded.name,
				vgi_name = excluded.vgi_name,
				subcenter = excluded.subcenter,
				contractor_name = excluded.contractor_name;
		`, a.id, a.sectorID, a.name, a.vgiName, a.subcenter, a.contractor)
		if err != nil {
			return fmt.Errorf("greška pri unosu branjenog područja %d: %w", a.id, err)
		}
	}

	// 3. Početni korisnici s višestrukim funkcijama
	var userCount int
	err := database.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	if err != nil {
		return err
	}

	if userCount == 0 {
		defaultPw := "gocop2026"
		pwHash, err := bcrypt.GenerateFromPassword([]byte(defaultPw), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		hashStr := string(pwHash)
		now := time.Now().UTC()

		var users []seedUser
		if raw, err := os.ReadFile(ImenikPath); err == nil {
			if err := json.Unmarshal(raw, &users); err != nil {
				return fmt.Errorf("greška pri čitanju %s: %w", ImenikPath, err)
			}
			log.Printf("Imenik: %d djelatnika iz %s", len(users), ImenikPath)
		} else {
			log.Printf("Imenik %s nije nađen — stvara se samo račun admin; djelatnici stižu sinkronizacijom ili uvozom", ImenikPath)
		}

		tx, err := database.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		insertUserStmt, err := tx.Prepare(`
			INSERT INTO users (
				id, username, password_hash, full_name, title, is_global_admin,
				must_change_password, org_type, org_name, phone, mobile_phone, short_phone, email,
				is_active, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer insertUserStmt.Close()

		insertDutyStmt, err := tx.Prepare(`
			INSERT INTO duties (
				id, user_id, title, role, scope_type, sector_id, area_id, section_codes,
				is_primary, is_temporary, is_active, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 1, ?)
		`)
		if err != nil {
			return err
		}
		defer insertDutyStmt.Close()

		userIDs := make(map[string]string)

		for _, u := range users {
			// isti korisnik na svakom čvoru — identitet slijedi iz korisničkog imena
			uIDStr := StableID("user", u.Username).String()
			userIDs[u.Username] = uIDStr

			adminFlag := 0
			if u.IsGlobalAdmin {
				adminFlag = 1
			}

			_, err = insertUserStmt.Exec(
				uIDStr, u.Username, hashStr, u.FullName, u.Title, adminFlag,
				u.OrgType, u.OrgName, u.Phone, u.MobilePhone, u.ShortPhone, u.Email,
				now, now,
			)
			if err != nil {
				return fmt.Errorf("greška pri unosu korisnika %s: %w", u.Username, err)
			}

			for i, d := range u.Duties {
				dID := StableID("duty", fmt.Sprintf("%s|%d", u.Username, i))
				isPrimaryInt := 0
				if d.IsPrimary {
					isPrimaryInt = 1
				}

				_, err = insertDutyStmt.Exec(
					dID.String(), uIDStr, d.Title, d.Role, d.ScopeType, d.SectorID, d.AreaID, d.SectionCodes, isPrimaryInt, now,
				)
				if err != nil {
					return fmt.Errorf("greška pri unosu dužnosti '%s' za %s: %w", d.Title, u.Username, err)
				}
			}
		}

		// System admin alias 'admin'
		if _, ok := userIDs["admin"]; !ok {
			adminAliasID := StableID("user", "admin")
			_, _ = insertUserStmt.Exec(
				adminAliasID.String(), "admin", hashStr, "Administrator Sustava", "admin", 1,
				"HRVATSKE_VODE", "Centar obrane od poplava",
				"031/252-802", "", "", "copos@voda.hr",
				now, now,
			)
			dAdmin := StableID("duty", "admin|0")
			_, _ = insertDutyStmt.Exec(
				dAdmin.String(), adminAliasID.String(), "Glavni administrator COP-a", "GLOBAL_ADMIN", "ALL", "DIREKCIJA", nil, "", 1, now,
			)
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	// 4. Početne štićene dionice (465 dionica iz teritorijalnih jedinica)
	var sectionCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM sections").Scan(&sectionCount); err == nil && sectionCount == 0 {
		var rawSections []struct {
			Code          string `json:"code"`
			AreaID        int    `json:"area_id"`
			SectorID      string `json:"sector_id"`
			Watercourse   string `json:"watercourse"`
			ProtectedArea string `json:"protected_area"`
			Embankments   any    `json:"embankments"`
			Structures    any    `json:"structures"`
			Gauges        any    `json:"gauges"`
			Notes         string `json:"notes"`
		}
		if err := json.Unmarshal(sectionsJSON, &rawSections); err != nil {
			return fmt.Errorf("greška pri čitanju sections.json: %w", err)
		}

		tx, err := database.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		insertSecStmt, err := tx.Prepare(`
			INSERT INTO sections (
				code, area_id, sector_id, description, protected_area,
				embankments, structures, gauges, notes, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer insertSecStmt.Close()

		now := time.Now().UTC().Format(time.RFC3339)
		for _, s := range rawSections {
			embJSON, _ := json.Marshal(s.Embankments)
			strJSON, _ := json.Marshal(s.Structures)
			gagJSON, _ := json.Marshal(s.Gauges)

			_, err = insertSecStmt.Exec(
				s.Code, s.AreaID, s.SectorID, s.Watercourse, s.ProtectedArea,
				string(embJSON), string(strJSON), string(gagJSON), s.Notes, now, now,
			)
			if err != nil {
				return fmt.Errorf("greška pri unosu dionice %s: %w", s.Code, err)
			}
		}

		if err := tx.Commit(); err != nil {
			return err
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
		if err := json.Unmarshal(territoriesJSON, &rawCounties); err != nil {
			return fmt.Errorf("greška pri čitanju territories.json: %w", err)
		}

		tx, err := database.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		insertCountyStmt, err := tx.Prepare(`
			INSERT INTO counties (id, code, name, seat, prefect, area_sqkm, population, email, phone)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer insertCountyStmt.Close()

		insertMuniStmt, err := tx.Prepare(`
			INSERT INTO municipalities (id, county_id, name, type, head_title, head_name, postal_code, area_sqkm, population)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer insertMuniStmt.Close()

		insertSettlementStmt, err := tx.Prepare(`
			INSERT INTO settlements (id, municipality_id, county_id, name, postal_code, population)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer insertSettlementStmt.Close()

		for _, c := range rawCounties {
			_, err := insertCountyStmt.Exec(c.ID, c.Code, c.Name, c.Seat, c.Prefect, c.AreaSqKm, c.Population, c.Email, c.Phone)
			if err != nil {
				return fmt.Errorf("greška pri unosu županije %s: %w", c.Name, err)
			}

			for _, m := range c.Municipalities {
				_, err := insertMuniStmt.Exec(m.ID, m.CountyID, m.Name, m.Type, m.HeadTitle, m.HeadName, m.PostalCode, m.AreaSqKm, m.Population)
				if err != nil {
					return fmt.Errorf("greška pri unosu općine %s: %w", m.Name, err)
				}

				for _, s := range m.Settlements {
					_, err := insertSettlementStmt.Exec(s.ID, s.MunicipalityID, s.CountyID, s.Name, s.PostalCode, s.Population)
					if err != nil {
						return fmt.Errorf("greška pri unosu naselja %s: %w", s.Name, err)
					}
				}
			}
		}

		if err := tx.Commit(); err != nil {
			return err
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
		if err := json.Unmarshal(sectionTerritoriesJSON, &rawSecTerrs); err == nil && len(rawSecTerrs) > 0 {
			tx, err := database.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()

			stmt, err := tx.Prepare(`
				INSERT INTO section_territories (id, section_code, county_id, municipality_id, settlement_id, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`)
			if err != nil {
				return err
			}
			defer stmt.Close()

			now := time.Now()
			for _, st := range rawSecTerrs {
				_, err := stmt.Exec(st.ID, st.SectionCode, st.CountyID, st.MunicipalityID, st.SettlementID, now)
				if err != nil {
					return fmt.Errorf("greška pri unosu veze dionice %s: %w", st.SectionCode, err)
				}
			}

			if err := tx.Commit(); err != nil {
				return err
			}
		}
	}

	// 7. Registar vodomjernih postaja izveden iz vodomjera navedenih na dionicama
	if err := seedStations(database); err != nil {
		return err
	}

	// 8. Registar vodnih tijela i veze dionica i postaja na njega
	if err := seedWatercourses(database); err != nil {
		return err
	}

	// 9. Obala i raspon stacionaže dionica, pročitani iz opisa
	if err := structureSections(database); err != nil {
		return err
	}

	// 10. Registar objekata: crpne stanice i ustave, za sad iz evidencije Baranje
	if err := seedStructures(database); err != nil {
		return err
	}

	return nil
}
