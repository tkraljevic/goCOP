package service

import (
	"fmt"
	"strings"

	"gocop/internal/models"
)

// Pravila upravljanja računima i dužnostima.
//
// Tko upravlja: razina 1 svime; uprava sektora (razina 2) računima i
// dužnostima svog sektora, ali ne dijeli uloge razine 1; uprava područja
// (razina 3) računima i dužnostima svog područja, ne dijeli uloge razina 1
// i 2. Ispod razine 3 nitko ne upravlja računima. Doseg dužnosti određuje
// uloga: uloga sektora traži sektor, uloga područja područje, uloga dionice
// dionice.

// actorRank je razina s koje netko upravlja: 1 uprava organizacije, 2
// sektor, 3 područje, 0 kad ne upravlja ničim
func actorRank(p *models.UserPermissions) int {
	switch {
	case p == nil:
		return 0
	case p.IsGlobalAdmin:
		return 1
	case len(p.AdminSectors) > 0:
		return 2
	case len(p.AdminAreas) > 0:
		return 3
	}
	return 0
}

// areaSector vraća sektor područja; prazno kad područje nije poznato
type areaSector func(areaID int) string

// dutyInScope javlja pokriva li actor s te razine sektor ili područje dužnosti
func dutyInScope(p *models.UserPermissions, rank int, sectorID *string, areaID *int, sectors areaSector) bool {
	switch rank {
	case 1:
		return true
	case 2:
		if areaID != nil && *areaID > 0 {
			return p.AdminSectors[sectors(*areaID)]
		}
		return sectorID != nil && p.AdminSectors[*sectorID]
	case 3:
		return areaID != nil && p.AdminAreas[*areaID]
	}
	return false
}

// mayAssign javlja smije li actor dodijeliti ili opozvati dužnost s tom
// ulogom i dosegom
func mayAssign(p *models.UserPermissions, role models.Role, sectorID *string, areaID *int, sectors areaSector) error {
	rank := actorRank(p)
	if rank == 0 {
		return ErrUnauthorized
	}
	if role == models.RoleGlobalAdmin && rank > 1 {
		return ErrUnauthorized
	}
	if role.Rank() < rank {
		return fmt.Errorf("%w: uloga „%s“ dodjeljuje se s više razine", ErrUnauthorized, role.Label())
	}
	if !dutyInScope(p, rank, sectorID, areaID, sectors) {
		return fmt.Errorf("%w: izvan vašeg sektora ili područja", ErrUnauthorized)
	}
	return nil
}

// mayManage javlja smije li actor uređivati ili brisati tuđi račun: sve
// dužnosti te osobe moraju biti u njegovom dosegu i na njegovoj razini ili niže
func mayManage(p *models.UserPermissions, target *models.User, sectors areaSector) error {
	rank := actorRank(p)
	if rank == 0 {
		return ErrUnauthorized
	}
	if rank == 1 {
		return nil
	}
	if target.IsGlobalAdmin {
		return ErrUnauthorized
	}
	if len(target.Duties) == 0 {
		return fmt.Errorf("%w: osoba nema dužnosti u vašem dosegu", ErrUnauthorized)
	}
	for _, d := range target.Duties {
		if err := mayAssign(p, d.Role, d.SectorID, d.AreaID, sectors); err != nil {
			return err
		}
	}
	return nil
}

// normalizeScope izvodi doseg iz uloge i provjerava da je cilj upisan
func normalizeScope(role models.Role, sectorID *string, areaID *int, sectionCodes string, sectors areaSector) (models.ScopeType, *string, *int, error) {
	scope := role.NaturalScope()
	if areaID != nil && *areaID <= 0 {
		areaID = nil
	}
	if sectorID != nil && strings.TrimSpace(*sectorID) == "" {
		sectorID = nil
	}
	// sektor slijedi iz područja kad nije upisan
	if sectorID == nil && areaID != nil {
		if s := sectors(*areaID); s != "" {
			sectorID = &s
		}
	}
	switch scope {
	case models.ScopeAll:
		return scope, nil, nil, nil
	case models.ScopeSector:
		if sectorID == nil {
			return scope, nil, nil, fmt.Errorf("%w: uloga „%s“ traži %s", ErrInvalidUserData, role.Label(), models.Terms().Lower("sektor"))
		}
	case models.ScopeArea:
		if areaID == nil {
			return scope, nil, nil, fmt.Errorf("%w: uloga „%s“ traži %s", ErrInvalidUserData, role.Label(), models.Terms().Lower("podrucje"))
		}
	case models.ScopeSection:
		// dionice, ili bar područje: terenske uloge smiju pokrivati cijelo područje
		if strings.TrimSpace(sectionCodes) == "" && areaID != nil {
			scope = models.ScopeArea
		}
	}
	return scope, sectorID, areaID, nil
}
