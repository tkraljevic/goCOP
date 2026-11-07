package service_test

// Obavijest o roku članstva u mreži i o kopiji ključa mreže (G9b): ide
// svakom aktivnom globalnom administratoru na adresu na koju smije ići PIN,
// računom za slanje PIN-a.

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
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

	// račun za slanje koji se ne da pročitati je greška baze, ne „nema
	// pošiljatelja”
	if _, err := o.baza.Exec(`ALTER TABLE racuni_sustava RENAME TO racuni_negdje`); err != nil {
		t.Fatal(err)
	}
	if err := o.dk.PosaljiObavijest(ctx, pero, "goCOP: rok", "tekst"); err == nil || errors.Is(err, service.ErrNemaPosiljatelja) {
		t.Errorf("račun se ne da pročitati: %v", err)
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

// Nova prijava kroz pravi račun za slanje: poruka ide administratoru bez slike i
// bez kopije u Poslano, a ima svoje brojilo, pa niz prijava ne zadrži obavijest
// o roku članstva
func TestPorukaOPrijaviKrozRacunZaSlanje(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	if _, err := o.baza.Exec(`UPDATE users SET is_global_admin = 1 WHERE username = 'uprava'`); err != nil {
		t.Fatal(err)
	}
	prije := repository.Spremiste()
	t.Cleanup(func() { repository.SetSpremiste(prije) })
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	repository.SetSpremiste(sp)

	repo := repository.NewPrijavaGreskeRepository(o.baza, ledger.New(o.baza, "cvor-a"))
	s := service.NewPrijavaGreskeService(repo, "cvor-a")
	defer s.PokreniPoruke(ctx, o.dk, func() string { return "https://cop.primjer.hr" })()
	var slika bytes.Buffer
	if err := png.Encode(&slika, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	pocetak := o.posta.broj()
	p, err := s.Posalji(ctx, &models.User{ID: uuid.New(), FullName: "Mara Marić"}, service.UlazPrijaveGreske{Opis: "Graf se ne crta.", Stranica: "/karta", Slika: slika.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	for range 400 {
		if o.posta.broj() > pocetak {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if o.posta.broj() != pocetak+1 {
		t.Fatalf("poruka nije poslana: %d", o.posta.broj()-pocetak)
	}
	m := o.posta.zadnja(t)
	if m.Za.Address != "uprava@voda.hr" || !strings.Contains(m.Tekst, "https://cop.primjer.hr/administracija/prijave-gresaka/"+p.ID) || !m.BezKopije {
		t.Errorf("poruka: %+v", m)
	}
	if len(m.Privitci) != 0 || len(m.Ugradjene) != 0 || m.HTML != "" {
		t.Errorf("slika je u poruci: %d %d", len(m.Privitci), len(m.Ugradjene))
	}

	// granica po osobi se broji odvojeno: četvrta poruka istoj osobi stane, a
	// obavijest o roku i dalje ide
	uprava := service.PrimateljObavijesti{ID: "uprava", Ime: "Uprava", Adresa: "uprava@voda.hr"}
	for range 3 {
		if err := o.dk.PosaljiPorukuOPrijavi(ctx, uprava, "goCOP: prijava", "tekst"); err != nil {
			t.Fatal(err)
		}
	}
	if err := o.dk.PosaljiPorukuOPrijavi(ctx, uprava, "goCOP: prijava", "tekst"); !errors.Is(err, service.ErrSlanjeOgraniceno) {
		t.Errorf("preko granice: %v", err)
	}
	if err := o.dk.PosaljiObavijest(ctx, uprava, "goCOP: rok članstva u mreži", "tekst"); err != nil {
		t.Errorf("obavijest nakon poruka o prijavama: %v", err)
	}
	bezKljuca := service.NewDrugiKorak(o.dkRepo, repository.NewRacuniSustavaRepository(o.baza), o.repo, repository.NewAktiRepository(o.baza, ledger.New(o.baza, "cvor-a")))
	if err := bezKljuca.PosaljiPorukuOPrijavi(ctx, uprava, "goCOP: prijava", "tekst"); !errors.Is(err, service.ErrNemaKljucaPrijave) {
		t.Errorf("bez ključa: %v", err)
	}
}

// Poruku o prijavi može pokrenuti svaki prijavljeni korisnik: kad joj veza pukne,
// PIN i dalje ide. Stanku koju je postavio PIN poruka o prijavi poštuje.
func TestPorukaOPrijaviNeZaustavljaPIN(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	ctx := context.Background()
	uprava := service.PrimateljObavijesti{ID: "uprava", Ime: "Uprava", Adresa: "uprava@voda.hr"}
	veza := func(err error) {
		o.posta.mu.Lock()
		defer o.posta.mu.Unlock()
		o.posta.greska = err
	}

	veza(errors.New("veza je pukla"))
	if err := o.dk.PosaljiPorukuOPrijavi(ctx, uprava, "goCOP: prijava", "tekst"); !errors.Is(err, service.ErrSlanjeZastalo) {
		t.Errorf("slanje bez veze: %v", err)
	}
	if st, err := o.dk.Stanje(ctx); err != nil || st.StankaDo.After(o.sad) {
		t.Errorf("poruka o prijavi je zaustavila slanje: %+v %v", st, err)
	}
	veza(nil)
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err != nil {
		t.Errorf("PIN nakon pukle poruke o prijavi: %v", err)
	}

	veza(errors.New("veza je pukla"))
	if _, err := o.dk.PosaljiProbniPIN(ctx, o.admin); err == nil {
		t.Fatal("PIN bez veze")
	}
	veza(nil)
	pocetak := o.posta.broj()
	if err := o.dk.PosaljiPorukuOPrijavi(ctx, uprava, "goCOP: prijava", "tekst"); !errors.Is(err, service.ErrSlanjeZastalo) {
		t.Errorf("poruka za vrijeme stanke: %v", err)
	}
	if o.posta.broj() != pocetak {
		t.Error("poruka je otišla za vrijeme stanke")
	}
}
