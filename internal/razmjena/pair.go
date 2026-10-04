package razmjena

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
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
	// SAS je inačica dogovora koda; 2 = s obvezom unaprijed (dogovoriKod).
	// Stariji program je ne šalje i s njim se ne uparuje.
	SAS int `json:"sas,omitempty"`
}

// sasInacica je dogovor koda koji ovaj program traži od druge strane
const sasInacica = 2

func (id Identity) hello() Hello {
	name := id.Name
	if name == "" {
		name = hostname()
	}
	return Hello{Protocol: id.Protocol, DeviceID: id.DeviceID, Name: name, Version: id.Version, Meta: id.Meta, SAS: sasInacica}
}

// PairResult is what a completed (but not yet confirmed) handshake knows:
// who is on the other end, proven by TLS, and the code both screens must
// show before anything is stored.
type PairResult struct {
	Peer    Hello
	PeerKey ed25519.PublicKey
	SAS     string
	conn    *tls.Conn
	// dec i citac čitaju cijeli razgovor: dekoder zna u međuspremnik
	// uzeti i sljedeću poruku druge strane, pa se ne smije zamijeniti novim
	dec   *json.Decoder
	citac *ograniceniCitac
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

// Ograde poruka uparivanja: obje se čitaju od uređaja kojem se još ne
// vjeruje
const (
	najveciHello   = 64 << 10
	najvecaPotvrda = 1 << 20
)

// ErrNotAPeer marks a connection that never spoke TLS — a probe, a
// scanner, a browser — as opposed to a device that handshook and then
// misbehaved. Only the former is worth waiting past.
var ErrNotAPeer = errors.New("the connection did not complete a TLS handshake")

// ErrWrongProtocol is a device of another application, refused before a
// human is ever asked to compare codes.
var ErrWrongProtocol = errors.New("the peer speaks a different protocol")

// ErrStaroUparivanje: druga strana dogovara kod bez obveze unaprijed (stariji
// program); takav kod napadač u sredini može namjestiti, pa se ne uparuje
var ErrStaroUparivanje = errors.New("druga strana ima stariji program koji uparuje bez zaštite koda — nadogradite ga, pa uparite ponovno")

// ErrObveza: druga strana je otkrila broj koji ne odgovara njezinoj obvezi
var ErrObveza = errors.New("druga strana nije poštovala obvezu pri dogovoru koda — uparivanje prekinuto")

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
			res, err := completeHandshake(a.conn.(*tls.Conn), priv, id, false)
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
	return completeHandshake(conn.(*tls.Conn), priv, id, true)
}

// completeHandshake: pozivatelj je strana koja je nazvala (Dial); ona se u
// dogovoru koda obvezuje prva
func completeHandshake(conn *tls.Conn, priv ed25519.PrivateKey, id Identity, pozivatelj bool) (*PairResult, error) {
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
	citac := &ograniceniCitac{r: conn}
	dec := json.NewDecoder(citac)
	peer, err := razmijeniHello(enc, dec, citac, id)
	if err != nil {
		conn.Close()
		return nil, err
	}
	nP, nS, err := dogovoriKod(enc, dec, citac, id.Protocol, pozivatelj)
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return &PairResult{
		Peer:    peer,
		PeerKey: peerKey,
		SAS:     SASCode(id.Protocol, priv.Public().(ed25519.PublicKey), peerKey, nP, nS),
		conn:    conn,
		dec:     dec,
		citac:   citac,
	}, nil
}

// razmijeniHello pošalje Hello ovog uređaja i pročita Hello druge strane:
// istog protokola i s dogovorom koda koji ovaj program traži
func razmijeniHello(enc *json.Encoder, dec *json.Decoder, citac *ograniceniCitac, id Identity) (Hello, error) {
	var peer Hello
	if err := enc.Encode(id.hello()); err != nil {
		return peer, err
	}
	// Hello se čita prije ikakvog povjerenja (i Dial ga čita od slušalice
	// koju je zadao korisnik), pa je ograđen: pravi je manji od kilobajta.
	citac.postavi(najveciHello)
	if err := dec.Decode(&peer); err != nil {
		// EOF here means the connection was accepted and then closed
		// without a word, which in practice means one thing: the other
		// machine is not waiting to pair. Saying "reading the peer's
		// hello: EOF" leaves somebody reading a protocol detail and
		// guessing at the remedy, when the remedy is a sentence.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return peer, fmt.Errorf("that machine answered but is not waiting to pair — start pairing there first, then dial it from here while it waits")
		}
		return peer, fmt.Errorf("reading the peer's hello: %w", err)
	}
	if peer.Protocol != id.Protocol {
		return peer, fmt.Errorf("%w: it says %q, this device speaks %q", ErrWrongProtocol, peer.Protocol, id.Protocol)
	}
	if peer.SAS < sasInacica {
		return peer, ErrStaroUparivanje
	}
	return peer, nil
}

// sasPoruka je korak dogovora koda: obveza (sažetak broja) ili sam broj
type sasPoruka struct {
	Obveza []byte `json:"obveza,omitempty"`
	Broj   []byte `json:"broj,omitempty"`
}

const (
	velicinaBroja    = 32
	najvecaSASPoruka = 4 << 10
)

func obvezaZa(protocol string, broj []byte) []byte {
	h := sha256.Sum256(append([]byte(protocol+"-pair-commit-v2|"), broj...))
	return h[:]
}

// dogovoriKod daje dva nasumična broja iz kojih se, uz oba ključa, računa
// kod uparivanja. Pozivatelj se obveže na svoj broj (pošalje sažetak),
// slušalica pošalje svoj, tek onda pozivatelj otkrije svoj, a slušalica
// provjeri obvezu. Napadač u sredini zato ne može tražiti ključ koji daje
// isti kod na oba ekrana: na svakoj strani jedan od brojeva stiže od poštene
// strane tek nakon što se on sam obvezao, pa mu je kod slučajan (1 : 1 000 000).
// Vraća broj pozivatelja i broj slušalice.
func dogovoriKod(enc *json.Encoder, dec *json.Decoder, citac *ograniceniCitac, protocol string, pozivatelj bool) (nP, nS []byte, err error) {
	moj := make([]byte, velicinaBroja)
	_, _ = rand.Read(moj) // od Go 1.24 ne vraća grešku
	s := sasRazgovor{enc: enc, dec: dec, citac: citac, protocol: protocol}
	if pozivatelj {
		tudji, err := s.kaoPozivatelj(moj)
		return moj, tudji, err
	}
	tudji, err := s.kaoSlusalica(moj)
	return tudji, moj, err
}

// sasRazgovor su poruke dogovora koda na jednoj vezi
type sasRazgovor struct {
	enc      *json.Encoder
	dec      *json.Decoder
	citac    *ograniceniCitac
	protocol string
}

func (s sasRazgovor) procitaj() (sasPoruka, error) {
	var m sasPoruka
	s.citac.postavi(najvecaSASPoruka)
	if err := s.dec.Decode(&m); err != nil {
		return m, fmt.Errorf("dogovor koda uparivanja: %w", err)
	}
	return m, nil
}

// kaoPozivatelj: obveza na moj broj, broj slušalice, otkrivanje mog broja.
// Vraća broj slušalice.
func (s sasRazgovor) kaoPozivatelj(moj []byte) ([]byte, error) {
	if err := s.enc.Encode(sasPoruka{Obveza: obvezaZa(s.protocol, moj)}); err != nil {
		return nil, err
	}
	m, err := s.procitaj()
	if err != nil {
		return nil, err
	}
	if len(m.Broj) != velicinaBroja {
		return nil, ErrObveza
	}
	return m.Broj, s.enc.Encode(sasPoruka{Broj: moj})
}

// kaoSlusalica: obveza pozivatelja, moj broj, otkriveni broj pozivatelja
// koji mora odgovarati obvezi. Vraća broj pozivatelja.
func (s sasRazgovor) kaoSlusalica(moj []byte) ([]byte, error) {
	obveza, err := s.procitaj()
	if err != nil {
		return nil, err
	}
	if len(obveza.Obveza) != sha256.Size {
		return nil, ErrObveza
	}
	if err := s.enc.Encode(sasPoruka{Broj: moj}); err != nil {
		return nil, err
	}
	otkriven, err := s.procitaj()
	if err != nil {
		return nil, err
	}
	if len(otkriven.Broj) != velicinaBroja || !bytes.Equal(obvezaZa(s.protocol, otkriven.Broj), obveza.Obveza) {
		return nil, ErrObveza
	}
	return otkriven.Broj, nil
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
	// potvrda nosi samo potvrdnicu članstva i ključ mreže
	var theirs Confirm
	r.citac.postavi(najvecaPotvrda)
	if err := r.dec.Decode(&theirs); err != nil {
		return false, nil, fmt.Errorf("the peer went away before confirming: %w", err)
	}
	if !approved || !theirs.Approved {
		return false, nil, nil
	}
	return true, theirs.Payload, nil
}
