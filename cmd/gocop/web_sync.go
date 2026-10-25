package main

// WEB sync u pokretanju (plan WEB synca, F3b): jezgra iz internal/razmjena
// na UDP utičnici programa, predana servisu (peers.PokreniWebSync). Radi
// uz web_sync_prijava i sastajalište: na stalnom čvoru s
// posluzi_sastajaliste odmah, s vlastitim ključem i slanjem kroz vlastito
// sastajalište, a na ostalima kad se zna ključ sastajališta iz članstva
// (pri pokretanju ili nakon prijave), sa slanjem kroz prijavu. Uz web_sync
// = true WEB sync radi, a uz web_sync = false na pozive odgovara
// „isključeno” (Odluke (25. 10. 2026.), 1).
//
// Nova UDP utičnica (promjena mreže) zaustavi WEB sync stare, pa se sesije
// zatvore, i pokrene ga na novoj, što obriše pauze. Poruke sastajališta
// idu WEB syncu koji upravo radi. Prije svake pripreme u pozadini mreža se
// mjeri iznova, kad je zadnje mjerenje starije od 20 s.

import (
	"context"
	"crypto/ed25519"
	"time"

	"gocop/internal/razmjena"
)

// starostMjerenja: starije mjerenje mreže prije poziva se ponavlja (plan
// WEB synca, F1: „prije poziva, kad je zadnji rezultat stariji od 20 s”)
const starostMjerenja = 20 * time.Second

// cvorWeba je čvor koji drži WEB sync (peers.Service)
type cvorWeba interface {
	PostavkeWebSynca(ctx context.Context) razmjena.PostavkeWeb
	PokreniWebSync(ctx context.Context, w *razmjena.WebSync, ukljucen bool, prijePoziva func(context.Context)) func()
}

// webPovezivosti je WEB sync koji upravo radi i utičnica na kojoj radi
type webPovezivosti struct {
	w     *razmjena.WebSync
	u     *razmjena.UDP
	stani func()
}

// kljucCvora je ključ čvora za utičnicu (ključ za resetiranje QUIC veza);
// nil kad WEB synca nema
func (p *povezivost) kljucCvora() ed25519.PrivateKey {
	if p.webCvor == nil {
		return nil
	}
	return p.webCvor.PostavkeWebSynca(p.ctx).Priv
}

// pokreniWeb pokrene WEB sync na trenutnoj utičnici, kad ga još nema, a
// zna se kamo poruke idu; inače ne radi ništa
func (p *povezivost) pokreniWeb() {
	p.webMu.Lock()
	defer p.webMu.Unlock()
	p.pokreniWebPodBravom()
}

// pokreniWebPodBravom je pokreniWeb pod webMu
func (p *povezivost) pokreniWebPodBravom() {
	p.mu.Lock()
	u, radi := p.udp, p.web
	p.mu.Unlock()
	if p.webCvor == nil || u == nil || radi != nil && radi.u == u {
		return
	}
	kljuc, posalji, ok := p.sastajalisteWeba()
	if !ok {
		return
	}
	postavke := p.webCvor.PostavkeWebSynca(p.ctx)
	postavke.Posalji, postavke.Sastajaliste, postavke.Mreza = posalji, kljuc, p.mrezaZaPoziv
	w, err := razmjena.NoviWebSync(u, postavke)
	if err != nil {
		p.javi("povezivost: WEB sync ne radi: %v", err)
		return
	}
	stani := p.webCvor.PokreniWebSync(p.ctx, w, p.cfg.WebSync, p.osvjeziMrezu)
	p.mu.Lock()
	p.web = &webPovezivosti{w: w, u: u, stani: stani}
	p.mu.Unlock()
}

// zaustaviWeb zaustavi WEB sync koji radi, pa se njegove sesije zatvore
func (p *povezivost) zaustaviWeb() {
	p.webMu.Lock()
	defer p.webMu.Unlock()
	p.zaustaviWebPodBravom()
}

// zaustaviWebPodBravom je zaustaviWeb pod webMu
func (p *povezivost) zaustaviWebPodBravom() {
	p.mu.Lock()
	radi := p.web
	p.web = nil
	p.mu.Unlock()
	if radi != nil {
		radi.stani()
	}
}

// sastajalisteWeba je ključ sastajališta i slanje poruka do njega: na
// stalnom čvoru vlastiti ključ i vlastito sastajalište, inače ključ iz
// članstva i prijava; ok je false dok se ključ ne zna
func (p *povezivost) sastajalisteWeba() (ed25519.PublicKey, func(razmjena.PotpisanaPoruka) error, bool) {
	if p.sastajaliste != nil {
		return p.kljucCvora().Public().(ed25519.PublicKey), p.sastajaliste.Posalji, true
	}
	if p.signal == nil {
		return nil, nil, false
	}
	kljuc, err := p.signal.Kljuc(p.ctx)
	return kljuc, p.signal.Posalji, err == nil
}

// webRadi je WEB sync koji upravo radi; nil kad ga nema
func (p *povezivost) webRadi() *razmjena.WebSync {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.web == nil {
		return nil
	}
	return p.web.w
}

// naPoruku preda poruku sastajališta WEB syncu koji upravo radi
func (p *povezivost) naPoruku(pp razmjena.PotpisanaPoruka) {
	if w := p.webRadi(); w != nil {
		w.NaPoruku(pp)
	}
}

// naNemu: sastajalište nije proslijedilo poruku sesije
func (p *povezivost) naNemu(sesija, razlog string) {
	if w := p.webRadi(); w != nil {
		w.NaNemu(sesija, razlog)
	}
}

// mrezaZaPoziv su vlastiti kandidati iz zadnjeg mjerenja i vrsta mreže
// (histereza) za poziv i odziv
func (p *povezivost) mrezaZaPoziv() ([]razmjena.KandidatPoruke, *razmjena.NATPoruke) {
	p.mu.Lock()
	vrsta := p.mreza.Vrsta
	p.mu.Unlock()
	nat := &razmjena.NATPoruke{Vrsta: vrsta, UDPBlokiran: vrsta == razmjena.UDPBlokiran}
	z := p.zadnjaProvjera()
	if z == nil {
		return nil, nat
	}
	out := make([]razmjena.KandidatPoruke, 0, len(z.Kandidati))
	for _, k := range z.Kandidati {
		out = append(out, razmjena.KandidatPoruke{Vrsta: k.Vrsta, Adresa: k.Adresa.String()})
	}
	return out, nat
}

// osvjeziMrezu prije pripreme ponovi mjerenje mreže kad je zadnje starije
// od 20 s
func (p *povezivost) osvjeziMrezu(ctx context.Context) {
	if z := p.zadnjaProvjera(); z == nil || time.Since(z.Kad) > starostMjerenja {
		p.provjeriVezu(ctx)
	}
}
