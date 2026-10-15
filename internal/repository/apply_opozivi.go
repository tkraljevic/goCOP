package repository

import (
	"context"

	"gocop/internal/ledger"
)

// entitetOpozivi su opozivi članstva; zapisuje ih sloj razmjene (peers)
const entitetOpozivi = "opozivi"

// opozivZaPovrsinu je verzija opoziva koja ide na površinu: zadnja živa.
// Opoziv se ne poništava (removeFromSurface ga ne briše), pa arhivirana
// verzija, koju može poslati i član s izmijenjenim programom, ne smije
// zakloniti živu. Inače opoziv ne bi stigao u tablicu kad obje verzije
// stignu zajedno ili ostanu samo u knjizi, a opozvani čvor bi ostao
// vjerodostojan (peers.opozvano čita samo tablicu).
func opozivZaPovrsinu(ctx context.Context, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	povijest, err := rec.History(ctx, v.Entity, v.EntityID)
	for _, x := range povijest {
		if !x.Archived {
			return x, true, err
		}
	}
	return v, false, err
}
