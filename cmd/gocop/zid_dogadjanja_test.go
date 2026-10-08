package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Zid čvora čita prijave s terena po pravilu prijava: objavljenu prijavu
// Marka Markića vidi on sam, a Pero Perić, vodočuvar istog područja, ne
// vidi, iako zid sektora vidi (Odluke (8. 10. 2026.), pitanje 20)
func TestZidCvoraCitaPrijavePoPraviluPrijava(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	repo := repository.NewPrijavaRepository(baza, rec)
	prijave := service.NewPrijavaService(repo, users, service.NewVodocuvarService(repository.NewVodocuvarRepository(baza, rec), users, "test"), "test")

	marko := models.User{ID: uuid.New(), FullName: "Marko Markić"}
	sad := time.Now()
	p := &models.PrijavaSTerena{UserID: marko.ID.String(), Ime: marko.FullName, Sektor: "B", AreaID: 34, Vrsta: models.PrijavaObavijest,
		Naslov: "Otpad u kanalu", Opis: "Zatečeno pri obilasku.", Datum: sad, Broj: 41, Godina: sad.In(models.Zagreb).Year(),
		Status: models.PrijavaObjavljena, ObjavljenoAt: &sad}
	if err := repo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}

	zid := service.NewZidService(rec, nil, nil, nil, userRepo, nil, nil)
	postaviZid(zid, &service.AktService{}, prijave)
	podrucje34 := 34
	for _, k := range []struct {
		ime   string
		perms *models.UserPermissions
		vidi  bool
	}{
		{"autor", &models.UserPermissions{User: marko, AllowedSectors: map[string]bool{"B": true}}, true},
		{"drugi vodočuvar", &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}, AllowedAreas: map[int]bool{34: true},
			User: models.User{ID: uuid.New(), FullName: "Pero Perić",
				Duties: []models.Duty{{Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, AreaID: &podrucje34, IsActive: true}}}}, false},
	} {
		dog, _, err := zid.Zadnji(ctx, k.perms, service.FiltarZida{Modul: service.ModulPrijave, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if vidi := len(dog) == 1 && dog[0].Naslov == "Obavijest s terena "+p.Oznaka(); vidi != k.vidi {
			t.Errorf("%s: zid prijava %+v, očekivano vidi=%v", k.ime, dog, k.vidi)
		}
	}
}
