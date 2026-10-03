package service

import (
	"fmt"
	"strings"
	"time"

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

// rokUprave je rok privremene uprave nad dosegom dužnosti koja se dodjeljuje
// (sektor, područje): kraj najdulje actorove dužnosti koja mu daje upravu
// nad tim dosegom na razini s koje upravlja, kad su sve takve privremene;
// nil kad je ta uprava stalna (i za stalnog globalnog administratora).
func rokUprave(p *models.UserPermissions, sectorID *string, areaID *int, sectors areaSector) *time.Time {
	rank := actorRank(p)
	if rank == 1 && p.User.IsGlobalAdmin {
		return nil
	}
	ciljSektor := ""
	if sectorID != nil {
		ciljSektor = *sectorID
	}
	if areaID != nil {
		if s := sectors(*areaID); s != "" {
			ciljSektor = s
		}
	}
	sad := time.Now()
	var rok *time.Time
	for _, d := range p.User.Duties {
		if !d.IsActive || d.Role.RazinaUprave() != rank || (d.ExpiresAt != nil && d.ExpiresAt.Before(sad)) {
			continue
		}
		switch rank {
		case 2:
			if d.SectorID == nil || *d.SectorID != ciljSektor {
				continue
			}
		case 3:
			if d.AreaID == nil || areaID == nil || *d.AreaID != *areaID {
				continue
			}
		}
		if d.ExpiresAt == nil {
			return nil
		}
		if rok == nil || d.ExpiresAt.After(*rok) {
			r := *d.ExpiresAt
			rok = &r
		}
	}
	return rok
}

// ograniciRok: dužnost koja daje upravu na razini actora, a dodjeljuje je
// privremena uprava, traje najdulje do isteka te uprave nad istim dosegom;
// inače bi račun koji je otvorila zadržao upravu i poslije isteka. dosad je
// dužnost prije izmjene (nil pri dodjeli): izmjena iste uloge i dosega smije
// zadržati ono što je dužnost već imala, pa spremanje tuđe stalne dužnosti
// nije skraćuje. Vraća je li dužnost privremena i njezin rok.
func ograniciRok(p *models.UserPermissions, role models.Role, sectorID *string, areaID *int, sectionCodes string, sectors areaSector,
	privremena bool, rok *time.Time, dosad *models.Duty) (bool, *time.Time) {
	razina := role.RazinaUprave()
	if razina == 0 || razina > actorRank(p) {
		return privremena, rok
	}
	r := rokUprave(p, sectorID, areaID, sectors)
	if r == nil {
		return privremena, rok
	}
	if dosad != nil && dosad.Role == role && istiNiz(dosad.SectorID, sectorID) && istiBroj(dosad.AreaID, areaID) && dosad.SectionCodes == sectionCodes {
		if dosad.ExpiresAt == nil {
			return privremena, rok // stalna ostaje kakva je bila
		}
		if dosad.ExpiresAt.After(*r) {
			r = dosad.ExpiresAt
		}
	}
	if rok == nil || rok.After(*r) {
		rok = r
	}
	return true, rok
}

// stalnaUpravaOrganizacije: zastavicu globalnog administratora daje samo
// stalna uprava organizacije (zastavica ili dužnost razine 1 bez roka);
// privremena bi se njome učinila trajnom
func stalnaUpravaOrganizacije(p *models.UserPermissions) bool {
	return p != nil && p.IsGlobalAdmin && rokUprave(p, nil, nil, nil) == nil
}

func istiNiz(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
func istiBroj(a, b *int) bool   { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// sektoriPodrucja gradi areaSector iz popisa područja
func sektoriPodrucja(listAreas func(sectorID string) ([]models.Area, error)) areaSector {
	m := map[int]string{}
	if areas, err := listAreas(""); err == nil {
		for _, a := range areas {
			m[a.ID] = a.SectorID
		}
	}
	return func(id int) string { return m[id] }
}

// dutyInScope javlja pokriva li actor s te razine sektor ili područje dužnosti
func dutyInScope(p *models.UserPermissions, rank int, sectorID *string, areaID *int, sectors areaSector) bool {
	switch rank {
	case 1:
		return true
	case 2:
		// i upisani sektor mora biti u dosegu (zapis s tuđim sektorom uz
		// područje iz svog ne prolazi)
		if areaID != nil && *areaID > 0 {
			return p.AdminSectors[sectors(*areaID)] && (sectorID == nil || p.AdminSectors[*sectorID])
		}
		return sectorID != nil && p.AdminSectors[*sectorID]
	case 3:
		return areaID != nil && p.AdminAreas[*areaID] && (sectorID == nil || *sectorID == sectors(*areaID))
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

// zaduzujeTudji provjerava račun kojem dužnost dodaje ili produljuje netko
// tko nije globalni administrator: račun mora postojati, biti uključen, ne
// biti globalni administrator i imati bar jednu aktivnu, neisteklu dužnost.
// Račun bez dužnosti (opozvane ili istekle, ili otvoren bez dužnosti)
// mayManage štiti od svih osim globalnog administratora; kad bi mu uprava
// smjela dodati dužnost u svom dosegu, odmah bi ga smjela i uređivati,
// uključiti i poništiti mu lozinku. Takav račun zadužuje globalni
// administrator.
func zaduzujeTudji(target *models.User) error {
	if target == nil {
		return ErrUserNotFound
	}
	if target.IsGlobalAdmin {
		return fmt.Errorf("%w: globalnog administratora zadužuje globalni administrator", ErrUnauthorized)
	}
	if !target.IsActive {
		return fmt.Errorf("%w: isključen račun zadužuje globalni administrator", ErrUnauthorized)
	}
	sad := time.Now()
	for _, d := range target.Duties {
		if d.IsActive && (d.ExpiresAt == nil || d.ExpiresAt.After(sad)) {
			return nil
		}
	}
	return fmt.Errorf("%w: račun bez aktivne dužnosti zadužuje globalni administrator", ErrUnauthorized)
}

// dionicaPodrucja vraća područje i sektor dionice; ok=false kad je nema u
// registru
type dionicaPodrucja func(code string) (areaID int, sectorID string, ok bool)

// sifreDionica rastavlja popis šifri dionica odvojenih zarezom
func sifreDionica(sectionCodes string) []string {
	var out []string
	for _, k := range strings.Split(sectionCodes, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// normalizeScope izvodi doseg iz uloge i provjerava da je cilj upisan i
// dosljedan: sektor se uvijek uzima iz područja, a dionice moraju biti iz
// tog područja. Inače bi se uz područje iz svog dosega upisao tuđi sektor
// ili tuđe dionice, a ovlasti se računaju iz upisanog (NewUserPermissions):
// uprava sektora A dodijelila bi sebi upravu sektora B.
func normalizeScope(role models.Role, sectorID *string, areaID *int, sectionCodes string, sectors areaSector, dionice dionicaPodrucja) (models.ScopeType, *string, *int, error) {
	scope := role.NaturalScope()
	if scope == models.ScopeAll {
		return scope, nil, nil, nil
	}
	if areaID != nil && *areaID <= 0 {
		areaID = nil
	}
	if sectorID != nil && strings.TrimSpace(*sectorID) == "" {
		sectorID = nil
	}
	// dionice određuju područje: sve iz jednog, i iz upisanog ako ga ima
	for _, k := range sifreDionica(sectionCodes) {
		a, _, ok := dionice(k)
		if !ok {
			return scope, nil, nil, fmt.Errorf("%w: dionica %s nije u registru", ErrInvalidUserData, k)
		}
		if areaID == nil {
			areaID = &a
		}
		if *areaID != a {
			return scope, nil, nil, fmt.Errorf("%w: dionica %s nije u području %d", ErrInvalidUserData, k, *areaID)
		}
	}
	// sektor slijedi iz područja; upisani se mora slagati
	if areaID != nil {
		s := sectors(*areaID)
		if s == "" {
			return scope, nil, nil, fmt.Errorf("%w: područje %d nije u registru", ErrInvalidUserData, *areaID)
		}
		if sectorID != nil && *sectorID != s {
			return scope, nil, nil, fmt.Errorf("%w: područje %d nije u sektoru %s", ErrInvalidUserData, *areaID, *sectorID)
		}
		sectorID = &s
	}
	switch scope {
	case models.ScopeSector:
		if sectorID == nil {
			return scope, nil, nil, fmt.Errorf("%w: uloga „%s“ traži %s", ErrInvalidUserData, role.Label(), models.Terms().Lower("sektor"))
		}
	case models.ScopeArea:
		if areaID == nil {
			return scope, nil, nil, fmt.Errorf("%w: uloga „%s“ traži %s", ErrInvalidUserData, role.Label(), models.Terms().Lower("podrucje"))
		}
	case models.ScopeSection:
		// dionice, ili bar područje: terenske uloge smiju pokrivati cijelo
		// područje. Samo uz sektor dužnost ne bi davala nikakvo pravo
		// (NewUserPermissions), a stajala bi kao valjana.
		if strings.TrimSpace(sectionCodes) == "" {
			if areaID == nil {
				return scope, nil, nil, fmt.Errorf("%w: uloga „%s“ traži dionice ili %s", ErrInvalidUserData, role.Label(), models.Terms().Lower("podrucje"))
			}
			scope = models.ScopeArea
		}
	}
	return scope, sectorID, areaID, nil
}
