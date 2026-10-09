package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Prijavu koju osoba ne smije čitati Get odbija s ErrUnauthorized; tuđi
// nacrt s ErrTudjiNacrt, koji je i dalje ErrUnauthorized s istom porukom,
// da ga rute mogu pokazati kao nepostojeći. Podaci su izmišljeni.
func TestGetPrijaveKojaSeNeCita(t *testing.T) {
	k := novaKnjigaProba(t)
	repo := repository.NewPrijavaRepository(k.o.baza, ledger.New(k.o.baza, "test"))
	prijave := &PrijavaService{repo: repo, vodocuvar: k.o.vs}
	ctx := context.Background()
	upisi := func(status string) *models.PrijavaSTerena {
		t.Helper()
		p := &models.PrijavaSTerena{UserID: k.ana.ID.String(), Ime: k.ana.FullName, Sektor: "P", AreaID: 1, Vrsta: models.PrijavaObavijest,
			Naslov: "Srušeno stablo", Opis: "opis", Datum: time.Now(), Status: status}
		if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	objavljena, nacrt := upisi(models.PrijavaObjavljena), upisi(models.PrijavaNacrt)
	dionicar := k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.2")
	if _, err := prijave.Get(ctx, dionicar, objavljena.ID); !errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrTudjiNacrt) {
		t.Errorf("objavljena prijava bez veze: %v", err)
	}
	_, err := prijave.Get(ctx, dionicar, nacrt.ID)
	if !errors.Is(err, ErrTudjiNacrt) || !errors.Is(err, ErrUnauthorized) || err.Error() != ErrUnauthorized.Error() {
		t.Errorf("tuđi nacrt: %v", err)
	}
	if p, err := prijave.Get(ctx, k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1}), objavljena.ID); err != nil || p == nil {
		t.Errorf("rukovoditelj područja: %v", err)
	}
}
