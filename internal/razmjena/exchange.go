package razmjena

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
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

// Conn is an authenticated exchange connection: the TLS session plus the
// proven identity on the other end.
type Conn struct {
	PeerKey ed25519.PublicKey
	raw     *tls.Conn
	enc     *json.Encoder
	dec     *json.Decoder
}

func newConn(c *tls.Conn) (*Conn, error) {
	key, err := peerPublicKey(rawPeerCerts(c))
	if err != nil {
		c.Close()
		return nil, err
	}
	return &Conn{PeerKey: key, raw: c, enc: json.NewEncoder(c), dec: json.NewDecoder(bufio.NewReader(c))}, nil
}

// Send and Receive move envelopes with a per-message deadline: a peer
// that stalls mid-exchange fails loudly instead of holding the port.
func (c *Conn) Send(e Envelope) error {
	_ = c.raw.SetWriteDeadline(time.Now().Add(60 * time.Second))
	return c.enc.Encode(e)
}

func (c *Conn) Receive() (Envelope, error) {
	_ = c.raw.SetReadDeadline(time.Now().Add(120 * time.Second))
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
func ServeExchange(ctx context.Context, priv ed25519.PrivateKey, protocol string, port int, trusted KeyChecker, handle func(*Conn)) error {
	cfg, err := tlsConfig(priv, protocol)
	if err != nil {
		return err
	}
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), cfg)
	if err != nil {
		return err
	}
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
		go func(nc net.Conn) {
			tc := nc.(*tls.Conn)
			_ = tc.SetDeadline(time.Now().Add(30 * time.Second))
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
			handle(conn)
		}(raw)
	}
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
