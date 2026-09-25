package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// KisomjerService uređuje registar točaka kvazi-kišomjera i slivova.
// Uređuje ga globalni administrator, kao i registar vodotoka; čitaju ga svi
// koji vide registre, a ponajprije preuzimanje oborina za prognozu.
type KisomjerService struct {
	repo *repository.KisomjerRepository
}

func NewKisomjerService(repo *repository.KisomjerRepository) *KisomjerService {
	return &KisomjerService{repo: repo}
}

func (s *KisomjerService) ListKisomjeri(ctx context.Context) ([]models.Kisomjer, error) {
	return s.repo.ListKisomjeri(ctx)
}

func (s *KisomjerService) GetKisomjer(ctx context.Context, code string) (*models.Kisomjer, error) {
	return s.repo.GetKisomjer(ctx, code)
}

func (s *KisomjerService) ListSlivovi(ctx context.Context) ([]models.Sliv, error) {
	return s.repo.ListSlivovi(ctx)
}

// CreateKisomjer upisuje novu točku; bez šifre izvodi je iz sliva i naziva
func (s *KisomjerService) CreateKisomjer(ctx context.Context, perms *models.UserPermissions, k *models.Kisomjer) error {
	if err := requireGlobalAdmin(perms, "upis kišomjera"); err != nil {
		return err
	}
	k.Code = strings.TrimSpace(strings.ToLower(k.Code))
	if k.Code == "" {
		k.Code = Slugify(strings.TrimSpace(k.Sliv + " " + k.Naziv))
	}
	if err := validateKisomjer(k); err != nil {
		return err
	}
	if postojeci, err := s.repo.GetKisomjer(ctx, k.Code); err != nil {
		return err
	} else if postojeci != nil {
		return fmt.Errorf("kišomjer sa šifrom %q već postoji", k.Code)
	}
	return s.repo.CreateKisomjer(ctx, k)
}

// UpdateKisomjer mijenja točku
func (s *KisomjerService) UpdateKisomjer(ctx context.Context, perms *models.UserPermissions, k *models.Kisomjer) error {
	if err := requireGlobalAdmin(perms, "izmjenu kišomjera"); err != nil {
		return err
	}
	k.Code = strings.TrimSpace(strings.ToLower(k.Code))
	if err := validateKisomjer(k); err != nil {
		return err
	}
	postojeci, err := s.repo.GetKisomjer(ctx, k.Code)
	if err != nil {
		return err
	}
	if postojeci == nil {
		return fmt.Errorf("kišomjer %q ne postoji", k.Code)
	}
	k.CreatedAt = postojeci.CreatedAt
	return s.repo.UpdateKisomjer(ctx, k)
}

// DeleteKisomjer briše točku
func (s *KisomjerService) DeleteKisomjer(ctx context.Context, perms *models.UserPermissions, code string) error {
	if err := requireGlobalAdmin(perms, "brisanje kišomjera"); err != nil {
		return err
	}
	return s.repo.DeleteKisomjer(ctx, strings.TrimSpace(code))
}

// Polozaj je novi položaj jedne točke, kako dođe s karte
type Polozaj struct {
	Code      string  `json:"code"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
}

// PomakniKisomjere sprema položaje točaka premještenih na karti. Mijenja
// samo koordinate; sve ostalo na točki ostaje.
func (s *KisomjerService) PomakniKisomjere(ctx context.Context, perms *models.UserPermissions, polozaji []Polozaj) (int, error) {
	if err := requireGlobalAdmin(perms, "premještanje kišomjera"); err != nil {
		return 0, err
	}
	n := 0
	for _, p := range polozaji {
		k, err := s.repo.GetKisomjer(ctx, strings.TrimSpace(p.Code))
		if err != nil {
			return n, err
		}
		if k == nil {
			return n, fmt.Errorf("kišomjer %q ne postoji", p.Code)
		}
		if !koordinateValjane(p.Latitude, p.Longitude) {
			return n, fmt.Errorf("kišomjer %q: koordinate %.5f, %.5f nisu valjane", p.Code, p.Latitude, p.Longitude)
		}
		if math.Abs(k.Latitude-p.Latitude) < 1e-7 && math.Abs(k.Longitude-p.Longitude) < 1e-7 {
			continue
		}
		k.Latitude, k.Longitude = round6(p.Latitude), round6(p.Longitude)
		if err := s.repo.UpdateKisomjer(ctx, k); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// UpsertSliv upisuje ili mijenja sliv
func (s *KisomjerService) UpsertSliv(ctx context.Context, perms *models.UserPermissions, m *models.Sliv) error {
	if err := requireGlobalAdmin(perms, "upis sliva"); err != nil {
		return err
	}
	m.Oznaka = strings.TrimSpace(m.Oznaka)
	m.Naziv = strings.TrimSpace(m.Naziv)
	if m.Oznaka == "" || m.Naziv == "" {
		return fmt.Errorf("sliv mora imati oznaku i naziv")
	}
	return s.repo.UpsertSliv(ctx, m)
}

// DeleteSliv briše sliv
func (s *KisomjerService) DeleteSliv(ctx context.Context, perms *models.UserPermissions, oznaka string) error {
	if err := requireGlobalAdmin(perms, "brisanje sliva"); err != nil {
		return err
	}
	return s.repo.DeleteSliv(ctx, strings.TrimSpace(oznaka))
}

func validateKisomjer(k *models.Kisomjer) error {
	k.Naziv = strings.TrimSpace(k.Naziv)
	k.Sliv = strings.TrimSpace(k.Sliv)
	k.Pojas = strings.TrimSpace(k.Pojas)
	k.Napomena = strings.TrimSpace(k.Napomena)
	if k.Code == "" {
		return fmt.Errorf("šifra kišomjera je obavezna")
	}
	if k.Naziv == "" {
		return fmt.Errorf("naziv kišomjera je obavezan")
	}
	if !koordinateValjane(k.Latitude, k.Longitude) {
		return fmt.Errorf("kišomjer mora imati valjane koordinate")
	}
	if k.Tezina != nil && (*k.Tezina < 0 || *k.Tezina > 1) {
		return fmt.Errorf("težina mora biti između 0 i 1")
	}
	k.Latitude, k.Longitude = round6(k.Latitude), round6(k.Longitude)
	return nil
}

// koordinateValjane prima samo točke u Europi oko sliva Dunava: sve drugo je
// zamjena širine i dužine ili tipkarska pogreška.
func koordinateValjane(lat, lon float64) bool {
	return lat >= 40 && lat <= 52 && lon >= 8 && lon <= 24
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
