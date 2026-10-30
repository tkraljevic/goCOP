package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// OpenDB otvara SQLite bazu u čistom Go načinu rada (modernc.org/sqlite) s optimalnim postavkama (WAL mod)
func OpenDB(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("greška pri kreiranju direktorija za bazu: %w", err)
		}
	}

	// SQLite DSN s pragmama: WAL mod, busy timeout 5s, uključeni foreign keys
	// i trajni upis: potvrđena ovjera akta ne smije nestati kad nestane struje;
	// transakcija traži pravo pisanja na početku (UpisOdPocetka)
	dsn := ZaPisanje(dbPath, "_pragma=journal_mode(WAL)", "_pragma=foreign_keys(ON)")

	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("greška pri otvaranju SQLite baze: %w", err)
	}

	// Ograničenje na odgovarajući pool konekcija
	database.SetMaxOpenConns(25)
	database.SetMaxIdleConns(5)

	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("greška pri spajanju na SQLite bazu: %w", err)
	}

	return database, nil
}
