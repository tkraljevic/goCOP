package tailscale

import (
	"net"
	"net/netip"
	"slices"
	"testing"
)

func TestJeTailscale(t *testing.T) {
	for adresa, ocekivano := range map[string]bool{
		"100.64.0.0":          true,
		"100.101.102.103":     true,
		"100.127.255.255":     true,
		"100.63.255.255":      false,
		"100.128.0.0":         false,
		"192.168.1.10":        false,
		"10.0.0.5":            false,
		"127.0.0.1":           false,
		"::ffff:100.101.1.2":  true, // IPv4 kroz IPv6 utičnicu
		"fd7a:115c:a1e0::1":   true,
		"fd7a:115c:a1e0:ab::": true,
		"fd7a:115c:a1e1::1":   false,
		"fd00::1":             false,
		"::1":                 false,
	} {
		if JeTailscale(netip.MustParseAddr(adresa)) != ocekivano {
			t.Errorf("%s: očekivano %v", adresa, ocekivano)
		}
	}
	if JeTailscale(netip.Addr{}) {
		t.Error("neispravna adresa nije Tailscale")
	}
}

func adrese(s ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(s))
	for _, a := range s {
		out = append(out, netip.MustParseAddr(a))
	}
	return out
}

// Vlastita Tailscale adresa je samo adresa iz raspona na Tailscaleovoj
// kartici: CGNAT adresa od mobilnog operatera na drugoj kartici nije
func TestVlastiteAdrese(t *testing.T) {
	stara := sucelja
	t.Cleanup(func() { sucelja = stara })
	sucelja = func() []Sucelje {
		return []Sucelje{
			{Ime: "lo", Adrese: adrese("127.0.0.1", "::1")},
			{Ime: "eth0", Adrese: adrese("192.168.1.10", "100.70.0.9")}, // operater (CGNAT)
			{Ime: "wwan0", Adrese: adrese("100.80.1.2")},
			{Ime: "tailscale0", Adrese: adrese("100.101.102.103", "fd7a:115c:a1e0::5", "fe80::1")},
			{Ime: "utun4", Adrese: adrese("::ffff:100.101.1.2", "fd7a:115c:a1e0::7")},
			{Ime: "utun2", Adrese: adrese("10.8.0.2")}, // drugi VPN
			{Ime: "Tailscale", Adrese: adrese("100.90.0.1")},
		}
	}
	dobiveno := VlastiteAdrese()
	ocekivano := adrese("100.101.102.103", "fd7a:115c:a1e0::5", "100.101.1.2", "fd7a:115c:a1e0::7", "100.90.0.1")
	if !slices.Equal(dobiveno, ocekivano) {
		t.Errorf("vlastite adrese %v, očekivano %v", dobiveno, ocekivano)
	}

	sucelja = func() []Sucelje { return []Sucelje{{Ime: "en0", Adrese: adrese("192.168.1.10")}} }
	if a := VlastiteAdrese(); len(a) != 0 {
		t.Errorf("računalo bez Tailscalea nema Tailscale adresa: %v", a)
	}
}

// Na macOS-u karticu utunN dobiva svaki VPN, a adresu iz 100.64.0.0/10 daju
// i drugi (Cloudflare WARP 100.96.x.x, NetBird, NordVPN Meshnet). Takva
// kartica nije Tailscale dok nema i adresu iz fd7a:115c:a1e0::/48.
func TestUtunDrugogVPNaNijeTailscale(t *testing.T) {
	stara := sucelja
	t.Cleanup(func() { sucelja = stara })
	sucelja = func() []Sucelje {
		return []Sucelje{
			{Ime: "en0", Adrese: adrese("192.168.1.50")},
			{Ime: "utun5", Adrese: adrese("100.96.0.5", "fe80::5")},                  // WARP
			{Ime: "utun100", Adrese: adrese("100.70.3.4", "2606:4700:110:8a::5")},    // drugi VPN s IPv6
			{Ime: "utun3", Adrese: adrese("::ffff:100.80.0.2", "fd7a:115c:a1e1::2")}, // izvan Tailscaleova raspona
		}
	}
	if a := VlastiteAdrese(); len(a) != 0 {
		t.Errorf("Mac bez Tailscalea, s drugim VPN-om, nema Tailscale adresa: %v", a)
	}

	// Tailscale na macOS-u: ista kartica ima i IPv4 i IPv6 adresu
	sucelja = func() []Sucelje {
		return []Sucelje{
			{Ime: "utun5", Adrese: adrese("100.96.0.5")},
			{Ime: "utun8", Adrese: adrese("100.101.0.1", "fd7a:115c:a1e0::1", "fe80::8")},
		}
	}
	if a, ocekivano := VlastiteAdrese(), adrese("100.101.0.1", "fd7a:115c:a1e0::1"); !slices.Equal(a, ocekivano) {
		t.Errorf("Mac s Tailscaleom i WARP-om: %v, očekivano %v", a, ocekivano)
	}
}

// Kartice se čitaju sa stvarnog računala; adresa petlje uvijek postoji
func TestCitajSucelja(t *testing.T) {
	var petlja bool
	for _, s := range citajSucelja() {
		petlja = petlja || slices.ContainsFunc(s.Adrese, netip.Addr.IsLoopback)
	}
	if !petlja {
		t.Error("na popisu kartica nema adrese petlje")
	}
}

func TestAdreseIz(t *testing.T) {
	dobiveno := adreseIz([]net.Addr{
		&net.IPNet{IP: net.ParseIP("100.101.102.103"), Mask: net.CIDRMask(32, 32)}, // 16 bajtova
		&net.IPNet{IP: net.IPv4(192, 168, 1, 10).To4(), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.IP{1, 2, 3}}, // neispravna
		&net.IPAddr{IP: net.ParseIP("10.0.0.1")},
	})
	if ocekivano := adrese("100.101.102.103", "192.168.1.10"); !slices.Equal(dobiveno, ocekivano) {
		t.Errorf("adrese %v, očekivano %v", dobiveno, ocekivano)
	}
}
