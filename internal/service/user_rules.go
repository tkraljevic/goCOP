package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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

// rokUprave je uprava actora nad dosegom dužnosti koja se dodjeljuje
// (sektor, područje), na razini s koje upravlja. Stalna je kad je ijedna
// takva dužnost stalna (i za stalnog globalnog administratora): izvor je tada
// nil. Inače je privremena: izvor je dužnost koja traje najdulje, a rok njezin
// istek, nil dok se ne zna (privremeno imenovanje traje dok traje obrana).
// Kad cilj nije zadan, gledaju se sve upravne dužnosti na toj razini:
// uprava je privremena samo kad su sve privremene.
func rokUprave(p *models.UserPermissions, sectorID *string, areaID *int, sectors areaSector) (*time.Time, *models.Duty) {
	rank := actorRank(p)
	if rank == 1 && p.User.IsGlobalAdmin {
		return nil, nil
	}
	cilj := ciljniSektor(sectorID, areaID, sectors)
	sad := time.Now()
	var izvor *models.Duty
	for i := range p.User.Duties {
		d := &p.User.Duties[i]
		if !upravljaCiljem(d, rank, cilj, areaID, sad) {
			continue
		}
		if !privremenaDuznost(*d) {
			return nil, nil
		}
		if izvor == nil || duljeTraje(*d, *izvor) {
			izvor = d
		}
	}
	if izvor == nil {
		return nil, nil
	}
	return izvor.ExpiresAt, izvor
}

// ciljniSektor je sektor dosega: sektor područja kad je područje zadano i
// poznato, inače upisani sektor
func ciljniSektor(sectorID *string, areaID *int, sectors areaSector) string {
	cilj := ""
	if sectorID != nil {
		cilj = *sectorID
	}
	if areaID != nil {
		if s := sectors(*areaID); s != "" {
			cilj = s
		}
	}
	return cilj
}

// upravljaCiljem: aktivna, neistekla dužnost na razini s koje actor
// upravlja, za sektor ili područje dosega; bez zadanog sektora ili
// područja svaka na toj razini
func upravljaCiljem(d *models.Duty, rank int, ciljSektor string, areaID *int, sad time.Time) bool {
	if !d.IsActive || d.Role.RazinaUprave() != rank || (d.ExpiresAt != nil && d.ExpiresAt.Before(sad)) {
		return false
	}
	switch rank {
	case 2:
		return d.SectorID != nil && (ciljSektor == "" || *d.SectorID == ciljSektor)
	case 3:
		return d.AreaID != nil && (areaID == nil || *d.AreaID == *areaID)
	}
	return true
}

// privremenaDuznost: privremena ispomoć (privremeno imenovanje) ili dužnost
// s istekom; stalna nema ni jedno ni drugo
func privremenaDuznost(d models.Duty) bool { return d.IsTemporary || d.ExpiresAt != nil }

// duljeTraje: dužnost a traje dulje od b; dužnost bez poznatog kraja traje
// dulje od svake s krajem
func duljeTraje(a, b models.Duty) bool {
	if b.ExpiresAt == nil {
		return false
	}
	return a.ExpiresAt == nil || a.ExpiresAt.After(*b.ExpiresAt)
}

// privremenost je ono što dužnost čini privremenom: je li privremena, zadani
// datum, istek s obranom i dužnost uprave o kojoj ovisi
type privremenost struct {
	privremena bool
	rok        *time.Time
	sObranom   bool
	ovisiO     *uuid.UUID
}

// ograniciRok: dužnost koja daje upravu (na razini actora ili nižoj), a
// dodjeljuje je privremena uprava, i sama je privremena: ističe najkasnije
// zajedno s dužnošću iz koje je ta uprava (ovisiO), a zadani rok ne smije biti
// dulji od njezina. Terenske dužnosti (bez uprave) ostaju kako su tražene.
// dosad je dužnost prije izmjene (nil pri dodjeli): izmjena iste uloge i
// dosega ne skraćuje ni ne produljuje ono što je dužnost već imala, pa
// spremanje tuđe dužnosti ne mijenja njezin vijek. Vraća dopušteno od
// traženog.
func ograniciRok(p *models.UserPermissions, role models.Role, sectorID *string, areaID *int, sectionCodes string, sectors areaSector,
	trazeno privremenost, dosad *models.Duty) privremenost {
	if role.RazinaUprave() == 0 {
		return trazeno
	}
	r, izvor := rokUprave(p, sectorID, areaID, sectors)
	if izvor == nil {
		return trazeno
	}
	if istaDuznost(dosad, role, sectorID, areaID, sectionCodes) {
		if !privremenaDuznost(*dosad) {
			return trazeno // stalna ostaje kakva je bila
		}
		// dosadašnja ovisnost i istek s obranom ostaju, a rok najviše do
		// kasnijeg od uprave i dosadašnjeg
		return privremenost{privremena: true, rok: raniji(trazeno.rok, kasniji(r, dosad.ZadaniRok())),
			sObranom: trazeno.sObranom || dosad.IsticeSObranom, ovisiO: dosad.OvisiO}
	}
	return privremenost{privremena: true, rok: raniji(trazeno.rok, r), sObranom: trazeno.sObranom, ovisiO: &izvor.ID}
}

// istaDuznost: izmjena zadržava ulogu i doseg dosadašnje dužnosti
func istaDuznost(dosad *models.Duty, role models.Role, sectorID *string, areaID *int, sectionCodes string) bool {
	return dosad != nil && dosad.Role == role && istiNiz(dosad.SectorID, sectorID) && istiBroj(dosad.AreaID, areaID) && dosad.SectionCodes == sectionCodes
}

// kasniji od dva kraja; nil (kraj se ne zna) je kasniji od svakog
func kasniji(a, b *time.Time) *time.Time {
	if a == nil || b == nil {
		return nil
	}
	if b.After(*a) {
		return b
	}
	return a
}

// raniji od dva kraja; nil (bez kraja) ne skraćuje drugi
func raniji(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil || a.Before(*b) {
		return a
	}
	return b
}

// stalnaUpravaOrganizacije: zastavicu globalnog administratora daje samo
// stalna uprava organizacije (zastavica ili dužnost razine 1 bez roka);
// privremena bi se njome učinila trajnom
func stalnaUpravaOrganizacije(p *models.UserPermissions) bool {
	if p == nil || !p.IsGlobalAdmin {
		return false
	}
	_, izvor := rokUprave(p, nil, nil, nil)
	return izvor == nil
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
	if !role.Poznata() {
		return fmt.Errorf("%w: nepoznata uloga „%s“", ErrUnauthorized, role)
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
