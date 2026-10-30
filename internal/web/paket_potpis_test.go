package web

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// paketLetve sastavlja paket historijata letve iz male arhive; s ključem je
// potpisan, bez njega nije
func paketLetve(t *testing.T, letva string, kljuc ed25519.PrivateKey) []byte {
	t.Helper()
	koren, baza := t.TempDir(), filepath.Join(t.TempDir(), "arhiva.db")
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	if _, err := arhiva.Upisi(koren, "dunav", letva, "cop", "vodostaj", "satni", []arhiva.Redak{{Vrijeme: kad, Vrijednost: -160}}); err != nil {
		t.Fatal(err)
	}
	if _, err := arhiva.Izgradi(koren, baza, letva, io.Discard); err != nil {
		t.Fatal(err)
	}
	a, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	arhiva.PostaviKljucIzdavaca(kljuc)
	defer arhiva.PostaviKljucIzdavaca(nil)
	var b bytes.Buffer
	if _, err := arhiva.Izvezi(a, letva, 1, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// okolinaPaketa je historijat letve Batina s pregledom i ugradnjom paketa;
// ugradjeno pamti što je stiglo do ugradnje
type okolinaPaketa struct {
	h         *StationsHandler
	letva     *models.Station
	ugradjeno []string
	adresa    string // s koje adrese stižu zahtjevi; zadano iz lokalne mreže
}

func novaOkolinaPaketa(t *testing.T) *okolinaPaketa {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "paket.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "test")
	stationRepo := repository.NewStationRepository(baza, rec)
	stations := service.NewStationService(stationRepo,
		service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker()), service.NewSSEBroker())
	o := &okolinaPaketa{letva: &models.Station{ID: uuid.New(), Code: "batina", Name: "Batina", Watercourse: "Dunav"}, adresa: adresaLokalno}
	if err := stationRepo.CreateStation(context.Background(), o.letva); err != nil {
		t.Fatal(err)
	}
	o.h = NewStationsHandler(stations, nil)
	o.h.SetPaket(func() string { return "" }, func() string { return "test" },
		func(s *arhiva.Sadrzaj) error { o.ugradjeno = append(o.ugradjeno, s.Manifest.Letva); return nil },
		predlozakStranice(t, "paket_pregled.html"))
	return o
}

func (o *okolinaPaketa) zahtjev(metoda, putanja string, tijelo io.Reader, vrsta string) *http.Request {
	r := httptest.NewRequest(metoda, putanja, tijelo)
	r.RemoteAddr, r.Host = o.adresa, adresaCvoraTesta
	if vrsta != "" {
		r.Header.Set("Content-Type", vrsta)
	}
	r.SetPathValue("id", o.letva.ID.String())
	c := context.WithValue(r.Context(), contextKeyUser, adminIvic)
	return r.WithContext(context.WithValue(c, contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
}

// pregledaj šalje paket na pregled i vraća stranicu pregleda
func (o *okolinaPaketa) pregledaj(t *testing.T, paket []byte) string {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	f, _ := mw.CreateFormFile("paket", "batina_v1.cop") // upis u memoriju ne pada
	_, _ = f.Write(paket)                               // upis u memoriju ne pada
	_ = mw.Close()                                      // upis u memoriju ne pada
	w := httptest.NewRecorder()
	o.h.PregledPaketa(w, o.zahtjev(http.MethodPost, "/stations/x/paket/pregled", &b, mw.FormDataContentType()))
	return w.Body.String()
}

// ugradi stavlja paket u pripremu, kao pregled, i potvrđuje ugradnju
func (o *okolinaPaketa) ugradi(t *testing.T, paket []byte, letva string) (string, string) {
	t.Helper()
	put := filepath.Join(t.TempDir(), "paket.cop")
	if err := os.WriteFile(put, paket, 0o600); err != nil {
		t.Fatal(err)
	}
	kljuc := cekaju.spremi(put, letva)
	w := httptest.NewRecorder()
	o.h.UgradiPaket(w, o.zahtjev(http.MethodPost, "/stations/x/paket/ugradi",
		strings.NewReader("kljuc="+kljuc), "application/x-www-form-urlencoded"))
	_, upit := preusmjerenje(t, w)
	return upit.Get("success"), upit.Get("error")
}

// Odluke (30. 10. 2026.), 13: nepotpisan paket letve se ne ugrađuje, ni
// s pregleda ni kad se ugradnja potvrdi. Potpisan paket prolazi, a pregled
// kaže tko ga je potpisao.
func TestPaketLetveBezPotpisaSeOdbija(t *testing.T) {
	_, kljuc, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	nepotpisan, potpisan := paketLetve(t, "batina", nil), paketLetve(t, "batina", kljuc)
	o := novaOkolinaPaketa(t)

	if html := o.pregledaj(t, []byte("nije paket")); !strings.Contains(html, "paket nije ZIP") || strings.Contains(html, "Ugradi historijat") {
		t.Error("pregled datoteke koja nije paket")
	}
	html := o.pregledaj(t, nepotpisan)
	if !strings.Contains(html, "paket nije potpisan, pa se ne ugrađuje") || strings.Contains(html, "Ugradi historijat") {
		t.Error("pregled nudi ugradnju nepotpisanog paketa")
	}
	if _, greska := o.ugradi(t, nepotpisan, "batina"); !strings.Contains(greska, "paket nije potpisan") || len(o.ugradjeno) != 0 {
		t.Errorf("ugradnja nepotpisanog paketa: %q, ugrađeno %v", greska, o.ugradjeno)
	}

	html = o.pregledaj(t, potpisan)
	if !strings.Contains(html, "Paket kaže da ga je izdao čvor cop-osijek.") || !strings.Contains(html, "Ugradi historijat") {
		t.Error("pregled potpisanog paketa ne kaže tko ga je potpisao ili ne nudi ugradnju")
	}
	if uspjeh, greska := o.ugradi(t, potpisan, "batina"); uspjeh == "" || greska != "" || len(o.ugradjeno) != 1 {
		t.Errorf("ugradnja potpisanog paketa: %q %q, ugrađeno %v", uspjeh, greska, o.ugradjeno)
	}

	// paket druge letve ne ugrađuje se ni kad je ugradnja potvrđena
	if _, greska := o.ugradi(t, paketLetve(t, "vukovar", kljuc), "vukovar"); !strings.Contains(greska, "paket nosi historijat letve vukovar") || len(o.ugradjeno) != 1 {
		t.Errorf("ugradnja paketa druge letve: %q, ugrađeno %v", greska, o.ugradjeno)
	}
}

// Potpis mora biti člana mreže ili ovog računala, kao kod .cop paketa
// kanala. Dok računalo nije u mreži, ne zna čiji je ključ, pa paket ugrađuje
// samo s ovog računala ili iz lokalne mreže, kao staru .db arhivu (Odluke
// (30. 10. 2026.), 13).
func TestPaketLetveTraziPotpisClanaMreze(t *testing.T) {
	javni, kljuc, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	potpisan := paketLetve(t, "batina", kljuc)
	o := novaOkolinaPaketa(t)
	var pitan ed25519.PublicKey
	odgovor := func(p peers.Potpisnik, err error) {
		o.h.SetPotpisnik(func(_ context.Context, izdao string, k ed25519.PublicKey) (peers.Potpisnik, error) {
			if izdao != "cop-osijek" {
				t.Errorf("pitan za izdavača %q", izdao)
			}
			pitan = k
			return p, err
		})
	}
	// prolazi kaže da pregled nudi ugradnju s porukom poruka i da se paket
	// potvrdom ugrađuje; inače oboje javljaju grešku poruka
	provjeri := func(opis, poruka string, prolazi bool) {
		t.Helper()
		prije := len(o.ugradjeno)
		html := o.pregledaj(t, potpisan)
		if !strings.Contains(html, poruka) || strings.Contains(html, "Ugradi historijat") != prolazi {
			t.Errorf("%s: pregled ne kaže %q ili krivo nudi ugradnju", opis, poruka)
		}
		zeli := 0
		if prolazi {
			zeli = 1
		}
		uspjeh, greska := o.ugradi(t, potpisan, "batina")
		if prolazi != (uspjeh != "") || (!prolazi && !strings.Contains(greska, poruka)) || len(o.ugradjeno)-prije != zeli {
			t.Errorf("%s: ugradnja %q %q, ugrađeno %v", opis, uspjeh, greska, o.ugradjeno)
		}
	}

	odgovor(peers.PotpisnikNeprovjeren, errors.New("paket je potpisao čvor cop-osijek koji nije član naše mreže"))
	provjeri("ključ izvan mreže", "nije član naše mreže", false)
	if !pitan.Equal(javni) {
		t.Error("članstvo se ne provjerava po ključu kojim je paket potpisan")
	}

	bezMreze := "Paket kaže da ga je izdao čvor cop-osijek. Potpis je ispravan, ali ovo računalo još nije u mreži"
	odgovor(peers.PotpisnikNeprovjeren, nil)
	provjeri("bez mreže, iz lokalne mreže", bezMreze, true)
	odgovor(peers.PotpisnikClan, nil)
	provjeri("član mreže", "Paket je potpisao čvor cop-osijek, član mreže.", true)
	odgovor(peers.PotpisnikOvoRacunalo, nil)
	provjeri("ovo računalo", "Paket je potpisalo ovo računalo.", true)

	// izvana: član mreže i ovo računalo prolaze, a neprovjeren ključ ne
	o.adresa = adresaIzvana
	provjeri("ovo računalo, izvana", "Paket je potpisalo ovo računalo.", true)
	odgovor(peers.PotpisnikClan, nil)
	provjeri("član mreže, izvana", "član mreže.", true)
	odgovor(peers.PotpisnikNeprovjeren, nil)
	provjeri("bez mreže, izvana", zabranaIzvana.neprovjeren, false)
	o.h.SetPotpisnik(nil)
	provjeri("bez provjere potpisnika, izvana", zabranaIzvana.neprovjeren, false)
	o.adresa = adresaLokalno
	provjeri("bez provjere potpisnika, iz lokalne mreže", bezMreze, true)
}

// Poslužitelj bez razmjene ne zna čiji je ključ, a s razmjenom pita nju;
// računalo koje još nije u mreži tuđi ključ ostavlja neprovjerenim.
func TestPotpisnikPaketaNaPosluzitelju(t *testing.T) {
	javni, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	if p, err := s.potpisnikPaketa(context.Background(), "cop-osijek", javni); p != peers.PotpisnikNeprovjeren || err != nil {
		t.Errorf("bez razmjene: %v %v", p, err)
	}
	s.peersService, _, _ = cvorZaUvoz(t, "novi-laptop")
	if p, err := s.potpisnikPaketa(context.Background(), "cop-osijek", javni); p != peers.PotpisnikNeprovjeren || err != nil {
		t.Errorf("računalo bez mreže: %v %v", p, err)
	}
}
