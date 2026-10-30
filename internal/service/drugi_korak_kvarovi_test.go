package service_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// postarSIshodima je lažna pošta kojoj test zadaje ishod prijave na
// poslužitelj i odbijanje poruke; pamti što se tražilo u adresaru
type postarSIshodima struct {
	*laznaPosta
	prijava  error // poslužitelj ne primi prijavu
	odbijeno error // poslužitelj ne primi poruku
	upiti    []string
}

func (p *postarSIshodima) Prijavi(ctx context.Context, ps posta.Postavke, r posta.Racun) (string, error) {
	if p.prijava != nil {
		return "", p.prijava
	}
	return p.laznaPosta.Prijavi(ctx, ps, r)
}

func (p *postarSIshodima) Imenik(ctx context.Context, ps posta.Postavke, r posta.Racun, upit string) ([]posta.Kontakt, error) {
	p.mu.Lock()
	p.upiti = append(p.upiti, upit)
	p.mu.Unlock()
	return p.laznaPosta.Imenik(ctx, ps, r, upit)
}

func (p *postarSIshodima) Posalji(ctx context.Context, ps posta.Postavke, r posta.Racun, poruke []posta.Poruka) ([]error, error) {
	if p.odbijeno != nil {
		return []error{p.odbijeno}, nil
	}
	return p.laznaPosta.Posalji(ctx, ps, r, poruke)
}

// izvrsi izvršava upit nad bazom okoline (okidač kvara, izmjena mimo programa)
func (o *okolinaPIN) izvrsi(t *testing.T, upit string) {
	t.Helper()
	if _, err := o.baza.Exec(upit); err != nil {
		t.Fatalf("%v\n%s", err, upit)
	}
}

// zabrani postavlja okidač koji odbija radnju (INSERT, UPDATE, DELETE) nad tablicom
func (o *okolinaPIN) zabrani(t *testing.T, ime, radnja, tablica string) {
	t.Helper()
	o.izvrsi(t, `CREATE TRIGGER `+ime+` BEFORE `+radnja+` ON `+tablica+` BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`)
}

// novaPrijava otvara prijavu na čekanju za Peru i vraća token i poslani PIN;
// sat se pomakne da slanje ne udari u ograničenje po osobi
func (o *okolinaPIN) novaPrijava(t *testing.T) (string, string) {
	t.Helper()
	o.pomakni(6 * time.Minute)
	p, err := o.dk.ZapocniPrijavu(context.Background(), o.osoba, "", "")
	if err != nil || !p.PINPoslan {
		t.Fatalf("prijava na čekanju: %+v %v", p, err)
	}
	return p.Token, o.posta.pin(t)
}

const sjemeTesta = "sjeme-cvora-za-test-32-bajta-xxx"

// Račun za slanje PIN-a upisuje samo globalni administrator, s imenom i
// lozinkom koje poslužitelj primi. Prazna adresa traži se u adresaru po
// imenu bez domene; adresa koja se ne nađe ili nije adresa ne sprema se,
// kao ni račun koji baza ne primi. Na čvoru tada ostaje dosadašnji račun.
func TestPosiljateljPINaKadNestoNeValja(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	p := &postarSIshodima{laznaPosta: o.posta}
	o.dk.SetPostar(p)
	osoba := &models.UserPermissions{User: *o.osoba}
	for naziv, tko := range map[string]*models.UserPermissions{"bez ovlasti": nil, "nije administrator": osoba} {
		if _, err := o.dk.SpremiPosiljatelja(ctx, tko, "VODA\\pin2", "tajna", "pin2@voda.hr"); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: upis pošiljatelja: %v", naziv, err)
		}
		if err := o.dk.ObrisiPosiljatelja(ctx, tko); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: brisanje pošiljatelja: %v", naziv, err)
		}
	}
	if _, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "  ", "tajna", ""); err == nil || !strings.Contains(err.Error(), "upišite korisničko ime i lozinku") {
		t.Errorf("prazno ime: %v", err)
	}

	p.prijava = errors.New("poslužitelj ne prima lozinku")
	_, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin2", "kriva", "pin2@voda.hr")
	if !errors.Is(err, p.prijava) || !strings.Contains(err.Error(), "prijava na poslužitelj e-pošte nije prošla") || service.PrijavaProsla(err) {
		t.Errorf("odbijena prijava: %v (prijava prošla: %v)", err, service.PrijavaProsla(err))
	}
	p.prijava = nil

	o.posta.mu.Lock()
	o.posta.adresar = []posta.Kontakt{{Ime: "PIN slanje", Email: "pin.slanje@voda.hr"}}
	o.posta.mu.Unlock()
	pos, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin.slanje@voda.hr", "tajna", "")
	if err != nil || pos.Adresa != "pin.slanje@voda.hr" || pos.Korisnik != "VODA\\pin.slanje@voda.hr" {
		t.Fatalf("adresa iz adresara: %+v %v", pos, err)
	}
	if upit := p.upiti[len(p.upiti)-1]; upit != "pin.slanje" {
		t.Errorf("u adresaru je traženo %q, očekivano pin.slanje", upit)
	}

	o.posta.mu.Lock()
	o.posta.adresar = nil
	o.posta.mu.Unlock()
	if _, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\nepoznat", "tajna", ""); !service.PrijavaProsla(err) ||
		!strings.Contains(err.Error(), "nije pronađena u adresaru") {
		t.Errorf("adresa koje nema u adresaru: %v", err)
	}
	if _, err := o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin3", "tajna", "PIN <pin3@voda.hr>"); !service.PrijavaProsla(err) ||
		!strings.Contains(err.Error(), "nije ispravna") {
		t.Errorf("adresa s imenom: %v", err)
	}

	o.zabrani(t, "zabrana_novog", "INSERT", "racuni_sustava")
	o.zabrani(t, "zabrana_izmjene", "UPDATE", "racuni_sustava")
	_, err = o.dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin4", "tajna", "pin4@voda.hr")
	if !service.PrijavaProsla(err) || errors.Unwrap(err) == nil || !strings.Contains(errors.Unwrap(err).Error(), "zabranjeno") {
		t.Errorf("račun koji baza ne primi: %v", err)
	}
	st, err := o.dk.Stanje(ctx)
	if err != nil || st.Posiljatelj == nil || st.Posiljatelj.Adresa != "pin.slanje@voda.hr" {
		t.Errorf("pošiljatelj nakon neuspjelih upisa: %+v %v", st, err)
	}
}

// Bez ključa čvora drugi korak ne radi ništa: svaka radnja javlja da ključa
// nema, a pošta ne odlazi. S ključem, a bez podešenog poslužitelja e-pošte,
// ni račun za slanje ni PIN ne prolaze; kao ni lozinka zaključana drugim
// ključem.
func TestDrugiKorakBezKljucaIPoste(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	dk := service.NewDrugiKorak(o.dkRepo, repository.NewRacuniSustavaRepository(o.baza), o.repo,
		repository.NewAktiRepository(o.baza, ledger.New(o.baza, "cvor-a")))
	dk.SetPostar(o.posta)
	dk.SetSat(func() time.Time { return o.sad })
	dk.SetKljuc([]byte(sjemeTesta))
	dk.SetKljuc(nil)

	prije := o.posta.broj()
	for naziv, err := range map[string]error{
		"upis pošiljatelja": func() error {
			_, err := dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin", "tajna", "pin@voda.hr")
			return err
		}(),
		"probni PIN":      func() error { _, err := dk.PosaljiProbniPIN(ctx, o.admin); return err }(),
		"prijava":         func() error { _, err := dk.ZapocniPrijavu(ctx, o.osoba, "", ""); return err }(),
		"čekanje":         func() error { _, err := dk.Cekanje(ctx, "bilo-sto"); return err }(),
		"računalo":        func() error { _, err := dk.ZapamtiRacunalo(ctx, o.osoba, "", "", ""); return err }(),
		"rezervni kodovi": func() error { _, err := dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", false); return err }(),
		"privremeni kod":  func() error { _, _, err := dk.IzdajPrivremeniKod(ctx, o.admin, o.osoba.ID, false); return err }(),
	} {
		if !errors.Is(err, service.ErrNemaKljucaPrijave) {
			t.Errorf("%s bez ključa: %v", naziv, err)
		}
	}
	if dk.ProvjeriRacunalo(ctx, o.osoba, "", "") {
		t.Error("računalo prolazi bez ključa")
	}
	dk.JaviPromjenuAdrese(ctx, o.osoba, "pero.peric@voda.hr", "novi@voda.hr")
	if o.posta.broj() != prije {
		t.Error("bez ključa je poslana pošta")
	}

	dk.SetKljuc([]byte(sjemeTesta))
	if _, err := dk.SpremiPosiljatelja(ctx, o.admin, "VODA\\pin", "tajna", "pin@voda.hr"); err == nil ||
		!strings.Contains(err.Error(), "poslužitelj e-pošte nije podešen") {
		t.Errorf("upis pošiljatelja bez poslužitelja: %v", err)
	}
	if _, err := dk.PosaljiProbniPIN(ctx, o.admin); !errors.Is(err, service.ErrNemaPosiljatelja) || !strings.Contains(err.Error(), "nije podešen") {
		t.Errorf("probni PIN bez poslužitelja: %v", err)
	}

	dk.SetKljuc([]byte("drugo-sjeme-cvora-32-bajta-yyyyy"))
	dk.SetPosta(func(context.Context) posta.Postavke {
		return posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: "owa.primjer.hr"}
	})
	if _, err := dk.PosaljiProbniPIN(ctx, o.admin); !errors.Is(err, service.ErrNemaPosiljatelja) {
		t.Errorf("lozinka zaključana drugim ključem: %v", err)
	}
	if o.posta.broj() != prije {
		t.Error("PIN je poslan bez poslužitelja ili s krivim ključem")
	}
}

// Probni PIN šalje samo globalni administrator kojeg ima, na dopuštenu
// adresu, s ispravnim pošiljateljem; poruka koju poslužitelj ne primi ne
// računa se kao uspjela proba. Račun koji poslužitelj odbije označi se
// neispravnim, a kad ni to ne uspije, slanje svejedno javlja neispravan
// račun.
func TestProbniPINKadNestoNeValja(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	osoba := &models.UserPermissions{User: *o.osoba}
	if _, err := o.dk.PosaljiProbniPIN(ctx, osoba); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("probni PIN osobe koja nije administrator: %v", err)
	}
	nitko := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), Username: "nitko"}}
	if _, err := o.dk.PosaljiProbniPIN(ctx, nitko); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("probni PIN administratora kojeg nema: %v", err)
	}
	vanjski := o.korisnik(t, "vanjski", "vanjski@primjer.hr")
	if _, err := o.dk.PosaljiProbniPIN(ctx, &models.UserPermissions{IsGlobalAdmin: true, User: *vanjski}); !errors.Is(err, service.ErrAdresaNijeDopustena) {
		t.Errorf("probni PIN na adresu izvan domene: %v", err)
	}

	p := &postarSIshodima{laznaPosta: o.posta, odbijeno: errors.New("550 sandučić ne postoji")}
	o.dk.SetPostar(p)
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); !errors.Is(err, service.ErrPINNijePoslan) || !strings.Contains(err.Error(), "550") {
		t.Errorf("poruka koju poslužitelj ne primi: %v", err)
	}
	if st, err := o.dk.Stanje(ctx); err != nil || st.ProbaUspjela {
		t.Errorf("proba je uspjela iako poruka nije primljena: %+v %v", st, err)
	}

	o.dk.SetPostar(o.posta)
	o.posta.mu.Lock()
	o.posta.greska = posta.ErrPrijava
	o.posta.mu.Unlock()
	o.zabrani(t, "zabrana_oznake", "UPDATE", "racuni_sustava")
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); !errors.Is(err, service.ErrPosiljateljNeispravan) {
		t.Errorf("odbijena lozinka pošiljatelja: %v", err)
	}
	if st, err := o.dk.Stanje(ctx); err != nil || st.Posiljatelj == nil || st.Posiljatelj.NeispravanOd != nil {
		t.Errorf("pošiljatelj kojeg baza nije označila: %+v %v", st, err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana_oznake`)
	o.posta.mu.Lock()
	o.posta.greska = nil
	o.posta.mu.Unlock()

	if err := o.dk.ObrisiPosiljatelja(ctx, o.admin); err != nil {
		t.Fatal(err)
	}
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); !errors.Is(err, service.ErrNemaPosiljatelja) {
		t.Errorf("probni PIN bez pošiljatelja: %v", err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); !errors.Is(err, service.ErrNemaPosiljatelja) {
		t.Errorf("uključivanje PIN-a bez pošiljatelja: %v", err)
	}

	o.izvrsi(t, `UPDATE users SET created_at = 'nije vrijeme' WHERE username = 'uprava'`)
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err == nil || errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("probni PIN kad se račun ne da pročitati: %v", err)
	}
}

// Obavijest o promjeni adrese ide samo na staru adresu na koju je smio ići
// PIN; prazna nova adresa piše se kao „(prazno)”.
func TestObavijestOPromjeniAdrese(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	prije := o.posta.broj()
	o.dk.JaviPromjenuAdrese(ctx, nil, "pero.peric@voda.hr", "")
	o.dk.JaviPromjenuAdrese(ctx, o.osoba, "pero@primjer.hr", "pero.peric@voda.hr")
	if o.posta.broj() != prije {
		t.Fatal("obavijest je otišla bez osobe ili na staru adresu izvan domene")
	}
	o.dk.JaviPromjenuAdrese(ctx, o.osoba, " pero.peric@voda.hr ", "  ")
	m := o.posta.zadnja(t)
	if o.posta.broj() != prije+1 || m.Za.Address != "pero.peric@voda.hr" || !strings.Contains(m.Tekst, "u: (prazno).") ||
		!strings.Contains(m.Tekst, "pperic") {
		t.Errorf("obavijest: %+v", m)
	}
}

// Dok je red za slanje zauzet, slanje čiji je zahtjev prekinut odustaje
// zbog prekida i ne čeka istek reda; slanje koje drži red završi normalno.
// Zapis mora reći da je razlog prekid: kad bi slanje čekalo istek, pisalo bi
// da slanje nije došlo na red.
func TestPrekinutoSlanjeNeCekaRed(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.dk.SetIstekSlanja(time.Minute)
	unutra, pusti := make(chan struct{}), make(chan struct{})
	var jednom sync.Once
	o.posta.mu.Lock()
	o.posta.usred = func(context.Context) error {
		jednom.Do(func() { close(unutra); <-pusti })
		return nil
	}
	o.posta.mu.Unlock()
	gotovo := make(chan error, 1)
	go func() {
		_, err := o.dk.PosaljiProbniPIN(context.Background(), o.admin)
		gotovo <- err
	}()
	<-unutra
	prekinut, prekini := context.WithCancel(context.Background())
	prekini()
	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })
	o.dk.JaviPromjenuAdrese(prekinut, o.osoba, "pero.peric@voda.hr", "novi@voda.hr")
	log.SetOutput(stari)
	close(pusti)
	if err := <-gotovo; err != nil {
		t.Fatalf("probni PIN: %v", err)
	}
	if z := zapis.String(); !strings.Contains(z, "obavijest o promjeni adrese za pperic nije poslana") ||
		!strings.Contains(z, context.Canceled.Error()) || strings.Contains(z, "nije došlo na red") {
		t.Errorf("zapis o neposlanoj obavijesti: %q; očekivan prekid zahtjeva", z)
	}
	o.posta.mu.Lock()
	defer o.posta.mu.Unlock()
	if len(o.posta.poruke) != 1 || !strings.Contains(o.posta.poruke[0].Predmet, "probni") {
		t.Errorf("poslano: %+v; očekivan samo probni PIN", o.posta.poruke)
	}
}

// Prijava na čekanju: bez osobe se ne otvara; razlog koji program ne
// poznaje čita se kao neposlan PIN; novi PIN koji se ne da spremiti ne
// poništava stari; osoba čiji se račun ne da pročitati ili je isključen ne
// dobiva novi PIN, a isključenoj se prijava briše. Prijava koja se ne da
// spremiti ne otvara se.
func TestPrijavaNaCekanjuKadBazaZakaze(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	if _, err := o.dk.ZapocniPrijavu(ctx, nil, "", ""); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("prijava bez osobe: %v", err)
	}
	if _, err := o.dk.Cekanje(ctx, "nije-token"); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("čekanje s lažnim tokenom: %v", err)
	}
	if _, err := o.dk.PonovnoPosalji(ctx, "nije-token"); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("novi PIN s lažnim tokenom: %v", err)
	}

	bez := o.korisnik(t, "bezposte", "")
	p, err := o.dk.ZapocniPrijavu(ctx, bez, "", "")
	if err != nil || p.PINPoslan {
		t.Fatalf("prijava bez adrese: %+v %v", p, err)
	}
	o.izvrsi(t, `UPDATE prijave_na_cekanju SET razlog = 'nepoznat'`)
	if s, err := o.dk.Cekanje(ctx, p.Token); err != nil || s.PINPoslan || !errors.Is(s.Razlog, service.ErrPINNijePoslan) {
		t.Errorf("nepoznat razlog: %+v %v", s, err)
	}
	o.zabrani(t, "zabrana_razloga", "UPDATE OF razlog", "prijave_na_cekanju")
	o.pomakni(service.RazmakPonovnogSlanja)
	ishod, err := o.dk.PonovnoPosalji(ctx, p.Token)
	if err != nil || ishod.PINPoslan || !errors.Is(ishod.Razlog, service.ErrNemaAdrese) {
		t.Errorf("novi PIN bez adrese: %+v %v", ishod, err)
	}
	if s, err := o.dk.Cekanje(ctx, p.Token); err != nil || !errors.Is(s.Razlog, service.ErrPINNijePoslan) {
		t.Errorf("razlog koji se nije dao spremiti: %+v %v", s, err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana_razloga`)

	token, stari := o.novaPrijava(t)
	o.zabrani(t, "zabrana_pina", "UPDATE OF pin_hash", "prijave_na_cekanju")
	o.pomakni(service.RazmakPonovnogSlanja)
	if _, err := o.dk.PonovnoPosalji(ctx, token); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("novi PIN koji se ne da spremiti: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana_pina`)
	if _, err := o.dk.ProvjeriKod(ctx, token, stari); err != nil {
		t.Errorf("stari PIN nakon nespremljenog novog: %v", err)
	}

	token, _ = o.novaPrijava(t)
	o.izvrsi(t, `UPDATE users SET created_at = 'nije vrijeme' WHERE username = 'pperic'`)
	o.pomakni(service.RazmakPonovnogSlanja)
	if _, err := o.dk.PonovnoPosalji(ctx, token); err == nil || errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("novi PIN kad se račun ne da pročitati: %v", err)
	}
	o.izvrsi(t, `UPDATE users SET created_at = '2026-10-03 10:00:00', is_active = 0 WHERE username = 'pperic'`)
	if _, err := o.dk.PonovnoPosalji(ctx, token); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("novi PIN isključenoj osobi: %v", err)
	}
	if _, err := o.dk.Cekanje(ctx, token); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("prijava isključene osobe nije obrisana: %v", err)
	}
	o.izvrsi(t, `UPDATE users SET is_active = 1 WHERE username = 'pperic'`)

	o.zabrani(t, "zabrana_prijave", "INSERT", "prijave_na_cekanju")
	o.pomakni(6 * time.Minute)
	if _, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", ""); err == nil {
		t.Error("prijava koja se ne da spremiti je otvorena")
	}

	prije := o.posta.broj()
	o.baza.Close()
	o.pomakni(6 * time.Minute)
	if _, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", ""); err == nil {
		t.Error("prijava iz zatvorene baze je otvorena")
	}
	if o.posta.broj() != prije {
		t.Error("PIN je poslan iz zatvorene baze")
	}
}

// Provjera koda kad baza zakaže: pokušaj koji se ne da zabilježiti ne troši
// ni pokušaj ni PIN; prijava koja se ne da potrošiti ne pušta unutra, kao
// ni prijava ili kod koje je netko u međuvremenu potrošio; isključen račun
// i račun koji se ne da pročitati ne prolaze. Kod koji se ne da potrošiti
// ostaje neiskorišten, a prijava je potrošena.
func TestProvjeraKodaKadBazaZakaze(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()

	token, pin := o.novaPrijava(t)
	o.zabrani(t, "zabrana", "UPDATE OF pokusaja", "prijave_na_cekanju")
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("pokušaj koji se ne da zabilježiti: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana`)
	if s, err := o.dk.Cekanje(ctx, token); err != nil || s.Preostalo != service.NajviseKrivihUnosa {
		t.Errorf("pokušaj je potrošen: %+v %v", s, err)
	}
	o.zabrani(t, "zabrana", "DELETE", "prijave_na_cekanju")
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("prijava koja se ne da potrošiti: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana`)
	if ishod, err := o.dk.ProvjeriKod(ctx, token, pin); err != nil || ishod.Korisnik.ID != o.osoba.ID {
		t.Errorf("isti PIN nakon kvara: %+v %v", ishod, err)
	}

	token, pin = o.novaPrijava(t)
	o.izvrsi(t, `CREATE TRIGGER preskoci BEFORE DELETE ON prijave_na_cekanju BEGIN SELECT RAISE(IGNORE); END`)
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("prijava potrošena u međuvremenu: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER preskoci`)

	o.izvrsi(t, `UPDATE users SET is_active = 0 WHERE username = 'pperic'`)
	token, pin = o.novaPrijava(t)
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); !errors.Is(err, service.ErrAccountInactive) {
		t.Errorf("isključen račun: %v", err)
	}
	o.izvrsi(t, `UPDATE users SET is_active = 1 WHERE username = 'pperic'`)
	token, pin = o.novaPrijava(t)
	o.izvrsi(t, `UPDATE users SET created_at = 'nije vrijeme' WHERE username = 'pperic'`)
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); err == nil || errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("račun koji se ne da pročitati: %v", err)
	}
	o.izvrsi(t, `UPDATE users SET created_at = '2026-10-03 10:00:00' WHERE username = 'pperic'`)

	kodovi, err := o.dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", false)
	if err != nil {
		t.Fatal(err)
	}
	token, _ = o.novaPrijava(t)
	o.izvrsi(t, `ALTER TABLE kodovi_prijave RENAME TO kodovi_negdje`)
	if _, err := o.dk.ProvjeriKod(ctx, token, kodovi[0]); err == nil || errors.Is(err, service.ErrKrivKod) {
		t.Errorf("kodovi koji se ne daju pročitati: %v", err)
	}
	o.izvrsi(t, `ALTER TABLE kodovi_negdje RENAME TO kodovi_prijave`)
	o.zabrani(t, "zabrana", "UPDATE", "kodovi_prijave")
	if _, err := o.dk.ProvjeriKod(ctx, token, kodovi[0]); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("kod koji se ne da potrošiti: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana`)
	if n, _, _ := o.dk.StanjeRezervnih(ctx, o.osoba.ID); n != service.BrojRezervnihKodova {
		t.Errorf("rezervnih kodova nakon kvara: %d, očekivano %d", n, service.BrojRezervnihKodova)
	}
	if _, err := o.dk.Cekanje(ctx, token); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("prijava nakon koda koji se nije dao potrošiti: %v", err)
	}

	token, _ = o.novaPrijava(t)
	o.izvrsi(t, `CREATE TRIGGER preskoci BEFORE UPDATE ON kodovi_prijave BEGIN SELECT RAISE(IGNORE); END`)
	if _, err := o.dk.ProvjeriKod(ctx, token, kodovi[1]); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("kod potrošen u međuvremenu: %v", err)
	}
}

// Zapamćeno računalo: bez osobe i lozinke se ne pamti, opis preglednika
// skraćuje se na 300 znakova, upotreba koja se ne da zabilježiti ne
// zaključava računalo. Zaboravljanje i pamćenje koje baza ne primi javljaju
// grešku. Rezervni i privremeni kodovi traže osobu koju ima i bazu koja ih
// primi.
func TestRacunalaIKodoviKadBazaZakaze(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	if _, err := o.dk.ZapamtiRacunalo(ctx, nil, "", "", ""); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("računalo bez osobe: %v", err)
	}
	bezLozinke := *o.osoba
	bezLozinke.PasswordHash = ""
	if _, err := o.dk.ZapamtiRacunalo(ctx, &bezLozinke, "", "", ""); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("računalo osobe bez lozinke: %v", err)
	}
	dugi := strings.Repeat("Mozilla/5.0 ", 40)
	token, err := o.dk.ZapamtiRacunalo(ctx, o.osoba, "", "198.51.100.7", dugi)
	if err != nil {
		t.Fatal(err)
	}
	racunala, err := o.dk.Racunala(ctx, o.osoba.ID, token)
	if err != nil || len(racunala) != 1 || racunala[0].UserAgent != dugi[:300] || !racunala[0].OvoRacunalo {
		t.Fatalf("zapamćeno računalo: %+v %v", racunala, err)
	}
	if o.dk.ProvjeriRacunalo(ctx, nil, token, "") || o.dk.ProvjeriRacunalo(ctx, o.osoba, "nije-token", "") ||
		o.dk.ProvjeriRacunalo(ctx, &bezLozinke, token, "") {
		t.Error("računalo prolazi bez osobe, tokena ili lozinke")
	}
	o.zabrani(t, "zabrana", "UPDATE", "zapamcena_racunala")
	if !o.dk.ProvjeriRacunalo(ctx, o.osoba, token, "198.51.100.8") {
		t.Error("računalo ne prolazi kad se upotreba ne da zabilježiti")
	}
	o.izvrsi(t, `DROP TRIGGER zabrana`)
	if r, _ := o.dk.Racunala(ctx, o.osoba.ID, token); len(r) != 1 || r[0].Koristeno != nil || r[0].IPZadnji != "198.51.100.7" {
		t.Errorf("upotreba je zabilježena: %+v", r)
	}

	o.zabrani(t, "zabrana", "DELETE", "zapamcena_racunala")
	if err := o.dk.Zaboravi(ctx, o.osoba.ID, racunala[0].ID, false); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("zaboravljanje koje baza ne primi: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana`)
	if err := o.dk.Zaboravi(ctx, o.osoba.ID, racunala[0].ID, false); err != nil {
		t.Errorf("zaboravljanje: %v", err)
	}
	if r, _ := o.dk.Racunala(ctx, o.osoba.ID, token); len(r) != 0 {
		t.Errorf("zaboravljeno računalo je još na popisu: %+v", r)
	}
	o.zabrani(t, "zabrana", "INSERT", "zapamcena_racunala")
	if _, err := o.dk.ZapamtiRacunalo(ctx, o.osoba, "", "", "Firefox"); err == nil {
		t.Error("računalo koje baza ne primi je zapamćeno")
	}
	o.izvrsi(t, `DROP TRIGGER zabrana`)

	if _, err := o.dk.NapraviRezervneKodove(ctx, nil, "lozinka1", false); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("rezervni kodovi bez osobe: %v", err)
	}
	if _, err := o.dk.NapraviRezervneKodove(ctx, &models.User{ID: uuid.New()}, "lozinka1", false); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("rezervni kodovi osobe koje nema: %v", err)
	}
	if _, _, err := o.dk.IzdajPrivremeniKod(ctx, o.admin, uuid.New(), false); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("privremeni kod osobi koje nema: %v", err)
	}
	o.zabrani(t, "zabrana", "INSERT", "kodovi_prijave")
	if _, err := o.dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", false); err == nil {
		t.Error("rezervni kodovi koje baza ne primi su napravljeni")
	}
	if _, _, err := o.dk.IzdajPrivremeniKod(ctx, o.admin, o.osoba.ID, false); err == nil {
		t.Error("privremeni kod koji baza ne primi je izdan")
	}
	o.izvrsi(t, `DROP TRIGGER zabrana`)
	if n, kad, err := o.dk.StanjeRezervnih(ctx, o.osoba.ID); n != 0 || kad != nil || err != nil {
		t.Errorf("rezervni kodovi nakon kvara: %d %v %v", n, kad, err)
	}

	o.izvrsi(t, `UPDATE users SET created_at = 'nije vrijeme' WHERE username = 'pperic'`)
	if _, err := o.dk.NapraviRezervneKodove(ctx, o.osoba, "lozinka1", false); err == nil || errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("rezervni kodovi kad se račun ne da pročitati: %v", err)
	}
	if _, _, err := o.dk.IzdajPrivremeniKod(ctx, o.admin, o.osoba.ID, false); err == nil || errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("privremeni kod kad se račun ne da pročitati: %v", err)
	}
}

// Postavka PIN-a izvana: domena mora biti domena, a postavka koju baza ne
// primi ostaje kakva je bila. Kad veza s poslužiteljem pukne, slanje stoji
// i stanje to kaže. Račun za slanje koji se ne da pročitati ruši stanje i
// uključivanje PIN-a, a uzbunu bez pošiljatelja ne diže.
func TestStanjeIOpcijeDrugogKorakaKadBazaZakaze(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	if err := o.dk.SpremiOpcije(ctx, o.admin, service.OpcijePrijaveIzvana{PIN: true, Domena: "voda"}, true); err == nil ||
		!strings.Contains(err.Error(), "nije ispravna") {
		t.Errorf("domena bez točke: %v", err)
	}
	o.zabrani(t, "zabrana_nove", "INSERT", "postavke")
	o.zabrani(t, "zabrana_izmjene", "UPDATE", "postavke")
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, false, false); err == nil {
		t.Error("postavka koju baza ne primi je spremljena")
	}
	o.izvrsi(t, `DROP TRIGGER zabrana_nove`)
	o.izvrsi(t, `DROP TRIGGER zabrana_izmjene`)
	if !o.dk.Ukljuceno(ctx) {
		t.Error("PIN se isključio iako postavka nije spremljena")
	}

	o.posta.mu.Lock()
	o.posta.greska = errors.New("veza prekinuta")
	o.posta.mu.Unlock()
	if p, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "", ""); err != nil || p.PINPoslan || !errors.Is(p.Razlog, service.ErrSlanjeZastalo) {
		t.Errorf("prijava kad veza pukne: %+v %v", p, err)
	}
	if st, err := o.dk.Stanje(ctx); err != nil || !st.StankaDo.After(o.sad) {
		t.Errorf("stanje nakon pukle veze: %+v %v", st, err)
	}
	o.posta.mu.Lock()
	o.posta.greska = nil
	o.posta.mu.Unlock()

	o.izvrsi(t, `ALTER TABLE racuni_sustava RENAME TO racuni_negdje`)
	if _, err := o.dk.Stanje(ctx); err == nil {
		t.Error("stanje bez računa za slanje bez greške")
	}
	if o.dk.BezPosiljatelja(ctx) {
		t.Error("uzbuna bez pošiljatelja kad se račun ne da pročitati")
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, false, false); err != nil {
		t.Fatal(err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, o.admin, true, true); err == nil || errors.Is(err, service.ErrNemaPosiljatelja) {
		t.Errorf("uključivanje kad se račun ne da pročitati: %v", err)
	}
}

// Pomoćne poruke drugog koraka: krivi kod kaže koliko je pokušaja ostalo,
// adresa bez @ ili bez imena ne prikazuje se, razlozi PIN-a prevode se u
// oznake i natrag, a prava pošta bez podešenog poslužitelja ne šalje ništa.
func TestDrugiKorakPomocneFunkcije(t *testing.T) {
	if s := (service.KrivKod{Preostalo: 3}).Error(); s != "kod nije točan; preostalo pokušaja: 3" {
		t.Errorf("krivi kod: %q", s)
	}
	for adresa, ocek := range map[string]string{"bez-majmunca": "", "@voda.hr": "", " ana@voda.hr ": "a***@voda.hr"} {
		if s := service.MaskirajAdresu(adresa); s != ocek {
			t.Errorf("MaskirajAdresu(%q) = %q, očekivano %q", adresa, s, ocek)
		}
	}
	if s := service.OznakaRazloga(nil); s != "" {
		t.Errorf("oznaka bez razloga: %q", s)
	}
	if s := service.OznakaRazloga(errors.New("nešto treće")); s != "nije" {
		t.Errorf("oznaka nepoznatog razloga: %q", s)
	}
	if r := service.RazlogIzOznake("nepoznata"); r != nil {
		t.Errorf("razlog nepoznate oznake: %v", r)
	}
	if r := service.RazlogIzOznake("zastalo"); r != service.ErrSlanjeZastalo {
		t.Errorf("razlog oznake zastalo: %v", r)
	}
	if service.PrijavaProsla(errors.New("x")) {
		t.Error("obična greška kaže da je prijava prošla")
	}

	ctx := context.Background()
	var prava service.PravaPosta
	racun := posta.Racun{Korisnik: "pin", Lozinka: "tajna"}
	if _, err := prava.Prijavi(ctx, posta.Postavke{}, racun); err == nil || !strings.Contains(err.Error(), "nije upisan") {
		t.Errorf("prijava bez poslužitelja: %v", err)
	}
	if _, err := prava.Posalji(ctx, posta.Postavke{}, racun, []posta.Poruka{{Predmet: "proba"}}); err == nil || !strings.Contains(err.Error(), "nije upisan") {
		t.Errorf("slanje bez poslužitelja: %v", err)
	}
	if k, err := prava.Imenik(ctx, posta.Postavke{}, racun, " "); k != nil || err != nil {
		t.Errorf("adresar s praznim upitom: %v %v", k, err)
	}
	if _, err := prava.Imenik(ctx, posta.Postavke{}, racun, "pero"); err == nil || !strings.Contains(err.Error(), "Exchange") {
		t.Errorf("adresar bez Exchangea: %v", err)
	}
}

// Prijava kojoj su pokušaji već potrošeni briše se i ne pušta ni točan PIN;
// prijava koja se ne da pročitati je greška, ne istek.
func TestPrijavaSPotrosenimPokusajima(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	ctx := context.Background()
	token, pin := o.novaPrijava(t)
	o.izvrsi(t, `UPDATE prijave_na_cekanju SET pokusaja = 5`)
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); !errors.Is(err, service.ErrPrevisePokusaja) {
		t.Errorf("točan PIN uz potrošene pokušaje: %v", err)
	}
	if _, err := o.dk.Cekanje(ctx, token); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("prijava s potrošenim pokušajima nije obrisana: %v", err)
	}
	// prijava koju je u međuvremenu potrošio drugi unos javlja istek, ne
	// previše pokušaja: okidač preskače brisanje, kao da je redak već nestao
	token, pin = o.novaPrijava(t)
	o.izvrsi(t, `UPDATE prijave_na_cekanju SET pokusaja = 5`)
	o.izvrsi(t, `CREATE TRIGGER vec_obrisana BEFORE DELETE ON prijave_na_cekanju BEGIN SELECT RAISE(IGNORE); END`)
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("prijava potrošena u međuvremenu: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER vec_obrisana`)

	// kad je osobi već puna granica unosa u tijeku, ni točan PIN se ne
	// uspoređuje; kad se ti unosi završe, isti PIN prolazi
	token, pin = o.novaPrijava(t)
	service.UnosaUTijekuZaTest(o.dk, o.osoba.ID, service.KrivihNaSatZaTest)
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); !errors.Is(err, service.ErrKodoviZakljucani) {
		t.Errorf("točan PIN uz punu granicu unosa u tijeku: %v", err)
	}
	service.UnosaUTijekuZaTest(o.dk, o.osoba.ID, 0)
	if _, err := o.dk.ProvjeriKod(ctx, token, pin); err != nil {
		t.Errorf("točan PIN kad se unosi završe: %v", err)
	}
	token, _ = o.novaPrijava(t)
	o.izvrsi(t, `UPDATE prijave_na_cekanju SET expires_at = 'nije vrijeme'`)
	if _, err := o.dk.Cekanje(ctx, token); err == nil || errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Errorf("nečitljiva prijava: %v", err)
	}
}
