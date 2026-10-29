package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
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

// Karta bez interneta je zasebna postavka: spremanje izgleda, „Vrati
// zadano” i vlastita ikona ne pišu je u zapis postavki karata i ne miču
// je, a micanje karte piše prazan opis. Opis sprema samo uprava
// organizacije; neispravan opis i neuspjelo spremanje ne mijenjaju kartu
// programa.
func TestSpremanjePostavkiKarteNeNosiKartu(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	prijeP, prijeK := models.PostavkeKartePrograma(), models.VektorskaKartaPrograma()
	t.Cleanup(func() { models.SetPostavkeKarte(prijeP); models.SetVektorskaKarta(prijeK) })
	models.SetVektorskaKarta(nil)

	zapis := func(id string) string {
		t.Helper()
		var v string
		if err := o.baza.QueryRow(`SELECT vrijednost FROM postavke WHERE id = ?`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	karta := models.VektorskaKarta{Naziv: " Baranja ", Otisak: strings.Repeat("ab", 32), Bajtova: 150,
		Obuhvat: [4]float64{18.3, 45.5, 18.95, 45.95}, NajviseZ: 15, Dio: 100, Dijelovi: []string{strings.Repeat("cd", 32), strings.Repeat("ef", 32)}}

	if o.akti.KartaBezInterneta(ctx) != nil {
		t.Fatal("karta bez zapisa")
	}
	for _, perms := range []*models.UserPermissions{nil, upravaSektoraP} {
		if err := o.akti.SpremiKartuBezInterneta(ctx, perms, &karta); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("bez ovlasti: %v", err)
		}
	}
	losa := karta
	losa.Dijelovi = losa.Dijelovi[:1]
	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, &losa); err == nil || !strings.Contains(err.Error(), "dijelova") {
		t.Fatalf("loš opis: %v", err)
	}
	verzijaOpisa := func() int {
		t.Helper()
		var n int
		if err := o.baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ?`,
			repository.EntityPostavke, repository.PostavkaKartaBezInterneta).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, &karta); err != nil {
		t.Fatal(err)
	}
	// ista karta učitana ponovno (npr. njezin paket nakon što je opis već
	// stigao razmjenom) ne piše novu verziju
	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, &karta); err != nil || verzijaOpisa() != 1 {
		t.Fatalf("ista karta: %d verzija opisa, %v", verzijaOpisa(), err)
	}
	if k := o.akti.KartaBezInterneta(ctx); k == nil || k.Naziv != "Baranja" || len(k.Dijelovi) != 2 {
		t.Fatalf("spremljena karta: %+v", k)
	}
	if k := models.VektorskaKartaPrograma(); k == nil || k.Naziv != "Baranja" {
		t.Fatalf("karta ne vrijedi odmah: %+v", k)
	}

	ik, err := models.ProciscenaIkona("Crpka", []byte(`<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="5"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	izgled := models.PostavkeKarte{Podloge: []models.PodlogaKarte{}, SKrugom: true, Ikone: []models.VlastitaIkona{ik}, Vektorska: &karta}
	for opis, p := range map[string]*models.PostavkeKarte{"izgled s ikonom": &izgled, "zadano": nil} {
		if err := o.akti.SpremiPostavkeKarte(ctx, upravaOrganizacije, p); err != nil {
			t.Fatalf("%s: %v", opis, err)
		}
		if v := zapis(repository.PostavkaKarta); strings.Contains(v, "vektorska") || strings.Contains(v, karta.Otisak) {
			t.Errorf("%s: zapis postavki karata nosi kartu: %s", opis, v)
		}
		if o.akti.KartaBezInterneta(ctx) == nil || models.VektorskaKartaPrograma() == nil || models.PostavkeKartePrograma().Vektorska == nil {
			t.Errorf("%s: spremanje postavki karata maknulo je kartu", opis)
		}
	}
	var uKnjizi, sKartom int
	if err := o.baza.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CAST(payload AS TEXT) LIKE '%vektorska%'), 0) FROM record_versions WHERE entity = ? AND entity_id = ?`,
		repository.EntityPostavke, repository.PostavkaKarta).Scan(&uKnjizi, &sKartom); err != nil || uKnjizi != 2 || sKartom != 0 {
		t.Errorf("verzije postavki karata u knjizi: %d, s kartom %d, %v", uKnjizi, sKartom, err)
	}

	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, nil); err != nil {
		t.Fatal(err)
	}
	if v := zapis(repository.PostavkaKartaBezInterneta); v != "" || o.akti.KartaBezInterneta(ctx) != nil || models.VektorskaKartaPrograma() != nil {
		t.Errorf("maknuta karta: %q", v)
	}
	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, nil); err != nil || verzijaOpisa() != 2 {
		t.Errorf("ponovno micanje: %d verzija opisa, %v", verzijaOpisa(), err)
	}
	// opis koji je stigao s drugog čvora, s drugim redom polja, isti je opis
	drugi := `{"dijelovi":["` + karta.Dijelovi[0] + `","` + karta.Dijelovi[1] + `"],"dio":100,"najvise_z":15,"obuhvat":[18.3,45.5,18.95,45.95],` +
		`"bajtova":150,"otisak":"` + karta.Otisak + `","naziv":"Baranja"}`
	if _, err := o.baza.Exec(`UPDATE postavke SET vrijednost = ? WHERE id = ?`, drugi, repository.PostavkaKartaBezInterneta); err != nil {
		t.Fatal(err)
	}
	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, &karta); err != nil || verzijaOpisa() != 2 {
		t.Errorf("opis s drugog čvora: %d verzija opisa, %v", verzijaOpisa(), err)
	}
	if models.VektorskaKartaPrograma() == nil {
		t.Error("karta koja je već zapisana ne vrijedi u programu")
	}
	// drukčiji opis iste datoteke (drugi naziv) je nova verzija
	preimenovana := karta
	preimenovana.Naziv = "Baranja i Slavonija"
	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, &preimenovana); err != nil || verzijaOpisa() != 3 {
		t.Errorf("preimenovana karta: %d verzija opisa, %v", verzijaOpisa(), err)
	}

	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, &karta); err != nil {
		t.Fatal(err)
	}
	pokvariTablicu(t, o, "postavke")
	if err := o.akti.SpremiKartuBezInterneta(ctx, upravaOrganizacije, nil); err == nil {
		t.Fatal("micanje karte iz nedostupne postavke javilo je uspjeh")
	}
	if models.VektorskaKartaPrograma() == nil {
		t.Error("neuspjelo micanje maknulo je kartu programa")
	}
	if o.akti.KartaBezInterneta(ctx) != nil {
		t.Error("karta bez baze postavki")
	}
}
