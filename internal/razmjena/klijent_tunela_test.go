package razmjena

// Klijent tunela. Razmjena kroz tunel ide preko mreže u memoriji
// (mrezatest), u mjehuru (testing/synctest): sat je probni, pa se rokovi i RTT
// mjere točno, a test ne čeka stvarno vrijeme.

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/mrezatest"
)

// kasnjenjeTunela je jednosmjerno kašnjenje mreže u memoriji
const kasnjenjeTunela = 10 * time.Millisecond

// webCertifikat je certifikat web poslužitelja za imena, potpisan testnim
// CA-om, i korijeni sa samo tim CA-om. Nastaje u mjehuru, pa vrijedi oko
// probnog sata (1. 1. 2000.).
func webCertifikat(t *testing.T, imena ...string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	_, kljucCA, err := ed25519.GenerateKey(rand.Reader)
	bezGreske(t, err)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Testni CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	derCA, err := x509.CreateCertificate(rand.Reader, ca, ca, kljucCA.Public(), kljucCA)
	bezGreske(t, err)
	ca, err = x509.ParseCertificate(derCA)
	bezGreske(t, err)
	_, kljuc, err := ed25519.GenerateKey(rand.Reader)
	bezGreske(t, err)
	list := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: imena[0]}, DNSNames: imena,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, list, ca, kljuc.Public(), kljucCA)
	bezGreske(t, err)
	korijeni := x509.NewCertPool()
	korijeni.AddCert(ca)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: kljuc}, korijeni
}

// tunelUMemoriji je mreža u memoriji s web poslužiteljem tunela čvora B na
// https://cop.primjer.hr (kao Cloudflare ispred čvora) i klijent tunela koji
// vjeruje testnom CA-u. Sve nastaje u mjehuru i staje s testom.
type tunelUMemoriji struct {
	m       *mrezatest.Mreza
	cert    tls.Certificate
	klijent *KlijentTunela
	// pitan broji koliko je puta B pitao vjeruje li ključu, a obrada koliko
	// je veza došlo do obrade razmjene
	pitan, obrada atomic.Int32
}

// noviTunelUMemoriji pokreće B s ključem i imenom protokola; B vjeruje samo
// ključu poznat
func noviTunelUMemoriji(t *testing.T, kljucB ed25519.PrivateKey, protokolB string, poznat ed25519.PublicKey) *tunelUMemoriji {
	t.Helper()
	cert, korijeni := webCertifikat(t, "cop.primjer.hr", "sutljivi.primjer.hr", "prazan.primjer.hr")
	m := mrezatest.NovaMreza()
	m.Kasnjenje = kasnjenjeTunela
	// klijent ne zadaje ime poslužitelja (ServerName): zadaje ga sam iz
	// adrese, kao tls.Dialer
	tu := &tunelUMemoriji{m: m, cert: cert, klijent: &KlijentTunela{TLS: &tls.Config{RootCAs: korijeni}, Dial: m.DialContext}}
	tunel := NoviTunel()
	mux := http.NewServeMux()
	mux.Handle("GET "+PutTunela, tunel.Handler())
	tu.web(t, "cop.primjer.hr:443", mux)
	ctx := t.Context()
	vjeruje := func(k ed25519.PublicKey, _ []byte) bool {
		tu.pitan.Add(1)
		return k.Equal(poznat)
	}
	go func() {
		if err := ServeExchangeOn(ctx, kljucB, protokolB, nil, tunel, vjeruje, tu.odgovori, NovaOgradaTunela(nil)); err != nil {
			t.Error(err)
		}
	}()
	return tu
}

// web pokreće web poslužitelj s certifikatom na adresi mreže
func (tu *tunelUMemoriji) web(t *testing.T, adresa string, h http.Handler) {
	t.Helper()
	s, err := tu.m.Slusaj(adresa)
	bezGreske(t, err)
	srv := &http.Server{Handler: h, ErrorLog: log.New(io.Discard, "", 0)}
	t.Cleanup(func() { srv.Close() })
	go func() {
		if err := srv.Serve(tls.NewListener(s, &tls.Config{Certificates: []tls.Certificate{tu.cert}, MinVersion: tls.VersionTLS13})); !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	}()
}

// sutljivi je poslužitelj na adresi mreže koji završi vanjski TLS, a zatim
// samo čita i nikad ne odgovori. Na kraju testa čeka da pozivatelj zatvori
// svoje veze: zatvaranje putuje kašnjenje mreže, a sat mjehura stane čim test
// završi.
func (tu *tunelUMemoriji) sutljivi(t *testing.T, adresa string) {
	t.Helper()
	s, err := tu.m.Slusaj(adresa)
	bezGreske(t, err)
	ln := tls.NewListener(s, &tls.Config{Certificates: []tls.Certificate{tu.cert}, MinVersion: tls.VersionTLS13})
	var veze sync.WaitGroup
	t.Cleanup(func() {
		ln.Close()
		veze.Wait()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			veze.Go(func() {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c) // čita dok pozivatelj ne zatvori; ishod je na strani pozivatelja
			})
		}
	}()
}

// odgovori je obrada razmjene na B: primi omotnicu i javi koju je primio
func (tu *tunelUMemoriji) odgovori(c *Conn) {
	defer c.Close()
	tu.obrada.Add(1)
	e, err := c.Receive()
	if err != nil {
		return
	}
	odg, err := NewEnvelope("odgovor", map[string]string{"stiglo": e.Kind})
	if err != nil {
		return
	}
	_ = c.Send(odg) // je li odgovor stigao, provjerava pozivatelj
}

// Razmjena kroz tunel u memoriji: upareni ključ razmijeni omotnicu u svakom
// smjeru, a RTT je točno jedan obilazak (dva kašnjenja). Nepoznati ključ ne
// dođe do obrade, krivi očekivani ključ odbija pozivatelj rečenicom koju
// korisnik zna, poslužitelj koji ne odgovori na nadogradnju istekne s rokom,
// a web bez puta tunela i certifikat za drugo ime ne daju vezu.
func TestRazmjenaKrozTunelUMemoriji(t *testing.T) {
	const adresa = "https://cop.primjer.hr"
	t.Run("upareni ključ", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
			c, err := tu.klijent.DialTunel(t.Context(), kljucA, "gocop-test", nil, adresa, kljucB.Public().(ed25519.PublicKey))
			if err != nil {
				t.Fatalf("spajanje kroz tunel: %v", err)
			}
			defer c.Close()
			if c.RTT != 2*kasnjenjeTunela {
				t.Errorf("RTT kroz tunel je %v, a ne %v", c.RTT, 2*kasnjenjeTunela)
			}
			e, err := razgovaraj(c)
			if err != nil || e.Kind != "odgovor" || string(e.Payload) != `{"stiglo":"frontier"}` {
				t.Fatalf("odgovor kroz tunel: %+v %v", e, err)
			}
			if n := tu.obrada.Load(); n != 1 {
				t.Errorf("obrađeno je %d veza", n)
			}
		})
	})
	t.Run("nepoznat ključ", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
			// rukovanje pozivatelja završi prije nego što B vidi ključ, pa
			// stranac odbijanje vidi tek kao zatvorenu vezu
			c, err := tu.klijent.DialTunel(t.Context(), newKey(t), "gocop-test", nil, adresa, kljucB.Public().(ed25519.PublicKey))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if _, err := razgovaraj(c); err == nil {
				t.Error("nepoznati ključ je dobio odgovor kroz tunel")
			}
			synctest.Wait()
			if p, o := tu.pitan.Load(), tu.obrada.Load(); p != 1 || o != 0 {
				t.Errorf("B je pitan %d puta, obradio %d veza", p, o)
			}
		})
	})
	t.Run("drugi očekivani ključ", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
			_, err := tu.klijent.DialTunel(t.Context(), kljucA, "gocop-test", nil, adresa, newKey(t).Public().(ed25519.PublicKey))
			if !errors.Is(err, ErrDrugiKljuc) {
				t.Fatalf("kriv ključ druge strane: %v", err)
			}
			if ocekivano := "čvor na https://cop.primjer.hr dokazao je drugi ključ od uparenog — razmjena odbijena"; err.Error() != ocekivano {
				t.Errorf("poruka %q, a ne %q", err, ocekivano)
			}
		})
	})
	t.Run("poslužitelj ne odgovara na nadogradnju", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
			tu.sutljivi(t, "sutljivi.primjer.hr:443")
			ctx, otkazi := context.WithTimeout(t.Context(), 20*time.Second)
			defer otkazi()
			pocetak := time.Now()
			_, err := tu.klijent.DialTunel(ctx, kljucA, "gocop-test", nil, "https://sutljivi.primjer.hr", kljucB.Public().(ed25519.PublicKey))
			if !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "tunel https://sutljivi.primjer.hr: ") {
				t.Fatalf("nadogradnja bez odgovora: %v", err)
			}
			if d := time.Since(pocetak); d != 20*time.Second {
				t.Errorf("poziv je odustao nakon %v, a ne nakon roka od 20 s", d)
			}
		})
	})
	t.Run("web bez puta tunela", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
			tu.web(t, "prazan.primjer.hr:443", http.NewServeMux())
			_, err := tu.klijent.DialTunel(t.Context(), kljucA, "gocop-test", nil, "https://prazan.primjer.hr", kljucB.Public().(ed25519.PublicKey))
			if err == nil || !strings.HasPrefix(err.Error(), "tunel https://prazan.primjer.hr: ") || !strings.Contains(err.Error(), "bad status") {
				t.Fatalf("web bez puta tunela: %v", err)
			}
		})
	})
	t.Run("certifikat za drugo ime", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
			tu.sutljivi(t, "krivo.primjer.hr:443")
			_, err := tu.klijent.DialTunel(t.Context(), kljucA, "gocop-test", nil, "https://krivo.primjer.hr", kljucB.Public().(ed25519.PublicKey))
			var ime x509.HostnameError
			if !errors.As(err, &ime) || !strings.HasPrefix(err.Error(), "tunel https://krivo.primjer.hr: ") {
				t.Fatalf("certifikat za drugo ime: %v", err)
			}
		})
	})
}

// Nadogradnja na WebSocket kroz tunel ista je kao do 0.0.39 (provjereno
// usporedbom sa starim DialTunel 21. 10. 2026.): Host i Origin bez porta 443,
// koji ide samo u adresu za spajanje, verzija 13, bez Sec-WebSocket-Protocol.
// Prvi okvir klijenta je binaran (0x82), jer posrednik po RFC 6455 smije
// zatvoriti tekstni okvir koji nije UTF-8. Ime za vanjski TLS (SNI) je
// domena tunela.
func TestNadogradnjaTunelaKaoDoSada(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		cert, korijeni := webCertifikat(t, "cop.primjer.hr")
		m := mrezatest.NovaMreza()
		s, err := m.Slusaj("cop.primjer.hr:443")
		bezGreske(t, err)
		sni := make(chan string, 1)
		ln := tls.NewListener(s, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13,
			GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
				sni <- h.ServerName
				return nil, nil
			}})
		defer ln.Close()
		snimka := make(chan string, 1)
		go snimiNadogradnju(ln, snimka)

		k := &KlijentTunela{TLS: &tls.Config{RootCAs: korijeni}, Dial: m.DialContext}
		kljuc := newKey(t)
		if _, err := k.DialTunel(t.Context(), kljuc, "gocop-test", nil, "https://cop.primjer.hr", kljuc.Public().(ed25519.PublicKey)); err == nil || !strings.HasPrefix(err.Error(), "tunel https://cop.primjer.hr: ") {
			t.Errorf("poslužitelj je zatvorio vezu nakon prvog okvira: %v", err)
		}
		if ime := <-sni; ime != "cop.primjer.hr" {
			t.Errorf("SNI vanjskog TLS-a: %q", ime)
		}
		ocekivano := "GET /razmjena/tunel HTTP/1.1\r\n" +
			"Host: cop.primjer.hr\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Key: <16 bajtova>\r\n" +
			"Origin: https://cop.primjer.hr\r\n" +
			"Sec-WebSocket-Version: 13\r\n" +
			"\r\n" +
			"prvi okvir: 0x82, maskiran\n"
		if dobiveno := <-snimka; dobiveno != ocekivano {
			t.Errorf("nadogradnja na WebSocket:\n%s\nočekivano:\n%s", dobiveno, ocekivano)
		}
	})
}

// snimiNadogradnju primi jednu vezu, zapiše zahtjev za nadogradnju (ključ
// samo kao duljinu, jer je slučajan), odgovori kao WebSocket poslužitelj i
// zapiše zaglavlje prvog okvira klijenta
func snimiNadogradnju(ln net.Listener, snimka chan<- string) {
	c, err := ln.Accept()
	if err != nil {
		snimka <- err.Error()
		return
	}
	defer c.Close()
	br := bufio.NewReader(c)
	var sb strings.Builder
	kljuc := ""
	for {
		redak, err := br.ReadString('\n')
		if err != nil {
			snimka <- sb.String() + err.Error()
			return
		}
		if k, ok := strings.CutPrefix(redak, "Sec-WebSocket-Key: "); ok {
			kljuc = strings.TrimSpace(k)
			b, err := base64.StdEncoding.DecodeString(kljuc)
			redak = fmt.Sprintf("Sec-WebSocket-Key: <%d bajtova>\r\n", len(b))
			if err != nil {
				redak = "Sec-WebSocket-Key: " + err.Error() + "\r\n"
			}
		}
		sb.WriteString(redak)
		if redak == "\r\n" {
			break
		}
	}
	potvrda := sha1.Sum([]byte(kljuc + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if _, err := fmt.Fprintf(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(potvrda[:])); err != nil {
		snimka <- sb.String() + err.Error()
		return
	}
	zaglavlje := make([]byte, 2)
	if _, err := io.ReadFull(br, zaglavlje); err != nil {
		snimka <- sb.String() + err.Error()
		return
	}
	maska := "maskiran"
	if zaglavlje[1]&0x80 == 0 {
		maska = "nemaskiran"
	}
	snimka <- sb.String() + fmt.Sprintf("prvi okvir: %#x, %s\n", zaglavlje[0], maska)
}

// Adresa web poslužitelja tunela dobiva port 443 samo kad ga nema, a ime za
// provjeru certifikata je bez porta i uglatih zagrada.
func TestVanjskaAdresa(t *testing.T) {
	for _, s := range []struct{ baza, adresa, ime string }{
		{"https://cop.primjer.hr", "cop.primjer.hr:443", "cop.primjer.hr"},
		{"https://cop.primjer.hr:8443", "cop.primjer.hr:8443", "cop.primjer.hr"},
		{"https://[::1]:8443", "[::1]:8443", "::1"},
		{"https://[::1]", "[::1]:443", "::1"},
	} {
		if adresa, ime := vanjskaAdresa(s.baza); adresa != s.adresa || ime != s.ime {
			t.Errorf("%s → %q %q, očekivano %q %q", s.baza, adresa, ime, s.adresa, s.ime)
		}
	}
}

// Klijent tunela bez postavke vjeruje sustavnim certifikatima, a ime
// poslužitelja uzima iz adrese kad ga postavka nema, kao tls.Dialer. Zadana
// postavka se pri tome ne mijenja.
func TestVanjskiTLSKlijentaTunela(t *testing.T) {
	if cfg := (&KlijentTunela{}).vanjskiTLS("cop.primjer.hr"); cfg.ServerName != "cop.primjer.hr" || cfg.RootCAs != nil || cfg.InsecureSkipVerify {
		t.Errorf("zadani vanjski TLS: ime %q, korijeni %v, bez provjere %v", cfg.ServerName, cfg.RootCAs, cfg.InsecureSkipVerify)
	}
	korijeni := x509.NewCertPool()
	zadan := &tls.Config{RootCAs: korijeni}
	if cfg := (&KlijentTunela{TLS: zadan}).vanjskiTLS("cop.primjer.hr"); cfg == zadan || cfg.ServerName != "cop.primjer.hr" || cfg.RootCAs != korijeni || zadan.ServerName != "" {
		t.Errorf("vanjski TLS iz postavke: ime %q, zadano ime %q", cfg.ServerName, zadan.ServerName)
	}
	sImenom := &tls.Config{ServerName: "drugi.primjer.hr"}
	if cfg := (&KlijentTunela{TLS: sImenom}).vanjskiTLS("cop.primjer.hr"); cfg.ServerName != "drugi.primjer.hr" {
		t.Errorf("ime iz postavke: %q", cfg.ServerName)
	}
}
