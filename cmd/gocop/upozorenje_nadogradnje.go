package main

import (
	"cmp"
	"context"
	"fmt"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
)

// upozorenjaNadogradnje su upozorenja pločice razmjene o zapisima koji
// čekaju nadogradnju programa (Odluke (11. 10. 2026.), 1; G5 u
// docs/plan-web-sync.md §3): koliko ih je stiglo na ovo računalo i koliko
// ih čuva svako drugo računalo koje je to javilo. Broje se zapisi, ne
// verzije. Računalo koje broj ne javlja (0.0.38) i ono kod kojeg ništa ne
// čeka nemaju retka, pa se nula nikad ne prikazuje.
func upozorenjaNadogradnje(ctx context.Context, rec *ledger.Recorder, cvorovi []peers.PeerStatus) []string {
	var out []string
	ceka, err := rec.CekaNadogradnju(ctx)
	switch {
	case err != nil:
		out = append(out, "Ne da se izbrojiti koliko je stiglo zapisa koje ovaj program još ne razumije: "+err.Error())
	case ceka > 0:
		out = append(out, tekstStiglo(ceka))
	}
	for _, ps := range cvorovi {
		if n := ps.State.CekaNadogradnju; n > 0 {
			out = append(out, fmt.Sprintf("%s čuva %d %s će primijeniti nakon nadogradnje.", cmp.Or(ps.Name, ps.NodeID), n, zapisKoji(n)))
		}
	}
	return out
}

// tekstStiglo je upozorenje za n zapisa koje ovaj program još ne razumije
func tekstStiglo(n int) string {
	return fmt.Sprintf("%s %d %s ovaj program još ne razumije. Primijenit će %s nakon nadogradnje goCOP-a.",
		models.UzBrojHR(n, "Stigao je", "Stigla su", "Stiglo je"), n, zapisKoji(n), models.UzBrojHR(n, "ga", "ih", "ih"))
}

// zapisKoji je „zapis koji” ili „zapisa koje”, kako ide uz broj
func zapisKoji(n int) string {
	return models.UzBrojHR(n, "zapis koji", "zapisa koje", "zapisa koje")
}
