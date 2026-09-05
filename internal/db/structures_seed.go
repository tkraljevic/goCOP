package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// Objekti iz evidencije VGI Baranja (crpne stanice i ustave s kotom nule,
// kapacitetom i vodostajima pogona). Prvo punjenje registra objekata; ostala
// područja se pune iz dokumentacije i ručno.
type seedStructure struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	SectorID     string   `json:"sector_id"`
	AreaID       int      `json:"area_id"`
	ZeroDatum    *float64 `json:"zero_datum"`
	CapacityText string   `json:"capacity_text"`
	StartCm      *int     `json:"start_cm"`
	StartText    string   `json:"start_text"`
	StopCm       *int     `json:"stop_cm"`
	StopText     string   `json:"stop_text"`
	Origin       string   `json:"origin"`
}

// seedStructures upisuje objekte koji još ne postoje i veže ih na vodomjere
// i dionice istog područja po nazivu. Ne piše verzije: sjeme je isto na
// svakom čvoru (identitet iz šifre), pa nema što putovati.
func seedStructures(database *sql.DB) error {
	raw, err := readDataFile("objekti_bp16.json")
	if errors.Is(err, ErrNoDataFile) {
		return nil
	}
	if err != nil {
		return err
	}
	var items []seedStructure
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("greška pri čitanju objekti_bp16.json: %w", err)
	}

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	added, linkedStations := 0, 0
	for _, it := range items {
		id := StableID("structure", it.Code).String()

		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM structures WHERE id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		system := ""
		if it.ZeroDatum != nil {
			system = "TRST"
		}
		stationID := matchStation(tx, it)
		if stationID != "" {
			linkedStations++
		}
		if _, err := tx.Exec(`INSERT INTO structures (id, code, name, kind, sector_id, area_id, watercourse_code, station_id,
			zero_datum, zero_datum_system, capacity_text, start_cm, start_text, stop_cm, stop_text, notes, origin,
			latitude, longitude, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?, '', ?, NULL, NULL, ?, ?)`,
			id, it.Code, it.Name, it.Kind, it.SectorID, it.AreaID, stationID,
			it.ZeroDatum, system, it.CapacityText, it.StartCm, it.StartText, it.StopCm, it.StopText, it.Origin, now, now); err != nil {
			return fmt.Errorf("greška pri upisu objekta %s: %w", it.Name, err)
		}
		added++

	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if added > 0 {
		log.Printf("Registar objekata: %d objekata (BP16), %d s vodomjerom", added, linkedStations)
	}
	return nil
}

// matchStation traži vodomjer istog naziva među postajama mjerodavnim za
// dionice istog područja: "CS Draž" ↔ "CS Draž", "Ustava Zmajevac" ↔
// "CS i ustava Zmajevac"
func matchStation(tx *sql.Tx, it seedStructure) string {
	rows, err := tx.Query(`SELECT DISTINCT st.id, st.name FROM stations st
		JOIN section_stations ss ON ss.station_id = st.id
		JOIN sections s ON s.code = ss.section_code WHERE s.area_id = ?`, it.AreaID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	want := normalizeName(it.Name)
	base := structureBaseName(it.Name)
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) != nil {
			continue
		}
		got := normalizeName(name)
		if got == want || got == "cs i ustava "+base {
			return id
		}
	}
	return ""
}

func normalizeName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// structureBaseName skida vrstu s početka naziva: "Ustava Draž" → "draž"
func structureBaseName(name string) string {
	n := normalizeName(name)
	for _, prefix := range []string{"cs ", "ustava ", "crpna stanica ", "sifon "} {
		if strings.HasPrefix(n, prefix) {
			return strings.TrimPrefix(n, prefix)
		}
	}
	return n
}
