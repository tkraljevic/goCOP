package razmjena

// Pronalaženje u lokalnoj mreži nakon LAN1 (plan WEB synca): filtar
// sučelja, proba u tri navrata koja završi čim se jave tražena računala,
// ograda glasnika i glasnik koji grešku vrati pozivatelju. Probe i glasnik
// rade na simnetu, u mjehuru (uMjehuru), pa se rokovi mjere probnim satom.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/testutils/simnet"
)

func ipMreza(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	ip, n, err := net.ParseCIDR(cidr)
	bezGreske(t, err)
	n.IP = ip
	return n
}

// Proba ne ide na mostove spremnika i virtualnih strojeva, VPN tunele ni
// Tailscale, ni na isključeno sučelje; uključeno sučelje bez broadcasta
// (petlja) ne daje usmjereni broadcast, a ni adresa koja nije IPv4 mreža.
func TestCiljeviProbeBezIzbacenihSucelja(t *testing.T) {
	gore := net.FlagUp | net.FlagBroadcast
	maska16 := ipMreza(t, "192.168.5.20/24")
	maska16.Mask = net.CIDRMask(120, 128)
	sva := []sucelje{
		{ime: "en0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "192.168.1.10/24"), ipMreza(t, "fe80::1/64"), &net.IPAddr{IP: net.IPv4(192, 168, 9, 9)}}},
		{ime: "eth1", zastavice: gore, adrese: []net.Addr{maska16, &net.IPNet{IP: net.IPv4(10, 1, 1, 1), Mask: net.IPMask{255, 255}}}},
		{ime: "docker0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "172.17.0.1/16")}},
		{ime: "br-3f2a", zastavice: gore, adrese: []net.Addr{ipMreza(t, "172.18.0.1/16")}},
		{ime: "veth12", zastavice: gore, adrese: []net.Addr{ipMreza(t, "172.19.0.1/16")}},
		{ime: "virbr0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "192.168.122.1/24")}},
		{ime: "utun3", zastavice: gore, adrese: []net.Addr{ipMreza(t, "100.101.0.9/32")}},
		{ime: "tailscale0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "100.101.0.10/32")}},
		{ime: "vEthernet (WSL)", zastavice: gore, adrese: []net.Addr{ipMreza(t, "172.24.0.1/20")}},
		{ime: "bridge100", zastavice: gore, adrese: []net.Addr{ipMreza(t, "192.168.64.1/24")}},
		{ime: "en5", zastavice: net.FlagBroadcast, adrese: []net.Addr{ipMreza(t, "192.168.2.10/24")}},
		{ime: "lo0", zastavice: net.FlagUp | net.FlagLoopback, adrese: []net.Addr{ipMreza(t, "127.0.0.1/8")}},
	}
	var got []string
	for _, a := range ciljeviProbe(4712, sva) {
		got = append(got, a.String())
	}
	want := []string{"255.255.255.255:4712", "127.0.0.1:4712", "192.168.1.255:4712", "192.168.5.255:4712"}
	if !slices.Equal(got, want) {
		t.Errorf("ciljevi probe %v, želim %v", got, want)
	}
	if len(procitajSucelja()) == 0 || len(vlastiteMreze()) == 0 {
		t.Error("računalo bez sučelja i mreža, a barem petlja postoji")
	}
}

// odgovornik glumi glasnika na utičnici: na svaku probu protokola odgovori
// beaconom, osim na prvih propusti
func odgovornik(t *testing.T, conn net.PacketConn, b Beacon, propusti int) {
	t.Helper()
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if string(buf[:n]) != probeText(b.Protocol) {
				continue
			}
			if propusti > 0 {
				propusti--
				continue
			}
			out, _ := json.Marshal(b)      // Beacon ne pada
			_, _ = conn.WriteTo(out, from) // test vidi izostanak odgovora
		}
	}()
}

// simnetPronalazenja daje dvije UDP utičnice na simnetu, 5 ms u svakom smjeru
func simnetPronalazenja(t *testing.T) (a, b net.PacketConn) {
	t.Helper()
	n := &simnet.Simnet{Router: &simnet.PerfectRouter{}}
	veza := simnet.NodeBiDiLinkSettings{Latency: 5 * time.Millisecond}
	a = n.NewEndpoint(&net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 50000}, veza)
	b = n.NewEndpoint(&net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 4712}, veza)
	bezGreske(t, n.Start())
	t.Cleanup(func() {
		a.Close()
		b.Close()
		n.Close()
	})
	return a, b
}

// Prva proba se izgubi, a računalo se ipak nađe drugom; traženo računalo
// završi pronalaženje odmah, a bez traženih se čeka cijeli rok.
func TestProbaUTriNavrata(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		a, b := simnetPronalazenja(t)
		odgovornik(t, b, Beacon{Protocol: "gocop-proba", DeviceID: "laptop-baranja", ExchangePort: 4711}, 1)
		ciljevi := []net.Addr{b.LocalAddr()}
		pocetak := time.Now()
		nadjeni := pronadjiNa(a, "gocop-proba", 700*time.Millisecond, ciljevi, []string{"laptop-baranja"})
		trajalo := time.Since(pocetak)
		if len(nadjeni) != 1 || nadjeni[0].Addr != "10.0.0.2" || nadjeni[0].ExchangePort != 4711 {
			t.Fatalf("nađeni %+v", nadjeni)
		}
		if trajalo < 150*time.Millisecond || trajalo > 200*time.Millisecond {
			t.Errorf("trajalo %v, želim drugu probu (150 ms) i jedan obilazak", trajalo)
		}

		pocetak = time.Now()
		if nadjeni := pronadjiNa(a, "gocop-proba", 700*time.Millisecond, ciljevi, nil); len(nadjeni) != 1 || time.Since(pocetak) != 700*time.Millisecond {
			t.Errorf("bez traženih: %+v nakon %v", nadjeni, time.Since(pocetak))
		}
	})
}

// adresaBezPorta je adresa koja nije host:port
type adresaBezPorta struct{}

func (adresaBezPorta) Network() string { return "udp" }
func (adresaBezPorta) String() string  { return "nije-adresa" }

// Zbirka uzima svako računalo jednom, samo valjan beacon istog protokola, s
// adresom s koje je odgovor stigao.
func TestZbirkaOdgovora(t *testing.T) {
	z := novaZbirka("gocop-proba", []string{"laptop-baranja", "ured-vukovar"})
	s := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 4712}
	z.dodaj([]byte("nije beacon"), s)
	z.dodaj(beacon(t, Beacon{Protocol: "drugi-program", DeviceID: "laptop-baranja"}), s)
	z.dodaj(beacon(t, Beacon{Protocol: "gocop-proba"}), s)
	z.dodaj(beacon(t, Beacon{Protocol: "gocop-proba", DeviceID: "ured-vukovar"}), adresaBezPorta{})
	z.dodaj(beacon(t, Beacon{Protocol: "gocop-proba", DeviceID: "laptop-baranja", Name: "prvi"}), s)
	z.dodaj(beacon(t, Beacon{Protocol: "gocop-proba", DeviceID: "laptop-baranja", Name: "drugi"}), s)
	if len(z.nadjeni) != 1 || z.nadjeni[0].Name != "prvi" || z.sviNadjeni() {
		t.Fatalf("nađeni %+v, svi %v", z.nadjeni, z.sviNadjeni())
	}
	z.dodaj(beacon(t, Beacon{Protocol: "gocop-proba", DeviceID: "ured-vukovar"}), s)
	if !z.sviNadjeni() || novaZbirka("gocop-proba", nil).sviNadjeni() {
		t.Error("svi traženi su se javili, a bez traženih pronalaženje ide do roka")
	}
}

// Glasnik odgovara izvoru iz vlastite podmreže i ne odgovara tuđoj; kraj
// ctx ga završi bez greške.
func TestGlasnikSamoVlastitojPodmrezi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		for _, sl := range []struct {
			mreza    string
			odgovori bool
		}{{"10.0.0.0/24", true}, {"192.168.1.0/24", false}} {
			a, b := simnetPronalazenja(t)
			ctx, otkazi := context.WithCancel(t.Context())
			kraj := make(chan error, 1)
			mreze := func() []*net.IPNet { return []*net.IPNet{ipMreza(t, sl.mreza)} }
			go func() {
				kraj <- glasnik(ctx, b, "gocop-proba", func() Beacon { return Beacon{DeviceID: "ured-vukovar"} }, novaOgradaGlasnika(mreze))
			}()
			nadjeni := pronadjiNa(a, "gocop-proba", 700*time.Millisecond, []net.Addr{b.LocalAddr()}, []string{"ured-vukovar"})
			if (len(nadjeni) == 1) != sl.odgovori || (sl.odgovori && nadjeni[0].Protocol != "gocop-proba") {
				t.Errorf("podmreža %s: nađeni %+v", sl.mreza, nadjeni)
			}
			otkazi()
			if err := <-kraj; err != nil {
				t.Errorf("glasnik nakon kraja: %v", err)
			}
		}
	})
}

// lazniPaketi je utičnica koja čitanjem vraća zadane pakete pa grešku, a
// slanje vraća grešku slanja
type lazniPaketi struct {
	net.PacketConn
	paketi       []string
	greskaSlanja error
}

func (l *lazniPaketi) ReadFrom(p []byte) (int, net.Addr, error) {
	if len(l.paketi) == 0 {
		return 0, nil, errors.New("čitanje je palo")
	}
	n := copy(p, l.paketi[0])
	l.paketi = l.paketi[1:]
	return n, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50000}, nil
}

func (l *lazniPaketi) WriteTo([]byte, net.Addr) (int, error) { return 0, l.greskaSlanja }
func (l *lazniPaketi) Close() error                          { return nil }

// Greška čitanja i greška slanja završe glasnika i vrate se pozivatelju.
func TestGlasnikVracaGresku(t *testing.T) {
	info := func() Beacon { return Beacon{DeviceID: "ured-vukovar"} }
	o := novaOgradaGlasnika(func() []*net.IPNet { return nil })
	if err := glasnik(t.Context(), &lazniPaketi{paketi: []string{"tuđa proba"}}, "gocop-proba", info, o); err == nil || !strings.Contains(err.Error(), "čitanje") {
		t.Errorf("greška čitanja: %v", err)
	}
	slanje := errors.New("slanje je palo")
	if err := glasnik(t.Context(), &lazniPaketi{paketi: []string{"gocop-proba-discover-v1"}, greskaSlanja: slanje}, "gocop-proba", info, o); !errors.Is(err, slanje) {
		t.Errorf("greška slanja: %v", err)
	}
}

// Ograda glasnika: petlja i link-local uvijek, vlastita podmreža da, tuđa i
// adresa koja nije UDP ne; najviše 2 odgovora po izvoru i 20 ukupno u
// sekundi, pa nova sekunda kreće ispočetka. Podmreže se čitaju najviše svakih
// 30 s.
func TestOgradaGlasnika(t *testing.T) {
	citanja := 0
	o := novaOgradaGlasnika(func() []*net.IPNet {
		citanja++
		return []*net.IPNet{ipMreza(t, "192.168.1.0/24")}
	})
	sada := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	izvor := func(ip string) net.Addr { return &net.UDPAddr{IP: net.ParseIP(ip), Port: 50000} }
	for ip, smije := range map[string]bool{"127.0.0.1": true, "169.254.10.1": true, "192.168.1.50": true, "192.168.2.50": false, "8.8.8.8": false} {
		if o.pusti(izvor(ip), sada) != smije {
			t.Errorf("%s: smije %v", ip, !smije)
		}
	}
	if o.pusti(&net.IPAddr{IP: net.IPv4(192, 168, 1, 7)}, sada) {
		t.Error("adresa koja nije UDP")
	}
	if !o.pusti(izvor("192.168.1.50"), sada) || o.pusti(izvor("192.168.1.50"), sada) {
		t.Error("drugi odgovor istom izvoru smije, treći ne")
	}
	pusteno := 4 // 127.0.0.1, 169.254.10.1 i dva puta 192.168.1.50
	for i := 1; i <= 30; i++ {
		if o.pusti(izvor("192.168.1."+itoa(100+i)), sada) {
			pusteno++
		}
	}
	if pusteno != najviseOdgovora {
		t.Errorf("u sekundi pušteno %d, želim %d", pusteno, najviseOdgovora)
	}
	if !o.pusti(izvor("192.168.1.50"), sada.Add(time.Second)) {
		t.Error("nova sekunda nije krenula ispočetka")
	}
	if citanja != 1 {
		t.Errorf("podmreže pročitane %d puta u sekundi", citanja)
	}
	o.pusti(izvor("192.168.1.60"), sada.Add(osvjeziMrezeNakon+time.Second))
	if citanja != 2 {
		t.Errorf("podmreže nakon 30 s pročitane %d puta", citanja)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n) // broj
	return string(b)
}

// Kad se utičnica za probu ne da otvoriti, pronalaženje javi grešku, a
// traženo računalo nije nađeno.
func TestProbaBezUticnice(t *testing.T) {
	staro := otvoriProbu
	t.Cleanup(func() { otvoriProbu = staro })
	otvoriProbu = func() (net.PacketConn, error) { return nil, errors.New("nema slobodnog porta") }
	if _, err := Discover("gocop-proba", time.Second, 4712); err == nil {
		t.Error("pronalaženje bez utičnice bez greške")
	}
	if _, ok := FindDevice("gocop-proba", "laptop-baranja", time.Second, 4712); ok {
		t.Error("nađeno bez utičnice")
	}
}
