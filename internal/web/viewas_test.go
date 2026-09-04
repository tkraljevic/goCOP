package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Zapis pod tuđim imenom ne smije nastati ni omaškom, pa se u tuđem pogledu
// propušta samo čitanje i izlaz natrag k sebi.
func TestUTudemPogleduProlaziSamoCitanje(t *testing.T) {
	slucajevi := []struct {
		metoda, putanja string
		prolazi         bool
	}{
		{http.MethodGet, "/users", true},
		{http.MethodGet, "/api/sections/A.19.1", true},
		{http.MethodHead, "/dashboard", true},
		{http.MethodPost, "/users/create", false},
		{http.MethodPost, "/users/delete", false},
		{http.MethodPost, "/sections/update", false},
		{http.MethodPost, "/profile/change-password", false},
		{http.MethodPost, "/api/network/create", false},
		{http.MethodPost, "/view-as/stop", true},
		{http.MethodPost, "/view-as/9f1c0b2e-0000-7000-8000-000000000000", true},
	}

	for _, s := range slucajevi {
		r := httptest.NewRequest(s.metoda, s.putanja, nil)
		if got := readOnlyRequest(r); got != s.prolazi {
			t.Errorf("%s %s: prolazi = %v, očekivano %v", s.metoda, s.putanja, got, s.prolazi)
		}
	}
}
