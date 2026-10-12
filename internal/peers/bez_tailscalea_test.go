package peers

// Čvorovi bez Tailscalea (ostali čvorovi u tvrtki) ne smiju vidjeti ni
// zvati Tailscale adrese, a njihov razgovor mora ostati isti kao prije.
// Ovdje se gleda što stvarno ide žicom: lažni sugovornik pročita poruke
// pravog čvora, ili ih pošalje sam.

import (
	"context"
	"fmt"
	"net"
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

// Čvor s Tailscaleom čvoru bez Tailscalea ne šalje svoje adrese: u granici
// javi samo da ga ima, a delta je ista kao prije. Čvoru koji javi
// Tailscale pošalje ih (provjera da test gleda pravo mjesto).
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
	if !strings.Contains(granica, `"tailscale":true`) {
		t.Errorf("granica čvora s Tailscaleom: %s", granica)
	}
	if strings.Contains(delta, "tailscale") || strings.Contains(delta, "100.101.") || strings.Contains(delta, "fd7a:") {
		t.Errorf("čvor bez Tailscalea dobio je Tailscale adrese: %s", delta)
	}

	adr, ch = lazniSugovornik(t, firma, frontierMsg{Frontier: map[string]string{}, Tailscale: true}, deltaMsg{})
	upoznaj(t, laptop, firma, adr)
	if _, _, err := laptop.SyncWith(ctxRub, firma.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, delta := poruke(t, ch); !strings.Contains(delta, fmt.Sprintf(`"tailscale":["100.101.0.1:%d","[fd7a:115c:a1e0::1]:%d"]`, laptop.ports.Exchange, laptop.ports.Exchange)) {
		t.Errorf("čvor s Tailscaleom nije dobio adrese: %s", delta)
	}
}

// Čvor bez Tailscalea šalje iste poruke kao prije (ni riječi o
// Tailscaleu) i ne pamti adrese koje mu netko ipak pošalje
func TestCvorBezTailscaleaNeSaljeNiNePrima(t *testing.T) {
	c := clanovi(t, "ured-firme", "laptop-baranja")
	firma, laptop := c[0], c[1]
	ctx, otkazi := context.WithTimeout(ctxRub, 20*time.Second)
	defer otkazi()
	conn, err := razmjena.DialExchange(ctx, laptop.node.key, Protocol, laptop.vjerodajnice, adresaNaPetlji(firma), firma.javni())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	razgovor := []struct {
		vrsta string
		v     any
	}{
		{kindFrontier, frontierMsg{Frontier: map[string]string{}, Tailscale: true}},
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
			t.Errorf("čvor bez Tailscalea spominje Tailscale: %s", o)
		}
	}
	cekajDa(t, "čvor bez Tailscalea zabilježi razmjenu", func() bool {
		st, _ := firma.syncStates(ctxRub)
		return st[laptop.node.ID].LastOK != nil
	})
	var zapamceno string
	if err := firma.db.QueryRow(`SELECT tailscale FROM peer_sync WHERE node_id = ?`, laptop.node.ID).Scan(&zapamceno); err != nil || zapamceno != "" {
		t.Errorf("čvor bez Tailscalea zapamtio je %q (%v)", zapamceno, err)
	}
	nemaTailscaleaUKnjizi(t, firma)
}

// Knjiga nikad nema Tailscale adresu: dva čvora s Tailscaleom razgovaraju
// preko njega (viđena adresa je Tailscale), a čvor bez Tailscalea s oba.
// Na kraju nijedna verzija zapisa čvora ni na jednom računalu nema adresu
// iz 100.64.0.0/10 ni fd7a:115c:a1e0::/48, a čvor bez Tailscalea ih ne
// zove. Zapise čvorova upisuje samo SavePeer (uparivanje, potvrda,
// pronalaženje, bilješka o razmjeni, ručni unos), pa to vrijedi za sve
// putove; TestTailscaleAdreseNeIduUZapisCvora ih gleda izravno.
func TestKnjigaNikadNemaTailscaleAdresa(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar", "ured-firme")
	laptop, ured, firma := c[0], c[1], c[2]
	laptop.ZadajTailscale(adresa("100.101.0.1"), adresa("fd7a:115c:a1e0::1"))
	ured.ZadajTailscale(adresa("100.101.0.2"))
	prerusena := prerusenaRazmjena(t, ured, tcpAdresa(t, "100.101.0.2:4710"), tcpAdresa(t, "100.101.0.1:50000"))
	upoznaj(t, laptop, ured, prerusena)

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

// Isključen Tailscale na čvoru koji ga ima: granica ga ne javlja, pa drugi
// ne šalje svoje adrese i zaboravi njegove
func TestIskljucenTailscaleNeSaljeNiNePrima(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	laptop, ured := c[0], c[1]
	laptop.ZadajTailscale(adresa("100.101.0.1"))
	ured.ZadajTailscale(adresa("100.101.0.2"))
	ured.upisiTailscale(ctxRub, laptop.node.ID, []string{"100.101.0.1:4710"})
	laptop.PostaviTailscale(false)
	if _, _, err := laptop.SyncWith(ctxRub, ured.node.ID); err != nil {
		t.Fatal(err)
	}
	cekajDa(t, "ured zaboravi Tailscale adrese laptopa", func() bool { return len(ured.tailscaleOd(ctxRub, laptop.node.ID)) == 0 })
	var zapamceno string
	if err := laptop.db.QueryRow(`SELECT tailscale FROM peer_sync WHERE node_id = ?`, ured.node.ID).Scan(&zapamceno); err != nil || zapamceno != "" {
		t.Errorf("laptop s isključenim Tailscaleom zapamtio je %q (%v)", zapamceno, err)
	}
	nemaTailscaleaUKnjizi(t, laptop)
	nemaTailscaleaUKnjizi(t, ured)
}
