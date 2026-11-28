package peers

// Opis veze za dodatni korak razgovora (VezaKoraka, korak_razgovora.go):
// kojim putem veza ide, tko je nazvao i je li druga strana u lokalnoj
// mreži. Korak po tome bira koliko šalje i radi li uopće; biblioteka sama
// ništa od toga ne zaključuje.

import (
	"net"
	"net/netip"
	"slices"

	"gocop/internal/razmjena"
	"gocop/internal/tailscale"
)

// vezaRazgovora je VezaKoraka za vezu razgovora; zvao: ovo računalo je
// nazvalo
type vezaRazgovora struct {
	c    *razmjena.Conn
	zvao bool
}

func (v vezaRazgovora) Put() razmjena.Put      { return v.c.Put }
func (v vezaRazgovora) Pozivatelj() bool       { return v.zvao }
func (v vezaRazgovora) LokalnaMreza() bool     { return putULokalnojMrezi(v.c, v.zvao) }
func (v vezaRazgovora) IstaLokalnaMreza() bool { return istaLokalnaMreza(v.c) }

// putULokalnojMrezi: veza iz pronalaženja, ili na privatnu adresu druge
// strane (i dolazna na portu), kao korak LAN synca u krugu (krug.dodaj).
// Uže pravilo je istaLokalnaMreza. Dolazna veza s adrese ovog računala
// (127.0.0.1, ::1) nije lokalna mreža: tako stiže i ono što s interneta na
// port prosljeđuje posrednik na ovom računalu. zvao: ovo računalo je
// nazvalo.
func putULokalnojMrezi(c *razmjena.Conn, zvao bool) bool {
	if c.Put != razmjena.PutAdresa {
		return c.Put == razmjena.PutLAN
	}
	adresa := c.RemoteAddr().String()
	return privatnaAdresa(adresa) && (zvao || !adresaOvogRacunala(adresa))
}

// adresaOvogRacunala: adresa, s portom ili bez, je loopback
func adresaOvogRacunala(adresa string) bool {
	host, _, err := net.SplitHostPort(adresa)
	if err != nil {
		host = adresa // adresa bez porta
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

// istaLokalnaMreza: druga strana je u istoj lokalnoj mreži kao ovo
// računalo, ista kuća ili ured. Veza iz pronalaženja (PutLAN) jest. Veza na
// adresu (PutAdresa), odlazna i dolazna, jest kad je adresa druge strane u
// mreži sučelja kojim veza ide, onoga koje nosi lokalnu adresu veze. Kad se
// to ne da utvrditi, nije. Sučelja se biraju kao za pronalaženje
// (razmjena.MrezeLokalnihSucelja), pa VPN tunel i most spremnika ili
// virtualnog stroja nisu lokalna mreža. Ni adresa ovog računala (127.0.0.1,
// ::1; posrednik na istom računalu), ni Tailscaleovi rasponi, ni link-local
// adresa (fe80::, 169.254.) nisu.
//
// Poznato ograničenje: vidi se samo adresa s koje veza stiže. Kad čvor
// radi u Dockeru uz most (bridge), sučelje spremnika je npr. 172.17.0.2/16:
// veza koju Docker prosljeđuje svojim posrednikom (npr. IPv6 na spremnik
// bez IPv6) stiže s 172.17.0.1 i izgleda kao ista lokalna mreža, a laptop iz
// iste kuće (192.168.1.20) ne, jer ta mreža nije na sučelju spremnika. Uz
// mrežu računala (host) radi kao i drugi. Isto tako veza koju usmjerivač
// VPN-a (npr. Tailscaleova ruta podmreže, VPN s maskiranjem) ili kućni
// usmjerivač prosljeđuje sa svoje LAN adrese izgleda kao ista lokalna mreža,
// a i sučelje sa širokom maskom (npr. 10.0.0.0/8 preko više gradova) cijelu
// tu mrežu računa kao istu lokalnu mrežu.
func istaLokalnaMreza(c *razmjena.Conn) bool {
	if c.Put != razmjena.PutAdresa {
		return c.Put == razmjena.PutLAN
	}
	udaljena, ok := ipZaMrezu(c.RemoteAddr())
	lokalna, ok2 := ipZaMrezu(c.LocalAddr())
	if !ok || !ok2 {
		return false
	}
	return uMrezamaSucelja(suceljaSAdresom(mrezeSucelja(), lokalna), udaljena)
}

// mrezeSucelja su mreže sučelja ovog računala koja vode u lokalnu mrežu,
// po sučelju; var, da test podmetne svoja sučelja
var mrezeSucelja = razmjena.MrezeLokalnihSucelja

// ipZaMrezu je IP adresa kraja veze za usporedbu s mrežama sučelja: ne
// adresa ovog računala (loopback), ne link-local (zona sučelja se ne vidi u
// mreži), ne Tailscaleova i ne nešto što nije IP (tunel)
func ipZaMrezu(a net.Addr) (net.IP, bool) {
	ip, ok := ipVeze(a)
	return net.IP(ip.AsSlice()), ok && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !tailscale.JeTailscale(ip)
}

// suceljaSAdresom su sučelja koja nose adresu ip
func suceljaSAdresom(sucelja [][]*net.IPNet, ip net.IP) [][]*net.IPNet {
	var out [][]*net.IPNet
	for _, mreze := range sucelja {
		if slices.ContainsFunc(mreze, func(n *net.IPNet) bool { return n.IP.Equal(ip) }) {
			out = append(out, mreze)
		}
	}
	return out
}

// uMrezamaSucelja: ip je u mreži jednog od sučelja; mreža Tailscalea se ne
// gleda
func uMrezamaSucelja(sucelja [][]*net.IPNet, ip net.IP) bool {
	for _, mreze := range sucelja {
		for _, n := range mreze {
			if a, ok := netip.AddrFromSlice(n.IP); ok && !tailscale.JeTailscale(a) && n.Contains(ip) {
				return true
			}
		}
	}
	return false
}
