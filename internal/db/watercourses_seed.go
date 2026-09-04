package db

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"gocop/internal/hydro"
	"gocop/internal/models"
)

//go:embed watercourses.json
var watercoursesJSON []byte

// Registar vodnih tijela dolazi iz Odluke o popisu voda I. reda (NN 79/2010),
// dopunjen enciklopedijskim podacima. Dionice i postaje vežu se na njega po
// nazivu — ali samo kad je poklapanje jednoznačno.
//
// Vode istog imena postoje: "potok Karašica (Baranja)" i "rijeka Karašica
// (miholjačka)" dvije su različite vode. Kad naziv iz dokumentacije odgovara
// objema, veza se ne postavlja, jer bi kriva voda vodila na krive podatke.

type seedWatercourse struct {
	OfficialName string   `json:"official_name"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Category     string   `json:"category"`
	Subcategory  string   `json:"subcategory"`
	WikiSlug     string   `json:"wiki_slug"`
	Origin       string   `json:"origin"`
	LengthKm     *float64 `json:"length_km"`
	BasinKm2     *float64 `json:"basin_km2"`
	AvgFlowM3S   *float64 `json:"avg_flow_m3s"`
	Source       string   `json:"source"`
	Mouth        string   `json:"mouth"`
	FlowsInto    string   `json:"flows_into"`
}

// seedWatercourses puni registar vodnih tijela i veže dionice i postaje na njega
func seedWatercourses(database *sql.DB) error {
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM watercourses").Scan(&count); err != nil {
		return err
	}

	if count == 0 {
		var waters []seedWatercourse
		if err := json.Unmarshal(watercoursesJSON, &waters); err != nil {
			return fmt.Errorf("greška pri čitanju watercourses.json: %w", err)
		}

		tx, err := database.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		stmt, err := tx.Prepare(`
			INSERT INTO watercourses (
				code, official_name, name, kind, category, subcategory, wiki_slug, origin,
				length_km, basin_km2, avg_flow_m3s, source, mouth, flows_into
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(code) DO NOTHING
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, w := range waters {
			_, err := stmt.Exec(
				hydro.WatercourseCode(w.OfficialName), w.OfficialName, w.Name, w.Kind,
				w.Category, w.Subcategory, w.WikiSlug, w.Origin,
				w.LengthKm, w.BasinKm2, w.AvgFlowM3S, w.Source, w.Mouth, w.FlowsInto,
			)
			if err != nil {
				return fmt.Errorf("greška pri unosu vodnog tijela %q: %w", w.OfficialName, err)
			}
		}

		if err := tx.Commit(); err != nil {
			return err
		}
		log.Printf("Registar vodnih tijela: %d voda", len(waters))
	}

	return linkWatercourses(database)
}

// watercourseIndex gradi kazalo naziv → sve vode tog imena
func watercourseIndex(database *sql.DB) (map[string][]hydro.Candidate, error) {
	rows, err := database.Query(`SELECT code, name, official_name, kind FROM watercourses`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	index := map[string][]hydro.Candidate{}
	for rows.Next() {
		var code, name, official, kind string
		if err := rows.Scan(&code, &name, &official, &kind); err != nil {
			return nil, err
		}

		qualifier := hydro.Qualifier(official)

		seen := map[string]bool{}
		for _, candidateName := range []string{name, official} {
			key := hydro.WatercourseKey(candidateName)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true

			duplicate := false
			for _, c := range index[key] {
				if c.Code == code {
					duplicate = true
				}
			}
			if !duplicate {
				index[key] = append(index[key], hydro.Candidate{Code: code, Kind: kind, Qualifier: qualifier})
			}
		}
	}

	return index, rows.Err()
}

// linkWatercourses veže dionice i postaje na registar vodnih tijela.
//
// Vode koje Odluka o popisu voda I. reda ne navodi — mali potoci, kanali i
// retencije — upisuju se u registar iz dokumentacije dionica. Nisu I. reda, ali
// postoje i štite se, pa dionica ne smije ostati bez svoje vode.
func linkWatercourses(database *sql.DB) error {
	index, err := watercourseIndex(database)
	if err != nil {
		return err
	}

	type sectionRow struct{ code, desc, areaText string }
	var sections []sectionRow

	rows, err := database.Query(`
		SELECT s.code, s.description,
		       COALESCE(a.name, '') || ' ' || COALESCE(a.vgi_name, '') || ' ' || COALESCE(a.subcenter, '')
		FROM sections s
		LEFT JOIN areas a ON a.id = s.area_id
		WHERE s.watercourse_code = ''
	`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r sectionRow
		if err := rows.Scan(&r.code, &r.desc, &r.areaText); err != nil {
			rows.Close()
			return err
		}
		sections = append(sections, r)
	}
	rows.Close()

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insertWater, err := tx.Prepare(`
		INSERT INTO watercourses (code, official_name, name, kind, category, subcategory, wiki_slug, origin)
		VALUES (?, ?, ?, ?, '', '', '', ?)
		ON CONFLICT(code) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer insertWater.Close()

	linkedSections, addedWaters := 0, 0

	for _, sec := range sections {
		name, kind := hydro.ParseWatercourseWithKind(sec.desc)
		if strings.TrimSpace(name) == "" {
			continue
		}

		code := hydro.ResolveWatercourse(index, name, kind, sec.areaText)

		if code == "" && len(index[hydro.WatercourseKey(name)]) == 0 {
			// Voda postoji u dokumentaciji, ali ne i u Odluci — upiši je
			official := strings.TrimSpace(kind + " " + name)
			code = hydro.WatercourseCode(official)
			if _, err := insertWater.Exec(code, official, name, kind, models.WatercourseOriginDocumentation); err != nil {
				return fmt.Errorf("greška pri unosu vodnog tijela %q iz dokumentacije: %w", official, err)
			}
			index[hydro.WatercourseKey(name)] = []hydro.Candidate{{Code: code, Kind: kind}}
			addedWaters++
		}

		if code == "" {
			continue
		}
		if _, err := tx.Exec(`UPDATE sections SET watercourse_code = ? WHERE code = ?`, code, sec.code); err != nil {
			return fmt.Errorf("greška pri vezanju dionice %s: %w", sec.code, err)
		}
		linkedSections++
	}

	// Postaje — vodotok postaje utvrđen je ranije, iz naziva ili stacionaže.
	// Branjeno područje postaje uzima se s dionica kojima je mjerodavna.
	stationRows, err := tx.Query(`
		SELECT st.id, st.watercourse,
		       COALESCE(GROUP_CONCAT(DISTINCT a.name), '')
		FROM stations st
		LEFT JOIN section_stations ss ON ss.station_id = st.id
		LEFT JOIN sections s ON s.code = ss.section_code
		LEFT JOIN areas a ON a.id = s.area_id
		WHERE st.watercourse <> '' AND st.watercourse_code = ''
		GROUP BY st.id
	`)
	if err != nil {
		return err
	}
	type stationLink struct{ id, code string }
	var stationLinks []stationLink
	for stationRows.Next() {
		var id, name, areaText string
		if err := stationRows.Scan(&id, &name, &areaText); err != nil {
			stationRows.Close()
			return err
		}
		if code := hydro.ResolveWatercourse(index, name, "", areaText); code != "" {
			stationLinks = append(stationLinks, stationLink{id, code})
		}
	}
	stationRows.Close()

	for _, l := range stationLinks {
		if _, err := tx.Exec(`UPDATE stations SET watercourse_code = ? WHERE id = ?`, l.code, l.id); err != nil {
			return fmt.Errorf("greška pri vezanju postaje %s: %w", l.id, err)
		}
	}

	// Postaje kojima vodotok nije utvrđen iz naziva ni stacionaže: kad su SVE
	// dionice kojima je postaja mjerodavna na istoj vodi, postaja stoji na toj
	// vodi. Batina ovim pravilom ne prolazi — mjerodavna je i za Karašicu i za
	// Dunav — pa se lokacija ne izvodi iz jedne vrste dionica.
	inferred, err := tx.Exec(`
		UPDATE stations SET
			watercourse_code = (
				SELECT MIN(s.watercourse_code) FROM section_stations ss
				JOIN sections s ON s.code = ss.section_code
				WHERE ss.station_id = stations.id AND s.watercourse_code <> ''
			),
			watercourse = (
				SELECT MIN(w.name) FROM section_stations ss
				JOIN sections s ON s.code = ss.section_code
				JOIN watercourses w ON w.code = s.watercourse_code
				WHERE ss.station_id = stations.id AND s.watercourse_code <> ''
			),
			watercourse_source = ?
		WHERE watercourse = ''
		  AND EXISTS (
			SELECT 1 FROM section_stations ss JOIN sections s ON s.code = ss.section_code
			WHERE ss.station_id = stations.id AND s.watercourse_code <> ''
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM section_stations ss JOIN sections s ON s.code = ss.section_code
			WHERE ss.station_id = stations.id AND s.watercourse_code <> ''
			GROUP BY ss.station_id HAVING COUNT(DISTINCT s.watercourse_code) > 1
		  )
	`, models.WatercourseFromSections)
	if err != nil {
		return fmt.Errorf("greška pri izvođenju vodotoka postaja iz dionica: %w", err)
	}
	inferredCount, _ := inferred.RowsAffected()

	if err := tx.Commit(); err != nil {
		return err
	}

	if linkedSections+len(stationLinks)+addedWaters+int(inferredCount) > 0 {
		log.Printf("Vezano na registar vodnih tijela: %d dionica, %d postaja izravno + %d izvedeno iz dionica (%d voda dodano iz dokumentacije)",
			linkedSections, len(stationLinks), inferredCount, addedWaters)
	}

	return nil
}
