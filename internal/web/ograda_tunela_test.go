package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"gocop/internal/razmjena"

	"golang.org/x/net/websocket"
)

func kljucZaTest(t *testing.T) ed25519.PublicKey {
	t.Helper()
	return tajniZaTest(t).Public().(ed25519.PublicKey)
}

func tajniZaTest(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, tajni, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return tajni
}

// Dok ključ nije dokazan, klijent je IPv4 adresa ili IPv6 mreža /64 (2
// mjesta), a mreža IPv4 /24 ima ih 4, IPv6 /48 dva
func TestTunelKlijentIMreza(t *testing.T) {
	o := novaOgradaTunela(granicaTunela)
	uzmi := func(a string) bool {
		_, ok := o.uzmiStranca(netip.MustParseAddr(a))
		return ok
	}
	for _, a := range []string{"2001:db8:1:2::1", "2001:db8:1:2::2"} {
		if !uzmi(a) {
			t.Fatalf("%s odbijen", a)
		}
	}
	if uzmi("2001:db8:1:2:ffff::9") {
		t.Error("treća veza iste mreže /64 dobila je mjesto")
	}
	if uzmi("2001:db8:1:3::1") {
		t.Error("treća veza iste mreže /48 dobila je mjesto")
	}
	for _, a := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.2", "203.0.113.3"} {
		if !uzmi(a) {
			t.Fatalf("%s odbijen", a)
		}
	}
	if uzmi("203.0.113.4") {
		t.Error("peta veza iste mreže /24 dobila je mjesto")
	}
	if !uzmi("198.51.100.1") || !uzmi("2001:db8:2::1") {
		t.Error("druga mreža je zasebna")
	}
	// sastajalište svoja rukovanja i dalje broji po IPv4 adresi i mreži IPv6 /48
	if k := kljucKlijentaTunela(netip.MustParseAddr("2001:db8:1:7::ff")); k.String() != "2001:db8:1::" {
		t.Errorf("klijent sastajališta za IPv6: %s", k)
	}
	if k := kljucKlijentaTunela(netip.MustParseAddr("203.0.113.5")); k.String() != "203.0.113.5" {
		t.Errorf("klijent sastajališta za IPv4: %s", k)
	}
	if granicaTunela.stranaca+granicaTunela.poznatih != 32 {
		t.Errorf("stranaca i poznatih %d, a tunel ima 32 rukovanja", granicaTunela.stranaca+granicaTunela.poznatih)
	}
}

// Ograda tunela kroz pravi rukovatelj: klijent je adresa iz CF-Connecting-IP
// od posrednika, a adrese iste mreže /64 dijele mjesta
func TestOgradaTunelaKrozRukovatelj(t *testing.T) {
	o := novaOgradaTunela(granicaTunela)
	pusti := make(chan struct{})
	h := (&Server{}).klijentSloj(o.omotaj(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-pusti }), "puno"))
	zahtjev := func(adresa string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/razmjena/tunel", nil)
		r.RemoteAddr = "127.0.0.1:5555"
		r.Host = adresaCvoraTesta
		r.Header.Set("CF-Connecting-IP", adresa)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	gotovo := make(chan struct{}, 4)
	for _, a := range []string{"2001:db8:aa:1::1", "2001:db8:aa:1::2"} {
		go func(a string) { zahtjev(a); gotovo <- struct{}{} }(a)
	}
	for i := 0; i < 50 && func() bool { o.mu.Lock(); defer o.mu.Unlock(); return o.stranaca < 2 }(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if w := zahtjev("2001:db8:aa:1::3"); w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Errorf("treća veza iste mreže /64: %d", w.Code)
	}
	close(pusti)
	<-gotovo
	<-gotovo
}

// Kad ključ prođe, veza pusti mjesto stranca i drži mjesto člana: najviše
// dva po ključu i 32 ukupno. Isti ključ na istoj vezi prolazi ponovno, drugi
// ne; puštena veza više ne dobiva mjesto.
func TestClanPrelaziNaSvojeMjesto(t *testing.T) {
	o := novaOgradaTunela(granicaOgrade{stranaca: 1, poKlijentu: 1, poMrezi: 1, clanova: 3, poKljucu: 2})
	a := netip.MustParseAddr("203.0.113.1")
	kljuc, drugi := kljucZaTest(t), kljucZaTest(t)
	var mjesta []*mjestoTunela
	for i := range 3 {
		m, ok := o.uzmiStranca(a)
		if !ok {
			t.Fatalf("stranac %d nije dobio mjesto", i+1)
		}
		if _, ok := o.uzmiStranca(a); ok {
			t.Fatal("dva stranca s mjestom za jednog")
		}
		mjesta = append(mjesta, m)
		if i < 2 && !m.Clan(kljuc) {
			t.Fatalf("član %d nije dobio mjesto", i+1)
		}
	}
	if mjesta[2].Clan(kljuc) {
		t.Error("treća veza istog ključa dobila je mjesto člana")
	}
	if !mjesta[0].Clan(kljuc) || mjesta[0].Clan(drugi) {
		t.Error("ponovni dokaz na istoj vezi")
	}
	if !mjesta[2].Clan(drugi) {
		t.Error("drugi ključ nije dobio mjesto člana")
	}
	if m, ok := o.uzmiStranca(a); !ok || m.Clan(kljucZaTest(t)) {
		t.Error("četvrti član preko ukupne granice")
	}
	for _, m := range mjesta {
		m.pusti()
		m.pusti()
	}
	if mjesta[0].Clan(kljuc) {
		t.Error("puštena veza je dobila mjesto člana")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.clanova != 0 || len(o.poKljucu) != 0 || o.stranaca != 1 {
		t.Errorf("nakon puštanja: %d članova, %d ključeva, %d stranaca", o.clanova, len(o.poKljucu), o.stranaca)
	}
}

// Kad su mjesta stranaca puna, klijent s kojeg je član dokazao ključ ulazi
// na svoja mjesta, a nepoznati ne
func TestPoznatiKlijentImaSvojaMjesta(t *testing.T) {
	o := novaOgradaTunela(granicaOgrade{stranaca: 1, poKlijentu: 2, poMrezi: 4, poznatih: 1, clanova: 4, poKljucu: 2})
	poznat := netip.MustParseAddr("192.0.2.10")
	m, _ := o.uzmiStranca(poznat)
	m.Clan(kljucZaTest(t))
	m.pusti()
	if _, ok := o.uzmiStranca(netip.MustParseAddr("198.51.100.1")); !ok {
		t.Fatal("prvi stranac")
	}
	if _, ok := o.uzmiStranca(netip.MustParseAddr("198.51.100.2")); ok {
		t.Error("nepoznati klijent preko granice stranaca")
	}
	p, ok := o.uzmiStranca(poznat)
	if !ok || !p.pricuva {
		t.Fatal("poznati klijent nije ušao na svoja mjesta")
	}
	if _, ok := o.uzmiStranca(poznat); ok {
		t.Error("poznati klijent preko svojih mjesta")
	}
	p.pusti()
	if _, ok := o.uzmiStranca(poznat); !ok {
		t.Error("pušteno mjesto poznatog klijenta")
	}
}

// Dok mnogo praznih veza s više adresa drži mjesta, član koji je već
// razmijenio s istog klijenta prolazi
func TestTunelUzPrazneVezePustaPoznatogClana(t *testing.T) {
	kljucA, kljucB := tajniZaTest(t), tajniZaTest(t)
	javniA := kljucA.Public().(ed25519.PublicKey)
	javniB := kljucB.Public().(ed25519.PublicKey)

	tunel := razmjena.NoviTunel()
	mux := http.NewServeMux()
	mux.Handle("GET "+razmjena.PutTunela, ograditiTunel(tunel.Handler()))
	srv := httptest.NewTLSServer((&Server{}).klijentSloj(mux))
	defer srv.Close()
	ctx, otkazi := context.WithCancel(context.Background())
	defer otkazi()
	vjeruje := func(k ed25519.PublicKey, _ []byte) bool { return k.Equal(javniA) }
	zatvori := func(c *razmjena.Conn) { c.Close() }
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, kljucB, "gocop", nil, tunel, vjeruje, zatvori, razmjena.NovaOgradaTunela(razmjena.NovaOgrada())) // staje s testom
	}()

	k := &razmjena.KlijentTunela{TLS: srv.Client().Transport.(*http.Transport).TLSClientConfig}
	pokusaj := func() error {
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		conn, err := k.DialTunel(c, kljucA, "gocop", nil, srv.URL, javniB)
		if err == nil {
			conn.Close()
		}
		return err
	}
	if err := pokusaj(); err != nil {
		t.Fatalf("bez praznih veza: %v", err)
	}

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	prazna := func(ip string) {
		for ctx.Err() == nil {
			cfg, _ := websocket.NewConfig("wss://"+u.Host+razmjena.PutTunela, "https://"+u.Host) // adrese su ispravne, iz poslužitelja testa
			cfg.TlsConfig = &tls.Config{InsecureSkipVerify: true}
			cfg.Header = http.Header{"Cf-Connecting-Ip": {ip}}
			ws, err := websocket.DialConfig(cfg)
			if err != nil {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			_, _ = ws.Read(make([]byte, 1)) // šuti dok je čvor ne zatvori
			ws.Close()
		}
	}
	for i := range 16 {
		for range 3 {
			go prazna(fmt.Sprintf("198.51.%d.1", i+1))
		}
	}
	time.Sleep(time.Second)
	for i := range 4 {
		if err := pokusaj(); err != nil {
			t.Errorf("pokušaj %d uz prazne veze: %v", i+1, err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}
