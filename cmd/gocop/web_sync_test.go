package main

// WEB sync u pokretanju (web_sync.go): pokreće se na utičnici kad se zna
// sastajalište, jednom po utičnici, nova utičnica ga pokreće iznova, a
// poruke sastajališta idu onom koji upravo radi.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"gocop/internal/config"
	"gocop/internal/razmjena"
)

// lazniWebCvor bilježi WEB syncove koje mu program preda
type lazniWebCvor struct {
	priv ed25519.PrivateKey

	mu        sync.Mutex
	pokrenuti []*razmjena.WebSync
	ukljuceni []bool
	prije     []func(context.Context)
	stalo     int
}

func noviLazniWebCvor(t *testing.T) *lazniWebCvor {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &lazniWebCvor{priv: priv}
}

func (c *lazniWebCvor) PostavkeWebSynca(context.Context) razmjena.PostavkeWeb {
	return razmjena.PostavkeWeb{Priv: c.priv, Protokol: "gocop", Trusted: func(ed25519.PublicKey, []byte) bool { return true }, Handle: func(*razmjena.Conn) {}}
}

func (c *lazniWebCvor) PokreniWebSync(_ context.Context, w *razmjena.WebSync, ukljucen bool, prije func(context.Context)) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pokrenuti, c.ukljuceni, c.prije = append(c.pokrenuti, w), append(c.ukljuceni, ukljucen), append(c.prije, prije)
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.stalo++
	}
}

func (c *lazniWebCvor) stanje() (pokrenuto, stalo int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pokrenuti), c.stalo
}

// udpZaTest je utičnica WEB synca na nasumičnom portu, s ključem čvora
func udpZaTest(t *testing.T, p *povezivost) *razmjena.UDP {
	t.Helper()
	conn, _, err := razmjena.OtvoriUDP(0) // nasumičan port: upozorenja o zauzetom portu nema
	if err != nil {
		t.Fatal(err)
	}
	u := razmjena.NoviUDPCvora(conn, p.kljucCvora())
	t.Cleanup(func() { _ = u.Close() }) // drugo zatvaranje ne smeta
	return u
}

// Na stalnom čvoru WEB sync kreće čim je utičnica tu, s vlastitim
// sastajalištem, jednom po utičnici; nova utičnica zaustavi stari i pokrene
// novi, a gašenje ga zaustavi. Poziv šalje kandidate iz zadnjeg mjerenja, a
// mreža se mjeri iznova tek kad je mjerenje starije od 20 s.
func TestWebSyncNaStalnomCvoru(t *testing.T) {
	c := noviLazniWebCvor(t)
	d := make(dnevnikPovezivosti, 16)
	ctx, otkazi := context.WithCancel(context.Background())
	t.Cleanup(otkazi)
	p := &povezivost{cfg: config.Povezivost{WebSyncPrijava: true, PosluziSastajaliste: true, WebSync: true, Stun: []string{lazniSTUN(t)}},
		javi: d.javi, ctx: ctx, webCvor: c}
	p.pokreniSignal(lazniCvor{}, noviLazniWeb())
	d.cekaj(t, "ovo računalo je sastajalište")
	p.pokreniWeb() // bez utičnice ništa
	p.udp = udpZaTest(t, p)
	p.pokreniWeb()
	p.pokreniWeb() // ista utičnica: isti WEB sync
	if n, _ := c.stanje(); n != 1 || !c.ukljuceni[0] || p.webRadi() != c.pokrenuti[0] {
		t.Fatalf("pokrenuto %d, %v", n, c.ukljuceni)
	}
	if k, n := p.mrezaZaPoziv(); k != nil || n.Vrsta != "" {
		t.Errorf("bez mjerenja: %v %+v", k, n)
	}
	c.prije[0](ctx)
	prvo := p.zadnjaProvjera()
	if k, _ := p.mrezaZaPoziv(); prvo == nil || len(k) == 0 || len(k) != len(prvo.Kandidati) {
		t.Fatalf("kandidati nakon mjerenja: %v (%+v)", k, prvo)
	}
	p.osvjeziMrezu(ctx)
	if p.zadnjaProvjera().Kad != prvo.Kad {
		t.Error("svježe mjerenje je ponovljeno")
	}
	p.mu.Lock()
	p.zadnja.Kad = time.Now().Add(-time.Minute)
	p.mu.Unlock()
	p.osvjeziMrezu(ctx)
	if !p.zadnjaProvjera().Kad.After(prvo.Kad) {
		t.Error("staro mjerenje nije ponovljeno")
	}
	for range 70 {
		p.naPoruku(razmjena.PotpisanaPoruka{})
	}
	p.naNemu("nepoznata", razmjena.NemaNijePrijavljen)
	if w := c.pokrenuti[0]; w.Odbaceno.Load() == 0 {
		t.Error("poruke sastajališta nisu stigle WEB syncu")
	}
	p.novaUticnica()
	if n, stalo := c.stanje(); n != 2 || stalo != 1 || p.webRadi() != c.pokrenuti[1] {
		t.Errorf("nova utičnica: pokrenuto %d, stalo %d", n, stalo)
	}
	p.zaustaviWeb()
	p.zaustaviWeb()
	if _, stalo := c.stanje(); stalo != 2 || p.webRadi() != nil {
		t.Errorf("gašenje: stalo %d", stalo)
	}
	p.naPoruku(razmjena.PotpisanaPoruka{}) // bez WEB synca poruka ne ide nikamo
	p.naNemu("nepoznata", razmjena.NemaNijePrijavljen)
	p.zatvoriSastajaliste()
}

// Uz prijavu kod sastajališta WEB sync kreće tek kad se zna ključ
// sastajališta iz članstva, s web_sync = false kao odbijanje; WEB sync
// koji se ne da napraviti ide u dnevnik. Bez čvora za WEB sync nema ni
// ključa za utičnicu.
func TestWebSyncSPrijavom(t *testing.T) {
	c := noviLazniWebCvor(t)
	d := make(dnevnikPovezivosti, 16)
	p := &povezivost{cfg: config.Povezivost{WebSyncPrijava: true}, javi: d.javi, ctx: context.Background(), webCvor: c}
	p.udp = udpZaTest(t, p)
	p.pokreniWeb() // bez sastajališta ništa
	var kljuc ed25519.PublicKey
	p.signal = &razmjena.KlijentSignala{Kljuc: func(context.Context) (ed25519.PublicKey, error) {
		if kljuc == nil {
			return nil, errors.New("ključ sastajališta nije poznat")
		}
		return kljuc, nil
	}}
	p.pokreniWeb()
	if n, _ := c.stanje(); n != 0 {
		t.Fatal("WEB sync bez ključa sastajališta")
	}
	kljuc = c.priv.Public().(ed25519.PublicKey)
	p.pokreniWeb()
	if n, _ := c.stanje(); n != 1 || c.ukljuceni[0] {
		t.Fatalf("nakon ključa: %d, %v", n, c.ukljuceni)
	}
	p.zaustaviWeb()

	pokvaren := append(ed25519.PrivateKey{}, c.priv...) // javni dio ne odgovara tajnom, pa TLS ne nastaje
	for i := ed25519.SeedSize; i < len(pokvaren); i++ {
		pokvaren[i] ^= 0xff
	}
	c.priv = pokvaren
	p.pokreniWeb()
	d.cekaj(t, "WEB sync ne radi")
	if (&povezivost{}).kljucCvora() != nil {
		t.Error("ključ bez čvora")
	}
}

// Prijava usred zamjene utičnice (nakonPrijave → pokreniWeb) ne ostavi
// upaljen WEB sync stare utičnice: na kraju radi točno jedan, na novoj.
func TestNovaUticnicaUzPrijavu(t *testing.T) {
	c := noviLazniWebCvor(t)
	kljuc := c.priv.Public().(ed25519.PublicKey)
	p := &povezivost{cfg: config.Povezivost{WebSyncPrijava: true}, javi: func(string, ...any) {}, ctx: context.Background(), webCvor: c,
		signal: &razmjena.KlijentSignala{Kljuc: func(context.Context) (ed25519.PublicKey, error) { return kljuc, nil }}}
	p.udp = udpZaTest(t, p)
	p.pokreniWeb()
	for range 20 {
		var wg sync.WaitGroup
		wg.Go(p.novaUticnica)
		wg.Go(p.pokreniWeb)
		wg.Wait()
	}
	p.mu.Lock()
	radi, u := p.web, p.udp
	p.mu.Unlock()
	t.Cleanup(func() { _ = u.Close() }) // zadnja utičnica; ranije zatvara zamjena
	if pokrenuto, stalo := c.stanje(); pokrenuto-stalo != 1 || radi == nil || radi.u != u {
		t.Errorf("pokrenuto %d, stalo %d, radi na novoj %v", pokrenuto, stalo, radi != nil && radi.u == u)
	}
	p.zaustaviWeb()
}
