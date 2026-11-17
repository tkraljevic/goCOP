package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// sviNaPut uključuje put za prijenos, izdavanje i povrat, kako će biti kad
// odredišta dobiju zaprimanje
func sviNaPut(s *MtsService) {
	s.naPut = map[string]bool{models.PrometPrijenos: true, models.PrometIzdano: true, models.PrometPovrat: true}
}

// zaprimiSve zaprima sve što je na putu na dan slanja, kao da je odredište
// sve odmah preuzelo; za testove kojima put robe nije tema
func zaprimiSve(t *testing.T, s *MtsService, ctx context.Context, u *models.User) {
	t.Helper()
	sve, err := s.NaPutu(ctx, repository.FiltarPosiljki{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range sve {
		if _, err := s.Zaprimi(ctx, u, &models.UserPermissions{IsGlobalAdmin: true}, Zaprimanje{VezaID: p.Slanje.VezaID, Kolicina: p.NaPutu(), Datum: p.Slanje.Datum}); err != nil {
			t.Fatal(err)
		}
	}
}

// Prijenos među skladištima: izvor odmah gubi robu, odredište je dobiva tek
// zaprimanjem, djelomično, a ostatak se otpisuje kao manjak pri prijevozu
// uz razlog (Odluke 16. 11. 2026., Na putu, 3).
func TestNaPutuPrijenos(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	sviNaPut(s)
	drugo := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "CS Draž", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, drugo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 100}); err != nil {
		t.Fatal(err)
	}
	redci, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: sk.ID, NaSkladisteID: drugo.ID, VrstaID: "lopata", Kolicina: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 1 || !redci[0].IdeNaPut() || redci[0].PremaSkladisteID != drugo.ID {
		t.Fatalf("slanje: %+v", redci)
	}
	veza := redci[0].VezaID
	if a, b := kolicinaU(t, s, ctx, sk.ID, "lopata"), kolicinaU(t, s, ctx, drugo.ID, "lopata"); a != 70 || b != 0 {
		t.Fatalf("poslije slanja: izvor %v, odredište %v", a, b)
	}
	stize, err := s.NaPutu(ctx, repository.FiltarPosiljki{PremaSkladisteID: drugo.ID})
	if err != nil || len(stize) != 1 || stize[0].NaPutu() != 30 || stize[0].PremaSkladisteNaziv != "CS Draž" || stize[0].Slanje.SkladisteNaziv != sk.Naziv {
		t.Fatalf("stiže: %+v %v", stize, err)
	}

	if _, err := s.Zaprimi(ctx, u, uprava, Zaprimanje{VezaID: veza, Kolicina: 31}); err == nil || !strings.Contains(err.Error(), "Na putu je 30 kom") {
		t.Errorf("zaprimanje više nego je na putu: %v", err)
	}
	red, err := s.Zaprimi(ctx, u, uprava, Zaprimanje{VezaID: veza, Kolicina: 25, Preuzeo: "Ivo Ivić"})
	if err != nil {
		t.Fatal(err)
	}
	if red.SkladisteID != drugo.ID || red.Kolicina != 25 || red.VezaID != veza || red.Napomena != "iz "+sk.Naziv || red.Vrsta != models.PrometPrijenos {
		t.Errorf("redak zaprimanja: %+v", red)
	}
	if b := kolicinaU(t, s, ctx, drugo.ID, "lopata"); b != 25 {
		t.Errorf("poslije djelomičnog zaprimanja: %v", b)
	}
	if p, err := s.NaPutu(ctx, repository.FiltarPosiljki{Sektor: "B"}); err != nil || len(p) != 1 || p[0].NaPutu() != 5 {
		t.Fatalf("ostatak na putu: %+v %v", p, err)
	}
	if _, err := s.OtpisiManjak(ctx, u, &models.UserPermissions{AllowedSectors: map[string]bool{"F": true}}, veza, "nestalo"); err == nil {
		t.Error("manjak otpisuje tko ne smije zaprimiti")
	}

	if _, err := s.OtpisiManjak(ctx, u, uprava, veza, " "); err == nil {
		t.Error("manjak bez razloga")
	}
	otpis, err := s.OtpisiManjak(ctx, u, uprava, veza, "vreće poderane pri utovaru")
	if err != nil {
		t.Fatal(err)
	}
	if len(otpis) != 2 || otpis[1].Vrsta != models.PrometOtpis || otpis[1].Kolicina != -5 || !strings.Contains(otpis[1].Napomena, "poderane") {
		t.Errorf("otpis manjka: %+v", otpis)
	}
	if b := kolicinaU(t, s, ctx, drugo.ID, "lopata"); b != 25 {
		t.Errorf("manjak ne smije promijeniti stanje odredišta: %v", b)
	}
	if p, err := s.NaPutu(ctx, repository.FiltarPosiljki{}); err != nil || len(p) != 0 {
		t.Errorf("poslije manjka ništa nije na putu: %+v %v", p, err)
	}
	if _, err := s.OtpisiManjak(ctx, u, uprava, veza, "opet"); err == nil {
		t.Error("manjak zatvorene pošiljke")
	}
	if _, err := s.Zaprimi(ctx, u, uprava, Zaprimanje{VezaID: veza, Kolicina: 1}); err == nil || !strings.Contains(err.Error(), "više nema na putu") {
		t.Errorf("zatvorena pošiljka: %v", err)
	}
}

// Izdavanje na teren i povrat u skladište idu istim putem: teren zaprima
// rukovoditelj dionice ili vodočuvar, a skladište skladištar ili uprava.
func TestNaPutuTerenIPovrat(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	sviNaPut(s)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPunjeno, Kolicina: 1000}); err != nil {
		t.Fatal(err)
	}
	obrana := uuid.New().String()
	redci, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPunjeno,
		Kolicina: 400, AreaID: 34, SectionCode: "B.34.1", JournalID: obrana})
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 1 || redci[0].PremaAreaID != 34 || redci[0].PremaSectionCode != "B.34.1" {
		t.Fatalf("izdavanje: %+v", redci)
	}
	if teren, err := s.NaTerenu(ctx, obrana, ""); err != nil || len(teren) != 0 {
		t.Fatalf("na terenu prije zaprimanja: %+v %v", teren, err)
	}

	sektor, podrucje := "B", 34
	vodocuvar := &models.UserPermissions{PodrucjaDionica: map[int]bool{34: true}}
	if _, err := s.Zaprimi(ctx, u, &models.UserPermissions{AllowedSectors: map[string]bool{"F": true}}, Zaprimanje{VezaID: redci[0].VezaID, Kolicina: 400}); err == nil || !strings.Contains(err.Error(), "zaprima odredište") {
		t.Errorf("tuđi sektor zaprima: %v", err)
	}
	if _, err := s.Zaprimi(ctx, u, vodocuvar, Zaprimanje{VezaID: redci[0].VezaID, Kolicina: 400}); err != nil {
		t.Fatal(err)
	}
	teren, err := s.NaTerenu(ctx, obrana, "")
	if err != nil || len(teren) != 1 || teren[0].Kolicina != 400 || teren[0].SectionCode != "B.34.1" || teren[0].AreaID != podrucje {
		t.Fatalf("na terenu poslije zaprimanja: %+v %v", teren, err)
	}

	povrat, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPovrat, SkladisteID: sk.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPunjeno,
		Kolicina: 100, AreaID: 34, SectionCode: "B.34.1", JournalID: obrana})
	if err != nil {
		t.Fatal(err)
	}
	if len(povrat) != 1 || povrat[0].PremaSkladisteID != sk.ID || povrat[0].SkladisteID != "" {
		t.Fatalf("povrat: %+v", povrat)
	}
	if a := kolicinaU(t, s, ctx, sk.ID, "vrece-50x80"); a != 600 {
		t.Fatalf("skladište prije zaprimanja povrata: %v", a)
	}

	// dežurni COP-a sektora zaprima umjesto skladišta, ali samo uz ime osobe koja je robu preuzela
	rok := time.Now().Add(24 * time.Hour)
	dezurni := &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}, User: models.User{Duties: []models.Duty{{
		Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true, ExpiresAt: &rok}}}}
	if _, err := s.Zaprimi(ctx, u, dezurni, Zaprimanje{VezaID: povrat[0].VezaID, Kolicina: 100}); err == nil || !strings.Contains(err.Error(), "ime osobe") {
		t.Errorf("dežurni bez imena: %v", err)
	}
	if _, err := s.Zaprimi(ctx, u, dezurni, Zaprimanje{VezaID: povrat[0].VezaID, Kolicina: 100, Preuzeo: "Marko Marić", Datum: time.Now().AddDate(0, 0, 2)}); err == nil {
		t.Error("zaprimanje unaprijed")
	}
	if _, err := s.Zaprimi(ctx, u, dezurni, Zaprimanje{VezaID: povrat[0].VezaID, Kolicina: 100, Preuzeo: "Marko Marić", Datum: time.Now().AddDate(0, 0, -3)}); err == nil || !strings.Contains(err.Error(), "prije nego je poslana") {
		t.Errorf("zaprimanje prije slanja: %v", err)
	}
	if _, err := s.Zaprimi(ctx, u, dezurni, Zaprimanje{VezaID: povrat[0].VezaID, Kolicina: 100, Preuzeo: "Marko Marić"}); err != nil {
		t.Fatal(err)
	}
	if a := kolicinaU(t, s, ctx, sk.ID, "vrece-50x80"); a != 700 {
		t.Errorf("skladište poslije povrata: %v", a)
	}
}

// Dok vrsta zahvata ne ide na put, potez se upisuje odjednom kao i prije, a
// stari potezi bez odredišta nisu na putu.
func TestNaPutuIskljucen(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	s.naPut = map[string]bool{}
	drugo := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "CS Draž", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, drugo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 10}); err != nil {
		t.Fatal(err)
	}
	redci, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: sk.ID, NaSkladisteID: drugo.ID, VrstaID: "lopata", Kolicina: 4})
	if err != nil || len(redci) != 2 || redci[0].IdeNaPut() {
		t.Fatalf("prijenos odjednom: %+v %v", redci, err)
	}
	if p, err := s.NaPutu(ctx, repository.FiltarPosiljki{}); err != nil || len(p) != 0 {
		t.Errorf("stari potez na putu: %+v %v", p, err)
	}
	if _, err := s.Zaprimi(ctx, u, uprava, Zaprimanje{VezaID: redci[0].VezaID, Kolicina: 4}); err == nil {
		t.Error("zaprimanje poteza koji nije na putu")
	}
	if _, err := s.Zaprimi(ctx, nil, uprava, Zaprimanje{VezaID: redci[0].VezaID}); err == nil {
		t.Error("zaprimanje bez prijave")
	}
	if _, err := s.OtpisiManjak(ctx, nil, uprava, redci[0].VezaID, "x"); err == nil {
		t.Error("otpis bez prijave")
	}
	if _, err := s.Zaprimi(ctx, u, uprava, Zaprimanje{}); err == nil {
		t.Error("zaprimanje bez pošiljke")
	}
}
