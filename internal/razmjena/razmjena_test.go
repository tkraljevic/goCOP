package razmjena

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func TestKeyFileRoundTripsAndIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "device-key")
	first, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Error("loading the key file must return the same key that was minted")
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %o, want 0600", st.Mode().Perm())
	}
	if _, err := ParsePublicKey(PublicKeyString(first.Public().(ed25519.PublicKey))); err != nil {
		t.Errorf("public key does not round-trip through its string form: %v", err)
	}
}

func TestAFileThatIsNotAKeyIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-key")
	os.WriteFile(path, []byte("not a key"), 0o600)
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Error("a file that is not a PEM private key must be refused, not replaced")
	}
}

// A full pairing over a real TLS connection on loopback: both sides see
// the same six digits, both approve, and each learns the other's proven
// key — the identity a later exchange will pin.
func TestPairingAgreesOnTheCodeAndTheKeys(t *testing.T) {
	a, b := newKey(t), newKey(t)
	port := freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type outcome struct {
		res *PairResult
		err error
	}
	listened := make(chan outcome, 1)
	go func() {
		res, err := Listen(ctx, a, Identity{Protocol: "testproto", DeviceID: "A"}, port)
		listened <- outcome{res, err}
	}()

	var dialled *PairResult
	var err error
	for i := 0; i < 50; i++ {
		dialled, err = Dial(ctx, b, Identity{Protocol: "testproto", DeviceID: "B"}, fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	got := <-listened
	if got.err != nil {
		t.Fatalf("listen: %v", got.err)
	}

	if got.res.SAS != dialled.SAS {
		t.Errorf("the two screens show different codes: %s vs %s", got.res.SAS, dialled.SAS)
	}
	if !got.res.PeerKey.Equal(b.Public().(ed25519.PublicKey)) || !dialled.PeerKey.Equal(a.Public().(ed25519.PublicKey)) {
		t.Error("each side must learn the other's proven key")
	}
	if got.res.Peer.DeviceID != "B" || dialled.Peer.DeviceID != "A" {
		t.Error("hellos were not exchanged")
	}

	// approval carries a payload each way — the application's welcome pack
	type pack struct{ Note string }
	okListen := make(chan bool, 1)
	fromDial := make(chan json.RawMessage, 1)
	go func() {
		ok, theirs, _ := got.res.Finish(true, pack{"from A"})
		okListen <- ok
		fromDial <- theirs
	}()
	okDial, fromListen, err := dialled.Finish(true, pack{"from B"})
	if err != nil || !okDial || !<-okListen {
		t.Errorf("both approved, pairing must succeed (dial=%v err=%v)", okDial, err)
	}
	var packA, packB pack
	json.Unmarshal(fromListen, &packA)
	json.Unmarshal(<-fromDial, &packB)
	if packA.Note != "from A" || packB.Note != "from B" {
		t.Errorf("payloads did not cross: listener got %q, dialler got %q", packB.Note, packA.Note)
	}
}

// A refusal on either side fails the pairing and hands over nothing
func TestARefusalHandsOverNothing(t *testing.T) {
	a, b := newKey(t), newKey(t)
	port := freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type outcome struct {
		res *PairResult
		err error
	}
	listened := make(chan outcome, 1)
	go func() {
		res, err := Listen(ctx, a, Identity{Protocol: "testproto", DeviceID: "A"}, port)
		listened <- outcome{res, err}
	}()
	var dialled *PairResult
	var err error
	for i := 0; i < 50; i++ {
		dialled, err = Dial(ctx, b, Identity{Protocol: "testproto", DeviceID: "B"}, fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	got := <-listened

	go got.res.Finish(false, map[string]string{"secret": "never"})
	ok, theirs, err := dialled.Finish(true, map[string]string{"x": "y"})
	if err != nil || ok || len(theirs) != 0 {
		t.Errorf("a refused pairing must fail with no payload: ok=%v payload=%q err=%v", ok, theirs, err)
	}
}

// Devices of different applications never pair, and would not even show
// matching codes if they did.
func TestDevicesOfAnotherProtocolAreRefused(t *testing.T) {
	a, b := newKey(t), newKey(t)
	port := freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go Listen(ctx, a, Identity{Protocol: "app-one", DeviceID: "A"}, port)

	// keep dialling until the listener is up and gives its verdict
	var err error
	for i := 0; i < 50; i++ {
		_, err = Dial(ctx, b, Identity{Protocol: "app-two", DeviceID: "B"}, fmt.Sprintf("127.0.0.1:%d", port))
		if errors.Is(err, ErrWrongProtocol) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !errors.Is(err, ErrWrongProtocol) {
		t.Errorf("want ErrWrongProtocol, got %v", err)
	}

	pa, pb := a.Public().(ed25519.PublicKey), b.Public().(ed25519.PublicKey)
	if SASCode("app-one", pa, pb) == SASCode("app-two", pa, pb) {
		t.Error("the pairing code must depend on the protocol name")
	}
	if SASCode("app-one", pa, pb) != SASCode("app-one", pb, pa) {
		t.Error("the pairing code must not depend on which side computes it")
	}
}

// An exchange is only opened for a key the owner confirmed; a stranger
// with a valid key of its own is dropped at the door, and a dialler that
// meets a different key than the one paired refuses to talk.
func TestExchangePinsTheKey(t *testing.T) {
	server, paired, stranger := newKey(t), newKey(t), newKey(t)
	port := freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pairedPub := paired.Public().(ed25519.PublicKey)
	served := make(chan string, 4)
	go ServeExchange(ctx, server, "testproto", port,
		func(k ed25519.PublicKey) bool { return k.Equal(pairedPub) },
		func(c *Conn) {
			defer c.Close()
			e, err := c.Receive()
			if err != nil {
				return
			}
			var body struct{ Text string }
			e.Decode(&body)
			served <- body.Text
			reply, _ := NewEnvelope("ack", map[string]string{"got": body.Text})
			c.Send(reply)
		})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	serverPub := server.Public().(ed25519.PublicKey)

	var conn *Conn
	var err error
	for i := 0; i < 50; i++ {
		conn, err = DialExchange(ctx, paired, "testproto", addr, serverPub)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("paired device could not dial: %v", err)
	}
	env, _ := NewEnvelope("hello", map[string]string{"Text": "from paired"})
	if err := conn.Send(env); err != nil {
		t.Fatal(err)
	}
	reply, err := conn.Receive()
	if err != nil || reply.Kind != "ack" {
		t.Fatalf("no ack: kind=%q err=%v", reply.Kind, err)
	}
	conn.Close()
	if got := <-served; got != "from paired" {
		t.Errorf("server saw %q", got)
	}

	// a stranger connects, is dropped, and the handler never runs
	sc, err := DialExchange(ctx, stranger, "testproto", addr, serverPub)
	if err == nil {
		sc.Send(env)
		if _, err := sc.Receive(); err == nil {
			t.Error("a stranger must not get an answer")
		}
		sc.Close()
	}
	select {
	case got := <-served:
		t.Errorf("the handler ran for a stranger: %q", got)
	case <-time.After(300 * time.Millisecond):
	}

	// expecting the wrong key refuses before sending anything
	if _, err := DialExchange(ctx, paired, "testproto", addr, pairedPub); err == nil {
		t.Error("a server proving a different key than expected must be refused")
	}
}

// ServeExchange must FAIL when the port is taken, rather than block or
// pretend. The caller's retry loop depends on getting an error back
// promptly; one that hung would leave the same silent outage the loop
// was written to end.
func TestServeExchangeReportsATakenPort(t *testing.T) {
	port := freePort(t)
	holder, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Skip("cannot hold a port on this host")
	}
	defer holder.Close()

	done := make(chan error, 1)
	go func() {
		done <- ServeExchange(context.Background(), newKey(t), "testproto", port, func(ed25519.PublicKey) bool { return true }, func(*Conn) {})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("serving on a taken port must return an error")
		}
	case <-time.After(3 * time.Second):
		t.Error("ServeExchange hung on a taken port instead of failing")
	}
}

// Discovery over loopback: the announcer answers only its own protocol,
// and the address reported is where the answer came from.
func TestDiscoveryAnswersOnlyItsOwnProtocol(t *testing.T) {
	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Announce(ctx, "testproto", port, func() Beacon {
		return Beacon{DeviceID: "dev-1", Name: "laptop", ExchangePort: 4610}
	})
	time.Sleep(100 * time.Millisecond)

	found, err := Discover("testproto", 500*time.Millisecond, port)
	if err != nil {
		t.Fatal(err)
	}
	var hit *Found
	for i := range found {
		if found[i].DeviceID == "dev-1" {
			hit = &found[i]
		}
	}
	if hit == nil {
		t.Fatalf("announcer not found; got %+v", found)
	}
	if hit.Protocol != "testproto" || hit.ExchangePort != 4610 || hit.Addr == "" {
		t.Errorf("beacon came back wrong: %+v", *hit)
	}

	other, _ := Discover("some-other-app", 300*time.Millisecond, port)
	for _, f := range other {
		if f.DeviceID == "dev-1" {
			t.Error("an announcer must not answer probes of another protocol")
		}
	}
}

// A pairing flag naming a port nothing listens on is a leftover — a
// listener that was killed — and is dropped by whoever reads it.
func TestAStalePairingFlagIsDroppedOnRead(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	if err := WritePairingFlag(dir, port); err != nil {
		t.Fatal(err)
	}
	// age the flag past the grace period
	old := time.Now().Add(-time.Minute)
	os.Chtimes(pairingFlagPath(dir), old, old)

	if _, ok := ReadPairingFlag(dir); ok {
		t.Error("a flag naming a dead port must be dropped")
	}
	if _, err := os.Stat(pairingFlagPath(dir)); !os.IsNotExist(err) {
		t.Error("the stale flag file must be removed")
	}

	// a live listener keeps its flag
	ln, _ := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	defer ln.Close()
	WritePairingFlag(dir, port)
	os.Chtimes(pairingFlagPath(dir), old, old)
	if got, ok := ReadPairingFlag(dir); !ok || got != port {
		t.Errorf("a flag naming a live port must be honoured, got (%d, %v)", got, ok)
	}
	ClearPairingFlag(dir)
}
