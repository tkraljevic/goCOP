package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/models"
)

// Zapis iz knjige otprije vrste (drugi čvor sa starijim programom) na
// površini postaje izvedena točka.
func TestKisomjerIzKnjigeBezVrste(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "vrsta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	ctx := context.Background()
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	sada := time.Now()
	if err := upsertKisomjer(ctx, tx, models.Kisomjer{Code: "k-a", Naziv: "A", Latitude: 46, Longitude: 14,
		Aktivan: true, CreatedAt: sada, UpdatedAt: sada}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	k, err := NewKisomjerRepository(baza, nil).GetKisomjer(ctx, "k-a")
	if err != nil || k == nil || k.Vrsta != models.KisomjerIzvedeni || k.JeStvarni() {
		t.Fatalf("GetKisomjer = %+v, %v", k, err)
	}
}

// Letva uz kišomjer stiže i s drugog čvora, a izmjena je mijenja.
func TestKisomjerSLetvomIzKnjige(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "letva.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	ctx := context.Background()
	sada := time.Now()
	for _, letva := range []string{"lipa", "brod"} {
		tx, err := baza.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := upsertKisomjer(ctx, tx, models.Kisomjer{Code: "k-a", Naziv: "A", Vrsta: models.KisomjerStvarni, Izvor: "hv",
			Letva: letva, Latitude: 46, Longitude: 14, Aktivan: true, CreatedAt: sada, UpdatedAt: sada}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		repo := NewKisomjerRepository(baza, nil)
		k, err := repo.GetKisomjer(ctx, "k-a")
		if err != nil || k == nil || k.Letva != letva {
			t.Fatalf("GetKisomjer = %+v, %v; želim letvu %s", k, err, letva)
		}
		if drugi, err := repo.KisomjerUzLetvu(ctx, letva, "k-b"); err != nil || drugi != "k-a" {
			t.Errorf("KisomjerUzLetvu(%s) = %q, %v", letva, drugi, err)
		}
	}
}

// Ime letve čita se iz registra; letve koje nema daju prazno, a baza bez
// tablica grešku.
func TestImeLetveIKisomjerUzLetvu(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "ime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	ctx := context.Background()
	sada := time.Now()
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('l', 'lipa', 'Lipovac', ?, ?)`, sada, sada); err != nil {
		t.Fatal(err)
	}
	repo := NewKisomjerRepository(baza, nil)
	if ime, err := repo.ImeLetve(ctx, "lipa"); err != nil || ime != "Lipovac" {
		t.Errorf("ImeLetve(lipa) = %q, %v", ime, err)
	}
	if ime, err := repo.ImeLetve(ctx, "nema"); err != nil || ime != "" {
		t.Errorf("ImeLetve(nema) = %q, %v", ime, err)
	}
	if drugi, err := repo.KisomjerUzLetvu(ctx, "lipa", ""); err != nil || drugi != "" {
		t.Errorf("KisomjerUzLetvu bez kišomjera = %q, %v", drugi, err)
	}
	if _, err := baza.Exec(`DROP TABLE kisomjeri`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.KisomjerUzLetvu(ctx, "lipa", ""); err == nil {
		t.Error("KisomjerUzLetvu bez tablice nije javio grešku")
	}
	if _, err := baza.Exec(`DROP TABLE stations`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ImeLetve(ctx, "lipa"); err == nil {
		t.Error("ImeLetve bez tablice nije javio grešku")
	}
}
