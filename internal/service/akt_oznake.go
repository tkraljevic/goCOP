package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
)

// Sukob oznaka (Odluke (8. 10. 2026.), pitanje 4). Redni broj akta jedinstven
// je na čvoru (AktiRepository.OvjeriAkt), ali dva čvora bez veze mogu ovjeriti
// različite akte s istom oznakom. Kad ih razmjena spoji, oba ostaju valjana i
// potpisana; u popisu akata vidljivo su označena i čekaju ljudsku odluku.
// Pravilo rada je da sektor akte objavljuje na jednom čvoru; program to ne
// brani.

// SukobiOznaka su sukobi oznaka, po ID-u akta: gledaju se svi akti iste
// oznake u bazi, ne samo oni koje popis prikazuje
func (s *AktService) SukobiOznaka(ctx context.Context) (map[string]models.SukobOznake, error) {
	akti, err := s.repo.AktiIsteOznake(ctx)
	if err != nil {
		return nil, err
	}
	return models.SukobiOznaka(akti), nil
}

// SmijeRijesitiOznaku javlja smije li osoba sukob oznake akta označiti
// riješenim: Voditelj Centra obrane od poplava sektora akta ili njegov
// zamjenik, s dužnošću koja vrijedi sada
func SmijeRijesitiOznaku(perms *models.UserPermissions, a *models.Akt) bool {
	if perms == nil || a == nil {
		return false
	}
	for _, d := range aktivnaZaduzenja(perms) {
		if (d.Role == models.RoleCopLeader || d.Role == models.RoleCopDeputy) && d.SectorID != nil && *d.SectorID == a.Sektor {
			return true
		}
	}
	return false
}

// RijesiOznaku označava sukob oznake akta riješenim, s kratkom napomenom.
// Rješenje se upisuje na sve akte te oznake, u jednoj transakciji i s
// verzijama u knjizi, pa ga vide svi čvorovi; akti ostaju valjani i
// potpisani, jer rješenje ne ulazi u potpisani sadržaj.
func (s *AktService) RijesiOznaku(ctx context.Context, perms *models.UserPermissions, u *models.User, id, napomena string) error {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return err
	}
	napomena = strings.TrimSpace(napomena)
	switch {
	case a == nil:
		return fmt.Errorf("akt ne postoji")
	case u == nil || !SmijeRijesitiOznaku(perms, a):
		return fmt.Errorf("%w: sukob oznake rješava Voditelj Centra obrane od poplava ili zamjenik", ErrUnauthorized)
	case napomena == "":
		return fmt.Errorf("upišite kratku napomenu kako je sukob oznake riješen")
	}
	sukobi, err := s.SukobiOznaka(ctx)
	if err != nil {
		return err
	}
	sukob, ima := sukobi[a.ID]
	if !ima {
		return fmt.Errorf("akt %s ne dijeli oznaku s drugim aktom", a.Oznaka())
	}
	return s.repo.SaveAkti(ctx, rijeseniAkti(*a, sukob.Drugi, napomena, u, time.Now())...)
}

// rijeseniAkti su akt i ostali akti iste oznake, svaki s istim rješenjem
// koje ih sve obuhvaća
func rijeseniAkti(a models.Akt, drugi []models.Akt, napomena string, u *models.User, sad time.Time) []*models.Akt {
	skupina := append([]models.Akt{a}, drugi...)
	rjesenje := &models.RjesenjeOznake{Akti: models.IDAkata(skupina), Napomena: napomena,
		RijesioID: u.ID.String(), Rijesio: u.FullName, Kad: sad.UTC().Truncate(time.Second)}
	out := make([]*models.Akt, len(skupina))
	for i := range skupina {
		skupina[i].RijesenaOznaka = rjesenje
		out[i] = &skupina[i]
	}
	return out
}
