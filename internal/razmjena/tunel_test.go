package razmjena

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Razmjena kroz web tunel: HTTPS poslužitelj (kao Cloudflare ispred čvora)
// prima WebSocket na PutTunela, a unutra teče isti TLS s ključevima čvorova.
// Upareni ključ prolazi, nepoznati ne, a krivi očekivani ključ odbija pozivatelj.
func TestRazmjenaKrozTunel(t *testing.T) {
	_, kljucA, _ := ed25519.GenerateKey(rand.Reader)
	_, kljucB, _ := ed25519.GenerateKey(rand.Reader)
	_, stranac, _ := ed25519.GenerateKey(rand.Reader)
	javniA := kljucA.Public().(ed25519.PublicKey)
	javniB := kljucB.Public().(ed25519.PublicKey)

	tunel := NoviTunel()
	mux := http.NewServeMux()
	mux.Handle("GET "+PutTunela, tunel.Handler())
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	vanjskiTLS = srv.Client().Transport.(*http.Transport).TLSClientConfig
	defer func() { vanjskiTLS = nil }()

	ctx, otkazi := context.WithTimeout(context.Background(), 20*time.Second)
	defer otkazi()
	adresaDruge := make(chan string, 1)
	go ServeExchangeOn(ctx, kljucB, "gocop-test", tunel, func(k ed25519.PublicKey) bool { return k.Equal(javniA) }, func(c *Conn) {
		defer c.Close()
		// bilješka o razmjeni čita adresu druge strane; WebSocket na strani
		// poslužitelja je nema, a prazan URL je srušio čvor (0.0.12-alfa)
		adresaDruge <- c.RemoteAddr().String()
		e, err := c.Receive()
		if err != nil {
			return
		}
		odg, _ := NewEnvelope("odgovor", map[string]string{"stiglo": e.Kind})
		c.Send(odg)
	})

	adresa := srv.URL
	c, err := DialTunel(ctx, kljucA, "gocop-test", adresa, javniB)
	if err != nil {
		t.Fatalf("spajanje kroz tunel: %v", err)
	}
	pitanje, _ := NewEnvelope("frontier", map[string]int{"a": 1})
	if err := c.Send(pitanje); err != nil {
		t.Fatal(err)
	}
	e, err := c.Receive()
	if err != nil || e.Kind != "odgovor" {
		t.Fatalf("odgovor kroz tunel: %+v %v", e, err)
	}
	if a := <-adresaDruge; a == "" {
		t.Error("veza kroz tunel nema adresu druge strane")
	}
	_ = c.RemoteAddr().String()
	c.Close()

	// čvor kojeg druga strana ne poznaje ne dobiva ni bajt razmjene
	if c, err := DialTunel(ctx, stranac, "gocop-test", adresa, javniB); err == nil {
		pitanje, _ := NewEnvelope("frontier", nil)
		c.Send(pitanje)
		if _, err := c.Receive(); err == nil {
			t.Error("nepoznati ključ je prošao kroz tunel")
		}
		c.Close()
	}
	// pozivatelj koji očekuje drugi ključ odbija vezu
	if _, err := DialTunel(ctx, kljucA, "gocop-test", adresa, stranac.Public().(ed25519.PublicKey)); err == nil {
		t.Error("kriv ključ druge strane nije odbijen")
	}
}

func TestAdresaTunela(t *testing.T) {
	for ulaz, ocekivano := range map[string]string{
		"https://cop-osijek.com":                 "https://cop-osijek.com",
		"https://COP-Osijek.com/razmjena/tunel/": "https://cop-osijek.com",
		"wss://cop-osijek.com:8443":              "https://cop-osijek.com:8443",
	} {
		if !JeAdresaTunela(ulaz) {
			t.Errorf("%s nije prepoznata kao tunel", ulaz)
		}
		if n, err := NormalizirajTunel(ulaz); err != nil || n != ocekivano {
			t.Errorf("%s → %q (%v), očekivano %q", ulaz, n, err, ocekivano)
		}
	}
	if JeAdresaTunela("cop-osijek.com:4710") {
		t.Error("obična adresa prepoznata kao tunel")
	}
}
