package db

import (
	"database/sql"
	"fmt"
	"log"

	"gocop/internal/hydro"
)

// structureSections popunjava obalu i raspon stacionaže dionica iz njihova
// opisa — samo tamo gdje još nisu upisani, pa je bezopasno pokretati pri
// svakom startu i na već postojećim bazama.
//
// Opis ostaje netaknut; kad operater ručno ispravi obalu ili raspon, sljedeći
// start ih ne prepisuje.
func structureSections(database *sql.DB) error {
	rows, err := database.Query(`
		SELECT code, description FROM sections
		WHERE bank = '' AND rkm_from IS NULL
	`)
	if err != nil {
		return err
	}

	type update struct {
		code string
		desc hydro.SectionDescription
	}
	var updates []update
	for rows.Next() {
		var code, desc string
		if err := rows.Scan(&code, &desc); err != nil {
			rows.Close()
			return err
		}
		parsed := hydro.ParseSectionDescription(desc)
		if parsed.Bank == "" && !parsed.HasRange {
			continue
		}
		updates = append(updates, update{code, parsed})
	}
	rows.Close()

	if len(updates) == 0 {
		return nil
	}

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	withBank, withRange := 0, 0
	for _, u := range updates {
		var from, to any
		if u.desc.HasRange {
			from, to = u.desc.RkmFrom, u.desc.RkmTo
			withRange++
		}
		if u.desc.Bank != "" {
			withBank++
		}
		if _, err := tx.Exec(`UPDATE sections SET bank = ?, rkm_from = ?, rkm_to = ? WHERE code = ?`,
			u.desc.Bank, from, to, u.code); err != nil {
			return fmt.Errorf("greška pri strukturiranju dionice %s: %w", u.code, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Printf("Dionice strukturirane iz opisa: %d s obalom, %d s rasponom stacionaže (od %d obrađenih)",
		withBank, withRange, len(updates))
	return nil
}
