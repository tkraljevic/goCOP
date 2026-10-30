package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/service"
)

// Lozinka e-pošte se ne sprema bez imena i lozinke, bez ključa čvora ni kad
// je poslužitelj odbije. Bez upisanog imena uzima se adresa iz profila.
func TestSpremanjeLozinkePosteOdbija(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	srv, err := posta.PokreniProbniEWS(`voda.int\vod`, "Domenska-Lozinka-1")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Zatvori()
	o.akti.SetPosta(srv.Postavke())
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	bezAdrese := *vod
	bezAdrese.Email = ""
	for _, x := range []struct {
		opis              string
		u                 *models.User
		korisnik, lozinka string
		poruka            string
	}{
		{"bez lozinke, ime iz profila", vod, "", "", "upišite korisničko ime"},
		{"bez imena i adrese", &bezAdrese, "", "x", "upišite korisničko ime"},
		{"kriva lozinka", vod, `voda.int\vod`, "kriva", "odbio"},
	} {
		if _, err := o.akti.SpremiRacunPoste(ctx, x.u, x.korisnik, x.lozinka); err == nil || !strings.Contains(err.Error(), x.poruka) {
			t.Errorf("%s: %v", x.opis, err)
		}
	}
	o.akti.SetKljuc(nil)
	if _, err := o.akti.SpremiRacunPoste(ctx, vod, `voda.int\vod`, "Domenska-Lozinka-1"); err == nil || !strings.Contains(err.Error(), "ključ čvora") {
		t.Errorf("bez ključa čvora: %v", err)
	}
	if n := o.sanducicOsobe(t, vod.ID); n != 0 {
		t.Errorf("odbijena lozinka je spremljena (%d)", n)
	}
	// bez ključa čvora pokretanje ne dodaje otiske lozinke računa, a ne pada
	if err := o.akti.PopuniOtiskeSanducica(ctx); err != nil {
		t.Errorf("pokretanje bez ključa: %v", err)
	}
}

// Greška baze pri pokretanju se javlja, a ne prešućuje: kad se lozinka iz
// ranijih izdanja ne da vezati uz poslužitelj, otisak lozinke računa ne da
// upisati ili nevaljala lozinka ne da obrisati.
func TestPokretanjeJavljaGreskuSpremljenihLozinki(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	o.akti.SetPosta(nedostupnaPosta)
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	if _, err := o.akti.SpremiRacunPoste(ctx, vod, "vod@voda.hr", "Domenska-Lozinka-1"); err != nil {
		t.Fatal(err)
	}
	pokreni := func(opis, priprema, okidac string) {
		t.Helper()
		if _, err := o.db.Exec(priprema); err != nil {
			t.Fatal(err)
		}
		if _, err := o.db.Exec(`CREATE TRIGGER zabrana ` + okidac + ` ON posta_racuni BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`); err != nil {
			t.Fatal(err)
		}
		if err := service.PripremiSpremljeneLozinke(ctx, o.akti, nil); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
			t.Errorf("%s: %v", opis, err)
		}
		if _, err := o.db.Exec(`DROP TRIGGER zabrana`); err != nil {
			t.Fatal(err)
		}
	}
	pokreni("vezanje uz poslužitelj", `UPDATE posta_racuni SET posluzitelj = ''`, `BEFORE UPDATE OF posluzitelj`)
	pokreni("otisak lozinke računa", `UPDATE posta_racuni SET posluzitelj = '`+nedostupnaPosta.Otisak()+`', otisak_lozinke = NULL`, `BEFORE UPDATE OF otisak_lozinke`)
	pokreni("brisanje nevaljale lozinke", `UPDATE posta_racuni SET otisak_lozinke = x'00'`, `BEFORE DELETE`)
}

// Akt koji se crta iznova (bez izvornika) nosi žig, ali ne i sken potpisa
// ovjeritelja; izvornik pri ovjeri nosi oboje.
func TestOtisciIspisaBezSkenaPotpisa(t *testing.T) {
	o := novaOkolinaAkta(t)
	o.akti.SetKljuc(kljucCvora(1))
	ctx := context.Background()
	if err := o.akti.SpremiPotpisSliku(ctx, o.rukovod, slikaPNG(t, 300, 100)); err != nil {
		t.Fatal(err)
	}
	a := o.nacrtDezurnog(t, models.AktUspostava, time.Now().Add(-time.Hour).Truncate(time.Minute))
	a, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, potvrdaPrimatelja)
	if err != nil {
		t.Fatal(err)
	}
	if o.akti.OtisciAkta(ctx, a).Potpis == nil {
		t.Fatal("izvornik pri ovjeri mora nositi sken potpisa")
	}
	ispis := o.akti.OtisciIspisa(ctx, a)
	if ispis.Potpis != nil || (ispis.Zig == nil) != (o.akti.OtisciAkta(ctx, a).Zig == nil) {
		t.Errorf("ispis bez izvornika: %+v", ispis)
	}
}
