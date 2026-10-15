package razmjena

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"
)

// Odbijenica nosi kod i razlog koje je zadala aplikacija, a veza zna na
// koju je adresu ovog računala stigla: po njoj čvor odlučuje smije li
// razmjena na tim vratima (npr. Tailscale adresa dok je Tailscale
// isključen).
func TestOdbijanjeSRazlogomIAdresaVeze(t *testing.T) {
	posluzitelj, klijent := newKey(t), newKey(t)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	stigla := make(chan string, 1)
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(4, 4, time.Second), func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
		stigla <- c.LocalAddr().String()
		Odbij(c, KodOdbijeno, "razmjena na ovim vratima nije dopuštena")
	})
	c, err := DialExchange(ctx, klijent, "testproto", nil, adresa, posluzitelj.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	granica, _ := NewEnvelope("frontier", map[string]string{"a": "1"})
	if err := c.Send(granica); err != nil {
		t.Fatal(err)
	}
	e, err := c.Receive()
	if err != nil {
		t.Fatalf("odbijenica nije stigla: %v", err)
	}
	if e.Kind != VrstaOdbijeno || e.Reason != "razmjena na ovim vratima nije dopuštena" || e.Kod != KodOdbijeno {
		t.Errorf("odbijenica: %+v", e)
	}
	if got := <-stigla; got != adresa {
		t.Errorf("veza je stigla na %s, slušalica je na %s", got, adresa)
	}
	if c.LocalAddr().String() == adresa || c.LocalAddr().String() == "" {
		t.Errorf("adresa pozivatelja: %v", c.LocalAddr())
	}
}
