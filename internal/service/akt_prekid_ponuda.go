package service

import (
	"context"

	"gocop/internal/models"
)

// Prekidljivi su akti među zadanima uz koje se nudi „Prekini”: ovjereni,
// neponišteni akti o uspostavi koje još nijedan prekid ne stavlja izvan
// snage (AktiZaPrekid), u sektoru u kojem osoba sastavlja nacrte
// (SmijeSastaviti). Prekid se tako sastavlja sa stranice akta ili s popisa,
// bez odlaska na letvu.
func (s *AktService) Prekidljivi(ctx context.Context, perms *models.UserPermissions, akti []models.Akt) map[string]bool {
	out := map[string]bool{}
	naSnazi := map[string]map[string]bool{} // letva → neprekinute uspostave
	for _, a := range akti {
		if !a.Ovjeren() || !neponistenaUspostava(a, "") || !SmijeSastaviti(perms, a.Sektor) {
			continue
		}
		ids, ok := naSnazi[a.StationID]
		if !ok {
			ids = map[string]bool{}
			svi, err := s.AktiZaPrekid(ctx, a.StationID, "")
			if err != nil {
				continue // bez popisa se prekid ne nudi; nudi ga letva
			}
			for _, k := range svi {
				ids[k.ID] = true
			}
			naSnazi[a.StationID] = ids
		}
		if ids[a.ID] {
			out[a.ID] = true
		}
	}
	return out
}
