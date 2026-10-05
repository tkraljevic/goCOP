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

// najviseZaostaje: granica kruga smije zaostati iza sadašnjeg trenutka
// najviše dan; trajna greška (npr. akt s dionicom koje nema u registru)
// inače bi svaki prolaz iznova izvodio sve akte od zadnjeg čistog prolaza
const najviseZaostaje = unatragPriPokretanju

// pratiAkteNaSnazi izvodi povijest obrane za akte koji su stupili na snagu
// od zadnjeg čistog prolaza, odmah i zatim u krugu (svakih deset minuta):
// akt s kasnijim početkom tako ulazi u epizode i bez nove ovjere ili storna
// u sektoru. Greška se zapiše u dnevnik i krug ide dalje, a propušteno
// razdoblje obuhvati idući prolaz (prolazAkataNaSnazi). Staje kad stane čvor.
func pratiAkteNaSnazi(ctx context.Context, akti *service.AktService, svaki time.Duration) {
	t := time.NewTicker(svaki)
	defer t.Stop()
	zadnji := time.Now().Add(-unatragPriPokretanju)
	for {
		zadnji = prolazAkataNaSnazi(ctx, akti, zadnji, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// prolazAkataNaSnazi izvodi povijest za akte koji su stupili na snagu u
// razdoblju (zadnji, sad] i vraća granicu idućeg prolaza: sad kad je prolaz
// čist. Prolaz s upozorenjem akata (npr. prolazni SQLITE_BUSY) granicu ne
// pomiče, pa idući obuhvati i ovo razdoblje; izvođenje je idempotentno, pa
// ponovljeni akti ne dodaju verzije. Granica ipak ne zaostaje više od
// najviseZaostaje: tada se pomiče i bez čistog prolaza, uz jedno upozorenje
// da su neusklađeni akti preskočeni. Upozorenja privremenih imenovanja nisu
// vezana uz akte razdoblja, pa se samo zapišu.
func prolazAkataNaSnazi(ctx context.Context, akti *service.AktService, zadnji, sad time.Time) time.Time {
	upozorenja, privremene := akti.UskladiStupileNaSnagu(ctx, zadnji, sad)
	for _, u := range append(upozorenja, privremene...) {
		log.Printf("povijest obrane: %s", u)
	}
	if len(upozorenja) == 0 {
		return sad
	}
	if sad.Sub(zadnji) > najviseZaostaje {
		log.Printf("povijest obrane: akti koji su stupili na snagu od %s do %s nisu usklađeni ni nakon %s ponavljanja; preskače se na %s, a povijest im uskladi nova ovjera ili storno u sektoru",
			zadnji.Format(time.RFC3339), sad.Format(time.RFC3339), najviseZaostaje, sad.Format(time.RFC3339))
		return sad
	}
	return zadnji
}
