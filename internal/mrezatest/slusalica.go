package mrezatest

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

// zaostatak je koliko pozvanih veza slušalica drži dok ih netko ne prihvati.
// Pozivatelj čeka tek kad je zaostatak pun, kao kod TCP-a.
const zaostatak = 128

// Slusalica je net.Listener u memoriji. Nazovi vraća vezu odmah, ne čekajući
// Accept, dok u zaostatku ima mjesta.
type Slusalica struct {
	adresa    net.Addr
	mreza     *Mreza
	mu        sync.Mutex
	cond      *sync.Cond
	cekaju    []net.Conn // krajevi za Accept
	zatvorena bool
}

// NovaSlusalica otvara slušalicu na adresi, koja ne smije biti nil
func NovaSlusalica(adresa net.Addr) *Slusalica {
	s := &Slusalica{adresa: adresa}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Accept čeka sljedeću pozvanu vezu. Nakon Close vraća net.ErrClosed.
func (s *Slusalica) Accept() (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for !s.zatvorena && len(s.cekaju) == 0 {
		s.cond.Wait()
	}
	if s.zatvorena {
		return nil, greska("accept", nil, s.adresa, net.ErrClosed)
	}
	c := s.cekaju[0]
	s.cekaju = s.cekaju[1:]
	s.cond.Broadcast() // pozivatelj možda čeka mjesto u zaostatku
	return c, nil
}

// Close zatvara slušalicu. Veze koje još nitko nije prihvatio se zatvaraju,
// pa pozivatelj dobije io.EOF. Slušalica s mreže oslobađa svoju adresu.
func (s *Slusalica) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.zatvorena {
		return greska("close", nil, s.adresa, net.ErrClosed)
	}
	s.zatvorena = true
	for _, c := range s.cekaju {
		c.Close()
	}
	s.cekaju = nil
	s.cond.Broadcast()
	if s.mreza != nil {
		s.mreza.zaboravi(s)
	}
	return nil
}

// Addr vraća adresu slušalice
func (s *Slusalica) Addr() net.Addr { return s.adresa }

// Nazovi otvara vezu sa slušalicom; odakle je adresa pozivatelja i ne smije
// biti nil. Kad je zaostatak pun, čeka da Accept oslobodi mjesto, da istekne
// ctx ili da se slušalica zatvori; nakon Close vraća net.ErrClosed.
func (s *Slusalica) Nazovi(ctx context.Context, odakle net.Addr) (net.Conn, error) {
	stani := context.AfterFunc(ctx, s.probudi)
	defer stani()
	s.mu.Lock()
	defer s.mu.Unlock()
	for ctx.Err() == nil && !s.zatvorena && len(s.cekaju) >= zaostatak {
		s.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return nil, greska("dial", odakle, s.adresa, err)
	}
	if s.zatvorena {
		return nil, greska("dial", odakle, s.adresa, net.ErrClosed)
	}
	klijent, posluzitelj := ParSKasnjenjem(odakle, s.adresa, s.kasnjenje())
	s.cekaju = append(s.cekaju, posluzitelj)
	s.cond.Broadcast()
	return klijent, nil
}

func (s *Slusalica) probudi() {
	s.mu.Lock()
	s.cond.Broadcast()
	s.mu.Unlock()
}

// kasnjenje je kašnjenje nove veze: zadano na mreži, a bez mreže nikakvo
func (s *Slusalica) kasnjenje() time.Duration {
	if s.mreza == nil {
		return 0
	}
	return s.mreza.Kasnjenje
}

// Mreza drži slušalice po adresi, za kod koji sam bira kamo zove
// (http.Transport.DialContext, klijent tunela, s.dial u peers)
type Mreza struct {
	// Kasnjenje je jednosmjerno kašnjenje novih veza, pa pitanje i odgovor
	// traju dva kašnjenja. Postavlja se prije prvog poziva.
	Kasnjenje time.Duration

	mu        sync.Mutex
	slusalice map[string]*Slusalica
	pozvano   int // za izvorni port pozivatelja
}

// NovaMreza je mreža bez ijedne slušalice
func NovaMreza() *Mreza { return &Mreza{slusalice: map[string]*Slusalica{}} }

// Slusaj otvara slušalicu na adresi, npr. "cop.primjer.hr:443". Zauzeta
// adresa vraća grešku kao kod TCP-a (address already in use).
func (m *Mreza) Slusaj(adresa string) (*Slusalica, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.slusalice[adresa]; ok {
		return nil, greska("listen", nil, TCP(adresa), os.NewSyscallError("bind", syscall.EADDRINUSE))
	}
	s := NovaSlusalica(TCP(adresa))
	s.mreza = m
	m.slusalice[adresa] = s
	return s, nil
}

// DialContext zove slušalicu na adresi, istog oblika kao
// net.Dialer.DialContext. Pozivatelj dobiva adresu 127.0.0.1 s novim
// portom. Adresa na kojoj nitko ne sluša vraća *net.OpError sa
// syscall.ECONNREFUSED, kao kod TCP-a.
func (m *Mreza) DialContext(ctx context.Context, network, adresa string) (net.Conn, error) {
	m.mu.Lock()
	s := m.slusalice[adresa]
	m.pozvano++
	odakle := Adresa{Mreza: network, Tekst: fmt.Sprintf("127.0.0.1:%d", 49151+m.pozvano)}
	m.mu.Unlock()
	if s == nil {
		return nil, greska("dial", nil, Adresa{Mreza: network, Tekst: adresa}, os.NewSyscallError("connect", syscall.ECONNREFUSED))
	}
	return s.Nazovi(ctx, odakle)
}

// zaboravi oslobađa adresu zatvorene slušalice. Dok je slušalica otvorena,
// adresu nitko drugi ne može uzeti, a zatvara se samo jednom.
func (m *Mreza) zaboravi(s *Slusalica) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.slusalice, s.adresa.String())
}
