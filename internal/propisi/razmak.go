package propisi

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// razmakNN je najmanji razmak između dva upita Narodnim novinama, da ih
// program ne pita više od tri puta u sekundi.
const razmakNN = 350 * time.Millisecond

// razmakUpita drži razmak između HTTP zahtjeva cijelog procesa. Broji i
// zahtjev za preusmjeravanje (302), jer i njega šalje isti Transport.
type razmakUpita struct {
	dalje  http.RoundTripper
	razmak time.Duration

	mu     sync.Mutex
	zadnji time.Time
	sada   func() time.Time
	cekaj  func(context.Context, time.Duration) error
}

func noviRazmak(dalje http.RoundTripper) *razmakUpita {
	return &razmakUpita{dalje: dalje, razmak: razmakNN, sada: time.Now, cekaj: cekajRok}
}

// RoundTrip čeka da prođe razmak od prethodnog zahtjeva, pa šalje zahtjev.
// Čekanje prekida kontekst zahtjeva.
func (r *razmakUpita) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	var ceka time.Duration
	if !r.zadnji.IsZero() {
		ceka = r.zadnji.Add(r.razmak).Sub(r.sada())
	}
	if ceka > 0 {
		if err := r.cekaj(req.Context(), ceka); err != nil {
			r.mu.Unlock()
			return nil, err
		}
	}
	r.zadnji = r.sada()
	r.mu.Unlock()
	return r.dalje.RoundTrip(req)
}

// cekajRok čeka zadano vrijeme ili dok se kontekst ne prekine.
func cekajRok(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
