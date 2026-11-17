package service

import (
	"context"
	"errors"

	"gocop/internal/models"
)

// ObrisiPrazanPopis briše nacrt inventure u kojem ništa nije upisano: ni
// prebrojano ni potreba. Smije onaj tko smije pisati u skladište, i samo dok
// je nacrt prazan (Odluke 16. 11. 2026., 10); zaključena inventura i nacrt s
// upisom ostaju. U knjizi verzija nacrt ostaje kao obrisan.
func (s *MtsService) ObrisiPrazanPopis(ctx context.Context, u *models.User, perms *models.UserPermissions, id string) (*models.Popis, error) {
	p, err := s.repo.GetPopis(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("Inventura nije pronađena.")
	}
	sk, err := s.repo.GetSkladiste(ctx, p.SkladisteID)
	if err != nil {
		return nil, err
	}
	if u == nil || sk == nil || !s.SmijePisati(perms, sk) {
		return nil, errors.New("Nacrt inventure briše skladištar ili uprava branjenog područja i sektora.")
	}
	if p.Zakljucen() {
		return nil, errors.New("Zaključena inventura se ne briše; razlike su proknjižene.")
	}
	if !p.Prazan() {
		return nil, errors.New("Briše se samo prazan nacrt; u ovom je već nešto upisano.")
	}
	return p, s.repo.ArhivirajPopis(ctx, p)
}
