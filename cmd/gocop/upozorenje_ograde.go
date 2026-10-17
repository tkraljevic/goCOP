package main

import (
	"cmp"
	"fmt"
	"strings"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// upozorenjeOgrade je upozorenje pločice razmjene (Odluke (17. 10. 2026.),
// 1A): računalo koje ne prima tuđa očitanja starija od mjeseci (postavka
// povijest_mjeseci) javlja drugima koja je izostavilo (oznaka rupe/1,
// internal/peers/rupe.go). Računalo sa starijim programom to ne razumije:
// niz s rupom primi kao cijeli i preda ga dalje, pa se izostavljeno može
// izgubiti. Navode se računala bez oznake i ona čije se mogućnosti još ne
// znaju. Bez ograde upozorenja nema.
func upozorenjeOgrade(mjeseci int, cvorovi []peers.PeerStatus) []string {
	if mjeseci <= 0 {
		return nil
	}
	var stari []string
	for _, ps := range cvorovi {
		if razmjena.Dogovor(razmjena.NaseMogucnosti(), ps.State.Mogucnosti)[razmjena.ImeRupe] != 1 {
			stari = append(stari, cmp.Or(ps.Name, ps.NodeID))
		}
	}
	if len(stari) == 0 {
		return nil
	}
	return []string{tekstOgrade(mjeseci, stari)}
}

// tekstOgrade je upozorenje za jedno ili više računala sa starijim programom
func tekstOgrade(mjeseci int, stari []string) string {
	razdoblje := fmt.Sprintf("%d %s", mjeseci, models.UzBrojHR(mjeseci, "mjeseca", "mjeseca", "mjeseci"))
	if len(stari) == 1 {
		return fmt.Sprintf("Ovo računalo ne čuva tuđa očitanja starija od %s, a računalo %s ima stariji program koji to ne zna, "+
			"pa bi stara očitanja moglo izgubiti. Dok se %s ne nadogradi, isključite ograničenje povijesti (povijest_mjeseci) "+
			"u postavkama ovog računala.", razdoblje, stari[0], stari[0])
	}
	return fmt.Sprintf("Ovo računalo ne čuva tuđa očitanja starija od %s, a računala %s i %s imaju stariji program koji to ne zna, "+
		"pa bi stara očitanja mogla izgubiti. Dok se ne nadograde, isključite ograničenje povijesti (povijest_mjeseci) "+
		"u postavkama ovog računala.",
		razdoblje, strings.Join(stari[:len(stari)-1], ", "), stari[len(stari)-1])
}
