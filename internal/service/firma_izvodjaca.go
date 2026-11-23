package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Dužnost rukovoditelja obrane izvođača za branjeno područje i zamjenika
// (Odluke (22. 11. 2026.), pitanje 4). Program je dopušta samo kad račun
// ima vrstu organizacije „pravna osoba” i upisanu firmu, a ta je firma u
// registru licenciranih firmi aktivna i vezana na branjeno područje
// dužnosti. Inače kaže što nedostaje, npr. „Firma Primjer nije vezana na
// BP 34.”

// SetFirme daje servisu registar licenciranih firmi s vezama na područja
// (repository.OrgRepository.ListContractors)
func (s *UserService) SetFirme(f func(ctx context.Context) ([]models.Contractor, error)) { s.firme = f }

// errBezRegistraFirmi: servis nema registra firmi, pa ne zna je li firma
// vezana na područje
var errBezRegistraFirmi = errors.New("registar licenciranih firmi nije dostupan, pa se ne vidi je li firma vezana na područje")

// dajeUloguRacunu je mayGrant uz provjeru firme izvođača (firmaRacuna) za
// račun koji daje racun
func (s *UserService) dajeUloguRacunu(actor *models.UserPermissions, racun func() (*models.User, error), role models.Role, sectorID *string, areaID *int, sectors areaSector) error {
	if err := mayGrant(actor, role, sectorID, areaID, sectors); err != nil {
		return err
	}
	return s.firmaRacuna(racun, role, areaID)
}

// mijenjaDuznost je mayChange uz provjeru firme izvođača kad izmjena daje
// novu ulogu ili doseg; ista dužnost se ne provjerava ponovno, da joj se
// rok i naziv mogu urediti i kad se firma u međuvremenu promijenila
func (s *UserService) mijenjaDuznost(actor *models.UserPermissions, dosad *models.Duty, role models.Role, sectorID *string, areaID *int, sectionCodes string, sectors areaSector) error {
	if err := mayChange(actor, dosad, role, sectorID, areaID, sectionCodes, sectors); err != nil {
		return err
	}
	if istaDuznost(dosad, role, sectorID, areaID, sectionCodes) {
		return nil
	}
	return s.firmaRacuna(s.racunPoOznaci(dosad.UserID), role, areaID)
}

// racunPoOznaci čita račun osobe kad ga provjera zatraži
func (s *UserService) racunPoOznaci(id uuid.UUID) func() (*models.User, error) {
	return func() (*models.User, error) { return s.userRepo.GetUserByID(id) }
}

// firmaRacuna provjerava firmu računa za ulogu obrane izvođača na području
// areaID (firmaZaObranu); ostale uloge ne traže ništa
func (s *UserService) firmaRacuna(racun func() (*models.User, error), role models.Role, areaID *int) error {
	if !role.ObranaIzvodjaca() || areaID == nil {
		return nil
	}
	u, err := racun()
	if err != nil {
		return err
	}
	if u == nil {
		return ErrUserNotFound
	}
	if s.firme == nil {
		return fmt.Errorf("%w: %w", ErrInvalidUserData, errBezRegistraFirmi)
	}
	firme, err := s.firme(context.Background())
	if err != nil {
		return fmt.Errorf("registar licenciranih firmi nije pročitan: %w", err)
	}
	return firmaZaObranu(u, role, *areaID, firme)
}

// firmaZaObranu: račun pravne osobe s firmom (racunPravneOsobe), firma je
// u registru, aktivna i vezana na područje. Isti naziv može imati više
// zapisa u registru (npr. dvaput upisana firma); dovoljan je jedan koji
// ispunjava sve.
func firmaZaObranu(u *models.User, role models.Role, podrucje int, firme []models.Contractor) error {
	if err := racunPravneOsobe(u, role); err != nil {
		return err
	}
	nadjene := models.FirmeRacuna(u, firme)
	if len(nadjene) == 0 {
		return fmt.Errorf("%w: firma %s s računa nije u registru licenciranih firmi", ErrInvalidUserData, strings.TrimSpace(u.OrgName))
	}
	aktivna := ""
	for _, c := range nadjene {
		if c.Active && c.VezanaNa(podrucje) {
			return nil
		}
		if c.Active {
			aktivna = c.Label()
		}
	}
	if aktivna == "" {
		return fmt.Errorf("%w: firma %s nije aktivna u registru licenciranih firmi", ErrInvalidUserData, nadjene[0].Label())
	}
	return fmt.Errorf("%w: firma %s nije vezana na %s %d", ErrInvalidUserData, aktivna, models.Terms().AreaShort, podrucje)
}

// racunPravneOsobe: na računu je vrsta organizacije „pravna osoba” i
// upisana firma
func racunPravneOsobe(u *models.User, role models.Role) error {
	switch {
	case u.OrgType != models.OrgPravnaOsoba:
		return fmt.Errorf("%w: ulogu „%s” dobiva samo račun s vrstom organizacije „%s”; promijenite je na računu osobe i upišite firmu",
			ErrInvalidUserData, role.Label(), models.OrgPravnaOsoba.Label())
	case strings.TrimSpace(u.OrgName) == "":
		return fmt.Errorf("%w: na računu osobe nije upisana firma, a ulogu „%s” dobiva samo osoba iz firme vezane na područje",
			ErrInvalidUserData, role.Label())
	}
	return nil
}
