package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Servis očitanja samo prosljeđuje sažetak spremištu.
func TestMjerenjaPreuzimanihKrozServis(t *testing.T) {
	o := novaOkolinaAkta(t)
	sad := time.Now()
	citaj := service.NewReadingService(repository.NewReadingRepository(o.baza, ledger.New(o.baza, "test")), nil, nil, nil, nil)
	if letve, err := citaj.MjerenjaPreuzimanih(context.Background(), sad.Add(-time.Hour), sad); err != nil || len(letve) != 0 {
		t.Errorf("bez očitanja: %+v %v", letve, err)
	}
}

// Napomenu sprema samo globalni administrator, najviše 500 znakova; razmaci i
// novi redovi se sažimaju, a prazan tekst je briše. Svako spremanje je verzija
// koja putuje razmjenom.
func TestNapomenaPodataka(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	if v := o.akti.NapomenaPodataka(ctx); v != "" {
		t.Fatalf("bez zapisa: %q", v)
	}
	for _, perms := range []*models.UserPermissions{nil, upravaSektoraP} {
		if err := o.akti.SpremiNapomenuPodataka(ctx, perms, "Ne smije."); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("bez ovlasti: %v", err)
		}
	}
	if v := o.akti.NapomenaPodataka(ctx); v != "" {
		t.Fatalf("spremljeno bez ovlasti: %q", v)
	}

	if err := o.akti.SpremiNapomenuPodataka(ctx, upravaOrganizacije, "  Izvor\r\nmijenja   adrese. "); err != nil {
		t.Fatal(err)
	}
	if v := o.akti.NapomenaPodataka(ctx); v != "Izvor mijenja adrese." {
		t.Errorf("spremljeno: %q", v)
	}
	var verzija int
	if err := o.baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ?`,
		repository.EntityPostavke, repository.PostavkaNapomenaPodataka).Scan(&verzija); err != nil || verzija != 1 {
		t.Errorf("verzija u knjizi: %d %v", verzija, err)
	}

	tocno := strings.Repeat("č", service.NapomenaPodatakaNajvise)
	if err := o.akti.SpremiNapomenuPodataka(ctx, upravaOrganizacije, tocno); err != nil {
		t.Fatalf("točno najviše znakova: %v", err)
	}
	err := o.akti.SpremiNapomenuPodataka(ctx, upravaOrganizacije, tocno+"č")
	if err == nil || !strings.Contains(err.Error(), "501") {
		t.Fatalf("preduga napomena: %v", err)
	}
	if v := o.akti.NapomenaPodataka(ctx); v != tocno {
		t.Error("preduga napomena nešto je promijenila")
	}

	if err := o.akti.SpremiNapomenuPodataka(ctx, upravaOrganizacije, " \n "); err != nil {
		t.Fatal(err)
	}
	if v := o.akti.NapomenaPodataka(ctx); v != "" {
		t.Errorf("nakon brisanja: %q", v)
	}
}
