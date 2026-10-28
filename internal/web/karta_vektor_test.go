package web

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// probnaPMTiles je najmanja vektorska karta koju program prima
func probnaPMTiles() []byte {
	z := make([]byte, 127, 200)
	copy(z, "PMTiles")
	z[7], z[99], z[101] = 3, 1, 15
	for i, v := range []float64{18.3, 45.5, 18.95, 45.95} {
		binary.LittleEndian.PutUint32(z[102+4*i:], uint32(int32(v*1e7)))
	}
	return append(z, bytes.Repeat([]byte("pločica"), 8)...)
}

// Karta bez interneta: učitana PMTiles karta postaje zadana i poslužuje se
// po dijelovima, njezin paket se preuzima i učitava natrag, „Vrati zadano”
// je ne miče, a „Makni kartu” je miče i s diska
func TestKartaBezInterneta(t *testing.T) {
	prije := models.PostavkeKartePrograma()
	staraMapa := mapaKarata.Load()
	t.Cleanup(func() { models.SetPostavkeKarte(prije); mapaKarata.Store(staraMapa) })
	models.SetPostavkeKarte(models.ZadanePostavkeKarte())

	dbPath := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	postaviMapuKarata(dbPath)
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	stationRepo, readingRepo := repository.NewStationRepository(baza, rec), repository.NewReadingRepository(baza, rec)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, repository.NewSectionRepository(baza, rec),
		repository.NewTerritoryRepository(baza, rec), readingRepo, users, service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo), "test")
	n, err := peers.LoadNode(dbPath, "cop-proba", "cop-proba", "test")
	if err != nil {
		t.Fatal(err)
	}
	ps, err := peers.NewService(baza, rec, n, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	h := NewAktiHandler(func() *service.AktService { return akti }, users, nil, nil, nil, nil, nil)
	h.SetPeers(ps)
	uprava := &models.User{FullName: "Uprava", IsGlobalAdmin: true}
	kao := func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyUser, uprava), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	}
	ucitaj := func(datoteka []byte, polja map[string]string) string {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		for k, v := range polja {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		if datoteka != nil {
			fw, err := mw.CreateFormFile("datoteka", "karta")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fw.Write(datoteka); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/administracija/karte/vektorska", &b)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		h.HandleUcitajKartu(w, kao(r))
		return mustUnescape(w.Header().Get("Location"))
	}

	if loc := ucitaj(probnaPMTiles(), map[string]string{"naziv": "Baranja", "izvor": "OpenStreetMap (proba)"}); !strings.Contains(loc, "je učitana") {
		t.Fatalf("učitavanje karte: %s", loc)
	}
	k := models.PostavkeKartePrograma().Vektorska
	if k == nil || k.Naziv != "Baranja" || !kartaOvdje(k) || !imaVektorskuKartu() {
		t.Fatalf("zadana karta: %+v", k)
	}
	adresa := adresaVektorskeKarte(k)

	// preglednik traži kartu po dijelovima
	r := httptest.NewRequest(http.MethodGet, adresa, nil)
	r.SetPathValue("ime", k.Otisak+".pmtiles")
	r.Header.Set("Range", "bytes=0-6")
	w := httptest.NewRecorder()
	ServeVektorskaKarta(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "PMTiles" {
		t.Errorf("dio karte: %d %q", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/karta/vektor/drugi.pmtiles", nil)
	r.SetPathValue("ime", "drugi.pmtiles")
	w = httptest.NewRecorder()
	ServeVektorskaKarta(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("tuđa karta: %d", w.Code)
	}

	// paket za druga računala, pa učitavanje paketa na čvor bez karte
	w = httptest.NewRecorder()
	h.HandleIzveziKartu(w, kao(httptest.NewRequest(http.MethodGet, "/administracija/karte/vektorska.cop", nil)))
	paket := w.Body.Bytes()
	if !strings.Contains(w.Header().Get("Content-Disposition"), "gocop-karta-baranja.cop") || !bytes.HasPrefix(paket, []byte("PK")) {
		t.Fatalf("paket karte: %v", w.Header())
	}
	if err := os.Remove(peers.PutKarte(mapaKarataCvora(), k.Otisak)); err != nil {
		t.Fatal(err)
	}
	if kartaOvdje(k) || adresaVektorskeKarte(k) != "" {
		t.Error("karta se vidi iako je nema na disku")
	}
	if loc := ucitaj(paket, nil); !strings.Contains(loc, "je učitana") || !kartaOvdje(models.PostavkeKartePrograma().Vektorska) {
		t.Fatalf("učitavanje paketa: %s", loc)
	}
	// paket se ne da spremiti kad se u mapu karata ne smije pisati
	if err := os.Chmod(mapaKarataCvora(), 0o500); err != nil {
		t.Fatal(err)
	}
	loc := ucitaj(paket, nil)
	if err := os.Chmod(mapaKarataCvora(), 0o750); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loc, "error=") {
		t.Errorf("paket u mapu bez pisanja: %s", loc)
	}

	// „Vrati zadano” vraća izgled, a karta ostaje
	rz := httptest.NewRequest(http.MethodPost, "/administracija/karte", strings.NewReader(url.Values{"radnja": {"zadano"}}.Encode()))
	rz.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.HandlePostavkeKarte(httptest.NewRecorder(), kao(rz))
	if models.PostavkeKartePrograma().Vektorska == nil {
		t.Error("vraćanje zadanog maknulo je kartu")
	}

	for opis, c := range map[string]struct {
		dat  []byte
		zeli string
	}{
		"bez datoteke": {nil, "izaberite kartu"},
		"nije karta":   {[]byte(strings.Repeat("x", 200)), "nije karta u obliku PMTiles"},
		"loš paket":    {append([]byte("PK"), bytes.Repeat([]byte{0}, 100)...), "nije .cop paket"},
	} {
		if loc := ucitaj(c.dat, map[string]string{"naziv": "X"}); !strings.Contains(loc, c.zeli) {
			t.Errorf("%s: %s", opis, loc)
		}
	}
	w = httptest.NewRecorder()
	h.HandleUcitajKartu(w, kao(httptest.NewRequest(http.MethodPost, "/administracija/karte/vektorska", strings.NewReader("x"))))
	if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "nije stigla") {
		t.Errorf("obrazac bez datoteke: %s", loc)
	}

	w = httptest.NewRecorder()
	h.HandleMakniKartu(w, kao(httptest.NewRequest(http.MethodPost, "/administracija/karte/vektorska/makni", nil)))
	if models.PostavkeKartePrograma().Vektorska != nil {
		t.Error("karta nije maknuta")
	}
	if _, err := os.Stat(peers.PutKarte(mapaKarataCvora(), k.Otisak)); !os.IsNotExist(err) {
		t.Errorf("datoteka karte ostala: %v", err)
	}
	w = httptest.NewRecorder()
	h.HandleIzveziKartu(w, kao(httptest.NewRequest(http.MethodGet, "/administracija/karte/vektorska.cop", nil)))
	if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "nema zadanu kartu") {
		t.Errorf("paket bez karte: %s", loc)
	}

	// bez akata i bez razmjene
	bez := NewAktiHandler(func() *service.AktService { return nil }, users, nil, nil, nil, nil, nil)
	for _, f := range []http.HandlerFunc{bez.HandleUcitajKartu, bez.HandleMakniKartu} {
		w := httptest.NewRecorder()
		f(w, kao(httptest.NewRequest(http.MethodPost, "/", nil)))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("bez akata: %d", w.Code)
		}
	}
	bezRazmjene := NewAktiHandler(func() *service.AktService { return akti }, users, nil, nil, nil, nil, nil)
	if _, err := bezRazmjene.ucitajKartu(httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader("")))); err == nil {
		t.Error("učitavanje bez razmjene")
	}
	// bez baze postavki karta se ne postavlja ni ne miče
	if _, err := baza.Exec(`ALTER TABLE postavke RENAME TO postavke_nema`); err != nil {
		t.Fatal(err)
	}
	if loc := ucitaj(probnaPMTiles(), map[string]string{"naziv": "Baranja"}); !strings.Contains(loc, "error=") {
		t.Errorf("učitavanje bez baze postavki: %s", loc)
	}
	w = httptest.NewRecorder()
	h.HandleMakniKartu(w, kao(httptest.NewRequest(http.MethodPost, "/", nil)))
	if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "error=") {
		t.Errorf("micanje bez baze postavki: %s", loc)
	}
}

// Bez baze (npr. u testovima predložaka) čvor nema mapu karata, pa ni kartu
func TestBezMapeKarata(t *testing.T) {
	stara := mapaKarata.Load()
	t.Cleanup(func() { mapaKarata.Store(stara) })
	mapaKarata.Store(nil)
	k := &models.VektorskaKarta{Otisak: strings.Repeat("ab", 32), Bajtova: 1}
	if mapaKarataCvora() != "" || kartaOvdje(k) || adresaVektorskeKarte(k) != "" || adresaVektorskeKarte(nil) != "" {
		t.Error("karta bez mape karata")
	}
}
