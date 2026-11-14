package web

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Sedam dana na vrhu kišomjera: dan bez podatka nije nula nego „—”, a dan
// sa zbrojem 0 ostaje 0,0 (izmjereno suho).
func TestSedamDanaKisomjera(t *testing.T) {
	sada := time.Date(2026, 11, 13, 10, 0, 0, 0, models.Zagreb)
	dan := func(d int, mm float64) RedKise {
		return RedKise{Kad: time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC), Sati: 24, Mm: mm}
	}
	dani := sedamDana([]RedKise{dan(13, 2.5), dan(12, 0), dan(10, 7.1), dan(1, 50)}, sada)
	if len(dani) != 7 || dani[0].Dan.Day() != 7 || dani[6].Dan.Day() != 13 {
		t.Fatalf("dani: %+v", dani)
	}
	if dani[6].Mm == nil || *dani[6].Mm != 2.5 || dani[5].Mm == nil || *dani[5].Mm != 0 || dani[4].Mm != nil {
		t.Errorf("dan s kišom, suh dan i dan bez podatka nisu razdvojeni: %+v", dani)
	}
	if z := zbrojSedam(dani); z != 9.6 {
		t.Errorf("zbroj sedam dana %v, a treba 9,6 (stariji dan ne ulazi)", z)
	}

	p := KisomjerStranica{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Tocka:     models.Kisomjer{Code: "orahovica", Naziv: "Orahovica", Sliv: "G", Aktivan: true, Izvor: "DHMZ", IzvorSifra: "274", Latitude: 45.537, Longitude: 17.885},
		SlivNaziv: "Belišće → Osijek", Stranica: "ocitanja", SedamDana: dani, ZbrojSedam: 9.6, DrugihUMedjuslivu: 16, Pogled: "7", Korak: "satni"}
	html := iscrtaj(t, "kisomjer.html", p)
	for _, want := range []string{`href="/slivovi#sliv-G" class="povratna-poveznica">← Slivovi i kišomjeri`, "Oborine zadnjih sedam dana",
		"<strong>—</strong>", "<strong>0,0</strong>", "<strong>2,5</strong>", "Ukupno 9,6 mm.", "45,537 · 17,885",
		"Drugi u međuslivu</dt><dd>16", "šifra postaje 274", `href="/slivovi/kisomjer/orahovica/edit"`, `aria-current="page">Očitanja`} {
		if !strings.Contains(html, want) {
			t.Errorf("vrh kišomjera nema %q", want)
		}
	}
}

// Drugi u međuslivu broji ostale točke istog međusliva; bez servisa, bez
// međusliva ili kad se popis ne da pročitati piše 0.
func TestDrugiUMedjuslivu(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewKisomjerService(repository.NewKisomjerRepository(baza, ledger.New(baza, "test")))
	ctx := context.Background()
	for _, k := range []models.Kisomjer{
		{Code: "k-g-1", Naziv: "G ravnica 1", Sliv: "G", Latitude: 45.6, Longitude: 18.1, Aktivan: true},
		{Code: "k-g-2", Naziv: "G ravnica 2", Sliv: "G", Latitude: 45.7, Longitude: 18.2, Aktivan: true},
		{Code: "k-e-1", Naziv: "E pobrđe", Sliv: "E", Latitude: 45.6, Longitude: 17.8, Aktivan: true},
	} {
		k := k
		if err := svc.CreateKisomjer(ctx, &models.UserPermissions{IsGlobalAdmin: true}, &k); err != nil {
			t.Fatal(err)
		}
	}
	h := &KisomjerStranicaHandler{svc: func() *service.KisomjerService { return svc }}
	if n := h.drugiUMedjuslivu(ctx, models.Kisomjer{Code: "k-g-1", Sliv: "G"}); n != 1 {
		t.Errorf("u međuslivu G uz k-g-1 je %d drugih, a treba 1", n)
	}
	if n := h.drugiUMedjuslivu(ctx, models.Kisomjer{Code: "dhmz-x"}); n != 0 {
		t.Errorf("točka bez međusliva ima %d drugih", n)
	}
	if n := (&KisomjerStranicaHandler{svc: func() *service.KisomjerService { return nil }}).drugiUMedjuslivu(ctx, models.Kisomjer{Sliv: "G"}); n != 0 {
		t.Errorf("bez servisa %d drugih", n)
	}
	baza.Close()
	if n := h.drugiUMedjuslivu(ctx, models.Kisomjer{Code: "k-g-1", Sliv: "G"}); n != 0 {
		t.Errorf("zatvorena baza dala %d drugih", n)
	}
}
