package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// OrgService vodi registar organizacije obrane: sektore i branjena područja.
// Uređuje ih samo globalni administrator, jer iz njih slijede ovlasti svih
// ostalih računa.
type OrgService struct {
	repo *repository.OrgRepository
	sse  *SSEBroker
}

func NewOrgService(repo *repository.OrgRepository, sse *SSEBroker) *OrgService {
	return &OrgService{repo: repo, sse: sse}
}

// reSectorID: oznaka sektora je slovo ili kratka riječ velikim slovima (A, B, DIREKCIJA)
var reSectorID = regexp.MustCompile(`^[A-ZČĆŠĐŽ][A-ZČĆŠĐŽ0-9_-]{0,19}$`)

func (s *OrgService) ListSectors(ctx context.Context) ([]models.Sector, error) {
	return s.repo.ListSectors(ctx)
}

func (s *OrgService) GetSector(ctx context.Context, id string) (*models.Sector, error) {
	return s.repo.GetSector(ctx, strings.ToUpper(strings.TrimSpace(id)))
}

func (s *OrgService) ListAreas(ctx context.Context, sectorID string) ([]models.Area, error) {
	return s.repo.ListAreas(ctx, sectorID)
}

func (s *OrgService) GetArea(ctx context.Context, id int) (*models.Area, error) {
	return s.repo.GetArea(ctx, id)
}

// Terms vraća nazive razina ustroja
func (s *OrgService) Terms(ctx context.Context) (models.OrgTerms, error) {
	return s.repo.GetTerms(ctx)
}

// SaveTerms mijenja nazive razina; vrijede na svim čvorovima mreže
func (s *OrgService) SaveTerms(ctx context.Context, perms *models.UserPermissions, t models.OrgTerms) error {
	if err := requireGlobalAdmin(perms, "uređivanje naziva razina"); err != nil {
		return err
	}
	if err := s.repo.SaveTerms(ctx, t); err != nil {
		return err
	}
	s.sse.Broadcast("organization_changed", "Nazivi razina ustroja su promijenjeni", models.TermsID)
	return nil
}

// SaveSector upisuje sektor; nov traži slobodnu oznaku, postojeći se mijenja
func (s *OrgService) SaveSector(ctx context.Context, perms *models.UserPermissions, sec *models.Sector, isNew bool) error {
	if err := requireGlobalAdmin(perms, "uređivanje sektora"); err != nil {
		return err
	}
	sec.ID = strings.ToUpper(strings.TrimSpace(sec.ID))
	sec.Name, sec.VgoName, sec.CenterCop = strings.TrimSpace(sec.Name), strings.TrimSpace(sec.VgoName), strings.TrimSpace(sec.CenterCop)
	if !reSectorID.MatchString(sec.ID) {
		return fmt.Errorf("oznaka sektora je slovo ili kratka riječ velikim slovima, npr. B ili DIREKCIJA")
	}
	if sec.Name == "" {
		return fmt.Errorf("naziv jedinice je obavezan")
	}
	if sec.Level != 1 {
		sec.Level = 2
	}
	existing, err := s.repo.GetSector(ctx, sec.ID)
	if err != nil {
		return err
	}
	if isNew && existing != nil {
		return fmt.Errorf("sektor %s već postoji", sec.ID)
	}
	if !isNew && existing == nil {
		return fmt.Errorf("sektor %s ne postoji", sec.ID)
	}
	if err := s.repo.SaveSector(ctx, sec); err != nil {
		return err
	}
	s.sse.Broadcast("organization_changed", "Sektor "+sec.ID+": "+sec.Name, sec.ID)
	return nil
}

// DeleteSector briše sektor na koji se ništa ne veže
func (s *OrgService) DeleteSector(ctx context.Context, perms *models.UserPermissions, id string) error {
	if err := requireGlobalAdmin(perms, "uređivanje sektora"); err != nil {
		return err
	}
	id = strings.ToUpper(strings.TrimSpace(id))
	n, err := s.repo.SectorInUse(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("sektor %s se ne može obrisati: na njega se veže %d branjenih područja, dionica ili zaduženja", id, n)
	}
	if err := s.repo.DeleteSector(ctx, id); err != nil {
		return err
	}
	s.sse.Broadcast("organization_changed", "Sektor "+id+" obrisan", id)
	return nil
}

// SaveArea upisuje branjeno područje; broj je njegov identitet u cijeloj mreži
func (s *OrgService) SaveArea(ctx context.Context, perms *models.UserPermissions, a *models.Area, isNew bool) error {
	if err := requireGlobalAdmin(perms, "uređivanje branjenih područja"); err != nil {
		return err
	}
	a.SectorID = strings.ToUpper(strings.TrimSpace(a.SectorID))
	a.Name, a.VgiName = strings.TrimSpace(a.Name), strings.TrimSpace(a.VgiName)
	a.Subcenter, a.ContractorName = strings.TrimSpace(a.Subcenter), strings.TrimSpace(a.ContractorName)
	if a.ID <= 0 {
		return fmt.Errorf("broj branjenog područja je obavezan i veći od nule")
	}
	if a.Name == "" {
		return fmt.Errorf("naziv branjenog područja je obavezan")
	}
	sector, err := s.repo.GetSector(ctx, a.SectorID)
	if err != nil {
		return err
	}
	if sector == nil {
		return fmt.Errorf("sektor %q ne postoji; prvo upišite sektor", a.SectorID)
	}
	existing, err := s.repo.GetArea(ctx, a.ID)
	if err != nil {
		return err
	}
	if isNew && existing != nil {
		return fmt.Errorf("branjeno područje %d već postoji (%s)", a.ID, existing.Name)
	}
	if !isNew && existing == nil {
		return fmt.Errorf("branjeno područje %d ne postoji", a.ID)
	}
	if err := s.repo.SaveArea(ctx, a); err != nil {
		return err
	}
	s.sse.Broadcast("organization_changed", fmt.Sprintf("Branjeno područje %d: %s", a.ID, a.Name), fmt.Sprint(a.ID))
	return nil
}

// DeleteArea briše branjeno područje na koje se ništa ne veže
func (s *OrgService) DeleteArea(ctx context.Context, perms *models.UserPermissions, id int) error {
	if err := requireGlobalAdmin(perms, "uređivanje branjenih područja"); err != nil {
		return err
	}
	n, err := s.repo.AreaInUse(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("branjeno područje %d se ne može obrisati: na njega se veže %d dionica ili zaduženja", id, n)
	}
	if err := s.repo.DeleteArea(ctx, id); err != nil {
		return err
	}
	s.sse.Broadcast("organization_changed", fmt.Sprintf("Branjeno područje %d obrisano", id), fmt.Sprint(id))
	return nil
}

// ListContractors vraća registar licenciranih firmi s mjestima rada
func (s *OrgService) ListContractors(ctx context.Context) ([]models.Contractor, error) {
	return s.repo.ListContractors(ctx)
}

// GetContractor vraća jednu firmu; nil kad je nema
func (s *OrgService) GetContractor(ctx context.Context, id string) (*models.Contractor, error) {
	return s.repo.GetContractor(ctx, id)
}

// ContractorIndex je tko gdje radi, za tablice ustroja
func (s *OrgService) ContractorIndex(ctx context.Context) (repository.ContractorIndex, error) {
	return s.repo.ContractorIndex(ctx)
}

// SaveContractor upisuje firmu i postavlja gdje radi; veze na nepostojeći
// sektor ili područje se odbijaju
func (s *OrgService) SaveContractor(ctx context.Context, perms *models.UserPermissions, c *models.Contractor, where []models.ContractorAssignment) error {
	if err := requireGlobalAdmin(perms, "uređivanje licenciranih firmi"); err != nil {
		return err
	}
	c.Name = strings.TrimSpace(c.Name)
	c.ShortName, c.OIB = strings.TrimSpace(c.ShortName), strings.TrimSpace(c.OIB)
	c.Address, c.Phone, c.Email = strings.TrimSpace(c.Address), strings.TrimSpace(c.Phone), strings.TrimSpace(c.Email)
	c.Contact, c.Notes = strings.TrimSpace(c.Contact), strings.TrimSpace(c.Notes)
	if c.Name == "" {
		return fmt.Errorf("naziv firme je obavezan")
	}
	if c.OIB != "" && !reOIB.MatchString(c.OIB) {
		return fmt.Errorf("OIB ima 11 znamenki")
	}
	areas, err := s.repo.ListAreas(ctx, "")
	if err != nil {
		return err
	}
	sectorOf := map[int]string{}
	for _, a := range areas {
		sectorOf[a.ID] = a.SectorID
	}
	seen := map[string]bool{}
	var clean []models.ContractorAssignment
	for _, w := range where {
		w.SectorID = strings.ToUpper(strings.TrimSpace(w.SectorID))
		if w.AreaID > 0 {
			sec, ok := sectorOf[w.AreaID]
			if !ok {
				return fmt.Errorf("branjeno područje %d ne postoji", w.AreaID)
			}
			w.SectorID = sec
		} else {
			sector, err := s.repo.GetSector(ctx, w.SectorID)
			if err != nil {
				return err
			}
			if sector == nil {
				return fmt.Errorf("sektor %q ne postoji", w.SectorID)
			}
		}
		if seen[w.Key()] {
			continue
		}
		seen[w.Key()] = true
		clean = append(clean, w)
	}
	if err := s.repo.SaveContractor(ctx, c, clean); err != nil {
		return err
	}
	s.sse.Broadcast("organization_changed", "Licencirana firma: "+c.Name, c.ID)
	return nil
}

// DeleteContractor briše firmu i gdje radi
func (s *OrgService) DeleteContractor(ctx context.Context, perms *models.UserPermissions, id string) error {
	if err := requireGlobalAdmin(perms, "uređivanje licenciranih firmi"); err != nil {
		return err
	}
	if err := s.repo.DeleteContractor(ctx, id); err != nil {
		return err
	}
	s.sse.Broadcast("organization_changed", "Licencirana firma obrisana", id)
	return nil
}

// reOIB: osobni identifikacijski broj ima 11 znamenki
var reOIB = regexp.MustCompile(`^[0-9]{11}$`)
