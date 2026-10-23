package razmjena

// Provjera mreže (netcheck.go, udp.go) na simnetu u mjehuru: dva lažna STUN
// poslužitelja odgovaraju javnom adresom koju test zada, pa se vide lak NAT,
// NAT ovisan o odredištu, jedan odgovor, blokiran UDP i ponavljanje upita.

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/testutils/simnet"
)

var (
	adresaRacunala  = &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 4713}
	adresaGoogle    = &net.UDPAddr{IP: net.ParseIP("74.125.250.129"), Port: 19302}
	adresaCloudflre = &net.UDPAddr{IP: net.ParseIP("162.159.207.0"), Port: 3478}
)

// odgovorNa je uspješan Binding odgovor na zahtjev s XOR-MAPPED-ADDRESS javna
func odgovorNa(zahtjev []byte, javna netip.AddrPort) []byte {
	id := idTransakcije(zahtjev)
	v := []byte{0, obiteljIPv4}
	v = binary.BigEndian.AppendUint16(v, javna.Port()^stunKolacic>>16)
	ip := javna.Addr().As4()
	maska := binary.BigEndian.AppendUint32(nil, stunKolacic)
	for i := range ip {
		ip[i] ^= maska[i]
	}
	return odgovorS(id, atribut(atrXorMapped, append(v, ip[:]...)...))
}

// lazniSTUN je STUN poslužitelj na simnetu: na n-ti zahtjev (od 0) odgovori
// ono što vrati odgovor (nil: ništa) nakon kasni
type lazniSTUN struct {
	mu      sync.Mutex
	primio  []time.Duration // kad je primio koji zahtjev, od početka
	pocetak time.Time
}

func (l *lazniSTUN) vrti(t *testing.T, c *simnet.SimConn, odgovor func(n int, zahtjev []byte) []byte, kasni func(n int) time.Duration) {
	b := make([]byte, 1500)
	for {
		n, od, err := c.ReadFrom(b)
		if err != nil {
			return
		}
		l.mu.Lock()
		redni := len(l.primio)
		l.primio = append(l.primio, time.Since(l.pocetak))
		l.mu.Unlock()
		zahtjev := append([]byte(nil), b[:n]...)
		if o := odgovor(redni, zahtjev); o != nil {
			go func() {
				time.Sleep(kasni(redni))
				_, _ = c.WriteTo(o, od) // simnet ne vraća grešku za poznatu adresu
			}()
		}
	}
}

// mrezaSTUN je UDP utičnica računala i dva lažna STUN poslužitelja na
// simnetu, 5 ms u svakom smjeru; nil odgovor znači da poslužitelja nema
func mrezaSTUN(t *testing.T, google, cloudflare func(n int, zahtjev []byte) []byte, kasni func(n int) time.Duration) (*UDP, *lazniSTUN, *lazniSTUN) {
	t.Helper()
	n := &simnet.Simnet{Router: &simnet.PerfectRouter{}}
	veza := simnet.NodeBiDiLinkSettings{Latency: 5 * time.Millisecond}
	u := NoviUDP(n.NewEndpoint(adresaRacunala, veza))
	lazni := [2]*lazniSTUN{{pocetak: time.Now()}, {pocetak: time.Now()}}
	var conns []*simnet.SimConn
	for i, p := range []struct {
		adresa  *net.UDPAddr
		odgovor func(int, []byte) []byte
	}{{adresaGoogle, google}, {adresaCloudflre, cloudflare}} {
		if p.odgovor != nil {
			c := n.NewEndpoint(p.adresa, veza)
			conns = append(conns, c)
			go lazni[i].vrti(t, c, p.odgovor, kasni)
		}
	}
	bezGreske(t, n.Start())
	t.Cleanup(func() {
		bezGreske(t, u.Close())
		for _, c := range conns {
			c.Close()
		}
		n.Close()
	})
	return u, lazni[0], lazni[1]
}

func uvijek(javna string) func(int, []byte) []byte {
	return func(_ int, z []byte) []byte { return odgovorNa(z, netip.MustParseAddrPort(javna)) }
}

func odmah(int) time.Duration { return 0 }

var posluzitelji = []PosluziteljSTUN{{Ime: "Google", Adresa: adresaGoogle}, {Ime: "Cloudflare", Adresa: adresaCloudflre}}

func TestProvjeraMreze(t *testing.T) {
	for _, c := range []struct {
		ime                string
		google, cloudflare func(int, []byte) []byte
		posluzitelji       []PosluziteljSTUN
		vrsta              string
		cuva               bool
		traje              time.Duration
	}{
		{"lak NAT koji čuva port", uvijek("198.51.100.7:4713"), uvijek("198.51.100.7:4713"), posluzitelji, NATLak, true, 10 * time.Millisecond},
		{"lak NAT koji mijenja port", uvijek("198.51.100.7:61000"), uvijek("198.51.100.7:61000"), posluzitelji, NATLak, false, 10 * time.Millisecond},
		{"NAT ovisan o odredištu", uvijek("198.51.100.7:61000"), uvijek("198.51.100.7:61001"), posluzitelji, NATOvisan, false, 10 * time.Millisecond},
		// port sačuvan samo prema jednom poslužitelju ne znači da NAT čuva
		// port, ma koji od njih bio prvi na popisu
		{"ovisan, port sačuvan prema prvom", uvijek("198.51.100.7:4713"), uvijek("198.51.100.7:4714"), posluzitelji, NATOvisan, false, 10 * time.Millisecond},
		{"ovisan, port sačuvan prema drugom", uvijek("198.51.100.7:4714"), uvijek("198.51.100.7:4713"), posluzitelji, NATOvisan, false, 10 * time.Millisecond},
		{"odgovorio samo jedan", uvijek("198.51.100.7:4713"), nil, posluzitelji, NATNepoznat, true, rokSTUN},
		{"nijedan nije odgovorio", nil, nil, posluzitelji, UDPBlokiran, false, rokSTUN},
		{"adrese se nisu dale razriješiti", nil, nil, []PosluziteljSTUN{{Ime: "Google"}, {Ime: "Cloudflare"}}, NATNepoznat, false, 0},
		{"jedan vraća grešku", func(_ int, z []byte) []byte {
			o := odgovorNa(z, netip.MustParseAddrPort("198.51.100.7:4713"))
			binary.BigEndian.PutUint16(o[0:2], 0x0111) // Binding Error Response
			return o
		}, uvijek("198.51.100.7:4713"), posluzitelji, NATNepoznat, true, rokSTUN},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				u, _, _ := mrezaSTUN(t, c.google, c.cloudflare, odmah)
				pocetak := time.Now()
				m := u.ProvjeriMrezu(t.Context(), c.posluzitelji)
				if m.Vrsta != c.vrsta || m.CuvaPort != c.cuva || m.Kad.IsZero() {
					t.Fatalf("mjerenje %+v", m)
				}
				for _, o := range m.Odgovori {
					if o.Odgovorio && o.RTT != 10*time.Millisecond {
						t.Errorf("%s: RTT %s", o.Ime, o.RTT)
					}
				}
				// s odgovorima provjera traje jedan obilazak, a bez njih točno rok
				if trajanje := time.Since(pocetak); trajanje != c.traje {
					t.Errorf("trajanje %s, želim %s", trajanje, c.traje)
				}
			})
		})
	}
}

// Upit ide u 0, 250 i 750 ms s novim ID-om; odgovor na prvi upit koji kasni
// vrijedi, a RTT se računa od tog upita.
func TestProvjeraMrezePonavljaUpit(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		tek := func(n int, z []byte) []byte {
			if n == 0 {
				return odgovorNa(z, netip.MustParseAddrPort("198.51.100.7:4713"))
			}
			return nil
		}
		kasni := func(int) time.Duration { return 900 * time.Millisecond }
		u, g, _ := mrezaSTUN(t, tek, nil, kasni)
		m := u.ProvjeriMrezu(t.Context(), posluzitelji[:1])
		if len(m.Odgovori) != 1 || !m.Odgovori[0].Odgovorio || m.Odgovori[0].RTT != 910*time.Millisecond {
			t.Fatalf("mjerenje %+v", m)
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		want := []time.Duration{5 * time.Millisecond, 255 * time.Millisecond, 755 * time.Millisecond}
		if len(g.primio) != 3 || g.primio[0] != want[0] || g.primio[1] != want[1] || g.primio[2] != want[2] {
			t.Errorf("upiti stigli u %v, želim %v", g.primio, want)
		}
	})
}

// Paketi koji nisu ničiji STUN odgovor broje se kao odbačeni.
func TestUDPOdbaceniPaketi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		u, _, _ := mrezaSTUN(t, uvijek("198.51.100.7:4713"), nil, odmah)
		tudji := odgovorNa(zahtjevBinding([12]byte{9}), netip.MustParseAddrPort("198.51.100.7:4713"))
		u.predaj(tudji, time.Now())
		u.predaj([]byte{0, 1, 2}, time.Now())
		if u.Odbaceni() != 2 || !u.Radi() || u.Port() != 4713 {
			t.Errorf("odbačeno %d, radi %v, port %d", u.Odbaceni(), u.Radi(), u.Port())
		}
	})
}

// Dva nagla niza od 40 paketa koji nisu QUIC, prije i za vrijeme čitanja, ne
// sruše čitača: višak baci quic-go ili ga čitač broji, a provjera mreže
// nakon toga radi.
func TestUDPNagliNizPaketa(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		n := &simnet.Simnet{Router: &simnet.PerfectRouter{}}
		veza := simnet.NodeBiDiLinkSettings{Latency: time.Millisecond}
		nas := n.NewEndpoint(adresaRacunala, veza)
		bomba := n.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("203.0.113.9"), Port: 5000}, veza)
		stun := n.NewEndpoint(adresaGoogle, veza)
		bezGreske(t, n.Start())
		posalji := func() {
			for i := range 40 {
				_, _ = bomba.WriteTo([]byte{0, byte(i), 0, 0}, adresaRacunala) // simnet ne vraća grešku za poznatu adresu
			}
		}
		posalji() // prije nego što itko čita: quic-go ih baca
		time.Sleep(10 * time.Millisecond)
		u := NoviUDP(nas)
		posalji() // za vrijeme čitanja
		time.Sleep(10 * time.Millisecond)
		go (&lazniSTUN{pocetak: time.Now()}).vrti(t, stun, uvijek("198.51.100.7:4713"), odmah)
		m := u.ProvjeriMrezu(t.Context(), posluzitelji[:1])
		if !m.Odgovori[0].Odgovorio || u.Odbaceni() == 0 || u.Odbaceni() > 80 || !u.Radi() {
			t.Errorf("nakon niza: %+v, odbačeno %d", m, u.Odbaceni())
		}
		bezGreske(t, u.Close())
		bomba.Close()
		stun.Close()
		n.Close()
	})
}

// Utičnica koja je zatvorena prestane čitati.
func TestUDPZatvaranje(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		n := &simnet.Simnet{Router: &simnet.PerfectRouter{}}
		c := n.NewEndpoint(adresaRacunala, simnet.NodeBiDiLinkSettings{Latency: time.Millisecond})
		bezGreske(t, n.Start())
		u := NoviUDP(c)
		if !u.Radi() {
			t.Fatal("ne radi odmah")
		}
		if err := u.Close(); err != nil {
			t.Fatal(err)
		}
		if u.Radi() {
			t.Error("radi nakon zatvaranja")
		}
		n.Close()
	})
}

func TestStanjeMrezeHistereza(t *testing.T) {
	var s StanjeMreze
	korak := func(vrsta string, promjena bool, want string) {
		t.Helper()
		if got := s.Dodaj(MjerenjeMreze{Vrsta: vrsta}); got != promjena || s.Vrsta != want || s.Zadnje.Vrsta != vrsta {
			t.Fatalf("nakon %s: promjena %v, vrsta %q", vrsta, got, s.Vrsta)
		}
	}
	korak(NATNepoznat, false, "")   // nepoznato ne postavlja ništa
	korak(NATLak, true, NATLak)     // prvo što nešto kaže vrijedi odmah
	korak(NATOvisan, false, NATLak) // jedno drukčije mjerenje ne mijenja
	korak(NATNepoznat, false, NATLak)
	korak(NATOvisan, false, NATLak) // nepoznato između briše kandidata
	korak(NATOvisan, true, NATOvisan)
	korak(NATOvisan, false, NATOvisan)
	korak(UDPBlokiran, false, NATOvisan)
	korak(NATLak, false, NATOvisan) // drugi kandidat kreće ispočetka
	korak(NATLak, true, NATLak)
}

// Opis mjerenja je jedna rečenica bez ijedne adrese, ni kad je javna
// adresa poznata.
func TestOpisMjerenja(t *testing.T) {
	javna := netip.MustParseAddrPort("203.0.113.7:4713")
	google := OdgovorPosluzitelja{Ime: "Google", Razrijesen: true, Odgovorio: true, Javna: javna, RTT: 12 * time.Millisecond}
	for _, c := range []struct {
		m    MjerenjeMreze
		want string
	}{
		{MjerenjeMreze{Vrsta: NATLak, CuvaPort: true, Javna: javna, Odgovori: []OdgovorPosluzitelja{google,
			{Ime: "Cloudflare", Razrijesen: true, Odgovorio: true, Javna: javna, RTT: 300 * time.Microsecond}}},
			"NAT je lak i čuva port. STUN: Google 12 ms, Cloudflare ispod 1 ms."},
		{MjerenjeMreze{Vrsta: NATOvisan, Odgovori: []OdgovorPosluzitelja{google}},
			"NAT ovisi o odredištu i ne čuva port. STUN: Google 12 ms."},
		{MjerenjeMreze{Vrsta: NATNepoznat, Odgovori: []OdgovorPosluzitelja{google, {Ime: "Cloudflare"}}},
			"vrsta NAT-a se ne zna. STUN: Google 12 ms, Cloudflare se ne da razriješiti."},
		{MjerenjeMreze{Vrsta: UDPBlokiran, Odgovori: []OdgovorPosluzitelja{{Ime: "Google", Razrijesen: true}}},
			"UDP do STUN poslužitelja ne prolazi. STUN: Google bez odgovora."},
		{MjerenjeMreze{Vrsta: NATNepoznat}, "vrsta NAT-a se ne zna. STUN poslužitelja nema."},
	} {
		if got := c.m.Opis(); got != c.want || strings.Contains(got, "203.0.113") {
			t.Errorf("%s: %q", c.m.Vrsta, got)
		}
	}
}

// Zauzet port daje nasumičan uz upozorenje; utičnica koja se nikako ne da
// otvoriti je greška.
func TestOtvoriUDP(t *testing.T) {
	zauzet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	bezGreske(t, err)
	defer zauzet.Close()
	port := zauzet.LocalAddr().(*net.UDPAddr).Port
	staro := otvoriUDP
	t.Cleanup(func() { otvoriUDP = staro })
	otvoriUDP = func(p int) (net.PacketConn, error) {
		if p == port {
			return nil, errors.New("adresa je zauzeta")
		}
		return net.ListenPacket("udp4", "127.0.0.1:0")
	}
	c, upozorenje, err := OtvoriUDP(port)
	if err != nil || upozorenje == nil || c.LocalAddr().(*net.UDPAddr).Port == port {
		t.Fatalf("zauzet port: %v %v", upozorenje, err)
	}
	c.Close()
	c, upozorenje, err = OtvoriUDP(port + 1)
	if err != nil || upozorenje != nil {
		t.Fatalf("slobodan port: %v %v", upozorenje, err)
	}
	c.Close()
	otvoriUDP = func(int) (net.PacketConn, error) { return nil, errors.New("nema mreže") }
	if c, upozorenje, err := OtvoriUDP(4713); err == nil || c != nil || upozorenje == nil {
		t.Errorf("utičnica bez mreže: %v %v", upozorenje, err)
	}
	otvoriUDP = staro
	if c, upozorenje, err := OtvoriUDP(0); err != nil || upozorenje != nil {
		t.Errorf("prava utičnica: %v %v", upozorenje, err)
	} else {
		c.Close()
	}
}

// tudjaAdresa je utičnica čija lokalna adresa nije UDP
type tudjaAdresa struct{ net.PacketConn }

func (tudjaAdresa) LocalAddr() net.Addr { return &net.IPAddr{IP: net.IPv4(10, 0, 0, 1)} }

// Prijenos je onaj nad utičnicom; utičnica bez UDP adrese nema port.
func TestUDPPrijenosIPort(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		n := &simnet.Simnet{Router: &simnet.PerfectRouter{}}
		c := n.NewEndpoint(adresaRacunala, simnet.NodeBiDiLinkSettings{Latency: time.Millisecond})
		bezGreske(t, n.Start())
		u := NoviUDP(tudjaAdresa{c})
		if u.Transport() == nil || u.Transport().Conn.LocalAddr().String() != "10.0.0.1" || u.Port() != 0 {
			t.Errorf("prijenos %v, port %d", u.Transport(), u.Port())
		}
		bezGreske(t, u.Close())
		n.Close()
	})
}
