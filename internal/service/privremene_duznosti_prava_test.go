package service_test

// Primljena privremena dužnost uprave organizacije kojoj se istek računa iz
// druge dužnosti čeka potvrdu i kad je upisani istek prošao: preračun isteka
// na ovom računalu ne smije je staviti na snagu bez potvrde. Primljena
// privremena dužnost niže razine stiže i preračunava se kao i prije.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func TestPreracunIstekaNeStavljaUpravuBezPotvrde(t *testing.T) {
	o := okolinaImenovanja(t)
	ctx := context.Background()
	rec := ledger.New(o.baza, "test")
	var uloga string
	if err := o.baza.QueryRow(`SELECT role FROM duties WHERE user_id = ? LIMIT 1`, o.rukovod.ID.String()).Scan(&uloga); err != nil {
		t.Fatal(err)
	}
	izvor := o.duznostOsobe(t, o.rukovod.ID, models.Role(uloga))
	proslo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	stigne := func(uloga models.Role) models.Duty {
		d := models.Duty{ID: uuid.New(), UserID: o.vodocuv.ID, Role: uloga, ScopeType: models.ScopeAll,
			IsActive: true, IsTemporary: true, OvisiO: &izvor.ID, ExpiresAt: &proslo, CreatedAt: time.Now()}
		p, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		v := ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: repository.EntityDuties, EntityID: d.ID.String(),
			NodeID: "laptop-baranja", Payload: p, CreatedAt: time.Now(), SchemaVersion: ledger.SchemaVersion}
		if _, err := rec.Apply(ctx, []ledger.Version{v}); err != nil {
			t.Fatal(err)
		}
		if err := repository.ApplyVersions(ctx, o.baza, rec, []ledger.Version{v}); err != nil {
			t.Fatal(err)
		}
		return d
	}
	uprava, obicna := stigne(models.RoleNationalLeader), stigne(models.RoleAreaDeputy)
	if err := o.users.UskladiPrivremene(); err != nil {
		t.Fatal(err)
	}
	if g, err := o.users.GetDuty(uprava.ID); err != nil || g != nil {
		t.Errorf("dužnost uprave organizacije bez potvrde: %+v %v", g, err)
	}
	if g, err := o.users.GetDuty(obicna.ID); err != nil || g == nil || g.ExpiresAt != nil && !g.ExpiresAt.After(proslo) {
		t.Errorf("privremena dužnost razine 3 nije stigla ili joj istek nije preračunat: %+v %v", g, err)
	}
	ceka, _, err := repository.PromjenePrava(ctx, o.baza, rec, 1)
	if err != nil || len(ceka) != 1 || ceka[0].Zapis != uprava.ID.String() {
		t.Errorf("čeka %+v %v", ceka, err)
	}
}
