package ledger

import (
	"context"
	"errors"
	"testing"
)

// Brisanje pojedine verzije (zapis s oglasne ploče) i spomenika: zadnja
// verzija živog zapisa ostaje, starija i spomenik odlaze.
func TestBrisanjeVerzijeISpomenika(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "ured")
	ctx := context.Background()
	prva, _ := rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 600})
	druga, _ := rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 610})
	if _, err := rec.Record(ctx, db, "stations", "st-2", probni{"Dalj", 500}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Archive(ctx, db, "stations", "st-2", probni{"Dalj", 500}); err != nil {
		t.Fatal(err)
	}
	if err := rec.DeleteVersion(ctx, druga); !errors.Is(err, ErrVazecaVerzija) {
		t.Fatalf("zadnja verzija živog zapisa se ne briše: %v", err)
	}
	if err := rec.DeleteVersion(ctx, prva); err != nil {
		t.Fatal(err)
	}
	if err := rec.DeleteVersion(ctx, prva); err == nil {
		t.Error("obrisana verzija više ne postoji")
	}
	if l, _ := rec.Latest(ctx, "stations", "st-1"); l == nil || l.VersionID != druga {
		t.Fatal("zadnja verzija mora ostati")
	}
	n, err := rec.PurgeArchived(ctx)
	if err != nil || n != 1 {
		t.Fatalf("spomenici: %d %v", n, err)
	}
	var ostalo int
	_ = db.QueryRow(`SELECT COUNT(*) FROM record_versions`).Scan(&ostalo)
	if ostalo != 2 {
		t.Errorf("ostaju zadnja verzija st-1 i prva verzija st-2: %d", ostalo)
	}
}
