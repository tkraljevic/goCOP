package razmjena

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/mrezatest"

	"golang.org/x/net/websocket"
)

// portUMemoriji je port razmjene čvora B na mreži u memoriji, sa zadanom
// ogradom porta; B vjeruje samo ključu člana
type portUMemoriji struct {
	m      *mrezatest.Mreza
	adresa string
	b      ed25519.PrivateKey
	clan   ed25519.PrivateKey
	ograda *Ograda
}

const adresaVrataB = "10.0.0.1:4710"

func noviPortUMemoriji(t *testing.T) *portUMemoriji {
	t.Helper()
	return noviPortNa(t, adresaVrataB)
}

// noviPortNa je portUMemoriji na adresi
func noviPortNa(t *testing.T, adresa string) *portUMemoriji {
	t.Helper()
	p := &portUMemoriji{m: mrezatest.NovaMreza(), adresa: adresa, b: newKey(t), clan: newKey(t), ograda: NovaOgrada()}
	ln, err := p.m.Slusaj(adresa)
	bezGreske(t, err)
	clan := p.clan.Public().(ed25519.PublicKey)
	vjeruje := func(k ed25519.PublicKey, _ []byte) bool { return k.Equal(clan) }
	odgovori := func(c *Conn) {
		defer c.Close()
		if _, err := c.Receive(); err == nil {
			odg, _ := NewEnvelope("odgovor", nil) // omotnica bez sadržaja ne pada
			_ = c.Send(odg)                       // je li odgovor stigao, provjerava član
		}
	}
	go func() {
		_ = ServeExchangeOn(t.Context(), p.b, "testproto", nil, ln, vjeruje, odgovori, p.ograda) // staje s testom
	}()
	return p
}

// razmijeni: član s adrese ip razmijeni jednu poruku s B; vraća grešku i
// koliko je trajalo
func (p *portUMemoriji) razmijeni(t *testing.T, ip string) (time.Duration, error) {
	t.Helper()
	pocetak := time.Now()
	ctx, otkazi := context.WithTimeout(t.Context(), 15*time.Second)
	defer otkazi()
	raw, err := p.m.DialContextOd(ip)(ctx, "tcp", p.adresa)
	if err != nil {
		return time.Since(pocetak), err
	}
	c, err := DialExchangeOn(ctx, raw, p.clan, "testproto", nil, p.adresa, p.b.Public().(ed25519.PublicKey))
	if err != nil {
		return time.Since(pocetak), err
	}
	defer c.Close()
	_, err = razgovaraj(c)
	return time.Since(pocetak), err
}

// tihe otvara n veza s adrese ip koje ništa ne šalju; zatvaraju se s testom
func (p *portUMemoriji) tihe(t *testing.T, ip string, n int) {
	t.Helper()
	for range n {
		c, err := p.m.DialContextOd(ip)(t.Context(), "tcp", p.adresa)
		bezGreske(t, err)
		t.Cleanup(func() { c.Close() })
	}
}

// Jedna adresa koja otvara tihe veze (64 odjednom, pa još dvije u sekundi)
// drži najviše četiri mjesta za rukovanje, pa član s druge adrese razmijeni
// odmah, a ne tek nakon roka rukovanja. Bez tihih veza razmjena prolazi isto.
func TestVrataRukovanjaPoAdresi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		p := noviPortUMemoriji(t)
		if _, err := p.razmijeni(t, "10.0.0.5"); err != nil {
			t.Fatalf("razmjena bez tihih veza: %v", err)
		}
		p.tihe(t, "10.0.0.66", 64)
		stani := make(chan struct{})
		go func() {
			for {
				select {
				case <-stani:
					return
				case <-time.After(500 * time.Millisecond):
					p.tihe(t, "10.0.0.66", 1)
				}
			}
		}()
		defer close(stani)
		time.Sleep(100 * time.Millisecond)
		trajanje, err := p.razmijeni(t, "10.0.0.7")
		if err != nil || trajanje > time.Second {
			t.Fatalf("član uz tihe veze s druge adrese: %v nakon %v", err, trajanje)
		}
	})
}

// Veza koja ne pošalje cijelu prvu poruku rukovanja (ClientHello) gubi
// mjesto nakon 2 s, a ne tek nakon roka cijelog rukovanja (15 s): bez ijednog
// bajta, s jednim bajtom i sa zaglavljem zapisa bez ostatka
func TestVrataRokPrvePorukeRukovanja(t *testing.T) {
	for ime, poslano := range map[string][]byte{
		"ništa":            nil,
		"jedan bajt":       {0x16},
		"zaglavlje zapisa": {0x16, 0x03, 0x01, 0x02, 0x00, 0x01, 0x00},
	} {
		t.Run(ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				p := noviPortUMemoriji(t)
				c, err := p.m.DialContextOd("10.0.0.66")(t.Context(), "tcp", adresaVrataB)
				bezGreske(t, err)
				defer c.Close()
				pocetak := time.Now()
				_, err = c.Write(poslano)
				bezGreske(t, err)
				_, err = c.Read(make([]byte, 1))
				if !errors.Is(err, io.EOF) {
					t.Fatalf("veza bez prve poruke: %v", err)
				}
				if d := time.Since(pocetak); d < rokClientHello || d > rokClientHello+100*time.Millisecond {
					t.Errorf("veza zatvorena nakon %v, a ne nakon %v", d, rokClientHello)
				}
			})
		})
	}
}

// TLS vrata zadržavaju GetConfigForClient koji je postavio pozivatelj, a
// vezi prije rukovanja postave rok cijelog rukovanja
func TestRokNakonClientHellaZadrzavaPostavke(t *testing.T) {
	poseban := &tls.Config{}
	cfg := sRokomNakonPozdrava(&tls.Config{GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) { return poseban, nil }})
	a, b := mrezatest.Par(mrezatest.TCP("10.0.0.1:1"), mrezatest.TCP("10.0.0.2:2"))
	defer a.Close()
	defer b.Close()
	_ = a.SetDeadline(time.Now().Add(-time.Second)) // rok je već istekao
	c, err := cfg.GetConfigForClient(&tls.ClientHelloInfo{Conn: &doPozdrava{Conn: a, rok: time.Now().Add(time.Minute)}})
	if err != nil || c != poseban {
		t.Fatalf("postavke pozivatelja: %v %v", c, err)
	}
	go func() { _, _ = b.Write([]byte{1}) }() // čitanje niže čeka taj bajt
	if _, err := a.Read(make([]byte, 1)); err != nil {
		t.Errorf("nakon ClientHella rok nije produžen: %v", err)
	}
	if c, err := sRokomNakonPozdrava(&tls.Config{}).GetConfigForClient(&tls.ClientHelloInfo{}); c != nil || err != nil {
		t.Errorf("bez postavki pozivatelja: %v %v", c, err)
	}
}

// U lokalnoj mreži IPv6 računala dijele mrežu /64, pa se na portu broje po
// cijeloj adresi: privatna (ULA) i javna iz iste mreže /64 kao port. Javna
// IPv6 izvana i dalje se broji po mreži /64.
func TestVrataIPv6ULokalnojMrezi(t *testing.T) {
	for _, s := range []struct {
		port, tihe, clan string
		prolazi          bool
	}{
		{"[fd00:1::1]:4710", "fd00:1::66", "fd00:1::7", true},
		{"[2001:db8:5:6::1]:4710", "2001:db8:5:6::66", "2001:db8:5:6::7", true},
		{"[2001:db8:5:6::1]:4710", "2001:db8:9:9::66", "2001:db8:9:9::7", false},
	} {
		t.Run(s.clan, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				p := noviPortNa(t, s.port)
				p.tihe(t, s.tihe, najviseRukovanjaPoAdresi)
				time.Sleep(100 * time.Millisecond)
				trajanje, err := p.razmijeni(t, s.clan)
				if prosao := err == nil && trajanje < time.Second; prosao != s.prolazi {
					t.Errorf("član uz četiri tihe veze iz iste mreže /64: %v nakon %v", err, trajanje)
				}
			})
		})
	}
}

// Kad tihe veze s više adresa zauzmu sva rukovanja, nova adresa odmah dobije
// zatvorenu vezu (ne čeka u redu), a adresa s koje je član već razmijenio
// ima svoja mjesta i prolazi
func TestVrataPricuvaZaPoznateAdrese(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		p := noviPortUMemoriji(t)
		odbijeno := make(chan RazlogVrata, 1)
		p.ograda.KadOdbije(func(r RazlogVrata, _ ed25519.PublicKey) {
			select {
			case odbijeno <- r:
			default:
			}
		})
		if _, err := p.razmijeni(t, "10.0.0.5"); err != nil {
			t.Fatalf("razmjena prije tihih veza: %v", err)
		}
		for i := range najviseRukovanja / najviseRukovanjaPoAdresi {
			p.tihe(t, fmt.Sprintf("10.0.1.%d", i+1), najviseRukovanjaPoAdresi)
		}
		time.Sleep(100 * time.Millisecond)
		if trajanje, err := p.razmijeni(t, "10.0.0.9"); err == nil || trajanje > time.Second {
			t.Errorf("nova adresa uz puna rukovanja: %v nakon %v", err, trajanje)
		}
		if r := <-odbijeno; r != VrataRukovanje {
			t.Errorf("razlog odbijene veze: %q", r)
		}
		if trajanje, err := p.razmijeni(t, "10.0.0.5"); err != nil || trajanje > time.Second {
			t.Errorf("poznata adresa uz puna rukovanja: %v nakon %v", err, trajanje)
		}
	})
}

// Odbijanja na ulazu javljaju se jedno po jedno: dok se jedno zapisuje,
// ostala se samo zatvore
func TestOdbijanjeNaUlazuJednoPoJedno(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o := NovaOgrada()
		o.odbijNaUlazu() // bez KadOdbije nema što javiti
		pusti := make(chan struct{})
		var javljeno atomic.Int32
		o.KadOdbije(func(r RazlogVrata, _ ed25519.PublicKey) {
			javljeno.Add(1)
			<-pusti
		})
		for range 100 {
			o.odbijNaUlazu()
		}
		synctest.Wait()
		if n := javljeno.Load(); n != 1 {
			t.Errorf("dok se zapisuje jedno, javljeno je %d", n)
		}
		close(pusti)
		synctest.Wait()
		o.odbijNaUlazu()
		synctest.Wait()
		if n := javljeno.Load(); n != 2 {
			t.Errorf("nakon zapisa javljeno je %d", n)
		}
	})
}

// Ključ adrese: IPv4 (i IPv4 u IPv6) je sama adresa, javna IPv6 mreža /64,
// a privatna, lokalna i Tailscaleova IPv6 cijela adresa bez zone
func TestKljucAdrese(t *testing.T) {
	for _, s := range []struct{ adresa, kljuc string }{
		{"192.0.2.7", "192.0.2.7"},
		{"::ffff:192.0.2.7", "192.0.2.7"},
		{"2001:db8:1:2:aaaa::1", "2001:db8:1:2::"},
		{"fe80::1%en0", "fe80::1"},
		{"fd00:1:2:3::5", "fd00:1:2:3::5"},
		{"fd7a:115c:a1e0:ab12:4843:cd96:6200:1", "fd7a:115c:a1e0:ab12:4843:cd96:6200:1"},
	} {
		if k := KljucAdrese(netip.MustParseAddr(s.adresa)); k.String() != s.kljuc {
			t.Errorf("%s: %s", s.adresa, k)
		}
	}
	if KljucAdrese(netip.Addr{}).IsValid() || MrezaAdrese(netip.Addr{}).IsValid() {
		t.Error("neispravna adresa je dobila ključ")
	}
}

// Izvor veze na portu: javna adresa izvana ima adresu i mrežu, privatna i
// javna IPv6 iz mreže /64 porta samo adresu, a veza bez IP adrese ništa
func TestIzvorVeze(t *testing.T) {
	port := &net.TCPAddr{IP: net.ParseIP("2001:db8:5:6::1"), Port: 4710}
	for _, s := range []struct {
		udaljena      net.Addr
		adresa, mreza string
	}{
		{&net.TCPAddr{IP: net.ParseIP("2001:db8:1:2::5"), Port: 50000}, "2001:db8:1:2::", "2001:db8:1::"},
		{&net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 50000}, "203.0.113.9", "203.0.113.0"},
		{&net.TCPAddr{IP: net.ParseIP("2001:db8:5:6::5"), Port: 50000}, "2001:db8:5:6::5", "invalid IP"},
		{&net.TCPAddr{IP: net.ParseIP("192.168.1.9"), Port: 50000}, "192.168.1.9", "invalid IP"},
		{&net.TCPAddr{IP: net.ParseIP("100.101.102.103"), Port: 50000}, "100.101.102.103", "invalid IP"},
		{nil, "invalid IP", "invalid IP"},
		{tunelAdresa("tunel" + PutTunela), "invalid IP", "invalid IP"},
	} {
		iz := izvorVeze(s.udaljena, port)
		if iz.adresa.String() != s.adresa || iz.mreza.String() != s.mreza {
			t.Errorf("%v: %s, %s", s.udaljena, iz.adresa, iz.mreza)
		}
	}
	if iz := izvorVeze(&net.TCPAddr{IP: net.ParseIP("2001:db8:5:6::5"), Port: 1}, &net.TCPAddr{IP: net.ParseIP("10.0.0.1")}); !iz.mreza.IsValid() {
		t.Error("IPv6 izvana uz IPv4 port nema mrežu")
	}
}

// Na portu s jedne javne mreže teče najviše osam rukovanja (dvije adrese po
// četiri), druga mreža i privatne adrese se ne broje u nju, a poznata adresa
// ima osam rukovanja i ne gleda mrežu
func TestVrataRukovanjaPoMrezi(t *testing.T) {
	o := NovaOgrada()
	lokalna := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 4710}
	izvorS := func(ip string) izvor { return izvorVeze(&net.TCPAddr{IP: net.ParseIP(ip), Port: 50000}, lokalna) }
	var pustanja []func()
	uzmi := func(ip string, n int) {
		t.Helper()
		for i := range n {
			pusti, ok := o.mjestoRukovanja(izvorS(ip))
			if !ok {
				t.Fatalf("%s: rukovanje %d bez mjesta", ip, i+1)
			}
			pustanja = append(pustanja, pusti)
		}
	}
	uzmi("203.0.113.1", najviseRukovanjaPoAdresi)
	uzmi("203.0.113.2", najviseRukovanjaPoAdresi)
	if _, ok := o.mjestoRukovanja(izvorS("203.0.113.3")); ok {
		t.Error("deveto rukovanje iste mreže /24")
	}
	uzmi("198.51.100.1", 1)
	uzmi("10.0.0.5", najviseRukovanjaPoAdresi)
	uzmi("10.0.0.6", najviseRukovanjaPoAdresi)
	o.poznate.Zapamti(netip.MustParseAddr("203.0.113.9"), ed25519.PublicKey("clan"))
	uzmi("203.0.113.9", najviseRukovanjaPoznate)
	if _, ok := o.mjestoRukovanja(izvorS("203.0.113.9")); ok {
		t.Error("poznata adresa preko svojih rukovanja")
	}
	for _, pusti := range pustanja {
		pusti()
	}
	if len(o.poAdresi) != 0 || len(o.poMrezi) != 0 || len(o.rukovanja) != 0 {
		t.Errorf("nakon puštanja: %d adresa, %d mreža, %d rukovanja", len(o.poAdresi), len(o.poMrezi), len(o.rukovanja))
	}
}

// Jedan ključ drži najviše četiri poznate adrese: peta istisne njegovu
// najstariju, a adrese drugih ključeva ostaju
func TestPoznateAdresePoKljucu(t *testing.T) {
	sada := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	p := NovePoznateAdrese()
	p.sat = func() time.Time { sada = sada.Add(time.Second); return sada }
	drugi := netip.MustParseAddr("192.0.2.1")
	p.Zapamti(drugi, ed25519.PublicKey("drugi"))
	adresa := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{198, 51, 100, byte(i)}) }
	for i := range najviseAdresaPoKljucu + 1 {
		p.Zapamti(adresa(i), ed25519.PublicKey("jedan"))
	}
	if p.Zna(adresa(0)) || !p.Zna(adresa(1)) || !p.Zna(adresa(najviseAdresaPoKljucu)) || !p.Zna(drugi) {
		t.Errorf("adrese jednog ključa: %d u pamćenju", len(p.kad))
	}
	p.Zapamti(adresa(1), ed25519.PublicKey("jedan")) // ponovni dokaz s iste adrese ne istiskuje
	p.Zapamti(drugi, ed25519.PublicKey("jedan"))     // adresa drugog ključa prelazi na ovaj
	if !p.Zna(drugi) || p.Zna(adresa(2)) || !p.Zna(adresa(1)) {
		t.Errorf("nakon prijelaza adrese: %d u pamćenju", len(p.kad))
	}
}

// Kad web ograda ključu ne da mjesto člana, vrata granicu druge strane čekaju
// najviše 2 s, pa odbijenica stiže i kad druga strana šuti
func TestTunelBezMjestaClanaOdbijaUKratkomRoku(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		kljucA, kljucB := newKey(t), newKey(t)
		tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
		tu.web(t, "puno.primjer.hr:443", saMjestom(bezMjesta{}, tu.tunel.Handler()))
		c, err := tu.klijent.DialTunel(t.Context(), kljucA, "gocop-test", nil, "https://puno.primjer.hr", kljucB.Public().(ed25519.PublicKey))
		bezGreske(t, err)
		defer c.Close()
		pocetak := time.Now()
		e, err := c.Receive()
		if err != nil || e.Kind != VrstaOdbijeno || e.Kod != KodZauzet {
			t.Fatalf("bez mjesta člana: %+v %v", e, err)
		}
		if d := time.Since(pocetak); d > rokOdbijanjaBezMjesta+time.Second {
			t.Errorf("odbijenica je stigla nakon %v", d)
		}
	})
}

// Poznata adresa vrijedi 24 sata od zadnjeg dokaza ključa; kad je pamćenje
// puno, zaboravi se najstarija
func TestPoznateAdrese(t *testing.T) {
	sada := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	p := NovePoznateAdrese()
	p.sat = func() time.Time { return sada }
	prva := netip.MustParseAddr("192.0.2.1")
	p.Zapamti(prva, ed25519.PublicKey("prvi"))
	p.Zapamti(netip.Addr{}, ed25519.PublicKey("prvi"))
	if !p.Zna(prva) || p.Zna(netip.Addr{}) {
		t.Fatal("zapamćena adresa")
	}
	sada = sada.Add(pamtiPoznatu + time.Second)
	if p.Zna(prva) {
		t.Error("adresa vrijedi i nakon 24 sata")
	}
	for i := range najvisePoznatih + 1 {
		sada = sada.Add(time.Second)
		p.Zapamti(netip.AddrFrom4([4]byte{198, 51, byte(i >> 8), byte(i)}), ed25519.PublicKey{byte(i >> 8), byte(i)})
	}
	if len(p.kad) != najvisePoznatih || p.Zna(netip.MustParseAddr("198.51.0.0")) || !p.Zna(netip.MustParseAddr("198.51.0.1")) {
		t.Errorf("puno pamćenje: %d adresa", len(p.kad))
	}
}

// Web ograda koja ključu ne da mjesto člana: veza dobije odbijenicu
// zauzetog čvora i ne dođe do razmjene
func TestTunelBezMjestaClanaOdbija(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		kljucA, kljucB := newKey(t), newKey(t)
		tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
		tu.web(t, "puno.primjer.hr:443", saMjestom(bezMjesta{}, tu.tunel.Handler()))
		c, err := tu.klijent.DialTunel(t.Context(), kljucA, "gocop-test", nil, "https://puno.primjer.hr", kljucB.Public().(ed25519.PublicKey))
		bezGreske(t, err)
		defer c.Close()
		e, err := razgovaraj(c)
		if err != nil || e.Kind != VrstaOdbijeno || e.Kod != KodZauzet {
			t.Fatalf("bez mjesta člana: %+v %v", e, err)
		}
		if n := tu.obrada.Load(); n != 0 {
			t.Errorf("obrađeno je %d veza", n)
		}
	})
}

type bezMjesta struct{}

func (bezMjesta) Clan(ed25519.PublicKey) bool { return false }

// saMjestom daje rukovatelju mjesto web ograde, kao što to radi web
func saMjestom(m MjestoVeze, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(SMjestom(r.Context(), m)))
	})
}

// Odgovor na nadogradnju tunela čita se najviše do 64 KiB: beskonačno
// zaglavlje prekida spajanje odmah, jasnom greškom
func TestNadogradnjaTunelaOgranicenaVelicinom(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		tu := noviTunelUMemoriji(t, newKey(t), "gocop-test", nil)
		tu.web(t, "dugi.primjer.hr:443", http.HandlerFunc(beskonacnoZaglavlje))
		pocetak := time.Now()
		_, _, err := tu.klijent.uTunel(t.Context(), "https://dugi.primjer.hr", PutTunela)
		if !errors.Is(err, errOdgovorNadogradnje) || !strings.HasPrefix(err.Error(), "tunel https://dugi.primjer.hr: ") {
			t.Fatalf("beskonačno zaglavlje: %v", err)
		}
		if d := time.Since(pocetak); d > time.Second {
			t.Errorf("spajanje je stalo tek nakon %v", d)
		}
	})
}

// beskonacnoZaglavlje odgovori 101 i jednim zaglavljem bez kraja, do 8 MiB,
// pa šuti dok druga strana ne zatvori vezu
func beskonacnoZaglavlje(w http.ResponseWriter, _ *http.Request) {
	nc, bw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer nc.Close()
	_, _ = bw.WriteString("HTTP/1.1 101 Switching Protocols\r\nX-A: ")
	komad := []byte(strings.Repeat("A", 64<<10))
	for range 128 {
		if _, err := bw.Write(komad); err != nil || bw.Flush() != nil {
			return
		}
	}
	_, _ = io.Copy(io.Discard, nc) // čita dok druga strana ne zatvori; ishod je na strani klijenta
}

// Odgovor s velikim, ali dopuštenim zaglavljem (40 KiB) i dalje daje
// WebSocket kroz koji razmjena radi
func TestNadogradnjaSVelikimZaglavljem(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		kljucA, kljucB := newKey(t), newKey(t)
		tu := noviTunelUMemoriji(t, kljucB, "gocop-test", kljucA.Public().(ed25519.PublicKey))
		ws := websocket.Server{
			Handshake: func(cfg *websocket.Config, _ *http.Request) error {
				cfg.Header = http.Header{"X-Veliko": {strings.Repeat("a", 40<<10)}}
				return nil
			},
			Handler: tu.tunel.Handler().(websocket.Server).Handler,
		}
		tu.web(t, "velik.primjer.hr:443", ws)
		c, err := tu.klijent.DialTunel(t.Context(), kljucA, "gocop-test", nil, "https://velik.primjer.hr", kljucB.Public().(ed25519.PublicKey))
		bezGreske(t, err)
		defer c.Close()
		if e, err := razgovaraj(c); err != nil || e.Kind != "odgovor" {
			t.Fatalf("razmjena uz veliko zaglavlje: %+v %v", e, err)
		}
	})
}

// mjestoKojeBroji daje mjesto člana i pamti ključeve koji su ga tražili
type mjestoKojeBroji struct {
	mu       sync.Mutex
	kljucevi []string
}

func (m *mjestoKojeBroji) Clan(k ed25519.PublicKey) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kljucevi = append(m.kljucevi, PublicKeyString(k))
	return true
}

// Sastajalište nakon rukovanja javi web ogradi da je računalo član. Kad
// ograda mjesto člana ne da, računalo dobije „nedostupno” (pokuša ponovno)
// i ne ulazi u registar; inače se prijavi, a ograda zna njegov ključ.
func TestSastajalisteTraziMjestoClana(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		su.mu.Lock()
		su.mjesto = bezMjesta{}
		su.mu.Unlock()
		kljucA := newKey(t)
		su.clan("laptop", kljucA)
		a := su.klijent(kljucA)
		pokreni(t, a)
		cekajStanje(t, a, "bez mjesta člana", func(st StanjeSignala) bool { return st.Kod == KodSignalaNedostupno })
		if n := su.r.Stanje().Prijavljenih; n != 0 {
			t.Errorf("u registru %d", n)
		}

		broji := &mjestoKojeBroji{}
		su.mu.Lock()
		su.mjesto = broji
		su.mu.Unlock()
		kljucB := newKey(t)
		su.clan("ured", kljucB)
		b := su.klijent(kljucB)
		pokreni(t, b)
		cekajStanje(t, b, "B prijavljen", prijavljen)
		broji.mu.Lock()
		defer broji.mu.Unlock()
		if !slices.Contains(broji.kljucevi, PublicKeyString(kljucB.Public().(ed25519.PublicKey))) {
			t.Errorf("ograda nije dobila ključ člana: %v", broji.kljucevi)
		}
	})
}

// Poznata adresa ulazi u pričuvu samo dok u njoj ima mjesta; puštena mjesta
// vraćaju se i adresi
func TestPricuvaRukovanjaImaGranicu(t *testing.T) {
	o := novaOgrada(1, 1, time.Second)
	poznata := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}) }
	for i := range pricuvaPoznatih + 1 {
		o.poznate.Zapamti(poznata(i), ed25519.PublicKey{byte(i)})
	}
	prvo, ok := o.mjestoRukovanja(izvor{adresa: netip.MustParseAddr("198.51.100.1")})
	if !ok {
		t.Fatal("prvo rukovanje")
	}
	if _, ok := o.mjestoRukovanja(izvor{adresa: netip.MustParseAddr("198.51.100.2")}); ok {
		t.Fatal("nepoznata adresa u pričuvi")
	}
	pustanja := []func(){prvo}
	for i := range pricuvaPoznatih {
		pusti, ok := o.mjestoRukovanja(izvor{adresa: poznata(i)})
		if !ok {
			t.Fatalf("poznata adresa %d bez mjesta u pričuvi", i)
		}
		pustanja = append(pustanja, pusti)
	}
	if _, ok := o.mjestoRukovanja(izvor{adresa: poznata(pricuvaPoznatih)}); ok {
		t.Error("poznata adresa preko pričuve")
	}
	for _, pusti := range pustanja {
		pusti()
		pusti()
	}
	if len(o.rukovanja) != 0 || len(o.pricuva) != 0 || len(o.poAdresi) != 0 {
		t.Errorf("nakon puštanja: %d rukovanja, %d u pričuvi, %d adresa", len(o.rukovanja), len(o.pricuva), len(o.poAdresi))
	}
}

// Adresa s koje je član već razmijenio (npr. ured iza jedne adrese) ima osam
// rukovanja odjednom: uz četiri tihe veze s nje član i dalje prolazi, a nova
// adresa ne
func TestVrataPoznataAdresaImaViseRukovanja(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		p := noviPortUMemoriji(t)
		if _, err := p.razmijeni(t, "10.0.0.5"); err != nil {
			t.Fatalf("prva razmjena: %v", err)
		}
		p.tihe(t, "10.0.0.5", najviseRukovanjaPoAdresi)
		p.tihe(t, "10.0.0.6", najviseRukovanjaPoAdresi)
		time.Sleep(100 * time.Millisecond)
		if trajanje, err := p.razmijeni(t, "10.0.0.5"); err != nil || trajanje > time.Second {
			t.Errorf("poznata adresa uz četiri tihe veze: %v nakon %v", err, trajanje)
		}
		if _, err := p.razmijeni(t, "10.0.0.6"); err == nil {
			t.Error("nova adresa uz četiri tihe veze")
		}
	})
}
