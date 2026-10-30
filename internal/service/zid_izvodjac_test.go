package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Poslovođa izvođača na zidu ne vidi dnevnik COP-a ni dežurstva, ni kad ih
// izričito traži filtrom, a uprava centra ih vidi (Odluke (30. 10. 2026.),
// 2). Podaci su izmišljeni.
func TestZidIzvodjacNeVidiDnevnikCOPaNiDezurstva(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	if err := o.svc.repo.SaveDezurstvo(ctx, &models.Dezurstvo{JournalID: o.dnevnik.ID, UserID: o.pperic.ID.String(), UserName: "Pero Perić",
		Od: dzSat(9, 8, 0), Do: dzSat(9, 12, 0), Opis: "Dežurstvo u COP-u", Mjesto: models.MjestoUred}); err != nil {
		t.Fatal(err)
	}
	zid := NewZidService(ledger.New(o.baza, "test"), o.svc.repo, nil, nil, nil, nil, nil)
	zid.SetIzvodjac(o.svc.IzvodjacZaCOP)
	sektor := "P"
	izvodjac := &models.UserPermissions{AllowedSectors: map[string]bool{"P": true}, User: models.User{ID: uuid.New(), FullName: "Ivo Izvođač",
		Duties: []models.Duty{{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}}}}
	administrator := &models.UserPermissions{IsGlobalAdmin: true, User: izvodjac.User}

	broj := func(perms *models.UserPermissions, f FiltarZida) int {
		t.Helper()
		f.Limit = 10
		dog, _, err := zid.Zadnji(ctx, perms, f)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, d := range dog {
			if d.Modul == ModulDnevnik || d.Modul == ModulDezurstva {
				n++
			}
		}
		return n
	}
	for _, f := range []FiltarZida{{}, {Modul: ModulDnevnik}, {Modul: ModulDezurstva}} {
		if n := broj(izvodjac, f); n != 0 {
			t.Errorf("izvođač uz filtar %q vidi %d događaja dnevnika COP-a i dežurstava", f.Modul, n)
		}
	}
	if broj(o.ovlUprav, FiltarZida{Modul: ModulDnevnik}) == 0 || broj(o.ovlUprav, FiltarZida{Modul: ModulDezurstva}) == 0 {
		t.Error("uprava centra ne vidi dnevnik COP-a ili dežurstva")
	}
	if broj(administrator, FiltarZida{}) == 0 {
		t.Error("globalni administrator s dužnošću poslovođe ne vidi dnevnik COP-a")
	}
	// skriveni moduli računa ostaju skriveni i uz pravilo za izvođača
	if n := broj(o.ovlUprav, FiltarZida{Modul: ModulDezurstva, Skriveni: []string{ModulDezurstva}}); n != 0 {
		t.Errorf("skriveni modul vidi se: %d", n)
	}
}
