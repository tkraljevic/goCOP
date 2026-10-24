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

// PutTunela je put na web sučelju na kojem čvor prima razmjenu kroz tunel
const PutTunela = "/razmjena/tunel"

// PutSignala je put na web sučelju na kojem se računala prijavljuju kod
// sastajališta za WEB sync (plan WEB synca, F2)
const PutSignala = "/razmjena/signal"

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
		return "", fmt.Errorf("adresa tunela je javna adresa čvora, oblika https://domena")
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

// wsVeza je WebSocket kao net.Conn; Close javlja handleru da smije završiti.
// Put je put web sučelja (PutTunela kad je prazan, ili PutSignala).
type wsVeza struct {
	*websocket.Conn
	kraj    chan struct{}
	zatvori sync.Once
	put     string
}

// RemoteAddr i LocalAddr: WebSocket na strani poslužitelja nema adresu
// druge strane (iza tunela je ionako adresa tunela), pa se vraća put
// umjesto praznog URL-a na kojem bi ispis pao.
func (v *wsVeza) RemoteAddr() net.Addr { return tunelAdresa("tunel" + v.putVeze()) }
func (v *wsVeza) LocalAddr() net.Addr  { return tunelAdresa(v.putVeze()) }

func (v *wsVeza) putVeze() string {
	if v.put == "" {
		return PutTunela
	}
	return v.put
}

func (v *wsVeza) Close() error {
	err := v.Conn.Close()
	v.zatvori.Do(func() { close(v.kraj) })
	return err
}

// ServeExchangeOn je ServeExchange nad zadanom slušalicom (npr. tunelom)
func ServeExchangeOn(ctx context.Context, priv ed25519.PrivateKey, protocol string, vj func() []byte, ln net.Listener, trusted KeyChecker, handle func(*Conn), o ...*Ograda) error {
	cfg, err := tlsConfigS(priv, protocol, vj)
	if err != nil {
		return err
	}
	return serveTLS(ctx, tls.NewListener(ln, cfg), protocol, trusted, handle, ogradaIz(o))
}

// KlijentTunela spaja se na čvor kroz tunel. Nulta vrijednost je zadano:
// TCP i vanjski TLS prema sustavnim certifikatima, s rokom od 15 s za oboje
// (kao do sada). Test zadaje svoje korijene i spajanje preko mreže u memoriji.
type KlijentTunela struct {
	TLS  *tls.Config                                                         // nil = sustavni certifikati
	Dial func(ctx context.Context, network, adresa string) (net.Conn, error) // nil = net.Dialer
}

// rokVanjske je rok za TCP i vanjski TLS do web poslužitelja tunela
const rokVanjske = 15 * time.Second

// DialTunel spaja se na čvor kroz tunel (https://domena) i, kao
// DialExchange, odbija nastaviti ako druga strana ne dokaže očekivani ključ.
// Unutra je isto rukovanje kao na portu razmjene (DialExchangeOn).
//
// ctx mora imati rok: vlastiti rok ima samo TCP s vanjskim TLS-om
// (rokVanjske), a nadogradnja na WebSocket i rukovanje unutra čekaju koliko
// ctx dopusti (peers zove s 20 s). Bez roka poziv na poslužitelj koji šuti
// visi zauvijek.
func (k *KlijentTunela) DialTunel(ctx context.Context, priv ed25519.PrivateKey, protocol string, vj func() []byte, adresa string, expect ed25519.PublicKey) (*Conn, error) {
	nc, baza, err := k.uTunel(ctx, adresa, PutTunela)
	if err != nil {
		return nil, err
	}
	conn, err := DialExchangeOn(ctx, nc, priv, protocol, vj, baza, expect)
	if err != nil {
		return nil, porukaTunela(baza, err)
	}
	return conn, nil
}

// uTunel otvara WebSocket na putu web sučelja (PutTunela, PutSignala) i
// vraća ga s adresom svedenom na https://domena[:port]; greška nosi tu
// adresu
func (k *KlijentTunela) uTunel(ctx context.Context, adresa, put string) (net.Conn, string, error) {
	baza, err := NormalizirajTunel(adresa)
	if err != nil {
		return nil, "", err
	}
	nc, err := k.otvoriWebSocket(ctx, baza, put)
	if err != nil {
		return nil, "", porukaTunela(baza, err)
	}
	return nc, baza, nil
}

// porukaTunela: kriv ključ kroz tunel javlja se rečenicom koju korisnik već
// zna (navodi je i poruka o ključu čvora u peers), a ostalo s adresom tunela
func porukaTunela(baza string, err error) error {
	if errors.Is(err, ErrDrugiKljuc) {
		return drugiKljuc{poruka: "čvor na " + baza + " dokazao je drugi ključ od uparenog — razmjena odbijena"}
	}
	return fmt.Errorf("tunel %s: %w", baza, err)
}

// otvoriWebSocket otvara WebSocket na putu web sučelja (baza je
// https://domena[:port]) i vraća ga kao net.Conn. Okviri su binarni (0x82):
// TLS unutra nije tekst, a posrednik po RFC 6455 smije zatvoriti tekstni
// okvir koji nije UTF-8.
func (k *KlijentTunela) otvoriWebSocket(ctx context.Context, baza, put string) (net.Conn, error) {
	vanjska, err := k.otvoriVanjsku(ctx, baza)
	if err != nil {
		return nil, err
	}
	ws, err := noviWebSocket(ctx, vanjska, nadogradnja(strings.TrimPrefix(baza, "https://"), put))
	if err != nil {
		vanjska.Close()
		return nil, err
	}
	ws.PayloadType = websocket.BinaryFrame
	// Omot nije samo radi Close: goli WebSocket klijenta javlja RemoteAddr
	// kao wss://…, a net.SplitHostPort iz toga čita računalo „wss”.
	return &wsVeza{Conn: ws, kraj: make(chan struct{}), put: put}, nil
}

// nadogradnja je zahtjev za WebSocket na putu web poslužitelja, isti kakav
// daje websocket.NewConfig("wss://"+host+put, "https://"+host), kojim ga je
// slao DialTunel do 0.0.39: Host i Origin su domena kako je upisana, bez
// dodanog porta 443, verzija 13, bez Sec-WebSocket-Protocol.
func nadogradnja(host, put string) *websocket.Config {
	return &websocket.Config{
		Location: &url.URL{Scheme: "wss", Host: host, Path: put},
		Origin:   &url.URL{Scheme: "https", Host: host},
		Version:  websocket.ProtocolVersionHybi13,
		Header:   http.Header{},
	}
}

// otvoriVanjsku otvara TCP i vanjski TLS do web poslužitelja tunela
// (Cloudflare), u roku rokVanjske za oboje, kao tls.Dialer do sada
func (k *KlijentTunela) otvoriVanjsku(ctx context.Context, baza string) (net.Conn, error) {
	ctx, otkazi := context.WithTimeout(ctx, rokVanjske)
	defer otkazi()
	adresa, ime := vanjskaAdresa(baza)
	raw, err := k.dial(ctx, "tcp", adresa)
	if err != nil {
		return nil, err
	}
	tc := tls.Client(raw, k.vanjskiTLS(ime))
	if err := tc.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return tc, nil
}

// vanjskaAdresa: adresa za spajanje na web poslužitelj tunela, s portom 443
// kad ga adresa nema (kao websocket.Dial), i ime za provjeru njegova
// certifikata, bez porta i uglatih zagrada IPv6 adrese. Port ide samo u
// adresu za spajanje, nikad u zaglavlje Host.
func vanjskaAdresa(baza string) (adresa, ime string) {
	host := strings.TrimPrefix(baza, "https://")
	ime, port, err := net.SplitHostPort(host)
	if err != nil {
		ime, port = strings.Trim(host, "[]"), "443"
	}
	return net.JoinHostPort(ime, port), ime
}

// vanjskiTLS je TLS prema web poslužitelju: zadani ili sustavni, s imenom
// poslužitelja kad ga postavka nema (kao tls.Dialer)
func (k *KlijentTunela) vanjskiTLS(ime string) *tls.Config {
	cfg := &tls.Config{}
	if k.TLS != nil {
		cfg = k.TLS.Clone()
	}
	if cfg.ServerName == "" {
		cfg.ServerName = ime
	}
	return cfg
}

// dial otvara TCP vezu zadanim spajanjem, ili kao net.Dialer
func (k *KlijentTunela) dial(ctx context.Context, network, adresa string) (net.Conn, error) {
	if k.Dial != nil {
		return k.Dial(ctx, network, adresa)
	}
	return (&net.Dialer{}).DialContext(ctx, network, adresa)
}

// noviWebSocket radi nadogradnju na WebSocket nad otvorenom vezom, u roku
// konteksta. websocket.NewClient sam ne gleda kontekst, pa radi u svojoj
// gorutini; kad kontekst istekne, rok na vezi prekine čekanje odgovora, kao u
// websocket.Dial (x/net v0.59.0). Vezu zatvara pozivatelj.
func noviWebSocket(ctx context.Context, rwc net.Conn, cfg *websocket.Config) (*websocket.Conn, error) {
	var ws *websocket.Conn
	var err error
	gotovo := make(chan struct{})
	go func() {
		defer close(gotovo)
		ws, err = websocket.NewClient(cfg, rwc)
	}()
	select {
	case <-gotovo:
		return ws, err
	case <-ctx.Done():
		_ = rwc.SetDeadline(time.Now()) // samo budi NewClient; veza se odmah zatim zatvara
		<-gotovo
		return nil, ctx.Err()
	}
}
