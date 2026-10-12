package peers

// Čvorovi koji razmjenjuju preko loopbacka, bez uparivanja: članstvo izda
// izmišljeni osnivač mreže (primiIzravno), a adresu drugoga test upiše sam.

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// slobodanPort je TCP port na loopbacku koji je upravo bio slobodan
func slobodanPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// cekajPort čeka da port na loopbacku primi vezu
func cekajPort(t *testing.T, port int) {
	t.Helper()
	if !portPrima(port, nil, 30*time.Second) {
		t.Fatalf("port %d ne prima veze", port)
	}
}

// portPrima čeka da port na loopbacku primi vezu, najviše rok; odustaje
// kad slušalica javi da je stala (npr. port je u međuvremenu uzeo drugi)
func portPrima(port int, stala <-chan error, rok time.Duration) bool {
	adresa := fmt.Sprintf("127.0.0.1:%d", port)
	kraj := time.Now().Add(rok)
	for time.Now().Before(kraj) {
		select {
		case <-stala:
			return false
		default:
		}
		if c, err := net.Dial("tcp", adresa); err == nil {
			c.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// naPetlji pokreće razmjenu čvora na slobodnom portu dok test traje. Port
// je slobodan kad ga test izabere, ali ga do pokretanja može uzeti test
// iz drugog paketa; tada se bira drugi.
func naPetlji(t *testing.T, s *Service) {
	t.Helper()
	for pokusaj := 0; pokusaj < 5; pokusaj++ {
		s.ports.Exchange = slobodanPort(t)
		ctx, otkazi := context.WithCancel(ctxRub)
		stala := make(chan error, 1)
		go func() { stala <- s.Serve(ctx) }()
		if portPrima(s.ports.Exchange, stala, 30*time.Second) {
			t.Cleanup(otkazi)
			return
		}
		otkazi()
	}
	t.Fatalf("%s: razmjena se ne da pokrenuti ni na jednom portu", s.node.ID)
}

// adresaNaPetlji je adresa porta razmjene čvora na loopbacku
func adresaNaPetlji(s *Service) string {
	return fmt.Sprintf("127.0.0.1:%d", s.ports.Exchange)
}

// upoznaj upisuje čvoru a čvor b sa zadanim adresama
func upoznaj(t *testing.T, a, b *Service, adrese ...string) {
	t.Helper()
	if err := a.SavePeer(ctxRub, Peer{NodeID: b.node.ID, Name: b.node.Name, PublicKey: b.node.PublicKey(), Addresses: adrese}); err != nil {
		t.Fatal(err)
	}
}

// clanovi su čvorovi iste mreže koji znaju jedan za drugoga i za članstvo
// drugoga, a razmjene primaju na loopbacku
func clanovi(t *testing.T, ids ...string) []*Service {
	t.Helper()
	osnivac := rubnaMreza(t, "osnivac-mreze")
	var out []*Service
	var clanstva []razmjena.Membership
	for _, id := range ids {
		s := rubniCvor(t, id)
		clanstva = append(clanstva, primiIzravno(t, osnivac, s))
		naPetlji(t, s)
		out = append(out, s)
	}
	for i, a := range out {
		for j, b := range out {
			if i == j {
				continue
			}
			if err := a.saveMembership(ctxRub, clanstva[j]); err != nil {
				t.Fatal(err)
			}
			upoznaj(t, a, b, adresaNaPetlji(b))
		}
	}
	return out
}

// prerusenaSlusalica predaje veze koje izgledaju kao da su stigle na adresu
// lokalna i s adrese udaljena (npr. Tailscale), a idu loopbackom
type prerusenaSlusalica struct {
	net.Listener
	lokalna, udaljena net.Addr
}

func (l prerusenaSlusalica) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return prerusenaVeza{Conn: c, lokalna: l.lokalna, udaljena: l.udaljena}, nil
}

type prerusenaVeza struct {
	net.Conn
	lokalna, udaljena net.Addr
}

func (v prerusenaVeza) LocalAddr() net.Addr {
	if v.lokalna != nil {
		return v.lokalna
	}
	return v.Conn.LocalAddr()
}

func (v prerusenaVeza) RemoteAddr() net.Addr {
	if v.udaljena != nil {
		return v.udaljena
	}
	return v.Conn.RemoteAddr()
}

// tcpAdresa je adresa veze za prerušenu slušalicu
func tcpAdresa(t *testing.T, a string) net.Addr {
	t.Helper()
	ap, err := net.ResolveTCPAddr("tcp", a)
	if err != nil {
		t.Fatal(err)
	}
	return ap
}

// prerusenaRazmjena: čvor s prima razmjene i na drugom portu, preko
// prerušene slušalice; vraća adresu tog porta na loopbacku
func prerusenaRazmjena(t *testing.T, s *Service, lokalna, udaljena net.Addr) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, otkazi := context.WithCancel(ctxRub)
	t.Cleanup(otkazi)
	sl := prerusenaSlusalica{Listener: ln, lokalna: lokalna, udaljena: udaljena}
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, s.node.key, Protocol, s.vjerodajnice, sl, s.trusted, func(c *razmjena.Conn) { s.primiRazmjenu(ctx, c) })
	}()
	return ln.Addr().String()
}

// cekajDa čeka uvjet koji druga strana ispuni u svojoj gorutini (npr.
// bilješku o razmjeni nakon što je pozivatelj već završio)
func cekajDa(t *testing.T, opis string, uvjet func() bool) {
	t.Helper()
	for i := 0; i < 250; i++ {
		if uvjet() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nije se dogodilo: %s", opis)
}

// izmisljeniTailnet spaja čvorove u izmišljenu Tailscale mrežu. Čvor i
// dobije adresu 100.101.0.(i+1). Veza na Tailscale adresu drugog čvora ide
// loopbackom, prerušena tako da obje strane vide Tailscale adrese, kao da je
// stigla preko Tailscalea. Ostale adrese čvor zove kao i inače, a Tailscale
// adresu izvan ove mreže ne može nazvati. Port uparivanja mora biti zadan
// prije poziva.
func izmisljeniTailnet(t *testing.T, cvorovi ...*Service) {
	t.Helper()
	ip := map[*Service]string{}
	for i, s := range cvorovi {
		ip[s] = fmt.Sprintf("100.101.0.%d", i+1)
		s.ZadajTailscale(adresa(ip[s]))
	}
	for _, a := range cvorovi {
		lokalna := tcpAdresa(t, ip[a]+":50000")
		putevi := map[string]string{} // Tailscale adresa s portom → loopback
		for _, b := range cvorovi {
			if a == b {
				continue
			}
			naB := fmt.Sprintf("%s:%d", ip[b], b.ports.Exchange)
			putevi[naB] = prerusenaRazmjena(t, b, tcpAdresa(t, naB), lokalna)
			if b.ports.Pair > 0 {
				putevi[fmt.Sprintf("%s:%d", ip[b], b.ports.Pair)] = fmt.Sprintf("127.0.0.1:%d", b.ports.Pair)
			}
		}
		a.dial = func(ctx context.Context, adresa string) (net.Conn, error) {
			return nazoviUTailnetu(ctx, adresa, putevi, lokalna)
		}
	}
}

// nazoviUTailnetu otvara vezu za čvor u izmišljenoj Tailscale mreži
func nazoviUTailnetu(ctx context.Context, adresa string, putevi map[string]string, lokalna net.Addr) (net.Conn, error) {
	var d net.Dialer
	cilj, ok := putevi[adresa]
	if !ok && jeTailscaleAdresa(adresa) {
		return nil, fmt.Errorf("dial tcp %s: i/o timeout", adresa)
	}
	if !ok {
		return d.DialContext(ctx, "tcp", adresa)
	}
	c, err := d.DialContext(ctx, "tcp", cilj)
	if err != nil {
		return nil, err
	}
	udaljena, _ := net.ResolveTCPAddr("tcp", adresa)
	return prerusenaVeza{Conn: c, lokalna: lokalna, udaljena: udaljena}, nil
}

// upari: a nazove b na adresi i oba čovjeka potvrde kod
func upari(t *testing.T, a, b *Service, adresa string) {
	t.Helper()
	if err := b.StartListening(); err != nil {
		t.Fatal(err)
	}
	var err error
	for i := 0; i < 200; i++ {
		if err = a.DialPair(ctxRub, adresa); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("nazivanje uparivanja: %v", err)
	}
	cekajDa(t, "b vidi kod uparivanja", func() bool { return b.PairStatus().Pending })
	ishodB := make(chan PairOutcome, 1)
	go func() {
		o, err := b.ConfirmPair(ctxRub, true, true)
		if err != nil {
			t.Error(err)
		}
		ishodB <- o
	}()
	o, err := a.ConfirmPair(ctxRub, true, true)
	if err != nil || !o.Paired {
		t.Fatalf("potvrda na %s: %+v %v", a.node.ID, o, err)
	}
	if o := <-ishodB; !o.Paired {
		t.Fatalf("potvrda na %s: %+v", b.node.ID, o)
	}
}
