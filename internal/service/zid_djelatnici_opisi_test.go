package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

func noviOpisivac() *opisivac {
	return &opisivac{s: &ZidService{}, imena: map[string]string{}, podrucja: map[int]string{16: "B"}}
}

func verzijaZapisa(t *testing.T, entitet string, x any, arhivirana bool) ledger.Version {
	t.Helper()
	b, err := json.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	return ledger.Version{Entity: entitet, Payload: b, Archived: arhivirana, CreatedAt: time.Now()}
}

// Promjene računa: odlazak, isključena i uključena prijava te oznaka
// globalnog administratora; telefon nije promjena ovlasti
func TestPromjeneRacuna(t *testing.T) {
	dan := time.Date(2026, 11, 1, 0, 0, 0, 0, models.Zagreb)
	radi := models.User{IsActive: true}
	otisao := models.User{Odlazak: models.OdlazakMirovina, OdlazakDan: &dan, IsGlobalAdmin: false}
	for _, tc := range []struct {
		ime        string
		prije, sad models.User
		naslov     string
		tekst      string
		istaknut   bool
	}{
		{"odlazak", radi, otisao, "Odlazak iz službe", "odlazak u mirovinu 1. 11. 2026.", true},
		{"odlazak bez dana", radi, models.User{Odlazak: models.OdlazakOdlazak}, "Odlazak iz službe", "odlazak", true},
		{"isključena", radi, models.User{}, "Isključena prijava", "", false},
		{"uključena", models.User{}, radi, "Ponovno uključena prijava", "", false},
		{"dana oznaka", radi, models.User{IsActive: true, IsGlobalAdmin: true}, "Dana oznaka globalnog administratora", "", true},
		{"oduzeta uz isključenje", models.User{IsActive: true, IsGlobalAdmin: true}, models.User{}, "Isključena prijava", "oduzeta oznaka globalnog administratora", true},
		{"telefon", radi, models.User{IsActive: true, Phone: "031"}, "", "", false},
	} {
		naslov, promjene, vazno := promjeneRacuna(tc.prije, tc.sad)
		if naslov != tc.naslov || strings.Join(promjene, " · ") != tc.tekst || vazno != tc.istaknut {
			t.Errorf("%s: %q %q %v", tc.ime, naslov, promjene, vazno)
		}
	}
}

// Naslov retka zaduženja po prethodnoj verziji; izmjena bez osobe (istek
// koji program mijenja sam) nije redak
func TestNaslovZaduzenja(t *testing.T) {
	aktivno := models.Duty{IsActive: true, Role: models.RoleWaterGuard}
	opozvano := models.Duty{Role: models.RoleWaterGuard}
	drugaUloga := models.Duty{IsActive: true, Role: models.RoleMachinist}
	b, p := "B", 16
	rok := models.PrestanakNakonDana(2026, 12, 15)
	sRokom := models.Duty{IsActive: true, Role: models.RoleWaterGuard, SectorID: &b, AreaID: &p, ExpiresAt: &rok}
	duljiRok := sRokom
	dulji := models.PrestanakNakonDana(2026, 12, 31)
	duljiRok.ExpiresAt = &dulji
	for _, tc := range []struct {
		ime            string
		prije          *models.Duty
		sad            models.Duty
		nova, znaSeTko bool
		naslov         string
	}{
		{"nova", nil, aktivno, true, false, "Dodijeljeno zaduženje"},
		{"nova opozvana", nil, opozvano, true, false, ""},
		{"opoziv", &aktivno, opozvano, false, true, "Opozvano zaduženje"},
		{"ponovno", &opozvano, aktivno, false, true, "Dodijeljeno zaduženje"},
		{"izmjena", &aktivno, drugaUloga, false, true, "Izmijenjeno zaduženje"},
		{"izmjena bez osobe", &aktivno, drugaUloga, false, false, ""},
		{"ista", &aktivno, aktivno, false, true, ""},
		{"dulji rok", &sRokom, duljiRok, false, true, "Izmijenjeno zaduženje"},
		{"isti rok", &sRokom, sRokom, false, true, ""},
		{"bez prethodne", nil, aktivno, false, true, ""},
		{"opozvana bez prethodne", nil, opozvano, false, true, ""},
	} {
		if got := naslovZaduzenja(tc.prije, tc.sad, tc.nova, tc.znaSeTko); got != tc.naslov {
			t.Errorf("%s: %q", tc.ime, got)
		}
	}
}

// Opis zaduženja: naziv uz ulogu, doseg i rok
func TestOpisZaduzenja(t *testing.T) {
	c := noviOpisivac()
	b, p := "B", 16
	rok := models.PrestanakNakonDana(2026, 12, 15)
	for _, tc := range []struct {
		d    models.Duty
		opis string
	}{
		{models.Duty{Role: models.RoleWaterGuard, Title: "Vodočuvar"}, "Vodočuvar"},
		{models.Duty{Role: models.RoleWaterGuard, Title: "Čuvar nasipa", SectorID: &b, AreaID: &p, SectionCodes: " B.16.1 ", ExpiresAt: &rok},
			"Čuvar nasipa (vodočuvar) · " + models.Terms().Get("sektor") + " B · BP 16 · B.16.1 · do 15. 12. 2026."},
		{models.Duty{Role: models.RoleWaterGuard, IsticeSObranom: true, IsTemporary: true}, "Vodočuvar · dok traje obrana"},
		{models.Duty{Role: models.RoleWaterGuard, IsTemporary: true}, "Vodočuvar · privremeno"},
	} {
		if got := c.opisZaduzenja(tc.d); got != tc.opis {
			t.Errorf("%q, a treba %q", got, tc.opis)
		}
	}
	if maloPrvo("") != "" || maloPrvo("Čuvar") != "čuvar" {
		t.Error("maloPrvo")
	}
}

// Redak računa i zaduženja iz same verzije, bez prethodne: otvoren i
// obrisan račun, dodijeljeno zaduženje s imenom onoga tko ga je dao, a
// verzija koja se ne da pročitati ili nema prethodne nije redak
func TestRedakBezPrethodne(t *testing.T) {
	c := noviOpisivac()
	u := racunUKnjizi{User: models.User{ID: uuid.New(), FullName: "Pero Perić", IsGlobalAdmin: true}}
	var d Dogadjaj
	if !c.racun(&d, verzijaZapisa(t, "users", u, false), true) || d.Naslov != "Otvoren račun" || !strings.Contains(d.Tekst, "globalni administrator") {
		t.Errorf("otvoren račun: %+v", d)
	}
	d = Dogadjaj{}
	if !c.racun(&d, verzijaZapisa(t, "users", u, true), false) || d.Naslov != "Obrisan račun" || d.Link != "" {
		t.Errorf("obrisan račun: %+v", d)
	}
	d = Dogadjaj{}
	if c.racun(&d, verzijaZapisa(t, "users", u, false), false) || c.racun(&d, ledger.Version{Payload: []byte("{")}, true) {
		t.Error("račun bez prethodne ili nečitljiv je redak")
	}

	dao := uuid.New()
	c.imena[dao.String()] = "Ana Uprava"
	p := 16
	z := zaduzenjeUKnjizi{Duty: models.Duty{ID: uuid.New(), UserID: uuid.New(), Role: models.RoleWaterGuard, IsActive: true, AreaID: &p, AssignedBy: &dao}}
	d = Dogadjaj{}
	if !c.zaduzenje(&d, verzijaZapisa(t, "duties", z, false), true) || d.Tko != "Ana Uprava" || d.Sektor != "B" || d.AreaID != 16 {
		t.Errorf("dodijeljeno zaduženje: %+v", d)
	}
	if c.zaduzenje(&d, verzijaZapisa(t, "duties", z, true), false) || c.zaduzenje(&d, ledger.Version{Payload: []byte("{")}, true) {
		t.Error("obrisano ili nečitljivo zaduženje je redak")
	}
	if c.prethodna("", &z) {
		t.Error("prethodna bez oznake")
	}

	// obrisana prethodna verzija nije stanje prije izmjene
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	c.s.rec, c.ctx = ledger.New(baza, "cvor-a"), context.Background()
	obrisana, err := c.s.rec.Archive(c.ctx, baza, "duties", z.ID.String(), z)
	if err != nil {
		t.Fatal(err)
	}
	if c.prethodna(obrisana, &z) || c.prethodna("nema-takve", &z) {
		t.Error("obrisana ili nepostojeća prethodna verzija je pročitana")
	}
}
