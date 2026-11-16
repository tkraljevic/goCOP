package service_test

import (
	"errors"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Odluke (16. 11. 2026.), Djelatnici 7: uprava sektora i uprava branjenog
// područja isključuju i ponovno uključuju račun osobe u svom dosegu, npr.
// kad vodočuvar ode iz službe; uprava drugog područja ne, a ni uprava
// područja upravu sektora. Osobe su izmišljene.
func TestIskljucenjeRacunaUDosegu(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	vodocuvar := o.osoba(t, "vprimjer", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(16)})
	upravaB := korUprava(t, o, "uprava-b", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	uprava16 := korUprava(t, o, "uprava-16", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	uprava17 := korUprava(t, o, "uprava-17", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(17)})

	iskljuci := func(tko *models.UserPermissions, osoba *models.User, ukljucen bool) error {
		req := korIzmjena(osoba)
		req.IsActive = ukljucen
		_, err := o.users.UpdateUser(tko, req)
		return err
	}
	if err := iskljuci(uprava17, vodocuvar, false); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava drugog područja isključuje: %v", err)
	}
	if err := iskljuci(uprava16, vodocuvar, false); err != nil {
		t.Fatalf("uprava područja ne isključuje vodočuvara: %v", err)
	}
	if x, _ := o.repo.GetUserByID(vodocuvar.ID); x.IsActive {
		t.Error("račun nije isključen")
	}
	x, _ := o.repo.GetUserByID(vodocuvar.ID)
	if err := iskljuci(upravaB, x, true); err != nil {
		t.Fatalf("uprava sektora ne uključuje: %v", err)
	}
	if x, _ := o.repo.GetUserByID(vodocuvar.ID); !x.IsActive {
		t.Error("račun nije ponovno uključen")
	}
	sef, _ := o.repo.GetUserByID(upravaB.User.ID)
	if err := iskljuci(uprava16, sef, false); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja isključuje upravu sektora: %v", err)
	}
}
