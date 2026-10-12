package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Dijeljena postavka drugog koraka, dopuštene adrese za PIN i zapamćena
// računala na profilu osobe.

// Postavka drugog koraka: bez nje, bez spremišta postavki, uz neispravan
// zapis ili kad se ne da pročitati PIN je isključen, a domena zadana.
// Domena se sprema malim slovima bez @; neispravna domena ostavlja zadanu.
func TestOpcijeDrugogKoraka(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	zadano := service.OpcijePrijaveIzvana{Domena: service.ZadanaDomenaPIN}
	var nema *service.DrugiKorak
	if got := nema.Opcije(ctx); got != zadano {
		t.Errorf("bez drugog koraka: %+v", got)
	}
	bezPostavki := service.NewDrugiKorak(o.dkRepo, nil, o.repo, nil)
	if got := bezPostavki.Opcije(ctx); got != zadano {
		t.Errorf("bez spremišta postavki: %+v", got)
	}
	if got := o.dk.Opcije(ctx); got != zadano {
		t.Errorf("bez spremljene postavke: %+v", got)
	}

	postavke := repository.NewAktiRepository(o.baza, ledger.New(o.baza, "cvor-a"))
	for _, x := range []struct {
		opis, zapis string
		zeli        service.OpcijePrijaveIzvana
	}{
		{"uključen, domena s @ i velikim slovima", `{"pin": true, "domena": " @Primjer.HR "}`, service.OpcijePrijaveIzvana{PIN: true, Domena: "primjer.hr"}},
		{"uključen, domena bez točke", `{"pin": true, "domena": "primjer"}`, service.OpcijePrijaveIzvana{PIN: true, Domena: service.ZadanaDomenaPIN}},
		{"uključen, domena s razmakom", `{"pin": true, "domena": "primjer hr.hr"}`, service.OpcijePrijaveIzvana{PIN: true, Domena: service.ZadanaDomenaPIN}},
		{"isključen, bez domene", `{"pin": false}`, zadano},
		{"neispravan zapis", `{"pin": tru`, zadano},
	} {
		if err := postavke.SavePostavka(ctx, service.PostavkaPrijavaIzvana, x.zapis); err != nil {
			t.Fatal(err)
		}
		if got := o.dk.Opcije(ctx); got != x.zeli {
			t.Errorf("%s: %+v, želi %+v", x.opis, got, x.zeli)
		}
	}
	if err := postavke.SavePostavka(ctx, service.PostavkaPrijavaIzvana, `{"pin": true}`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`ALTER TABLE postavke RENAME TO postavke_nema`); err != nil {
		t.Fatal(err)
	}
	if got := o.dk.Opcije(ctx); got != zadano {
		t.Errorf("postavka se ne da pročitati: %+v", got)
	}
}

// PIN ide sam po sebi samo na ispravnu adresu bez imena u dopuštenoj
// domeni, bez obzira na velika slova i praznine na rubovima. Prazna adresa,
// adresa s imenom ili u zagradama i adresa druge domene (i poddomene) se
// odbijaju svaka svojom greškom.
func TestDopustenaAdresaZaPIN(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	izvan := "PIN se šalje samo na službenu adresu e-pošte (@voda.hr) ili na adresu koju je potvrdio administrator"
	for _, x := range []struct {
		adresa string
		zeli   error
		poruka string
	}{
		{"pero.peric@voda.hr", nil, ""},
		{"  Pero.Peric@VODA.HR ", nil, ""},
		{"", service.ErrNemaAdrese, ""},
		{"   ", service.ErrNemaAdrese, ""},
		{"Pero Perić <pero.peric@voda.hr>", service.ErrNeispravnaAdresa, ""},
		{"<pero.peric@voda.hr>", service.ErrNeispravnaAdresa, ""},
		{"pero.peric", service.ErrNeispravnaAdresa, `adresa e-pošte nije ispravna: "pero.peric"`},
		{"pero.peric@izvodjac.hr", service.ErrAdresaNijeDopustena, izvan},
		{"pero.peric@ured.voda.hr", service.ErrAdresaNijeDopustena, izvan},
	} {
		err := o.dk.DopustenaAdresa(ctx, x.adresa)
		if (x.zeli == nil) != (err == nil) || (x.zeli != nil && !errors.Is(err, x.zeli)) {
			t.Errorf("%q: %v, želi %v", x.adresa, err, x.zeli)
			continue
		}
		if x.poruka != "" && err.Error() != x.poruka {
			t.Errorf("%q: poruka %q, želi %q", x.adresa, err.Error(), x.poruka)
		}
	}
}

// Popis zapamćenih računala na profilu pokazuje računala koja još vrijede
// i označava preglednik iz kojeg se gleda; isteklo računalo se ne pokazuje.
// „Zaboravi ovo računalo” briše samo taj preglednik te osobe i šuti kad ga
// nema; „zaboravi sva” se tuđim očima odbija i ništa ne briše. Bez ključa
// čvora popisa nema, a greška čitanja se vraća.
func TestZapamcenaRacunalaNaProfilu(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	druga := o.korisnik(t, "aanic", "ana.anic@voda.hr")

	bezKljuca := service.NewDrugiKorak(o.dkRepo, nil, o.repo, nil)
	if _, err := bezKljuca.Racunala(ctx, o.osoba.ID, ""); !errors.Is(err, service.ErrNemaKljucaPrijave) {
		t.Fatalf("bez ključa čvora: %v", err)
	}

	prvi, err := o.dk.ZapamtiRacunalo(ctx, o.osoba, "", "203.0.113.9", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	o.pomakni(2 * time.Hour)
	drugi, err := o.dk.ZapamtiRacunalo(ctx, o.osoba, "", "203.0.113.10", "Chrome")
	if err != nil {
		t.Fatal(err)
	}
	oznake := func(token string) map[string]bool {
		t.Helper()
		r, err := o.dk.Racunala(ctx, o.osoba.ID, token)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, x := range r {
			out[x.UserAgent] = x.OvoRacunalo
		}
		return out
	}
	if got := oznake(prvi); len(got) != 2 || !got["Firefox"] || got["Chrome"] {
		t.Errorf("gleda se iz prvog preglednika: %v", got)
	}
	for _, token := range []string{"", "nije-token"} {
		if got := oznake(token); len(got) != 2 || got["Firefox"] || got["Chrome"] {
			t.Errorf("token %q: %v", token, got)
		}
	}

	// prvo računalo isteklo je, drugo još vrijedi
	o.pomakni(service.TrajanjeRacunala - time.Hour)
	if got := oznake(drugi); len(got) != 1 || !got["Chrome"] {
		t.Errorf("isteklo računalo na popisu: %v", got)
	}

	// bez ključa, s neispravnim tokenom i za drugu osobu ništa se ne briše
	if err := bezKljuca.ZaboraviOvo(ctx, o.osoba.ID, drugi); err != nil {
		t.Errorf("bez ključa: %v", err)
	}
	if err := o.dk.ZaboraviOvo(ctx, o.osoba.ID, "nije-token"); err != nil {
		t.Errorf("neispravan token: %v", err)
	}
	if err := o.dk.ZaboraviOvo(ctx, druga.ID, drugi); err != nil {
		t.Errorf("druga osoba: %v", err)
	}
	if got := oznake(drugi); len(got) != 1 {
		t.Fatalf("računalo obrisano bez razloga: %v", got)
	}
	if err := o.dk.ZaboraviOvo(ctx, o.osoba.ID, drugi); err != nil {
		t.Fatal(err)
	}
	if got := oznake(drugi); len(got) != 0 {
		t.Fatalf("računalo nije zaboravljeno: %v", got)
	}

	// istekli zapis prvog računala još je u bazi; zaboravi sva ga briše
	if _, err := o.dk.ZaboraviSva(ctx, o.osoba.ID, true); !errors.Is(err, service.ErrTudjimOcima) {
		t.Errorf("tuđim očima: %v", err)
	}
	if n, err := o.dk.ZaboraviSva(ctx, o.osoba.ID, false); err != nil || n != 1 {
		t.Errorf("zaboravi sva: %d %v", n, err)
	}
	if n, err := o.dk.ZaboraviSva(ctx, o.osoba.ID, false); err != nil || n != 0 {
		t.Errorf("zaboravi sva drugi put: %d %v", n, err)
	}

	if _, err := o.baza.Exec(`ALTER TABLE zapamcena_racunala RENAME TO zapamcena_racunala_nema`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.dk.Racunala(ctx, o.osoba.ID, prvi); err == nil {
		t.Error("greška čitanja nije vraćena")
	}
}

// Promjena lozinke opoziva drugi korak: briše zapamćena računala, prijave
// na čekanju i privremene kodove. Kad se bilo što od toga ne da obrisati,
// promjena javlja da je lozinka promijenjena, a opoziv nije prošao. Bez
// drugog koraka opoziv ne radi ništa.
func TestOpozivDrugogKorakaPriPromjeniLozinke(t *testing.T) {
	ctx := context.Background()
	var nema *service.DrugiKorak
	if err := nema.Opozovi(ctx, uuid.New()); err != nil {
		t.Errorf("bez drugog koraka: %v", err)
	}
	for _, tablica := range []string{"zapamcena_racunala", "prijave_na_cekanju", "kodovi_prijave"} {
		t.Run(tablica, func(t *testing.T) {
			o := okolinaDrugogKoraka(t)
			if _, err := o.baza.Exec(`ALTER TABLE ` + tablica + ` RENAME TO ` + tablica + `_nema`); err != nil {
				t.Fatal(err)
			}
			err := o.auth.ChangePassword(o.osoba.ID, "lozinka1", "lozinka2", uuid.Nil)
			if err == nil || !strings.HasPrefix(err.Error(), "lozinka je promijenjena, ali zapamćena računala nisu zaboravljena: ") {
				t.Fatalf("promjena lozinke: %v", err)
			}
			if err := o.auth.ProvjeriLozinku(o.osoba.ID, "lozinka2"); err != nil {
				t.Errorf("nova lozinka ne vrijedi: %v", err)
			}
		})
	}
}
