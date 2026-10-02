package razmjena

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// The exchange runs over the same TLS shape as pairing, with the one
// difference that makes it safe to run unattended: the peer's public key
// is REQUIRED to match one the owner confirmed. Pairing earns trust once,
// with a human watching; every exchange spends it, with nobody watching.

// Envelope is one message of an exchange conversation. The package fixes
// only the frame; what the kinds mean and what the payload holds is the
// application's protocol. Reason accompanies a refusal, sent before
// closing so the other machine reads a sentence instead of EOF.
type Envelope struct {
	Kind     string          `json:"kind"`
	Reason   string          `json:"reason,omitempty"`
	DeviceID string          `json:"deviceId,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// NewEnvelope marshals v as the payload of an envelope of the given kind.
func NewEnvelope(kind string, v any) (Envelope, error) {
	if v == nil {
		return Envelope{Kind: kind}, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Kind: kind, Payload: b}, nil
}

// Decode unmarshals the payload into v.
func (e Envelope) Decode(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(e.Payload, v)
}

// NajvecaPoruka je najveća poruka razmjene koja se prima. Mora pokriti
// starije pošiljatelje, čija delta nema ogradu u bajtovima i raste s
// knjigom (oko 49 MB prvom razmjenom 2026.), i sadržaj (48 MiB kao base64,
// oko 64 MB). Kad svi čvorovi budu na 0.0.25 ili novijem, smije na 128 MiB.
const NajvecaPoruka = 256 << 20

// ErrPorukaPrevelika je poruka razmjene veća od dopuštene; veza se prekida
var ErrPorukaPrevelika = errors.New("poruka razmjene veća od dopuštene")

// prevelikaPoruka kaže i koliko je dopušteno; errors.Is je prepoznaje kao
// ErrPorukaPrevelika
type prevelikaPoruka struct{ ograda int64 }

func (e prevelikaPoruka) Error() string {
	if e.ograda >= 1<<20 {
		return fmt.Sprintf("poruka razmjene veća od %d MiB", e.ograda>>20)
	}
	return fmt.Sprintf("poruka razmjene veća od %d KiB", e.ograda>>10)
}

func (e prevelikaPoruka) Is(cilj error) bool { return cilj == ErrPorukaPrevelika }

// ograniceniCitac broji bajtove jedne poruke i prekida kad prijeđe ogradu.
// Ograda se postavi prije svakog čitanja poruke. Dekoder čita unaprijed:
// bajtovi sljedeće poruke pročitani uz prethodnu broje se prethodnoj, pa
// ograda nije točna na bajt (najviše dvostruka), ali nijedna poruka ne raste
// bez granice.
type ograniceniCitac struct {
	r      io.Reader
	ostalo int64
	ograda int64
}

func (o *ograniceniCitac) postavi(n int64) { o.ostalo, o.ograda = n, n }

func (o *ograniceniCitac) Read(p []byte) (int, error) {
	if o.ostalo <= 0 {
		return 0, prevelikaPoruka{o.ograda}
	}
	if int64(len(p)) > o.ostalo {
		p = p[:o.ostalo]
	}
	n, err := o.r.Read(p)
	o.ostalo -= int64(n)
	return n, err
}

// Conn is an authenticated exchange connection: the TLS session plus the
// proven identity on the other end.
type Conn struct {
	PeerKey ed25519.PublicKey
	raw     *tls.Conn
	enc     *json.Encoder
	dec     *json.Decoder
	citac   *ograniceniCitac
	najvise int64 // ograda jedne primljene poruke; NajvecaPoruka
}

func newConn(c *tls.Conn) (*Conn, error) {
	key, err := peerPublicKey(rawPeerCerts(c))
	if err != nil {
		c.Close()
		return nil, err
	}
	// json.Decoder ionako čita u svoj međuspremnik, pa bufio nije potreban
	citac := &ograniceniCitac{r: c}
	return &Conn{PeerKey: key, raw: c, enc: json.NewEncoder(c), dec: json.NewDecoder(citac), citac: citac, najvise: NajvecaPoruka}, nil
}

// Send and Receive move envelopes with a per-message deadline: a peer
// that stalls mid-exchange fails loudly instead of holding the port.
func (c *Conn) Send(e Envelope) error {
	_ = c.raw.SetWriteDeadline(time.Now().Add(60 * time.Second))
	return c.enc.Encode(e)
}

func (c *Conn) Receive() (Envelope, error) {
	_ = c.raw.SetReadDeadline(time.Now().Add(120 * time.Second))
	c.citac.postavi(c.najvise)
	var e Envelope
	err := c.dec.Decode(&e)
	return e, err
}

func (c *Conn) Close() error { return c.raw.Close() }

// RemoteAddr is where the peer connected from.
func (c *Conn) RemoteAddr() net.Addr { return c.raw.RemoteAddr() }

// KeyChecker answers whether a proven public key belongs to a paired
// device — the application implements it over its list of peers.
type KeyChecker func(pub ed25519.PublicKey) bool

// ServeExchange accepts connections and hands each authenticated one to
// handle, until ctx ends. Unpaired keys are dropped at the door: the TLS
// handshake proves possession, the pin decides trust, and nothing else is
// read from a stranger. It returns an error at once when the port is
// taken — a caller's retry loop depends on that, and one that hung would
// leave a silent outage.
//
// Ograda (po želji) dijele sve slušalice jednog čvora; bez nje slušalica
// dobije svoju sa zadanim granicama.
func ServeExchange(ctx context.Context, priv ed25519.PrivateKey, protocol string, port int, trusted KeyChecker, handle func(*Conn), o ...*Ograda) error {
	cfg, err := tlsConfig(priv, protocol)
	if err != nil {
		return err
	}
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), cfg)
	if err != nil {
		return err
	}
	return serveTLS(ctx, ln, trusted, handle, ogradaIz(o))
}

// Ograde primanja. Rukovanje (TLS prije nego što se zna tko je s druge
// strane) jedino je što stranac smije potrošiti, pa ih teče najviše 32
// odjednom i svako traje najviše 15 s (TLS 1.3 je u lokalnoj mreži gotov
// ispod sekunde, kroz Cloudflare u nekoliko obilazaka). Razmjena s poznatim
// čvorom drži u memoriji oko tri veličine poruke, a na istom računalu rade i
// prognoze, pa ih teče najviše četiri; peta dobije odbijenicu koju i stariji
// čvor ispiše ("čvor odbio razmjenu: …") i pokuša opet sljedećim krugom.
const (
	najviseRukovanja = 32
	rokRukovanja     = 15 * time.Second
	najviseRazmjena  = 4
)

// RazlogZauzet je odbijenica razmjene kad čvor već razmjenjuje s drugima
const RazlogZauzet = "čvor je zauzet, pokušajte kasnije"

// VrstaOdbijeno je vrsta poruke kojom čvor odbija razmjenu. Stariji čvor
// ne zna za nju, ali ispiše Reason uz nju.
const VrstaOdbijeno = "odbijeno"

// Ograda kaže koliko rukovanja i razmjena teče odjednom. Port i tunel dijele
// razmjene, a rukovanja imaju svoja (NovaOgradaTunela).
type Ograda struct {
	rukovanja chan struct{}
	razmjene  chan struct{}
	rok       time.Duration
}

// NovaOgrada daje ogradu sa zadanim granicama čvora
func NovaOgrada() *Ograda { return novaOgrada(najviseRukovanja, najviseRazmjena, rokRukovanja) }

// NovaOgradaTunela je zasebna ograda tunela: kroz njega dolazi bilo tko s
// interneta, pa njegova nedovršena rukovanja ne smiju zauzeti mjesta porta
// razmjene u lokalnoj mreži, a rukovanje kroz Cloudflare traje djelić
// sekunde, pa onaj tko šuti gubi mjesto nakon 5 s. Razmjene dijeli s
// ogradom porta: koliko razmjena čvor nosi odjednom ne ovisi o tome kojim su
// vratima došle.
func NovaOgradaTunela(port *Ograda) *Ograda {
	o := novaOgrada(najviseRukovanja/2, najviseRazmjena, 5*time.Second)
	if port != nil {
		o.razmjene = port.razmjene
	}
	return o
}

func novaOgrada(rukovanja, razmjena int, rok time.Duration) *Ograda {
	return &Ograda{rukovanja: make(chan struct{}, rukovanja), razmjene: make(chan struct{}, razmjena), rok: rok}
}

func ogradaIz(o []*Ograda) *Ograda {
	if len(o) > 0 && o[0] != nil {
		return o[0]
	}
	return NovaOgrada()
}

// serveTLS prima veze s TLS slušalice dok ctx traje; zajedničko portu
// razmjene i tunelu
func serveTLS(ctx context.Context, ln net.Listener, trusted KeyChecker, handle func(*Conn), o *Ograda) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		raw, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		// kad su sva mjesta za rukovanje zauzeta, petlja čeka i ne prima
		// dalje: nove veze čekaju u redu jezgre (ili tunela) umjesto da se
		// gomilaju u memoriji
		select {
		case o.rukovanja <- struct{}{}:
		case <-ctx.Done():
			raw.Close()
			return nil
		}
		go func(nc net.Conn) {
			// greška u jednoj vezi ne smije srušiti čvor: veza se zatvori,
			// a ostale i web sučelje rade dalje
			defer func() {
				if r := recover(); r != nil {
					log.Printf("razmjena: veza %s prekinuta zbog greške: %v", nc.RemoteAddr(), r)
					nc.Close()
				}
			}()
			pustiRukovanje := sync.OnceFunc(func() { <-o.rukovanja })
			defer pustiRukovanje()
			tc := nc.(*tls.Conn)
			_ = tc.SetDeadline(time.Now().Add(o.rok))
			if err := tc.HandshakeContext(ctx); err != nil {
				tc.Close()
				return
			}
			_ = tc.SetDeadline(time.Time{})
			conn, err := newConn(tc)
			if err != nil {
				return
			}
			if !trusted(conn.PeerKey) {
				conn.Close()
				return
			}
			pustiRukovanje()
			select {
			case o.razmjene <- struct{}{}:
			default:
				odbijZauzet(conn)
				return
			}
			defer func() { <-o.razmjene }()
			handle(conn)
		}(raw)
	}
}

// odbijZauzet javlja poznatom čvoru da je ovaj zauzet. Pozivatelj uvijek
// prvi šalje svoju granicu; ona se pročita prije odbijenice, da zatvaranje
// s nepročitanim podacima ne pošalje RST koji bi odbijenicu progutao.
func odbijZauzet(c *Conn) {
	defer c.Close()
	_ = c.raw.SetReadDeadline(time.Now().Add(10 * time.Second))
	c.citac.postavi(1 << 20)
	var prva Envelope
	_ = c.dec.Decode(&prva)
	_ = c.Send(Envelope{Kind: VrstaOdbijeno, Reason: RazlogZauzet})
}

// DialExchange connects to a peer and refuses to proceed unless the key
// it proves is the one expected.
func DialExchange(ctx context.Context, priv ed25519.PrivateKey, protocol, addr string, expect ed25519.PublicKey) (*Conn, error) {
	cfg, err := tlsConfig(priv, protocol)
	if err != nil {
		return nil, err
	}
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: cfg}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := newConn(raw.(*tls.Conn))
	if err != nil {
		return nil, err
	}
	if !conn.PeerKey.Equal(expect) {
		got := PublicKeyString(conn.PeerKey)
		conn.Close()
		// Both fingerprints, and the two ways this happens. The second
		// one — another process holding that machine's sync port — was
		// invisible for a day because this message only offered the
		// first.
		return nil, fmt.Errorf("the device at %s proved a DIFFERENT key than the one paired — refusing to exchange. It presented %s…, the pairing recorded %s…. Either that machine was rebuilt (pair again), or another process holds its sync port", addr, got[:8], PublicKeyString(expect)[:8])
	}
	return conn, nil
}
