package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// kisomjerSLetvom je servis kišomjera nad bazom s jednom letvom „lipa”.
func kisomjerSLetvom(t *testing.T) (*KisomjerService, *sql.DB) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "letva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	sada := time.Now()
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at)
		VALUES ('lipa-id', 'lipa', 'Lipovac', ?, ?)`, sada, sada); err != nil {
		t.Fatal(err)
	}
	return NewKisomjerService(repository.NewKisomjerRepository(baza, ledger.New(baza, "test-node"))), baza
}

func stvarniUzLetvu(code, letva string) *models.Kisomjer {
	return &models.Kisomjer{Code: code, Naziv: "Kišomjer " + code, Vrsta: models.KisomjerStvarni,
		Izvor: "hv", Letva: letva, Latitude: 45.5, Longitude: 18.5, Aktivan: true}
}

func TestKisomjerUzLetvu(t *testing.T) {
	svc, _ := kisomjerSLetvom(t)
	ctx := context.Background()

	if err := svc.CreateKisomjer(ctx, kisomjerAdmin, stvarniUzLetvu("k-lipa", " lipa ")); err != nil {
		t.Fatalf("upis uz letvu: %v", err)
	}
	k, err := svc.GetKisomjer(ctx, "k-lipa")
	if err != nil || k == nil || k.Letva != "lipa" {
		t.Fatalf("spremljeno = %+v, %v", k, err)
	}
	if id, ime := svc.LetvaKisomjera(ctx, k.Letva); id != "lipa-id" || ime != "Lipovac" {
		t.Errorf("LetvaKisomjera = %q, %q", id, ime)
	}
	if id, ime := svc.LetvaKisomjera(ctx, ""); id != "" || ime != "" {
		t.Errorf("LetvaKisomjera bez letve = %q, %q", id, ime)
	}

	// Izmjena istog kišomjera zadržava letvu: on sam nije „drugi” kišomjer.
	k.Naziv = "Lipovac kiša"
	if err := svc.UpdateKisomjer(ctx, kisomjerAdmin, k); err != nil {
		t.Fatalf("izmjena: %v", err)
	}

	odbijeno := func(naziv string, err error, dio string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), dio) {
			t.Errorf("%s: očekivana greška s %q, dobiveno %v", naziv, dio, err)
		}
	}
	odbijeno("druga veza na istu letvu",
		svc.CreateKisomjer(ctx, kisomjerAdmin, stvarniUzLetvu("k-drugi", "lipa")), "k-lipa")
	odbijeno("nepostojeća letva",
		svc.CreateKisomjer(ctx, kisomjerAdmin, stvarniUzLetvu("k-treci", "nema")), "nema u registru")
	izvedena := &models.Kisomjer{Code: "k-izv", Naziv: "Izvedena", Sliv: "Z", Letva: "lipa",
		Latitude: 45.5, Longitude: 18.5, Aktivan: true}
	odbijeno("izvedena točka uz letvu", svc.CreateKisomjer(ctx, kisomjerAdmin, izvedena), "stvarni kišomjer")

	// Bez letve kišomjer se sprema kao i prije.
	if err := svc.CreateKisomjer(ctx, kisomjerAdmin, stvarniUzLetvu("k-bez", "")); err != nil {
		t.Fatalf("upis bez letve: %v", err)
	}
}

// Kad baza ne odgovara, provjera letve vraća grešku umjesto da propusti upis.
func TestKisomjerUzLetvuGreskaBaze(t *testing.T) {
	svc, baza := kisomjerSLetvom(t)
	ctx := context.Background()
	if _, err := baza.Exec(`DROP TABLE stations`); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateKisomjer(ctx, kisomjerAdmin, stvarniUzLetvu("k-a", "lipa")); err == nil {
		t.Error("upis bez tablice letvi je prošao")
	}
	if _, ime := svc.LetvaKisomjera(ctx, "lipa"); ime != "" {
		t.Errorf("LetvaKisomjera bez tablice = %q", ime)
	}
}

// Kad letva postoji, a tablica kišomjera ne odgovara, provjera druge veze
// vraća grešku.
func TestKisomjerUzLetvuGreskaDrugeVeze(t *testing.T) {
	svc, baza := kisomjerSLetvom(t)
	ctx := context.Background()
	if _, err := baza.Exec(`ALTER TABLE kisomjeri RENAME COLUMN letva TO stara_letva`); err != nil {
		t.Fatal(err)
	}
	if err := svc.provjeriLetvu(ctx, stvarniUzLetvu("k-a", "lipa")); err == nil {
		t.Error("provjera bez stupca letve je prošla")
	}
}

// Obrazac očitanja doznaje kišomjer uz letvu; bez registra, bez letve ili
// kad baza ne odgovara kišomjera nema.
func TestKisomjerUzLetvuZaOcitanje(t *testing.T) {
	svc, baza := kisomjerSLetvom(t)
	ctx := context.Background()
	if err := svc.CreateKisomjer(ctx, kisomjerAdmin, stvarniUzLetvu("k-lipa", "lipa")); err != nil {
		t.Fatal(err)
	}
	if k := svc.KisomjerUzLetvu(ctx, "lipa"); k == nil || k.Code != "k-lipa" {
		t.Errorf("uz letvu = %+v", k)
	}
	if k := svc.KisomjerUzLetvu(ctx, "brod"); k != nil {
		t.Errorf("letva bez kišomjera = %+v", k)
	}
	if k := svc.KisomjerUzLetvu(ctx, ""); k != nil {
		t.Errorf("bez letve = %+v", k)
	}
	if k := (*KisomjerService)(nil).KisomjerUzLetvu(ctx, "lipa"); k != nil {
		t.Errorf("bez registra = %+v", k)
	}
	if _, err := baza.Exec(`DROP TABLE kisomjeri`); err != nil {
		t.Fatal(err)
	}
	if k := svc.KisomjerUzLetvu(ctx, "lipa"); k != nil {
		t.Errorf("bez tablice = %+v", k)
	}
}
