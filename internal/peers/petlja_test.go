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
	adresa := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 250; i++ {
		if c, err := net.Dial("tcp", adresa); err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("port %d ne prima veze", port)
}

// naPetlji pokreće razmjenu čvora na slobodnom portu dok test traje
func naPetlji(t *testing.T, s *Service) {
	t.Helper()
	s.ports.Exchange = slobodanPort(t)
	ctx, otkazi := context.WithCancel(ctxRub)
	t.Cleanup(otkazi)
	go func() { _ = s.Serve(ctx) }()
	cekajPort(t, s.ports.Exchange)
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
