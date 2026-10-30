package web

// Sastajalište (ograda_signala.go): ruta je 404 dok računalo nije
// sastajalište ili nema rukovatelja; veza ide rukovatelju tek uz oboje, a
// ograda pusti najviše 8 veza s jedne adrese. Svaka WebSocket ruta je bez
// rokova i registrirana u server.go.

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"gocop/internal/config"
	"gocop/internal/razmjena"
)

func TestRutaSignala(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	stigao := 0
	trazi := func() int {
		w := httptest.NewRecorder()
		s.handleSignal(w, httptest.NewRequest(http.MethodGet, razmjena.PutSignala, nil))
		return w.Code
	}
	postavi := func(prijava, posluzi bool) {
		p := s.postavke.snimka()
		p.Vrijedi.Povezivost = config.Povezivost{WebSyncPrijava: prijava, PosluziSastajaliste: posluzi}
		s.SetPostavkeCvora(p)
	}
	if trazi() != http.StatusNotFound {
		t.Error("bez rukovatelja i bez postavke")
	}
	s.SetSignal(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stigao++ }))
	for _, c := range []struct{ prijava, posluzi bool }{{false, false}, {false, true}, {true, false}} {
		postavi(c.prijava, c.posluzi)
		if trazi() != http.StatusNotFound || stigao != 0 {
			t.Errorf("%+v: veza prošla", c)
		}
	}
	postavi(true, true)
	if trazi() != http.StatusOK || stigao != 1 {
		t.Errorf("sastajalište: rukovatelj pozvan %d puta", stigao)
	}
	if (&Server{}).posluziSastajaliste() {
		t.Error("poslužitelj bez postavki je sastajalište")
	}
}

// Ograda sastajališta: deveta veza s iste adrese dobije 503 s Retry-After,
// dok druga adresa i dalje ulazi
func TestOgradaSignala(t *testing.T) {
	pusti := make(chan struct{})
	usla := make(chan struct{}, najviseSignalaPoKlijentu+2)
	h := (&Server{}).klijentSloj(ograditiSignal(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usla <- struct{}{}
		<-pusti
	})))
	zahtjev := func(adresa string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, razmjena.PutSignala, nil)
		r.RemoteAddr = adresa + ":5555"
		r.Host = adresaCvoraTesta
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	gotovo := make(chan struct{}, najviseSignalaPoKlijentu+1)
	for range najviseSignalaPoKlijentu {
		go func() { zahtjev("198.51.100.7"); gotovo <- struct{}{} }()
	}
	for range najviseSignalaPoKlijentu {
		<-usla
	}
	if w := zahtjev("198.51.100.7"); w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Errorf("deveta veza s iste adrese: %d", w.Code)
	}
	go func() { zahtjev("198.51.100.8"); gotovo <- struct{}{} }()
	select {
	case <-usla:
	case <-time.After(5 * time.Second):
		t.Fatal("druga adresa nije ušla")
	}
	close(pusti)
	for range najviseSignalaPoKlijentu + 1 {
		<-gotovo
	}
	if najviseSignala < 64 {
		t.Errorf("ukupna granica %d manja od registra sastajališta (64)", najviseSignala)
	}
}

// Svaka WebSocket ruta je netaknuto i registrirana u server.go, a svaka
// ruta s putom iz paketa razmjena je na popisu WebSocket ruta
func TestWebSocketRuteNetaknute(t *testing.T) {
	var uzorci []string
	for _, r := range ruteIzIzvora(t) {
		uzorci = append(uzorci, r.uzorak)
	}
	for _, r := range webSocketRute {
		if !pravilaRuta[r].netaknuto {
			t.Errorf("%s nije netaknuto: rok od 60 s bi je prekinuo", r)
		}
		if !slices.Contains(uzorci, r) {
			t.Errorf("%s nije registrirana u server.go", r)
		}
	}
	for _, put := range putoviRazmjene {
		if !slices.Contains(webSocketRute, "GET "+put) {
			t.Errorf("%s nije na popisu WebSocket ruta", put)
		}
	}
}
