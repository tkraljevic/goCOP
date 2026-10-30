package razmjena

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// slusalica otvara TLS slušalicu razmjene na slučajnom portu i poslužuje je
// sa zadanom ogradom
func slusalica(t *testing.T, ctx context.Context, kljuc ed25519.PrivateKey, o *Ograda, trusted KeyChecker, handle func(*Conn)) string {
	t.Helper()
	cfg, err := tlsConfig(kljuc, "testproto")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = serveTLS(ctx, ln, cfg, "testproto", trusted, handle, o) // staje s ctx testa, a slušalicu koja padne provjerava TestSlusalicaRazmjeneKadPadneIliJePuna
	}()
	return ln.Addr().String()
}

// Poruka razmjene veća od ograde prekida vezu jasnom greškom, a brojanje
// kreće ispočetka sa svakom porukom: dvije poruke ispod ograde prolaze iako
// su zajedno iznad nje.
func TestPorukaVecaOdOgradeSeOdbija(t *testing.T) {
	posluzitelj, klijent := newKey(t), newKey(t)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	greske := make(chan error, 3)
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(4, 4, time.Second), func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
		defer c.Close()
		c.najvise = 4 << 10
		for i := 0; i < 3; i++ {
			_, err := c.Receive()
			greske <- err
			if err != nil {
				return
			}
		}
	})
	c, err := DialExchange(ctx, klijent, "testproto", nil, adresa, posluzitelj.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, n := range []int{3000, 3000, 20000} {
		e, _ := NewEnvelope("delta", strings.Repeat("a", n))
		if err := c.Send(e); err != nil {
			break
		}
	}
	for i, ocekujeGresku := range []bool{false, false, true} {
		err := <-greske
		if ocekujeGresku != (err != nil) {
			t.Fatalf("poruka %d: greška %v", i+1, err)
		}
		if ocekujeGresku {
			if !errors.Is(err, ErrPorukaPrevelika) || !strings.Contains(err.Error(), "poruka razmjene veća od 4 KiB") {
				t.Fatalf("prevelika poruka: %v", err)
			}
		}
	}
}

// Ograda zadana pri primanju vrijedi samo za tu poruku: manja odbije
// poruku koju bi zadana pustila, a sljedeća poruka opet ima zadanu ogradu
func TestOgradaPoPoruci(t *testing.T) {
	posluzitelj, klijent := newKey(t), newKey(t)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	greske := make(chan error, 3)
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(4, 4, time.Second), func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
		defer c.Close()
		c.najvise = 64 << 10
		for _, najvise := range []int64{4 << 10, 0, 4 << 10} {
			var err error
			if najvise == 0 {
				_, err = c.Receive()
			} else {
				_, err = c.PrimiDo(najvise)
			}
			greske <- err
			if err != nil {
				return
			}
		}
	})
	c, err := DialExchange(ctx, klijent, "testproto", nil, adresa, posluzitelj.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, n := range []int{3000, 20000, 20000} {
		e, err := NewEnvelope("delta", strings.Repeat("a", n))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Send(e); err != nil {
			break
		}
	}
	for i, ocekujeGresku := range []bool{false, false, true} {
		err := <-greske
		if ocekujeGresku != (err != nil) {
			t.Fatalf("poruka %d: greška %v", i+1, err)
		}
		if ocekujeGresku && (!errors.Is(err, ErrPorukaPrevelika) || !strings.Contains(err.Error(), "poruka razmjene veća od 4 KiB")) {
			t.Fatalf("prevelika poruka: %v", err)
		}
	}
}

// Kad teče najviše razmjena, sljedeći poznati čvor dobije odbijenicu s
// razlogom (stariji čvor je ispiše), a ne prekinutu vezu; kad se mjesto
// oslobodi, razmjena opet prolazi.
func TestZauzetCvorOdbijaRazmjenuSRazlogom(t *testing.T) {
	posluzitelj, klijent := newKey(t), newKey(t)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	pusti := make(chan struct{})
	usao := make(chan struct{}, 4)
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(4, 1, time.Second), func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
		defer c.Close()
		if _, err := c.Receive(); err != nil {
			return
		}
		usao <- struct{}{}
		<-pusti
		odg, _ := NewEnvelope("frontier", nil)
		c.Send(odg)
	})
	javni := posluzitelj.Public().(ed25519.PublicKey)
	granica, _ := NewEnvelope("frontier", map[string]string{"a": "1"})

	prvi, err := DialExchange(ctx, klijent, "testproto", nil, adresa, javni)
	if err != nil {
		t.Fatal(err)
	}
	defer prvi.Close()
	prvi.Send(granica)
	<-usao

	drugi, err := DialExchange(ctx, klijent, "testproto", nil, adresa, javni)
	if err != nil {
		t.Fatal(err)
	}
	drugi.Send(granica)
	e, err := drugi.Receive()
	drugi.Close()
	if err != nil {
		t.Fatalf("zauzet čvor je prekinuo vezu bez odbijenice: %v", err)
	}
	if e.Kind != VrstaOdbijeno || e.Reason != RazlogZauzet || e.Kod != KodZauzet {
		t.Fatalf("odbijenica: %+v", e)
	}

	close(pusti)
	if e, err := prvi.Receive(); err != nil || e.Kind != "frontier" {
		t.Fatalf("prva razmjena: %+v %v", e, err)
	}
	treci, err := DialExchange(ctx, klijent, "testproto", nil, adresa, javni)
	if err != nil {
		t.Fatal(err)
	}
	defer treci.Close()
	treci.Send(granica)
	if e, err := treci.Receive(); err != nil || e.Kind != "frontier" {
		t.Fatalf("razmjena nakon oslobođenog mjesta: %+v %v", e, err)
	}
}

// Veza koja ne dovrši TLS drži mjesto za rukovanje najviše rok ograde:
// poslužitelj je zatvori. Dok je drži, poznati čvor koji je već razmijenio
// s te adrese dobije mjesto u pričuvi i odmah prolazi.
func TestRukovanjeImaRokIOgraduMjesta(t *testing.T) {
	posluzitelj, klijent := newKey(t), newKey(t)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(1, 4, 300*time.Millisecond), func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
		defer c.Close()
		if _, err := c.Receive(); err != nil {
			return
		}
		odg, _ := NewEnvelope("ok", nil)
		c.Send(odg)
	})
	razmijeni := func() error {
		c, err := DialExchange(ctx, klijent, "testproto", nil, adresa, posluzitelj.Public().(ed25519.PublicKey))
		if err != nil {
			return err
		}
		defer c.Close()
		e, _ := NewEnvelope("frontier", nil)
		c.Send(e)
		_, err = c.Receive()
		return err
	}
	if err := razmijeni(); err != nil {
		t.Fatalf("razmjena prije šutljive veze: %v", err)
	}
	sutljivi, err := net.Dial("tcp", adresa)
	if err != nil {
		t.Fatal(err)
	}
	defer sutljivi.Close()
	time.Sleep(50 * time.Millisecond) // šutljivi zauzme jedino mjesto

	gotov := make(chan error, 1)
	go func() { gotov <- razmijeni() }()

	_ = sutljivi.SetReadDeadline(time.Now().Add(3 * time.Second))
	pocetak := time.Now()
	_, err = sutljivi.Read(make([]byte, 1))
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("poslužitelj nije zatvorio vezu koja nije dovršila rukovanje")
	}
	if d := time.Since(pocetak); d > 2*time.Second {
		t.Fatalf("veza bez rukovanja zatvorena tek nakon %v", d)
	}
	select {
	case err := <-gotov:
		if err != nil {
			t.Fatalf("poznati čvor nije prošao uz šutljivog: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("poznati čvor nije dobio mjesto za rukovanje")
	}
}

// Hello i potvrda uparivanja čitaju se od uređaja kojem se još ne vjeruje,
// pa su ograđeni: lažna slušalica koja pošalje golem Hello ili golemu
// potvrdu ne puni memoriju, nego uparivanje pada jasnom greškom.
func TestUparivanjeOgradjujePorukeDrugeStrane(t *testing.T) {
	for _, slucaj := range []struct {
		ime        string
		velikiHelo bool
	}{{"hello", true}, {"potvrda", false}} {
		t.Run(slucaj.ime, func(t *testing.T) {
			lazni, pravi := newKey(t), newKey(t)
			cfg, err := tlsConfig(lazni, "testproto")
			if err != nil {
				t.Fatal(err)
			}
			ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			go func() {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				dec := json.NewDecoder(c)
				var h Hello
				_ = dec.Decode(&h)
				ime := "lažni"
				if slucaj.velikiHelo {
					ime = strings.Repeat("x", 200<<10)
				}
				enc := json.NewEncoder(c)
				_ = enc.Encode(Hello{Protocol: "testproto", DeviceID: "lazni", Name: ime, SAS: sasInacica})
				// dogovor koda kao poštena slušalica: obveza, moj broj, otkriveni broj
				var obveza, otkriven sasPoruka
				_ = dec.Decode(&obveza)
				_ = enc.Encode(sasPoruka{Broj: make([]byte, velicinaBroja)})
				_ = dec.Decode(&otkriven)
				var p Confirm
				_ = dec.Decode(&p)
				veliki, _ := json.Marshal(strings.Repeat("y", 2<<20))
				json.NewEncoder(c).Encode(Confirm{Approved: true, Payload: veliki})
				time.Sleep(time.Second)
			}()
			ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
			defer otkazi()
			res, err := Dial(ctx, pravi, Identity{Protocol: "testproto", DeviceID: "pravi", Name: "pravi"}, ln.Addr().String())
			if slucaj.velikiHelo {
				if !errors.Is(err, ErrPorukaPrevelika) {
					t.Fatalf("golem Hello: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ok, _, err := res.Finish(true, nil)
			if ok || !errors.Is(err, ErrPorukaPrevelika) {
				t.Fatalf("golema potvrda: %v %v", ok, err)
			}
		})
	}
}

// WebSocket tunela koji nitko ne prima (razmjena ne radi, port razmjene 0)
// zatvara se nakon roka predaje, umjesto da gorutina i utičnica vise.
func TestTunelBezRazmjeneZatvaraVezu(t *testing.T) {
	stari := rokPredaje
	rokPredaje = 200 * time.Millisecond
	defer func() { rokPredaje = stari }()

	tunel := NoviTunel()
	defer tunel.Close()
	mux := http.NewServeMux()
	mux.Handle("GET "+PutTunela, tunel.Handler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ws, err := websocket.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+PutTunela, "", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err = ws.Read(make([]byte, 16))
	var ne net.Error
	if err == nil || (errors.As(err, &ne) && ne.Timeout()) {
		t.Fatalf("tunel bez razmjene nije zatvorio vezu: %v", err)
	}
}

// Tunel ima svoja rukovanja (kraći rok, ne zauzima mjesta porta), a
// razmjene dijeli s portom. Rukovanja ima koliko web ograda tunela pušta
// veza prije dokaza ključa (32).
func TestOgradaTunelaDijeliRazmjeneSPortom(t *testing.T) {
	port := NovaOgrada()
	tunel := NovaOgradaTunela(port)
	if tunel.razmjene != port.razmjene {
		t.Error("tunel i port moraju dijeliti granicu razmjena")
	}
	if tunel.rukovanja == port.rukovanja || cap(tunel.rukovanja) != najviseRukovanja {
		t.Errorf("tunel ima svojih %d rukovanja, a ne %d", cap(tunel.rukovanja), najviseRukovanja)
	}
	if tunel.rok >= port.rok {
		t.Errorf("rok rukovanja kroz tunel %v nije kraći od porta %v", tunel.rok, port.rok)
	}
}

// Zatvoren tunel zatvara i vezu koja čeka predaju i vezu koja je predana:
// gašenje čvora ne ostavlja otvorene WebSocket veze
func TestZatvorenTunelZatvaraVeze(t *testing.T) {
	for _, predana := range []bool{false, true} {
		tunel := NoviTunel()
		mux := http.NewServeMux()
		mux.Handle("GET "+PutTunela, tunel.Handler())
		srv := httptest.NewServer(mux)

		ws, err := websocket.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+PutTunela, "", srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		if predana {
			if _, err := tunel.Accept(); err != nil {
				t.Fatal(err)
			}
		} else {
			time.Sleep(100 * time.Millisecond) // veza je stigla do predaje
		}
		tunel.Close()
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, err = ws.Read(make([]byte, 16))
		var ne net.Error
		if err == nil || (errors.As(err, &ne) && ne.Timeout()) {
			t.Errorf("predana=%v: zatvoren tunel nije zatvorio vezu: %v", predana, err)
		}
		ws.Close()
		srv.Close()
	}
}

// tokOdjednom je prijenos koji jednim čitanjem preda sve što stane, kao tok
// QUIC-a kad je stiglo više poruka
type tokOdjednom struct{ *bytes.Reader }

func (tokOdjednom) Write(p []byte) (int, error)      { return len(p), nil }
func (tokOdjednom) Close() error                     { return nil }
func (tokOdjednom) SetReadDeadline(time.Time) error  { return nil }
func (tokOdjednom) SetWriteDeadline(time.Time) error { return nil }
func (tokOdjednom) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (tokOdjednom) RemoteAddr() net.Addr             { return &net.TCPAddr{} }

// Bajtove sljedeće poruke koje je dekoder pročitao uz veliku poruku
// sljedeća poruka troši od svoje ograde: poruka veća od svoje ograde ne
// prolazi ni kad je cijela već pročitana, a manje poruke iza velike
// prolaze kao i prije
func TestOgradaBrojiProcitanoUnaprijed(t *testing.T) {
	var tok bytes.Buffer
	enc := json.NewEncoder(&tok)
	for _, n := range []int{200 << 10, 10 << 10, 20 << 10, 1 << 10} {
		e, err := NewEnvelope("delta", strings.Repeat("a", n))
		if err != nil {
			t.Fatal(err)
		}
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
	c := newConnKljuc(tokOdjednom{bytes.NewReader(tok.Bytes())}, nil, nil)
	for i, slucaj := range []struct {
		najvise int64
		prolazi bool
	}{{1 << 20, true}, {16 << 10, true}, {4 << 10, false}} {
		_, err := c.PrimiDo(slucaj.najvise)
		if slucaj.prolazi != (err == nil) {
			t.Fatalf("poruka %d uz ogradu %d: %v", i+1, slucaj.najvise, err)
		}
		if !slucaj.prolazi && !errors.Is(err, ErrPorukaPrevelika) {
			t.Fatalf("poruka %d: %v", i+1, err)
		}
	}
}
