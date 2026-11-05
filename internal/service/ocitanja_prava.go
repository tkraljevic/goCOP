package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
)

// Prava na očitanja letve postaje (Odluke (8. 10. 2026.), pitanje 8).
// Sektor letve dolazi preko njezinih dionica (dionica → područje → sektor) i
// iz upisanog praćenja (Station.PrateSektori, PratePodrucja). Očitanje
// upisuje svatko s valjanom dužnošću u sektoru koji letvu prati, i Dežurni
// operater u COP-u, ali ne Gost ni Preglednik: po tome je šire od ranijeg
// pravila („tko god prolazi pored letve”). Letva kojoj sektor još nije
// upisan, ni preko dionica, prima očitanje od svakoga tko igdje upisuje,
// da noćni upis dežurnog ne zapne. Pravilo vrijedi na čvorovima od 0.0.35;
// čvor na 0.0.34 zadržava staro (tko smije uređivati dionicu letve, a na
// letvu bez dionica svatko tko upisuje), a očitanja s novog čvora prima
// razmjenom kao i dosad.

// dosegLetve su dionice letve i sektori koji je prate
type dosegLetve struct {
	sifre      []string          // šifre dionica letve
	dionice    []*models.Section // dionice letve koje su u registru
	sektori    []string          // svi sektori koji letvu prate: preko dionica i upisani
	bezSektora bool              // letva bez dionica i bez upisanog praćenja
}

// dosegLetve skuplja dionice letve i sektore koji je prate
func (s *ReadingService) dosegLetve(st *models.Station) dosegLetve {
	d := dosegLetve{sifre: st.SectionCodes, bezSektora: len(st.SectionCodes) == 0 && !st.ImaPracenje()}
	for _, code := range st.SectionCodes {
		if sec, err := s.sectionService.GetSectionWithDetails(code); err == nil && sec != nil {
			d.dionice = append(d.dionice, sec)
			d.sektori = append(d.sektori, sec.SectorID)
		}
	}
	d.sektori = append(d.sektori, st.SektoriPracenja(s.sektoriPodrucja(st))...)
	return d
}

// sektoriPodrucja su sektori područja iz registra (područje → sektor), samo
// kad ih upisano praćenje letve treba
func (s *ReadingService) sektoriPodrucja(st *models.Station) map[int]string {
	if len(st.PratePodrucja) == 0 || s.userService == nil {
		return nil
	}
	podrucja, _ := s.userService.ListAreas("")
	out := make(map[int]string, len(podrucja))
	for _, a := range podrucja {
		out[a.ID] = a.SectorID
	}
	return out
}

// upisujeNaLetvu: valjana dužnost s pravom upisa u sektoru koji letvu prati
// (RadiUSektoru), ili uprava tog sektora; letva bez sektora prima očitanje
// od svakoga tko igdje upisuje
func upisujeNaLetvu(perms *models.UserPermissions, d dosegLetve) bool {
	if d.bezSektora {
		return hasAnyWriteRight(perms)
	}
	for _, sektor := range d.sektori {
		if perms.RadiUSektoru(sektor) || perms.AdminSectors[sektor] {
			return true
		}
	}
	return false
}

// pravoPremaDionicama je dosadašnje pravilo upisa na letvu s dionicama: tko
// smije uređivati bar jednu njezinu dionicu. Ostaje uz novo, da ono ne bude
// uže od staroga ni kad dužnosti nemaju upisan sektor.
func (s *ReadingService) pravoPremaDionicama(perms *models.UserPermissions, d dosegLetve) bool {
	for _, code := range d.sifre {
		if perms.AllowedSections[code] {
			return true
		}
	}
	for _, sec := range d.dionice {
		if s.sectionService.CanEditSection(perms, sec) {
			return true
		}
	}
	return false
}

// Ispravak i brisanje očitanja letve postaje (Odluke (8. 10. 2026.),
// pitanje 9). Briše, i tuđe očitanje ispravlja, samo onaj tko odgovara za
// letvu: dužnost na njezinoj dionici, uprava njezina područja ili sektora i
// razina 1; za letvu bez dionica uprava sektora koji je prati i razina 1.
// Autor svoje očitanje ispravlja (ne briše) dok upisuje na letvu, a kad
// izgubi dužnost u njezinu sektoru, još RokIspravkaOcitanja od upisa.

// RokIspravkaOcitanja je koliko dugo od upisa autor smije ispraviti svoje
// očitanje i kad više nema dužnost u sektoru letve
const RokIspravkaOcitanja = 72 * time.Hour

// PravaLetve je ono što osoba smije s očitanjima jedne letve postaje
type PravaLetve struct {
	Upisuje  bool // upisuje novo očitanje (CanRecordStation)
	Odgovara bool // odgovara za letvu: briše i ispravlja i tuđa očitanja
	// UzIzvor: letva nije u sektoru osobe, ali osoba igdje upisuje očitanja,
	// pa smije upisati jedno očitanje kad navede tko ga je očitao i kako je
	// vrijednost dobivena (npr. telefonom kad telemetrija ne radi; Odluke
	// 5. 11. 2026.). Uvoz niza, ispravak i brisanje tuđih ostaju sektoru.
	UzIzvor bool
	osoba   string // tko pita, za vlastita očitanja
}

// PravaNaLetvu slaže prava osobe na očitanja letve postaje; bez osobe ili
// letve nema nijednog
func (s *ReadingService) PravaNaLetvu(perms *models.UserPermissions, st *models.Station) PravaLetve {
	if perms == nil || st == nil {
		return PravaLetve{}
	}
	if perms.IsGlobalAdmin {
		return PravaLetve{Upisuje: true, Odgovara: true, osoba: perms.User.ID.String()}
	}
	d := s.dosegLetve(st)
	p := PravaLetve{Odgovara: odgovaraZaLetvu(perms, d), osoba: perms.User.ID.String()}
	p.Upisuje = p.Odgovara || upisujeNaLetvu(perms, d) || s.pravoPremaDionicama(perms, d)
	p.UzIzvor = !p.Upisuje && hasAnyWriteRight(perms)
	return p
}

// ErrTrebaIzvor: tuđa letva, a izvor očitanja nije upisan
var ErrTrebaIzvor = errors.New("ova letva nije u vašem sektoru: upišite tko je očitao i kako ste dobili vrijednost, npr. „vodočuvar, telefonom”")

// MozeUpisati javlja smije li osoba upisati jedno očitanje na letvu, svoju
// ili tuđu uz izvor (PravaLetve.UzIzvor)
func (p PravaLetve) MozeUpisati() bool { return p.Upisuje || p.UzIzvor }

// MozeIspraviti javlja smije li osoba ispraviti očitanje u trenutku sad: tko
// odgovara za letvu svako, autor svoje dok upisuje na letvu, a nakon toga
// još RokIspravkaOcitanja od upisa
func (p PravaLetve) MozeIspraviti(rd *models.Reading, sad time.Time) bool {
	if p.Odgovara {
		return true
	}
	if !p.autor(rd) {
		return false
	}
	return p.Upisuje || sad.Sub(rd.CreatedAt) < RokIspravkaOcitanja
}

// MozeObrisati javlja smije li osoba obrisati očitanje letve: samo tko za
// nju odgovara, i kad je očitanje njegovo
func (p PravaLetve) MozeObrisati() bool { return p.Odgovara }

// autor javlja je li očitanje upisala ova osoba
func (p PravaLetve) autor(rd *models.Reading) bool {
	return rd != nil && rd.UserID != "" && rd.UserID == p.osoba
}

// odgovaraZaLetvu: dužnost na dionici letve ili uprava njezina područja ili
// sektora; letva bez dionica: uprava sektora koji je prati. Razinu 1
// propušta već PravaNaLetvu.
func odgovaraZaLetvu(perms *models.UserPermissions, d dosegLetve) bool {
	if len(d.sifre) > 0 {
		return odgovaraZaDionicu(perms, d.dionice)
	}
	for _, sektor := range d.sektori {
		if perms.AdminSectors[sektor] {
			return true
		}
	}
	return false
}

// odgovaraZaDionicu: dužnost na ijednoj od dionica (navodi je, ili dužnost
// dionica bez popisa pokriva cijelo njezino područje) ili uprava njezina
// područja ili sektora
func odgovaraZaDionicu(perms *models.UserPermissions, dionice []*models.Section) bool {
	for _, sec := range dionice {
		if perms.AllowedSections[sec.Code] || perms.RadiNaDionici(sec.AreaID, sec.Code) ||
			perms.CanAdminister(sec.SectorID, sec.AreaID) {
			return true
		}
	}
	return false
}

// pravoNaUpisPostaje: osoba smije upisati očitanje na letvu postaje kad je
// letva njezina, ili kad je tuđa a izvor je upisan (Observer)
func (s *ReadingService) pravoNaUpisPostaje(ctx context.Context, perms *models.UserPermissions, rd *models.Reading) (*models.Station, error) {
	st, err := s.postajaZaUpis(ctx, rd.StationID)
	if err != nil {
		return nil, err
	}
	p := s.PravaNaLetvu(perms, st)
	switch {
	case p.Upisuje:
		return st, nil
	case p.UzIzvor && strings.TrimSpace(rd.Observer) != "":
		return st, nil
	case p.UzIzvor:
		return nil, ErrTrebaIzvor
	}
	return nil, fmt.Errorf("nemate pravo upisivati očitanja na %s", st.Name)
}
