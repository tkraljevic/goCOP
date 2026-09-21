package razmjena

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Discovery is a phonebook, never a source of trust: a UDP beacon that
// lets devices find each other's CURRENT addresses on the local network,
// because laptops move and nobody should be typing IPs. Everything a
// beacon says is an unverified claim — pairing still proves keys with
// the six digits, and every exchange still pins them. A liar on the LAN
// can waste a connection attempt; it cannot become trusted.

// Beacon is what an announcing device claims about itself.
type Beacon struct {
	Protocol     string `json:"protocol"`
	DeviceID     string `json:"deviceId"`
	Name         string `json:"name"`
	ExchangePort int    `json:"exchangePort"`
	// PairPort is set while the device is waiting for a pairing — the
	// window in which a pairing attempt with no address can find it.
	PairPort int               `json:"pairPort,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// Found is one discovered device: the beacon plus where it answered from.
type Found struct {
	Beacon
	Addr string `json:"addr"` // IP only; combine with the beacon's ports
}

func probeText(protocol string) string { return protocol + "-discover-v1" }

// Announce answers discovery probes of this protocol on the given UDP port
// until ctx ends. Probes of other protocols are ignored, so two
// applications can share a network without answering each other.
// Multiple announcers on one machine (a pairing ceremony beside a standing
// server) each bind their own port in tests; in production the standing
// server owns the port and the pairing ceremony updates what it says.
func Announce(ctx context.Context, protocol string, port int, info func() Beacon) error {
	conn, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	want := probeText(protocol)
	buf := make([]byte, 512)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if strings.TrimSpace(string(buf[:n])) != want {
			continue
		}
		b := info()
		b.Protocol = protocol
		out, err := json.Marshal(b)
		if err != nil {
			continue
		}
		_, _ = conn.WriteTo(out, from)
	}
}

// Discover probes the local network and collects whoever answers within
// the timeout. Deduplicated by device id; the address is where the reply
// actually came from, which is the one fact a beacon cannot fake.
func Discover(protocol string, timeout time.Duration, port int) ([]Found, error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	probe := []byte(probeText(protocol))
	for _, dst := range probeTargets(port) {
		_, _ = conn.WriteTo(probe, dst)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	seen := map[string]bool{}
	var out []Found
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			break // deadline is the normal exit
		}
		var b Beacon
		if json.Unmarshal(buf[:n], &b) != nil || b.DeviceID == "" || b.Protocol != protocol || seen[b.DeviceID] {
			continue
		}
		seen[b.DeviceID] = true
		host, _, err := net.SplitHostPort(from.String())
		if err != nil {
			continue
		}
		out = append(out, Found{Beacon: b, Addr: host})
	}
	return out, nil
}

// probeTargets is every place worth shouting: the limited broadcast, each
// interface's directed broadcast, and localhost — the last one for the
// two-instances-on-one-machine rig everything here was developed against.
func probeTargets(port int) []net.Addr {
	var out []net.Addr
	add := func(ip net.IP) {
		out = append(out, &net.UDPAddr{IP: ip, Port: port})
	}
	add(net.IPv4bcast)
	add(net.IPv4(127, 0, 0, 1))
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			ip := ipn.IP.To4()
			mask := ipn.Mask
			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip[i] | ^mask[i]
			}
			add(bcast)
		}
	}
	return out
}

// FindDevice is Discover narrowed to one device id — the auto-sync path:
// the stored address failed or is missing, so ask the network where the
// device is NOW.
func FindDevice(protocol, deviceID string, timeout time.Duration, port int) (Found, bool) {
	found, err := Discover(protocol, timeout, port)
	if err != nil {
		return Found{}, false
	}
	for _, f := range found {
		if f.DeviceID == deviceID {
			return f, true
		}
	}
	return Found{}, false
}

// The pairing flag is how a pairing ceremony and a standing server share
// one voice: the server holds the discovery socket for as long as it
// runs, so a separate pairing process cannot announce itself — found in
// the field the first time a machine with a running server tried to pair.
// The ceremony writes its port to a file in dir; the standing beacon reads
// it on every probe and answers for both. Removed when the ceremony ends.

func pairingFlagPath(dir string) string { return filepath.Join(dir, "pairing-port") }

// WritePairingFlag and ClearPairingFlag bracket the ceremony.
func WritePairingFlag(dir string, port int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(pairingFlagPath(dir), []byte(strconv.Itoa(port)), 0o644)
}

func ClearPairingFlag(dir string) {
	_ = os.Remove(pairingFlagPath(dir))
}

// ReadPairingFlag reports the ceremony's port, if one is waiting — and
// checks that one really is. A listener killed mid-ceremony left the file
// behind, and from then on every other machine's scan showed this device
// as "waiting to pair" on a port nobody answered. The file
// is a claim; the socket is the fact. A flag naming a port nothing
// listens on is removed here, by whoever reads it first.
func ReadPairingFlag(dir string) (int, bool) {
	p := pairingFlagPath(dir)
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return 0, false
	}
	// The ceremony writes the flag a few milliseconds before its socket
	// is bound. A flag that young is taken at its word; a killed listener
	// leaves its flag behind for hours, not milliseconds.
	if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) < flagGrace {
		return n, true
	}
	if !portAnswers(n) {
		_ = os.Remove(p)
		return 0, false
	}
	return n, true
}

// flagGrace is how long a freshly written pairing flag is believed
// without checking the socket — the ceremony's own bind time, generously.
const flagGrace = 3 * time.Second

// portAnswers is the cheapest possible "is anyone there": a TCP connect
// to loopback, closed at once. The pairing listener accepts and waits
// for a TLS hello it never gets; a connect that is closed unread costs
// it nothing and pairs with nobody.
func portAnswers(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}
