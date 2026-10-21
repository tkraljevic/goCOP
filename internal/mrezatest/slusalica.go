package mrezatest

import (
	"context"
	"net"
	"sync"
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

// probudi budi Nazovi koji čeka mjesto u zaostatku; zove se kad mu istekne
// ili se otkaže ctx
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
