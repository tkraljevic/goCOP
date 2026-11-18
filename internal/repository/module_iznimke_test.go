package repository

import (
	"context"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Iznimke računa idu po imenu osobe, s modulima koje prikazuju i skrivaju;
// iznimka računa kojeg nema u tablici korisnika ima prazno ime
func TestIznimkeRacunaPoImenu(t *testing.T) {
	baza := bazaRacuna(t)
	rec := ledger.New(baza, "ured")
	users := NewUserRepository(baza, rec)
	repo := NewModuleRepository(baza, rec)
	ctx := context.Background()
	if l, err := repo.IznimkeRacuna(ctx); err != nil || len(l) != 0 {
		t.Fatalf("prazno: %v %v", l, err)
	}
	var id []string
	for _, ime := range []string{"Petra Primjer", "ana Primjer"} {
		u := &models.User{Username: ime, FullName: ime, IsActive: true, OrgType: models.OrgHrvatskeVode}
		if err := users.CreateUser(u, nil); err != nil {
			t.Fatal(err)
		}
		id = append(id, u.ID.String())
	}
	for _, i := range []struct {
		id            string
		shown, hidden []string
	}{
		{id[0], nil, []string{models.ModuleRegisters}},
		{id[1], []string{models.ModuleAdmin}, nil},
		{"nema-ga", []string{models.ModuleUsers}, nil},
	} {
		if err := repo.SetUserOverride(ctx, i.id, i.shown, i.hidden); err != nil {
			t.Fatal(err)
		}
	}
	l, err := repo.IznimkeRacuna(ctx)
	if err != nil || len(l) != 3 {
		t.Fatalf("%+v %v", l, err)
	}
	if l[0].Ime != "" || l[0].UserID != "nema-ga" || l[1].Ime != "ana Primjer" || l[2].Ime != "Petra Primjer" {
		t.Errorf("redoslijed: %q %q %q", l[0].Ime, l[1].Ime, l[2].Ime)
	}
	if len(l[1].Shown) != 1 || l[1].Shown[0] != models.ModuleAdmin || len(l[2].Hidden) != 1 || l[2].Hidden[0] != models.ModuleRegisters {
		t.Errorf("moduli: %+v / %+v", l[1].UserModules, l[2].UserModules)
	}

	baza.Close()
	if _, err := repo.IznimkeRacuna(ctx); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
