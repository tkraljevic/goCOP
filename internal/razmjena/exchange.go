package razmjena

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
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
//
// Uz odbijenicu ide i Kod, broj razloga iz kodovi.go. Poruke bez njega
// ostaju bajt po bajt iste kao u 0.0.38, a čvor 0.0.38 polje zanemari.
type Envelope struct {
	Kind     string          `json:"kind"`
	Reason   string          `json:"reason,omitempty"`
	Kod      KodRazloga      `json:"kod,omitempty"`
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

// ErrDrugiKljuc je greška kad druga strana dokaže drugi ključ od uparenog
var ErrDrugiKljuc = errors.New("druga strana dokazala je drugi ključ od uparenog")

// drugiKljuc nosi tekst za korisnika; errors.Is je prepoznaje kao
// ErrDrugiKljuc. Tekstovi na portu i kroz tunel ostaju kakvi su bili, jer ih
// ljudi čitaju i navode (kljuc_cvora.go u peers).
type drugiKljuc struct{ poruka string }

func (e drugiKljuc) Error() string { return e.poruka }

func (e drugiKljuc) Is(cilj error) bool { return cilj == ErrDrugiKljuc }

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

// prijenos je ono što razmjena traži od veze: čitanje, pisanje, rokove i
// adrese. Razmjena danas uvijek ide preko *tls.Conn, na portu i kroz tunel, a
// kasnije i preko toka QUIC-a (F3), koji nije *tls.Conn. Kroz tunel je ispod
// TLS-a WebSocket (wsVeza), od kojeg TLS uzima rokove i adrese, pa provjera
// niže drži i njega.
type prijenos interface {
	io.ReadWriteCloser
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}

var (
	_ prijenos = (*tls.Conn)(nil)
	_ prijenos = (*wsVeza)(nil)
)

// Conn is an authenticated exchange connection: the TLS session plus the
// proven identity on the other end.
type Conn struct {
	PeerKey ed25519.PublicKey
	// Vjerodajnice su potvrda članstva (i ovlast) iz certifikata druge
	// strane, JSON; prazno kod starijih programa
	Vjerodajnice []byte
	// Put kojim je veza došla ili otišla. Postavlja ga peers, na strani koja
	// zove i na strani koja prima. Put i RTT nikad ne idu na žicu.
	Put Put
	// RTT je trajanje TLS rukovanja razmjene na strani koja zove: otprilike
	// jedan obilazak do druge strane i natrag, isto na portu i kroz tunel. U
	// njemu je i ono što obje strane rade tijekom rukovanja (vjerodajnice za
	// certifikat, u peers čitanje iz baze) i čekanje na slobodno mjesto za
	// rukovanje kod druge strane, pa pod opterećenjem ispadne veći od
	// samog obilaska. Na strani koja prima je nula.
	RTT     time.Duration
	raw     prijenos
	enc     *json.Encoder
	dec     *json.Decoder
	citac   *ograniceniCitac
	najvise int64 // ograda jedne primljene poruke; NajvecaPoruka
}

// newConnKljuc je veza razmjene nad prijenosom s već dokazanim ključem
func newConnKljuc(p prijenos, key ed25519.PublicKey, vj []byte) *Conn {
	// json.Decoder ionako čita u svoj međuspremnik, pa bufio nije potreban
	citac := &ograniceniCitac{r: p}
	return &Conn{PeerKey: key, Vjerodajnice: vj, raw: p, enc: json.NewEncoder(p), dec: json.NewDecoder(citac), citac: citac, najvise: NajvecaPoruka}
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

// LocalAddr je adresa ovog računala na koju je veza stigla (ili s koje je
// otišla); kroz tunel je to put tunela, ne IP adresa
func (c *Conn) LocalAddr() net.Addr { return c.raw.LocalAddr() }

// KeyChecker answers whether a proven public key belongs to a member —
// the application implements it over its memberships, and may accept a
// membership the peer presents in its certificate (vjerodajnice).
type KeyChecker func(pub ed25519.PublicKey, vjerodajnice []byte) bool

// ServeExchange accepts connections and hands each authenticated one to
// handle, until ctx ends. Unpaired keys are dropped at the door: the TLS
// handshake proves possession, the pin decides trust, and nothing else is
// read from a stranger. It returns an error at once when the port is
// taken — a caller's retry loop depends on that, and one that hung would
// leave a silent outage.
//
// Ograda (po želji) dijele sve slušalice jednog čvora; bez nje slušalica
// dobije svoju sa zadanim granicama.
func ServeExchange(ctx context.Context, priv ed25519.PrivateKey, protocol string, vj func() []byte, port int, trusted KeyChecker, handle func(*Conn), o ...*Ograda) error {
	cfg, err := tlsConfigS(priv, protocol, vj)
	if err != nil {
		return err
	}
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), cfg)
	if err != nil {
		return err
	}
	return serveTLS(ctx, ln, protocol, trusted, handle, ogradaIz(o))
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

// vrata primaju veze razmjene: provjere nakon rukovanja i mjesta u ogradi
type vrata struct {
	protokol string
	trusted  KeyChecker
	handle   func(*Conn)
	o        *Ograda
}

// serveTLS prima veze s TLS slušalice dok ctx traje; zajedničko portu
// razmjene i tunelu
func serveTLS(ctx context.Context, ln net.Listener, protocol string, trusted KeyChecker, handle func(*Conn), o *Ograda) error {
	v := &vrata{protokol: protocol, trusted: trusted, handle: handle, o: o}
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
		go v.primiTLS(ctx, raw)
	}
}

// primiTLS dovrši TLS rukovanje s vezom koju je slušalica primila, u roku
// ograde, i preda vezu na provjeru
func (v *vrata) primiTLS(ctx context.Context, nc net.Conn) {
	// greška u jednoj vezi ne smije srušiti čvor: veza se zatvori,
	// a ostale i web sučelje rade dalje
	defer func() {
		if r := recover(); r != nil {
			log.Printf("razmjena: veza %s prekinuta zbog greške: %v", nc.RemoteAddr(), r)
			nc.Close()
		}
	}()
	pustiRukovanje := sync.OnceFunc(func() { <-v.o.rukovanja })
	defer pustiRukovanje()
	tc := nc.(*tls.Conn)
	_ = tc.SetDeadline(time.Now().Add(v.o.rok)) // ne da se postaviti samo na zatvorenoj vezi, a na njoj rukovanje ionako padne
	if err := tc.HandshakeContext(ctx); err != nil {
		tc.Close()
		return
	}
	_ = tc.SetDeadline(time.Time{}) // kao gore; rokove dalje postavlja svaka poruka (Send, Receive)
	v.primiVezu(tc, tc.ConnectionState().PeerCertificates, pustiRukovanje)
}

// primiVezu provjeri vezu nakon rukovanja i preda je razmjeni: ključ i ime u
// certifikatu, članstvo, pa mjesto za razmjenu. Ne ovisi o prijenosu, pa isto
// radi i za tok QUIC-a (F3). Svaka provjera koja ne prođe ima svoj izlaz, da
// se odbijanja poslije mogu brojati po razlogu (G7).
func (v *vrata) primiVezu(p prijenos, certs []*x509.Certificate, pustiRukovanje func()) {
	key, vj, err := dokazaniKljuc(certs, v.protokol)
	if err != nil {
		_ = p.Close() // veza s nečim što nije čvor s istim imenom protokola samo se prekida; greška zatvaranja nema što reći
		return
	}
	conn := newConnKljuc(p, key, vj)
	if !v.trusted(conn.PeerKey, conn.Vjerodajnice) {
		conn.Close()
		return
	}
	pustiRukovanje()
	select {
	case v.o.razmjene <- struct{}{}:
	default:
		odbijZauzet(conn)
		return
	}
	defer func() { <-v.o.razmjene }()
	v.handle(conn)
}

// odbijZauzet javlja poznatom čvoru da je ovaj zauzet
func odbijZauzet(c *Conn) { Odbij(c, KodZauzet, RazlogZauzet) }

// Odbij odbija razmjenu s poznatim čvorom i zatvara vezu; druga strana
// prepozna kod i ispiše razlog. Pozivatelj uvijek prvi šalje svoju granicu;
// ona se pročita prije odbijenice, da zatvaranje s nepročitanim podacima ne
// pošalje RST koji bi odbijenicu progutao.
func Odbij(c *Conn, kod KodRazloga, razlog string) {
	defer c.Close()
	_ = c.raw.SetReadDeadline(time.Now().Add(10 * time.Second))
	c.citac.postavi(1 << 20)
	var prva Envelope
	_ = c.dec.Decode(&prva)
	_ = c.Send(Odbijenica(kod, razlog))
}

// DialExchange connects to a peer and refuses to proceed unless the key
// it proves is the one expected.
func DialExchange(ctx context.Context, priv ed25519.PrivateKey, protocol string, vj func() []byte, addr string, expect ed25519.PublicKey) (*Conn, error) {
	// rok vrijedi za spajanje i rukovanje zajedno, kao uz tls.Dialer
	ctx, otkazi := context.WithTimeout(ctx, 15*time.Second)
	defer otkazi()
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return DialExchangeOn(ctx, raw, priv, protocol, vj, addr, expect)
}

// DialExchangeOn je DialExchange preko već otvorene veze do drugog čvora
// (npr. veza koju je otvorio test); addr je samo za poruku o grešci. Veza se
// zatvara kad razmjena ne može početi.
func DialExchangeOn(ctx context.Context, raw net.Conn, priv ed25519.PrivateKey, protocol string, vj func() []byte, addr string, expect ed25519.PublicKey) (*Conn, error) {
	cfg, err := tlsConfigS(priv, protocol, vj)
	if err != nil {
		raw.Close()
		return nil, err
	}
	// Drugu stranu ovaj čvor provjeri usred rukovanja, prije nego što pošalje
	// svoj certifikat: u njemu je potvrda članstva, a na adresi iz
	// pronalaženja može se javiti bilo tko u lokalnoj mreži
	var key ed25519.PublicKey
	var vjDruge []byte
	cfg.VerifyConnection = func(cs tls.ConnectionState) (err error) {
		key, vjDruge, err = provjeriDrugu(cs.PeerCertificates, protocol, addr, expect)
		return err
	}
	tc := tls.Client(raw, cfg)
	// TLS 1.3 rukovanje na strani koja zove traje jedan obilazak i malo
	// računanja, pa je to RTT veze, i kroz tunel (što još ulazi: Conn.RTT)
	pocetak := time.Now()
	if err := tc.HandshakeContext(ctx); err != nil {
		tc.Close()
		return nil, err
	}
	rtt := time.Since(pocetak)
	conn := newConnKljuc(tc, key, vjDruge)
	conn.RTT = rtt
	return conn, nil
}

// provjeriDrugu provjerava drugu stranu na strani koja zove: certifikat čvora
// s istim imenom protokola (dokazaniKljuc) i ključ koji ovaj čvor očekuje.
// Zove je TLS usred rukovanja (VerifyConnection), pa greška stiže pozivatelju
// DialExchangeOn ista kakva je i bila nakon rukovanja.
func provjeriDrugu(certs []*x509.Certificate, protocol, addr string, expect ed25519.PublicKey) (ed25519.PublicKey, []byte, error) {
	key, vj, err := dokazaniKljuc(certs, protocol)
	if err != nil {
		return nil, nil, err
	}
	if !key.Equal(expect) {
		// Both fingerprints, and the two ways this happens. The second
		// one — another process holding that machine's sync port — was
		// invisible for a day because this message only offered the
		// first.
		return nil, nil, drugiKljuc{poruka: fmt.Sprintf("the device at %s proved a DIFFERENT key than the one paired — refusing to exchange. It presented %s…, the pairing recorded %s…. Either that machine was rebuilt (pair again), or another process holds its sync port", addr, PublicKeyString(key)[:8], PublicKeyString(expect)[:8])}
	}
	return key, vj, nil
}
