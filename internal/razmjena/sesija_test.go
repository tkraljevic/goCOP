package razmjena

// Sesija WEB synca (sesija.go): kanali, kontrolni kanal (ping, zatvaram,
// pokvaren okvir), greška u obradi i kodovi zatvaranja. Dio testova vodi
// drugu stranu ručno, na simnetu, da može šutjeti ili slati krive poruke.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

// rucnaSesija je sesija ovog računala (A) na simnetu, a druga strana je
// gola QUIC veza s kontrolnim tokom, koju test vodi sam
func rucnaSesija(t *testing.T) (s *SesijaWeb, druga *quic.Conn, kontrola *quic.Stream) {
	t.Helper()
	s, druga, kontrola = rucnaVeza(t)
	s.pokreni(func(*SesijaWeb) {})
	return s, druga, kontrola
}

// rucnaVeza je kao rucnaSesija, ali sesija je samo postavljena, a nije
// pokrenuta
func rucnaVeza(t *testing.T) (s *SesijaWeb, druga *quic.Conn, kontrola *quic.Stream) {
	t.Helper()
	privA, privB := newKey(t), newKey(t)
	trA, trB := mrezaQUIC(t)
	cfg, err := tlsZaQUIC(privB, "testproto", nil, alpnWeb("testproto"))
	bezGreske(t, err)
	ln, err := trB.Listen(cfg, postavkeQUIC())
	bezGreske(t, err)
	t.Cleanup(func() { _ = ln.Close() }) // greška zatvaranja na kraju testa nema što reći
	cfgA, err := tlsKlijentaWeb(privA, "testproto", nil, adresaQUICB.String(), privB.Public().(ed25519.PublicKey))
	bezGreske(t, err)
	veza, err := trA.Dial(t.Context(), adresaQUICB, cfgA, postavkeQUIC())
	bezGreske(t, err)
	tok, err := veza.OpenStreamSync(t.Context())
	bezGreske(t, err)
	_, _ = tok.Write([]byte{kanalKontrola}) // tok tek tada stigne drugoj strani
	druga, err = ln.Accept(t.Context())
	bezGreske(t, err)
	kontrola, err = druga.AcceptStream(t.Context())
	bezGreske(t, err)
	if k, err := citajKanal(kontrola); err != nil || k != kanalKontrola {
		t.Fatalf("kanal %d: %v", k, err)
	}
	s = &SesijaWeb{Sesija: NovaSesija(), Druga: privB.Public().(ed25519.PublicKey), veza: veza, kontrola: tok,
		v: &vrata{protokol: "testproto", trusted: svima, handle: func(c *Conn) { c.Close() }, o: NovaOgrada()}}
	s.postavi()
	t.Cleanup(func() {
		_ = veza.CloseWithError(0, "") // kraj testa
		s.Cekaj()
	})
	return s, druga, kontrola
}

// Ping: druga strana odgovori, pa ping vrati RTT; kad šuti, ping čeka dok
// se veza ne zatvori ili ne istekne kontekst. Ping na koji ova strana treba
// odgovoriti dobije odgovor, a odgovor koji nitko ne čeka se zanemaruje.
func TestPingSesije(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		s, druga, kontrola := rucnaSesija(t)
		go func() {
			if p, err := procitajKontrolu(kontrola); err == nil && p.Vrsta == kontrolaPing && p.Odgovori {
				_ = pisiKontrolu(kontrola, porukaKontrole{Vrsta: kontrolaPing, Br: p.Br}) // ishod vidi Ping
			}
		}()
		if rtt, err := s.Ping(t.Context()); err != nil || rtt < 10*time.Millisecond || rtt > 50*time.Millisecond {
			t.Errorf("ping: %v %v", rtt, err)
		}
		bezGreske(t, pisiKontrolu(kontrola, porukaKontrole{Vrsta: kontrolaPing, Odgovori: true}))
		if p, err := procitajKontrolu(kontrola); err != nil || p.Vrsta != kontrolaPing || p.Odgovori {
			t.Errorf("odgovor na ping: %+v %v", p, err)
		}
		bezGreske(t, pisiKontrolu(kontrola, porukaKontrole{Vrsta: kontrolaPing}))
		bezGreske(t, pisiKontrolu(kontrola, porukaKontrole{Vrsta: kontrolaPing}))
		bezGreske(t, pisiKontrolu(kontrola, porukaKontrole{Vrsta: "nova-vrsta"}))
		time.Sleep(100 * time.Millisecond)
		ctx, otkazi := context.WithTimeout(t.Context(), time.Second)
		defer otkazi()
		// odgovori koje nitko nije čekao ostanu bez broja, pa ih Ping preskoči
		if _, err := s.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("ping bez odgovora: %v", err)
		}
		go func() {
			time.Sleep(time.Second)
			_ = druga.CloseWithError(quic.ApplicationErrorCode(KodOpce), "") // ishod vidi Ping
		}()
		if _, err := s.Ping(t.Context()); err == nil {
			t.Error("ping na zatvorenoj vezi")
		}
		if _, err := s.Ping(t.Context()); err == nil {
			t.Error("ping nakon zatvaranja")
		}
		if s.Kod() != KodOpce {
			t.Errorf("kod %#x", s.Kod())
		}
	})
}

// Druga strana javi zatvaram s kodom: sesija zatvori vezu istim kodom.
// Pokvaren okvir na kontrolnom kanalu zatvara sesiju kodom 0x0100.
func TestKontrolniKanalZatvara(t *testing.T) {
	for ime, c := range map[string]struct {
		posalji func(io.Writer) error
		kod     KodRazloga
	}{
		"zatvaram": {func(w io.Writer) error {
			return pisiKontrolu(w, porukaKontrole{Vrsta: kontrolaZatvaram, Kod: KodNijeClan})
		}, KodNijeClan},
		"pokvaren":  {func(w io.Writer) error { return PisiOkvir(w, []byte("nije json")) }, KodOpce},
		"bez vrste": {func(w io.Writer) error { return PisiOkvir(w, []byte(`{"sesija":"x"}`)) }, KodOpce},
	} {
		t.Run(ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				s, _, kontrola := rucnaSesija(t)
				bezGreske(t, c.posalji(kontrola))
				if !zatvorena(s, time.Second) || s.Kod() != c.kod {
					t.Errorf("kod %#x", s.Kod())
				}
			})
		})
	}
}

// Zatvori javi zatvaram, a kad druga strana ne zatvori vezu, zatvori je
// sama nakon 1 s. Drugi Zatvori čeka prvi.
func TestZatvoriBezOdgovora(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		s, _, kontrola := rucnaSesija(t)
		gotov := make(chan struct{})
		go func() {
			s.Zatvori(KodZauzet)
			close(gotov)
		}()
		if p, err := procitajKontrolu(kontrola); err != nil || p.Vrsta != kontrolaZatvaram || p.Kod != KodZauzet {
			t.Errorf("zatvaram: %+v %v", p, err)
		}
		pocetak := time.Now()
		s.Zatvori(0)
		<-gotov
		if d := time.Since(pocetak); d < rokZatvaranja-50*time.Millisecond || d > rokZatvaranja || s.Kod() != KodZauzet {
			t.Errorf("zatvoreno nakon %v, kod %#x", time.Since(pocetak), s.Kod())
		}
	})
}

// Tok na kanalu koji sesija ne zna (i rezerviranim „stranicama” 0x02 i
// 0x03), drugi kontrolni tok i tok koji u roku ne javi kanal dobiju 0x0101;
// sesija i dalje radi.
func TestNepoznatKanal(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		a, b, _ := javniPar(t, internetUdaraca(t))
		sa, _ := sesijaPara(t, a, b)
		for _, kanal := range [][]byte{{0x02, 1}, {0x03, 1}, {0x07, 1}, {kanalKontrola, 1}, nil} {
			tok, err := sa.veza.OpenStreamSync(t.Context())
			bezGreske(t, err)
			_, _ = tok.Write(kanal) // ishod je u čitanju
			if kanal == nil {
				tok.Close() // tok bez ijednog bajta, samo kraj
			}
			var se *quic.StreamError
			if _, err := io.ReadAll(tok); !errors.As(err, &se) || se.ErrorCode != quic.StreamErrorCode(KodNepoznatKanal) {
				t.Errorf("kanal %x: %v", kanal, err)
			}
		}
		if _, err := razgovorNa(t.Context(), sa); err != nil {
			t.Error(err)
		}
	})
}

// Greška u obradi razgovora ne ruši čvor: tok se zatvara kodom 0x0100, a
// sesija radi dalje.
func TestGreskaUObradiRazgovora(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		a, b, _ := javniPar(t, internetUdaraca(t), func(p *PostavkeWeb) {
			p.Handle = func(c *Conn) {
				e, _ := c.Receive() // greška čitanja je ovdje isto što i prazna omotnica
				if e.Kind == "padni" {
					panic("namjerno: greška u obradi")
				}
				_ = c.Send(Envelope{Kind: "odgovor"}) // ishod vidi pozivatelj
			}
		})
		sa, _ := sesijaPara(t, a, b)
		c, err := sa.Razgovor(t.Context())
		bezGreske(t, err)
		bezGreske(t, c.Send(Envelope{Kind: "padni"}))
		var se *quic.StreamError
		if _, err := c.Receive(); !errors.As(err, &se) || se.ErrorCode != quic.StreamErrorCode(KodOpce) {
			t.Errorf("razgovor s greškom: %v", err)
		}
		c.Close()
		if _, err := razgovorNa(t.Context(), sa); err != nil {
			t.Errorf("nakon greške: %v", err)
		}
	})
}

// Razgovor na zatvorenoj sesiji ne počinje.
func TestRazgovorNaZatvorenojSesiji(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		a, b, _ := javniPar(t, internetUdaraca(t))
		sa, sb := sesijaPara(t, a, b)
		sb.Zatvori(0)
		zatvorena(sa, time.Second)
		if _, err := sa.Razgovor(t.Context()); err == nil {
			t.Error("razgovor na zatvorenoj sesiji")
		}
	})
}

// Na mjestu pozdrava stigne druga poruka kontrole: to nije pozdrav.
func TestNijePozdrav(t *testing.T) {
	var b bytes.Buffer
	bezGreske(t, pisiKontrolu(&b, porukaKontrole{Vrsta: kontrolaPing}))
	if _, err := procitajPozdrav(&b); !errors.Is(err, errNijePozdrav) {
		t.Errorf("ping umjesto pozdrava: %v", err)
	}
}

// Zakašnjeli odgovor na ping kojem je istekao kontekst ne vrijedi za
// idući ping: odgovor nosi broj pinga. Ping koji čeka red (drugi Ping je u
// tijeku) stane s kontekstom.
func TestPingZakasnjeliOdgovor(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		s, _, kontrola := rucnaSesija(t)
		pinga := make(chan porukaKontrole, 2)
		go func() {
			for range 2 {
				p, err := procitajKontrolu(kontrola)
				if err != nil {
					return
				}
				pinga <- p
			}
		}()
		ctx, otkazi := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer otkazi()
		if _, err := s.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("prvi ping: %v", err)
		}
		prvi := <-pinga
		go func() {
			drugi := <-pinga
			_ = pisiKontrolu(kontrola, porukaKontrole{Vrsta: kontrolaPing, Br: prvi.Br}) // zakašnjeli odgovor; ishod vidi Ping
			time.Sleep(30 * time.Millisecond)
			_ = pisiKontrolu(kontrola, porukaKontrole{Vrsta: kontrolaPing, Br: drugi.Br}) // isto
		}()
		if rtt, err := s.Ping(t.Context()); err != nil || rtt < 30*time.Millisecond {
			t.Errorf("drugi ping: %v %v (zakašnjeli odgovor ne vrijedi)", rtt, err)
		}
		if prvi.Br == 0 {
			t.Error("ping bez broja")
		}
		s.pingRed <- struct{}{}
		ctx, otkazi = context.WithCancel(t.Context())
		otkazi()
		if _, err := s.Ping(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("ping u redu: %v", err)
		}
		<-s.pingRed
		// odgovor koji nitko nije uzeo ne istisne idući: zadnji pobjeđuje
		s.javiPing(7)
		s.javiPing(8)
		if b := <-s.ping; b != 8 {
			t.Errorf("zadnji odgovor %d", b)
		}
	})
}

// Sesiju smije zatvoriti netko drugi (ProvjeriSve, kraj Radi, nova sesija)
// odmah nakon upisa, prije nego što je pokrenuta: zatvaranje radi, a kraj
// se javi kad sesija krene.
func TestZatvorenaPrijePokretanja(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		s, _, _ := rucnaVeza(t)
		zatvorio := make(chan struct{})
		go func() {
			s.Zatvori(KodNijeClan)
			close(zatvorio)
		}()
		kraj := make(chan struct{})
		s.pokreni(func(*SesijaWeb) { close(kraj) })
		<-zatvorio
		<-kraj
		if s.Kod() != KodNijeClan {
			t.Errorf("kod %#x", s.Kod())
		}
	})
}
