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
// razdoblju (zadnji, sad] i vraća granicu idućeg prolaza: sad samo kad je
// prolaz čist. Prolaz s greškom ili upozorenjem (npr. prolazni SQLITE_BUSY)
// granicu ne pomiče, pa idući obuhvati i ovo razdoblje; izvođenje je
// idempotentno, pa ponovljeni akti ne dodaju verzije.
func prolazAkataNaSnazi(ctx context.Context, akti *service.AktService, zadnji, sad time.Time) time.Time {
	upozorenja := akti.UskladiStupileNaSnagu(ctx, zadnji, sad)
	for _, u := range upozorenja {
		log.Printf("povijest obrane: %s", u)
	}
	if len(upozorenja) > 0 {
		return zadnji
	}
	return sad
}
