package service_test

// Obavijest o roku članstva u mreži i o kopiji ključa mreže (G9b): ide
// svakom aktivnom globalnom administratoru na adresu na koju smije ići PIN,
// računom za slanje PIN-a.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func TestObavijestiAdministratorima(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	if p, err := o.dk.PrimateljiObavijesti(ctx); len(p) != 0 || err != nil {
		t.Fatalf("bez globalnih administratora u bazi: %v %v", p, err)
	}
	o.korisnik(t, "vanjski", "vanjski@primjer.com")
	o.korisnik(t, "umirovljen", "umirovljen@voda.hr")
	if _, err := o.baza.Exec(`UPDATE users SET is_global_admin = 1 WHERE username IN ('uprava', 'pperic', 'vanjski', 'umirovljen')`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`UPDATE users SET is_active = 0 WHERE username = 'umirovljen'`); err != nil {
		t.Fatal(err)
	}
	primatelji, err := o.dk.PrimateljiObavijesti(ctx)
	if err != nil {
		t.Fatal(err)
	}
	adrese := map[string]bool{}
	for _, p := range primatelji {
		adrese[p.Adresa] = true
	}
	if len(primatelji) != 2 || !adrese["uprava@voda.hr"] || !adrese["pero.peric@voda.hr"] {
		t.Fatalf("primatelji %+v", primatelji)
	}
	for _, p := range primatelji {
		if err := o.dk.PosaljiObavijest(ctx, p, "goCOP: rok članstva u mreži", "Laptop Baranja: članstvo u mreži ističe 3. 11. 2026."); err != nil {
			t.Fatal(err)
		}
	}
	if m := o.posta.zadnja(t); o.posta.broj() != 2 || m.Predmet != "goCOP: rok članstva u mreži" || m.Za.Name == "" {
		t.Errorf("poruka %+v (%d poziva)", m, o.posta.broj())
	}

	// obavijesti imaju svoje brojilo: četvrta u 15 minuta istoj osobi stane,
	// a PIN te osobe i dalje ide
	var uprava, pero service.PrimateljObavijesti
	for _, p := range primatelji {
		if p.Adresa == "uprava@voda.hr" {
			uprava = p
		} else {
			pero = p
		}
	}
	for range 2 {
		if err := o.dk.PosaljiObavijest(ctx, uprava, "goCOP: kopija ključa mreže", "tekst"); err != nil {
			t.Fatal(err)
		}
	}
	if err := o.dk.PosaljiObavijest(ctx, uprava, "goCOP: kopija ključa mreže", "tekst"); !errors.Is(err, service.ErrSlanjeOgraniceno) {
		t.Errorf("četvrta obavijest u 15 minuta: %v", err)
	}
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Errorf("PIN nakon obavijesti: %v", err)
	}

	o.posta.greska = errors.New("veza je pukla")
	if err := o.dk.PosaljiObavijest(ctx, pero, "goCOP: rok", "tekst"); !errors.Is(err, service.ErrSlanjeZastalo) {
		t.Errorf("slanje bez veze: %v", err)
	}
	bezKljuca := service.NewDrugiKorak(o.dkRepo, repository.NewRacuniSustavaRepository(o.baza), o.repo, repository.NewAktiRepository(o.baza, ledger.New(o.baza, "cvor-a")))
	if err := bezKljuca.PosaljiObavijest(ctx, pero, "goCOP: rok", "tekst"); !errors.Is(err, service.ErrNemaKljucaPrijave) {
		t.Errorf("bez ključa: %v", err)
	}
}

// Kad se popis administratora ne da pročitati, primatelja nema, uz grešku.
func TestPrimateljiObavijestiBezBaze(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cvor-a")
	users := repository.NewUserRepository(baza, rec)
	dk := service.NewDrugiKorak(repository.NewDrugiKorakRepository(baza), repository.NewRacuniSustavaRepository(baza), users, repository.NewAktiRepository(baza, rec))
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if p, err := dk.PrimateljiObavijesti(context.Background()); p != nil || err == nil {
		t.Errorf("zatvorena baza: %v %v", p, err)
	}
	if _, err := users.GlobalniAdministratori(); err == nil {
		t.Error("popis iz zatvorene baze")
	}
}
