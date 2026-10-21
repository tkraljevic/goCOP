package peers

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// brojacDnevnika broji retke dnevnika koji sadrže traženi tekst; piše se
// iz gorutina razmjene, pa ide pod bravom
type brojacDnevnika struct {
	mu     sync.Mutex
	trazi  string
	n      int
	zadnji string
}

func (b *brojacDnevnika) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if strings.Contains(string(p), b.trazi) {
		b.n++
		b.zadnji = string(p)
	}
	return len(p), nil
}

func (b *brojacDnevnika) broj() (int, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n, b.zadnji
}

func brojiDnevnik(t *testing.T, trazi string) *brojacDnevnika {
	t.Helper()
	b := &brojacDnevnika{trazi: trazi}
	stari := log.Writer()
	log.SetOutput(b)
	t.Cleanup(func() { log.SetOutput(stari) })
	return b
}

// [sync] tailscale = false: čvor ne javlja svoje adrese, ne pamti tuđe i ne
// zove ih, a odmah nakon uključivanja opet radi s njima
func TestPrekidacTailscaleIskljucen(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	s.ZadajTailscale(adresa("100.101.0.1"))
	s.upisiTailscale(ctxRub, "ured-vukovar", []string{"100.101.0.2:4710"})
	p := &Peer{NodeID: "ured-vukovar", Addresses: []string{"192.168.24.70:4710"}}

	s.PostaviTailscale(false)
	if s.TailscaleUkljucen() || s.mojeTailscale() != nil {
		t.Error("isključen Tailscale i dalje daje adrese")
	}
	if got := s.adreseZaSpajanje(ctxRub, p); len(got) != 1 || got[0].put != razmjena.PutAdresa {
		t.Errorf("isključen Tailscale zove: %v", got)
	}
	s.zapamtiTailscale(ctxRub, "ured-vukovar", syncOutcome{})
	if got := s.tailscaleOd(ctxRub, "ured-vukovar"); len(got) != 1 {
		t.Errorf("isključen Tailscale ne prima ništa ni ne briše: %v", got)
	}

	s.PostaviTailscale(true)
	if got := s.adreseZaSpajanje(ctxRub, p); len(got) != 2 || got[1] != (kandidat{"100.101.0.2:4710", razmjena.PutTailscale}) {
		t.Errorf("nakon uključivanja: %v", got)
	}
}

// Veza je na vlastitoj Tailscale adresi samo kad je adresa iz raspona i
// među Tailscale adresama ovog računala
func TestVezaNaVlastitiTailscale(t *testing.T) {
	s := &Service{}
	s.ZadajTailscale(adresa("100.101.0.2"), adresa("fd7a:115c:a1e0::2"))
	for a, ocekivano := range map[string]bool{
		"100.101.0.2:4710":          true,
		"[fd7a:115c:a1e0::2]:4710":  true,
		"[::ffff:100.101.0.2]:4710": true,
		"100.101.0.3:4710":          false, // tuđa adresa iz raspona (npr. operater)
		"192.168.1.2:4710":          false,
	} {
		if s.naVlastitiTailscale(tcpAdresa(t, a)) != ocekivano {
			t.Errorf("%s: očekivano %v", a, ocekivano)
		}
	}
	if s.naVlastitiTailscale(&net.UnixAddr{Name: "/razmjena/tunel", Net: "unix"}) {
		t.Error("veza kroz tunel nije na Tailscale adresi")
	}
}

// Isključen Tailscale: razmjena koja stigne na Tailscale adresu ovog
// računala se odbija, a pozivatelj zaboravi tu adresu i zove sljedeću.
// Dnevnik dobije jedan redak po čvoru, ma koliko puta zvao.
func TestIskljucenTailscaleOdbijaRazmjenuNaTailscaleAdresi(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	a.ZadajTailscale(adresa("100.101.0.1"))
	b.ZadajTailscale(adresa("100.101.0.2"))
	b.PostaviTailscale(false)
	dnevnik := brojiDnevnik(t, "Tailscale je u razmjeni isključen")

	naTailscale := prerusenaRazmjena(t, b, tcpAdresa(t, "100.101.0.2:4710"), nil)
	upoznaj(t, a, b, naTailscale, adresaNaPetlji(b))
	a.upisiTailscale(ctxRub, b.node.ID, []string{"100.101.0.2:4710"})
	b.upisiTailscale(ctxRub, a.node.ID, []string{"100.101.0.1:4710"})

	for i := 0; i < 2; i++ {
		if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
			t.Fatalf("razmjena %d nije prešla na sljedeću adresu: %v", i+1, err)
		}
	}
	if got := a.tailscaleOd(ctxRub, b.node.ID); len(got) != 0 {
		t.Errorf("A pamti Tailscale adrese čvora kojem je isključen: %v", got)
	}
	if n, redak := dnevnik.broj(); n != 1 || !strings.Contains(redak, "laptop-baranja") {
		t.Errorf("dnevnik: %d redaka, zadnji %q", n, redak)
	}
	peerA, _ := b.GetPeer(ctxRub, a.node.ID)
	if got := b.adreseZaSpajanje(ctxRub, peerA); len(got) != 1 || got[0].put != razmjena.PutAdresa {
		t.Errorf("B s isključenim Tailscaleom zove: %v", got)
	}
	st, _ := b.syncStates(ctxRub)
	if len(st[a.node.ID].Tailscale) != 1 {
		t.Errorf("B je dirao A-ove adrese iako je Tailscale isključen: %v", st[a.node.ID].Tailscale)
	}
	nemaTailscaleaUKnjizi(t, a)
	nemaTailscaleaUKnjizi(t, b)

	// uključen: ista vrata primaju razmjenu
	b.PostaviTailscale(true)
	upoznaj(t, a, b, naTailscale)
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatalf("uključen Tailscale: %v", err)
	}
}

// Nepoznati pozivatelj u dnevniku je njegov ključ
func TestOdbijenaTailscaleRazmjenaNepoznatogCvora(t *testing.T) {
	b := rubniCvor(t, "ured-vukovar")
	b.ZadajTailscale(adresa("100.101.0.2"))
	b.PostaviTailscale(false)
	dnevnik := brojiDnevnik(t, "Tailscale je u razmjeni isključen")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, otkazi := context.WithTimeout(ctxRub, 10*time.Second)
	defer otkazi()
	odbijeno := make(chan bool, 1)
	sl := prerusenaSlusalica{Listener: ln, lokalna: tcpAdresa(t, "100.101.0.2:4710")}
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, b.node.key, Protocol, nil, sl, func(ed25519.PublicKey, []byte) bool { return true },
			func(c *razmjena.Conn) { odbijeno <- b.odbijTailscale(c, nil) })
	}()
	a := rubniCvor(t, "tudji-laptop")
	conn, err := razmjena.DialExchange(ctx, a.node.key, Protocol, nil, ln.Addr().String(), b.javni())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	granica, _ := razmjena.NewEnvelope(kindFrontier, frontierMsg{})
	if err := conn.Send(granica); err != nil {
		t.Fatal(err)
	}
	e, err := conn.Receive()
	if err != nil || e.Kind != razmjena.VrstaOdbijeno || e.Reason != razlogBezTailscalea || e.Kod != razmjena.KodBezTailscalea {
		t.Fatalf("odbijenica: %+v %v", e, err)
	}
	// na žici kod ide iza razloga, bez deviceId (G3, plan WEB synca, §3)
	if b, _ := json.Marshal(e); string(b) != `{"kind":"odbijeno","reason":"`+razlogBezTailscalea+`","kod":263}` {
		t.Errorf("odbijenica na žici: %s", b)
	}
	if !<-odbijeno {
		t.Error("razmjena nije odbijena")
	}
	if n, redak := dnevnik.broj(); n != 1 || !strings.Contains(redak, a.node.PublicKey()) {
		t.Errorf("dnevnik: %d, %q", n, redak)
	}
	// tekst odbijenice ostaje kakav je bio: „čvor odbio razmjenu: razlog”
	if got := (odbijenaRazmjena{razmjena.RazlogZauzet, razmjena.KodZauzet}).Error(); got != "čvor odbio razmjenu: "+razmjena.RazlogZauzet {
		t.Errorf("tekst odbijenice: %q", got)
	}
	if b.odbioTailscale(ctxRub, "tudji-laptop", fmt.Errorf("veza prekinuta")) || b.odbioTailscale(ctxRub, "tudji-laptop", odbijenaRazmjena{razmjena.RazlogZauzet, razmjena.KodZauzet}) {
		t.Error("druge greške nisu odbijeni Tailscale")
	}
}
