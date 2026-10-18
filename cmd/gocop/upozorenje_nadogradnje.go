package main

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
)

// upozorenjaNadogradnje su upozorenja pločice razmjene o zapisima koji
// čekaju nadogradnju programa (Odluke (11. 10. 2026.), 1; G5 u
// docs/plan-web-sync.md §3): koliko ih je stiglo na ovo računalo i koliko
// ih čuva svako drugo računalo koje je to javilo. Broje se zapisi, ne
// verzije. Računalo koje broj ne javlja (0.0.38) i ono kod kojeg ništa ne
// čeka nemaju retka, pa se nula nikad ne prikazuje. Greška brojanja ide u
// dnevnik, a na pločici piše samo što učiniti.
func upozorenjaNadogradnje(ctx context.Context, rec *ledger.Recorder, cvorovi []peers.PeerStatus, sad time.Time) []string {
	var out []string
	ceka, err := rec.CekaNadogradnju(ctx)
	switch {
	case err != nil:
		log.Printf("razmjena: brojanje zapisa koji čekaju nadogradnju: %v", err)
		out = append(out, "Ne da se izbrojiti koliko je stiglo zapisa koje ovaj program još ne razumije. Javite administratoru: pojedinosti su u dnevniku programa.")
	case ceka > 0:
		out = append(out, tekstStiglo(ceka))
	}
	for _, ps := range cvorovi {
		if ps.State.CekaNadogradnju > 0 {
			out = append(out, tekstDrugog(ps, sad))
		}
	}
	return out
}

// tekstStiglo je upozorenje za n zapisa koje ovaj program još ne razumije.
// Zamjenica u drugoj rečenici odnosi se na sve što je stiglo, pa je „ga”
// samo uz jedan zapis, a uz 21 zapis „ih”.
func tekstStiglo(n int) string {
	zamjenica := "ih"
	if n == 1 {
		zamjenica = "ga"
	}
	return fmt.Sprintf("%s %d %s ovaj program još ne razumije. Primijenit će %s nakon nadogradnje goCOP-a.",
		models.UzBrojHR(n, "Stigao je", "Stigla su", "Stiglo je"), n, zapisKoji(n), zamjenica)
}

// tekstDrugog je redak za drugo računalo koje čuva zapise za nadogradnju.
// Broj je iz zadnje razmjene s njim; kad ona nije od danas, redak kaže kad
// je broj javljen, jer se od tada mogao nadograditi.
func tekstDrugog(ps peers.PeerStatus, sad time.Time) string {
	n := ps.State.CekaNadogradnju
	ime := cmp.Or(ps.Name, ps.NodeID)
	if z := ps.State.LastOK; z != nil && !istiDan(*z, sad) {
		return fmt.Sprintf("%s je %s javio da čuva %d %s će primijeniti nakon nadogradnje.", ime, danJavljanja(*z, sad), n, zapisKoji(n))
	}
	return fmt.Sprintf("%s čuva %d %s će primijeniti nakon nadogradnje.", ime, n, zapisKoji(n))
}

// istiDan: oba trenutka su istog dana po zagrebačkom vremenu
func istiDan(a, b time.Time) bool {
	return a.In(models.Zagreb).Format(time.DateOnly) == b.In(models.Zagreb).Format(time.DateOnly)
}

// danJavljanja je dan po zagrebačkom vremenu, s godinom samo kad nije ova
func danJavljanja(kad, sad time.Time) string {
	kad = kad.In(models.Zagreb)
	if kad.Year() == sad.In(models.Zagreb).Year() {
		return kad.Format("2. 1.")
	}
	return kad.Format("2. 1. 2006.")
}

// zapisKoji je „zapis koji” ili „zapisa koje”, kako ide uz broj
func zapisKoji(n int) string {
	return models.UzBrojHR(n, "zapis koji", "zapisa koje", "zapisa koje")
}
