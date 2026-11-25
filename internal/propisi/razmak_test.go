package propisi

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

type prijenosFunc func(*http.Request) (*http.Response, error)

func (f prijenosFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func odgovori(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

// Odluke (25. 11. 2026.): Narodne novine program pita najviše tri puta u
// sekundi, za cijeli proces: između dva zahtjeva (i preusmjeravanja) čeka
// 350 ms. Sat je podmetnut, pa test ne čeka.
func TestRazmakMedjuZahtjevima(t *testing.T) {
	sada := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	var cekanja []time.Duration
	r := noviRazmak(prijenosFunc(odgovori))
	r.sada = func() time.Time { return sada }
	r.cekaj = func(_ context.Context, d time.Duration) error {
		cekanja = append(cekanja, d)
		sada = sada.Add(d)
		return nil
	}
	posalji := func() {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://narodne-novine.nn.hr/", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := r.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	posalji()
	posalji()
	sada = sada.Add(100 * time.Millisecond)
	posalji()
	sada = sada.Add(time.Second)
	posalji()
	if zeli := []time.Duration{350 * time.Millisecond, 250 * time.Millisecond}; !reflect.DeepEqual(cekanja, zeli) {
		t.Errorf("čekanja %v, želi %v", cekanja, zeli)
	}
}

// Odluke (25. 11. 2026.): čekanje na red prekida se s rokom rukovatelja, a
// zahtjev se tada ne šalje.
func TestRazmakPrekidKonteksta(t *testing.T) {
	poslano := 0
	r := noviRazmak(prijenosFunc(func(req *http.Request) (*http.Response, error) {
		poslano++
		return odgovori(req)
	}))
	r.razmak = time.Hour
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://narodne-novine.nn.hr/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := r.RoundTrip(req); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
	}
	ctx, prekini := context.WithCancel(context.Background())
	prekini()
	if _, err := r.RoundTrip(req.WithContext(ctx)); !errors.Is(err, context.Canceled) || poslano != 1 {
		t.Errorf("greška %v, poslano %d", err, poslano)
	}
	if err := cekajRok(context.Background(), time.Millisecond); err != nil {
		t.Errorf("kratko čekanje: %v", err)
	}
}
