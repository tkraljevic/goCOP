package peers

// Promjene prava koje stižu uvozom paketa ili arhive čekaju potvrdu kao i
// one iz razmjene (repository/apply_ograda_prava.go): uvoz ide istim upisom
// (primijeniPodBravom) i istom primjenom na površinu. Dužnost računa koji
// čeka potvrdu ne javlja grešku uvoza, nego dolazi s potvrdom računa. Imena
// su izmišljena.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func TestUvozPromjenePravaCekaPotvrdu(t *testing.T) {
	laptop, ured := rubniCvor(t, "laptop-baranja"), rubniCvor(t, "cop-osijek")
	ured.OnApplied(func(ctx context.Context, verzije []ledger.Version) error {
		return repository.ApplyVersions(ctx, ured.db, ured.rec, verzije)
	})
	for _, c := range []*Service{laptop, ured} {
		if _, err := c.db.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'B', 'V', 'C')`); err != nil {
			t.Fatal(err)
		}
	}
	racuni := repository.NewUserRepository(laptop.db, laptop.rec)
	sektor := "B"
	novi := func(ime string, admin bool) models.User {
		u := models.User{ID: uuid.New(), Username: ime, FullName: "Račun " + ime, PasswordHash: "s", IsActive: true,
			IsGlobalAdmin: admin, OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		d := &models.Duty{Title: "Rukovoditelj sektora", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector,
			SectorID: &sektor, IsActive: true}
		if err := racuni.CreateUser(&u, d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	obican, admin := novi("obican", false), novi("upravitelj", true)
	verzije, err := laptop.rec.Since(ctxRub, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ured.primijeniPodBravom(ctxRub, verzije); err != nil {
		t.Fatal(err)
	}
	naUredu := repository.NewUserRepository(ured.db, ured.rec)
	if u, err := naUredu.GetUserByID(obican.ID); err != nil || u == nil {
		t.Fatalf("račun bez zastavice nije stigao: %v", err)
	}
	if u, err := naUredu.GetUserByID(admin.ID); err == nil && u != nil {
		t.Fatal("račun sa zastavicom globalnog administratora stigao je bez potvrde")
	}
	ceka, _, err := repository.PromjenePrava(ctxRub, ured.db, ured.rec, 1)
	if err != nil || len(ceka) != 1 || ceka[0].Cvor != "laptop-baranja" {
		t.Fatalf("čeka %+v %v", ceka, err)
	}
	if err := repository.PotvrdiPromjenuPrava(ctxRub, ured.db, ured.rec, nil, ceka[0].VersionID, "Ivan Ivić (iivic)"); err != nil {
		t.Fatal(err)
	}
	u, err := naUredu.GetUserByID(admin.ID)
	if err != nil || u == nil || !u.IsGlobalAdmin {
		t.Fatalf("nakon potvrde: %+v %v", u, err)
	}
	if d, err := naUredu.GetDutiesForUser(admin.ID); err != nil || len(d) != 1 {
		t.Errorf("dužnost nije stigla s potvrdom računa: %+v %v", d, err)
	}
}
