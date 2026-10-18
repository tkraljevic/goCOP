package sadrzaj

import (
	"context"
	"path/filepath"
	"testing"
)

// Zaključava da baza dokumenata piše tako da potvrđen upis preživi nestanak
// struje: svaka veza ima synchronous FULL (2). Uz NORMAL je glavna baza nakon
// nestanka struje mogla zadržati ovjeru skenom, a sam sken nestati.
func TestBazaDokumenataPiseTrajno(t *testing.T) {
	s, err := Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	for i := range 2 {
		veza, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer veza.Close()
		var synchronous int
		if err := veza.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&synchronous); err != nil {
			t.Fatal(err)
		}
		if synchronous != 2 {
			t.Errorf("%d. veza: synchronous %d, treba 2 (FULL)", i+1, synchronous)
		}
	}
}
