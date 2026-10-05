package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Dionice akta: letva bez dionica nije mjerodavna, a greška čitanja
// registra dionica vraća se, ne preskače dionicu
func TestDioniceAktaGreske(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "dionice.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	s := &AktService{stations: repository.NewStationRepository(baza, rec), sections: repository.NewSectionRepository(baza, rec)}
	ctx := context.Background()
	if _, err := s.dioniceAkta(ctx, &models.Station{ID: uuid.New(), Name: "Primjerovo"}, nil); err == nil || !strings.Contains(err.Error(), "nije mjerodavan") {
		t.Errorf("letva bez dionica: %v", err)
	}
	if _, err := baza.Exec(`ALTER TABLE sections RENAME TO nema_dionica`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dioniceAkta(ctx, &models.Station{ID: uuid.New(), Name: "Primjerovo", SectionCodes: []string{"P.1.1"}}, nil); err == nil || !strings.Contains(err.Error(), "P.1.1") {
		t.Errorf("greška čitanja registra dionica: %v", err)
	}
}
