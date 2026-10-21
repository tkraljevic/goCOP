package peers

// Put veze: obje strane znaju kojim je putem razmjena išla, a ovaj čvor
// pamti put zadnje veze s drugim računalom (pokusaji.go).

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// Strana koja prima zna kojim je putem razmjena stigla: na port razmjene
// (adresa), kroz tunel (tunel) i na Tailscale adresu ovog računala s
// obične adrese druge strane, kao preko rute podmreže (tailscale). Put daju
// vrata u Serve, a ne test; strana koja zove ima svoj put.
func TestPutDolazneVeze(t *testing.T) {
	c := clanoviMreze(t, false, "laptop-baranja", "ured-vukovar", "ured-osijek")
	a, b, d := c[0], c[1], c[2]

	// b prima na portu razmjene
	naPetlji(t, b)

	// d prima kroz tunel, iza web poslužitelja s TLS-om, i na slušalici na
	// kojoj veza stiže na njegovu Tailscale adresu s obične adrese u drugoj
	// mreži. Tunel nije na b: naPetlji pri ponovnom pokušaju zaustavi prvo
	// pokretanje, a ono zatvori tunel.
	srv := httptest.NewTLSServer(d.TunelHandler())
	t.Cleanup(srv.Close)
	a.tunelKlijent.TLS = srv.Client().Transport.(*http.Transport).TLSClientConfig
	d.ZadajTailscale(adresa("100.101.0.2"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	d.slusalica = prerusenaSlusalica{Listener: ln, lokalna: tcpAdresa(t, "100.101.0.2:4710"), udaljena: tcpAdresa(t, "192.168.24.80:51234")}
	ctx, otkazi := context.WithCancel(ctxRub)
	t.Cleanup(otkazi)
	stala := make(chan error, 1)
	go func() { stala <- d.Serve(ctx) }()

	for _, sl := range []struct {
		ime     string
		cvor    *Service
		adresa  string
		dolazni razmjena.Put
		odlazni razmjena.Put
	}{
		{"port razmjene", b, adresaNaPetlji(b), razmjena.PutAdresa, razmjena.PutAdresa},
		{"tunel", d, srv.URL, razmjena.PutKrozTunel, razmjena.PutKrozTunel},
		{"ruta podmreže", d, ln.Addr().String(), razmjena.PutTailscale, razmjena.PutAdresa},
	} {
		upoznaj(t, a, sl.cvor, sl.adresa)
		if _, _, err := a.SyncWith(ctxRub, sl.cvor.node.ID); err != nil {
			select {
			case e := <-stala:
				t.Fatalf("%s: %v (slušalica je stala: %v)", sl.ime, err, e)
			default:
				t.Fatalf("%s: %v", sl.ime, err)
			}
		}
		if got := a.putZadnjeVeze(sl.cvor.node.ID); got != sl.odlazni {
			t.Errorf("%s: pozivatelj pamti put %q, očekivan %q", sl.ime, got, sl.odlazni)
		}
		cekajDa(t, sl.ime+": put dolazne veze", func() bool { return sl.cvor.putZadnjeVeze(a.node.ID) == sl.dolazni })
	}
}

// Veza na običnu adresu druge strane koja ide s Tailscale adrese ovog
// računala (ruta podmreže, izlazni čvor) išla je preko Tailscalea. Uz nju
// je izmjeren RTT, a SyncWith zapamti njezin put.
func TestPutOdlazneVezePrekoRutePodmreze(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	a.ZadajTailscale(adresa("100.101.0.1"))
	upoznaj(t, a, b, "192.168.24.80:4710")
	lokalna, udaljena := tcpAdresa(t, "100.101.0.1:51234"), tcpAdresa(t, "192.168.24.80:4710")
	a.dial = func(ctx context.Context, kamo string) (net.Conn, error) {
		if kamo != udaljena.String() {
			return nil, fmt.Errorf("dial tcp %s: i/o timeout", kamo)
		}
		var dialer net.Dialer
		veza, err := dialer.DialContext(ctx, "tcp", adresaNaPetlji(b))
		if err != nil {
			return nil, err
		}
		return prerusenaVeza{Conn: veza, lokalna: lokalna, udaljena: udaljena}, nil
	}

	peer, expect, err := a.partnerZaRazmjenu(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := a.nazovi(ctxRub, peer, kandidat{"192.168.24.80:4710", razmjena.PutAdresa}, expect)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if conn.Put != razmjena.PutTailscale {
		t.Errorf("put veze %q, očekivan tailscale", conn.Put)
	}
	if conn.RTT <= 0 || conn.RTT >= 20*time.Second {
		t.Errorf("RTT %v", conn.RTT)
	}

	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	if got := a.putZadnjeVeze(b.node.ID); got != razmjena.PutTailscale {
		t.Errorf("SyncWith pamti put %q, očekivan tailscale", got)
	}

	// i dok je Tailscale u razmjeni isključen, veza je išla preko njega
	a.PostaviTailscale(false)
	if conn, err = a.nazovi(ctxRub, peer, kandidat{"192.168.24.80:4710", razmjena.PutAdresa}, expect); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if conn.Put != razmjena.PutTailscale {
		t.Errorf("s isključenim Tailscaleom put veze %q, očekivan tailscale", conn.Put)
	}
}

// Put zadnje veze pamti se po računalu, samo kad je veze bilo i računalo
// je poznato. Zaboravljeno računalo ga nema, a drugo svoj zadrži.
func TestPutZadnjeVeze(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	veza := &razmjena.Conn{Put: razmjena.PutLAN}
	s.zapamtiPut(nil, veza)
	s.zapamtiPut(&Peer{NodeID: "laptop-baranja"}, nil)
	if put := s.putZadnjeVeze("laptop-baranja"); put != "" {
		t.Errorf("put bez veze ili bez računala: %q", put)
	}
	s.zapamtiPut(&Peer{NodeID: "laptop-baranja"}, veza)
	s.zapamtiPut(&Peer{NodeID: "ured-vukovar"}, veza)
	if put := s.putZadnjeVeze("laptop-baranja"); put != razmjena.PutLAN {
		t.Errorf("put zadnje veze %q", put)
	}
	if err := s.ForgetPeer(ctxRub, "laptop-baranja"); err != nil {
		t.Fatal(err)
	}
	if put := s.putZadnjeVeze("laptop-baranja"); put != "" {
		t.Errorf("zaboravljeno računalo ima put %q", put)
	}
	if put := s.putZadnjeVeze("ured-vukovar"); put != razmjena.PutLAN {
		t.Errorf("drugo računalo nakon zaborava: put %q", put)
	}
}
