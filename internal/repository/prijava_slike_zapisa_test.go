package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Ista oznaka fotografije stoji na dvije prijave (npr. prijava stigla s
// drugog računala s istim opisom fotografije). Brisanje fotografije jedne
// prijave ne dira fotografiju druge, a rok čuvanja briše po prijavi.
func TestFotografijaSeBriseSamoSPrijave(t *testing.T) {
	dir := t.TempDir()
	baza, err := db.BazaIzPredloska(filepath.Join(dir, "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	sp, err := sadrzaj.Otvori(filepath.Join(dir, "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sp.Zatvori() //nolint:errcheck // privremeno spremište testa
	prijasnje := Spremiste()
	SetSpremiste(sp)
	defer SetSpremiste(prijasnje)
	ctx := context.Background()
	repo := NewPrijavaRepository(baza, ledger.New(baza, "cvor"))

	jpg := []byte("fotografija prve prijave")
	staro := time.Now().AddDate(0, 0, -400)
	spremi := func(id string, objavljeno *time.Time) {
		t.Helper()
		p := &models.PrijavaSTerena{ID: id, UserID: "u-" + id, Sektor: "B", AreaID: 16, Vrsta: models.PrijavaObavijest,
			Naslov: "Proba", Opis: "x", Datum: time.Now(), Status: models.PrijavaObjavljena, ObjavljenoAt: objavljeno,
			Slike: []models.SlikaPrijave{{ID: "s1", Bajtova: len(jpg), Sadrzaj: sadrzaj.Otisak(jpg)}}}
		if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveSlika(ctx, "s1", id, jpg); err != nil {
			t.Fatal(err)
		}
	}
	sad := time.Now()
	spremi("p1", &sad)
	spremi("p2", &staro)

	if err := repo.DeleteSlika(ctx, "p2", "s1"); err != nil {
		t.Fatal(err)
	}
	if sp.OtisakZapisaPoUlozi(ctx, EntityPrijave, "p1", ulogaSlike("s1")) == "" {
		t.Fatal("brisanje uz drugu prijavu odvezalo fotografiju prve")
	}
	if b, err := repo.Slika(ctx, "s1"); err != nil || string(b) != string(jpg) {
		t.Fatalf("fotografija prve prijave nakon brisanja uz drugu: %q %v", b, err)
	}
	if err := repo.DeleteSlika(ctx, "p2", "s1"); err != nil {
		t.Errorf("ponovno brisanje već maknute: %v", err)
	}

	// rok čuvanja: starija prijava više nema što maknuti, novija ostaje
	if n, err := repo.ObrisiStareSlike(ctx, time.Now().AddDate(0, 0, -180)); err != nil || n != 0 {
		t.Errorf("rok čuvanja: %d %v", n, err)
	}
	if n, err := repo.ObrisiStareSlike(ctx, time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Errorf("rok čuvanja nakon objave obje: %d %v", n, err)
	}
	if b, err := repo.Slika(ctx, "s1"); err != nil || len(b) != 0 {
		t.Errorf("fotografija ostala nakon roka: %d bajtova, %v", len(b), err)
	}

	// bez spremišta nema se što brisati
	SetSpremiste(nil)
	if err := repo.DeleteSlika(ctx, "p1", "s1"); err != nil {
		t.Errorf("brisanje bez spremišta: %v", err)
	}
}
