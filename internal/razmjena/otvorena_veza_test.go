package razmjena

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// pokvarenKljuc je ključ čiji javni dio ne pripada privatnom: certifikat
// se njime ne da potpisati
func pokvarenKljuc(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	k := append(ed25519.PrivateKey{}, newKey(t)...)
	for i := ed25519.SeedSize; i < len(k); i++ {
		k[i] ^= 0xff
	}
	return k
}

// pracenaVeza javlja je li zatvorena
type pracenaVeza struct {
	net.Conn
	zatvorena bool
}

func (v *pracenaVeza) Close() error {
	v.zatvorena = true
	return v.Conn.Close()
}

func otvori(t *testing.T, adresa string) *pracenaVeza {
	t.Helper()
	c, err := net.Dial("tcp", adresa)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &pracenaVeza{Conn: c}
}

// Razmjena preko veze koju je otvorio netko drugi radi kao DialExchange, a
// vidi adrese te veze. Pokvaren ključ ili drugi ključ druge strane vezu
// zatvore.
func TestRazmjenaPrekoOtvoreneVeze(t *testing.T) {
	posluzitelj, klijent := newKey(t), newKey(t)
	javni := posluzitelj.Public().(ed25519.PublicKey)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(4, 4, time.Second), func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
		defer c.Close()
		e, _ := c.Receive()
		_ = c.Send(e)
	})

	v := otvori(t, adresa)
	c, err := DialExchangeOn(ctx, v, klijent, "testproto", nil, adresa, javni)
	if err != nil {
		t.Fatal(err)
	}
	if c.LocalAddr().String() != v.LocalAddr().String() || c.RemoteAddr().String() != adresa {
		t.Errorf("adrese razgovora %s → %s", c.LocalAddr(), c.RemoteAddr())
	}
	poruka, _ := NewEnvelope("frontier", map[string]string{"a": "1"})
	if err := c.Send(poruka); err != nil {
		t.Fatal(err)
	}
	if e, err := c.Receive(); err != nil || e.Kind != "frontier" {
		t.Errorf("odgovor: %+v %v", e, err)
	}
	c.Close()

	v = otvori(t, adresa)
	if _, err := DialExchangeOn(ctx, v, pokvarenKljuc(t), "testproto", nil, adresa, javni); err == nil || !v.zatvorena {
		t.Errorf("pokvaren ključ: %v, veza zatvorena %v", err, v.zatvorena)
	}
	v = otvori(t, adresa)
	if _, err := DialExchangeOn(ctx, v, klijent, "testproto", nil, adresa, newKey(t).Public().(ed25519.PublicKey)); err == nil || !strings.Contains(err.Error(), "DIFFERENT key") {
		t.Errorf("drugi ključ: %v", err)
	}
	otkazan, odmah := context.WithCancel(ctx)
	odmah()
	v = otvori(t, adresa)
	if _, err := DialExchangeOn(otkazan, v, klijent, "testproto", nil, adresa, javni); !errors.Is(err, context.Canceled) || !v.zatvorena {
		t.Errorf("otkazano rukovanje: %v, veza zatvorena %v", err, v.zatvorena)
	}
}

// Druga strana s certifikatom koji nije Ed25519 nije čvor
func TestRazmjenaSCertifikatomKojiNijeCvor(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "tudji-posluzitelj"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &ec.PublicKey, ec)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: ec}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = c.(*tls.Conn).Handshake()
			defer c.Close()
			_, _ = c.Read(make([]byte, 1))
		}
	}()
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	if _, err := DialExchange(ctx, newKey(t), "testproto", nil, ln.Addr().String(), newKey(t).Public().(ed25519.PublicKey)); err == nil {
		t.Error("razmjena s posluziteljem bez ključa čvora")
	}
}

// Uparivanje preko otvorene veze s pokvarenim ključem ne krene i zatvori vezu
func TestUparivanjePrekoOtvoreneVezePokvarenimKljucem(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	v := otvori(t, ln.Addr().String())
	if _, err := DialOn(v, pokvarenKljuc(t), Identity{Protocol: "testproto", DeviceID: "laptop-baranja"}); err == nil || !v.zatvorena {
		t.Errorf("pokvaren ključ: %v, veza zatvorena %v", err, v.zatvorena)
	}
}

// Strana koja čeka uparivanje nad vezom koju je primila tuđa slušalica
// (AcceptOn) dogovori isti kod i dozna isti ključ kao uz Listen. Pokvaren
// ključ zatvori vezu, a veza koja se zatvori bez TLS rukovanja nije čvor.
func TestUparivanjeNadPrimljenomVezom(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	primljena := func() *pracenaVeza {
		c, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return &pracenaVeza{Conn: c}
	}
	a, b := newKey(t), newKey(t)
	idA := Identity{Protocol: "testproto", DeviceID: "A"}
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()

	nazvao := make(chan *PairResult, 1)
	go func() {
		res, err := Dial(ctx, b, Identity{Protocol: "testproto", DeviceID: "B"}, ln.Addr().String())
		if err != nil {
			t.Errorf("nazivanje: %v", err)
		}
		nazvao <- res
	}()
	res, err := AcceptOn(primljena(), a, idA)
	if err != nil {
		t.Fatal(err)
	}
	d := <-nazvao
	if d == nil || d.SAS != res.SAS {
		t.Fatalf("dva ekrana s različitim kodom: %+v %+v", res, d)
	}
	if !res.PeerKey.Equal(b.Public().(ed25519.PublicKey)) || !d.PeerKey.Equal(a.Public().(ed25519.PublicKey)) || res.Peer.DeviceID != "B" {
		t.Errorf("ključ ili ime druge strane: %+v", res.Peer)
	}
	res.conn.Close()
	d.conn.Close()

	otvori(t, ln.Addr().String())
	p := primljena()
	if _, err := AcceptOn(p, pokvarenKljuc(t), idA); err == nil || !p.zatvorena {
		t.Errorf("pokvaren ključ: %v, veza zatvorena %v", err, p.zatvorena)
	}
	otvori(t, ln.Addr().String()).Close()
	if _, err := AcceptOn(primljena(), a, idA); !errors.Is(err, ErrNotAPeer) {
		t.Errorf("veza bez rukovanja: %v", err)
	}
}
