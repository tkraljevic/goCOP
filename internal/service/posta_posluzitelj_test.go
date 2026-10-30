package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Spremljena lozinka e-pošte ide samo poslužitelju za koji je spremljena.
// Kad administrator upiše drugi poslužitelj, lozinka se ne otključava nego
// briše, i ne vraća se kad se poslužitelj vrati. Lozinka spremljena prije
// otiska poslužitelja veže se uz poslužitelj samo pri pokretanju čvora; pri
// uporabi se briše.
func TestLozinkaPosteIdeSamoSvomPosluzitelju(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	srv, err := posta.PokreniProbniEWS("voda.int\\vod", "Domenska-Lozinka-1")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Zatvori()
	pp := srv.Postavke()
	pp.Domena = "voda.int"
	o.akti.SetPosta(pp)
	if err := o.akti.SpremiPostu(ctx, o.admin, pp); err != nil {
		t.Fatal(err)
	}
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	spremi := func() {
		t.Helper()
		if _, err := o.akti.SpremiRacunPoste(ctx, vod, "vod@voda.hr", "Domenska-Lozinka-1"); err != nil {
			t.Fatal(err)
		}
	}
	spremi()
	if _, l, err := o.akti.RacunPosteOtkljucan(ctx, vod); err != nil || l != "Domenska-Lozinka-1" {
		t.Fatalf("isti poslužitelj: %q %v", l, err)
	}

	drugi := pp
	drugi.Posluzitelj = "https://drugi.primjer.test/EWS/Exchange.asmx"
	if err := o.akti.SpremiPostu(ctx, o.admin, drugi); err != nil {
		t.Fatal(err)
	}
	if _, l, err := o.akti.RacunPosteOtkljucan(ctx, vod); !errors.Is(err, service.ErrNemaLozinkePoste) ||
		!strings.Contains(err.Error(), "poslužitelj e-pošte promijenjen") || l != "" {
		t.Errorf("drugi poslužitelj: %q %v", l, err)
	}
	if n := o.sanducicOsobe(t, vod.ID); n != 0 {
		t.Errorf("lozinka za drugi poslužitelj nije obrisana (%d)", n)
	}
	if err := o.akti.SpremiPostu(ctx, o.admin, pp); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.akti.RacunPosteOtkljucan(ctx, vod); !errors.Is(err, service.ErrNemaLozinkePoste) {
		t.Errorf("vraćen poslužitelj vratio je obrisanu lozinku: %v", err)
	}

	// lozinka bez otiska poslužitelja ne vrijedi pri uporabi, nego se briše
	bezOtiska := func() {
		t.Helper()
		spremi()
		if _, err := o.db.Exec(`UPDATE posta_racuni SET posluzitelj = '' WHERE user_id = ?`, vod.ID.String()); err != nil {
			t.Fatal(err)
		}
	}
	bezOtiska()
	if _, l, err := o.akti.RacunPosteOtkljucan(ctx, vod); !errors.Is(err, service.ErrNemaLozinkePoste) || l != "" {
		t.Errorf("lozinka bez otiska pri uporabi: %q %v", l, err)
	}
	// pri pokretanju čvora lozinka iz ranijih izdanja veže se uz upisani poslužitelj
	bezOtiska()
	if err := service.PripremiSpremljeneLozinke(ctx, o.akti, nil); err != nil {
		t.Fatal(err)
	}
	var otisak string
	if err := o.db.QueryRow(`SELECT posluzitelj FROM posta_racuni WHERE user_id = ?`, vod.ID.String()).Scan(&otisak); err != nil || otisak != pp.Otisak() {
		t.Errorf("lozinka otprije otiska nije vezana uz poslužitelj: %q %v", otisak, err)
	}
	if _, l, err := o.akti.RacunPosteOtkljucan(ctx, vod); err != nil || l != "Domenska-Lozinka-1" {
		t.Fatalf("lozinka vezana pri pokretanju: %q %v", l, err)
	}
	// ni vezana lozinka ne ide poslužitelju upisanom poslije pokretanja
	if err := o.akti.SpremiPostu(ctx, o.admin, drugi); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.akti.RacunPosteOtkljucan(ctx, vod); !errors.Is(err, service.ErrNemaLozinkePoste) {
		t.Errorf("vezana lozinka otišla je drugom poslužitelju: %v", err)
	}

	// dok pošta nije podešena, lozinka se ne sprema, a lozinka iz ranijih
	// izdanja briše se pri pokretanju
	if err := o.akti.SpremiPostu(ctx, o.admin, pp); err != nil {
		t.Fatal(err)
	}
	bezOtiska()
	if err := o.akti.SpremiPostu(ctx, o.admin, posta.Postavke{Nacin: posta.NacinEWS}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.akti.SpremiRacunPoste(ctx, vod, "vod@voda.hr", "Domenska-Lozinka-1"); err == nil || !strings.Contains(err.Error(), "nije podešen") {
		t.Errorf("lozinka spremljena bez poslužitelja: %v", err)
	}
	if err := o.akti.PopuniOtiskeSanducica(ctx); err != nil {
		t.Fatal(err)
	}
	if n := o.sanducicOsobe(t, vod.ID); n != 0 {
		t.Errorf("lozinka bez otiska ostala je bez podešene pošte (%d)", n)
	}
}

// Lozinka računa za slanje PIN-a ide samo poslužitelju za koji je račun
// upisan; za drugi poslužitelj račun se briše, a PIN ne odlazi. Račun
// upisan prije otiska poslužitelja veže se uz poslužitelj samo pri
// pokretanju čvora; pri uporabi se briše.
func TestPINIdeSamoPosluziteljuPosiljatelja(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	posluzitelj := "owa.primjer.hr"
	o.dk.SetPosta(func(context.Context) posta.Postavke {
		return posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: posluzitelj}
	})
	racuni := repository.NewRacuniSustavaRepository(o.baza)
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Fatalf("isti poslužitelj: %v", err)
	}
	poslano := o.posta.broj()

	posluzitelj = "owa.drugi.test"
	if s, err := o.dk.Stanje(ctx); err != nil || s.Posiljatelj == nil {
		t.Fatalf("stanje: %+v %v", s, err)
	}
	// račun koji se ne da obrisati ostaje, ali PIN ni tada ne odlazi
	o.zabrani(t, "bez_brisanja", "DELETE", "racuni_sustava")
	o.pomakni(6 * time.Minute)
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("drugi poslužitelj, račun se ne briše: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER bez_brisanja`)
	o.pomakni(6 * time.Minute)
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); !errors.Is(err, service.ErrNemaPosiljatelja) {
		t.Errorf("drugi poslužitelj: %v", err)
	}
	if o.posta.broj() != poslano {
		t.Error("PIN je poslan na drugi poslužitelj")
	}
	if r, err := racuni.Racun(ctx, service.SustavPostaPIN); err != nil || r != nil {
		t.Errorf("račun za drugi poslužitelj nije obrisan: %+v %v", r, err)
	}

	spremi := func() {
		t.Helper()
		if _, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin", "tajna", "pin@voda.hr"); err != nil {
			t.Fatal(err)
		}
		o.izvrsi(t, `UPDATE racuni_sustava SET posluzitelj = ''`)
		o.pomakni(6 * time.Minute)
	}
	// račun bez otiska poslužitelja pri uporabi se briše, a PIN ne odlazi
	spremi()
	if s, err := o.dk.Stanje(ctx); err != nil || s.Posiljatelj == nil || !s.Posiljatelj.ZaDrugiPosluzitelj {
		t.Errorf("stanje računa bez otiska: %+v %v", s, err)
	}
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); !errors.Is(err, service.ErrNemaPosiljatelja) {
		t.Errorf("račun otprije otiska pri uporabi: %v", err)
	}
	if o.posta.broj() != poslano {
		t.Error("PIN je poslan računom bez otiska poslužitelja")
	}
	// pri pokretanju čvora račun iz ranijih izdanja veže se uz upisani poslužitelj
	spremi()
	if err := o.dk.VeziPosiljateljaOtprije(ctx); err != nil {
		t.Fatal(err)
	}
	ocekivano := posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: posluzitelj}.Otisak()
	if r, err := racuni.Racun(ctx, service.SustavPostaPIN); err != nil || r == nil || r.Posluzitelj != ocekivano {
		t.Errorf("račun otprije otiska nije vezan uz poslužitelj: %+v %v", r, err)
	}
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Fatalf("račun vezan pri pokretanju: %v", err)
	}
	// vezan račun s otiskom vezanje ne mijenja
	posluzitelj = "owa.treci.test"
	if err := o.dk.VeziPosiljateljaOtprije(ctx); err != nil {
		t.Fatal(err)
	}
	if r, err := racuni.Racun(ctx, service.SustavPostaPIN); err != nil || r == nil || r.Posluzitelj != ocekivano {
		t.Errorf("vezanje je promijenilo otisak računa: %+v %v", r, err)
	}
	// bez podešene pošte račun iz ranijih izdanja briše se pri pokretanju
	posluzitelj = "owa.drugi.test"
	spremi()
	posluzitelj = ""
	if err := o.dk.VeziPosiljateljaOtprije(ctx); err != nil {
		t.Fatal(err)
	}
	if r, err := racuni.Racun(ctx, service.SustavPostaPIN); err != nil || r != nil {
		t.Errorf("račun bez otiska ostao je bez podešene pošte: %+v %v", r, err)
	}
	if err := o.dk.VeziPosiljateljaOtprije(ctx); err != nil {
		t.Errorf("bez računa: %v", err)
	}
}

// nedostupnaPosta je poslužitelj e-pošte na kojemu nitko ne sluša: lozinka
// se uz njega sprema bez provjere, uz upozorenje
var nedostupnaPosta = posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: "https://127.0.0.1:1/EWS/Exchange.asmx"}

// spremiBezProvjere sprema lozinku e-pošte dok poslužitelj nije dostupan,
// pa je on ne provjeri, i veže je uz poslužitelj za: tako u bazi stoji i
// lozinka koju poslužitelj poslije odbije. Zadane postavke pošte zatim su za.
func (o *okolinaOvlasti) spremiBezProvjere(t *testing.T, u *models.User, korisnik, lozinka string, za posta.Postavke) {
	t.Helper()
	o.akti.SetPosta(nedostupnaPosta)
	if _, err := o.akti.SpremiRacunPoste(context.Background(), u, korisnik, lozinka); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`UPDATE posta_racuni SET posluzitelj = ? WHERE user_id = ?`, za.Otisak(), u.ID.String()); err != nil {
		t.Fatal(err)
	}
	o.akti.SetPosta(za)
}
