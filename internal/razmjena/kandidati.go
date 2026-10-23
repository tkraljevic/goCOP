package razmjena

// Vlastiti kandidati WEB synca (plan WEB synca, F1b): adrese na kojima se
// ovo računalo nudi drugima za UDP. Lokalni su samo IPv4 adrese iz privatnih
// mreža (RFC 1918) na sučeljima koja vode u lokalnu mrežu, istim filtrom kao
// proba pronalaženja (LAN1), i samo kad promet prema susjedu u toj mreži
// stvarno izlazi s te adrese: tako se izbaci mreža koja ide kroz Tailscale
// (ruta podmreže). U spremniku (Docker) lokalni je samo lan_adresa. Javni je
// adresa koju vide STUN poslužitelji, samo uz lak NAT i kad promet prema
// njima ne ide kroz tunel. Najviše ih je 8: probijanje s 32 lokalne adrese
// pada, a s 8 prolazi (irohov test).
//
// Kandidati su samo u memoriji. Nisu isto što i peers.kandidat, adresa
// drugog računala na koju ovo zove TCP.

import (
	"fmt"
	"net"
	"net/netip"
	"slices"

	"gocop/internal/tailscale"
)

// najviseKandidata je koliko se kandidata najviše nudi, javni s lokalnima
const najviseKandidata = 8

// Vrste kandidata, iste riječi kao u pozivu preko sastajališta (F2b)
const (
	KandidatLokalni = "lokalna"
	KandidatJavni   = "javna"
)

// Kandidat je vlastita adresa na kojoj se ovo računalo nudi za WEB sync.
// Sucelje je samo za stranicu Sinkronizacija i ne ide na žicu.
type Kandidat struct {
	Vrsta   string
	Adresa  netip.AddrPort
	Sucelje string
}

// Kuda promet prema STUN poslužitelju izlazi iz ovog računala
const (
	IzlazObican    = ""
	IzlazTailscale = "tailscale" // izlazni čvor Tailscalea
	IzlazVPN       = "vpn"
	IzlazBezRute   = "bez rute"
)

// IzlazSTUN je adresa i sučelje s kojih promet prema STUN poslužitelju
// izlazi, i kroz što ide
type IzlazSTUN struct {
	Ime     string
	Lokalna netip.Addr
	Sucelje string
	Kroz    string
	Greska  string // kad rute nema; na tu grešku F2b čeka rutu
}

// izlazFunc je lokalna adresa s koje bi UDP prema odredištu izašao
type izlazFunc func(netip.AddrPort) (netip.Addr, error)

// izlaznaAdresa pita jezgru kojim bi putem UDP prema dst izašao: povezana
// utičnica ne šalje ništa. Var, da test podmetne usmjeravanje.
var izlaznaAdresa izlazFunc = func(dst netip.AddrPort) (netip.Addr, error) {
	c, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(dst))
	if err != nil {
		return netip.Addr{}, err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap(), nil
}

// VlastitiKandidati su lokalni kandidati za port UDP utičnice, iz sučelja
// ovog računala, ili samo lan_adresa kad je zadana. Prvi je onaj s kojeg
// promet izlazi prema STUN-u. Razlozi kažu zašto je koja adresa izostala,
// za stranicu Sinkronizacija. Bez porta kandidata nema.
func VlastitiKandidati(port int, lanAdresa string, izlazi []IzlazSTUN) ([]Kandidat, []string) {
	if port <= 0 {
		return nil, nil
	}
	k, razlozi := lokalniKandidati(port, lanAdresa, procitajSucelja(), tailscale.VlastiteAdrese(), izlaznaAdresa)
	return poredaj(k, prvaIzlazna(izlazi), najviseKandidata), razlozi
}

// IzlazPremaSTUN je put kojim promet prema svakom razriješenom STUN
// poslužitelju izlazi iz ovog računala
func IzlazPremaSTUN(posluzitelji []PosluziteljSTUN) []IzlazSTUN {
	return izlaziPremaSTUN(posluzitelji, procitajSucelja(), tailscale.VlastiteAdrese(), izlaznaAdresa)
}

// SJavnim doda lokalnima javni kandidat, prvi: adresu koju vide STUN
// poslužitelji, samo uz lak NAT i kad promet prema njima ne ide kroz tunel.
// Granica od 8 vrijedi za sve zajedno.
func SJavnim(lokalni []Kandidat, m MjerenjeMreze, izlazi []IzlazSTUN) []Kandidat {
	if m.Vrsta != NATLak || !m.Javna.IsValid() || krozTunel(izlazi) {
		return lokalni
	}
	out := append([]Kandidat{{Vrsta: KandidatJavni, Adresa: m.Javna}}, lokalni...)
	return out[:min(najviseKandidata, len(out))]
}

// lokalniKandidati su kandidati sa sučelja koja vode u lokalnu mrežu, ili
// samo lan_adresa
func lokalniKandidati(port int, lanAdresa string, sva []sucelje, ts []netip.Addr, izlaz izlazFunc) ([]Kandidat, []string) {
	if lanAdresa != "" {
		return lanKandidat(lanAdresa, port)
	}
	var out []Kandidat
	var razlozi []string
	for _, s := range suceljaZaLAN(sva) {
		for _, a := range s.adrese {
			k, razlog := kandidatSa(s.ime, a, port, ts, izlaz)
			if razlog != "" {
				razlozi = append(razlozi, razlog)
			}
			if k.Adresa.IsValid() {
				out = append(out, k)
			}
		}
	}
	return out, razlozi
}

// suceljaZaLAN su sučelja koja vode u lokalnu mrežu i imaju broadcast:
// isti skup na koji ide proba pronalaženja (ciljeviProbe). Bez broadcasta su
// petlja i tuneli (WireGuard, WARP, Wintun).
func suceljaZaLAN(sva []sucelje) []sucelje {
	return slices.DeleteFunc(lokalnaSucelja(sva), func(s sucelje) bool {
		return s.zastavice&net.FlagBroadcast == 0 || s.zastavice&net.FlagLoopback != 0
	})
}

// kandidatSa je kandidat s jedne adrese sučelja; prazan kandidat uz razlog
// kad je adresa privatna, a ne nudi se, i bez razloga kad nije ni privatna
func kandidatSa(ime string, a net.Addr, port int, ts []netip.Addr, izlaz izlazFunc) (Kandidat, string) {
	p, ok := privatnaMreza(a, ts)
	if !ok {
		return Kandidat{}, ""
	}
	if p.Bits() > 30 {
		return Kandidat{}, fmt.Sprintf("%s na %s: mreža /%d nema susjeda za provjeru puta", p.Addr(), ime, p.Bits())
	}
	kroz, err := izlaz(netip.AddrPortFrom(susjed(p), uint16(port)))
	if err != nil || kroz != p.Addr() {
		return Kandidat{}, razlogPuta(p.Addr(), ime, kroz, err)
	}
	return Kandidat{Vrsta: KandidatLokalni, Adresa: netip.AddrPortFrom(p.Addr(), uint16(port)), Sucelje: ime}, ""
}

// privatnaMreza je mreža adrese sučelja kad je to IPv4 iz privatnih mreža i
// nije vlastita Tailscale adresa
func privatnaMreza(a net.Addr, ts []netip.Addr) (netip.Prefix, bool) {
	p, ok := prefiksIPv4(a)
	return p, ok && p.Addr().IsPrivate() && !slices.Contains(ts, p.Addr())
}

// prefiksIPv4 je IPv4 mreža adrese sučelja. Adresa i maska znaju stići u
// 16 bajtova; maska koja nije IPv4 maska ne daje mrežu.
func prefiksIPv4(a net.Addr) (netip.Prefix, bool) {
	ipn, ok := a.(*net.IPNet)
	if !ok || ipn.IP.To4() == nil {
		return netip.Prefix{}, false
	}
	m := ipn.Mask
	if len(m) == net.IPv6len {
		m = m[net.IPv6len-net.IPv4len:]
	}
	jedinice, bitova := m.Size()
	ip, _ := netip.AddrFromSlice(ipn.IP.To4()) // To4 je uvijek 4 bajta
	return netip.PrefixFrom(ip, jedinice), bitova == 32
}

// susjed je adresa u istoj mreži prema kojoj se provjerava put: prva adresa
// mreže, a kad je to sama adresa sučelja, druga. Broadcast se ne bira.
func susjed(p netip.Prefix) netip.Addr {
	s := p.Masked().Addr().Next()
	if s == p.Addr() {
		s = s.Next()
	}
	return s
}

// razlogPuta kaže zašto adresa nije kandidat: promet prema njezinoj mreži
// izlazi s druge adrese (npr. ruta podmreže kroz Tailscale) ili nema rute
func razlogPuta(a netip.Addr, ime string, kroz netip.Addr, err error) string {
	if err != nil {
		return fmt.Sprintf("%s na %s: nema rute prema toj mreži (%v)", a, ime, err)
	}
	return fmt.Sprintf("%s na %s: promet prema toj mreži izlazi s %s", a, ime, kroz)
}

// lanKandidat je lan_adresa kao jedini lokalni kandidat, bez provjere puta:
// spremnik u mostu ne vidi kućnu mrežu, a domaćin port prosljeđuje u njega
func lanKandidat(lan string, port int) ([]Kandidat, []string) {
	ip, err := netip.ParseAddr(lan)
	if err != nil || !ip.Is4() || !ip.IsPrivate() {
		return nil, []string{fmt.Sprintf("lan_adresa %q nije IPv4 adresa iz privatne mreže, pa se ne nudi", lan)}
	}
	return []Kandidat{{Vrsta: KandidatLokalni, Adresa: netip.AddrPortFrom(ip, uint16(port)), Sucelje: "lan_adresa"}}, nil
}

// poredaj stavlja kandidat s adresom prva na početak, briše dvojnike i
// zadržava najviše n
func poredaj(k []Kandidat, prva netip.Addr, n int) []Kandidat {
	rang := func(c Kandidat) int {
		if c.Adresa.Addr() == prva {
			return 0
		}
		return 1
	}
	k = slices.Clone(k)
	slices.SortStableFunc(k, func(a, b Kandidat) int { return rang(a) - rang(b) })
	var out []Kandidat
	for _, c := range k {
		if !slices.ContainsFunc(out, func(d Kandidat) bool { return d.Adresa == c.Adresa }) {
			out = append(out, c)
		}
	}
	return out[:min(n, len(out))]
}

// prvaIzlazna je adresa s koje obični promet izlazi prema prvom STUN
// poslužitelju; nevaljana kad takvog nema
func prvaIzlazna(izlazi []IzlazSTUN) netip.Addr {
	for _, i := range izlazi {
		if i.Kroz == IzlazObican {
			return i.Lokalna
		}
	}
	return netip.Addr{}
}

// krozTunel javlja ide li promet prema nekom STUN poslužitelju kroz tunel
func krozTunel(izlazi []IzlazSTUN) bool {
	return slices.ContainsFunc(izlazi, func(i IzlazSTUN) bool { return i.Kroz == IzlazTailscale || i.Kroz == IzlazVPN })
}

// izlaziPremaSTUN je izlaz prema svakom razriješenom poslužitelju
func izlaziPremaSTUN(posluzitelji []PosluziteljSTUN, sva []sucelje, ts []netip.Addr, izlaz izlazFunc) []IzlazSTUN {
	var out []IzlazSTUN
	for _, p := range posluzitelji {
		ap, ok := adresaPosluzitelja(p.Adresa)
		if !ok {
			continue
		}
		lok, err := izlaz(ap)
		out = append(out, razvrstajIzlaz(p.Ime, lok, err, sva, ts))
	}
	return out
}

// adresaPosluzitelja je adresa razriješenog STUN poslužitelja
func adresaPosluzitelja(a net.Addr) (netip.AddrPort, bool) {
	if a == nil {
		return netip.AddrPort{}, false
	}
	ap, err := netip.ParseAddrPort(a.String())
	return ap, err == nil
}

// razvrstajIzlaz kaže kroz što ide promet s lokalne adrese: vlastita
// Tailscale adresa je izlazni čvor, a sučelje koje ne vodi u lokalnu mrežu
// (tunel, izbačeno filtrom) VPN. Petlja je običan put: tako je kad je STUN na
// ovom istom računalu, u testovima.
func razvrstajIzlaz(ime string, lok netip.Addr, err error, sva []sucelje, ts []netip.Addr) IzlazSTUN {
	if err != nil {
		return IzlazSTUN{Ime: ime, Kroz: IzlazBezRute, Greska: err.Error()}
	}
	i := IzlazSTUN{Ime: ime, Lokalna: lok, Sucelje: suceljeAdrese(sva, lok)}
	switch {
	case slices.Contains(ts, lok):
		i.Kroz = IzlazTailscale
	case lok.IsLoopback():
	case i.Sucelje != "" && !slices.ContainsFunc(suceljaZaLAN(sva), func(s sucelje) bool { return s.ime == i.Sucelje }):
		i.Kroz = IzlazVPN
	}
	return i
}

// suceljeAdrese je ime sučelja koje ima adresu; prazno kad je nema nijedno
func suceljeAdrese(sva []sucelje, lok netip.Addr) string {
	for _, s := range sva {
		for _, a := range s.adrese {
			if p, ok := prefiksIPv4(a); ok && p.Addr() == lok {
				return s.ime
			}
		}
	}
	return ""
}
