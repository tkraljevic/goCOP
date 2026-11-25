package javnivodostaji

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Odluke (25. 11. 2026.): propisi se dohvaćaju istim čuvanim klijentom kao
// javni vodostaji. Klijent ima zadani rok, ne slijedi preusmjeravanje na
// drugi poslužitelj i ne spaja se na lokalnu mrežu, pa ni poveznica s
// tuđim poslužiteljem ne može program navesti na unutarnju adresu.
func TestJavniKlijentCuvaSpojIPreusmjeravanje(t *testing.T) {
	k := JavniKlijent(7 * time.Second)
	if k.Timeout != 7*time.Second {
		t.Errorf("rok %v, želi 7s", k.Timeout)
	}
	if k.CheckRedirect == nil {
		t.Fatal("klijent slijedi svako preusmjeravanje")
	}
	stiglo := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { stiglo = true }))
	defer srv.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := k.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	if !errors.Is(err, errLokalnaAdresa) || stiglo {
		t.Errorf("spoj na 127.0.0.1: greška %v, zahtjev stigao %v", err, stiglo)
	}
}
