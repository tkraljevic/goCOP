package db

import (
	"context"
	"path/filepath"
	"testing"
)

// Zaključava da glavna baza piše tako da potvrđen upis preživi nestanak
// struje: svaka veza, ne samo prva, ima synchronous FULL (2). Uz NORMAL
// ovjera akta mogla je nestati, a sljedeća ovjera dati isti broj drugom aktu.
func TestGlavnaBazaPiseTrajno(t *testing.T) {
	baza, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	ctx := context.Background()
	for i := range 2 {
		veza, err := baza.Conn(ctx)
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
