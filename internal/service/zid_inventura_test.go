package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Inventura na zidu: zaključenje je redak, a otključavanje redak „Inventura
// vraćena u nacrt” (Odluke (13. 10. 2026.)). Spremanje nacrta nije redak,
// ni prije zaključenja ni poslije otključavanja. Bez toga bi se na zidu
// vidjela dva zaključenja iste inventure, bez ičega između.
func TestZidInventuraZakljucenaIVracenaUNacrt(t *testing.T) {
	s, sk, ctx, u, uprava, baza := pripremiMtsIBazu(t)
	zid := NewZidService(ledger.New(baza, "test"), nil, nil, s.repo, nil, nil, nil)
	inventure := func() []Dogadjaj {
		t.Helper()
		dog, _, err := zid.Zadnji(ctx, uprava, FiltarZida{Modul: ModulSredstva, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		var out []Dogadjaj
		for _, d := range dog {
			if strings.Contains(d.Naslov, "nventura") {
				out = append(out, d)
			}
		}
		return out
	}

	dan := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, -1)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: "lopata",
		Kolicina: 46, Datum: dan.AddDate(0, 0, -5)}); err != nil {
		t.Fatal(err)
	}
	p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	postaviPrebrojano(t, p, "lopata", models.OblikOsnovni, 44)
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}
	if r := inventure(); len(r) != 0 {
		t.Errorf("nacrt na zidu: %+v", r)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.OtkljucajPopis(ctx, &models.User{ID: uuid.New(), FullName: "Marko Marić"}, glavniAdmin, p.ID); err != nil {
		t.Fatal(err)
	}
	nacrt, err := s.Popis(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SpremiPopis(ctx, u, uprava, nacrt); err != nil {
		t.Fatal(err)
	}

	r := inventure()
	if len(r) != 2 {
		t.Fatalf("redci inventure na zidu: %+v", r)
	}
	vracena, zakljucena := r[0], r[1]
	naDan := "Centralno skladište Osijek · na dan " + dan.Format("2.1.2006.")
	if vracena.Naslov != "Inventura vraćena u nacrt" || vracena.Tekst != naDan || vracena.Sektor != "B" || vracena.AreaID != 34 ||
		vracena.Link != "/sredstva/popisi/"+p.ID || vracena.Tko != "" {
		t.Errorf("otključavanje na zidu: %+v", vracena)
	}
	if zakljucena.Naslov != "Zaključena inventura" || zakljucena.Tekst != naDan+" · 1 redak s razlikom" || zakljucena.Tko != "Skladištar Osijek" || zakljucena.AreaID != 34 {
		t.Errorf("zaključenje na zidu: %+v", zakljucena)
	}

	// ponovno zaključenje je novi redak zaključenja
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
		t.Fatal(err)
	}
	if r := inventure(); len(r) != 3 || r[0].Naslov != "Zaključena inventura" || r[1].Naslov != "Inventura vraćena u nacrt" {
		t.Errorf("poslije ponovnog zaključenja: %+v", r)
	}
	// tuđi sektor ne vidi ni jedno ni drugo
	tudji, _, err := zid.Zadnji(ctx, &models.UserPermissions{AllowedSectors: map[string]bool{"F": true}}, FiltarZida{Modul: ModulSredstva, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range tudji {
		if strings.Contains(d.Naslov, "nventura") {
			t.Errorf("tuđi sektor vidi %q", d.Naslov)
		}
	}
}

// Prethodna verzija koje nema (sažimanje knjige) ili se ne da pročitati ne
// čini nacrt otključanim
func TestZidNacrtBezPrethodneVerzije(t *testing.T) {
	_, _, ctx, _, _, baza := pripremiMtsIBazu(t)
	c := &opisivac{s: &ZidService{rec: ledger.New(baza, "test")}, ctx: ctx}
	for _, id := range []string{"", "01a00000-0000-7000-8000-000000000001"} {
		if c.bilaZakljucena(id) {
			t.Errorf("%q: prethodna verzija koje nema bila je zaključena", id)
		}
	}
	baza.Close()
	if c.bilaZakljucena("01a00000-0000-7000-8000-000000000001") {
		t.Error("zatvorena knjiga: prethodna verzija bila je zaključena")
	}
}
