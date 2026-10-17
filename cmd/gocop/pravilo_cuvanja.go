package main

import (
	"context"
	"log"

	"gocop/internal/repository"
)

// postaviPraviloCuvanja postavlja koliko povijesti očitanja ovaj čvor prima
// razmjenom: zadnjih mjeseci za sve letve, a za praćene letve cijelu
// povijest. Kad se praćene letve ne daju pročitati, vrijedi samo razdoblje.
// Zove se pri pokretanju, prije nego što razmjena postoji, i na svaku
// promjenu praćenja. Razmjena promjenu pravila prepozna sama
// (repository.StanjeOgradePovijesti), pa joj se ovdje ništa ne javlja.
func postaviPraviloCuvanja(ctx context.Context, praceno func(context.Context) (map[string]bool, error), mjeseci int) {
	followed, err := praceno(ctx)
	if err != nil {
		log.Printf("Vodostaji: praćene letve nisu pročitane: %v", err)
		followed = map[string]bool{}
	}
	repository.SetReadingHistoryPolicy(repository.ReadingHistoryPolicy{Months: mjeseci, Followed: followed})
	if mjeseci > 0 {
		log.Printf("Vodostaji: razmjenom se preuzimaju očitanja zadnjih %d mjeseci, a za %d praćenih letvi cijela povijest",
			mjeseci, len(followed))
	}
}
