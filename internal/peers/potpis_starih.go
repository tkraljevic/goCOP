package peers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// PotpisiVlastiteStare potpiše stare nepotpisane verzije prava i mreže
// pod imenom ovog računala (Odluke (27. 11. 2026.)): entitete iz
// PostaviStrogePotpise, potpisnikom knjige. Primatelj takvu verziju bez
// potpisa od autora koji već potpisuje ne prima (nepotpisana), pa je novo
// računalo inače ne bi dobilo nikad. Zove se pri pokretanju, iza
// PostaviStrogePotpise; uzima bravu upisa, pa smije i dok razmjena radi.
//
// Verzije otprije 0.0.70 nemaju vrijeme primanja, pa se ne zna tko ih je
// napisao: ovo računalo, isto ime prije ponovne instalacije s drugim
// ključem, ili drugi član koji ih je podmetnuo pod ovo ime (do 0.0.69 to
// je prolazilo). Ovdje se isključuje samo drugi ključ: kad pod ovim imenom
// postoji članstvo s drugim ključem (drugiKljucPodImenom), ostaju bez
// potpisa. Podmetnuta verzija se ne da razlikovati od vlastite. Ako je
// takva u knjizi, naknadni potpis je potpiše ključem ovog računala i
// primatelji je onda primaju kao provjerenu. Zato se svaka takva potpisana
// verzija zapiše u dnevnik, da se može pregledati.
func (s *Service) PotpisiVlastiteStare(ctx context.Context) (int, error) {
	entiteti := slices.Sorted(maps.Keys(s.strogiEntiteti))
	if len(entiteti) == 0 {
		return 0, nil
	}
	drugi, err := s.drugiKljucPodImenom(ctx)
	if err != nil {
		// bez članstava stare se ne potpisuju, a novije vlastite da
		err = fmt.Errorf("članstva ovog računala ne daju se pročitati, verzije otprije 0.0.70 ostaju bez potpisa: %w", err)
		drugi = true
	}
	var prijePrimanja func(ledger.Version)
	if !drugi {
		prijePrimanja = func(v ledger.Version) {
			log.Printf("razmjena: naknadno potpisana verzija otprije 0.0.70 pod imenom ovog računala: %s/%s, verzija %s, napisana %s",
				v.Entity, v.EntityID, v.VersionID, v.CreatedAt.UTC().Format("2006-01-02 15:04:05"))
		}
	}
	if err := s.uzmiBravu(ctx); err != nil {
		return 0, err
	}
	defer s.pustiBravu()
	n, perr := s.rec.PotpisiVlastiteStare(ctx, entiteti, prijePrimanja)
	return n, errors.Join(err, perr)
}

// drugiKljucPodImenom: postoji članstvo pod imenom ovog računala s drugim
// ključem. U mreži se broje samo članstva s valjanim potpisom ključa mreže
// (potpisanaImena), i istekla: ponovna instalacija s novim ključem uvijek
// ostavi takvo, a lažno članstvo koje može napisati bilo koji član ne može
// spriječiti potpis. Računalo koje nije ni u jednoj mreži nema čime
// provjeriti potpis, pa broji svako članstvo iz knjige i tablice.
func (s *Service) drugiKljucPodImenom(ctx context.Context) (bool, error) {
	var clanstva []razmjena.Membership
	p, err := s.ucitajClanstva(ctx)
	switch {
	case err == nil:
		clanstva = p.potpisanaImena(s.node.ID)
	case errors.Is(err, errNijeClan):
		if clanstva, err = s.clanstvaBezMreze(ctx); err != nil {
			return false, err
		}
	default:
		return false, err
	}
	return imaDrugiKljuc(clanstva, s.node.ID, s.node.PublicKey()), nil
}

// imaDrugiKljuc: među članstvima je jedno pod imenom id s ključem koji nije
// kljuc
func imaDrugiKljuc(clanstva []razmjena.Membership, id, kljuc string) bool {
	for _, m := range clanstva {
		if m.DeviceID == id && m.DeviceKey != kljuc {
			return true
		}
	}
	return false
}

// clanstvaBezMreze su sva članstva iz knjige i tablice, bez provjere
// potpisa: računalo koje nije ni u jednoj mreži nema ključ mreže
func (s *Service) clanstvaBezMreze(ctx context.Context) ([]razmjena.Membership, error) {
	uKnjizi, err := s.clanstvaUKnjizi(ctx)
	if err != nil {
		return nil, err
	}
	uTablici, err := s.svaClanstva(ctx)
	if err != nil {
		return nil, err
	}
	return append(uKnjizi, uTablici...), nil
}
