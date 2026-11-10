package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/models"
)

// Uvoz očitanja dobiva krivulje letve iz arhive; bez arhive javi grešku, a
// ne pada.
func TestKrivuljeLetvi(t *testing.T) {
	put := filepath.Join(t.TempDir(), "arhiva.db")
	if _, err := krivuljeLetvi(put)(context.Background(), "botovo"); err == nil {
		t.Error("bez arhive mora biti greška")
	}

	a, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, q := range []string{
		`CREATE TABLE hq_krivulje (id INTEGER PRIMARY KEY, letva TEXT, vrijedi_od TEXT, vrijedi_do TEXT)`,
		`CREATE TABLE hq_odsjecci (krivulja INTEGER, od_cm INTEGER, do_cm INTEGER, oblik TEXT, p1 REAL, p2 REAL, p3 REAL, p4 REAL)`,
		`INSERT INTO hq_krivulje VALUES (1, 'botovo', '2026-01-01', '')`,
		`INSERT INTO hq_odsjecci VALUES (1, 0, 300, 'polinom', 0, 100, 50, 0)`,
	} {
		if _, err := a.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	krivulje, err := krivuljeLetvi(put)(context.Background(), "botovo")
	if err != nil || len(krivulje) != 1 {
		t.Fatalf("krivulje %+v, greška %v", krivulje, err)
	}
	if q, ok := krivulje[0].Protok(100); !ok || q != 150 {
		t.Errorf("protok pri 100 cm: %v %v", q, ok)
	}
	if k := models.KrivuljaUTrenutku(krivulje, time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)); k == nil {
		t.Error("krivulja mora vrijediti")
	}
}
