package main

// Tjedna priprema lanca na računalu koje izdaje prognozu: svake subote u
// 2:00 (i do 6:00, ako je računalo u 2:00 bilo ugašeno) lanac se pripremi
// kao „Pripremi model”, ali u privremenu bazu. Novi i stari lanac mjere se
// na zadnjih 12 tjedana arhive; novi se primijeni samo kad nije lošiji za
// više od 2 %. Dok traje obrana od poplava, priprema se preskače. Ishod
// ostaje u bazi prognoza (prognoza.TjednePripreme) i piše se u Postavkama
// prognoze.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// Razdoblje mjerenja tjedne pripreme: zadnjih 12 tjedana, do četiri dana
// prije pripreme, da svaka izdana prognoza ima s čim se usporediti.
const (
	tjedanaMjerenja = 12
	krajMjerenja    = 96 * time.Hour
	korakMjerenja   = 6 // sati između dvaju izdanja u mjerenju
	// zakasnjelaTjedna je koliko nakon 2:00 priprema još smije krenuti kad je
	// računalo u 2:00 bilo ugašeno.
	zakasnjelaTjedna = 4 * time.Hour
)

// izmjeriLanac mjeri lanac iz baze prognoza na arhivi od–do; testovi ga
// zamjenjuju.
var izmjeriLanac = izmjeriULanac

// izmjeriULanac mjeri lanac iz baze prognoza na arhivi od–do i vraća mjere
// po letvi i dosegu. Mjeri u privremenoj kopiji modela, jer provjera unatrag
// zapisuje promašaje.
func izmjeriULanac(arhiva, baza *sql.DB, od, do time.Time) (prognoza.MjereProvjere, error) {
	kopija, pocisti, err := privremenaBazaPrognoza()
	if err != nil {
		return nil, err
	}
	defer pocisti()
	if err := prepisiModel(baza, kopija); err != nil {
		return nil, err
	}
	o := prognoza.ZadaneOpcijeProvjere()
	o.Od, o.Do, o.Korak = od, do, korakMjerenja
	o.Zapisi, o.Mjera = true, prognoza.MjereProvjere{}
	_, err = provjeriUnatrag(arhiva, kopija, o)
	return o.Mjera, err
}

// tjednaPriprema drži ono o čemu ovisi tjedna priprema.
type tjednaPriprema struct {
	pb, arhiva *sql.DB
	izdaje     func() bool
	// obrana javlja traje li obrana od poplava i po kojem aktu (oznake)
	obrana func(context.Context) (string, bool)
	// izdaj izda prognozu novim modelom i vrati rečenicu o tome
	izdaj func(ctx context.Context, p *poslovi.Posao) (string, error)
	sat   func() time.Time

	radi sync.Mutex
}

// posluziteljTjedne je ono što tjedna priprema traži od poslužitelja.
type posluziteljTjedne interface {
	SetTjednaPriprema(func() web.StanjeTjedne)
}

// tjednaCvora pokrene tjednu pripremu lanca na čvoru i da stranicama njezino
// stanje. uloge su uloge čvora, utijeku javlja traje li satni krug, a
// objavi izdanje šalje razmjenom.
func tjednaCvora(ctx context.Context, server posluziteljTjedne, uloge func() peers.Uloge, pb, arhiva, registar *sql.DB,
	obrana func(context.Context) (string, bool), utijeku func() bool, osvjezivac *prognoza.Osvjezivac,
	objavi func(context.Context, *prognoza.Ishod)) *tjednaPriprema {
	izdaje := func() bool { return uloge().Izdaje }
	t := &tjednaPriprema{pb: pb, arhiva: arhiva, izdaje: izdaje, obrana: obrana, sat: time.Now,
		izdaj: func(ctx context.Context, p *poslovi.Posao) (string, error) {
			return izdajNovimModelom(ctx, p, utijeku(), osvjezivac, objavi)
		}}
	if t.obrana == nil {
		t.obrana = func(context.Context) (string, bool) { return "", false }
	}
	server.SetTjednaPriprema(stanjeTjedne(pb, registar, izdaje))
	go t.vrti(ctx)
	return t
}

// vrti provjerava svake minute je li priprema na redu, dok se ctx ne
// otkaže.
func (t *tjednaPriprema) vrti(ctx context.Context) {
	tik := time.NewTicker(time.Minute)
	defer tik.Stop()
	for {
		t.provjeri(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tik.C:
		}
	}
}

// provjeri pokrene pripremu kad je na redu: računalo izdaje prognozu, sad
// je subota između 2:00 i 6:00, a za taj termin pripreme još nema.
func (t *tjednaPriprema) provjeri(ctx context.Context) {
	sada := t.sat()
	termin := prognoza.TerminTjedne(sada, models.Zagreb)
	if sada.Sub(termin) >= zakasnjelaTjedna || !t.izdaje() || !t.radi.TryLock() {
		return
	}
	defer t.radi.Unlock()
	zadnje, err := prognoza.TjednePripreme(t.pb, 1)
	if err != nil {
		log.Printf("tjedna priprema: %v", err)
		return
	}
	if len(zadnje) > 0 && !zadnje[0].Kad.Before(termin) {
		return // za ovaj termin već je zapisana
	}
	ishod := t.pripremi(ctx, termin)
	if err := prognoza.UpisiTjednu(t.pb, ishod); err != nil {
		log.Printf("tjedna priprema, zapis: %v", err)
	}
	log.Printf("tjedna priprema %s: %s", termin.Format("2.1.2006."), opisTjedne(ishod))
}

// pripremi je jedna tjedna priprema za termin.
func (t *tjednaPriprema) pripremi(ctx context.Context, termin time.Time) prognoza.TjednaPriprema {
	out := prognoza.TjednaPriprema{Kad: termin, LanacOd: prognoza.NamjestenOd(t.pb)}
	if akt, traje := t.obrana(ctx); traje {
		out.Ishod, out.Razlog = prognoza.TjednaPreskocena, razlogObrane(akt)
		return out
	}
	if err := t.probajIPrimijeni(ctx, &out); err != nil {
		out.Ishod, out.Razlog = prognoza.TjednaGreska, err.Error()
	}
	return out
}

// probajIPrimijeni namjesti novi lanac u privremenu bazu, izmjeri ga uz
// sadašnji i primijeni ga ako nije lošiji; ishod upiše u out.
func (t *tjednaPriprema) probajIPrimijeni(ctx context.Context, out *prognoza.TjednaPriprema) error {
	p := &poslovi.Posao{Naziv: "tjedna priprema modela"}
	radna, pocisti, err := privremenaBazaPrognoza()
	if err != nil {
		return err
	}
	defer pocisti()
	if _, _, err := namjestiUBazu(t.arhiva, t.pb, radna, p); err != nil {
		return err
	}
	u, err := t.usporedi(radna, out.Kad)
	if err != nil {
		return err
	}
	out.Prije, out.Poslije, out.Losije = u.Prije, u.Poslije, u.Losije
	if out.Osnovica, err = t.osnovica(out.Prije); err != nil {
		return err
	}
	slabi := prognoza.LanacSlabi(out.Prije, out.Osnovica)
	if !u.NijeLosiji {
		out.Ishod, out.Slabi = prognoza.TjednaNijePrimijenjena, slabi
		return nil
	}
	return t.primijeni(ctx, radna, p, out, slabi)
}

// usporedi izmjeri sadašnji i novi lanac na zadnjih tjedanaMjerenja tjedana
// do krajMjerenja prije termina.
func (t *tjednaPriprema) usporedi(radna *sql.DB, termin time.Time) (prognoza.Usporedba, error) {
	do := termin.Add(-krajMjerenja)
	od := do.AddDate(0, 0, -7*tjedanaMjerenja)
	stara, err := izmjeriLanac(t.arhiva, t.pb, od, do)
	if err != nil {
		return prognoza.Usporedba{}, err
	}
	nova, err := izmjeriLanac(t.arhiva, radna, od, do)
	return prognoza.UsporediLance(stara, nova), err
}

// primijeni prepiše novi lanac u bazu prognoza, zapamti mu osnovicu i izda
// prognozu njime. Novi lanac koji ne vraća promašaj blizu onoga pri primjeni
// ne rješava slabljenje: osnovica ostaje stara, a upozorenje stoji.
func (t *tjednaPriprema) primijeni(ctx context.Context, radna *sql.DB, p *poslovi.Posao, out *prognoza.TjednaPriprema, slabi bool) error {
	if err := prepisiModel(radna, t.pb); err != nil {
		return err
	}
	zaboraviDnevneModele()
	out.Ishod, out.LanacOd = prognoza.TjednaPrimijenjena, prognoza.NamjestenOd(t.pb)
	out.Slabi = slabi && prognoza.LanacSlabi(out.Poslije, out.Osnovica)
	osnovica := out.Poslije
	if out.Slabi {
		osnovica = out.Osnovica
	}
	_, otisak, err := prognoza.SastaviModel(t.pb)
	if err == nil {
		err = prognoza.UpisiOsnovicu(t.pb, prognoza.Osnovica{Otisak: otisak, Promasaj: osnovica})
	}
	if err != nil {
		log.Printf("tjedna priprema, osnovica: %v", err)
	}
	poruka, err := t.izdaj(ctx, p)
	log.Printf("tjedna priprema: %s%v", poruka, greskaIliNista(err))
	return nil
}

// greskaIliNista je greška za dnevnik ili ništa.
func greskaIliNista(err error) any {
	if err == nil {
		return ""
	}
	return err
}

// osnovica je promašaj lanca koji vrijedi kad je primijenjen. Lanac
// primijenjen rukom ili primljen razmjenom (drugi otisak) osnovicu dobiva
// sada, pa se slabljenje mjeri od ovog tjedna.
func (t *tjednaPriprema) osnovica(sada float64) (float64, error) {
	_, otisak, err := prognoza.SastaviModel(t.pb)
	if err != nil {
		return 0, err
	}
	o, ima, err := prognoza.ProcitajOsnovicu(t.pb)
	if err != nil {
		return 0, err
	}
	if ima && o.Otisak == otisak && o.Promasaj > 0 {
		return o.Promasaj, nil
	}
	return sada, prognoza.UpisiOsnovicu(t.pb, prognoza.Osnovica{Otisak: otisak, Promasaj: sada})
}

// razlogObrane je razlog preskočene pripreme.
func razlogObrane(akt string) string {
	if akt == "" {
		return "traje obrana od poplava"
	}
	return "traje obrana od poplava (" + akt + ")"
}

// opisTjedne je ishod u jednom retku, za dnevnik programa.
func opisTjedne(p prognoza.TjednaPriprema) string {
	switch p.Ishod {
	case prognoza.TjednaPreskocena, prognoza.TjednaGreska:
		return p.Ishod + ": " + p.Razlog
	}
	s := fmt.Sprintf("%s (%.1f → %.1f cm)", p.Ishod, p.Prije, p.Poslije)
	if p.Slabi {
		s += ", stari lanac slabi"
	}
	return s
}

// obranaUAktima javlja traje li obrana od poplava na ovom računalu (isto
// pravilo kao zid događanja) i oznake akata po kojima traje.
func obranaUAktima(traje func(context.Context) bool, stanja func(context.Context, string, time.Time) (map[string]models.StanjeObrane, error),
	akt func(context.Context, string) (*models.Akt, error)) func(context.Context) (string, bool) {
	return func(ctx context.Context) (string, bool) {
		if !traje(ctx) {
			return "", false
		}
		st, err := stanja(ctx, "", time.Now())
		if err != nil {
			return "", true
		}
		var oznake []string
		vidjeno := map[string]bool{}
		for _, s := range st {
			for _, a := range s.Aktivni {
				if a.AktID == "" || vidjeno[a.AktID] {
					continue
				}
				vidjeno[a.AktID] = true
				if x, err := akt(ctx, a.AktID); err == nil && x != nil {
					oznake = append(oznake, x.Oznaka())
				}
			}
		}
		sort.Strings(oznake)
		return strings.Join(oznake, ", "), true
	}
}

// stanjeTjedne daje stranicama zadnje tjedne pripreme iz baze prognoza i
// nazive letvi iz registra.
func stanjeTjedne(pb, registar *sql.DB, izdaje func() bool) func() web.StanjeTjedne {
	return func() web.StanjeTjedne {
		s := web.StanjeTjedne{Izdaje: izdaje(), Sljedeca: prognoza.SljedecaTjedna(time.Now(), models.Zagreb), Nazivi: map[string]string{}}
		var err error
		if s.Pripreme, err = prognoza.TjednePripreme(pb, 10); err != nil {
			log.Printf("tjedna priprema: %v", err)
		}
		r, err := registar.Query(`SELECT code, name FROM stations`)
		if err != nil {
			return s
		}
		defer r.Close()
		for r.Next() {
			var sifra, naziv string
			if r.Scan(&sifra, &naziv) == nil {
				s.Nazivi[sifra] = naziv
			}
		}
		return s
	}
}
