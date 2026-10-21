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
// ona bez brave prepisuje crypto/rand.Reader, a crypto/tls ga čita pri
// svakom zapisu. TLS gorutine koje testovi u stvarnom vremenu ostave (na
// loopbacku, nitko ih ne čeka) zato daju utrku, i kad se postavi izvan
// mjehura (provjereno 21. 10. 2026.). Ishod ovih testova ne ovisi o
// slučajnim brojevima.
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

// Ključ druge strane dobiva se samo iz certifikata čvora s istim imenom
// protokola: bez certifikata nema ključa, certifikat koji nije Ed25519 javlja
// se kao i do sada (i kad mu je ime krivo), a drugo ime kaže koje je ime
// stiglo i koje se očekuje.
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
	ocekivano := "certifikat druge strane je za „drugi-sync”, a ovo računalo očekuje „testproto-sync”"
	if _, _, err := dokazaniKljuc([]*x509.Certificate{certifikatCvora(t, k, "drugi", nil)}, "testproto"); err == nil || err.Error() != ocekivano {
		t.Errorf("drugo ime protokola: %v", err)
	}
	podmetnuto := strings.Repeat("x", 100) + "\nlažni redak"
	ocekivano = "certifikat druge strane je za „" + strings.Repeat("x", 64) + "…”, a ovo računalo očekuje „testproto-sync”"
	if _, _, err := dokazaniKljuc([]*x509.Certificate{certifikatCvora(t, k, podmetnuto, nil)}, "testproto"); err == nil || err.Error() != ocekivano {
		t.Errorf("dugo ime s novim redom: %v", err)
	}
	vj := []byte(`{"clanstvo":"izmišljeno"}`)
	kljuc, dobiveno, err := dokazaniKljuc([]*x509.Certificate{certifikatCvora(t, k, "testproto", vj)}, "testproto")
	if err != nil || !kljuc.Equal(k.Public()) || !bytes.Equal(dobiveno, vj) {
		t.Errorf("certifikat čvora: %v %s %v", kljuc, dobiveno, err)
	}
}

// Ime iz tuđeg certifikata ide u poruku skraćeno i bez kontrolnih znakova
func TestImeZaPoruku(t *testing.T) {
	for _, s := range []struct{ ime, poruka string }{
		{"gocop-sync", "gocop-sync"},
		{"", ""},
		{"a\nb\x00c\td", "a?b?c?d"},
		{"čvor-šđž", "čvor-šđž"},
		{strings.Repeat("ž", 64), strings.Repeat("ž", 64)},
		{strings.Repeat("ž", 65), strings.Repeat("ž", 64) + "…"},
	} {
		if p := imeZaPoruku(s.ime); p != s.poruka {
			t.Errorf("%q → %q, očekivano %q", s.ime, p, s.poruka)
		}
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

// adresaPorta je adresa porta razmjene na mreži u memoriji
const adresaPorta = "192.0.2.7:4710"

// slusalicaUMemoriji poslužuje razmjenu čvora s ključem i imenom protokola na
// adresaPorta mreže u memoriji, dok test traje
func slusalicaUMemoriji(t *testing.T, m *mrezatest.Mreza, kljuc ed25519.PrivateKey, protokol string, o *Ograda, trusted KeyChecker, handle func(*Conn)) {
	t.Helper()
	s, err := m.Slusaj(adresaPorta)
	bezGreske(t, err)
	ctx := t.Context()
	go func() {
		if err := ServeExchangeOn(ctx, kljuc, protokol, nil, s, trusted, handle, o); err != nil {
			t.Error(err)
		}
	}()
}

// Certifikat s drugim imenom protokola (CN) odbija se na portu i kroz tunel,
// na strani koja zove i na strani koja prima: ni pitanje o članstvu ni obrada
// razmjene se ne dogode. Odbijeno rukovanje oslobodi svoje mjesto.
func TestKrivCNOdbijen(t *testing.T) {
	ocekivano := "certifikat druge strane je za „drugi-sync”, a ovo računalo očekuje „testproto-sync”"
	t.Run("port, pozivatelj", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			m := mrezatest.NovaMreza()
			posluzitelj := newKey(t)
			var b brojac
			slusalicaUMemoriji(t, m, posluzitelj, "drugi", NovaOgrada(), b.vjeruje, b.odgovori)
			raw, err := m.DialContext(t.Context(), "tcp", adresaPorta)
			bezGreske(t, err)
			_, err = DialExchangeOn(t.Context(), raw, newKey(t), "testproto", nil, adresaPorta, posluzitelj.Public().(ed25519.PublicKey))
			if err == nil || err.Error() != ocekivano {
				t.Fatalf("poslužitelj s drugim imenom protokola: %v", err)
			}
			// poslužitelj je dovršio svoje rukovanje i provjeru imena
			synctest.Wait()
			if p, o := b.pitan.Load(), b.obrada.Load(); p != 0 || o != 0 {
				t.Errorf("poslužitelj s drugim imenom pitan je %d puta, obradio %d veza", p, o)
			}
		})
	})
	t.Run("port, primatelj", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			ctx, otkazi := context.WithTimeout(t.Context(), time.Minute)
			defer otkazi()
			m := mrezatest.NovaMreza()
			posluzitelj := newKey(t)
			var b brojac
			// jedno mjesto za rukovanje: sljedeća veza prolazi samo ako ga
			// odbijena oslobodi
			slusalicaUMemoriji(t, m, posluzitelj, "testproto", novaOgrada(1, 4, 5*time.Second), b.vjeruje, b.odgovori)
			raw, err := m.DialContext(ctx, "tcp", adresaPorta)
			bezGreske(t, err)
			cfg, err := tlsConfig(newKey(t), "drugi")
			bezGreske(t, err)
			c := tls.Client(raw, cfg)
			defer c.Close()
			if err := c.HandshakeContext(ctx); err != nil {
				t.Fatalf("rukovanje s drugim imenom protokola: %v", err)
			}
			if _, err := c.Read(make([]byte, 1)); err != io.EOF {
				t.Errorf("primatelj nije zatvorio vezu s drugim imenom protokola: %v", err)
			}
			synctest.Wait()
			if p := b.pitan.Load(); p != 0 {
				t.Errorf("članstvo uz drugo ime protokola pitano je %d puta", p)
			}
			raw, err = m.DialContext(ctx, "tcp", adresaPorta)
			bezGreske(t, err)
			pravi, err := DialExchangeOn(ctx, raw, newKey(t), "testproto", nil, adresaPorta, posluzitelj.Public().(ed25519.PublicKey))
			if err != nil {
				t.Fatalf("veza nakon odbijene: %v", err)
			}
			defer pravi.Close()
			if _, err := razgovaraj(pravi); err != nil {
				t.Errorf("razmjena nakon odbijene: %v", err)
			}
		})
	})
	t.Run("tunel, pozivatelj", func(t *testing.T) {
		uMjehuru(t, func(t *testing.T) {
			kljucA, kljucB := newKey(t), newKey(t)
			tu := noviTunelUMemoriji(t, kljucB, "drugi", kljucA.Public().(ed25519.PublicKey))
			_, err := tu.klijent.DialTunel(t.Context(), kljucA, "testproto", nil, "https://cop.primjer.hr", kljucB.Public().(ed25519.PublicKey))
			if err == nil || err.Error() != "tunel https://cop.primjer.hr: "+ocekivano {
				t.Fatalf("čvor s drugim imenom protokola kroz tunel: %v", err)
			}
			synctest.Wait()
			if p, o := tu.pitan.Load(), tu.obrada.Load(); p != 0 || o != 0 {
				t.Errorf("čvor s drugim imenom pitan je %d puta, obradio %d veza", p, o)
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
				t.Fatalf("rukovanje s drugim imenom protokola kroz tunel: %v", err)
			}
			if _, err := c.Read(make([]byte, 1)); err != io.EOF {
				t.Errorf("primatelj nije zatvorio vezu s drugim imenom protokola: %v", err)
			}
			synctest.Wait()
			if p, o := tu.pitan.Load(), tu.obrada.Load(); p != 0 || o != 0 {
				t.Errorf("članstvo uz drugo ime protokola pitano je %d puta, obrađeno %d veza", p, o)
			}
		})
	})
}

// RTT na portu razmjene je trajanje rukovanja na strani koja zove: točno
// jedan obilazak (dva kašnjenja). Strana koja prima nema RTT, a put veze
// postavlja tek peers.
func TestRTTRazmjeneNaPortu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		const kasnjenje = 25 * time.Millisecond
		m := mrezatest.NovaMreza()
		m.Kasnjenje = kasnjenje
		kljucA, kljucB := newKey(t), newKey(t)
		primljeno := make(chan *Conn, 1)
		var b brojac
		slusalicaUMemoriji(t, m, kljucB, "testproto", NovaOgrada(), b.vjeruje, func(c *Conn) {
			primljeno <- c
			b.odgovori(c)
		})
		ctx := t.Context()
		raw, err := m.DialContextOd("192.0.2.8")(ctx, "tcp", adresaPorta)
		bezGreske(t, err)
		c, err := DialExchangeOn(ctx, raw, kljucA, "testproto", nil, adresaPorta, kljucB.Public().(ed25519.PublicKey))
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
// ErrDrugiKljuc, i samo kao ona, a tekst je ostao isti (ljudi ga navode).
func TestDrugiKljucJeErrDrugiKljuc(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := mrezatest.NovaMreza()
		var b brojac
		slusalicaUMemoriji(t, m, newKey(t), "testproto", NovaOgrada(), b.vjeruje, b.odgovori)
		raw, err := m.DialContext(t.Context(), "tcp", adresaPorta)
		bezGreske(t, err)
		ocekivan := newKey(t).Public().(ed25519.PublicKey)
		_, err = DialExchangeOn(t.Context(), raw, newKey(t), "testproto", nil, adresaPorta, ocekivan)
		if !errors.Is(err, ErrDrugiKljuc) {
			t.Fatalf("drugi ključ: %v", err)
		}
		if !strings.HasPrefix(err.Error(), "the device at "+adresaPorta+" proved a DIFFERENT key than the one paired — refusing to exchange. It presented ") ||
			!strings.HasSuffix(err.Error(), "…, the pairing recorded "+PublicKeyString(ocekivan)[:8]+"…. Either that machine was rebuilt (pair again), or another process holds its sync port") {
			t.Errorf("tekst greške: %v", err)
		}
		if errors.Is(err, ErrPorukaPrevelika) {
			t.Error("greška drugog ključa prepoznaje se i kao prevelika poruka")
		}
	})
}

// Pozivatelj provjeri ključ i ime protokola druge strane usred rukovanja,
// prije nego što pošalje svoj certifikat, u kojem je potvrda članstva. Strana
// s drugim ključem ili drugim imenom protokola certifikat ne dobije, a greška
// je ista kao kad se provjeravalo nakon rukovanja. Očekivana strana ga dobije.
func TestPozivateljNeSaljeCertifikatKrivojStrani(t *testing.T) {
	ocekivan := newKey(t)
	for _, sl := range []struct {
		ime      string
		kljuc    ed25519.PrivateKey
		protokol string
		greska   string // početak greške; prazno: veza prolazi
	}{
		{"drugi ključ", newKey(t), "testproto", "the device at " + adresaPorta + " proved a DIFFERENT key than the one paired — refusing to exchange. It presented "},
		{"drugo ime protokola", ocekivan, "drugi", "certifikat druge strane je za „drugi-sync”, a ovo računalo očekuje „testproto-sync”"},
		{"očekivana strana", ocekivan, "testproto", ""},
	} {
		t.Run(sl.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				klijent, posluzitelj := mrezatest.Par(mrezatest.TCP("192.0.2.8:49152"), mrezatest.TCP(adresaPorta))
				cfg, err := tlsConfig(sl.kljuc, sl.protokol)
				bezGreske(t, err)
				var certifikata atomic.Int32
				cfg.VerifyPeerCertificate = func(certs [][]byte, _ [][]*x509.Certificate) error {
					certifikata.Add(int32(len(certs)))
					return nil
				}
				rukovanje := make(chan error, 1)
				go func() {
					s := tls.Server(posluzitelj, cfg)
					defer s.Close()
					rukovanje <- s.HandshakeContext(t.Context())
				}()
				vj := func() []byte { return []byte(`{"clanstvo":"izmišljeno"}`) }
				c, err := DialExchangeOn(t.Context(), klijent, newKey(t), "testproto", vj, adresaPorta, ocekivan.Public().(ed25519.PublicKey))
				errPosluzitelja := <-rukovanje
				if sl.greska == "" {
					if err != nil || errPosluzitelja != nil || certifikata.Load() != 1 || !c.PeerKey.Equal(ocekivan.Public()) {
						t.Fatalf("očekivana strana: %v, poslužitelj %v, certifikata %d", err, errPosluzitelja, certifikata.Load())
					}
					c.Close()
					return
				}
				if err == nil || !strings.HasPrefix(err.Error(), sl.greska) {
					t.Errorf("greška pozivatelja: %v", err)
				}
				if errors.Is(err, ErrDrugiKljuc) != (sl.ime == "drugi ključ") {
					t.Errorf("greška %v kao ErrDrugiKljuc: %v", err, errors.Is(err, ErrDrugiKljuc))
				}
				if errPosluzitelja == nil || certifikata.Load() != 0 {
					t.Errorf("druga strana: rukovanje %v, dobila certifikata %d", errPosluzitelja, certifikata.Load())
				}
			})
		})
	}
}
