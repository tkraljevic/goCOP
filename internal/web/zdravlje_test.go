package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /zdravlje je za Postavu na istom računalu: tunel (cloudflared je također
// na ovom računalu, ali dodaje zaglavlje) i mreža dobiju 404
func TestZdravljeSamoSOvogRacunala(t *testing.T) {
	stara := verzijaPrograma
	SetVerzijaPrograma("0.0.28-alfa (86f791c*)")
	defer SetVerzijaPrograma(stara)

	slucajevi := []struct {
		ime       string
		adresa    string
		zaglavlje string
		zelim     int
	}{
		{"ovo računalo", "127.0.0.1:50000", "", http.StatusOK},
		{"ovo računalo IPv6", "[::1]:50000", "", http.StatusOK},
		{"kroz tunel", "127.0.0.1:50000", ZaglavljeCloudflare, http.StatusNotFound},
		{"podmetnut X-Forwarded-For", "127.0.0.1:50000", "X-Forwarded-For", http.StatusNotFound},
		{"lokalna mreža", "192.168.1.5:50000", "", http.StatusNotFound},
	}
	for _, s := range slucajevi {
		r := httptest.NewRequest(http.MethodGet, "/zdravlje", nil)
		r.RemoteAddr = s.adresa
		r.Host = "127.0.0.1:1160" // Postava pita čvor na adresi petlje
		if s.zaglavlje != "" {
			r.Header.Set(s.zaglavlje, "203.0.113.7")
		}
		w := httptest.NewRecorder()
		(&Server{}).serveZdravlje(w, r)
		if w.Code != s.zelim {
			t.Errorf("%s: %d, želim %d", s.ime, w.Code, s.zelim)
			continue
		}
		if s.zelim != http.StatusOK {
			continue
		}
		var z Zdravlje
		if err := json.NewDecoder(w.Body).Decode(&z); err != nil {
			t.Fatal(err)
		}
		if z.Izdanje != "0.0.28-alfa" || !z.Radi {
			t.Errorf("%s: %+v", s.ime, z)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: odgovor se smije spremati", s.ime)
		}
	}
}

// Čvor koji nije smio krenuti, pod Postavom (Odluke (8. 10. 2026.), plan
// izdanja 0.0.35, stavka 6): /zdravlje kao i inače, uz razlog; svaka druga
// stranica kaže da čvor ne radi, a putanje i upute samo na ovom računalu
func TestOdbijenoPokretanje(t *testing.T) {
	stara := verzijaPrograma
	SetVerzijaPrograma("0.0.35-alfa (86f791c)")
	defer SetVerzijaPrograma(stara)
	h := OdbijenoPokretanje("Čvor se ne pokreće: nema ključa čvora /data/node-key.\nŠto učiniti: vratite ga.")

	zahtjev := func(put, adresa, zaglavlje string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, put, nil)
		r.RemoteAddr = adresa
		r.Host = "localhost:1160"
		if zaglavlje != "" {
			r.Header.Set(zaglavlje, "203.0.113.7")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := zahtjev("/zdravlje", "127.0.0.1:50000", "")
	var z Zdravlje
	if err := json.NewDecoder(w.Body).Decode(&z); err != nil || w.Code != http.StatusOK {
		t.Fatalf("/zdravlje: %d %v", w.Code, err)
	}
	if z != (Zdravlje{Izdanje: "0.0.35-alfa", Radi: true, Odbijeno: "Čvor se ne pokreće: nema ključa čvora /data/node-key."}) {
		t.Errorf("/zdravlje: %+v", z)
	}
	if w := zahtjev("/zdravlje", "127.0.0.1:50000", ZaglavljeCloudflare); w.Code != http.StatusNotFound {
		t.Errorf("/zdravlje kroz tunel: %d", w.Code)
	}

	for _, put := range []string{"/", "/prijava"} {
		w := zahtjev(put, "[::1]:50000", "")
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Što učiniti: vratite ga.") {
			t.Errorf("%s s ovog računala: %d %q", put, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: odgovor se smije spremati", put)
		}
	}
	for _, s := range []struct{ adresa, zaglavlje string }{
		{"127.0.0.1:50000", ZaglavljeCloudflare},
		{"192.168.1.5:50000", ""},
	} {
		w := zahtjev("/", s.adresa, s.zaglavlje)
		if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "/data") || !strings.Contains(w.Body.String(), "Čvor se ne pokreće") {
			t.Errorf("izvana (%s %s): %d %q", s.adresa, s.zaglavlje, w.Code, w.Body.String())
		}
	}
}
