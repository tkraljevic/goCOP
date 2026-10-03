package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		if s.zaglavlje != "" {
			r.Header.Set(s.zaglavlje, "203.0.113.7")
		}
		w := httptest.NewRecorder()
		ServeZdravlje(w, r)
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
