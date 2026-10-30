package razmjena

// Vrata razmjene javljaju ogradi zašto nisu pustila vezu (G7): rukovanje,
// certifikat koji nije certifikat čvora, drugo ime protokola, čvor koji nije
// član i puna mjesta za razmjenu. Na loopbacku, u stvarnom vremenu.

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"slices"
	"sync"
	"testing"
	"time"
)

// zapisVrata pamti što je ograda javila
type zapisVrata struct {
	mu       sync.Mutex
	razlozi  []RazlogVrata
	kljucevi []ed25519.PublicKey
}

func (z *zapisVrata) biljezi(r RazlogVrata, k ed25519.PublicKey) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.razlozi, z.kljucevi = append(z.razlozi, r), append(z.kljucevi, k)
}

// cekaj čeka da ograda javi n razloga, najviše 5 s, i vrati zadnji s ključem
func (z *zapisVrata) cekaj(t *testing.T, n int) (RazlogVrata, ed25519.PublicKey) {
	t.Helper()
	for kraj := time.Now().Add(5 * time.Second); time.Now().Before(kraj); time.Sleep(10 * time.Millisecond) {
		z.mu.Lock()
		if len(z.razlozi) >= n {
			r, k := z.razlozi[n-1], z.kljucevi[n-1]
			z.mu.Unlock()
			return r, k
		}
		z.mu.Unlock()
	}
	t.Fatalf("ograda nije javila %d. odbijanje", n)
	return "", nil
}

// tlsSaCertifikatom se rukuje s vratima pokazujući zadani certifikat, bez
// provjere druge strane, i čeka da vrata vezu zatvore
func tlsSaCertifikatom(t *testing.T, adresa string, cert tls.Certificate) {
	t.Helper()
	c, err := tls.Dial("tcp", adresa, &tls.Config{Certificates: []tls.Certificate{cert}, InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second)) // vrata vezu zatvore; rok samo da test ne stoji
	_, _ = c.Read(make([]byte, 1))                         // čeka zatvaranje, sadržaj nije bitan
}

func certifikatECDSA(t *testing.T) tls.Certificate {
	t.Helper()
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	bezGreske(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "testproto-sync"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &ec.PublicKey, ec)
	bezGreske(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: ec}
}

func TestVrataJavljajuZastoNisuPustila(t *testing.T) {
	posluzitelj, klijent := newKey(t), newKey(t)
	javni := posluzitelj.Public().(ed25519.PublicKey)
	ctx, otkazi := context.WithTimeout(context.Background(), 30*time.Second)
	defer otkazi()
	var z zapisVrata
	o := novaOgrada(4, 0, time.Second) // bez ijednog mjesta za razmjenu
	o.KadOdbije(z.biljezi)
	adresa := slusalica(t, ctx, posluzitelj, o, func(k ed25519.PublicKey, _ []byte) bool {
		return k.Equal(klijent.Public())
	}, func(c *Conn) { c.Close() })

	// rukovanje koje ne počne
	raw, err := net.Dial("tcp", adresa)
	bezGreske(t, err)
	raw.Close()
	if r, k := z.cekaj(t, 1); r != VrataRukovanje || k != nil {
		t.Errorf("prekinuto rukovanje: %s %x", r, k)
	}

	// certifikat koji nije certifikat čvora, i čvor pod drugim imenom protokola
	tlsSaCertifikatom(t, adresa, certifikatECDSA(t))
	if r, _ := z.cekaj(t, 2); r != VrataNijeCvor {
		t.Errorf("certifikat ECDSA: %s", r)
	}
	drugi, err := selfSignedCert(newKey(t), "drugiproto")
	bezGreske(t, err)
	tlsSaCertifikatom(t, adresa, drugi)
	if r, _ := z.cekaj(t, 3); r != VrataDrugiProtokol {
		t.Errorf("drugo ime protokola: %s", r)
	}

	// čvor koji nije član, pa član kad su sva mjesta za razmjenu zauzeta
	stranac := newKey(t)
	if c, err := DialExchangeOn(ctx, otvori(t, adresa), stranac, "testproto", nil, adresa, javni); err == nil {
		_, _ = razgovaraj(c) // vrata zatvore vezu
	}
	if r, k := z.cekaj(t, 4); r != VrataNijeClan || !k.Equal(stranac.Public()) {
		t.Errorf("nije član: %s %x", r, k)
	}
	c, err := DialExchangeOn(ctx, otvori(t, adresa), klijent, "testproto", nil, adresa, javni)
	bezGreske(t, err)
	if e, err := razgovaraj(c); err != nil || e.Kod != KodZauzet {
		t.Errorf("odbijenica pune ograde: %+v %v", e, err)
	}
	if r, k := z.cekaj(t, 5); r != VrataZauzeto || !k.Equal(klijent.Public()) {
		t.Errorf("zauzeto: %s %x", r, k)
	}
}

// Oznake druge strane koje ovaj program ne zna, poredane, bez neispravnih.
// web/1 program zna od F3b, iako ga javlja samo računalo s WEB syncom.
func TestNepoznateOznake(t *testing.T) {
	got := NepoznateOznake([]string{OznakaRazmjena, OznakaRupe, "web/1", "web/2", "stranice/2", "losa oznaka", "razmjena/2"})
	if want := []string{"stranice/2"}; !slices.Equal(got, want) {
		t.Errorf("nepoznate oznake %v, želim %v", got, want)
	}
	if NepoznateOznake(NaseMogucnosti()) != nil {
		t.Error("vlastite oznake su nepoznate")
	}
	var mnogo []string
	for i := range 1000 {
		mnogo = append(mnogo, fmt.Sprintf("oznaka-%04d/1", i))
	}
	got = NepoznateOznake(append(mnogo, OznakaRazmjena))
	if len(got) != najviseOznaka-1 || !slices.IsSorted(got) || got[0] != "oznaka-0000/1" {
		t.Errorf("od 1000 nepoznatih javljeno %d: %v…", len(got), got[:min(3, len(got))])
	}
}
