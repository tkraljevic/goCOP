package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Postavke karata sprema samo uprava organizacije. Neispravna podloga se ne
// sprema i poruka kaže koja i zašto; ispravne vrijede odmah u programu, a
// „Vrati zadano” vraća narančaste nasipe i satelit.
func TestPostavkeKarteSpremaSamoUpravaOrganizacije(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	prije := models.PostavkeKartePrograma()
	t.Cleanup(func() { models.SetPostavkeKarte(prije) })

	if got := o.akti.PostavkeKarte(ctx); len(got.Podloge) != 1 || got.Podloge[0] != models.SatelitEsri {
		t.Fatalf("bez spremljenih: %+v", got)
	}
	nove := models.PostavkeKarte{
		Objekti: map[string]models.StilObjekta{models.StructureKindDam: {Boja: "#8b5a2b", Ikona: "brana"}},
		Podloge: []models.PodlogaKarte{{Naziv: "Topo", Vrsta: models.PodlogaXYZ, URL: "https://t.example/{z}/{x}/{y}.png"}},
	}
	for _, perms := range []*models.UserPermissions{nil, upravaSektoraP} {
		if err := o.akti.SpremiPostavkeKarte(ctx, perms, &nove); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("bez ovlasti: %v", err)
		}
	}

	losa := models.PostavkeKarte{Podloge: []models.PodlogaKarte{{Naziv: "Topo", Vrsta: models.PodlogaXYZ, URL: "http://t.example/{z}/{x}/{y}"}}}
	if err := o.akti.SpremiPostavkeKarte(ctx, upravaOrganizacije, &losa); err == nil ||
		err.Error() != "Postavke karata nisu spremljene. Topo: adresa mora početi s https://." {
		t.Fatalf("loša podloga: %v", err)
	}

	if err := o.akti.SpremiPostavkeKarte(ctx, upravaOrganizacije, &nove); err != nil {
		t.Fatal(err)
	}
	got := o.akti.PostavkeKarte(ctx)
	if got.Stil(models.StructureKindDam).Boja != "#8b5a2b" || len(got.Podloge) != 1 || got.Podloge[0].Naziv != "Topo" {
		t.Errorf("spremljeno: %+v", got)
	}
	if models.PostavkeKartePrograma().Podloge[0].Naziv != "Topo" {
		t.Error("postavke ne vrijede odmah")
	}

	if err := o.akti.SpremiPostavkeKarte(ctx, upravaOrganizacije, nil); err != nil {
		t.Fatal(err)
	}
	if got := o.akti.PostavkeKarte(ctx); got.Stil(models.StructureKindEmbankment).Boja != "#e8590c" || got.Podloge[0] != models.SatelitEsri {
		t.Errorf("vraćeno zadano: %+v", got)
	}

	// spremanje koje ne uspije ne mijenja postavke programa
	pokvariTablicu(t, o, "postavke")
	if err := o.akti.SpremiPostavkeKarte(ctx, upravaOrganizacije, &nove); err == nil {
		t.Fatal("spremanje u nedostupnu postavku javilo je uspjeh")
	}
	if err := o.akti.SpremiPostavkeKarte(ctx, upravaOrganizacije, nil); err == nil || !strings.Contains(err.Error(), "postavke") {
		t.Fatalf("vraćanje zadanog u nedostupnu postavku: %v", err)
	}
	if models.PostavkeKartePrograma().Podloge[0] != models.SatelitEsri {
		t.Error("neuspjelo spremanje promijenilo postavke programa")
	}
}
