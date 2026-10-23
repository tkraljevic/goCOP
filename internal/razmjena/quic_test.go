package razmjena

// Razmjena preko QUIC-a (quic.go, quic_slusalica.go) na simnetu, mreži u
// memoriji iz quic-go-a, u mjehuru (uMjehuru): sat je probni, pa se rokovi
// ispituju bez čekanja.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/testutils/simnet"
)

var (
	adresaQUICA = &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 4713}
	adresaQUICB = &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 4713}
)

// mrezaQUIC daje dva prijenosa QUIC-a na simnetu, 5 ms u svakom smjeru;
// zatvaraju se na kraju testa
func mrezaQUIC(t *testing.T) (a, b *quic.Transport) {
	t.Helper()
	n := &simnet.Simnet{Router: &simnet.PerfectRouter{}}
	veza := simnet.NodeBiDiLinkSettings{Latency: 5 * time.Millisecond}
	a = &quic.Transport{Conn: n.NewEndpoint(adresaQUICA, veza)}
	b = &quic.Transport{Conn: n.NewEndpoint(adresaQUICB, veza)}
	bezGreske(t, n.Start())
	t.Cleanup(func() {
		a.Close()
		b.Close()
		n.Close()
	})
	return a, b
}

func svima(ed25519.PublicKey, []byte) bool { return true }

// slusalicaQUIC pokrene ServeExchangeQUIC na b i vrati kanal s njezinim
// ishodom
func slusalicaQUIC(ctx context.Context, b *quic.Transport, kljuc ed25519.PrivateKey, o *Ograda, trusted KeyChecker, handle func(*Conn)) chan error {
	kraj := make(chan error, 1)
	go func() { kraj <- ServeExchangeQUIC(ctx, b, kljuc, "testproto", nil, trusted, handle, o) }()
	return kraj
}

// Razgovor preko QUIC-a: druga strana vidi ključ i vjerodajnice, adrese su
// adrese veze i ostaju i nakon zatvaranja, a RTT je jedan obilazak.
func TestRazmjenaPrekoQUICa(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		posluzitelj, klijent := newKey(t), newKey(t)
		a, b := mrezaQUIC(t)
		ctx, otkazi := context.WithCancel(t.Context())
		defer otkazi()
		videno := make(chan *Conn, 1)
		kraj := slusalicaQUIC(ctx, b, posluzitelj, nil, svima, func(c *Conn) {
			defer c.Close()
			e, _ := c.Receive() // jeka: grešku razgovora vidi strana koja zove
			_ = c.Send(e)       // isto
			videno <- c
		})
		vj := func() []byte { return []byte(`{"clan":"laptop"}`) }
		c, err := DialExchangeQUIC(ctx, a, adresaQUICB, klijent, "testproto", vj, posluzitelj.Public().(ed25519.PublicKey))
		bezGreske(t, err)
		if c.RTT < 10*time.Millisecond || c.RTT > 50*time.Millisecond {
			t.Errorf("RTT %v, želim oko 10 ms", c.RTT)
		}
		odgovor, err := razgovaraj(c)
		if err != nil || odgovor.Kind != "frontier" {
			t.Fatalf("odgovor %+v: %v", odgovor, err)
		}
		synctest.Wait() // druga strana je zatvorila svoj kraj i prestala čitati
		bezGreske(t, c.Close())
		bezGreske(t, c.Close())
		if c.LocalAddr().String() != adresaQUICA.String() || c.RemoteAddr().String() != adresaQUICB.String() {
			t.Errorf("adrese nakon zatvaranja: %v → %v", c.LocalAddr(), c.RemoteAddr())
		}
		s := <-videno
		if !s.PeerKey.Equal(klijent.Public()) || string(s.Vjerodajnice) != `{"clan":"laptop"}` || s.RemoteAddr().String() != adresaQUICA.String() {
			t.Errorf("druga strana vidi %x, %s, %v", s.PeerKey, s.Vjerodajnice, s.RemoteAddr())
		}
		otkazi()
		if err := <-kraj; err != nil {
			t.Errorf("slušalica nakon kraja: %v", err)
		}
	})
}

// Krivi ključ druge strane prekida poziv usred rukovanja, a ključ koji nije
// član vrata zatvore bez razgovora.
func TestQUICKriviKljucIStranac(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		posluzitelj, klijent := newKey(t), newKey(t)
		a, b := mrezaQUIC(t)
		ctx, otkazi := context.WithCancel(t.Context())
		defer otkazi()
		var pozvan atomic.Bool
		slusalicaQUIC(ctx, b, posluzitelj, nil, func(ed25519.PublicKey, []byte) bool { return false }, func(c *Conn) { pozvan.Store(true) })
		if _, err := DialExchangeQUIC(ctx, a, adresaQUICB, klijent, "testproto", nil, newKey(t).Public().(ed25519.PublicKey)); err == nil {
			t.Fatal("poziv s krivim očekivanim ključem je prošao")
		}
		c, err := DialExchangeQUIC(ctx, a, adresaQUICB, klijent, "testproto", nil, posluzitelj.Public().(ed25519.PublicKey))
		bezGreske(t, err)
		if _, err := razgovaraj(c); err == nil {
			t.Error("stranac je dobio odgovor")
		}
		if pozvan.Load() {
			t.Error("razmjena sa strancem je počela")
		}
	})
}

// Pokvaren ključ ne da certifikat ni na jednoj strani, a druga slušalica na
// istom prijenosu ne može se otvoriti.
func TestQUICPokvarenKljucIZauzetPrijenos(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		a, b := mrezaQUIC(t)
		ctx, otkazi := context.WithCancel(t.Context())
		defer otkazi()
		if _, err := DialExchangeQUIC(ctx, a, adresaQUICB, pokvarenKljuc(t), "testproto", nil, newKey(t).Public().(ed25519.PublicKey)); err == nil {
			t.Error("poziv s pokvarenim ključem je prošao")
		}
		if err := ServeExchangeQUIC(ctx, b, pokvarenKljuc(t), "testproto", nil, svima, func(*Conn) {}); err == nil {
			t.Error("slušalica s pokvarenim ključem je krenula")
		}
		slusalicaQUIC(ctx, b, newKey(t), nil, svima, func(*Conn) {})
		synctest.Wait()
		if err := ServeExchangeQUIC(ctx, b, newKey(t), "testproto", nil, svima, func(*Conn) {}); err == nil {
			t.Error("druga slušalica na istom prijenosu je krenula")
		}
	})
}

// Slušalica kojoj se prijenos zatvori javi grešku; puna ograda s krajem ctx
// zatvori primljenu vezu i stane bez greške.
func TestQUICSlusalicaPadneIliJePuna(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		_, b := mrezaQUIC(t)
		ctx, otkazi := context.WithCancel(t.Context())
		defer otkazi()
		kraj := slusalicaQUIC(ctx, b, newKey(t), nil, svima, func(*Conn) {})
		synctest.Wait()
		b.Close()
		if err := <-kraj; err == nil {
			t.Error("slušalica zatvorenog prijenosa nije javila grešku")
		}

		posluzitelj := newKey(t)
		a2, b2 := mrezaQUIC(t)
		ctx2, otkazi2 := context.WithCancel(t.Context())
		defer otkazi2()
		kraj2 := slusalicaQUIC(ctx2, b2, posluzitelj, novaOgrada(0, 4, time.Second), svima, func(*Conn) {})
		_, err := DialExchangeQUIC(ctx, a2, adresaQUICB, newKey(t), "testproto", nil, posluzitelj.Public().(ed25519.PublicKey))
		bezGreske(t, err)
		time.Sleep(50 * time.Millisecond) // zadnja poruka rukovanja putuje do slušalice, koja tada čeka mjesto u ogradi
		synctest.Wait()
		otkazi2()
		if err := <-kraj2; err != nil {
			t.Errorf("puna slušalica nakon kraja: %v", err)
		}
	})
}

// Tko ne otvori tok u roku ograde, gubi vezu; greška u obradi razmjene ne
// ruši slušalicu.
func TestQUICBezTokaIGreskaUObradi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		posluzitelj := newKey(t)
		a, b := mrezaQUIC(t)
		ctx, otkazi := context.WithCancel(t.Context())
		defer otkazi()
		o := novaOgrada(4, 4, time.Second)
		var z zapisVrata
		o.KadOdbije(z.biljezi)
		kraj := slusalicaQUIC(ctx, b, posluzitelj, o, svima, func(*Conn) { panic("probna greška") })
		javni := posluzitelj.Public().(ed25519.PublicKey)

		tih, err := DialExchangeQUIC(ctx, a, adresaQUICB, newKey(t), "testproto", nil, javni)
		bezGreske(t, err)
		time.Sleep(2 * time.Second) // dulje od roka ograde
		if _, err := tih.Receive(); err == nil {
			t.Error("veza bez toka je ostala otvorena")
		}
		synctest.Wait()
		if r, _ := z.cekaj(t, 1); r != VrataRukovanje {
			t.Errorf("veza bez toka na vratima: %s", r)
		}

		c, err := DialExchangeQUIC(ctx, a, adresaQUICB, newKey(t), "testproto", nil, javni)
		bezGreske(t, err)
		if _, err := razgovaraj(c); err == nil {
			t.Error("razmjena s greškom u obradi je dala odgovor")
		}
		select {
		case err := <-kraj:
			t.Fatalf("slušalica je stala: %v", err)
		default:
		}
	})
}

// Poziv kojem druga strana ne da tok čeka do kraja ctx i zatvori vezu.
func TestQUICPozivBezToka(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		posluzitelj := newKey(t)
		a, b := mrezaQUIC(t)
		cfg, err := tlsZaQUIC(posluzitelj, "testproto", nil)
		bezGreske(t, err)
		ln, err := b.Listen(cfg, &quic.Config{MaxIncomingStreams: -1})
		bezGreske(t, err)
		defer ln.Close()
		go func() {
			if v, err := ln.Accept(t.Context()); err == nil {
				<-v.Context().Done()
			}
		}()
		ctx, otkazi := context.WithTimeout(t.Context(), 3*time.Second)
		defer otkazi()
		_, err = DialExchangeQUIC(ctx, a, adresaQUICB, newKey(t), "testproto", nil, posluzitelj.Public().(ed25519.PublicKey))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("poziv bez toka: %v, želim istek roka", err)
		}
	})
}

// ALPN WEB synca izvodi se samo iz imena protokola.
func TestAlpnWeb(t *testing.T) {
	if alpnWeb("gocop") != "gocop-web/1" || alpnWeb("gocop-sim") != "gocop-sim-web/1" {
		t.Error(alpnWeb("gocop"), alpnWeb("gocop-sim"))
	}
}
