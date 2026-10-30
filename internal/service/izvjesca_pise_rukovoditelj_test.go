package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Odluke (30. 10. 2026.), 3: dnevno izvješće dionice pišu i predaju samo
// rukovoditelj dionice i zamjenik rukovoditelja dionice. Uprava područja i
// sektora, globalni administrator i vodočuvar izvješće čitaju, ali ga ne
// pišu i ne predaju; uprava ga i dalje briše.
func TestDnevnoIzvjescePiseSamoRukovoditeljDionice(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	zamjenik := ovlastiOsobe(duznost(models.RoleSectionDeputy, models.ScopeSection, "P", 1, "P.1.1"))
	upravaPodrucja := ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "P", 1, ""))
	upravaSektora := ovlastiOsobe(duznost(models.RoleSectorLeader, models.ScopeSector, "P", 0, ""))
	vodocuvar := ovlastiOsobe(duznost(models.RoleWaterGuard, models.ScopeSection, "P", 1, "P.1.1"))
	admin := models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Pero Perić", IsGlobalAdmin: true})

	nacrt := punoIzvjesce("P.1.1", danas())
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, nacrt); err != nil {
		t.Fatalf("rukovoditelj dionice piše: %v", err)
	}
	for _, s := range []struct {
		ime   string
		perms *models.UserPermissions
	}{
		{"uprava područja", upravaPodrucja},
		{"uprava sektora", upravaSektora},
		{"globalni administrator", admin},
		{"vodočuvar dionice", vodocuvar},
	} {
		if err := o.svc.Spremi(ctx, o.urednik, s.perms, o.d111, punoIzvjesce("P.1.1", danas().AddDate(0, 0, -1))); err == nil || !strings.Contains(err.Error(), "rukovoditelj dionice") {
			t.Errorf("%s piše: %v", s.ime, err)
		}
		if err := o.svc.Predaj(ctx, o.urednik, s.perms, o.d111, nacrt.ID); err == nil || !strings.Contains(err.Error(), "rukovoditelj dionice") {
			t.Errorf("%s predaje: %v", s.ime, err)
		}
		if !o.svc.SmijeVidjeti(s.perms, o.d111) {
			t.Errorf("%s više ne čita izvješće", s.ime)
		}
	}
	if g, err := o.svc.Get(ctx, nacrt.ID); err != nil || g.Predano() {
		t.Fatal("izvješće je predao netko tko ga ne piše")
	}

	// zamjenik rukovoditelja dionice piše i predaje
	jucer := punoIzvjesce("P.1.1", danas().AddDate(0, 0, -1))
	if err := o.svc.Spremi(ctx, o.urednik, zamjenik, o.d111, jucer); err != nil {
		t.Fatalf("zamjenik piše: %v", err)
	}
	if err := o.svc.Predaj(ctx, o.urednik, zamjenik, o.d111, jucer.ID); err != nil {
		t.Fatalf("zamjenik predaje: %v", err)
	}

	// nacrt ne briše vodočuvar; briše ga uprava područja
	if err := o.svc.Obrisi(ctx, o.urednik, vodocuvar, o.d111, nacrt.ID); err == nil || !strings.Contains(err.Error(), "nacrt izvješća briše") {
		t.Errorf("vodočuvar briše nacrt: %v", err)
	}
	if err := o.svc.Obrisi(ctx, o.urednik, upravaPodrucja, o.d111, nacrt.ID); err != nil {
		t.Errorf("uprava područja briše nacrt: %v", err)
	}
	// predano ne briše zamjenik, nego uprava
	if err := o.svc.Obrisi(ctx, o.urednik, zamjenik, o.d111, jucer.ID); err == nil || !strings.Contains(err.Error(), "samo uprava") {
		t.Errorf("zamjenik briše predano: %v", err)
	}
	if err := o.svc.Obrisi(ctx, o.urednik, upravaSektora, o.d111, jucer.ID); err != nil {
		t.Errorf("uprava sektora briše predano: %v", err)
	}
	if err := o.svc.Obrisi(ctx, o.urednik, upravaSektora, nil, jucer.ID); err == nil {
		t.Error("brisanje bez dionice")
	}
	if o.svc.SmijeBrisati(upravaSektora, o.d111, nil) || o.svc.SmijeBrisati(upravaSektora, nil, nacrt) {
		t.Error("brisanje bez izvješća ili dionice")
	}
}
