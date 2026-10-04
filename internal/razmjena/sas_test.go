package razmjena

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func slobodanPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// Stariji program dogovara kod bez obveze unaprijed; takav se kod može
// namjestiti, pa se s njim ne uparuje
func TestUparivanjeOdbijaStarijiDogovorKoda(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, stari, _ := ed25519.GenerateKey(rand.Reader)
	_, novi, _ := ed25519.GenerateKey(rand.Reader)
	cfg, err := tlsConfig(stari, "app")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if c.(*tls.Conn).Handshake() != nil {
			return
		}
		_ = json.NewEncoder(c).Encode(Hello{Protocol: "app", DeviceID: "stari"})
		_, _ = io.Copy(io.Discard, c)
	}()
	if _, err := Dial(ctx, novi, Identity{Protocol: "app", DeviceID: "novi"}, ln.Addr().String()); !errors.Is(err, ErrStaroUparivanje) {
		t.Fatalf("želim ErrStaroUparivanje, dobio %v", err)
	}
}

// Pozivatelj koji otkrije drugi broj od onoga na koji se obvezao (napadač
// koji bi tako namjestio kod) prekida uparivanje
func TestUparivanjeOdbijaPrekrsenuObvezu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, slusalica, _ := ed25519.GenerateKey(rand.Reader)
	_, varalica, _ := ed25519.GenerateKey(rand.Reader)
	port := slobodanPort(t)
	gotovo := make(chan error, 1)
	go func() {
		_, err := Listen(ctx, slusalica, Identity{Protocol: "app", DeviceID: "slusalica"}, port)
		gotovo <- err
	}()

	cfg, err := tlsConfig(varalica, "app")
	if err != nil {
		t.Fatal(err)
	}
	var c *tls.Conn
	for i := 0; i < 50; i++ {
		if c, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), cfg); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	_ = enc.Encode(Hello{Protocol: "app", DeviceID: "varalica", SAS: sasInacica})
	var h Hello
	if err := dec.Decode(&h); err != nil {
		t.Fatal(err)
	}
	obecan, otkriven := make([]byte, velicinaBroja), make([]byte, velicinaBroja)
	_, _ = rand.Read(obecan)
	_, _ = rand.Read(otkriven)
	_ = enc.Encode(sasPoruka{Obveza: obvezaZa("app", obecan)})
	var m sasPoruka
	if err := dec.Decode(&m); err != nil || len(m.Broj) != velicinaBroja {
		t.Fatalf("slušalica nije poslala svoj broj: %+v %v", m, err)
	}
	_ = enc.Encode(sasPoruka{Broj: otkriven})

	select {
	case err := <-gotovo:
		if !errors.Is(err, ErrObveza) {
			t.Fatalf("želim ErrObveza, dobio %v", err)
		}
	case <-ctx.Done():
		t.Fatal("slušalica nije prekinula uparivanje")
	}
}
