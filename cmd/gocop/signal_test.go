package main

// Sastajalište i prijava u pokretanju (signal.go)

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gocop/internal/config"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/web"
)

// lazniCvor gradi prijavu bez sastajališta u blizini i sastajalište s
// novim ključem (ili grešku)
type lazniCvor struct {
	greskaSastajalista error
}

func (lazniCvor) KlijentSignala(adresa, cvor string, javi func(string, ...any)) *razmjena.KlijentSignala {
	return &razmjena.KlijentSignala{Adresa: adresa, Javi: javi,
		Kljuc: func(context.Context) (ed25519.PublicKey, error) {
			return nil, fmt.Errorf("%w: %s", razmjena.ErrNepoznatCvorSastajalista, cvor)
		}}
}

func (c lazniCvor) Sastajaliste(javi func(string, ...any)) (*razmjena.Sastajaliste, error) {
	if c.greskaSastajalista != nil {
		return nil, c.greskaSastajalista
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return razmjena.NovoSastajaliste(razmjena.PostavkeSastajalista{Priv: priv, Protokol: "gocop", Javi: javi, Sat: time.Now,
		Vjeruje: func(ed25519.PublicKey, []byte) bool { return true }, Ime: func(ed25519.PublicKey) string { return "" }})
}

// Stalni čvor poslužuje sastajalište i pokazuje svoje brojke; računalo
// bez upisanog sastajališta to javlja; računalo s postavkama se prijavljuje
// i javlja stanje s domenom sastajališta; sastajalište koje se ne da
// napraviti ide u dnevnik
func TestPokreniSignal(t *testing.T) {
	d := make(dnevnikPovezivosti, 8)
	w := noviLazniWeb()
	staro := razmakProvjereClanstava
	t.Cleanup(func() { razmakProvjereClanstava = staro })
	razmakProvjereClanstava = 10 * time.Millisecond
	ctxS, otkaziS := context.WithCancel(context.Background())
	p := &povezivost{cfg: config.Povezivost{WebSyncPrijava: true, PosluziSastajaliste: true, WebSync: true}, javi: d.javi, ctx: ctxS}
	p.pokreniSignal(lazniCvor{}, w)
	d.cekaj(t, "ovo računalo je sastajalište")
	if st := w.signalStanje(); w.signal == nil || !st.Sastajaliste || !st.WebSync || st.Prijavljenih != 0 {
		t.Errorf("sastajalište: %+v", st)
	}
	time.Sleep(50 * time.Millisecond) // nekoliko provjera članstava
	otkaziS()
	p.teku.Wait()
	p.zatvoriSastajaliste()

	w = noviLazniWeb()
	(&povezivost{cfg: config.Povezivost{WebSyncPrijava: true, Sastajaliste: "https://cop.primjer.hr"}, javi: d.javi}).pokreniSignal(lazniCvor{}, w)
	if st := w.signalStanje(); st.Kod != razmjena.KodSignalaBezPostavke || w.signal != nil {
		t.Errorf("bez čvora sastajališta: %+v", st)
	}

	w = noviLazniWeb()
	p = &povezivost{cfg: config.Povezivost{WebSyncPrijava: true, Sastajaliste: "https://Cop.Primjer.hr", SastajalisteCvor: "cop-osijek"}, javi: d.javi}
	p.pokreniSignal(lazniCvor{}, w)
	if st := w.signalStanje(); st.Posluzitelj != "cop.primjer.hr" || st.WebSync || p.klijent == nil || p.klijent.(*razmjena.KlijentSignala).Adresa != "https://Cop.Primjer.hr" {
		t.Errorf("prijava: %+v", st)
	}
	ctx, otkazi := context.WithCancel(context.Background())
	p.prijaviSe(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for w.signalStanje().Kod != razmjena.KodSignalaNepoznatCvor && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if st := w.signalStanje(); st.Kod != razmjena.KodSignalaNepoznatCvor {
		t.Errorf("nepoznat čvor sastajališta: %+v", st)
	}
	otkazi()
	p.teku.Wait()

	w = noviLazniWeb()
	d = make(dnevnikPovezivosti, 8)
	(&povezivost{cfg: config.Povezivost{WebSyncPrijava: true, PosluziSastajaliste: true}, javi: d.javi}).pokreniSignal(lazniCvor{greskaSastajalista: errors.New("pokvaren ključ")}, w)
	d.cekaj(t, "sastajalište ne radi: pokvaren ključ")
	if w.signal != nil || w.signalStanje != nil {
		t.Error("sastajalište koje ne radi predano je webu")
	}
	if domenaSastajalista("https://%zz") != "" || domenaSastajalista(" https://cop.primjer.hr:8443/x ") != "cop.primjer.hr" {
		t.Error("domena sastajališta")
	}
}

// Nakon promjene mreže: prijava iznova, nova utičnica na istom portu i
// nova provjera mreže; utičnica koja se ne da otvoriti ide u dnevnik, a
// bez utičnice nema ni rute ni nove utičnice
func TestPonovnoNakonPromjeneMreze(t *testing.T) {
	d := make(dnevnikPovezivosti, 16)
	p := &povezivost{cfg: config.Povezivost{WebSyncPrijava: true, Stun: []string{lazniSTUN(t)}}, javi: d.javi, ctx: context.Background()}
	p.puls() // bez prijave ne radi ništa
	if p.bezRute() {
		t.Error("bez utičnice nema ni rute")
	}
	p.novaUticnica() // bez utičnice ne radi ništa
	conn, upozorenje, err := razmjena.OtvoriUDP(0)
	if err != nil || upozorenje != nil {
		t.Fatal(err)
	}
	p.udp = razmjena.NoviUDP(conn)
	port := p.udp.Port()
	p.klijent = lazniCvor{}.KlijentSignala("https://cop.primjer.hr", "cop-osijek", d.javi)
	p.puls()
	p.ponovno("promijenile su se adrese ovog računala")
	d.cekaj(t, "promijenile su se adrese ovog računala, pa WEB sync kreće iznova")
	p.teku.Wait()
	if p.udp == nil || p.udp.Port() != port || p.zadnjaProvjera() == nil {
		t.Errorf("nakon promjene: utičnica %v, provjera %v", p.udp, p.zadnjaProvjera())
	}

	staro := obnoviUticnicu
	t.Cleanup(func() { obnoviUticnicu = staro })
	obnoviUticnicu = func(stara *razmjena.UDP) (*razmjena.UDP, error, error) {
		_ = stara.Close()
		return nil, errors.New("UDP port je zauzet"), errors.New("nema mreže")
	}
	d = make(dnevnikPovezivosti, 8)
	p.javi = d.javi
	p.novaUticnica()
	d.cekaj(t, "UDP port je zauzet")
	if r := d.cekaj(t, "nova UDP utičnica se ne da otvoriti"); !strings.Contains(r, "nema mreže") || p.udp != nil {
		t.Errorf("bez nove utičnice: %q, %v", r, p.udp)
	}
}

// Čuvar mreže radi dok traje kontekst, s adresama koje mu da program
func TestCuvajMrezu(t *testing.T) {
	staro := lokalneAdrese
	t.Cleanup(func() { lokalneAdrese = staro })
	lokalneAdrese = func() []string { return []string{"192.168.1.20/24"} }
	p := &povezivost{javi: func(string, ...any) {}}
	ctx, otkazi := context.WithCancel(context.Background())
	p.cuvajMrezu(ctx)
	otkazi()
	p.teku.Wait()
}

// Stanje prijave prelazi na pločicu bez gubitka
func TestSignalZaWeb(t *testing.T) {
	od := time.Unix(1_790_990_000, 0)
	st := razmjena.StanjeSignala{Prijavljen: true, Od: od, Kod: razmjena.KodSignalaKopija, PonovnoU: od.Add(time.Minute), Odbijen: true, OdstupanjeSata: time.Minute}
	got := signalZaWeb(st, "cop.primjer.hr", true)
	want := web.SignalStanje{Posluzitelj: "cop.primjer.hr", Prijavljen: true, Od: od, Kod: razmjena.KodSignalaKopija, PonovnoU: od.Add(time.Minute),
		Odbijen: true, OdstupanjeSata: time.Minute, WebSync: true}
	if got.Posluzitelj != want.Posluzitelj || got.Prijavljen != want.Prijavljen || !got.Od.Equal(want.Od) || got.Kod != want.Kod ||
		!got.PonovnoU.Equal(want.PonovnoU) || got.Odbijen != want.Odbijen || got.OdstupanjeSata != want.OdstupanjeSata || got.WebSync != want.WebSync {
		t.Errorf("stanje za pločicu: %+v", got)
	}
}

// laznaPrijava broji što čuvar mreže traži od prijave
type laznaPrijava struct{ pulsevi, ponovno int }

func (*laznaPrijava) Radi(context.Context)           {}
func (l *laznaPrijava) Puls()                        { l.pulsevi++ }
func (l *laznaPrijava) PonovnoSpoji()                { l.ponovno++ }
func (*laznaPrijava) Stanje() razmjena.StanjeSignala { return razmjena.StanjeSignala{} }

// Utičnica bez rute kreće iznova bez nove prijave: prijava ide TCP-om;
// promjena mreže prijavljuje iznova
func TestPonovnoBezRuteNeDiraPrijavu(t *testing.T) {
	d := make(dnevnikPovezivosti, 8)
	l := &laznaPrijava{}
	p := &povezivost{javi: d.javi, ctx: context.Background(), klijent: l}
	p.puls()
	p.ponovno(peers.RazlogBezRute)
	if l.ponovno != 0 || l.pulsevi != 1 {
		t.Errorf("bez rute: %+v", l)
	}
	p.ponovno(peers.RazlogNovaMreza)
	if l.ponovno != 1 {
		t.Errorf("nova mreža: %+v", l)
	}
}

// Mjerenje koje je teklo na utičnici zamijenjenoj usred mjerenja ne ide u
// vrstu mreže, nego se ponovi na novoj
func TestMjerenjeNaZamijenjenojUticnici(t *testing.T) {
	pusti, upiti := make(chan struct{}), make(chan struct{}, 8)
	d := make(dnevnikPovezivosti, 16)
	p := &povezivost{cfg: config.Povezivost{WebSyncPrijava: true, Stun: []string{lazniSTUNSa(t, pusti, upiti)}}, javi: d.javi, ctx: context.Background()}
	conn, upozorenje, err := razmjena.OtvoriUDP(0)
	if err != nil || upozorenje != nil {
		t.Fatal(err, upozorenje)
	}
	p.udp = razmjena.NoviUDP(conn)
	nakon := make(chan struct{})
	go func() { p.provjeriNakonZamjene(context.Background()); close(nakon) }()
	<-upiti // prvo mjerenje je krenulo na staroj utičnici
	p.novaUticnica()
	close(pusti)
	<-nakon
	if z := p.zadnjaProvjera(); z == nil || z.Greska != "" {
		t.Errorf("nakon zamjene: %+v", z)
	}
	if st := p.mreza.Vrsta; st == razmjena.UDPBlokiran {
		t.Errorf("mjerenje stare utičnice ušlo je u vrstu mreže: %q", st)
	}
	p.teku.Wait()
	_ = p.udp.Close() // kraj testa
}

// Provjera članstava ide svakih razmakProvjereClanstava dok traje kontekst
func TestProvjeravajClanstva(t *testing.T) {
	staro := razmakProvjereClanstava
	t.Cleanup(func() { razmakProvjereClanstava = staro })
	razmakProvjereClanstava = 5 * time.Millisecond
	puta := make(chan struct{}, 8)
	ctx, otkazi := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() {
		provjeravajClanstva(ctx, func() {
			select {
			case puta <- struct{}{}:
			default:
			}
		})
		close(gotovo)
	}()
	<-puta
	<-puta
	otkazi()
	<-gotovo
}

// Prijava odmah nakon svježeg mjerenja ne mjeri još jednom; nakon starog
// mjerenja mjeri iznova, a bez STUN poslužitelja nikad
func TestMjerenjeNakonPrijave(t *testing.T) {
	p := &povezivost{cfg: config.Povezivost{WebSyncPrijava: true, Stun: []string{lazniSTUN(t)}}, javi: func(string, ...any) {}, ctx: context.Background()}
	p.nakonPrijave()
	p.teku.Wait()
	prva := p.zadnjaProvjera()
	if prva == nil {
		t.Fatal("prva prijava bez mjerenja")
	}
	p.nakonPrijave()
	p.teku.Wait()
	if z := p.zadnjaProvjera(); !z.Kad.Equal(prva.Kad) {
		t.Error("svježe mjerenje je ponovljeno")
	}
	p.mu.Lock()
	p.zadnja.Kad = p.zadnja.Kad.Add(-2 * svjezeMjerenje)
	p.mu.Unlock()
	p.nakonPrijave()
	p.teku.Wait()
	if z := p.zadnjaProvjera(); !z.Kad.After(prva.Kad.Add(-2 * svjezeMjerenje)) {
		t.Error("staro mjerenje nije ponovljeno")
	}
	bez := &povezivost{javi: func(string, ...any) {}, ctx: context.Background()}
	bez.nakonPrijave()
	if bez.zadnjaProvjera() != nil {
		t.Error("mjerenje bez STUN poslužitelja")
	}
}
