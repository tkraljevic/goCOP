package mletva

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Popuštanje vrijedi samo za istek. Certifikat koji nije potpisalo poznato
// tijelo odbija se kao i inače, pa lozinka ne ode nikome tko se samo
// predstavi kao sustav.
func TestTLSOdbijaNepoznatogPotpisnika(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("lozinka je stigla do poslužitelja s nepoznatim certifikatom")
	}))
	defer s.Close()
	k := &Klijent{Adresa: s.URL}
	err := k.Prijava(context.Background(), "pero", "tajna")
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("greška %v", err)
	}
}
