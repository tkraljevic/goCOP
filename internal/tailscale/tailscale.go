// Paket tailscale prepoznaje Tailscale adrese ovog računala.
//
// Tailscale je privatna mreža koju imaju samo neki čvorovi (Odluke
// 12. 10. 2026.). Uređaj u njoj dobiva adresu iz 100.64.0.0/10 i iz
// fd7a:115c:a1e0::/48, na mrežnoj kartici koja se na Linuxu zove
// tailscale0, na macOS-u utunN, a na Windowsima Tailscale.
//
// Raspon 100.64.0.0/10 nije samo Tailscaleov: istu adresu (CGNAT) daju i
// mobilni operateri, npr. laptopu s ugrađenim modemom, i drugi VPN-ovi
// (Cloudflare WARP, NetBird, NordVPN Meshnet). Zato se kao vlastita Tailscale
// adresa broji samo adresa iz raspona na Tailscaleovoj kartici. Na Linuxu i
// Windowsima to se vidi po imenu. Na macOS-u ime utunN dobiva svaki VPN, pa je
// utun kartica Tailscaleova tek kad ima i adresu iz fd7a:115c:a1e0::/48, koju
// daje samo Tailscale. Na računalu bez Tailscalea VlastiteAdrese je prazan
// popis.
package tailscale

import (
	"net"
	"net/netip"
	"slices"
	"strings"
)

var (
	raspon4 = netip.MustParsePrefix("100.64.0.0/10")
	raspon6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// JeTailscale javlja je li adresa iz raspona koje Tailscale dodjeljuje
// uređajima. Sama po sebi ne znači da je adresa ovog računala.
func JeTailscale(a netip.Addr) bool {
	a = a.Unmap()
	return raspon4.Contains(a) || raspon6.Contains(a)
}

// Sucelje je mrežna kartica ovog računala s njezinim adresama
type Sucelje struct {
	Ime    string
	Adrese []netip.Addr
}

// sucelja čita mrežne kartice ovog računala; testovi je zamijene
var sucelja = citajSucelja

// VlastiteAdrese vraća Tailscale adrese ovog računala: adrese iz
// Tailscaleovih raspona na Tailscaleovoj kartici. Čita kartice svaki put
// iznova, jer se Tailscale može upaliti i nakon goCOP-a.
func VlastiteAdrese() []netip.Addr {
	var out []netip.Addr
	for _, s := range sucelja() {
		if !karticaTailscalea(s) {
			continue
		}
		for _, a := range s.Adrese {
			if JeTailscale(a) {
				out = append(out, a.Unmap())
			}
		}
	}
	return out
}

// karticaTailscalea: tailscale0 (Linux) i Tailscale (Windows) po imenu;
// utunN (macOS) samo uz adresu iz fd7a:115c:a1e0::/48, jer to ime dobiva
// svaki VPN. Mac na kojem je u Tailscale mreži isključen IPv6 tako se ne
// prepozna i radi kao računalo bez Tailscalea.
func karticaTailscalea(s Sucelje) bool {
	ime := strings.ToLower(s.Ime)
	if strings.HasPrefix(ime, "tailscale") {
		return true
	}
	return strings.HasPrefix(ime, "utun") && slices.ContainsFunc(s.Adrese, raspon6.Contains)
}

// citajSucelja vraća kartice ovog računala; bez popisa kartica nema ni
// Tailscalea, pa greška daje prazan popis
func citajSucelja() []Sucelje {
	kartice, _ := net.Interfaces()
	out := make([]Sucelje, 0, len(kartice))
	for _, k := range kartice {
		adrese, _ := k.Addrs()
		out = append(out, Sucelje{Ime: k.Name, Adrese: adreseIz(adrese)})
	}
	return out
}

// adreseIz pretvara adrese kartice (net.IPNet) u netip.Addr
func adreseIz(adrese []net.Addr) []netip.Addr {
	var out []netip.Addr
	for _, a := range adrese {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip, ok := netip.AddrFromSlice(n.IP); ok {
			out = append(out, ip.Unmap())
		}
	}
	return out
}
