package razmjena

// Razmjena kroz tunel. Kad čvor smije izvana samo na web (Cloudflare tunel
// prenosi HTTPS i WebSocket, ne čisti TCP na portu razmjene), ista razmjena
// ide unutar WebSocketa na adresi web sučelja. Unutra teče isti TLS s
// ključevima čvorova kao na portu razmjene: tunel i Cloudflare vide samo
// šifrirane bajtove, a nepoznati ključ se odbija na istim vratima.

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// vanjskiTLS je TLS prema web poslužitelju (Cloudflare); nil je provjera
// prema sustavnim certifikatima. Testovi ga zamjenjuju svojim.
var vanjskiTLS *tls.Config

// PutTunela je put na web sučelju na kojem čvor prima razmjenu kroz tunel
const PutTunela = "/razmjena/tunel"

// JeAdresaTunela javlja ide li adresa kroz tunel (https://domena) a ne na
// port razmjene
func JeAdresaTunela(adresa string) bool {
	a := strings.ToLower(strings.TrimSpace(adresa))
	return strings.HasPrefix(a, "https://") || strings.HasPrefix(a, "wss://")
}

// NormalizirajTunel svodi adresu tunela na https://domena[:port], bez puta
func NormalizirajTunel(adresa string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(adresa))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("adresa tunela je oblika https://domena, npr. https://cop-osijek.com")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

// Tunel je slušalica razmjene na web sučelju: svaki WebSocket koji stigne na
// PutTunela postaje jedna veza za ServeExchangeOn.
type Tunel struct {
	veze    chan net.Conn
	gotov   chan struct{}
	zatvori sync.Once
}

// rokPredaje je koliko WebSocket čeka da ga razmjena primi (Accept)
var rokPredaje = 10 * time.Second

// NoviTunel otvara slušalicu tunela
func NoviTunel() *Tunel {
	return &Tunel{veze: make(chan net.Conn), gotov: make(chan struct{})}
}

// Handler prima WebSocket i predaje ga razmjeni; drži vezu dok je razmjena
// ne zatvori.
func (t *Tunel) Handler() http.Handler {
	return websocket.Server{
		// Izvor (Origin) se ne provjerava: ovo nije preglednik, nego čvor, a
		// tko je s druge strane dokazuje ključem unutar TLS-a.
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			ws.PayloadType = websocket.BinaryFrame
			v := &wsVeza{Conn: ws, kraj: make(chan struct{})}
			// Predaja čeka Accept najviše rokPredaje: kad razmjena ne radi
			// (port razmjene 0) ili je zauzeta, veza se zatvori umjesto da
			// gorutina i utičnica vise zauvijek.
			cekaj := time.NewTimer(rokPredaje)
			defer cekaj.Stop()
			select {
			case t.veze <- v:
			case <-t.gotov:
				ws.Close()
				return
			case <-cekaj.C:
				ws.Close()
				return
			}
			select {
			case <-v.kraj:
			case <-t.gotov:
				ws.Close()
			}
		},
	}
}

// Accept čeka sljedeću vezu kroz tunel
func (t *Tunel) Accept() (net.Conn, error) {
	select {
	case v := <-t.veze:
		return v, nil
	case <-t.gotov:
		return nil, net.ErrClosed
	}
}

// Close zatvara slušalicu
func (t *Tunel) Close() error {
	t.zatvori.Do(func() { close(t.gotov) })
	return nil
}

// Addr je adresa slušalice; tunel nema svoju, pa je to put
func (t *Tunel) Addr() net.Addr { return tunelAdresa(PutTunela) }

type tunelAdresa string

func (a tunelAdresa) Network() string { return "websocket" }
func (a tunelAdresa) String() string  { return string(a) }

// wsVeza je WebSocket kao net.Conn; Close javlja handleru da smije završiti
type wsVeza struct {
	*websocket.Conn
	kraj    chan struct{}
	zatvori sync.Once
}

// RemoteAddr i LocalAddr: WebSocket na strani poslužitelja nema adresu
// druge strane (iza tunela je ionako adresa tunela), pa se vraća put
// tunela umjesto praznog URL-a na kojem bi ispis pao.
func (v *wsVeza) RemoteAddr() net.Addr { return tunelAdresa("tunel" + PutTunela) }
func (v *wsVeza) LocalAddr() net.Addr  { return tunelAdresa(PutTunela) }

func (v *wsVeza) Close() error {
	err := v.Conn.Close()
	v.zatvori.Do(func() { close(v.kraj) })
	return err
}

// ServeExchangeOn je ServeExchange nad zadanom slušalicom (npr. tunelom)
func ServeExchangeOn(ctx context.Context, priv ed25519.PrivateKey, protocol string, ln net.Listener, trusted KeyChecker, handle func(*Conn), o ...*Ograda) error {
	cfg, err := tlsConfig(priv, protocol)
	if err != nil {
		return err
	}
	return serveTLS(ctx, tls.NewListener(ln, cfg), trusted, handle, ogradaIz(o))
}

// DialTunel spaja se na čvor kroz tunel (https://domena) i, kao
// DialExchange, odbija nastaviti ako druga strana ne dokaže očekivani ključ.
func DialTunel(ctx context.Context, priv ed25519.PrivateKey, protocol, adresa string, expect ed25519.PublicKey) (*Conn, error) {
	baza, err := NormalizirajTunel(adresa)
	if err != nil {
		return nil, err
	}
	wsURL := "wss://" + strings.TrimPrefix(baza, "https://") + PutTunela
	wcfg, err := websocket.NewConfig(wsURL, baza)
	if err != nil {
		return nil, err
	}
	wcfg.Dialer = &net.Dialer{Timeout: 15 * time.Second}
	wcfg.TlsConfig = vanjskiTLS
	ws, err := wcfg.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("tunel %s: %w", baza, err)
	}
	ws.PayloadType = websocket.BinaryFrame
	cfg, err := tlsConfig(priv, protocol)
	if err != nil {
		ws.Close()
		return nil, err
	}
	tc := tls.Client(&wsVeza{Conn: ws, kraj: make(chan struct{})}, cfg)
	hctx, otkazi := context.WithTimeout(ctx, 30*time.Second)
	defer otkazi()
	if err := tc.HandshakeContext(hctx); err != nil {
		tc.Close()
		return nil, fmt.Errorf("tunel %s: %w", baza, err)
	}
	conn, err := newConn(tc)
	if err != nil {
		return nil, err
	}
	if !conn.PeerKey.Equal(expect) {
		conn.Close()
		return nil, errors.New("čvor na " + baza + " dokazao je drugi ključ od uparenog — razmjena odbijena")
	}
	return conn, nil
}
