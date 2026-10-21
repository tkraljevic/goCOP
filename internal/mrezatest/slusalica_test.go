package mrezatest

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

// Slušalica bez mreže: Nazovi i Accept daju dva kraja iste veze s adresama
// pozivatelja i slušalice. Nakon Close Accept i Nazovi vraćaju net.ErrClosed,
// a već prihvaćena veza radi dalje, kao kod TCP-a.
func TestSlusalica(t *testing.T) {
	s := NovaSlusalica(posluziteljeva)
	if s.Addr() != posluziteljeva {
		t.Errorf("adresa slušalice: %v", s.Addr())
	}
	k, err := s.Nazovi(context.Background(), klijentova)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if k.LocalAddr() != klijentova || k.RemoteAddr() != posluziteljeva || p.LocalAddr() != posluziteljeva || p.RemoteAddr() != klijentova {
		t.Errorf("adrese: %v→%v i %v→%v", k.LocalAddr(), k.RemoteAddr(), p.LocalAddr(), p.RemoteAddr())
	}
	napisi(t, k, "pitanje")
	if got := procitaj(t, p, 7); string(got) != "pitanje" {
		t.Errorf("poslužitelj je primio %q", got)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = s.Accept()
	var op *net.OpError
	if !errors.Is(err, net.ErrClosed) || !errors.As(err, &op) || op.Op != "accept" {
		t.Errorf("Accept nakon Close: %v", err)
	}
	if _, err := s.Nazovi(context.Background(), klijentova); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Nazovi nakon Close: %v", err)
	}
	if err := s.Close(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("drugo zatvaranje: %v", err)
	}
	napisi(t, p, "odgovor")
	if got := procitaj(t, k, 7); string(got) != "odgovor" {
		t.Errorf("klijent je primio %q", got)
	}
}

// Poslužitelj zatvara slušalicu kad mu otkažu kontekst (kao razmjena), pa
// Accept koji čeka izlazi s net.ErrClosed. Nazovi s otkazanim kontekstom
// odmah vraća grešku konteksta.
func TestAcceptINazoviSOtkazanimKontekstom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NovaSlusalica(posluziteljeva)
		ctx, otkazi := context.WithCancel(context.Background())
		stani := context.AfterFunc(ctx, func() { s.Close() })
		defer stani()
		prihvat := make(chan error, 1)
		go func() {
			_, err := s.Accept()
			prihvat <- err
		}()
		synctest.Wait()
		otkazi()
		if err := <-prihvat; !errors.Is(err, net.ErrClosed) {
			t.Errorf("Accept nakon otkazanog konteksta: %v", err)
		}
		_, err := NovaSlusalica(posluziteljeva).Nazovi(ctx, klijentova)
		var op *net.OpError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &op) || op.Op != "dial" {
			t.Errorf("Nazovi s otkazanim kontekstom: %v", err)
		}
	})
}

// Dok ih nitko ne prihvati, slušalica drži do 128 veza i Nazovi ne čeka.
// Kad je zaostatak pun, Nazovi čeka da Accept oslobodi mjesto, da istekne
// kontekst ili da se slušalica zatvori. Zatvaranje zatvara i neprihvaćene
// veze, pa njihov pozivatelj dobije io.EOF.
func TestZaostatakSlusalice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NovaSlusalica(posluziteljeva)
		defer s.Close() // pusti pozive koji čekaju i kad test stane prije kraja
		var klijenti []net.Conn
		for range zaostatak {
			k, err := s.Nazovi(context.Background(), klijentova)
			if err != nil {
				t.Fatal(err)
			}
			klijenti = append(klijenti, k)
		}

		ctx, otkazi := context.WithTimeout(context.Background(), 3*time.Second)
		defer otkazi()
		pocetak := time.Now()
		if _, err := s.Nazovi(ctx, klijentova); !errors.Is(err, context.DeadlineExceeded) || time.Since(pocetak) != 3*time.Second {
			t.Errorf("pun zaostatak: %v nakon %v", err, time.Since(pocetak))
		}

		poziv := make(chan error, 1)
		go func() {
			_, err := s.Nazovi(context.Background(), klijentova)
			poziv <- err
		}()
		synctest.Wait()
		select {
		case err := <-poziv:
			t.Fatalf("Nazovi nije čekao mjesto: %v", err)
		default:
		}
		p, err := s.Accept()
		if err != nil {
			t.Fatal(err)
		}
		if err := <-poziv; err != nil {
			t.Errorf("Nazovi nakon Accept: %v", err)
		}
		napisi(t, klijenti[0], "1")
		if got := procitaj(t, p, 1); string(got) != "1" {
			t.Errorf("prvi prihvaćen nije prvi pozvani: %q", got)
		}

		go func() {
			_, err := s.Nazovi(context.Background(), klijentova)
			poziv <- err
		}()
		synctest.Wait()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-poziv; !errors.Is(err, net.ErrClosed) {
			t.Errorf("Nazovi koji čeka, nakon Close: %v", err)
		}
		if _, err := klijenti[1].Read(make([]byte, 1)); err != io.EOF {
			t.Errorf("neprihvaćena veza nakon Close: %v", err)
		}
	})
}
