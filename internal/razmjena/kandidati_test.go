package razmjena

// Vlastiti kandidati (kandidati.go) s lažnim popisom sučelja i lažnim
// usmjeravanjem: izbačena sučelja (Docker, Tailscale, WARP, mostovi),
// adrese koje nisu privatni IPv4, maska u 16 bajtova, ruta podmreže kroz
// Tailscale, izlazni čvor, lan_adresa, granica od 8 i javni kandidat.

import (
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// usmjeravanje je lažna jezgra: odredište po mreži → adresa s koje promet
// izlazi; bez pravila nema rute
type usmjeravanje map[string]string

func (u usmjeravanje) izlaz(dst netip.AddrPort) (netip.Addr, error) {
	for mreza, kroz := range u {
		if netip.MustParsePrefix(mreza).Contains(dst.Addr()) {
			return netip.MustParseAddr(kroz), nil
		}
	}
	return netip.Addr{}, errors.New("network is unreachable")
}

func adreseKandidata(k []Kandidat) []string {
	var out []string
	for _, c := range k {
		out = append(out, c.Vrsta+" "+c.Adresa.String())
	}
	return out
}

func TestLokalniKandidati(t *testing.T) {
	gore := net.FlagUp | net.FlagBroadcast
	maska16 := ipMreza(t, "192.168.5.20/24")
	maska16.Mask = net.CIDRMask(120, 128)
	sva := []sucelje{
		{ime: "en0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "192.168.1.10/24"), ipMreza(t, "fe80::1/64"), ipMreza(t, "fd00::5/64"),
			ipMreza(t, "169.254.3.4/16"), &net.IPAddr{IP: net.IPv4(192, 168, 9, 9)}}},
		{ime: "eth1", zastavice: gore, adrese: []net.Addr{maska16, &net.IPNet{IP: net.IPv4(10, 1, 1, 1), Mask: net.IPMask{255, 255}}}},
		{ime: "wwan0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "100.80.1.2/24")}}, // LTE iza CGNAT-a
		{ime: "eth2", zastavice: gore, adrese: []net.Addr{ipMreza(t, "10.20.0.5/16")}},   // ruta podmreže ide kroz Tailscale
		{ime: "eth3", zastavice: gore, adrese: []net.Addr{ipMreza(t, "10.30.0.5/16")}},   // nema rute
		{ime: "eth4", zastavice: gore, adrese: []net.Addr{ipMreza(t, "172.20.1.1/31")}},  // bez susjeda
		{ime: "docker0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "172.17.0.1/16")}},
		{ime: "utun3", zastavice: net.FlagUp | net.FlagPointToPoint, adrese: []net.Addr{ipMreza(t, "100.101.0.9/32")}},
		{ime: "tailscale0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "100.101.0.10/32")}},
		{ime: "Tailscale", zastavice: net.FlagUp, adrese: []net.Addr{ipMreza(t, "100.101.0.11/32")}},
		{ime: "CloudflareWARP", zastavice: net.FlagUp | net.FlagPointToPoint, adrese: []net.Addr{ipMreza(t, "172.16.0.2/32")}},
		{ime: "eth5", zastavice: gore, adrese: []net.Addr{ipMreza(t, "192.168.77.4/24")}}, // vlastita Tailscale adresa u privatnoj mreži
		{ime: "tap0", zastavice: gore, adrese: []net.Addr{ipMreza(t, "10.8.0.6/24")}},     // OpenVPN s broadcastom
		{ime: "en5", zastavice: net.FlagBroadcast, adrese: []net.Addr{ipMreza(t, "192.168.2.10/24")}},
		{ime: "lo0", zastavice: net.FlagUp | net.FlagLoopback | net.FlagBroadcast, adrese: []net.Addr{ipMreza(t, "127.0.0.1/8")}},
	}
	jezgra := usmjeravanje{"192.168.1.0/24": "192.168.1.10", "192.168.5.0/24": "192.168.5.20", "10.20.0.0/16": "100.101.0.10", "10.8.0.0/24": "10.8.0.6"}
	ts := []netip.Addr{netip.MustParseAddr("192.168.77.4"), netip.MustParseAddr("100.101.0.10")}
	k, razlozi := lokalniKandidati(4713, "", sva, ts, jezgra.izlaz)
	if got, want := adreseKandidata(k), []string{"lokalna 192.168.1.10:4713", "lokalna 192.168.5.20:4713"}; !slices.Equal(got, want) {
		t.Errorf("kandidati %v, želim %v", got, want)
	}
	if k[0].Sucelje != "en0" {
		t.Errorf("sučelje %q", k[0].Sucelje)
	}
	for _, r := range []string{"10.20.0.5 na eth2: promet prema toj mreži izlazi s 100.101.0.10", "10.30.0.5 na eth3: nema rute",
		"172.20.1.1 na eth4: mreža /31 nema susjeda"} {
		if !slices.ContainsFunc(razlozi, func(s string) bool { return strings.HasPrefix(s, r) }) {
			t.Errorf("nema razloga %q u %q", r, razlozi)
		}
	}
	if len(razlozi) != 3 {
		t.Errorf("razlozi: %q", razlozi)
	}
}

// lan_adresa (spremnik u mostu) zamjenjuje kandidate sa sučelja, bez
// provjere puta; kriva vrijednost se ne nudi, uz razlog
func TestLanAdresaKandidat(t *testing.T) {
	sva := []sucelje{{ime: "eth0", zastavice: net.FlagUp | net.FlagBroadcast, adrese: []net.Addr{ipMreza(t, "172.17.0.2/16")}}}
	k, razlozi := lokalniKandidati(4713, "192.168.1.2", sva, nil, usmjeravanje{}.izlaz)
	if got := adreseKandidata(k); !slices.Equal(got, []string{"lokalna 192.168.1.2:4713"}) || razlozi != nil || k[0].Sucelje != "lan_adresa" {
		t.Errorf("lan_adresa: %v %v", got, razlozi)
	}
	for _, krivo := range []string{"8.8.8.8", "fd00::5", "::ffff:192.168.1.2", "ured"} {
		if k, razlozi := lokalniKandidati(4713, krivo, sva, nil, usmjeravanje{}.izlaz); k != nil || len(razlozi) != 1 {
			t.Errorf("%q: %v %v", krivo, k, razlozi)
		}
	}
}

// Prvi je kandidat s kojeg promet izlazi prema STUN-u, dvojnici odlaze, a
// ostaje najviše 8; javni kandidat ide prvi, samo uz lak NAT i bez tunela.
func TestPoredakIJavniKandidat(t *testing.T) {
	var k []Kandidat
	for i := 1; i <= 12; i++ {
		k = append(k, Kandidat{Vrsta: KandidatLokalni, Adresa: netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 0, byte(i), 1}), 4713)})
	}
	k = append(k, k[0])
	prva := netip.AddrFrom4([4]byte{10, 0, 11, 1})
	izlazi := []IzlazSTUN{{Ime: "Google", Kroz: IzlazBezRute}, {Ime: "Cloudflare", Lokalna: prva}}
	p := poredaj(k, prvaIzlazna(izlazi), najviseKandidata)
	if len(p) != 8 || p[0].Adresa.Addr() != prva || p[1].Adresa != k[0].Adresa {
		t.Errorf("poredak: %v", adreseKandidata(p))
	}
	if prvaIzlazna(nil).IsValid() || len(poredaj(nil, prva, 8)) != 0 {
		t.Error("bez izlaza ili bez kandidata")
	}
	javna := netip.MustParseAddrPort("203.0.113.7:4713")
	lak := MjerenjeMreze{Vrsta: NATLak, Javna: javna}
	if s := SJavnim(p, lak, izlazi); len(s) != 8 || s[0].Vrsta != KandidatJavni || s[0].Adresa != javna {
		t.Errorf("s javnim: %v", adreseKandidata(s))
	}
	for opis, c := range map[string]struct {
		m      MjerenjeMreze
		izlazi []IzlazSTUN
	}{
		"ovisan NAT":       {MjerenjeMreze{Vrsta: NATOvisan, Javna: javna}, nil},
		"bez adrese":       {MjerenjeMreze{Vrsta: NATLak}, nil},
		"kroz Tailscale":   {lak, []IzlazSTUN{{Kroz: IzlazTailscale}}},
		"kroz drugi tunel": {lak, []IzlazSTUN{{Kroz: IzlazObican}, {Kroz: IzlazVPN}}},
	} {
		if s := SJavnim(p[:2], c.m, c.izlazi); len(s) != 2 {
			t.Errorf("%s: %v", opis, adreseKandidata(s))
		}
	}
}

// Izlaz prema STUN-u: vlastita Tailscale adresa je izlazni čvor, tunel po
// imenu je VPN, a mobilni modem, PPP, most s LAN-om, petlja i nepoznato
// sučelje su običan put. Bez rute ostaje greška, a neriješen poslužitelj
// nema izlaza.
func TestIzlazPremaSTUN(t *testing.T) {
	sva := []sucelje{
		{ime: "en0", zastavice: net.FlagUp | net.FlagBroadcast, adrese: []net.Addr{ipMreza(t, "192.168.1.10/24")}},
		{ime: "utun4", zastavice: net.FlagUp | net.FlagPointToPoint, adrese: []net.Addr{ipMreza(t, "10.99.0.2/32")}},
		{ime: "tailscale0", zastavice: net.FlagUp, adrese: []net.Addr{ipMreza(t, "100.101.0.10/32")}},
		{ime: "CloudflareWARP", zastavice: net.FlagUp, adrese: []net.Addr{ipMreza(t, "172.16.0.2/32")}},
		{ime: "Cellular", zastavice: net.FlagUp, adrese: []net.Addr{ipMreza(t, "10.140.3.7/30")}},
		{ime: "ppp0", zastavice: net.FlagUp | net.FlagPointToPoint, adrese: []net.Addr{ipMreza(t, "100.70.1.2/32")}},
		{ime: "vEthernet (External)", zastavice: net.FlagUp | net.FlagBroadcast, adrese: []net.Addr{ipMreza(t, "192.168.1.20/24")}},
	}
	ts := []netip.Addr{netip.MustParseAddr("100.101.0.10")}
	posluzitelji := []PosluziteljSTUN{
		{Ime: "obican", Adresa: &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 3478}},
		{Ime: "izlazni čvor", Adresa: &net.UDPAddr{IP: net.IPv4(2, 2, 2, 2), Port: 3478}},
		{Ime: "vpn", Adresa: &net.UDPAddr{IP: net.IPv4(3, 3, 3, 3), Port: 3478}},
		{Ime: "petlja", Adresa: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3478}},
		{Ime: "nepoznato sučelje", Adresa: &net.UDPAddr{IP: net.IPv4(4, 4, 4, 4), Port: 3478}},
		{Ime: "bez rute", Adresa: &net.UDPAddr{IP: net.IPv4(5, 5, 5, 5), Port: 3478}},
		{Ime: "WARP", Adresa: &net.UDPAddr{IP: net.IPv4(6, 6, 6, 6), Port: 3478}},
		{Ime: "LTE", Adresa: &net.UDPAddr{IP: net.IPv4(7, 7, 7, 7), Port: 3478}},
		{Ime: "PPP", Adresa: &net.UDPAddr{IP: net.IPv4(8, 8, 4, 4), Port: 3478}},
		{Ime: "most s LAN-om", Adresa: &net.UDPAddr{IP: net.IPv4(9, 9, 9, 9), Port: 3478}},
		{Ime: "neriješen"},
	}
	jezgra := usmjeravanje{"1.1.1.1/32": "192.168.1.10", "2.2.2.2/32": "100.101.0.10", "3.3.3.3/32": "10.99.0.2",
		"127.0.0.0/8": "127.0.0.1", "4.4.4.4/32": "192.168.200.1", "6.6.6.6/32": "172.16.0.2", "7.7.7.7/32": "10.140.3.7",
		"8.8.4.4/32": "100.70.1.2", "9.9.9.9/32": "192.168.1.20"}
	got := map[string]IzlazSTUN{}
	for _, i := range izlaziPremaSTUN(posluzitelji, sva, ts, jezgra.izlaz) {
		got[i.Ime] = i
	}
	for ime, kroz := range map[string]string{"obican": IzlazObican, "izlazni čvor": IzlazTailscale, "vpn": IzlazVPN,
		"petlja": IzlazObican, "nepoznato sučelje": IzlazObican, "bez rute": IzlazBezRute, "WARP": IzlazVPN,
		"LTE": IzlazObican, "PPP": IzlazObican, "most s LAN-om": IzlazObican} {
		if got[ime].Kroz != kroz {
			t.Errorf("%s: kroz %q, želim %q", ime, got[ime].Kroz, kroz)
		}
	}
	if len(got) != 10 || got["obican"].Sucelje != "en0" || got["bez rute"].Greska == "" || got["obican"].Lokalna.String() != "192.168.1.10" {
		t.Errorf("izlazi: %+v", got)
	}
}

// Prava jezgra: prema petlji promet izlazi s petlje, a prema IPv6 adresi
// IPv4 utičnica ne ide; omoti nad pravim sučeljima rade, a bez porta
// kandidata nema.
func TestPravaIzlaznaAdresa(t *testing.T) {
	if a, err := izlaznaAdresa(netip.MustParseAddrPort("127.0.0.1:4713")); err != nil || a.String() != "127.0.0.1" {
		t.Errorf("petlja: %v %v", a, err)
	}
	if _, err := izlaznaAdresa(netip.MustParseAddrPort("[::1]:4713")); err == nil {
		t.Error("IPv6 na udp4 bez greške")
	}
	if k, r := VlastitiKandidati(0, "", nil); k != nil || r != nil {
		t.Errorf("bez porta: %v %v", k, r)
	}
	if k, _ := VlastitiKandidati(4713, "192.168.1.2", nil); len(k) != 1 {
		t.Errorf("lan_adresa: %v", k)
	}
	if i := IzlazPremaSTUN([]PosluziteljSTUN{{Ime: "petlja", Adresa: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3478}}}); len(i) != 1 || i[0].Kroz != IzlazObican {
		t.Errorf("izlaz prema petlji: %+v", i)
	}
}

// Susjed je prva adresa mreže, a kad je to sama adresa sučelja, druga.
func TestSusjed(t *testing.T) {
	for p, want := range map[string]string{"192.168.1.10/24": "192.168.1.1", "192.168.1.1/24": "192.168.1.2", "10.0.0.6/30": "10.0.0.5", "10.0.0.5/30": "10.0.0.6"} {
		if got := susjed(netip.MustParsePrefix(p)); got.String() != want {
			t.Errorf("%s: %s, želim %s", p, got, want)
		}
	}
}
