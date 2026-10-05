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
// od zadnjeg prolaza, odmah i zatim u krugu (svakih deset minuta): akt s
// kasnijim početkom tako ulazi u epizode i bez nove ovjere ili storna u
// sektoru. Greška se zapiše u dnevnik i krug ide dalje (propušteno izvede
// iduća ovjera ili storno u sektoru). Staje kad stane čvor.
func pratiAkteNaSnazi(ctx context.Context, akti *service.AktService, svaki time.Duration) {
	t := time.NewTicker(svaki)
	defer t.Stop()
	zadnji := time.Now().Add(-unatragPriPokretanju)
	for {
		sad := time.Now()
		for _, u := range akti.UskladiStupileNaSnagu(ctx, zadnji, sad) {
			log.Printf("povijest obrane: %s", u)
		}
		zadnji = sad
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
