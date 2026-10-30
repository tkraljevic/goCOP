package repository

import (
	"context"

	"gocop/internal/ledger"
)

// entitetOpozivi su opozivi članstva; zapisuje ih sloj razmjene (peers)
const entitetOpozivi = "opozivi"

// opozivZaPovrsinu je verzija opoziva koja ide na površinu: zadnja živa
// koja vrijedi sama za sebe (opozivVrijedi). Opoziv se ne poništava
// (removeFromSurface ga ne briše), pa ni arhivirana verzija ni verzija koja
// ne vrijedi sama za sebe ne mijenjaju ono što je u tablici. Tako opoziv
// stigne u tablicu i kad više verzija stigne zajedno ili ostanu samo u
// knjizi.
func opozivZaPovrsinu(ctx context.Context, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	povijest, err := rec.History(ctx, v.Entity, v.EntityID)
	for _, x := range povijest {
		if !x.Archived && opozivVrijedi(x) {
			return x, true, err
		}
	}
	return v, false, err
}
