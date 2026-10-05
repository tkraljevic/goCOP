package main

import (
	"context"
	"log"
	"time"

	"gocop/internal/service"
)

// unatragPriPokretanju: prvi prolaz gleda dan unatrag, za akte koji su
// stupili na snagu dok je čvor bio ugašen
const unatragPriPokretanju = 24 * time.Hour

// najviseZaostaje: koliko se najdulje ponavlja razdoblje s upozorenjem akata,
// mjereno od prvog takvog prolaza (ne od granice, koja pri pokretanju već
// zaostaje unatragPriPokretanju); trajna greška (npr. akt s dionicom koje
// nema u registru) inače bi svaki prolaz iznova izvodio sve akte od zadnjeg
// čistog prolaza
const najviseZaostaje = 24 * time.Hour

// granicaAkata je stanje kruga akata na snazi: zadnji je granica idućeg
// prolaza, a greskaOd trenutak prvog prolaza s upozorenjem akata od zadnjeg
// čistog prolaza (nula kad ga nema)
type granicaAkata struct {
	zadnji   time.Time
	greskaOd time.Time
}

// pratiAkteNaSnazi izvodi povijest obrane za akte koji su stupili na snagu
// od zadnjeg čistog prolaza, odmah i zatim u krugu (svakih deset minuta):
// akt s kasnijim početkom tako ulazi u epizode i bez nove ovjere ili storna
// u sektoru. Greška se zapiše u dnevnik i krug ide dalje, a propušteno
// razdoblje obuhvati idući prolaz (prolazAkataNaSnazi). Staje kad stane čvor.
func pratiAkteNaSnazi(ctx context.Context, akti *service.AktService, svaki time.Duration) {
	t := time.NewTicker(svaki)
	defer t.Stop()
	g := granicaAkata{zadnji: time.Now().Add(-unatragPriPokretanju)}
	for {
		g = prolazAkataNaSnazi(ctx, akti, g, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// prolazAkataNaSnazi izvodi povijest za akte koji su stupili na snagu u
// razdoblju (g.zadnji, sad] i vraća stanje idućeg prolaza: granicu sad kad je
// prolaz čist. Prolaz s upozorenjem akata (npr. prolazni SQLITE_BUSY) granicu
// ne pomiče, pa idući obuhvati i ovo razdoblje; izvođenje je idempotentno, pa
// ponovljeni akti ne dodaju verzije. Ponavljanje ipak ne traje dulje od
// najviseZaostaje od prvog prolaza s upozorenjem: tada se granica pomiče i
// bez čistog prolaza, uz jedno upozorenje da su neusklađeni akti preskočeni.
// Upozorenja privremenih imenovanja nisu vezana uz akte razdoblja, pa se samo
// zapišu.
func prolazAkataNaSnazi(ctx context.Context, akti *service.AktService, g granicaAkata, sad time.Time) granicaAkata {
	upozorenja, privremene := akti.UskladiStupileNaSnagu(ctx, g.zadnji, sad)
	for _, u := range append(upozorenja, privremene...) {
		log.Printf("povijest obrane: %s", u)
	}
	if len(upozorenja) == 0 {
		return granicaAkata{zadnji: sad}
	}
	if g.greskaOd.IsZero() {
		g.greskaOd = sad
	}
	if sad.Sub(g.greskaOd) > najviseZaostaje {
		log.Printf("povijest obrane: akti koji su stupili na snagu od %s do %s nisu usklađeni ni nakon %s ponavljanja; preskače se na %s, a povijest im uskladi nova ovjera ili storno u sektoru",
			g.zadnji.Format(time.RFC3339), sad.Format(time.RFC3339), najviseZaostaje, sad.Format(time.RFC3339))
		return granicaAkata{zadnji: sad}
	}
	return g
}
