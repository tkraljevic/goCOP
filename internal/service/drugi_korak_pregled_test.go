package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Prekinuto slanje (preglednik je odustao, ili Postar vrati
// context.Canceled) nije kvar poslužitelja: slanje drugima ne stoji pet
// minuta, a zahtjev koji je odustao ne prekida poruku na pola.
func TestPrekinutoSlanjeNeZaustavljaDruge(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()

	o.posta.usred = func(context.Context) error { return context.Canceled }
	p, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "203.0.113.9", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.PINPoslan || !errors.Is(p.Razlog, service.ErrSlanjeZastalo) {
		t.Fatalf("prekinuto slanje: %+v", p)
	}
	if st, _ := o.dk.Stanje(ctx); !st.StankaDo.IsZero() {
		t.Fatalf("prekid je zaustavio slanje do %s", st.StankaDo)
	}
	o.posta.usred = nil
	ana := o.korisnik(t, "ana", "ana@voda.hr")
	if p, err := o.dk.ZapocniPrijavu(ctx, ana, "203.0.113.10", ""); err != nil || !p.PINPoslan {
		t.Fatalf("slanje drugoj osobi nakon prekida: %+v %v", p, err)
	}

	// zahtjev odustane usred slanja: poruka ipak ode, prijava se spremi
	zahtjev, odustani := context.WithCancel(ctx)
	o.posta.usred = func(c context.Context) error {
		odustani()
		return c.Err()
	}
	ivo := o.korisnik(t, "ivo", "ivo@voda.hr")
	p, err = o.dk.ZapocniPrijavu(zahtjev, ivo, "203.0.113.11", "")
	if err != nil || !p.PINPoslan {
		t.Fatalf("slanje je ovisilo o zahtjevu: %+v %v", p, err)
	}
	o.posta.usred = nil
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, o.posta.pin(t)); err != nil {
		t.Fatalf("PIN poslan dok je zahtjev odustao: %v", err)
	}

	// i novi PIN: zahtjev odustane usred ponovnog slanja, a novi PIN vrijedi
	p, err = o.dk.ZapocniPrijavu(ctx, ivo, "203.0.113.11", "")
	if err != nil || !p.PINPoslan {
		t.Fatalf("druga prijava: %+v %v", p, err)
	}
	o.pomakni(service.RazmakPonovnogSlanja + time.Second)
	zahtjev, odustani = context.WithCancel(ctx)
	o.posta.usred = func(context.Context) error {
		odustani()
		return nil
	}
	if x, err := o.dk.PonovnoPosalji(zahtjev, p.Token); err != nil || !x.PINPoslan {
		t.Fatalf("ponovno slanje je ovisilo o zahtjevu: %+v %v", x, err)
	}
	o.posta.usred = nil
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, o.posta.pin(t)); err != nil {
		t.Fatalf("novi PIN poslan dok je zahtjev odustao: %v", err)
	}
}

// Red za slanje čeka se najviše IstekSlanja: kad je slanje zauzeto dulje,
// prijava odmah dobije ErrSlanjeZastalo (stranica nudi kodove i novi PIN),
// bez stanke za ostale.
func TestCekanjeRedaZaSlanjeJeOgraniceno(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	o.dk.SetIstekSlanja(100 * time.Millisecond)

	usao, pusti := make(chan struct{}), make(chan struct{})
	var jednom sync.Once
	o.posta.usred = func(context.Context) error {
		jednom.Do(func() { close(usao) })
		<-pusti
		return nil
	}
	sigurnosno := time.AfterFunc(5*time.Second, func() { close(pusti) })
	defer sigurnosno.Stop()
	prvi := make(chan *service.PocetakPrijave, 1)
	go func() {
		p, _ := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
		prvi <- p
	}()
	<-usao

	ana := o.korisnik(t, "ana", "ana@voda.hr")
	pocetak := time.Now()
	p, err := o.dk.ZapocniPrijavu(ctx, ana, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if trajalo := time.Since(pocetak); trajalo > 2*time.Second {
		t.Fatalf("čekanje reda trajalo je %s", trajalo)
	}
	if p.PINPoslan || !errors.Is(p.Razlog, service.ErrSlanjeZastalo) {
		t.Fatalf("zauzeto slanje: %+v", p)
	}
	if sigurnosno.Stop() {
		close(pusti)
	}
	if p := <-prvi; p == nil || !p.PINPoslan {
		t.Fatalf("prvo slanje: %+v", p)
	}
	if st, _ := o.dk.Stanje(ctx); !st.StankaDo.IsZero() {
		t.Fatal("čekanje reda ne smije zaustaviti slanje")
	}
}

// Provjera lozinke pošiljatelja i probni PIN idu novom, još neprijavljenom
// vezom; svako slanje ide skupom veza računa sustava, ne osobnog sandučića.
func TestPosiljateljNovomVezomISvojimKlijentom(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	o.posta.adresar = []posta.Kontakt{{Ime: "PIN", Email: "pin@voda.hr"}}
	o.posta.postavke = nil
	if _, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin", "tajna", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Fatal(err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{"prijavi posta-pin true", "imenik posta-pin true", "posalji posta-pin true", "posalji posta-pin false"}
	if got := strings.Join(o.posta.postavke, "|"); got != strings.Join(want, "|") {
		t.Fatalf("pozivi pošte:\n%s\nočekivano:\n%s", got, strings.Join(want, "|"))
	}
}

// Pokušaj se zauzme prije usporedbe: od 50 istodobnih krivih unosa za istu
// prijavu na čekanju uspoređuje se najviše pet, pa osoba nakon toga nije
// zaključana (deset krivih u satu) i novom prijavom ulazi točnim PIN-om.
func TestIstodobniKodoviNajvisePetPoPrijavi(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	p, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if err != nil {
		t.Fatal(err)
	}
	krivi := "000000"
	if o.posta.pin(t) == krivi {
		krivi = "111111"
	}
	const n = 50
	var wg sync.WaitGroup
	kreni := make(chan struct{})
	greske := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-kreni
			_, greske[i] = o.dk.ProvjeriKod(ctx, p.Token, krivi)
		}(i)
	}
	close(kreni)
	wg.Wait()
	krivih := 0
	for _, err := range greske {
		switch {
		case errors.Is(err, service.ErrKrivKod):
			krivih++
		case errors.Is(err, service.ErrPrevisePokusaja), errors.Is(err, service.ErrDrugiKorakIstekao),
			errors.Is(err, service.ErrKodoviZakljucani): // deset unosa osobe već je u tijeku
		default:
			t.Fatalf("istodobni unos: %v", err)
		}
	}
	if krivih > service.NajviseKrivihUnosa-1 {
		t.Fatalf("krivih s preostalim pokušajima: %d", krivih)
	}
	o.pomakni(2 * time.Minute)
	p, err = o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if err != nil || !p.PINPoslan {
		t.Fatalf("nova prijava: %+v %v", p, err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, o.posta.pin(t)); err != nil {
		t.Fatalf("točan PIN nakon istodobnih krivih (uspoređeno ih je više od pet): %v", err)
	}
}

// Razlog neposlanog PIN-a stoji uz prijavu na čekanju: stranica ga zna i
// nakon krivog unosa, a nestane kad novi PIN ode.
func TestRazlogNeposlanogPINaOstaje(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	o.posta.greska = errors.New("veza je pukla")
	p, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", "")
	if err != nil || p.PINPoslan || !errors.Is(p.Razlog, service.ErrSlanjeZastalo) {
		t.Fatalf("početak: %+v %v", p, err)
	}
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, "R1-AAAA-BBBB"); !errors.Is(err, service.ErrKrivKod) {
		t.Fatalf("krivi kod: %v", err)
	}
	st, err := o.dk.Cekanje(ctx, p.Token)
	if err != nil || st.PINPoslan || !errors.Is(st.Razlog, service.ErrSlanjeZastalo) || !service.ProlazanRazlog(st.Razlog) {
		t.Fatalf("stanje nakon krivog koda: %+v %v", st, err)
	}

	// novi PIN dok slanje stoji: razlog ostaje
	o.pomakni(2 * time.Minute)
	if x, err := o.dk.PonovnoPosalji(ctx, p.Token); err != nil || x.PINPoslan || !errors.Is(x.Razlog, service.ErrSlanjeZastalo) {
		t.Fatalf("ponovno dok stoji: %+v %v", x, err)
	}
	if st, _ := o.dk.Cekanje(ctx, p.Token); !errors.Is(st.Razlog, service.ErrSlanjeZastalo) {
		t.Fatalf("razlog nakon neuspjelog ponovnog: %v", st.Razlog)
	}
	o.posta.greska = nil
	o.pomakni(6 * time.Minute)
	if x, err := o.dk.PonovnoPosalji(ctx, p.Token); err != nil || !x.PINPoslan {
		t.Fatalf("ponovno: %+v %v", x, err)
	}
	if st, _ := o.dk.Cekanje(ctx, p.Token); !st.PINPoslan || st.Razlog != nil {
		t.Fatalf("nakon poslanog PIN-a: %+v", st)
	}

	// trajni razlog: novi PIN nema smisla
	bez := o.korisnik(t, "bezposte", "")
	p, _ = o.dk.ZapocniPrijavu(ctx, bez, "", "")
	if st, _ := o.dk.Cekanje(ctx, p.Token); !errors.Is(st.Razlog, service.ErrNemaAdrese) || service.ProlazanRazlog(st.Razlog) {
		t.Fatalf("bez adrese: %+v", st)
	}
	for _, e := range []error{service.ErrNemaAdrese, service.ErrSlanjeOgraniceno, service.ErrPosiljateljNeispravan, fmt.Errorf("%w: x", service.ErrSlanjeZastalo)} {
		if got := service.RazlogIzOznake(service.OznakaRazloga(e)); !errors.Is(e, got) {
			t.Errorf("oznaka %q za %v vraća %v", service.OznakaRazloga(e), e, got)
		}
	}
}

// PIN se uključuje samo zahtjevom izvana (s čvora koji prima prijave
// izvana), nakon probe na njemu; isključuje se odasvud. Kad je uključen, a
// čvor nema ispravnog pošiljatelja, to se vidi u stanju i jednom na sat u
// zapisniku.
func TestPINSeUkljucujeIzvanaIUpozoravaBezPosiljatelja(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Fatal(err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, false); !errors.Is(err, service.ErrUkljuciIzvana) || o.dk.Ukljuceno(ctx) {
		t.Fatalf("uključen iz lokalne mreže: %v", err)
	}
	if err := o.dk.SpremiOpcije(ctx, o.admin, service.OpcijePrijaveIzvana{PIN: true}, false); !errors.Is(err, service.ErrUkljuciIzvana) {
		t.Fatalf("opcije iz lokalne mreže: %v", err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); err != nil {
		t.Fatal(err)
	}
	// uključen ostaje uključen i kad se domena sprema iz lokalne mreže
	if err := o.dk.SpremiOpcije(ctx, o.admin, service.OpcijePrijaveIzvana{PIN: true, Domena: "voda.hr"}, false); err != nil {
		t.Fatalf("domena uz uključen PIN: %v", err)
	}
	if st, _ := o.dk.Stanje(ctx); st.BezPosiljatelja || o.dk.BezPosiljatelja(ctx) {
		t.Fatal("ispravan pošiljatelj javljen kao neispravan")
	}

	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	defer log.SetOutput(stari)

	if err := o.dk.ObrisiPosiljatelja(ctx, o.admin); err != nil {
		t.Fatal(err)
	}
	// čvor koji nije primio prijavu izvana (samo lokalna mreža) pošiljatelja
	// ne treba: nema uzbune ni zapisa
	o.dk.TrebaDrugiKorak(ctx, false)
	_, _ = o.dk.Ocisti(ctx)
	if st, _ := o.dk.Stanje(ctx); st.BezPosiljatelja || st.PrimaIzvana || o.dk.BezPosiljatelja(ctx) || strings.Contains(zapis.String(), "UPOZORENJE") {
		t.Fatal("uzbuna na čvoru bez prijava izvana")
	}
	o.dk.TrebaDrugiKorak(ctx, true)
	if st, _ := o.dk.Stanje(ctx); !st.BezPosiljatelja || !st.PrimaIzvana || !o.dk.BezPosiljatelja(ctx) {
		t.Fatal("PIN uključen bez pošiljatelja nije javljen")
	}
	upozorenja := func() int { return strings.Count(zapis.String(), "UPOZORENJE") }
	for i := 0; i < 3; i++ {
		if _, err := o.dk.Ocisti(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if n := upozorenja(); n != 1 {
		t.Fatalf("upozorenja u satu: %d", n)
	}
	o.pomakni(time.Hour + time.Minute)
	_, _ = o.dk.Ocisti(ctx)
	if n := upozorenja(); n != 2 {
		t.Fatalf("upozorenja nakon sata: %d", n)
	}
	// tjedan bez prijave izvana: uzbuna prestaje
	o.pomakni(service.PamtiPrijavuIzvana)
	_, _ = o.dk.Ocisti(ctx)
	if st, _ := o.dk.Stanje(ctx); st.BezPosiljatelja || o.dk.BezPosiljatelja(ctx) || upozorenja() != 2 {
		t.Fatalf("uzbuna tjedan nakon zadnje prijave izvana (upozorenja %d)", upozorenja())
	}
	// isključuje se odasvud
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, false, false); err != nil || o.dk.Ukljuceno(ctx) {
		t.Fatalf("isključivanje iz lokalne mreže: %v", err)
	}
	if st, _ := o.dk.Stanje(ctx); st.BezPosiljatelja {
		t.Fatal("isključen PIN javljen kao bez pošiljatelja")
	}
}

// Osoba ne može sebi upisati adresu koju već ima drugi aktivni djelatnik:
// time bi njemu ugasila PIN (zajednička adresa).
func TestVlastitaAdresaNeSmijeBitiTudja(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ja := &models.UserPermissions{User: *o.osoba}
	zahtjev := func(email string) service.UpdateUserRequest {
		x := o.osoba
		return service.UpdateUserRequest{ID: x.ID, Username: x.Username, FullName: x.FullName, OrgType: x.OrgType,
			Email: email, IsActive: true, TrenutnaLozinka: "lozinka1"}
	}
	for _, a := range []string{"uprava@voda.hr", "  Uprava@VODA.hr "} {
		if _, err := o.users.UpdateUser(ja, zahtjev(a)); !errors.Is(err, service.ErrAdresaZauzeta) {
			t.Fatalf("tuđa adresa %q: %v", a, err)
		}
	}
	if u, _ := o.repo.GetUserByID(o.osoba.ID); u.Email != "pero.peric@voda.hr" {
		t.Fatalf("adresa je promijenjena: %q", u.Email)
	}
	// adresa neaktivnog računa je slobodna
	stari := o.korisnik(t, "stari", "stari@voda.hr")
	stari.IsActive = false
	if err := o.repo.UpdateUser(stari); err != nil {
		t.Fatal(err)
	}
	if _, err := o.users.UpdateUser(ja, zahtjev("stari@voda.hr")); err != nil {
		t.Fatalf("adresa neaktivnog računa: %v", err)
	}
}

// Iz adresara tvrtke osoba ne mijenja svoju adresu e-pošte (to ide s
// profila, uz lozinku); tuđu mijenja kao administrator, a ostala polja
// svoja i dalje.
func TestAdresarNeMijenjaVlastituAdresu(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	akti := service.NewAktService(nil, nil, nil, nil, nil, o.users, nil, "cvor-a")
	uprava := o.admin.User
	if err := akti.PrimijeniKontakt(ctx, o.admin, uprava.ID.String(), map[string]string{"email": "netko.drugi@voda.hr", "phone": "031 222 333"}); !errors.Is(err, service.ErrVlastitaAdresaIzAdresara) {
		t.Fatalf("vlastita adresa iz adresara: %v", err)
	}
	if u, _ := o.repo.GetUserByID(uprava.ID); u.Email != "uprava@voda.hr" {
		t.Fatalf("vlastita adresa promijenjena: %q", u.Email)
	}
	if err := akti.PrimijeniKontakt(ctx, o.admin, uprava.ID.String(), map[string]string{"email": "Uprava@voda.hr", "phone": "031 222 333"}); err != nil {
		t.Fatalf("ista vlastita adresa i telefon: %v", err)
	}
	if err := akti.PrimijeniKontakt(ctx, o.admin, o.osoba.ID.String(), map[string]string{"email": "pero.novi@voda.hr"}); err != nil {
		t.Fatalf("tuđa adresa: %v", err)
	}
	if u, _ := o.repo.GetUserByID(o.osoba.ID); u.Email != "pero.novi@voda.hr" {
		t.Fatalf("tuđa adresa nije upisana: %q", u.Email)
	}
}

// Filtar „bez e-pošte” nalazi sve aktivne račune kojima PIN nema kamo:
// praznu adresu, adresu izvan dopuštene domene i zajedničku adresu.
func TestFiltarBezAdreseZaPIN(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.korisnik(t, "prazna", "")
	o.korisnik(t, "gmail", "ime@gmail.com")
	o.korisnik(t, "slicna", "ime@vodavoda.hr")
	o.korisnik(t, "prvi", "zajednicka@voda.hr")
	o.korisnik(t, "drugi", " Zajednicka@VODA.hr")
	o.korisnik(t, "velika", "Velika.Slova@VODA.HR")
	ugasen := o.korisnik(t, "ugasen", "")
	ugasen.IsActive = false
	if err := o.repo.UpdateUser(ugasen); err != nil {
		t.Fatal(err)
	}
	popis, err := o.users.ListUsers("", 0, "", "", string(repository.StanjeBezEposte))
	if err != nil {
		t.Fatal(err)
	}
	var imena []string
	for _, u := range popis {
		imena = append(imena, u.Username)
	}
	if got := strings.Join(imena, ","); got != "drugi,gmail,prazna,prvi,slicna" {
		t.Fatalf("bez adrese za PIN: %s", got)
	}
}

// Čvor šalje najviše 60 PIN-ova na sat, ma koliko osoba: 61. ne ide
// poslužitelju, a nakon sata slanje opet radi.
func TestSezdesetPINovaNaSatPoCvoru(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t) // probni PIN je prvi od 60
	ctx := context.Background()
	hash, err := o.auth.HashPassword("lozinka1")
	if err != nil {
		t.Fatal(err)
	}
	osoba := func(i int) *models.User {
		u := &models.User{Username: fmt.Sprintf("osoba%02d", i), PasswordHash: hash, FullName: "x", Email: fmt.Sprintf("osoba%02d@voda.hr", i),
			IsActive: true, OrgType: models.OrgHrvatskeVode}
		if err := o.repo.CreateUser(u, nil); err != nil {
			t.Fatal(err)
		}
		return u
	}
	for i := 2; i <= 60; i++ {
		if p, err := o.dk.ZapocniPrijavu(ctx, osoba(i), "", ""); err != nil || !p.PINPoslan {
			t.Fatalf("%d. PIN: %+v %v", i, p, err)
		}
	}
	prije := o.posta.broj()
	p, err := o.dk.ZapocniPrijavu(ctx, osoba(61), "", "")
	if err != nil || p.PINPoslan || !errors.Is(p.Razlog, service.ErrSlanjeOgraniceno) {
		t.Fatalf("61. PIN: %+v %v", p, err)
	}
	if o.posta.broj() != prije {
		t.Fatal("61. PIN je otišao poslužitelju")
	}
	o.pomakni(time.Hour + time.Second)
	if p, err := o.dk.ZapocniPrijavu(ctx, osoba(62), "", ""); err != nil || !p.PINPoslan {
		t.Fatalf("nakon sata: %+v %v", p, err)
	}
}
