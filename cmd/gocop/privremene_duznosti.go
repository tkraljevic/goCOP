package main

import (
	"context"
	"log"
	"time"

	"gocop/internal/service"
)

// pratiPrivremeneDuznosti preračunava istek privremenih imenovanja odmah i
// zatim u krugu (svakih deset minuta): akt o prekidu obrane može stići
// razmjenom s drugog čvora, a ovjera i opoziv na ovom čvoru preračunavaju ga
// i sami. Staje kad stane čvor.
func pratiPrivremeneDuznosti(ctx context.Context, us *service.UserService, svaki time.Duration) {
	t := time.NewTicker(svaki)
	defer t.Stop()
	for {
		if err := us.UskladiPrivremene(); err != nil {
			log.Printf("privremena imenovanja: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
