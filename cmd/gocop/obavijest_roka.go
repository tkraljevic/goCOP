package main

// E-pošta o roku članstva i ovlasti i o kopiji ključa mreže (G9b; Odluke
// (23. 10. 2026.)): računalo jednom na sat pogleda obavijesti i svakom
// globalnom administratoru pošalje one koje još nije dobio, ni s drugog
// računala (zapis je u knjizi). Računalo kojem to nije prvi posao čeka
// (peers.CekanjeObavijesti), da poruka ne ode dvaput. Računalo bez računa
// za slanje PIN-a ne šalje ništa, a obavijesti ga čekaju dok se račun ne
// upiše.

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"gocop/internal/peers"
	"gocop/internal/service"
)

// razmakObavijestiRoka je koliko se često gledaju rokovi, a
// prvaObavijestRoka koliko nakon pokretanja prvi put (prva razmjena prije)
const (
	razmakObavijestiRoka = time.Hour
	prvaObavijestRoka    = 2 * time.Minute
)

// posiljateljObavijesti šalje obavijest globalnim administratorima
// (service.DrugiKorak)
type posiljateljObavijesti interface {
	PrimateljiObavijesti(ctx context.Context) ([]service.PrimateljObavijesti, error)
	PosaljiObavijest(ctx context.Context, p service.PrimateljObavijesti, predmet, tekst string) error
}

// pokreniObavijesti pokrene slanje obavijesti u pozadini: nova kopija
// ključa mreže potakne slanje odmah. Vraća zaustavljanje, koje slanje
// otkaže i pričeka, da se baza ne zatvori prije nego što je poslana poruka
// zapisana.
func pokreniObavijesti(ctx context.Context, p *peers.Service, dk posiljateljObavijesti) func() {
	ctx, otkazi := context.WithCancel(ctx)
	potakni := make(chan struct{}, 1)
	p.KadNovaObavijest(func() {
		select {
		case potakni <- struct{}{}:
		default: // poticaj već čeka
		}
	})
	var teku sync.WaitGroup
	teku.Go(func() { obavijestiORoku(ctx, p, dk, prvaObavijestRoka, razmakObavijestiRoka, potakni) })
	return func() {
		otkazi()
		teku.Wait()
	}
}

// obavjestavac šalje obavijesti o roku i o kopiji ključa mreže;
// bezPrimatelja pamti da je u dnevniku već zapisano da nitko nema adresu za
// obavijest, a prvi kad je ovo računalo prvi put vidjelo koju obavijest
// (za čekanje dok je ne pošalje računalo kojem je to posao)
type obavjestavac struct {
	p             *peers.Service
	dk            posiljateljObavijesti
	bezPrimatelja bool
	prvi          map[string]time.Time
	sad           func() time.Time
}

func noviObavjestavac(p *peers.Service, dk posiljateljObavijesti) *obavjestavac {
	return &obavjestavac{p: p, dk: dk, prvi: map[string]time.Time{}, sad: time.Now}
}

// obavijestiORoku gleda obavijesti dok ctx traje: prvi put nakon prva, pa
// svaki razmak, i odmah kad stigne poticaj (nova kopija ključa mreže)
func obavijestiORoku(ctx context.Context, p *peers.Service, dk posiljateljObavijesti, prva, razmak time.Duration, potakni <-chan struct{}) {
	o := noviObavjestavac(p, dk)
	t := time.NewTimer(prva)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-potakni:
			o.posalji(ctx)
		case <-t.C:
			o.posalji(ctx)
			t.Reset(razmak)
		}
	}
}

// posalji pošalje svakom primatelju obavijesti koje još nije dobio, a za
// koje je prošlo čekanje ovog računala
func (o *obavjestavac) posalji(ctx context.Context) {
	zrele := o.zrele(ctx, append(o.p.ObavijestiRoka(ctx), o.p.ObavijestiKljuca(ctx)...))
	if len(zrele) == 0 {
		return
	}
	for _, pr := range o.primatelji(ctx) {
		if ctx.Err() != nil || !o.posaljiJednom(ctx, pr, zrele) {
			return
		}
	}
}

// zrele su obavijesti kojima je prošlo čekanje ovog računala od prvog
// viđenja (peers.CekanjeObavijesti)
func (o *obavjestavac) zrele(ctx context.Context, obavijesti []peers.ObavijestRoka) []peers.ObavijestRoka {
	sada := o.sad()
	var out []peers.ObavijestRoka
	for _, ob := range obavijesti {
		prvi, vidjena := o.prvi[ob.Kljuc]
		if !vidjena {
			prvi, o.prvi[ob.Kljuc] = sada, sada
		}
		if sada.Sub(prvi) >= o.p.CekanjeObavijesti(ctx, ob) {
			out = append(out, ob)
		}
	}
	return out
}

// primatelji su globalni administratori s adresom za obavijest; kad ih
// nema, to se jednom zapiše u dnevnik
func (o *obavjestavac) primatelji(ctx context.Context) []service.PrimateljObavijesti {
	primatelji, err := o.dk.PrimateljiObavijesti(ctx)
	if err != nil {
		log.Printf("razmjena: primatelji obavijesti administratorima se ne daju pročitati: %v", err)
		return nil
	}
	if len(primatelji) == 0 && !o.bezPrimatelja {
		log.Printf("razmjena: obavijesti administratorima nemaju kome ići: nijedan globalni administrator nema adresu na koju smije ići PIN")
	}
	o.bezPrimatelja = len(primatelji) == 0
	return primatelji
}

// posaljiJednom pošalje primatelju obavijesti koje još nije dobio, jednu
// poruku po skupini (rokovi, ključ mreže), i zapamti ih, i kad se program
// upravo gasi; vraća false kad slanje s ovog računala ne ide (nema računa
// za slanje), pa ostali ne treba ni pokušati
func (o *obavjestavac) posaljiJednom(ctx context.Context, pr service.PrimateljObavijesti, obavijesti []peers.ObavijestRoka) bool {
	for _, skupina := range peers.Skupine(o.p.NoveObavijestiRoka(ctx, pr.ID, obavijesti)) {
		predmet, tekst := peers.TekstObavijesti(skupina, o.p.Node().Name)
		err := o.dk.PosaljiObavijest(ctx, pr, predmet, tekst)
		switch {
		case tihaGreskaSlanja(err):
			return false
		case err != nil:
			log.Printf("razmjena: obavijest „%s” nije poslana na %s: %v", predmet, service.MaskirajAdresu(pr.Adresa), err)
		default:
			o.p.ZabiljeziObavijestiRoka(context.WithoutCancel(ctx), pr.ID, skupina)
		}
	}
	return true
}

// tihaGreskaSlanja: na računalu bez računa za slanje, ili s lozinkom koju je
// poslužitelj odbio (to već piše u dnevniku), obavijest samo čeka
func tihaGreskaSlanja(err error) bool {
	return errors.Is(err, service.ErrNemaPosiljatelja) || errors.Is(err, service.ErrNemaKljucaPrijave) || errors.Is(err, service.ErrPosiljateljNeispravan)
}
