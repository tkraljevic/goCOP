package razmjena

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Provjera ključa stane čim druga strana dokaže ključ: druga strana ne
// pita vjeruje li ovom računalu i ne otvori razmjenu, nego broji rukovanje
// koje nije završilo. Drugi ključ, pokvaren vlastiti ključ i otkazana
// provjera javljaju grešku, a veza je uvijek zatvorena.
func TestProvjeraKljucaNeOtvaraRazmjenu(t *testing.T) {
	posluzitelj, klijent := newKey(t), newKey(t)
	javni := posluzitelj.Public().(ed25519.PublicKey)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	var pitan, obrada atomic.Int32
	odbijeno := make(chan RazlogVrata, 8)
	o := novaOgrada(4, 4, time.Second)
	o.KadOdbije(func(r RazlogVrata, _ ed25519.PublicKey) { odbijeno <- r })
	adresa := slusalica(t, ctx, posluzitelj, o, func(ed25519.PublicKey, []byte) bool { pitan.Add(1); return true },
		func(c *Conn) { obrada.Add(1); c.Close() })

	v := otvori(t, adresa)
	rtt, err := ProvjeriKljucOn(ctx, v, klijent, "testproto", adresa, javni)
	if err != nil || rtt <= 0 || !v.zatvorena {
		t.Fatalf("provjera: %v, %v, veza zatvorena %v", rtt, err, v.zatvorena)
	}
	select {
	case r := <-odbijeno:
		if r != VrataRukovanje {
			t.Errorf("druga strana broji %q", r)
		}
	case <-ctx.Done():
		t.Fatal("druga strana nije zabilježila prekinuto rukovanje")
	}

	v = otvori(t, adresa)
	if _, err := ProvjeriKljucOn(ctx, v, klijent, "testproto", adresa, newKey(t).Public().(ed25519.PublicKey)); !errors.Is(err, ErrDrugiKljuc) || !v.zatvorena {
		t.Errorf("drugi ključ: %v, veza zatvorena %v", err, v.zatvorena)
	}
	v = otvori(t, adresa)
	if _, err := ProvjeriKljucOn(ctx, v, pokvarenKljuc(t), "testproto", adresa, javni); err == nil || !v.zatvorena {
		t.Errorf("pokvaren ključ: %v, veza zatvorena %v", err, v.zatvorena)
	}
	otkazan, odmah := context.WithCancel(ctx)
	odmah()
	v = otvori(t, adresa)
	if _, err := ProvjeriKljucOn(otkazan, v, klijent, "testproto", adresa, javni); !errors.Is(err, context.Canceled) || !v.zatvorena {
		t.Errorf("otkazana provjera: %v, veza zatvorena %v", err, v.zatvorena)
	}
	if p, n := pitan.Load(), obrada.Load(); p != 0 || n != 0 {
		t.Errorf("druga strana je pitana %d puta i otvorila %d razmjena", p, n)
	}
}

// Kroz tunel isto: RTT je jedan obilazak, a B ne dođe do pitanja o ključu
// ni do obrade. Drugi ključ javlja se rečenicom tunela, a kriva adresa i
// tunel koji se ne javlja greškom s adresom.
func TestProvjeraKljucaKrozTunel(t *testing.T) {
	const adresa = "https://cop.primjer.hr"
	uMjehuru(t, func(t *testing.T) {
		kljucA, kljucB := newKey(t), newKey(t)
		tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
		rtt, err := tu.klijent.ProvjeriTunel(t.Context(), kljucA, "gocop-test", adresa, kljucB.Public().(ed25519.PublicKey))
		if err != nil || rtt != 2*kasnjenjeTunela {
			t.Fatalf("provjera kroz tunel: %v, %v", rtt, err)
		}
		_, err = tu.klijent.ProvjeriTunel(t.Context(), kljucA, "gocop-test", adresa, newKey(t).Public().(ed25519.PublicKey))
		if !errors.Is(err, ErrDrugiKljuc) || !strings.Contains(err.Error(), "čvor na "+adresa+" dokazao je drugi ključ") {
			t.Errorf("drugi ključ kroz tunel: %v", err)
		}
		if _, err := tu.klijent.ProvjeriTunel(t.Context(), kljucA, "gocop-test", "https://", kljucB.Public().(ed25519.PublicKey)); err == nil {
			t.Error("adresa tunela bez domene")
		}
		if _, err := tu.klijent.ProvjeriTunel(t.Context(), kljucA, "gocop-test", "https://nepoznat.primjer.hr", kljucB.Public().(ed25519.PublicKey)); err == nil ||
			!strings.Contains(err.Error(), "tunel https://nepoznat.primjer.hr") {
			t.Errorf("tunel koji se ne javlja: %v", err)
		}
		synctest.Wait()
		if p, n := tu.pitan.Load(), tu.obrada.Load(); p != 0 || n != 0 {
			t.Errorf("B je pitan %d puta i obradio %d veza", p, n)
		}
	})
}

// tudjiPosluzitelj na loopbacku pokaže certifikat i rukuje se ključem koji
// zada test; uz zahtjev traži certifikat druge strane, kao čvor
func tudjiPosluzitelj(t *testing.T, cert tls.Certificate, zahtjev bool) string {
	t.Helper()
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	if zahtjev {
		cfg.ClientAuth = tls.RequireAnyClientCert
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.(*tls.Conn).Handshake() // ishod gleda klijent
			}(c)
		}
	}()
	return ln.Addr().String()
}

// Tko pokaže tuđi javni certifikat (B ga daje svakome tko se spoji), a
// rukuje se svojim ključem, nije prošao: ključ je dokazan tek potpisom
// rukovanja. Poslužitelj koji ne traži certifikat druge strane, a pravim
// ključem potpiše rukovanje, prolazi.
func TestProvjeraKljucaTraziPotpis(t *testing.T) {
	kljucB, klijent := newKey(t), newKey(t)
	javniB := kljucB.Public().(ed25519.PublicKey)
	certB, err := selfSignedCert(kljucB, "testproto", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()

	lazni := tls.Certificate{Certificate: certB.Certificate, PrivateKey: newKey(t)}
	adresa := tudjiPosluzitelj(t, lazni, true)
	if _, err := ProvjeriKljucOn(ctx, otvori(t, adresa), klijent, "testproto", adresa, javniB); err == nil || !strings.Contains(err.Error(), "invalid signature") {
		t.Errorf("tuđi certifikat bez ključa: %v", err)
	}
	adresa = tudjiPosluzitelj(t, certB, false)
	if rtt, err := ProvjeriKljucOn(ctx, otvori(t, adresa), klijent, "testproto", adresa, javniB); err != nil || rtt <= 0 {
		t.Errorf("bez zahtjeva za certifikat: %v, %v", rtt, err)
	}
}
