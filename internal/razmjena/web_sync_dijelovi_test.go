package razmjena

// Dijelovi WEB synca bez cijele pripreme: kandidati druge strane, put
// QUIC-a, stroj uz dolaznu vezu, vjerodajnice u poruci, ključ za
// resetiranje, red poruka i rubovi priprema.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/testutils/simnet"
)

// Kandidat do kojeg promet ide preko vlastite Tailscale adrese (ruta
// podmreže, izlazni čvor) se izbacuje, kao i kandidat bez rute i adresa na
// koju se ne udara; ostali ostaju redom. Put QUIC-a je tailscale samo kad
// izlazi s Tailscale adrese.
func TestKandidatiDruge(t *testing.T) {
	ts := netip.MustParseAddr("100.101.102.103")
	lan := netip.MustParseAddr("192.168.5.20")
	izlaz := func(a netip.AddrPort) (netip.Addr, error) {
		switch {
		case a.Addr() == netip.MustParseAddr("10.9.9.9"):
			return netip.Addr{}, errors.New("network is unreachable")
		case a.Addr() == netip.MustParseAddr("192.168.1.23"), a.Addr() == netip.MustParseAddr("198.51.100.7"):
			return ts, nil // ruta podmreže i izlazni čvor kroz Tailscale
		}
		return lan, nil
	}
	kandidati := []KandidatPoruke{
		{Vrsta: KandidatLokalni, Adresa: "192.168.1.23:4713"},
		{Vrsta: KandidatJavni, Adresa: "198.51.100.7:4713"},
		{Vrsta: KandidatLokalni, Adresa: "10.9.9.9:4713"},
		{Vrsta: KandidatLokalni, Adresa: "nije-adresa"},
		{Vrsta: KandidatLokalni, Adresa: "192.168.5.30:4713"},
		{Vrsta: KandidatJavni, Adresa: "203.0.113.5:4713"},
	}
	ostali, izbaceni := kandidatiDruge(kandidati, []netip.Addr{ts}, izlaz)
	if !slices.Equal(ostali, kandidati[4:]) || len(izbaceni) != 4 || !strings.Contains(izbaceni[0], "Tailscale") || !strings.Contains(izbaceni[2], "nema rute") {
		t.Errorf("ostali %v, izbačeni %q", ostali, izbaceni)
	}
	if ostali, izbaceni := kandidatiDruge(kandidati[:2], nil, izlaz); len(ostali) != 2 || izbaceni != nil {
		t.Errorf("bez Tailscalea: %v %q", ostali, izbaceni)
	}
	for a, want := range map[string]Put{"192.168.1.23:4713": PutTailscale, "192.168.5.30:4713": PutProbijeno, "10.9.9.9:4713": PutProbijeno} {
		if got := putQUIC(netip.MustParseAddrPort(a), []netip.Addr{ts}, izlaz); got != want {
			t.Errorf("put do %s: %s", a, got)
		}
	}
}

// Kandidat do kojeg promet ide kroz Tailscale ne dobiva udarce ni u
// pravoj pripremi: A drugoj strani šalje samo odgovore na njezine udarce.
// Sesija čiji promet izlazi s Tailscale adrese vodi se kao tailscale.
func TestPripremaBezTailscaleKandidata(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, _ := javniPar(t, m)
		ts := netip.MustParseAddr("100.101.102.103")
		a.w.ts = func() []netip.Addr { return []netip.Addr{ts} }
		a.w.izlaz = func(d netip.AddrPort) (netip.Addr, error) {
			if d == vrataB {
				return ts, nil // izlazni čvor
			}
			return netip.MustParseAddr("192.0.2.1"), nil
		}
		var mu sync.Mutex
		vrste := map[byte]int{}
		m.Promatraj(func(p simnet.Packet) {
			if p.From.String() == vrataA.String() && jeUdarac(p.Data) {
				mu.Lock()
				vrste[p.Data[0]]++
				mu.Unlock()
			}
		})
		s, err := a.w.Pripremi(t.Context(), b.kljuc)
		bezGreske(t, err)
		mu.Lock()
		defer mu.Unlock()
		if s.Put != PutTailscale || vrste[vrstaUdarac] != 0 || vrste[vrstaOdgovor] == 0 {
			t.Errorf("put %s, A je poslao %v", s.Put, vrste)
		}
	})
}

// Stroj prima dolaznu QUIC vezu (DogadjajPrihvacen) samo na B, u svakoj
// fazi u kojoj probijanje traje; A je ne prima, a gotovo probijanje ništa.
func TestDolaznaVezaUStroju(t *testing.T) {
	prihvacena := Dogadjaj{Vrsta: DogadjajPrihvacen, Sad: t0}
	for _, faza := range []Faza{FazaCekaSinkro, FazaUdara, FazaCekaLokalni, FazaQUIC} {
		b := Probijanje{Faza: faza, rok: t0.Add(time.Second)}
		if p, r := b.Prijelaz(prihvacena); p.Faza != FazaUspjeh || !slices.Equal(vrsteRadnji(r), []VrstaRadnje{RadnjaStani, RadnjaKraj}) || !r[1].Uspjeh {
			t.Errorf("B u fazi %d: faza %d, %v", faza, p.Faza, r)
		}
		a := b
		a.a = true
		if p, r := a.Prijelaz(prihvacena); p.Faza != faza || r != nil {
			t.Errorf("A u fazi %d: faza %d, %v", faza, p.Faza, r)
		}
	}
	for _, faza := range []Faza{FazaCekaOdziv, FazaUspjeh, FazaNeuspjeh} {
		if p, r := (Probijanje{Faza: faza}).Prijelaz(prihvacena); p.Faza != faza || r != nil {
			t.Errorf("faza %d: %d %v", faza, p.Faza, r)
		}
	}
}

// Poziv nosi vjerodajnice pošiljatelja, a provjera ih predaje trusted:
// računalo koje primatelj još ne zna prođe s članstvom iz poruke. Tijelo s
// vjerodajnicama čita i 0.0.42 (oblik tijela bez polja vj), a potpis
// vrijedi nad bajtovima kako su stigli.
func TestPozivNosiVjerodajnice(t *testing.T) {
	p := novaProvjera(t)
	javniA := p.a.Public().(ed25519.PublicKey)
	p.pp.Vjeruje = func(k ed25519.PublicKey, vj []byte) bool { return k.Equal(javniA) && string(vj) == `{"clanstvo":"a"}` }
	tijelo := p.tijelo()
	tijelo.Vj = []byte(`{"clanstvo":"a"}`)
	poziv := Potpisi(p.a, "gocop", VrstaPoziv, tijelo)
	if got, err := p.pp.Provjeri(poziv); err != nil || string(got.Vj) != `{"clanstvo":"a"}` {
		t.Fatalf("poziv s vjerodajnicama: %+v %v", got, err)
	}
	if _, err := p.pp.Provjeri(Potpisi(p.a, "gocop", VrstaPoziv, p.tijelo())); !errors.Is(err, ErrNepoznatPosiljatelj) {
		t.Errorf("poziv bez vjerodajnica: %v", err)
	}
	// tijelo kako ga zna 0.0.42: bez polja vj
	var staro struct {
		Ver       int              `json:"ver"`
		Sesija    string           `json:"sesija"`
		Od        string           `json:"od"`
		Tajna     []byte           `json:"tajna,omitempty"`
		Kandidati []KandidatPoruke `json:"kandidati,omitempty"`
	}
	if err := json.Unmarshal(poziv.Tijelo, &staro); err != nil || staro.Sesija != tijelo.Sesija || string(staro.Tajna) != "tajna" {
		t.Errorf("0.0.42 čita %+v: %v", staro, err)
	}
	if !bytes.Contains(poziv.Tijelo, []byte(`"vj":`)) {
		t.Errorf("tijelo bez vjerodajnica: %s", poziv.Tijelo)
	}
}

// Ključ za resetiranje QUIC veza ovisi samo o ključu čvora: isti ključ,
// isti ključ resetiranja; nova utičnica (ObnoviUDP) ga zadrži, a bez ključa
// čvora ga nema.
func TestKljucResetaIzKljucaCvora(t *testing.T) {
	priv := newKey(t)
	if k1, k2 := kljucReseta(priv), kljucReseta(ed25519.NewKeyFromSeed(priv.Seed())); k1 == nil || *k1 != *k2 || *k1 == *kljucReseta(newKey(t)) {
		t.Error("ključ resetiranja ne ovisi samo o ključu čvora")
	}
	if kljucReseta(nil) != nil {
		t.Error("ključ resetiranja bez ključa čvora")
	}
	stari := otvoriUDP
	otvoriUDP = func(int) (net.PacketConn, error) { return net.ListenPacket("udp4", "127.0.0.1:0") }
	t.Cleanup(func() { otvoriUDP = stari })
	u := NoviUDPCvora(otvoriProbnuUDP(t), priv)
	obican := NoviUDP(otvoriProbnuUDP(t))
	defer obican.Close()
	if u.reset == nil || *u.reset != *kljucReseta(priv) || u.tr.StatelessResetKey != u.reset {
		t.Fatal("utičnica nema ključ resetiranja")
	}
	n, upozorenje, err := ObnoviUDP(u)
	if upozorenje != nil {
		t.Logf("nova utičnica: %v", upozorenje)
	}
	bezGreske(t, err)
	defer n.Close()
	if n.reset != u.reset || n.tr.StatelessResetKey != u.reset {
		t.Error("nova utičnica nema isti ključ resetiranja")
	}
	if obican.reset != nil {
		t.Error("NoviUDP ima ključ resetiranja")
	}
}

// otvoriProbnuUDP je UDP utičnica na petlji, bez slanja izvan računala
func otvoriProbnuUDP(t *testing.T) net.PacketConn {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	bezGreske(t, err)
	return c
}

// Pokvaren ključ ne da TLS ni slušalici ni pozivatelju, pa ni WEB sync ne
// nastaje.
func TestWebSyncPokvarenKljuc(t *testing.T) {
	if _, err := tlsPosluziteljaWeb(pokvarenKljuc(t), "testproto", nil); err == nil {
		t.Error("slušalica s pokvarenim ključem")
	}
	if _, err := tlsKlijentaWeb(pokvarenKljuc(t), "testproto", nil, "x", nil); err == nil {
		t.Error("pozivatelj s pokvarenim ključem")
	}
	if _, err := NoviWebSync(nil, PostavkeWeb{Priv: pokvarenKljuc(t), Protokol: "testproto"}); err == nil {
		t.Error("WEB sync s pokvarenim ključem")
	}
}

// Red poruka sastajališta je 64: NaPoruku nikad ne čeka, a što ne stane,
// odbaci. Kad utičnica već ima slušalicu QUIC-a, Radi javi grešku, a isti
// WebSync ne radi dvaput.
func TestRedPorukaIDvaRadi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		s := stranaJavna(t, m, vrataA.String(), newKey(t))
		w, err := NoviWebSync(s.u, PostavkeWeb{Priv: s.priv, Protokol: "testproto", Trusted: svima, Handle: func(*Conn) {},
			Posalji: func(PotpisanaPoruka) error { return nil }, Mreza: func() ([]KandidatPoruke, *NATPoruke) { return nil, nil }})
		bezGreske(t, err)
		for range redPorukaWeb + 3 {
			w.NaPoruku(PotpisanaPoruka{Vrsta: VrstaPoziv})
		}
		if w.Odbaceno.Load() != 3 {
			t.Errorf("odbačeno %d", w.Odbaceno.Load())
		}
		ln, err := s.u.tr.Listen(w.tls, postavkeQUIC())
		bezGreske(t, err)
		if err := w.Radi(t.Context()); err == nil || w.radi.Load() {
			t.Errorf("Radi uz tuđu slušalicu: %v", err)
		}
		bezGreske(t, ln.Close())
		ctx, otkazi := context.WithCancel(t.Context())
		gotov := make(chan error, 1)
		go func() { gotov <- w.Radi(ctx) }()
		for !w.radi.Load() {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(time.Second) // red se isprazni: poruke bez tijela se odbace
		if err := w.Radi(ctx); !errors.Is(err, ErrWebSyncVecRadi) {
			t.Errorf("drugi Radi: %v", err)
		}
		w.NaSesiju(nil) // zadano: ništa
		otkazi()
		if err := <-gotov; err != nil {
			t.Error(err)
		}
	})
}

// Rubovi pripreme bez stroja: ishod starije radnje QUIC se odbacuje,
// prekid poziva bez poziva ne smeta, poruka nepoznate vrste ne ide
// stroju, pun ulaz poruku odbaci, a predaja nakon kraja zatvori vezu.
func TestRuboviPripreme(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, _ := javniPar(t, m)
		_, otkazi := context.WithCancelCause(t.Context())
		p := a.w.novaPriprema(otkazi, b.kljuc, NovaSesija(), true)
		p.prekiniQUIC(Radnja{})
		p.p.Faza = FazaQUIC
		p.radnja = 2
		if _, ima := p.naIshod(1, nil, errors.New("stari")); ima {
			t.Error("ishod starije radnje stigao je stroju")
		}
		if d, ima := p.naIshod(2, nil, errors.New("pao")); !ima || d.Vrsta != DogadjajQUIC || d.Uspjeh {
			t.Errorf("ishod ove radnje: %+v", d)
		}
		if _, prima := p.prima("nepoznata", TijeloPoruke{}); prima {
			t.Error("stroj prima nepoznatu vrstu")
		}
		nista := func() (Dogadjaj, bool) { return Dogadjaj{}, false }
		for range cap(p.ulaz) + 1 {
			p.ponudi(nista)
		}
		close(p.kraj)
		p.predaj(nil, nista) // ulaz je pun, a priprema gotova: ne čeka
		if len(p.ulaz) != cap(p.ulaz) {
			t.Errorf("ulaz %d", len(p.ulaz))
		}
	})
}

// Odziv „probaj” bez valjane tajne: ključevi udaraca se ne daju izvesti,
// pa pozivatelj probijanje odmah prekida kao odbijeno. Odziv s tuđim
// potpisom nad tijelom druge strane stroj prima, ali ne prođe provjeru.
func TestOdzivBezTajne(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, posta := javniPar(t, m)
		stranac := newKey(t)
		posta.zadrziAko(func(pp PotpisanaPoruka) bool {
			if pp.Vrsta != VrstaOdziv {
				return false
			}
			o, _ := pp.Zaglavlje() // posta ga je već pročitala
			krivi := Potpisi(stranac, "testproto", VrstaOdziv, o)
			krivi.Tijelo = pp.Tijelo // tijelo B-a, potpis drugoga
			a.w.NaPoruku(krivi)
			o.Tajna, o.Izdano = []byte("kratko"), 0
			a.w.NaPoruku(Potpisi(b.priv, "testproto", VrstaOdziv, o))
			return true
		})
		if _, err := a.w.Pripremi(t.Context(), b.kljuc); greskaPripreme(err) != RazlogOdbijeno {
			t.Errorf("odziv bez tajne: %v", err)
		}
	})
}

// Pozdrav nosi znanje (Znanje) obiju strana, a priprema bez živog
// sastajališta (poziv ne ode) ne ostavlja ništa.
func TestPozdravNosiZnanje(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, _ := javniPar(t, m, func(p *PostavkeWeb) {
			z := &Znanje{Mogucnosti: []string{"web/1", "razmjena/1"}}
			p.Znanje = func() *Znanje { return z }
		})
		sa, sb := sesijaPara(t, a, b)
		for _, s := range []*SesijaWeb{sa, sb} {
			if s.Znanje == nil || !slices.Equal(s.Znanje.Mogucnosti, []string{"web/1", "razmjena/1"}) {
				t.Errorf("znanje druge strane: %+v", s.Znanje)
			}
		}
	})
}

// Poziv QUIC-a na adresu na kojoj nitko ne sluša istekne s rokom, a
// razmjena pozdrava na zatvorenoj vezi ne počne; neuspjeli pozdrav zatvori
// vezu.
func TestPozivIPozdravRubovi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		ov := otvoriVrata(t)
		a := &WebSync{PostavkeWeb: PostavkeWeb{Priv: ov.privA, Protokol: "testproto", Vj: func() []byte { return nil }, Znanje: func() *Znanje { return nil }}, u: ov.uA}
		ctx, otkazi := context.WithTimeout(t.Context(), time.Second)
		defer otkazi()
		if _, err := a.zovi(ctx, &priprema{druga: ov.b.kljuc, sesija: ov.p.sesija}, netip.MustParseAddrPort("198.51.100.200:4713")); err == nil {
			t.Error("poziv bez slušalice")
		}
		v, err := a.zovi(t.Context(), &priprema{druga: ov.b.kljuc, sesija: NovaSesija()}, vrataB)
		if err == nil || v == nil || v.veza.Context().Err() == nil {
			t.Errorf("pozdrav tuđe sesije: %v", err)
		}
		if err := v.razmijeniPozdrav(t.Context(), ov.p.sesija, nil); err == nil {
			t.Error("pozdrav na zatvorenoj vezi")
		}
		cfg, err := tlsConfig(ov.privA, "drugiproto")
		bezGreske(t, err)
		cfg.NextProtos = []string{alpnWeb("testproto")}
		_, _ = ov.uA.tr.Dial(t.Context(), net.UDPAddrFromAddrPort(vrataB), cfg, postavkeQUIC()) // ishod je na vratima
		if r, _ := ov.z.cekaj(t, 2); r != VrataDrugiProtokol {
			t.Errorf("drugo ime protokola na vratima: %s", r)
		}
	})
}
