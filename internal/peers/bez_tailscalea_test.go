package peers

// Čvorovi bez Tailscalea (ostali čvorovi u tvrtki) ne smiju vidjeti ni
// zvati Tailscale adrese, a njihov razgovor mora ostati isti kao prije.
// Ovdje se gleda što stvarno ide žicom: lažni sugovornik pročita poruke
// pravog čvora, ili ih pošalje sam.

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// lazniSugovornik prima razmjenu ključem čvora f, odgovori zadanom
// granicom i deltom i vrati poruke koje je pozivatelj poslao (granicu i
// deltu)
func lazniSugovornik(t *testing.T, f *Service, granica frontierMsg, delta deltaMsg) (string, chan []razmjena.Envelope) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, otkazi := context.WithCancel(ctxRub)
	t.Cleanup(otkazi)
	primljeno := make(chan []razmjena.Envelope, 1)
	posalji := func(c *razmjena.Conn, vrsta string, v any) {
		e, _ := razmjena.NewEnvelope(vrsta, v)
		_ = c.Send(e)
	}
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, f.node.key, Protocol, f.vjerodajnice, ln, f.trusted, func(c *razmjena.Conn) {
			defer c.Close()
			g, _ := c.Receive()
			posalji(c, kindFrontier, granica)
			d, _ := c.Receive()
			posalji(c, kindDelta, delta)
			_, _ = c.Receive() // želje sadržaja
			posalji(c, kindDone, doneMsg{})
			_, _ = c.Receive()
			primljeno <- []razmjena.Envelope{g, d}
		})
	}()
	return ln.Addr().String(), primljeno
}

func poruke(t *testing.T, ch chan []razmjena.Envelope) (granica, delta string) {
	t.Helper()
	select {
	case e := <-ch:
		return string(e[0].Payload), string(e[1].Payload)
	case <-time.After(20 * time.Second):
		t.Fatal("lažni sugovornik nije dobio razgovor")
	}
	return "", ""
}

// Čvor s Tailscaleom u razgovoru koji ne ide preko Tailscalea ne spominje
// Tailscale ni u granici ni u delti, pa čvor bez njega dobiva iste poruke
// kao prije. U razgovoru preko Tailscalea adrese idu (provjera da test
// gleda pravo mjesto).
func TestCvorBezTailscaleaNeDobivaTailscaleAdrese(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-firme")
	laptop, firma := c[0], c[1]
	laptop.ZadajTailscale(adresa("100.101.0.1"), adresa("fd7a:115c:a1e0::1"))

	adr, ch := lazniSugovornik(t, firma, frontierMsg{Frontier: map[string]string{}}, deltaMsg{})
	upoznaj(t, laptop, firma, adr)
	if _, _, err := laptop.SyncWith(ctxRub, firma.node.ID); err != nil {
		t.Fatal(err)
	}
	granica, delta := poruke(t, ch)
	for _, poruka := range []string{granica, delta} {
		if strings.Contains(poruka, "tailscale") || strings.Contains(poruka, "100.101.") || strings.Contains(poruka, "fd7a:") {
			t.Errorf("čvor bez Tailscalea dobio je nešto o Tailscaleu: %s", poruka)
		}
	}

	// isti lažni sugovornik, ali preko Tailscalea
	adr, ch = lazniSugovornik(t, firma, frontierMsg{Frontier: map[string]string{}}, deltaMsg{})
	naTailscale := "100.101.0.2:4710"
	laptop.dial = func(ctx context.Context, adresa string) (net.Conn, error) {
		return nazoviUTailnetu(ctx, adresa, map[string]string{naTailscale: adr}, tcpAdresa(t, "100.101.0.1:50000"))
	}
	upoznaj(t, laptop, firma)
	laptop.upisiTailscale(ctxRub, firma.node.ID, []string{naTailscale})
	if _, _, err := laptop.SyncWith(ctxRub, firma.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, delta := poruke(t, ch); !strings.Contains(delta, fmt.Sprintf(`"tailscale":["100.101.0.1:%d","[fd7a:115c:a1e0::1]:%d"]`, laptop.ports.Exchange, laptop.ports.Exchange)) {
		t.Errorf("u razgovoru preko Tailscalea adrese nisu poslane: %s", delta)
	}
}

// Čvor koji za sebe misli da ima Tailscale, a nije u istoj Tailscale mreži
// (Mac s Cloudflare WARP-om, kojem je stariji program utun karticu brojao
// kao Tailscale, ili čvor s vlastitim Tailscaleom), ne dobiva Tailscale
// adrese drugih čvorova ni kad razgovara s njima u oba smjera
func TestCvorIzvanTailscaleMrezeNeDobivaAdrese(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "mac-firme")
	laptop, mac := c[0], c[1]
	laptop.ZadajTailscale(adresa("100.101.0.1"))
	mac.ZadajTailscale(adresa("100.96.0.5"))
	for _, par := range [][2]*Service{{mac, laptop}, {laptop, mac}} {
		if _, _, err := par[0].SyncWith(ctxRub, par[1].node.ID); err != nil {
			t.Fatal(err)
		}
	}
	cekajDa(t, "oba zabilježe razgovor", func() bool {
		st, _ := mac.syncStates(ctxRub)
		st2, _ := laptop.syncStates(ctxRub)
		return st[laptop.node.ID].LastOK != nil && st2[mac.node.ID].LastOK != nil
	})
	if got := mac.tailscaleOd(ctxRub, laptop.node.ID); len(got) != 0 {
		t.Errorf("čvor izvan Tailscale mreže zna laptopove adrese: %v", got)
	}
	p, _ := mac.GetPeer(ctxRub, laptop.node.ID)
	if got := mac.adreseZaSpajanje(ctxRub, p); !slices.Equal(got, []string{adresaNaPetlji(laptop)}) {
		t.Errorf("čvor izvan Tailscale mreže zove %v", got)
	}
}

// Čvor bez Tailscalea šalje iste poruke kao prije (ni riječi o
// Tailscaleu) i ne pamti adrese koje mu netko ipak pošalje. Ne pamti ih ni
// čvor s Tailscaleom kad stignu izvan razgovora preko Tailscalea.
func TestCvorBezTailscaleaNeSaljeNiNePrima(t *testing.T) {
	c := clanovi(t, "ured-firme", "laptop-baranja", "ured-vukovar")
	firma, laptop, ured := c[0], c[1], c[2]
	ured.ZadajTailscale(adresa("100.101.0.2"))
	for _, s := range []*Service{firma, ured} {
		bezTailscaleaPrima(t, s, laptop)
	}
	nemaTailscaleaUKnjizi(t, firma)
}

// bezTailscaleaPrima: laptop u razgovoru u lokalnoj mreži pošalje čvoru s
// svoje Tailscale adrese, a s ih ne spominje i ne pamti
func bezTailscaleaPrima(t *testing.T, s, laptop *Service) {
	t.Helper()
	ctx, otkazi := context.WithTimeout(ctxRub, 20*time.Second)
	defer otkazi()
	conn, err := razmjena.DialExchange(ctx, laptop.node.key, Protocol, laptop.vjerodajnice, adresaNaPetlji(s), s.javni())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	razgovor := []struct {
		vrsta string
		v     any
	}{
		{kindFrontier, frontierMsg{Frontier: map[string]string{}}},
		{kindDelta, deltaMsg{Versions: nil, Tailscale: []string{"100.101.0.1:4710", "[fd7a:115c:a1e0::1]:4710"}}},
		{kindDone, doneMsg{}},
	}
	var odgovori []string
	for _, k := range razgovor {
		e, _ := razmjena.NewEnvelope(k.vrsta, k.v)
		if err := conn.Send(e); err != nil {
			t.Fatal(err)
		}
		odg, err := conn.Receive()
		if err != nil {
			t.Fatalf("odgovor na %s: %v", k.vrsta, err)
		}
		odgovori = append(odgovori, odg.Kind+" "+string(odg.Payload))
	}
	for _, o := range odgovori {
		if strings.Contains(o, "tailscale") || strings.Contains(o, "100.101.") {
			t.Errorf("%s spominje Tailscale: %s", s.node.ID, o)
		}
	}
	cekajDa(t, s.node.ID+" zabilježi razmjenu", func() bool {
		st, _ := s.syncStates(ctxRub)
		return st[laptop.node.ID].LastOK != nil
	})
	var zapamceno string
	if err := s.db.QueryRow(`SELECT tailscale FROM peer_sync WHERE node_id = ?`, laptop.node.ID).Scan(&zapamceno); err != nil || zapamceno != "" {
		t.Errorf("%s je zapamtio %q (%v)", s.node.ID, zapamceno, err)
	}
}

// Knjiga nikad nema Tailscale adresu: dva čvora s Tailscaleom razgovaraju
// preko njega (viđena adresa je Tailscale) i u lokalnoj mreži, a čvor bez
// Tailscalea s oba. Na kraju nijedna verzija zapisa čvora ni na jednom
// računalu nema adresu iz 100.64.0.0/10 ni fd7a:115c:a1e0::/48, a čvor bez
// Tailscalea ih ne zove. Zapise čvorova upisuje samo SavePeer (uparivanje,
// potvrda, pronalaženje, bilješka o razmjeni, ručni unos), a spomenik
// ForgetPeer, pa to vrijedi za sve putove; TestTailscaleAdreseNeIduUZapisCvora
// ih gleda izravno.
func TestKnjigaNikadNemaTailscaleAdresa(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar", "ured-firme")
	laptop, ured, firma := c[0], c[1], c[2]
	izmisljeniTailnet(t, laptop, ured)
	laptop.ZadajTailscale(adresa("100.101.0.1"), adresa("fd7a:115c:a1e0::1"))
	upoznaj(t, laptop, ured)
	laptop.upisiTailscale(ctxRub, ured.node.ID, []string{fmt.Sprintf("100.101.0.2:%d", ured.ports.Exchange)})

	for _, par := range [][2]*Service{{laptop, ured}, {ured, laptop}, {firma, laptop}, {firma, ured}, {laptop, firma}, {ured, firma}, {laptop, ured}} {
		if _, _, err := par[0].SyncWith(ctxRub, par[1].node.ID); err != nil {
			t.Fatalf("%s → %s: %v", par[0].node.ID, par[1].node.ID, err)
		}
	}
	for _, s := range c {
		nemaTailscaleaUKnjizi(t, s)
	}
	if got := ured.tailscaleOd(ctxRub, laptop.node.ID); len(got) != 2 {
		t.Errorf("ured ne zna laptopove Tailscale adrese: %v", got)
	}
	for _, drugi := range []*Service{laptop, ured} {
		p, _ := firma.GetPeer(ctxRub, drugi.node.ID)
		for _, a := range firma.adreseZaSpajanje(ctxRub, p) {
			if jeTailscaleAdresa(a) {
				t.Errorf("čvor bez Tailscalea zove %s na %s", drugi.node.ID, a)
			}
		}
		if got := firma.tailscaleOd(ctxRub, drugi.node.ID); len(got) != 0 {
			t.Errorf("čvor bez Tailscalea zna Tailscale adrese čvora %s: %v", drugi.node.ID, got)
		}
	}
}
