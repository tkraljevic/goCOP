package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gocop/internal/ledger"
)

// primjeraNeispravnih je koliko neispravnih verzija pokretanje navede u
// dnevniku; ostale samo broji
const primjeraNeispravnih = 5

// javiNeispravneUKnjizi pri pokretanju zapiše u dnevnik programa ima li u
// knjizi verzija koje razmjena danas ne bi primila: oznaka nije ispravnog
// oblika, vrijeme u njoj je predaleko u budućnosti ili ime autora ima znak
// |. Mogle su ući prije nego što je razmjena provjeravala oznaku. Knjigu ne
// mijenja; bez takvih verzija ne piše ništa. Čita cijelu knjigu, pa ide u
// svojoj gorutini, pod kontekstom programa: kad program stane prije nego
// što završi, ne piše ništa.
func javiNeispravneUKnjizi(ctx context.Context, rec *ledger.Recorder, sada time.Time) {
	if poruka := neispravneUKnjizi(ctx, rec, sada); poruka != "" && ctx.Err() == nil {
		log.Print(poruka)
	}
}

// neispravneUKnjizi je redak za dnevnik, ili prazno kad nema što javiti
func neispravneUKnjizi(ctx context.Context, rec *ledger.Recorder, sada time.Time) string {
	broj, primjeri, err := rec.NeispravneUKnjizi(ctx, sada, primjeraNeispravnih)
	if err != nil {
		return fmt.Sprintf("Knjiga verzija: provjera oznaka nije prošla: %v", err)
	}
	if broj == 0 {
		return ""
	}
	opisi := make([]string, len(primjeri))
	for i, v := range primjeri {
		opisi[i] = fmt.Sprintf("%.80q (entitet %.80q, autor %.80q: %v)", v.VersionID, v.Entity, v.NodeID, ledger.ProvjeriPrimljenu(v, "", sada))
	}
	return fmt.Sprintf("Knjiga verzija: %d verzija ima oznaku koju razmjena ne bi primila; razmjena ih ne šalje dalje i ne broji u granici, a knjiga ih čuva. Prve: %s",
		broj, strings.Join(opisi, "; "))
}
