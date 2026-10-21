package mrezatest

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

var (
	klijentova     = TCP("192.0.2.1:50000")
	posluziteljeva = TCP("192.0.2.2:4710")
)

// ishod je što je vratilo pisanje ili čitanje u drugoj gorutini
type ishod struct {
	n   int
	err error
}

// procitaj pročita točno n bajtova ili prekine test
func procitaj(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := io.ReadFull(c, b); err != nil {
		t.Fatalf("čitanje %d bajtova: %v", n, err)
	}
	return b
}

// napisi napiše b ili prekine test
func napisi(t *testing.T, c net.Conn, b string) {
	t.Helper()
	if _, err := c.Write([]byte(b)); err != nil {
		t.Fatalf("pisanje %q: %v", b, err)
	}
}

// Adresa je samo ime mreže i tekst, a TCP daje mrežu "tcp"
func TestAdresa(t *testing.T) {
	a := TCP("192.0.2.7:4710")
	if a.Network() != "tcp" || a.String() != "192.0.2.7:4710" {
		t.Errorf("TCP adresa: %q %q", a.Network(), a.String())
	}
	var tunel net.Addr = Adresa{Mreza: "websocket", Tekst: "https://cop.primjer.hr"}
	if tunel.Network() != "websocket" || tunel.String() != "https://cop.primjer.hr" {
		t.Errorf("adresa tunela: %q %q", tunel.Network(), tunel.String())
	}
}

// Veza u stvarnom vremenu: adrese krajeva, čitanje u dijelovima, prazno
// čitanje i pisanje, zatvaranje. Druga strana nakon zatvaranja pročita sve
// što joj je poslano pa dobije io.EOF, a njezino pisanje vraća broken pipe.
func TestParCitanjePisanjeIZatvaranje(t *testing.T) {
	a, b := Par(klijentova, posluziteljeva)
	if a.LocalAddr() != klijentova || a.RemoteAddr() != posluziteljeva || b.LocalAddr() != posluziteljeva || b.RemoteAddr() != klijentova {
		t.Errorf("adrese: %v→%v i %v→%v", a.LocalAddr(), a.RemoteAddr(), b.LocalAddr(), b.RemoteAddr())
	}
	if n, err := b.Read(nil); n != 0 || err != nil {
		t.Errorf("prazno čitanje bez podataka: %d, %v", n, err)
	}
	if n, err := a.Write(nil); n != 0 || err != nil {
		t.Errorf("prazno pisanje: %d, %v", n, err)
	}
	if n, err := a.Write([]byte("pozdrav")); n != 7 || err != nil {
		t.Fatalf("pisanje: %d, %v", n, err)
	}
	if got := procitaj(t, b, 3); string(got) != "poz" {
		t.Errorf("prvi dio: %q", got)
	}
	buf := make([]byte, 16)
	if n, err := b.Read(buf); err != nil || string(buf[:n]) != "drav" {
		t.Errorf("ostatak: %q, %v", buf[:n], err)
	}

	napisi(t, b, "ovo a neće pročitati")
	napisi(t, a, "kraj")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := b.Read(buf); err != nil || string(buf[:n]) != "kraj" {
		t.Errorf("poslano prije zatvaranja: %q, %v", buf[:n], err)
	}
	for range 2 {
		if n, err := b.Read(buf); n != 0 || err != io.EOF {
			t.Errorf("nakon zatvaranja: %d, %v", n, err)
		}
	}
	_, err := b.Write([]byte("x"))
	var op *net.OpError
	if !errors.Is(err, syscall.EPIPE) || !errors.As(err, &op) || op.Op != "write" || op.Source != posluziteljeva || op.Addr != klijentova {
		t.Errorf("pisanje prema zatvorenom kraju: %v", err)
	}

	if _, err := a.Read(buf); !errors.Is(err, net.ErrClosed) {
		t.Errorf("čitanje zatvorenog kraja: %v", err)
	}
	if _, err := a.Write(buf); !errors.Is(err, net.ErrClosed) {
		t.Errorf("pisanje zatvorenog kraja: %v", err)
	}
	if err := a.SetDeadline(time.Now()); !errors.Is(err, net.ErrClosed) {
		t.Errorf("rok zatvorenog kraja: %v", err)
	}
	if err := a.Close(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("drugo zatvaranje: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("zatvaranje druge strane: %v", err)
	}
}

// Pisanje ne čeka čitatelja dok u smjeru ima mjesta (64 KiB). Kad mjesta
// nema, pisač čeka, a nastavi čim druga strana pročita.
func TestPisacCekaMjesto(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := Par(klijentova, posluziteljeva)
		defer a.Close() // pusti pisača i kad test stane prije kraja
		poslano := bytes.Repeat([]byte("0123456789"), medjuspremnik/10+20)
		gotovo := make(chan ishod, 1)
		go func() {
			n, err := a.Write(poslano)
			gotovo <- ishod{n, err}
		}()
		synctest.Wait()
		select {
		case r := <-gotovo:
			t.Fatalf("pisanje nije čekalo mjesto: %d, %v", r.n, r.err)
		default:
		}
		primljeno := procitaj(t, b, len(poslano))
		if r := <-gotovo; r.n != len(poslano) || r.err != nil {
			t.Errorf("pisanje nakon čitanja: %d, %v", r.n, r.err)
		}
		if !bytes.Equal(primljeno, poslano) {
			t.Error("primljeno se razlikuje od poslanog")
		}
	})
}

// Pisača koji čeka mjesto pusti rok pisanja, zatvaranje njegova kraja i
// zatvaranje druge strane, koje s kašnjenjem stiže do njega jedno kašnjenje
// poslije. Vrati koliko je stalo i grešku kakvu daje TCP.
func TestBlokiranogPisacaPustaRokIZatvaranje(t *testing.T) {
	slucajevi := []struct {
		ime       string
		kasnjenje time.Duration
		pusti     func(a, b net.Conn) error
		greska    error
		trajanje  time.Duration
	}{
		{"rok pisanja", 0, func(a, _ net.Conn) error { return a.SetWriteDeadline(time.Now().Add(time.Second)) }, os.ErrDeadlineExceeded, time.Second},
		{"zatvaranje vlastitog kraja", 0, func(a, _ net.Conn) error { return a.Close() }, net.ErrClosed, 0},
		{"zatvaranje druge strane", 0, func(_, b net.Conn) error { return b.Close() }, syscall.EPIPE, 0},
		{"zatvaranje druge strane s kašnjenjem", 30 * time.Millisecond, func(_, b net.Conn) error { return b.Close() }, syscall.EPIPE, 30 * time.Millisecond},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b := ParSKasnjenjem(klijentova, posluziteljeva, sl.kasnjenje)
				defer a.Close() // pusti pisača i kad test stane prije kraja
				gotovo := make(chan ishod, 1)
				go func() {
					n, err := a.Write(make([]byte, medjuspremnik+10))
					gotovo <- ishod{n, err}
				}()
				synctest.Wait()
				pocetak := time.Now()
				if err := sl.pusti(a, b); err != nil {
					t.Fatal(err)
				}
				r := <-gotovo
				if r.n != medjuspremnik || !errors.Is(r.err, sl.greska) {
					t.Errorf("pisanje: %d, %v; očekivano %d, %v", r.n, r.err, medjuspremnik, sl.greska)
				}
				if d := time.Since(pocetak); d != sl.trajanje {
					t.Errorf("pisač je čekao %v, a ne %v", d, sl.trajanje)
				}
			})
		})
	}
}

// U 64 KiB broji se i ono što još putuje: s kašnjenjem pisač stane čim
// napiše 64 KiB, iako druga strana još ništa nije primila, a nastavi tek kad
// ona to primi i pročita
func TestUMedjuspremnikBrojiSeIOnoStoPutuje(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := ParSKasnjenjem(klijentova, posluziteljeva, time.Second)
		defer a.Close() // pusti pisača i kad test stane prije kraja
		pocetak := time.Now()
		if n, err := a.Write(make([]byte, medjuspremnik)); n != medjuspremnik || err != nil {
			t.Fatalf("pisanje 64 KiB: %d, %v", n, err)
		}
		gotovo := make(chan ishod, 1)
		go func() {
			n, err := a.Write([]byte("x"))
			gotovo <- ishod{n, err}
		}()
		synctest.Wait()
		select {
		case r := <-gotovo:
			t.Fatalf("pisanje nije čekalo mjesto dok bajtovi putuju: %d, %v", r.n, r.err)
		default:
		}
		procitaj(t, b, medjuspremnik)
		if r := <-gotovo; r.n != 1 || r.err != nil || time.Since(pocetak) != time.Second {
			t.Errorf("pisanje nakon čitanja: %d, %v nakon %v", r.n, r.err, time.Since(pocetak))
		}
		if got := procitaj(t, b, 1); string(got) != "x" || time.Since(pocetak) != 2*time.Second {
			t.Errorf("zadnji bajt: %q nakon %v", got, time.Since(pocetak))
		}
	})
}

// Rokovi idu po satu: čitanje čeka točno do roka, rok pomaknut dok
// čitatelj čeka vrijedi odmah, nula ukida rok, a istekli rok vrijedi i kad
// podaci već čekaju (kao TCP).
func TestRokovi(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := Par(klijentova, posluziteljeva)
		buf := make([]byte, 8)
		pocetak := time.Now()
		if err := b.SetReadDeadline(pocetak.Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		_, err := b.Read(buf)
		var ne net.Error
		if !errors.Is(err, os.ErrDeadlineExceeded) || !errors.As(err, &ne) || !ne.Timeout() {
			t.Errorf("istek roka: %v", err)
		}
		if d := time.Since(pocetak); d != 2*time.Second {
			t.Errorf("čitanje je čekalo %v", d)
		}

		pocetak = time.Now()
		if err := b.SetReadDeadline(pocetak.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		pomaknut := make(chan error, 1)
		go func() {
			time.Sleep(500 * time.Millisecond)
			pomaknut <- b.SetReadDeadline(time.Now().Add(2 * time.Second))
		}()
		if _, err := b.Read(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("pomaknut rok: %v", err)
		}
		if err := <-pomaknut; err != nil {
			t.Fatal(err)
		}
		if d := time.Since(pocetak); d != 2500*time.Millisecond {
			t.Errorf("s pomaknutim rokom čitanje je čekalo %v", d)
		}

		if err := b.SetReadDeadline(time.Time{}); err != nil {
			t.Fatal(err)
		}
		pocetak = time.Now()
		go func() {
			time.Sleep(time.Second)
			if _, err := a.Write([]byte("x")); err != nil {
				t.Error(err)
			}
		}()
		if n, err := b.Read(buf); err != nil || string(buf[:n]) != "x" || time.Since(pocetak) != time.Second {
			t.Errorf("bez roka: %q, %v nakon %v", buf[:n], err, time.Since(pocetak))
		}

		napisi(t, a, "y")
		if err := b.SetDeadline(time.Now().Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Read(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("čitanje nakon isteklog roka: %v", err)
		}
		if n, err := b.Write(buf); n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("pisanje nakon isteklog roka: %d, %v", n, err)
		}
		if err := b.SetDeadline(time.Time{}); err != nil {
			t.Fatal(err)
		}
		if n, err := b.Read(buf); err != nil || string(buf[:n]) != "y" {
			t.Errorf("nakon ukinutog roka: %q, %v", buf[:n], err)
		}
	})
}

// Izvan mjehura rokovi idu po stvarnom satu
func TestRokUStvarnomVremenu(t *testing.T) {
	_, b := Par(klijentova, posluziteljeva)
	pocetak := time.Now()
	if err := b.SetReadDeadline(pocetak.Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("istek roka: %v", err)
	}
	if d := time.Since(pocetak); d < 20*time.Millisecond {
		t.Errorf("čitanje je stalo prije roka, nakon %v", d)
	}
}

// S kašnjenjem ništa ne stiže prije vremena: bajt stiže točno nakon
// kašnjenja, odgovor nakon dva, a zatvaranje također putuje jedno kašnjenje
func TestKasnjenjePara(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const kasnjenje = 40 * time.Millisecond
		a, b := ParSKasnjenjem(klijentova, posluziteljeva, kasnjenje)
		buf := make([]byte, 8)
		pocetak := time.Now()
		napisi(t, a, "?")
		if err := b.SetReadDeadline(pocetak.Add(kasnjenje - time.Nanosecond)); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Read(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("prije kašnjenja: %v", err)
		}
		if err := b.SetReadDeadline(time.Time{}); err != nil {
			t.Fatal(err)
		}
		if n, err := b.Read(buf); err != nil || string(buf[:n]) != "?" || time.Since(pocetak) != kasnjenje {
			t.Errorf("pitanje: %q, %v nakon %v", buf[:n], err, time.Since(pocetak))
		}
		napisi(t, b, "!")
		if n, err := a.Read(buf); err != nil || string(buf[:n]) != "!" || time.Since(pocetak) != 2*kasnjenje {
			t.Errorf("odgovor: %q, %v nakon %v", buf[:n], err, time.Since(pocetak))
		}
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Read(buf); err != io.EOF || time.Since(pocetak) != 3*kasnjenje {
			t.Errorf("zatvaranje: %v nakon %v", err, time.Since(pocetak))
		}
	})
}

// Zatvaranje putuje i do pisača. Dok ne stigne, pisanje prema strani koja je
// zatvorila vezu prolazi, a od tada vraća broken pipe. Tako strana koja
// odgovori i odmah zatvori vezu (npr. odbije razgovor) ne pokvari drugoj
// strani slanje koje je već krenulo, pa ova još pročita odgovor.
func TestZatvaranjeStizePisacuNakonKasnjenja(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const kasnjenje = 50 * time.Millisecond
		a, b := ParSKasnjenjem(klijentova, posluziteljeva, kasnjenje)
		defer a.Close()
		pocetak := time.Now()
		napisi(t, b, "odbij")
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(kasnjenje / 5)
		napisi(t, a, "drugo pitanje")
		if got := procitaj(t, a, 5); string(got) != "odbij" || time.Since(pocetak) != kasnjenje {
			t.Errorf("odgovor: %q nakon %v", got, time.Since(pocetak))
		}
		if _, err := a.Read(make([]byte, 1)); err != io.EOF {
			t.Errorf("nakon odgovora: %v", err)
		}
		if _, err := a.Write([]byte("x")); !errors.Is(err, syscall.EPIPE) {
			t.Errorf("pisanje kad je zatvaranje stiglo: %v", err)
		}
	})
}

// Tisuću jednobajtnih pisanja s kašnjenjem stiže istim redom, i kad mnogo
// komada stiže u istom trenutku. Ponavlja se, jer bi zamjena mjesta bila
// slučajna.
func TestKasnjenjeCuvaRedoslijed(t *testing.T) {
	poslano := make([]byte, 1000)
	for i := range poslano {
		poslano[i] = byte(i % 251)
	}
	for range 10 {
		synctest.Test(t, func(t *testing.T) {
			a, b := ParSKasnjenjem(klijentova, posluziteljeva, 3*time.Millisecond)
			go func() {
				defer a.Close()
				for i := range poslano {
					if _, err := a.Write(poslano[i : i+1]); err != nil {
						t.Error(err)
						return
					}
					if i%10 == 0 {
						time.Sleep(time.Millisecond)
					}
				}
			}()
			var primljeno []byte
			buf := make([]byte, 7)
			for {
				n, err := b.Read(buf)
				primljeno = append(primljeno, buf[:n]...)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(primljeno, poslano) {
				t.Errorf("redoslijed se promijenio: %v", primljeno)
			}
		})
	}
}
