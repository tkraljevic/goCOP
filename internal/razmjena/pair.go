package razmjena

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Identity is what a device says about itself when pairing. The public
// key is deliberately NOT in here — it is read from the TLS certificate,
// where it was proven by the handshake; a key stated in a message would
// be a claim, not a proof.
type Identity struct {
	// Protocol names the application. Devices of different protocols
	// refuse to pair, and their pairing codes never match by construction.
	Protocol string
	DeviceID string
	// Name is shown to the person on the other screen. Empty means the
	// machine's hostname.
	Name    string
	Version string
	// Meta carries whatever else the application wants the other side to
	// know before the human decides — an edition, a role, a schema
	// version. Every value is a claim, not a proof: a patched build says
	// whatever it likes. It is for stopping honest mismatches, not attacks.
	Meta map[string]string
}

// Hello is each side's half of the pairing exchange, sent over the
// already-established TLS connection.
type Hello struct {
	Protocol string            `json:"protocol"`
	DeviceID string            `json:"deviceId"`
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Meta     map[string]string `json:"meta,omitempty"`
}

func (id Identity) hello() Hello {
	name := id.Name
	if name == "" {
		name = hostname()
	}
	return Hello{Protocol: id.Protocol, DeviceID: id.DeviceID, Name: name, Version: id.Version, Meta: id.Meta}
}

// PairResult is what a completed (but not yet confirmed) handshake knows:
// who is on the other end, proven by TLS, and the code both screens must
// show before anything is stored.
type PairResult struct {
	Peer    Hello
	PeerKey ed25519.PublicKey
	SAS     string
	conn    *tls.Conn
}

// PeerHost is the other machine's address without a port, or "" when it
// cannot be told.
//
// Without a port on purpose. What gets stored is where the machine is,
// not which door this one conversation used: pairing happens on one port
// and every exchange afterwards on another, so keeping the pairing port
// would send every later sync knocking at a door nobody answers.
func (r *PairResult) PeerHost() string {
	if r.conn == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.conn.RemoteAddr().String())
	if err != nil {
		return ""
	}
	return host
}

// Confirm tells the other side this person approved the code — sent only
// after a human said yes, and required from both directions before either
// side persists anything: one screen confirming is half a pairing.
//
// Payload is whatever the application wants to hand over at that moment —
// typically a membership certificate and the network's public key, so the
// newly paired device is a member the instant the ceremony ends. It is
// sent only alongside approval and read only from an approving peer.
type Confirm struct {
	Approved bool            `json:"approved"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// ErrNotAPeer marks a connection that never spoke TLS — a probe, a
// scanner, a browser — as opposed to a device that handshook and then
// misbehaved. Only the former is worth waiting past.
var ErrNotAPeer = errors.New("the connection did not complete a TLS handshake")

// ErrWrongProtocol is a device of another application, refused before a
// human is ever asked to compare codes.
var ErrWrongProtocol = errors.New("the peer speaks a different protocol")

// Listen waits for exactly one pairing attempt and returns it for the
// person to judge. The caller shows result.SAS, asks the human, then
// calls result.Finish with the answer.
func Listen(ctx context.Context, priv ed25519.PrivateKey, id Identity, port int) (*PairResult, error) {
	cfg, err := tlsConfig(priv, id.Protocol)
	if err != nil {
		return nil, err
	}
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), cfg)
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	type accepted struct {
		conn net.Conn
		err  error
	}
	// One connection used to be the whole ceremony: whoever connected
	// first was handshaken with, and if that was a port scanner, a
	// browser, or the beacon's own liveness probe, the
	// ceremony failed before the real device dialled. A connection that
	// never completes the TLS handshake is not a device; drop it and keep
	// waiting, until the context says stop.
	for {
		ch := make(chan accepted, 1)
		go func() {
			c, err := ln.Accept()
			ch <- accepted{c, err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case a := <-ch:
			if a.err != nil {
				return nil, a.err
			}
			res, err := completeHandshake(a.conn.(*tls.Conn), priv, id)
			if errors.Is(err, ErrNotAPeer) {
				continue
			}
			return res, err
		}
	}
}

// Dial connects to a listening device and returns the same judgment point.
func Dial(ctx context.Context, priv ed25519.PrivateKey, id Identity, addr string) (*PairResult, error) {
	cfg, err := tlsConfig(priv, id.Protocol)
	if err != nil {
		return nil, err
	}
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: cfg}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return completeHandshake(conn.(*tls.Conn), priv, id)
}

func completeHandshake(conn *tls.Conn, priv ed25519.PrivateKey, id Identity) (*PairResult, error) {
	deadline := time.Now().Add(30 * time.Second)
	_ = conn.SetDeadline(deadline)
	if err := conn.HandshakeContext(context.Background()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrNotAPeer, err)
	}
	peerKey, err := peerPublicKey(rawPeerCerts(conn))
	if err != nil {
		conn.Close()
		return nil, err
	}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(id.hello()); err != nil {
		conn.Close()
		return nil, err
	}
	var peer Hello
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&peer); err != nil {
		conn.Close()
		// EOF here means the connection was accepted and then closed
		// without a word, which in practice means one thing: the other
		// machine is not waiting to pair. Saying "reading the peer's
		// hello: EOF" leaves somebody reading a protocol detail and
		// guessing at the remedy, when the remedy is a sentence.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("that machine answered but is not waiting to pair — start pairing there first, then dial it from here while it waits")
		}
		return nil, fmt.Errorf("reading the peer's hello: %w", err)
	}
	if peer.Protocol != id.Protocol {
		conn.Close()
		return nil, fmt.Errorf("%w: it says %q, this device speaks %q", ErrWrongProtocol, peer.Protocol, id.Protocol)
	}
	_ = conn.SetDeadline(time.Time{})
	return &PairResult{
		Peer:    peer,
		PeerKey: peerKey,
		SAS:     SASCode(id.Protocol, priv.Public().(ed25519.PublicKey), peerKey),
		conn:    conn,
	}, nil
}

// Finish sends this person's verdict — with an optional payload for the
// peer — and waits for the other side's. Both must approve; either refusal,
// or a vanished peer, fails the pairing — and nothing was stored yet, which
// is what makes failing free. The peer's payload is returned only when the
// pairing succeeded.
func (r *PairResult) Finish(approved bool, payload any) (bool, json.RawMessage, error) {
	defer r.conn.Close()
	_ = r.conn.SetDeadline(time.Now().Add(120 * time.Second))

	mine := Confirm{Approved: approved}
	if approved && payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return false, nil, err
		}
		mine.Payload = b
	}
	if err := json.NewEncoder(r.conn).Encode(mine); err != nil {
		return false, nil, err
	}
	var theirs Confirm
	if err := json.NewDecoder(r.conn).Decode(&theirs); err != nil {
		return false, nil, fmt.Errorf("the peer went away before confirming: %w", err)
	}
	if !approved || !theirs.Approved {
		return false, nil, nil
	}
	return true, theirs.Payload, nil
}
