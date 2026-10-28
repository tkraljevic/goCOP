package repository

import (
	"testing"
	"time"
)

// Popis dionica slaže šifre po brojevima: B.34.2 prije B.34.10
func TestDioniceRedomPoBrojevima(t *testing.T) {
	baza, rec := bazaSPodrucjem(t)
	repo := NewSectionRepository(baza, rec)
	sada := time.Now().UTC()
	for _, kod := range []string{"B.34.10", "B.34.2", "B.34.1", "B.34.19"} {
		if _, err := baza.Exec(`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES (?, 34, 'B', ?, ?, ?)`, kod, "dionica "+kod, sada, sada); err != nil {
			t.Fatal(err)
		}
	}
	dionice, err := repo.ListSections("B", 34, "")
	if err != nil {
		t.Fatal(err)
	}
	var sifre []string
	for _, d := range dionice {
		sifre = append(sifre, d.Code)
	}
	if len(sifre) != 4 || sifre[0] != "B.34.1" || sifre[1] != "B.34.2" || sifre[2] != "B.34.10" || sifre[3] != "B.34.19" {
		t.Errorf("redoslijed dionica %v", sifre)
	}
}
