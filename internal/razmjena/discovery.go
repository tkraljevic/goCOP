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
//
// Greška čitanja ili slanja završi glasnika i vrati se pozivatelju, koji ga
// ponovno pokrene (peers.Announce); prije je glasnik tada tiho stao.
func Announce(ctx context.Context, protocol string, port int, info func() Beacon) error {
	conn, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	return glasnik(ctx, conn, protocol, info, novaOgradaGlasnika(vlastiteMreze))
}

// glasnik odgovara na probe na utičnici conn dok ctx traje i zatvori je kad
// završi. Odgovor ide na adresu i port s kojih je proba stigla.
func glasnik(ctx context.Context, conn net.PacketConn, protocol string, info func() Beacon, o *ogradaGlasnika) error {
	defer conn.Close() //nolint:errcheck // glasnik je gotov; greška zatvaranja nema kome reći
	stani := context.AfterFunc(ctx, func() {
		_ = conn.Close() // zatvaranje prekine čitanje; greška zatvaranja nema kome reći
	})
	defer stani()
	want := probeText(protocol)
	buf := make([]byte, 512)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return greskaGlasnika(ctx, err)
		}
		if strings.TrimSpace(string(buf[:n])) != want || !o.pusti(from, time.Now()) {
			continue
		}
		b := info()
		b.Protocol = protocol
		out, _ := json.Marshal(b) // Beacon su nizovi i brojevi, pa Marshal ne pada
		if _, err := conn.WriteTo(out, from); err != nil {
			return greskaGlasnika(ctx, err)
		}
	}
}

// greskaGlasnika: greška nakon kraja ctx je uredan kraj glasnika
func greskaGlasnika(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// Ograda glasnika (plan WEB synca, LAN1): odgovara se samo izvorima iz
// vlastite podmreže, s petlje i iz link-locala, najviše 20 odgovora u
// sekundi ukupno i 2 po izvoru. Proba iz lokalne mreže ide tri puta, pa jedan
// izvor u sekundi traži najviše toliko; glasnik tako ne postaje pojačalo
// prometa prema lažnoj adresi.
const (
	najviseOdgovora         = 20
	najviseOdgovoraPoIzvoru = 2
	osvjeziMrezeNakon       = 30 * time.Second
)

type ogradaGlasnika struct {
	mreze     func() []*net.IPNet // vlastite podmreže
	vlastite  []*net.IPNet
	procitane time.Time
	prozor    time.Time
	ukupno    int
	poIzvoru  map[string]int
}

func novaOgradaGlasnika(mreze func() []*net.IPNet) *ogradaGlasnika {
	return &ogradaGlasnika{mreze: mreze, poIzvoru: map[string]int{}}
}

// pusti javlja smije li glasnik u trenutku sada odgovoriti izvoru from, i
// ako smije, odgovor broji
func (o *ogradaGlasnika) pusti(from net.Addr, sada time.Time) bool {
	ip := ipIzvora(from)
	if ip == nil || !o.izMreze(ip, sada) {
		return false
	}
	if sada.Sub(o.prozor) >= time.Second {
		o.prozor, o.ukupno = sada, 0
		clear(o.poIzvoru)
	}
	kljuc := ip.String()
	if o.ukupno >= najviseOdgovora || o.poIzvoru[kljuc] >= najviseOdgovoraPoIzvoru {
		return false
	}
	o.ukupno++
	o.poIzvoru[kljuc]++
	return true
}

// izMreze: petlja, link-local ili vlastita podmreža. Podmreže se čitaju
// najviše svakih 30 s, da proba iz tuđe mreže ne čita sučelja za svaki paket.
func (o *ogradaGlasnika) izMreze(ip net.IP, sada time.Time) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	if o.vlastite == nil || sada.Sub(o.procitane) >= osvjeziMrezeNakon {
		o.vlastite, o.procitane = o.mreze(), sada
	}
	for _, n := range o.vlastite {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ipIzvora je IP adresa izvora probe; nil za adresu koja nije UDP
func ipIzvora(a net.Addr) net.IP {
	if u, ok := a.(*net.UDPAddr); ok {
		return u.IP
	}
	return nil
}

// vlastiteMreze su IPv4 podmreže svih uključenih sučelja ovog računala, i
// onih koje filtar izbacuje: proba sa spremnika na istom računalu smije
// dobiti odgovor
func vlastiteMreze() []*net.IPNet {
	var out []*net.IPNet
	for _, s := range procitajSucelja() {
		for _, a := range s.adrese {
			if ipn, ok := a.(*net.IPNet); ok && s.zastavice&net.FlagUp != 0 {
				out = append(out, ipn)
			}
		}
	}
	return out
}

// razmaciProbe: proba ide tri puta, u 0, 150 i 400 ms od početka, jer se
// jedan UDP paket lako izgubi (plan WEB synca, LAN1)
var razmaciProbe = []time.Duration{0, 150 * time.Millisecond, 400 * time.Millisecond}

// Discover probes the local network and collects whoever answers within
// the timeout. Deduplicated by device id; the address is where the reply
// actually came from, which is the one fact a beacon cannot fake.
func Discover(protocol string, timeout time.Duration, port int) ([]Found, error) {
	return Pronadji(protocol, timeout, port, nil)
}

// otvoriProbu otvara utičnicu s koje proba ide; var, da test podmetne
// utičnicu koja se ne da otvoriti
var otvoriProbu = func() (net.PacketConn, error) { return net.ListenPacket("udp4", ":0") }

// Pronadji proba lokalnu mrežu kao Discover, ali završi čim se jave sva
// tražena računala (trazeni); bez traženih skuplja do isteka roka.
func Pronadji(protocol string, timeout time.Duration, port int, trazeni []string) ([]Found, error) {
	conn, err := otvoriProbu()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return pronadjiNa(conn, protocol, timeout, ciljeviProbe(port, procitajSucelja()), trazeni), nil
}

// pronadjiNa šalje probu na ciljeve s utičnice conn tri puta (razmaciProbe) i
// skuplja odgovore do isteka roka ili dok se ne jave sva tražena računala
func pronadjiNa(conn net.PacketConn, protocol string, timeout time.Duration, ciljevi []net.Addr, trazeni []string) []Found {
	gotovo := make(chan struct{})
	defer close(gotovo)
	go saljiProbe(conn, []byte(probeText(protocol)), ciljevi, gotovo)
	_ = conn.SetReadDeadline(time.Now().Add(timeout)) // bez roka čitanje stoji; istek je uobičajen kraj
	z := novaZbirka(protocol, trazeni)
	buf := make([]byte, 2048)
	for !z.sviNadjeni() {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			break // istek roka je uobičajen kraj
		}
		z.dodaj(buf[:n], from)
	}
	return z.nadjeni
}

// saljiProbe šalje probu na sve ciljeve u trenucima razmaciProbe, dok
// pronalaženje ne završi (gotovo)
func saljiProbe(conn net.PacketConn, proba []byte, ciljevi []net.Addr, gotovo <-chan struct{}) {
	pocetak := time.Now()
	for _, r := range razmaciProbe {
		select {
		case <-gotovo:
			return
		case <-time.After(time.Until(pocetak.Add(r))):
		}
		for _, dst := range ciljevi {
			_, _ = conn.WriteTo(proba, dst) // cilj do kojeg se ne da poslati jednostavno ne odgovori
		}
	}
}

// zbirka su odgovori na probu, po oznaci računala, i koja se računala traže
type zbirka struct {
	protocol string
	trazeni  []string
	vidjeni  map[string]bool
	nadjeni  []Found
}

func novaZbirka(protocol string, trazeni []string) *zbirka {
	return &zbirka{protocol: protocol, trazeni: trazeni, vidjeni: map[string]bool{}}
}

// dodaj prima jedan odgovor: valjan beacon istog protokola, svako računalo
// jednom, s adresom s koje je odgovor stigao
func (z *zbirka) dodaj(p []byte, from net.Addr) {
	var b Beacon
	if json.Unmarshal(p, &b) != nil || b.DeviceID == "" || b.Protocol != z.protocol || z.vidjeni[b.DeviceID] {
		return
	}
	host, _, err := net.SplitHostPort(from.String())
	if err != nil {
		return
	}
	z.vidjeni[b.DeviceID] = true
	z.nadjeni = append(z.nadjeni, Found{Beacon: b, Addr: host})
}

// sviNadjeni: javila su se sva tražena računala; bez traženih nikad
func (z *zbirka) sviNadjeni() bool {
	for _, id := range z.trazeni {
		if !z.vidjeni[id] {
			return false
		}
	}
	return len(z.trazeni) > 0
}

// ciljeviProbe su sva mjesta na koja proba ide: ograničeni broadcast, petlja
// (dva programa na istom računalu, u razvoju i testovima) i usmjereni
// broadcast svakog sučelja koje vodi u lokalnu mrežu (lokalnaSucelja)
func ciljeviProbe(port int, sva []sucelje) []net.Addr {
	out := []net.Addr{
		&net.UDPAddr{IP: net.IPv4bcast, Port: port},
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port},
	}
	for _, s := range lokalnaSucelja(sva) {
		if s.zastavice&net.FlagBroadcast == 0 {
			continue
		}
		for _, a := range s.adrese {
			if b := usmjereniBroadcast(a); b != nil {
				out = append(out, &net.UDPAddr{IP: b, Port: port})
			}
		}
	}
	return out
}

// FindDevice is Discover narrowed to one device id — the auto-sync path:
// the stored address failed or is missing, so ask the network where the
// device is NOW. Završi čim se traženo računalo javi.
func FindDevice(protocol, deviceID string, timeout time.Duration, port int) (Found, bool) {
	found, err := Pronadji(protocol, timeout, port, []string{deviceID})
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
