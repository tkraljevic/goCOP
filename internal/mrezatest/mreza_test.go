package mrezatest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// Mreža zove slušalicu po adresi. Na adresi na kojoj nitko ne sluša poziv se
// odbija kao kod TCP-a (connection refused), zauzeta adresa se ne može
// ponovno otvoriti, svaki poziv dobije novi izvorni port, a zatvorena
// slušalica oslobađa adresu.
func TestMrezaPoAdresi(t *testing.T) {
	m := NovaMreza()
	_, err := m.DialContext(context.Background(), "tcp", "192.0.2.9:4710")
	var op *net.OpError
	if !errors.Is(err, syscall.ECONNREFUSED) || !errors.As(err, &op) || op.Op != "dial" || op.Addr.String() != "192.0.2.9:4710" {
		t.Errorf("nepoznata adresa: %v", err)
	}

	s, err := m.Slusaj("cop.primjer.hr:443")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Slusaj("cop.primjer.hr:443"); !errors.Is(err, syscall.EADDRINUSE) {
		t.Errorf("zauzeta adresa: %v", err)
	}
	k1, err := m.DialContext(context.Background(), "tcp", "cop.primjer.hr:443")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := m.DialContext(context.Background(), "tcp", "cop.primjer.hr:443")
	if err != nil {
		t.Fatal(err)
	}
	p1, err := s.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if k1.RemoteAddr().String() != "cop.primjer.hr:443" || p1.RemoteAddr() != k1.LocalAddr() || k1.LocalAddr() == k2.LocalAddr() {
		t.Errorf("adrese: %v→%v, %v, poslužitelj vidi %v", k1.LocalAddr(), k1.RemoteAddr(), k2.LocalAddr(), p1.RemoteAddr())
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DialContext(context.Background(), "tcp", "cop.primjer.hr:443"); !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("poziv zatvorene slušalice: %v", err)
	}
	if _, err := m.Slusaj("cop.primjer.hr:443"); err != nil {
		t.Errorf("adresa zatvorene slušalice: %v", err)
	}
}

// Mreža vraća iste greške kao net.Dialer, jer program tekst greške pokazuje
// uz adresu. Za istek i otkaz konteksta tekst se uspoređuje s onim što za
// istu adresu vrati net.Dialer, koji se tada ni ne pokuša spojiti. Kontekst
// se gleda prije adrese, pa ni adresa na kojoj nitko ne sluša ne mijenja
// grešku.
func TestGreskePozivaKaoNetDialer(t *testing.T) {
	const adresa = "192.0.2.9:4710"
	otkazan, otkazi := context.WithCancel(context.Background())
	otkazi()
	istekao, stani := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stani()
	slucajevi := []struct {
		ime    string
		ctx    context.Context
		greska error
		istek  bool
	}{
		{"otkazan kontekst", otkazan, context.Canceled, false},
		{"istekao kontekst", istekao, context.DeadlineExceeded, true},
		{"nitko ne sluša", context.Background(), syscall.ECONNREFUSED, false},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			ocekivano := "dial tcp 192.0.2.9:4710: connect: connection refused"
			if sl.ctx.Err() != nil {
				_, pravi := (&net.Dialer{}).DialContext(sl.ctx, "tcp", adresa)
				ocekivano = fmt.Sprint(pravi)
			}
			_, err := NovaMreza().DialContext(sl.ctx, "tcp", adresa)
			var ne net.Error
			if err == nil || err.Error() != ocekivano || !errors.Is(err, sl.greska) || !errors.As(err, &ne) || ne.Timeout() != sl.istek {
				t.Errorf("greška %v, a očekivano %q", err, ocekivano)
			}
		})
	}
}

// Poziv koji čeka mjesto u punom zaostatku završi kao na pravoj mreži:
// istekom konteksta s "i/o timeout", a kad se slušalica zatvori, odbijen
// (connection refused), a ne greškom zatvorene slušalice
func TestPozivKojiCekaPunZaostatak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const adresa = "192.0.2.2:4710"
		m := NovaMreza()
		s, err := m.Slusaj(adresa)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close() // pusti poziv koji čeka i kad test stane prije kraja
		for range zaostatak {
			if _, err := m.DialContext(context.Background(), "tcp", adresa); err != nil {
				t.Fatal(err)
			}
		}

		ctx, otkazi := context.WithTimeout(context.Background(), time.Second)
		defer otkazi()
		_, err = m.DialContext(ctx, "tcp", adresa)
		if err == nil || err.Error() != "dial tcp 192.0.2.2:4710: i/o timeout" || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("istek dok poziv čeka: %v", err)
		}

		poziv := make(chan error, 1)
		go func() {
			_, err := m.DialContext(context.Background(), "tcp", adresa)
			poziv <- err
		}()
		synctest.Wait()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-poziv; err == nil || err.Error() != "dial tcp 192.0.2.2:4710: connect: connection refused" {
			t.Errorf("zatvaranje dok poziv čeka: %v", err)
		}
	})
}

// Izvorni portovi idu redom od 49152, a iza 65535 opet kreću od 49152, pa
// ostaju ispravni i nakon mnogo poziva
func TestIzvorniPortoviOstajuURasponu(t *testing.T) {
	m := NovaMreza()
	if _, err := m.Slusaj("192.0.2.2:4710"); err != nil {
		t.Fatal(err)
	}
	var portovi []string
	poziv := func() {
		k, err := m.DialContext(context.Background(), "tcp", "192.0.2.2:4710")
		if err != nil {
			t.Fatal(err)
		}
		portovi = append(portovi, k.LocalAddr().String())
	}
	poziv()
	m.pozvano = izvornihPortova - 1 // kao nakon 16383 poziva
	poziv()
	poziv()
	if fmt.Sprint(portovi) != "[127.0.0.1:49152 127.0.0.1:65535 127.0.0.1:49152]" {
		t.Errorf("izvorni portovi: %v", portovi)
	}
}

// Računalo koje zove sa svoje adrese: slušalica vidi tu adresu s novim
// portom, a IPv6 adresu u uglatim zagradama
func TestDialContextOd(t *testing.T) {
	m := NovaMreza()
	s, err := m.Slusaj("192.0.2.2:4710")
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"192.168.24.92", "fd12::1"} {
		k, err := m.DialContextOd(ip)(context.Background(), "tcp", "192.0.2.2:4710")
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.Accept()
		if err != nil {
			t.Fatal(err)
		}
		host, _, err := net.SplitHostPort(p.RemoteAddr().String())
		if err != nil || host != ip || p.RemoteAddr() != k.LocalAddr() {
			t.Errorf("poslužitelj vidi %v, a pozivatelj je %v", p.RemoteAddr(), k.LocalAddr())
		}
	}
}

// Kašnjenje mreže vrijedi za svaku novu vezu: krug (pitanje i odgovor)
// traje točno dva kašnjenja. Mreža je ovdje nulta vrijednost s kašnjenjem,
// bez NovaMreza, jer i tako mora raditi.
func TestKrugPrekoMrezeTrajeDvaKasnjenja(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &Mreza{Kasnjenje: 25 * time.Millisecond}
		s, err := m.Slusaj("192.0.2.2:4710")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		go func() {
			p, err := s.Accept()
			if err != nil {
				t.Error(err)
				return
			}
			defer p.Close()
			b := make([]byte, 1)
			if _, err := io.ReadFull(p, b); err != nil {
				t.Error(err)
				return
			}
			if _, err := p.Write(b); err != nil {
				t.Error(err)
			}
		}()
		k, err := m.DialContext(context.Background(), "tcp", "192.0.2.2:4710")
		if err != nil {
			t.Fatal(err)
		}
		defer k.Close()
		pocetak := time.Now()
		napisi(t, k, "?")
		if got := procitaj(t, k, 1); string(got) != "?" {
			t.Errorf("odgovor: %q", got)
		}
		if d := time.Since(pocetak); d != 2*m.Kasnjenje {
			t.Errorf("krug je trajao %v, a ne %v", d, 2*m.Kasnjenje)
		}
	})
}
