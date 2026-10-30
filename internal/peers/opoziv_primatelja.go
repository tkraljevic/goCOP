package peers

// Opoziv članstva koji je potpisao ovlašteni primatelj (Odluke (30. 10.
// 2026.)). Primatelj opoziva članstva koja je sam izdao, ali ovlast za
// primanje opoziva samo ključ mreže (i njegova kopija na drugom računalu).
// Kad primatelj opozove člana čija ovlast do njega još nije stigla, ovlast
// ostaje. Zato računalo s ključem mreže za takvog člana pokaže upozorenje,
// dok mu ovlast ne oduzme ili ga ponovno ne primi.

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"slices"
	"strings"

	"gocop/internal/razmjena"
)

// upozorenjaOpozivaPrimatelja je rečenica za pločicu računala s ključem
// mreže: računala kojima je članstvo opozvao primatelj, a ovlast za
// primanje im još vrijedi (opozvaniPrimateljem)
func (s *Service) upozorenjaOpozivaPrimatelja(ctx context.Context) []string {
	if n := s.NetworkInfo(); n == nil || !n.DrziKljuc {
		return nil
	}
	imena := s.opozvaniPrimateljem(ctx)
	if len(imena) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("Članstvo ovih računala opozvalo je računalo s ovlašću za primanje, a njihova ovlast za primanje i dalje vrijedi: %s. Ovlast oduzima samo računalo s ključem mreže: na stranici Ovo računalo i mreža, pod Mreža, kliknite Oduzmi ovlast za primanje uz svako od njih.",
		strings.Join(imena, ", "))}
}

// opozvaniPrimateljem su imena računala, poredana, s opozivom članstva od
// primatelja (kljuceviOpozivaPrimatelja) i bez valjanog članstva, a s
// ovlašću za primanje istog ključa koja nije opozvana. Kad se opozivi ili
// članstva ne daju pročitati, popis je prazan.
func (s *Service) opozvaniPrimateljem(ctx context.Context) []string {
	opozivi, err := s.opoziviURasponu(ctx, OpozivClanstva+":", OpozivClanstva+";")
	if err != nil {
		return nil
	}
	p, err := s.ucitajClanstva(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for id, kljucevi := range kljuceviOpozivaPrimatelja(p, opozivi) {
		if s.ovlastNakonOpoziva(ctx, p, id, kljucevi) {
			out = append(out, s.imeRacunala(ctx, id))
		}
	}
	slices.Sort(out)
	return out
}

// kljuceviOpozivaPrimatelja su po računalu ključevi iz opoziva primatelja
// (opozivPrimatelja); potpisana članstva svakog računala čitaju se jednom
func kljuceviOpozivaPrimatelja(p *poznataClanstva, opozivi []Opoziv) map[string][]string {
	potpisana := map[string][]razmjena.Membership{}
	out := map[string][]string{}
	for _, op := range opozivi {
		if _, ima := potpisana[op.NodeID]; !ima {
			potpisana[op.NodeID] = p.potpisanaImena(op.NodeID)
		}
		if opozivPrimatelja(p.mreza, potpisana[op.NodeID], op) {
			out[op.NodeID] = append(out[op.NodeID], op.PublicKey)
		}
	}
	return out
}

// ovlastNakonOpoziva: računalo id nema valjano članstvo, a ovlast za
// primanje jednog od zadanih ključeva nije opozvana
func (s *Service) ovlastNakonOpoziva(ctx context.Context, p *poznataClanstva, id string, kljucevi []string) bool {
	if p.izabrano(ctx, id) != nil {
		return false
	}
	ovlasti, err := s.ovlastiCvora(ctx, id)
	return err == nil && slices.ContainsFunc(kljucevi, func(k string) bool { return zadnjaOvlast(ovlasti, k) != nil })
}

// opozivPrimatelja: op je ispravan opoziv članstva koji nije potpisao ključ
// mreže, nego primatelj koji je to ime s tim ključem primio (potpisana su
// potpisana članstva tog imena)
func opozivPrimatelja(mreza ed25519.PublicKey, potpisana []razmjena.Membership, op Opoziv) bool {
	if op.Vrsta != OpozivClanstva || !op.Ispravan() || op.Potpisnik == razmjena.PublicKeyString(mreza) {
		return false
	}
	return slices.ContainsFunc(potpisana, func(m razmjena.Membership) bool {
		return m.DeviceKey == op.PublicKey && m.Primatelj != nil && m.Primatelj.DeviceKey == op.Potpisnik
	})
}
