package razmjena

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/mrezatest"
)

// uMjehuru vodi test u mjehuru. Sat mjehura stane čim test i njegova
// čišćenja završe, pa zadnje čišćenje (čišćenje ide obrnutim redom) pusti
// sekundu probnog sata, da stigne zatvaranje koje još putuje mrežom.
//
// Stalna slučajnost (cryptotest.SetGlobalRandom) namjerno nije uključena:
// ona prepisuje crypto/rand.Reader, a gorutine koje drugi testovi ovog paketa
// ostave (TLS poslužitelji na loopbacku) taj isti Reader čitaju, pa provjera
// istodobnog pristupa javlja utrku (21. 10. 2026., otprilike jednom u 30
// ponavljanja). Ishod ovih testova ne ovisi o slučajnim brojevima.
func uMjehuru(t *testing.T, f func(t *testing.T)) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		t.Cleanup(func() { time.Sleep(time.Second) })
		f(t)
	})
}

// bezGreske prekida test na grešci pripreme
func bezGreske(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// razgovaraj pošalje granicu i vrati odgovor druge strane
func razgovaraj(c *Conn) (Envelope, error) {
	pitanje, err := NewEnvelope("frontier", map[string]int{"a": 1})
	if err != nil {
		return Envelope{}, err
	}
	if err := c.Send(pitanje); err != nil {
		return Envelope{}, err
	}
	return c.Receive()
}

// certifikatCvora je certifikat čvora kako ga TLS pročita s druge strane
func certifikatCvora(t *testing.T, k ed25519.PrivateKey, protokol string, vj []byte) *x509.Certificate {
	t.Helper()
	c, err := selfSignedCert(k, protokol, vj)
	bezGreske(t, err)
	x, err := x509.ParseCertificate(c.Certificate[0])
	bezGreske(t, err)
	return x
}

// Ključ druge strane dobiva se samo iz certifikata čvora iste mreže: bez
// certifikata nema ključa, certifikat koji nije Ed25519 javlja se kao i do
// sada (i kad mu je ime krivo), a ime druge mreže kaže koje je ime stiglo i
// koje se očekuje.
func TestDokazaniKljuc(t *testing.T) {
	if _, _, err := dokazaniKljuc(nil, "testproto"); err == nil || !strings.Contains(err.Error(), "no certificate") {
		t.Errorf("bez certifikata: %v", err)
	}
	ec, err := x509.ParseCertificate(ecCertifikat(t).Certificate[0])
	bezGreske(t, err)
	if _, _, err := dokazaniKljuc([]*x509.Certificate{ec}, "testproto"); err == nil || !strings.Contains(err.Error(), "not an Ed25519 key") {
		t.Errorf("certifikat EC s imenom „tudji”: %v", err)
	}
	k := newKey(t)
	ocekivano := "certifikat druge strane je za „drugi-sync”, a ovo računalo očekuje „testproto-sync” (postavka [sync] protokol)"
	if _, _, err := dokazaniKljuc([]*x509.Certificate{certifikatCvora(t, k, "drugi", nil)}, "testproto"); err == nil || err.Error() != ocekivano {
		t.Errorf("druga mreža: %v", err)
	}
	vj := []byte(`{"clanstvo":"izmišljeno"}`)
	kljuc, dobiveno, err := dokazaniKljuc([]*x509.Certificate{certifikatCvora(t, k, "testproto", vj)}, "testproto")
	if err != nil || !kljuc.Equal(k.Public()) || !bytes.Equal(dobiveno, vj) {
		t.Errorf("certifikat čvora: %v %s %v", kljuc, dobiveno, err)
	}
}

// Ime u certifikatu ide iz imena protokola, a oznake formata ne: oznaka
// vjerodajnica u certifikatu ista je i kad zasebna instanca (TESTNA
// SIMULACIJA) radi pod imenom „gocop-sim”, a ime razmjene „gocop” daje
// „gocop-sync”, koje šalju čvorovi 0.0.37–0.0.39.
func TestOznakeFormataNeOvisiOProtokolu(t *testing.T) {
	if prefiksVjerodajnica != "gocop:vjerodajnice:" {
		t.Errorf("oznaka vjerodajnica: %q", prefiksVjerodajnica)
	}
	if cn := cnRazmjene("gocop"); cn != "gocop-sync" {
		t.Errorf("ime certifikata mreže gocop: %q", cn)
	}
	vj := []byte(`{"clanstvo":"izmišljeno"}`)
	x := certifikatCvora(t, newKey(t), "gocop-sim", vj)
	if x.Subject.CommonName != "gocop-sim-sync" {
		t.Errorf("ime certifikata simulacije: %q", x.Subject.CommonName)
	}
	if len(x.URIs) != 1 || !strings.HasPrefix(x.URIs[0].String(), "urn:gocop:vjerodajnice:") {
		t.Errorf("vjerodajnice u certifikatu simulacije: %v", x.URIs)
	}
	if b := peerVjerodajnice([][]byte{x.Raw}); !bytes.Equal(b, vj) {
		t.Errorf("pročitane vjerodajnice: %s", b)
	}
}

// brojac broji pitanja o povjerenju i obrade na slušalici razmjene
type brojac struct{ pitan, obrada atomic.Int32 }

func (b *brojac) vjeruje(ed25519.PublicKey, []byte) bool {
	b.pitan.Add(1)
	return true
}

func (b *brojac) odgovori(c *Conn) {
	defer c.Close()
	b.obrada.Add(1)
	e, err := c.Receive()
	if err != nil {
		return
	}
	_ = c.Send(e) // je li odgovor stigao, provjerava pozivatelj
}

// Certifikat s imenom (CN) druge mreže odbija se na portu i kroz tunel, na
// strani koja zove i na strani koja prima: ni pitanje o članstvu ni obrada
// razmjene se ne dogode. Odbijeno rukovanje oslobodi svoje mjesto.
func TestKrivCNOdbijen(t *testing.T) {
	ocekivano := "certifikat druge strane je za „drugi-sync”, a ovo računalo očekuje „testproto-sync” (postavka [sync] protokol)"
	t.Run("port, pozivatelj", func(t *testing.T) {
		ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
		defer otkazi()
		posluzitelj := newKey(t)
		var b brojac
		adresa := slusalicaMreze(t, ctx, posluzitelj, "drugi", novaOgrada(4, 4, time.Second), b.vjeruje, b.odgovori)
		_, err := DialExchange(ctx, newKey(t), "testproto", nil, adresa, posluzitelj.Public().(ed25519.PublicKey))
		if err == nil || err.Error() != ocekivano {
			t.Fatalf("poslužitelj druge mreže: %v", err)
		}
		if p, o := b.pitan.Load(), b.obrada.Load(); p != 0 || o != 0 {
			t.Errorf("druga mreža je pitana %d puta, obradila %d veza", p, o)
		}
	})
	t.Run("port, primatelj", func(t *testing.T) {
		ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
		defer otkazi()
		posluzitelj := newKey(t)
		var b brojac
		// jedno mjesto za rukovanje: sljedeća veza prolazi samo ako ga
		// odbijena oslobodi
		adresa := slusalicaMreze(t, ctx, posluzitelj, "testproto", novaOgrada(1, 4, 5*time.Second), b.vjeruje, b.odgovori)
		cfg, err := tlsConfig(newKey(t), "drugi")
		bezGreske(t, err)
		c, err := tls.Dial("tcp", adresa, cfg)
		if err != nil {
			t.Fatalf("rukovanje s drugom mrežom: %v", err)
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second)) // istek bi bio greška testa niže
		if _, err := c.Read(make([]byte, 1)); err != io.EOF {
			t.Errorf("primatelj nije zatvorio vezu druge mreže: %v", err)
		}
		if p := b.pitan.Load(); p != 0 {
			t.Errorf("članstvo druge mreže pitano je %d puta", p)
		}
		pravi, err := DialExchange(ctx, newKey(t), "testproto", nil, adresa, posluzitelj.Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatalf("veza nakon odbijene: %v", err)
		}
		defer pravi.Close()
		if _, err := razgovaraj(pravi); err != nil {
			t.Errorf("razmjena nakon odbijene: %v", err)
		}
	})
	t.Run("tunel, pozivatelj", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "drugi", kljucA.Public().(ed25519.PublicKey))
			_, err := tu.klijent.DialTunel(t.Context(), kljucA, "testproto", nil, "https://cop.primjer.hr", kljucB.Public().(ed25519.PublicKey))
			if err == nil || err.Error() != "tunel https://cop.primjer.hr: "+ocekivano {
				t.Fatalf("čvor druge mreže kroz tunel: %v", err)
			}
			synctest.Wait()
			if p, o := tu.pitan.Load(), tu.obrada.Load(); p != 0 || o != 0 {
				t.Errorf("druga mreža je pitana %d puta, obradila %d veza", p, o)
			}
		})
	})
	t.Run("tunel, primatelj", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "testproto", kljucA.Public().(ed25519.PublicKey))
			nc, err := tu.klijent.otvoriWebSocket(t.Context(), "https://cop.primjer.hr", PutTunela)
			bezGreske(t, err)
			cfg, err := tlsConfig(kljucA, "drugi")
			bezGreske(t, err)
			c := tls.Client(nc, cfg)
			defer c.Close()
			if err := c.HandshakeContext(t.Context()); err != nil {
				t.Fatalf("rukovanje s drugom mrežom kroz tunel: %v", err)
			}
			if _, err := c.Read(make([]byte, 1)); err != io.EOF {
				t.Errorf("primatelj nije zatvorio vezu druge mreže: %v", err)
			}
			synctest.Wait()
			if p, o := tu.pitan.Load(), tu.obrada.Load(); p != 0 || o != 0 {
				t.Errorf("članstvo druge mreže pitano je %d puta, obrađeno %d veza", p, o)
			}
		})
	})
}

// RTT na portu razmjene je trajanje rukovanja na strani koja zove: točno
// jedan obilazak (dva kašnjenja). Strana koja prima nema RTT, a put veze
// postavlja tek pozivatelj paketa.
func TestRTTRazmjeneNaPortu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		const kasnjenje = 25 * time.Millisecond
		m := mrezatest.NovaMreza()
		m.Kasnjenje = kasnjenje
		s, err := m.Slusaj("192.0.2.7:4710")
		bezGreske(t, err)
		kljucA, kljucB := newKey(t), newKey(t)
		primljeno := make(chan *Conn, 1)
		ctx := t.Context()
		go func() {
			err := ServeExchangeOn(ctx, kljucB, "testproto", nil, s, func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
				primljeno <- c
				(&brojac{}).odgovori(c)
			}, NovaOgrada())
			if err != nil {
				t.Error(err)
			}
		}()
		raw, err := m.DialContextOd("192.0.2.8")(ctx, "tcp", "192.0.2.7:4710")
		bezGreske(t, err)
		c, err := DialExchangeOn(ctx, raw, kljucA, "testproto", nil, "192.0.2.7:4710", kljucB.Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if c.RTT != 2*kasnjenje || c.Put != "" {
			t.Errorf("pozivatelj: RTT %v (a ne %v), put %q", c.RTT, 2*kasnjenje, c.Put)
		}
		if _, err := razgovaraj(c); err != nil {
			t.Fatal(err)
		}
		if p := <-primljeno; p.RTT != 0 || p.Put != "" || p.RemoteAddr().String() != "192.0.2.8:49152" {
			t.Errorf("primatelj: RTT %v, put %q, s adrese %s", p.RTT, p.Put, p.RemoteAddr())
		}
	})
}

// Greška DialExchangeOn kad druga strana dokaže drugi ključ prepoznaje se kao
// ErrDrugiKljuc, a tekst je ostao isti (ljudi ga navode).
func TestDrugiKljucJeErrDrugiKljuc(t *testing.T) {
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	posluzitelj := newKey(t)
	var b brojac
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(4, 4, time.Second), b.vjeruje, b.odgovori)
	ocekivan := newKey(t).Public().(ed25519.PublicKey)
	_, err := DialExchange(ctx, newKey(t), "testproto", nil, adresa, ocekivan)
	if !errors.Is(err, ErrDrugiKljuc) {
		t.Fatalf("drugi ključ: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "the device at "+adresa+" proved a DIFFERENT key than the one paired — refusing to exchange. It presented ") ||
		!strings.HasSuffix(err.Error(), "…, the pairing recorded "+PublicKeyString(ocekivan)[:8]+"…. Either that machine was rebuilt (pair again), or another process holds its sync port") {
		t.Errorf("tekst greške: %v", err)
	}
	if errors.Is(errors.New("druga greška"), ErrDrugiKljuc) {
		t.Error("svaka greška je ErrDrugiKljuc")
	}
}
