package main

// Sastajalište u pokretanju (plan WEB synca, F2b). Uz web_sync_prijava
// stalni čvor s posluzi_sastajaliste poslužuje sastajalište na
// /razmjena/signal, a ostala računala se prijavljuju kod sastajališta iz
// postavki (sastajaliste, sastajaliste_cvor). Prijava kreće kad web sluša i
// UDP utičnica radi. Čuvar mreže (peers.CuvarMreze) nakon kratkog spavanja
// pošalje puls, a nakon dugog spavanja i promjene mreže prijavi se iznova,
// otvori novu UDP utičnicu i, kad je ruta tu, ponovno izmjeri mrežu.

import (
	"context"
	"net/url"
	"strings"
	"time"

	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/web"
)

// cvorSignala gradi prijavu i sastajalište iz čvora (peers.Service)
type cvorSignala interface {
	KlijentSignala(adresa, cvor string, javi func(string, ...any)) *razmjena.KlijentSignala
	Sastajaliste(javi func(string, ...any)) (*razmjena.Sastajaliste, error)
}

// prijavaSignala je prijava kod sastajališta (razmjena.KlijentSignala)
type prijavaSignala interface {
	Radi(ctx context.Context)
	Puls()
	PonovnoSpoji()
	Stanje() razmjena.StanjeSignala
}

// razmakProvjereClanstava: sastajalište svakih toliko ponovno provjeri
// članstva prijavljenih, jer članstvo i ovlast istječu i dok veza traje;
// var, da ga test skrati
var razmakProvjereClanstava = time.Hour

// pokreniSignal pripremi sastajalište ili prijavu i preda webu njihovo
// stanje; zove se prije nego web sluša
func (p *povezivost) pokreniSignal(cvor cvorSignala, w posluziteljPovezivosti) {
	switch {
	case p.cfg.PosluziSastajaliste:
		p.posluziSastajaliste(cvor, w)
	case strings.TrimSpace(p.cfg.Sastajaliste) == "" || strings.TrimSpace(p.cfg.SastajalisteCvor) == "":
		w.SetSignalStanje(func() web.SignalStanje { return web.SignalStanje{Kod: razmjena.KodSignalaBezPostavke} })
	default:
		k := cvor.KlijentSignala(p.cfg.Sastajaliste, p.cfg.SastajalisteCvor, p.javi)
		k.NaPrijavu, k.NaPoruku, k.NaNemu = p.nakonPrijave, p.naPoruku, p.naNemu
		p.klijent, p.signal = k, k
		posluzitelj := domenaSastajalista(p.cfg.Sastajaliste)
		w.SetSignalStanje(func() web.SignalStanje { return signalZaWeb(p.klijent.Stanje(), posluzitelj, p.cfg.WebSync) })
	}
}

// posluziSastajaliste preda webu sastajalište i njegove brojke
func (p *povezivost) posluziSastajaliste(cvor cvorSignala, w posluziteljPovezivosti) {
	r, err := cvor.Sastajaliste(p.javi)
	if err != nil {
		p.javi("povezivost: sastajalište ne radi: %v", err)
		return
	}
	r.NaPoruku = p.naPoruku // poruke kojima je cilj ovo računalo idu WEB syncu
	p.sastajaliste = r
	p.teku.Go(func() { provjeravajClanstva(p.ctx, r.ProvjeriSve) })
	w.SetSignal(r.Handler())
	w.SetSignalStanje(func() web.SignalStanje {
		st := r.Stanje()
		return web.SignalStanje{Sastajaliste: true, Prijavljenih: st.Prijavljenih, PozivaSat: st.PozivaSat, Prekida24h: st.Prekida24h,
			Kopije: st.Kopije, WebSync: p.cfg.WebSync}
	})
	p.javi("povezivost: ovo računalo je sastajalište za WEB sync")
}

// provjeravajClanstva svakih razmakProvjereClanstava ponovno provjeri
// članstva prijavljenih
func provjeravajClanstva(ctx context.Context, provjeri func()) {
	t := time.NewTicker(razmakProvjereClanstava)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			provjeri()
		}
	}
}

// signalZaWeb je stanje prijave za pločicu
func signalZaWeb(st razmjena.StanjeSignala, posluzitelj string, webSync bool) web.SignalStanje {
	return web.SignalStanje{Posluzitelj: posluzitelj, Prijavljen: st.Prijavljen, Od: st.Od, Kod: st.Kod, PonovnoU: st.PonovnoU,
		Odbijen: st.Odbijen, OdstupanjeSata: st.OdstupanjeSata, WebSync: webSync}
}

// domenaSastajalista je ime poslužitelja iz adrese sastajališta, za tekst
// na pločici (IP adresu pločica sama ne pokazuje)
func domenaSastajalista(adresa string) string {
	u, err := url.Parse(strings.TrimSpace(adresa))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// nakonPrijave: pri svakom spajanju na sastajalište mreža se izmjeri
// iznova (plan WEB synca, §2.2), u pozadini, da prijava ne čeka; tako se vidi
// i kad se ruta vratila uz iste adrese. WEB sync kreće ovdje kad pri
// pokretanju ključ sastajališta još nije bio poznat (tek upareno računalo).
func (p *povezivost) nakonPrijave() {
	p.pokreniWeb()
	if z := p.zadnjaProvjera(); len(p.cfg.Stun) == 0 || z != nil && time.Since(z.Kad) < svjezeMjerenje {
		return // prva prijava odmah nakon mjerenja pri pokretanju ne mjeri još jednom
	}
	p.zapocniMjerenje()
}

// svjezeMjerenje: mjerenje mlađe od toga ne ponavlja se pri prijavi
const svjezeMjerenje = time.Minute

// prijaviSe pokrene prijavu kod sastajališta, kad je ima
func (p *povezivost) prijaviSe(ctx context.Context) {
	if p.klijent != nil {
		p.teku.Go(func() { p.klijent.Radi(ctx) })
	}
}

// zatvoriSastajaliste prekine veze prijavljenih računala pri gašenju
func (p *povezivost) zatvoriSastajaliste() {
	if p.sastajaliste != nil {
		p.sastajaliste.Zatvori()
	}
}

// cuvajMrezu pokrene čuvara mreže
func (p *povezivost) cuvajMrezu(ctx context.Context) {
	c := &peers.CuvarMreze{Sat: time.Now, Adrese: lokalneAdrese, BezRute: p.bezRute, Puls: p.puls, Ponovno: p.ponovno}
	p.teku.Go(func() { c.Radi(ctx) })
}

// lokalneAdrese su adrese sučelja, a obnoviUticnicu otvara novu UDP
// utičnicu; var, da ih test podmetne
var (
	lokalneAdrese  = razmjena.LokalneAdrese
	obnoviUticnicu = razmjena.ObnoviUDP
)

func (p *povezivost) bezRute() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.udp != nil && p.udp.BezRute()
}

// puls nakon kratkog spavanja
func (p *povezivost) puls() {
	if p.klijent != nil {
		p.klijent.Puls()
	}
}

// ponovno nakon dugog spavanja ili promjene mreže: prijava iznova, nova
// utičnica i, kad je ruta tu, nova provjera mreže. Utičnica bez rute ne
// dira prijavu: ona ide TCP-om i može raditi i kad UDP ne prolazi.
func (p *povezivost) ponovno(razlog string) {
	p.javi("povezivost: %s, pa WEB sync kreće iznova", razlog)
	if p.klijent != nil && razlog != peers.RazlogBezRute {
		p.klijent.PonovnoSpoji()
	}
	p.novaUticnica()
	if len(p.cfg.Stun) > 0 {
		p.teku.Go(func() {
			_ = razmjena.CekajRutu(p.ctx, p.cfg.Stun[0]) // bez rute provjera kaže što vidi
			p.provjeriNakonZamjene(p.ctx)
		})
	}
}

// greskaZamjene je ishod mjerenja koje je teklo na utičnici zamijenjenoj
// usred mjerenja (naStalnoj)
const greskaZamjene = "Mreža se promijenila tijekom provjere, pa se provjera ponavlja."

// provjeriNakonZamjene izmjeri mrežu na novoj utičnici: mjerenje koje je u
// tijeku možda teče na staroj, pa se nakon njega mjeri još jednom
func (p *povezivost) provjeriNakonZamjene(ctx context.Context) {
	if r := p.provjeriVezu(ctx); r.Greska == greskaZamjene {
		p.provjeriVezu(ctx)
	}
}

// novaUticnica zamijeni UDP utičnicu novom na istom portu; vrsta mreže
// kreće ispočetka, jer je mreža nova. WEB sync stare utičnice staje (sesije
// se zatvaraju), a na novoj kreće iznova, bez pauza (web_sync.go).
func (p *povezivost) novaUticnica() {
	p.mu.Lock()
	stara := p.udp
	p.mu.Unlock()
	if stara == nil {
		return
	}
	p.zaustaviWeb()
	defer p.pokreniWeb()
	nova, upozorenje, err := obnoviUticnicu(stara)
	if upozorenje != nil {
		p.javi("povezivost: %v", upozorenje)
	}
	if err != nil {
		p.javi("povezivost: nova UDP utičnica se ne da otvoriti, pa WEB sync ne radi do ponovnog pokretanja: %v", err)
	}
	p.mu.Lock()
	p.udp, p.mreza = nova, razmjena.StanjeMreze{}
	p.mu.Unlock()
}
