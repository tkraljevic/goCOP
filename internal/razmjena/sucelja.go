package razmjena

// Filtar sučelja (plan WEB synca, LAN1). Pronalaženje u lokalnoj mreži ne
// proba, a WEB sync (F1) ne uzima za kandidate, sučelja koja ne vode u
// lokalnu mrežu ureda ili kuće: mostove spremnika i virtualnih strojeva, VPN
// tunele i Tailscale. Na Linuxu s Dockerom docker0 (172.17.0.1) ima privatnu
// adresu i bez filtra bi bio cilj probe i lokalni kandidat.

import (
	"net"
	"strings"
)

// izbacenaSucelja su početci imena sučelja koja filtar izbacuje: Docker
// (docker*, br-*, veth*), libvirt (virbr*), VPN i Tailscale na macOS-u
// (utun*), Tailscale na Linuxu (tailscale*), Hyper-V i WSL na Windowsu
// (vEthernet*) i mostovi na macOS-u (bridge*)
var izbacenaSucelja = []string{"docker", "br-", "veth", "virbr", "utun", "tailscale", "vEthernet", "bridge"}

// izbaceno javlja izbacuje li filtar sučelje s tim imenom
func izbaceno(ime string) bool {
	for _, p := range izbacenaSucelja {
		if strings.HasPrefix(ime, p) {
			return true
		}
	}
	return false
}

// sucelje je ono što pronalaženje gleda na jednom sučelju računala
type sucelje struct {
	ime       string
	zastavice net.Flags
	adrese    []net.Addr
}

// procitajSucelja su sučelja ovog računala; sučelje čije se adrese ne daju
// pročitati ostaje bez adresa. Var, da test podmetne lažni popis.
var procitajSucelja = func() []sucelje {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]sucelje, 0, len(ifs))
	for _, ifc := range ifs {
		adrese, _ := ifc.Addrs() // sučelje bez adresa nije cilj probe
		out = append(out, sucelje{ime: ifc.Name, zastavice: ifc.Flags, adrese: adrese})
	}
	return out
}

// lokalnaSucelja su sučelja koja vode u lokalnu mrežu: uključena i ne
// izbačena filtrom
func lokalnaSucelja(sva []sucelje) []sucelje {
	var out []sucelje
	for _, s := range sva {
		if s.zastavice&net.FlagUp != 0 && !izbaceno(s.ime) {
			out = append(out, s)
		}
	}
	return out
}

// usmjereniBroadcast je adresa usmjerenog broadcasta IPv4 mreže, ili nil
// za adresu koja nije IPv4 mreža. Maska IPv4 mreže zna stići i u 16 bajtova.
func usmjereniBroadcast(a net.Addr) net.IP {
	ipn, ok := a.(*net.IPNet)
	if !ok || ipn.IP.To4() == nil {
		return nil
	}
	m := ipn.Mask
	if len(m) == net.IPv6len {
		m = m[net.IPv6len-net.IPv4len:]
	}
	if len(m) != net.IPv4len {
		return nil
	}
	ip, b := ipn.IP.To4(), make(net.IP, net.IPv4len)
	for i := range b {
		b[i] = ip[i] | ^m[i]
	}
	return b
}
