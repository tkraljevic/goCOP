package web

// Odakle je zahtjev došao. Javni čvor stoji iza Cloudflare tunela: TCP
// druga strana je cloudflared na istom stroju (privatna adresa), a pravi
// klijent stoji u zaglavlju CF-Connecting-IP koje cloudflared uvijek
// postavi i prepiše ono što je klijent poslao. Zato:
//
//   - adresa klijenta iz zaglavlja vrijedi samo kad je TCP druga strana
//     pouzdan posrednik (zadano: ovo računalo i privatne mreže, kao dosad;
//     u gocop.toml [web] pouzdani_posrednici se suzi na stvarni posrednik);
//   - zahtjev koji nosi zaglavlje posrednika je vanjski, bez obzira na to
//     tko ga je poslao: s interneta kroz tunel ga ima uvijek, a izravan
//     klijent u lokalnoj mreži ga podmetanjem samo sebi postroži pravila.
//
// Odluke koje smiju vrijediti samo za izravnog klijenta (uparivanje bez
// prijave na svježem čvoru, zadana lozinka) gledaju KrozPosrednika; brojanje
// pokušaja prijave gleda Adresa.

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Klijent je ono što poslužitelj zna o porijeklu zahtjeva
type Klijent struct {
	Adresa         netip.Addr // klijent: iz zaglavlja pouzdanog posrednika, inače TCP druga strana
	Posrednik      netip.Addr // TCP druga strana (posrednik ili sam klijent)
	KrozPosrednika bool       // zahtjev nosi zaglavlje posrednika: došao je izvana (tunel)
	HTTPS          bool       // izvorni zahtjev bio je HTTPS (ovdje ili kod pouzdanog posrednika)
}

// String je adresa klijenta za ograničenje pokušaja i zapis sesije
func (k Klijent) String() string {
	if !k.Adresa.IsValid() {
		return ""
	}
	return k.Adresa.String()
}

// Posrednici su TCP adrese kojima se vjeruje zaglavlje s adresom klijenta
type Posrednici struct {
	mreze     []netip.Prefix
	zaglavlje string

	mu         sync.Mutex
	javljeno   map[netip.Addr]time.Time // nepouzdani posrednici, javljeni u dnevnik
	javljenoOd time.Time
}

// ZaglavljeCloudflare je zaglavlje u koje cloudflared upisuje klijenta
const ZaglavljeCloudflare = "CF-Connecting-IP"

// zaglavljaPosrednika označavaju da je zahtjev prošao kroz posrednika
var zaglavljaPosrednika = []string{ZaglavljeCloudflare, "X-Forwarded-For", "Forwarded", "X-Real-IP"}

// zadaneMreze su ovo računalo i privatne mreže: tunel stoji na istom stroju
// ili u Dockerovoj mreži, a adresa iz koje cloudflared dolazi nije unaprijed
// poznata
var zadaneMreze = []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

// ZadaniPosrednici vjeruju ovom računalu i privatnim mrežama
func ZadaniPosrednici() *Posrednici {
	p, _ := NoviPosrednici(nil, "")
	return p
}

// NoviPosrednici slaže popis iz adresa ili mreža (CIDR); prazan popis su
// zadane mreže. Zaglavlje je ono iz kojeg se čita klijent, prazno =
// CF-Connecting-IP.
func NoviPosrednici(popis []string, zaglavlje string) (*Posrednici, error) {
	if len(popis) == 0 {
		popis = zadaneMreze
	}
	p := &Posrednici{zaglavlje: http.CanonicalHeaderKey(strings.TrimSpace(zaglavlje)), javljeno: map[netip.Addr]time.Time{}}
	if p.zaglavlje == "" {
		p.zaglavlje = http.CanonicalHeaderKey(ZaglavljeCloudflare)
	}
	for _, s := range popis {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, fmt.Errorf("pouzdani posrednik %q nije adresa ni mreža", s)
			}
			s = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()).String()
		}
		m, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("pouzdani posrednik %q nije adresa ni mreža", s)
		}
		p.mreze = append(p.mreze, m.Masked())
	}
	if len(p.mreze) == 0 { // popis samo s praznim stavkama
		return NoviPosrednici(nil, zaglavlje)
	}
	return p, nil
}

func (p *Posrednici) pouzdan(a netip.Addr) bool {
	for _, m := range p.mreze {
		if m.Contains(a) {
			return true
		}
	}
	return false
}

// Klijent određuje porijeklo zahtjeva
func (p *Posrednici) Klijent(r *http.Request) Klijent {
	var k Klijent
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	if a, err := netip.ParseAddr(host); err == nil {
		k.Posrednik = a.Unmap()
	}
	k.Adresa = k.Posrednik
	k.KrozPosrednika = r.Header.Get(p.zaglavlje) != ""
	for _, z := range zaglavljaPosrednika {
		if r.Header.Get(z) != "" {
			k.KrozPosrednika = true
			break
		}
	}
	k.HTTPS = r.TLS != nil
	if !k.Posrednik.IsValid() || !p.pouzdan(k.Posrednik) {
		if r.Header.Get(p.zaglavlje) != "" {
			p.javiNepouzdan(k.Posrednik)
		}
		return k
	}
	if a, ok := adresaIzZaglavlja(r.Header.Get(p.zaglavlje), p); ok {
		k.Adresa = a
	}
	if proslijedenHTTPS(r) {
		k.HTTPS = true
	}
	return k
}

// proslijedenHTTPS: posrednik javlja da je preglednik do njega došao
// preko HTTPS-a (X-Forwarded-Proto ili Cloudflareov Cf-Visitor)
func proslijedenHTTPS(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") ||
		strings.Contains(strings.ReplaceAll(r.Header.Get("Cf-Visitor"), " ", ""), `"scheme":"https"`)
}

// javiNepouzdan jednom na sat zapiše posrednika koji šalje zaglavlje
// klijenta, a nije na popisu: krivo zadan popis inače bi sve korisnike s
// interneta tiho stopio u jednu adresu (i jedno ograničenje pokušaja)
func (p *Posrednici) javiNepouzdan(a netip.Addr) {
	if !a.IsValid() || a.IsGlobalUnicast() && !a.IsPrivate() {
		return // s interneta izravno: podmetnuto zaglavlje, ne krivo podešen posrednik
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.javljeno[a]; ok && time.Since(t) < time.Hour {
		return
	}
	// najviše 20 različitih na sat, da se dnevnik ne da puniti mijenjanjem adrese
	if time.Since(p.javljenoOd) > time.Hour {
		p.javljeno, p.javljenoOd = map[netip.Addr]time.Time{}, time.Now()
	}
	if len(p.javljeno) >= 20 {
		return
	}
	p.javljeno[a] = time.Now()
	log.Printf("web: %s šalje %s, a nije među pouzdanim posrednicima; ako je to tunel ili obratni posrednik, dodajte ga u gocop.toml [web] pouzdani_posrednici", a, p.zaglavlje)
}

// adresaIzZaglavlja čita adresu klijenta. Popis (X-Forwarded-For) čita se
// zdesna, preskačući pouzdane posrednike: lijevi kraj piše klijent sam.
func adresaIzZaglavlja(v string, p *Posrednici) (netip.Addr, bool) {
	dijelovi := strings.Split(v, ",")
	for i := len(dijelovi) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(dijelovi[i]))
		if err != nil {
			return netip.Addr{}, false
		}
		a = a.Unmap()
		if i > 0 && p.pouzdan(a) {
			continue
		}
		return a, true
	}
	return netip.Addr{}, false
}

type kljucKlijenta struct{}

// SetPosrednici zadaje pouzdane posrednike (gocop.toml [web])
func (s *Server) SetPosrednici(p *Posrednici) {
	if p != nil {
		s.posrednici = p
	}
}

func (s *Server) posredniciIliZadani() *Posrednici {
	if s.posrednici != nil {
		return s.posrednici
	}
	return ZadaniPosrednici()
}

// klijentSloj jednom odredi porijeklo zahtjeva i spremi ga u kontekst
func (s *Server) klijentSloj(next http.Handler) http.Handler {
	p := s.posredniciIliZadani()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := p.Klijent(r)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), kljucKlijenta{}, k)))
	})
}

// klijentIz vraća porijeklo zahtjeva; bez sloja (testovi rukovatelja)
// računa ga sa zadanim posrednicima
func klijentIz(r *http.Request) Klijent {
	if k, ok := r.Context().Value(kljucKlijenta{}).(Klijent); ok {
		return k
	}
	return ZadaniPosrednici().Klijent(r)
}
