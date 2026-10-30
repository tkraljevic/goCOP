package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/obracun"
)

// Izvođača plaća njegova firma, pa njegovo dežurstvo iz plana ne ulazi u
// obračun sati ni u IORS (Odluke (30. 10. 2026.)); dežurstvo djelatnika
// Hrvatskih voda ulazi kao i prije
func TestIzvodjacNijeUObracunu(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	izvodjac := uuid.New()
	djelatnici := duznostUSektoruOdUvijek("P")
	o.svc.SetPovijestZaduzenja(func(id uuid.UUID) ([]models.VerzijaZaduzenja, error) {
		if id != izvodjac {
			return djelatnici(id)
		}
		return []models.VerzijaZaduzenja{{Duty: models.Duty{Role: models.RoleServiceLeaderForeman, IsActive: true}, Od: dzSat(1, 0, 0)}}, nil
	})
	// dežurstvo upisano prije nadogradnje, kad je izvođač još ulazio u plan
	stari := &models.Dezurstvo{ID: uuid.NewString(), JournalID: o.dnevnik.ID, UserID: izvodjac.String(), UserName: "Ivo Izvođač",
		Od: dzSat(7, 8, 0), Do: dzSat(7, 16, 0), Opis: "Dežurstvo u COP-u", Potvrdio: "uprava"}
	if err := o.svc.repo.SaveDezurstvo(ctx, stari); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, o.peroDezura(dzSat(8, 8, 0), dzSat(8, 12, 0))); err != nil {
		t.Fatal(err)
	}
	ob, err := o.svc.Obracun(ctx, o.dnevnik, dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range ob.Grupe {
		for _, r := range g.Redovi {
			if r.UserID == izvodjac.String() {
				t.Errorf("izvođač je u obračunu: %+v", r)
			}
		}
	}
	if iors, err := o.svc.ObracunOsobe(ctx, o.dnevnik, izvodjac.String(), dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil); err != nil || len(iors.Redovi) != 0 {
		t.Errorf("IORS izvođača: %d redaka, %v", len(iors.Redovi), err)
	}
	// kontrola: djelatnik Hrvatskih voda ostaje u obračunu
	if iors, err := o.svc.ObracunOsobe(ctx, o.dnevnik, o.pperic.ID.String(), dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil); err != nil || iors.Stvarni != 4*time.Hour {
		t.Errorf("IORS djelatnika: %v, %v", iors.Stvarni, err)
	}
}
