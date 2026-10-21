package mrezatest

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Pozivatelj dobiva izvorne portove redom iz raspona privremenih portova po
// IANA-i (49152–65535), kao na pravom računalu, a nakon zadnjeg kreće
// ispočetka. Tako i simulacija od više dana ima samo ispravne brojeve portova.
const (
	prviIzvorniPort = 49152
	izvornihPortova = 65536 - prviIzvorniPort
)

// Mreza drži slušalice po adresi, za kod koji sam bira kamo zove
// (http.Transport.DialContext, klijent tunela, s.dial u peers). Nulta
// vrijednost je prazna mreža, kao i NovaMreza.
type Mreza struct {
	// Kasnjenje je jednosmjerno kašnjenje novih veza, pa pitanje i odgovor
	// traju dva kašnjenja. Postavlja se prije prvog poziva.
	Kasnjenje time.Duration

	mu        sync.Mutex
	slusalice map[string]*Slusalica
	pozvano   int // koliko je izvornih portova potrošeno u krugu raspona
}

// NovaMreza je mreža bez ijedne slušalice
func NovaMreza() *Mreza { return &Mreza{} }

// Slusaj otvara slušalicu na adresi, npr. "cop.primjer.hr:443". Zauzeta
// adresa vraća grešku kao kod TCP-a (address already in use).
func (m *Mreza) Slusaj(adresa string) (*Slusalica, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.slusalice == nil {
		m.slusalice = map[string]*Slusalica{}
	}
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
// portom. Greške su kao kod net.Dialer: adresa na kojoj nitko ne sluša, i
// slušalica zatvorena dok poziv čeka u punom zaostatku, vraćaju *net.OpError
// sa syscall.ECONNREFUSED, a istek i otkaz konteksta daju "i/o timeout" i
// "operation was canceled".
func (m *Mreza) DialContext(ctx context.Context, network, adresa string) (net.Conn, error) {
	return m.nazovi(ctx, "127.0.0.1", network, adresa)
}

// DialContextOd je DialContext računala s IP adresom ip: druga strana vidi
// njegove veze s te adrese i novog porta. Tako u testu s više računala na
// jednoj mreži svako zove sa svoje adrese, kao na pravoj mreži.
func (m *Mreza) DialContextOd(ip string) func(ctx context.Context, network, adresa string) (net.Conn, error) {
	return func(ctx context.Context, network, adresa string) (net.Conn, error) {
		return m.nazovi(ctx, ip, network, adresa)
	}
}

// nazovi zove slušalicu na adresi s IP adrese ip. Kontekst se provjerava
// prije svega, kao kod net.Dialer.
func (m *Mreza) nazovi(ctx context.Context, ip, network, adresa string) (net.Conn, error) {
	cilj := Adresa{Mreza: network, Tekst: adresa}
	if err := ctx.Err(); err != nil {
		return nil, greskaPoziva(cilj, err)
	}
	s, odakle := m.slusalicaIIzvor(ip, network, adresa)
	if s == nil {
		return nil, greskaPoziva(cilj, net.ErrClosed)
	}
	c, err := s.Nazovi(ctx, odakle)
	if err != nil {
		return nil, greskaPoziva(cilj, err)
	}
	return c, nil
}

// slusalicaIIzvor vraća slušalicu na adresi (nil kad je nema) i adresu
// pozivatelja s idućim izvornim portom
func (m *Mreza) slusalicaIIzvor(ip, network, adresa string) (*Slusalica, net.Addr) {
	m.mu.Lock()
	defer m.mu.Unlock()
	port := prviIzvorniPort + m.pozvano
	m.pozvano = (m.pozvano + 1) % izvornihPortova
	return m.slusalice[adresa], Adresa{Mreza: network, Tekst: net.JoinHostPort(ip, strconv.Itoa(port))}
}

// greskaPoziva vraća grešku koju za isti slučaj vraća net.Dialer, jer
// program tekst greške pokazuje korisniku uz adresu: bez izvorne adrese,
// istek i otkaz konteksta starim tekstom paketa net, a slušalica koje nema
// ili je zatvorena odbija poziv
func greskaPoziva(cilj net.Addr, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		err = istekPoziva{}
	case errors.Is(err, context.Canceled):
		err = otkazanPoziv{}
	default:
		err = os.NewSyscallError("connect", syscall.ECONNREFUSED)
	}
	return greska("dial", nil, cilj, err)
}

// istekPoziva je greška paketa net kad poziv ne stigne do roka konteksta:
// tekst "i/o timeout", net.Error s Timeout, a errors.Is je prepoznaje kao
// context.DeadlineExceeded
type istekPoziva struct{}

func (istekPoziva) Error() string     { return "i/o timeout" }
func (istekPoziva) Timeout() bool     { return true }
func (istekPoziva) Is(err error) bool { return err == context.DeadlineExceeded }

// otkazanPoziv je greška paketa net kad se kontekst poziva otkaže
type otkazanPoziv struct{}

func (otkazanPoziv) Error() string     { return "operation was canceled" }
func (otkazanPoziv) Is(err error) bool { return err == context.Canceled }

// zaboravi oslobađa adresu zatvorene slušalice. Dok je slušalica otvorena,
// adresu nitko drugi ne može uzeti, a zatvara se samo jednom.
func (m *Mreza) zaboravi(s *Slusalica) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.slusalice, s.adresa.String())
}
