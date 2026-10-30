package javnivodostaji

// Iz adrese javne stranice uzima se samo oznaka postaje, a stranica se
// dohvaća s poznatog poslužitelja izvora. Klijent ne slijedi preusmjeravanje
// na drugi poslužitelj i ne spaja se na adrese u lokalnoj mreži.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// adresaIzvora rastavlja adresu i javlja je li na poslužitelju izvora: ta
// domena ili njezina poddomena, http ili https, bez korisničkog imena i bez
// posebnog porta. Adresa upisana bez https:// čita se kao da ga ima.
func adresaIzvora(adresa, domena string) (*url.URL, bool) {
	u, ok := rastaviAdresu(adresa)
	if !ok {
		return nil, false
	}
	return u, naDomeni(u.Hostname(), domena)
}

// adresaNaPosluzitelju je kao adresaIzvora, ali poslužitelj mora biti točno
// jedan od zadanih, bez poddomena
func adresaNaPosluzitelju(adresa string, posluzitelji ...string) (*url.URL, bool) {
	u, ok := rastaviAdresu(adresa)
	if !ok {
		return nil, false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	for _, p := range posluzitelji {
		if host == p {
			return u, true
		}
	}
	return nil, false
}

// rastaviAdresu rastavlja adresu javne stranice: http ili https, bez
// korisničkog imena i bez posebnog porta
func rastaviAdresu(adresa string) (*url.URL, bool) {
	a := strings.TrimSpace(adresa)
	if a != "" && !strings.Contains(a, "://") {
		a = "https://" + strings.TrimPrefix(a, "//")
	}
	u, err := url.Parse(a)
	if err != nil || u.User != nil || u.Port() != "" {
		return nil, false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, false
	}
	return u, true
}

// naDomeni javlja je li poslužitelj ta domena ili njezina poddomena
func naDomeni(host, domena string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == domena || strings.HasSuffix(host, "."+domena)
}

// dioIzvora vraća put i upit adrese na poslužitelju izvora; prazno kad
// adresa nije s tog poslužitelja
func dioIzvora(adresa, domena string) (put, upit string) {
	u, ok := adresaIzvora(adresa, domena)
	if !ok {
		return "", ""
	}
	return u.Path, u.RawQuery
}

// zaDohvat je adresa kojom se izvor stvarno čita: uvijek https, bez
// fragmenta, samo poslužitelj, put i upit. Prazno kad adresa nije s
// poslužitelja izvora.
func zaDohvat(adresa, domena string) string {
	u, ok := adresaIzvora(adresa, domena)
	if !ok {
		return ""
	}
	v := url.URL{Scheme: "https", Host: strings.ToLower(u.Hostname()),
		Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
	return v.String()
}

// errLokalnaAdresa javlja da je poslužitelj izvora na adresi lokalne mreže
var errLokalnaAdresa = errors.New("poslužitelj izvora javlja se s adrese u lokalnoj mreži, a s nje se ne preuzima")

// cgnat su zajedničke adrese davatelja i Tailscalea, unutarnje kao i privatne
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// lokalnaAdresa javlja je li IP u lokalnoj, privatnoj ili posebnoj mreži
func lokalnaAdresa(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() {
		return true
	}
	return ip.IsPrivate() || cgnat.Contains(ip)
}

// provjeriSpoj odbija spoj na lokalnu adresu; poziva se s već nađenim IP-om
func provjeriSpoj(_, adresa string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(adresa)
	if err != nil {
		return err
	}
	if lokalnaAdresa(ap.Addr()) {
		return errLokalnaAdresa
	}
	return nil
}

// domenaHV su poslužitelji Hrvatskih voda. U službenoj mreži mogu se javiti
// s unutarnje adrese, a adresa za njih nikad ne dolazi iz registra.
const domenaHV = "voda.hr"

// spajac otvara veze za javne izvore. Posrednik iz okoline (HTTPS_PROXY) i
// poslužitelji Hrvatskih voda smiju biti u lokalnoj mreži; ostalo ne smije.
type spajac struct {
	cuvan, slobodan *net.Dialer
	posrednik       func(*http.Request) (*url.URL, error)
}

func noviSpajac() spajac {
	return spajac{
		cuvan:     &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: provjeriSpoj},
		slobodan:  &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second},
		posrednik: http.ProxyFromEnvironment,
	}
}

// DialContext bira čuvan ili slobodan spoj po poslužitelju
func (s spajac) DialContext(ctx context.Context, mreza, adresa string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(adresa)
	if err != nil {
		return nil, err
	}
	if s.cuva(host) {
		return s.cuvan.DialContext(ctx, mreza, adresa)
	}
	return s.slobodan.DialContext(ctx, mreza, adresa)
}

// cuva javlja ide li spoj na host kroz provjeru lokalne adrese
func (s spajac) cuva(host string) bool {
	return !naDomeni(host, domenaHV) && !s.jePosrednik(host)
}

// jePosrednik javlja je li host posrednik zadan u okolini programa
func (s spajac) jePosrednik(host string) bool {
	for _, primjer := range []string{"https://primjer.invalid/", "http://primjer.invalid/"} {
		req, err := http.NewRequest(http.MethodGet, primjer, nil)
		if err != nil {
			continue
		}
		if p, err := s.posrednik(req); err == nil && p != nil && strings.EqualFold(p.Hostname(), host) {
			return true
		}
	}
	return false
}

// srodanPosluzitelj javlja smije li se preusmjeravanje slijediti: isti
// poslužitelj, isti uz www ili bez njega, ili njegova poddomena.
func srodanPosluzitelj(prvi, novi *url.URL) bool {
	if port(prvi) != port(novi) {
		return false
	}
	a := strings.TrimPrefix(strings.ToLower(prvi.Hostname()), "www.")
	b := strings.TrimPrefix(strings.ToLower(novi.Hostname()), "www.")
	return a == b || strings.HasSuffix(b, "."+a)
}

// port je upisani port adrese; prazno kad je upisan zadani za shemu ili
// nijedan, pa prelazak s http na https ostaje dopušten
func port(u *url.URL) string {
	p := u.Port()
	if (p == "443" && strings.EqualFold(u.Scheme, "https")) || (p == "80" && strings.EqualFold(u.Scheme, "http")) {
		return ""
	}
	return p
}

// istiPosluzitelj slijedi preusmjeravanje samo unutar poslužitelja izvora
func istiPosluzitelj(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("izvor preusmjerava previše puta")
	}
	prvi := via[0].URL
	if prvi.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("izvor preusmjerava s https na http, a to se ne slijedi")
	}
	if !srodanPosluzitelj(prvi, req.URL) {
		return fmt.Errorf("izvor preusmjerava na drugi poslužitelj (%s), a to se ne slijedi", req.URL.Hostname())
	}
	return nil
}

// javniKlijent je HTTP klijent za javne izvore, s rokom i zadanim TLS-om
func javniKlijent(rok time.Duration, tlsCfg *tls.Config) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = tlsCfg
	t.DialContext = noviSpajac().DialContext
	return &http.Client{Timeout: rok, Transport: t, CheckRedirect: istiPosluzitelj}
}

// zadaniKlijent je klijent kojim se izvori čitaju kad test ne podmetne svoj
var zadaniKlijent = sync.OnceValue(func() *http.Client { return javniKlijent(20*time.Second, nil) })
