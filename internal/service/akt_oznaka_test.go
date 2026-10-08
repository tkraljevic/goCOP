package service_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// prenesiAkte prenosi verzije akata s jednog čvora na drugi, kao razmjena:
// u knjigu primatelja i na njegovu površinu
func prenesiAkte(t *testing.T, s, na *okolinaAkta) {
	t.Helper()
	ctx := context.Background()
	sve, err := ledger.New(s.baza, "test").Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var akti []ledger.Version
	for _, v := range sve {
		if v.Entity == repository.EntityAkti {
			akti = append(akti, v)
		}
	}
	rec := ledger.New(na.baza, "test")
	if _, err := rec.Apply(ctx, akti); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyVersions(ctx, na.baza, rec, akti); err != nil {
		t.Fatal(err)
	}
}

// Dva čvora bez veze ovjere svaki svoj akt i oba dobiju P-1/2026. Kad ih
// razmjena spoji, oba ostaju valjana i potpisana, a označena su kao sukob
// koji rješava Voditelj Centra obrane od poplava ili zamjenik; rješenje
// razmjenom vide svi čvorovi (Odluke (8. 10. 2026.), pitanje 4).
func TestIstaOznakaSDvaCvoraIRjesenjeSukoba(t *testing.T) {
	laptop, posluzitelj := novaOkolinaAkta(t), novaOkolinaAkta(t)
	for _, o := range []*okolinaAkta{laptop, posluzitelj} {
		_, k, _ := ed25519.GenerateKey(nil)
		o.akti.SetKljuc(k)
	}
	ctx := context.Background()
	sat := time.Now().Add(-time.Hour).Truncate(time.Minute)
	prvi, _ := laptop.ovjeri(t, models.AktUspostava, models.PhasePrep, sat)
	drugi, _ := posluzitelj.ovjeri(t, models.AktUspostava, models.PhasePrep, sat.Add(5*time.Minute))
	if prvi.Oznaka() != drugi.Oznaka() {
		t.Fatalf("bez veze oba čvora daju isti prvi broj: %s i %s", prvi.Oznaka(), drugi.Oznaka())
	}
	if s, err := laptop.akti.SukobiOznaka(ctx); err != nil || len(s) != 0 {
		t.Fatalf("prije razmjene nema sukoba: %v %v", s, err)
	}

	prenesiAkte(t, posluzitelj, laptop)
	sukobi, err := laptop.akti.SukobiOznaka(ctx)
	if err != nil || len(sukobi) != 2 || sukobi[prvi.ID].Rijesen() || len(sukobi[prvi.ID].Drugi) != 1 || sukobi[prvi.ID].Drugi[0].ID != drugi.ID {
		t.Fatalf("sukob nakon razmjene: %+v %v", sukobi, err)
	}
	for _, id := range []string{prvi.ID, drugi.ID} {
		if a, _ := laptop.akti.Get(ctx, id); !a.Ovjeren() || service.ProvjeriPotpis(a) != service.PotpisVrijedi {
			t.Errorf("akt %s nakon razmjene mora ostati ovjeren i potpisan: %+v", id, a)
		}
	}
	// sljedeći akt na laptopu nastavlja niz
	if treci, _ := laptop.ovjeri(t, models.AktPrekid, models.PhasePrep, sat.Add(10*time.Minute)); treci.Broj != 2 {
		t.Errorf("sljedeći broj nakon razmjene: %s", treci.Oznaka())
	}

	// tko smije riješiti sukob
	sektor, drugiSektor := "P", "Q"
	duznost := func(r models.Role, s *string) *models.UserPermissions {
		return models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{{Role: r, SectorID: s, IsActive: true}}})
	}
	istekla := time.Now().Add(-time.Hour)
	for _, c := range []struct {
		opis  string
		perms *models.UserPermissions
		smije bool
	}{
		{"voditelj COP-a sektora", duznost(models.RoleCopLeader, &sektor), true},
		{"zamjenik voditelja COP-a", duznost(models.RoleCopDeputy, &sektor), true},
		{"voditelj COP-a drugog sektora", duznost(models.RoleCopLeader, &drugiSektor), false},
		{"rukovoditelj sektora", duznost(models.RoleSectorLeader, &sektor), false},
		{"rukovoditelj područja", laptop.ovlasti, false},
		{"uprava organizacije", &models.UserPermissions{IsGlobalAdmin: true}, false},
		{"istekla dužnost voditelja", models.NewUserPermissions(models.User{Duties: []models.Duty{{Role: models.RoleCopLeader, SectorID: &sektor, IsActive: true, ExpiresAt: &istekla}}}), false},
		{"bez ovlasti", nil, false},
	} {
		if got := service.SmijeRijesitiOznaku(c.perms, prvi); got != c.smije {
			t.Errorf("%s: smije=%v, očekivano %v", c.opis, got, c.smije)
		}
	}

	voditelj := &models.User{ID: uuid.New(), FullName: "Ivo Ivić", Duties: []models.Duty{{Role: models.RoleCopLeader, SectorID: &sektor, IsActive: true}}}
	vOvl := models.NewUserPermissions(*voditelj)
	if err := laptop.akti.RijesiOznaku(ctx, laptop.ovlasti, laptop.rukovod, prvi.ID, "dopis"); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("rukovoditelj područja ne rješava sukob oznake: %v", err)
	}
	if err := laptop.akti.RijesiOznaku(ctx, vOvl, voditelj, prvi.ID, "  "); err == nil || !strings.Contains(err.Error(), "napomen") {
		t.Errorf("rješenje bez napomene: %v", err)
	}
	if err := laptop.akti.RijesiOznaku(ctx, vOvl, voditelj, "nema-ga", "dopis"); err == nil {
		t.Error("rješenje akta kojeg nema")
	}
	treci, _ := laptop.akti.List(ctx, nil, repository.FiltarAkata{Sektor: "P", Radnja: models.AktPrekid})
	if err := laptop.akti.RijesiOznaku(ctx, vOvl, voditelj, treci[0].ID, "dopis"); err == nil || !strings.Contains(err.Error(), "ne dijeli oznaku") {
		t.Errorf("akt bez sukoba: %v", err)
	}
	if err := laptop.akti.RijesiOznaku(ctx, vOvl, voditelj, prvi.ID, "Primateljima akta s poslužitelja poslan dopis s ispravkom oznake."); err != nil {
		t.Fatal(err)
	}
	sukobi, _ = laptop.akti.SukobiOznaka(ctx)
	if !sukobi[prvi.ID].Rijesen() || !sukobi[drugi.ID].Rijesen() || sukobi[drugi.ID].Rjesenje.Rijesio != "Ivo Ivić" {
		t.Fatalf("nakon rješenja: %+v", sukobi)
	}
	for _, id := range []string{prvi.ID, drugi.ID} {
		if a, _ := laptop.akti.Get(ctx, id); a.RijesenaOznaka == nil || service.ProvjeriPotpis(a) != service.PotpisVrijedi {
			t.Errorf("rješenje ne smije srušiti potpis akta %s: %+v", id, a)
		}
	}

	// rješenje putuje knjigom: poslužitelj ga vidi nakon razmjene
	prenesiAkte(t, laptop, posluzitelj)
	if s, _ := posluzitelj.akti.SukobiOznaka(ctx); !s[drugi.ID].Rijesen() || !strings.Contains(s[drugi.ID].Rjesenje.Napomena, "dopis") {
		t.Errorf("poslužitelj nakon razmjene ne vidi rješenje: %+v", s)
	}
}

// Kad se akti ne daju pročitati, sukob se ne provjerava ni rješava
func TestSukobOznakeKadBazaNeOdgovara(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	if _, err := o.baza.Exec(`DROP TABLE akti`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.akti.SukobiOznaka(ctx); err == nil {
		t.Error("sukobi oznaka bez tablice akata")
	}
	sektor := "P"
	voditelj := &models.User{ID: uuid.New(), FullName: "Iva Ivić", Duties: []models.Duty{{Role: models.RoleCopDeputy, SectorID: &sektor, IsActive: true}}}
	if err := o.akti.RijesiOznaku(ctx, models.NewUserPermissions(*voditelj), voditelj, uuid.NewString(), "dopis"); err == nil {
		t.Error("rješenje sukoba bez tablice akata")
	}
}
