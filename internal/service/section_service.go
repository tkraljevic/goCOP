package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/repository"
)

var (
	ErrSectionNotFound = errors.New("dionica nije pronađena")
	ErrInvalidSection  = errors.New("neispravni podaci dionice")
)

type SectionService struct {
	sectionRepo *repository.SectionRepository
	sse         *SSEBroker
}

func NewSectionService(sectionRepo *repository.SectionRepository, sse *SSEBroker) *SectionService {
	return &SectionService{
		sectionRepo: sectionRepo,
		sse:         sse,
	}
}

// ListSections vraća filtrirane dionice
func (s *SectionService) ListSections(sectorID string, areaID int, search string) ([]models.Section, error) {
	return s.sectionRepo.ListSections(sectorID, areaID, search)
}

// GetSectionWithDetails dohvaća dionicu i sve pripadajuće djelatnike na dionici i branjenom području
func (s *SectionService) GetSectionWithDetails(code string) (*models.Section, error) {
	sec, err := s.sectionRepo.GetSectionByCode(code)
	if err != nil {
		return nil, err
	}
	if sec == nil {
		return nil, ErrSectionNotFound
	}

	personnel, err := s.sectionRepo.GetSectionPersonnel(sec.Code, sec.AreaID, sec.SectorID)
	if err == nil {
		sec.Personnel = personnel
	}

	return sec, nil
}

// CanEditSection provjerava ima li prijavljeni korisnik ovlasti mijenjati zadanu dionicu
func (s *SectionService) CanEditSection(perms *models.UserPermissions, sec *models.Section) bool {
	if perms == nil || sec == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	// Voditelji/zamjenici sektora (npr. Mario Spajić, Tomislav Kraljević na Sektoru B)
	if perms.AdminSectors[sec.SectorID] || perms.AllowedSectors[sec.SectorID] {
		return true
	}
	// Rukovoditelji branjenog područja
	if perms.AdminAreas[sec.AreaID] || perms.AllowedAreas[sec.AreaID] {
		return true
	}
	// Specifično dodijeljene dionice (rukovoditelj dionice)
	if perms.AllowedSections[sec.Code] {
		return true
	}

	return false
}

// CanCreateSectionInArea provjerava može li korisnik dodavati novu dionicu u zadano branjeno područje
func (s *SectionService) CanCreateSectionInArea(perms *models.UserPermissions, sectorID string, areaID int) bool {
	if perms == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	if sectorID != "" && (perms.AdminSectors[sectorID] || perms.AllowedSectors[sectorID]) {
		return true
	}
	if areaID > 0 && (perms.AdminAreas[areaID] || perms.AllowedAreas[areaID]) {
		return true
	}
	return false
}

// SaveSection upisuje novu ili izmijenjenu dionicu s poddionicama. Nova
// traži pravo pisanja u području, postojeća pravo uređivanja te dionice.
func (s *SectionService) SaveSection(ctx context.Context, perms *models.UserPermissions, sec *models.Section, isNew bool) error {
	if sec == nil {
		return ErrInvalidSection
	}
	sec.Code = strings.ToUpper(strings.TrimSpace(sec.Code))
	if sec.Code == "" || sec.AreaID <= 0 {
		return ErrInvalidSection
	}
	if !reSectionCode.MatchString(sec.Code) {
		return fmt.Errorf("šifra dionice ima oblik SEKTOR.PODRUČJE.BROJ, npr. B.15.5")
	}
	existing, err := s.sectionRepo.GetSectionByCode(sec.Code)
	if err != nil {
		return err
	}
	if isNew {
		if existing != nil {
			return fmt.Errorf("dionica sa šifrom '%s' već postoji", sec.Code)
		}
		if err := s.sifraUPodrucju(ctx, sec); err != nil {
			return err
		}
		if !s.CanCreateSectionInArea(perms, sec.SectorID, sec.AreaID) {
			return ErrUnauthorized
		}
	} else {
		if existing == nil {
			return ErrSectionNotFound
		}
		if !s.CanEditSection(perms, existing) {
			return ErrUnauthorized
		}
		sec.AreaID, sec.SectorID = existing.AreaID, existing.SectorID
	}
	if err := validateParts(sec); err != nil {
		return err
	}
	// Registar voda zajednički je svim dionicama i mijenja ga globalni
	// administrator (WatercourseService); novi objekt smije upisati tko smije
	// urediti dionicu, kao i novi nasip.
	for _, p := range sec.Parts {
		for _, o := range p.Objects {
			if o.NewRecord != nil && o.NewRecord.Registry == models.RegistryWatercourse && (perms == nil || !perms.IsGlobalAdmin) {
				return fmt.Errorf("novu vodu u registar upisuje globalni administrator — %q", o.NewRecord.Name)
			}
		}
	}
	// nova veza na objekt ili vodomjer drugog područja dala bi pisanje po
	// njemu (objekt, očitanja, akti vodomjera); provjerava se nakon
	// povezivanja s registrima, jer se vodomjer veže i po nazivu
	provjeri := func(nove repository.NoveVezeDionice) error { return provjeriNoveVeze(perms, sec, nove) }
	if err := s.sectionRepo.SaveSectionUzProvjeru(ctx, sec, provjeri); err != nil {
		return err
	}
	if isNew {
		s.sse.Broadcast("section_created", fmt.Sprintf("Dodana nova dionica: %s", sec.Code), sec.Code)
	} else {
		s.sse.Broadcast("section_updated", fmt.Sprintf("Ažurirana dionica: %s", sec.Code), sec.Code)
	}
	return nil
}

// sifraUPodrucju provjerava da šifra nove dionice nosi njezin sektor i
// područje: B.15.5 ide u područje 15 sektora B. Pravo se priznaje po
// području, pa bi šifra tuđeg sektora ili područja upisala dionicu ondje
// gdje onaj tko piše nema ništa. Zadani sektor mora biti isti.
func (s *SectionService) sifraUPodrucju(ctx context.Context, sec *models.Section) error {
	dijelovi := strings.Split(sec.Code, ".")
	sektor, podrucje := dijelovi[0], dijelovi[1]
	if podrucje != strconv.Itoa(sec.AreaID) {
		return fmt.Errorf("šifra %s nosi branjeno područje %s, a dionica se upisuje u područje %d", sec.Code, podrucje, sec.AreaID)
	}
	if zadani := strings.ToUpper(strings.TrimSpace(sec.SectorID)); zadani != "" && zadani != sektor {
		return fmt.Errorf("šifra %s nosi sektor %s, a zadan je sektor %s", sec.Code, sektor, zadani)
	}
	sektorPodrucja, err := s.sectionRepo.SektorPodrucja(ctx, sec.AreaID)
	if err != nil {
		return err
	}
	if sektorPodrucja != sektor {
		return fmt.Errorf("branjeno područje %d pripada sektoru %s, a šifra %s sektoru %s", sec.AreaID, sektorPodrucja, sec.Code, sektor)
	}
	sec.SectorID = sektor
	return nil
}

// provjeriNoveVeze dopušta novu vezu dionice na objekt iz registra samo kad
// je objekt iz područja dionice ili kad onaj tko sprema piše u području ili
// sektoru objekta; vodomjer koji je već mjerodavan za druge dionice samo kad
// je neka od njih u području dionice ili onaj tko sprema piše na njoj.
// Objekt drugog područja smije stajati na dionici (CS Budžak područja 16 na
// B.34.1), ali vezu upisuje tko ondje piše: rukovoditelj dionice inače bi
// vezom sebi dao objekt, očitanja i akte tuđeg područja i sektora. Veze koje
// dionica već ima ostaju i ne provjeravaju se.
func provjeriNoveVeze(perms *models.UserPermissions, sec *models.Section, nove repository.NoveVezeDionice) error {
	if perms == nil {
		return ErrUnauthorized
	}
	if perms.IsGlobalAdmin {
		return nil
	}
	pise := func(sektor string, podrucje int) bool {
		return perms.AdminSectors[sektor] || perms.AdminAreas[podrucje] || perms.AllowedSectors[sektor] || perms.AllowedAreas[podrucje]
	}
	for _, o := range nove.Objekti {
		st := o.Objekt
		if st == nil {
			return fmt.Errorf("objekt %s nije u registru objekata", o.ID)
		}
		if st.AreaID != sec.AreaID && !pise(st.SectorID, st.AreaID) {
			return fmt.Errorf("%w: objekt „%s” pripada branjenom području %d (sektor %s); uz dionicu %s veže ga tko piše u tom području",
				ErrUnauthorized, st.Name, st.AreaID, st.SectorID, sec.Code)
		}
	}
	for _, v := range nove.Vodomjeri {
		if len(v.Dionice) == 0 {
			continue // vodomjer bez dionica nikome ne pripada
		}
		smije := false
		sifre := make([]string, 0, len(v.Dionice))
		for _, d := range v.Dionice {
			sifre = append(sifre, d.Code)
			if d.AreaID == sec.AreaID || pise(d.SectorID, d.AreaID) || perms.AllowedSections[d.Code] {
				smije = true
			}
		}
		if !smije {
			naziv := v.Naziv
			if naziv == "" {
				naziv = v.ID
			}
			return fmt.Errorf("%w: vodomjer „%s” mjerodavan je za dionice %s drugog područja; uz dionicu %s veže ga tko piše na nekoj od njih",
				ErrUnauthorized, naziv, strings.Join(sifre, ", "), sec.Code)
		}
	}
	return nil
}

var reSectionCode = regexp.MustCompile(`^[A-F]\.\d{1,2}\.\d{1,3}$`)

// validateParts provjerava poddionice: bar jedna, svaka s vodom ili opisom,
// raspon uređen od manje prema većoj stacionaži
func validateParts(sec *models.Section) error {
	if len(sec.Parts) == 0 {
		return fmt.Errorf("dionica mora imati bar jednu poddionicu")
	}
	for i := range sec.Parts {
		p := &sec.Parts[i]
		p.Seq = i + 1
		if strings.TrimSpace(p.WatercourseCode) == "" && strings.TrimSpace(p.Description) == "" {
			return fmt.Errorf("poddionica %d nema vodotok", i+1)
		}
		if p.KmFrom != nil && p.KmTo != nil && *p.KmFrom > *p.KmTo {
			*p.KmFrom, *p.KmTo = *p.KmTo, *p.KmFrom
		}
		if p.Bank != "" && p.Bank != "L" && p.Bank != "D" && p.Bank != "LD" {
			return fmt.Errorf("poddionica %d: obala je L, D ili LD", i+1)
		}
		for j := range p.Objects {
			if strings.TrimSpace(p.Objects[j].Name) == "" && p.Objects[j].StructureID == "" {
				return fmt.Errorf("poddionica %d: objekt bez naziva", i+1)
			}
		}
		for j := range p.Embankments {
			if strings.TrimSpace(p.Embankments[j].Name) == "" && p.Embankments[j].StructureID == "" {
				return fmt.Errorf("poddionica %d: nasip bez naziva", i+1)
			}
		}
	}
	return nil
}

// SljedecaSifra predlaže šifru sljedeće dionice u području: isti sektor i
// područje, prvi slobodan broj.
//
// Dionice se upisuju u nizu — sektor B ima 65 dionica u pet područja — pa je
// tipkanje šifre za svaku od njih posao koji program može obaviti. Prijedlog se
// smije prepisati: šifra nije uvijek neprekinut niz, a dionica koja je jednom
// ukinuta ne vraća svoj broj.
func (s *SectionService) SljedecaSifra(sectorID string, areaID int) string {
	if sectorID == "" || areaID <= 0 {
		return ""
	}
	predmetak := sectorID + "." + strconv.Itoa(areaID) + "."
	dionice, err := s.ListSections(sectorID, areaID, "")
	if err != nil {
		return predmetak + "1"
	}
	sifre := make([]string, 0, len(dionice))
	for _, d := range dionice {
		sifre = append(sifre, d.Code)
	}
	return sljedecaSifra(predmetak, sifre)
}

// sljedecaSifra je sam račun, odvojen da se može ispitati bez baze.
func sljedecaSifra(predmetak string, sifre []string) string {
	najveci := 0
	for _, c := range sifre {
		if !strings.HasPrefix(c, predmetak) {
			continue
		}
		// Rep mora biti sam broj: "B.34.2" da, "B.34.2a" i "B.340.1" ne.
		n, err := strconv.Atoi(strings.TrimPrefix(c, predmetak))
		if err == nil && n > najveci {
			najveci = n
		}
	}
	return predmetak + strconv.Itoa(najveci+1)
}
