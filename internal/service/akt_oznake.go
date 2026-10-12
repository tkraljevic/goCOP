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
// različite akte s istom oznakom, ili isti nacrt, koji razmjena nosi na oba,
// svaki sa svojim brojem. Kad ih razmjena spoji, sve ovjere ostaju valjane i
// potpisane; u popisu akata vidljivo su označene i čekaju ljudsku odluku.
// Pravilo rada je da sektor akte objavljuje na jednom čvoru; program to ne
// brani.

// SukobiOznaka su sukobi oznaka, po ID-u akta: gledaju se svi akti iste
// oznake u bazi, ne samo oni koje popis prikazuje, druge ovjere akata iz
// knjige verzija i sva rješenja
func (s *AktService) SukobiOznaka(ctx context.Context) (map[string]models.SukobOznake, error) {
	akti, err := s.repo.AktiIsteOznake(ctx)
	if err != nil {
		return nil, err
	}
	druge, err := s.repo.DrugeOvjereAkata(ctx)
	if err != nil {
		return nil, err
	}
	rjesenja, err := s.repo.ListRjesenjaOznaka(ctx)
	if err != nil {
		return nil, err
	}
	return models.SukobiOznaka(akti, druge, rjesenja), nil
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
// Rješenje je zaseban zapis s verzijom u knjizi, pa ga vide svi čvorovi, a
// akte ne dira: ostaju valjani i potpisani, a izmjena akta napravljena na
// drugom čvoru prije nego što je rješenje stiglo (npr. storno) ostaje.
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
		return fmt.Errorf("akt %s ne dijeli oznaku s drugim aktom i nije ovjeren na drugom čvoru", a.Oznaka())
	}
	return s.repo.SaveRjesenjeOznake(ctx, rjesenjeOznake(*a, sukob, napomena, u, time.Now()))
}

// rjesenjeOznake je rješenje koje obuhvaća sve ovjere sukoba akta: akt,
// ostale akte iste oznake i druge ovjere akta
func rjesenjeOznake(a models.Akt, sukob models.SukobOznake, napomena string, u *models.User, sad time.Time) *models.RjesenjeOznake {
	akti, kodovi := models.KljuceviRjesenja(sukob.SveOvjere(a))
	return &models.RjesenjeOznake{Sektor: a.Sektor, Oznaka: a.Oznaka(), Akti: akti, Kodovi: kodovi,
		Napomena: napomena, RijesioID: u.ID.String(), Rijesio: u.FullName, Kad: sad.UTC().Truncate(time.Second)}
}
