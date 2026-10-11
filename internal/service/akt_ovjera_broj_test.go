package service

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
)

// Izvanredno stanje u hitnom slučaju proglašava rukovoditelj branjenog
// područja: tada je potpisnik on, a ne sektor
func TestPotpisnikOvjere(t *testing.T) {
	sektor, podrucje := "P", 1
	osoba := func(r models.Role) (*models.User, *models.UserPermissions) {
		u := &models.User{ID: uuid.New(), FullName: "Ana Anić", Duties: []models.Duty{{Role: r, SectorID: &sektor, AreaID: &podrucje, IsActive: true}}}
		return u, models.NewUserPermissions(*u)
	}
	for _, c := range []struct {
		opis      string
		uloga     models.Role
		stupanj   models.DefensePhase
		potpisnik string
		uZamjeni  bool
	}{
		{"rukovoditelj BP, izvanredno stanje", models.RoleAreaLeader, models.PhaseState, "Rukovoditelj obrane od poplava za branjeno područje 1", false},
		{"zamjenik BP, izvanredno stanje", models.RoleAreaDeputy, models.PhaseState, "Rukovoditelj obrane od poplava za branjeno područje 1", true},
		{"rukovoditelj sektora, izvanredno stanje", models.RoleSectorLeader, models.PhaseState, "sektor", false},
		{"zamjenik sektora, izvanredna", models.RoleSectorDeputy, models.PhaseEmergency, "sektor", true},
		{"rukovoditelj BP, pripremno", models.RoleAreaLeader, models.PhasePrep, "sektor", false},
	} {
		u, p := osoba(c.uloga)
		a := &models.Akt{Sektor: sektor, AreaID: podrucje, Stupanj: c.stupanj, Potpisnik: "sektor"}
		odrediPotpisnika(p, u, a)
		if a.Potpisnik != c.potpisnik || a.UZamjeni != c.uZamjeni {
			t.Errorf("%s: potpisnik %q, u.z. %v", c.opis, a.Potpisnik, a.UZamjeni)
		}
	}
}

// Ovjera akta koji nije spremljen kao nacrt ne prolazi i ne objavljuje se;
// spremljen nacrt dobiva broj i ide u dnevnike aktivne obrane
func TestZakljucenjeOvjereDodjeljujeBrojSpremljenomNacrtu(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := repository.NewAktiRepository(baza, ledger.New(baza, "test"))
	svc := NewAktService(repo, nil, nil, nil, nil, nil, nil, "cvor-probni")
	dnevnik := &models.Journal{ID: "dnevnik-cop", CentarSektor: "P"}
	var objavljeno []string
	svc.SetObrana(func(context.Context, string) *models.Journal { return dnevnik }, func(_ context.Context, _ *models.User, a *models.Akt, j *models.Journal) []string {
		objavljeno = append(objavljeno, a.Oznaka()+" u "+j.ID)
		return []string{"dnevnik vodočuvara: probno upozorenje"}
	})
	pero := &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *pero}
	a := &models.Akt{Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)}

	if _, _, err := svc.zakljuciOvjeru(ctx, uprava, pero, a, time.Now()); err == nil {
		t.Fatal("akt koji nije spremljen kao nacrt ne ovjerava se")
	}
	if len(objavljeno) != 0 {
		t.Fatalf("neovjeren akt je objavljen: %v", objavljeno)
	}
	a.Status = models.AktNacrt
	if err := repo.SaveAkt(ctx, a); err != nil {
		t.Fatal(err)
	}
	ovjeren, upozorenja, err := svc.zakljuciOvjeru(ctx, uprava, pero, a, time.Now())
	if err != nil || ovjeren.Broj != 1 || !ovjeren.Ovjeren() {
		t.Fatalf("ovjera spremljenog nacrta: %+v %v", ovjeren, err)
	}
	if len(objavljeno) != 1 || objavljeno[0] != "P-1/"+a.Vrijedi.In(models.Zagreb).Format("2006")+" u dnevnik-cop" || len(upozorenja) != 1 {
		t.Errorf("objava ovjerenog akta: %v, upozorenja %v", objavljeno, upozorenja)
	}
}
