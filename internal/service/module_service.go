package service

import (
	"context"
	"fmt"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// ModuleService: što koji račun vidi. Čita se pri svakom zahtjevu (dvije
// male tablice), mijenja samo globalni administrator.
type ModuleService struct {
	repo *repository.ModuleRepository
}

func NewModuleService(repo *repository.ModuleRepository) *ModuleService {
	return &ModuleService{repo: repo}
}

// Visibility slaže skup vidljivih modula za račun
func (s *ModuleService) Visibility(ctx context.Context, u *models.User, perms *models.UserPermissions) (models.Visibility, error) {
	if u == nil {
		return nil, nil
	}
	rules, err := s.repo.RoleRules(ctx)
	if err != nil {
		return nil, err
	}
	override, err := s.repo.UserOverride(ctx, u.ID.String())
	if err != nil {
		return nil, err
	}
	return models.ResolveModules(u, perms, rules, override), nil
}

// RoleRow je jedan redak tablice uloge × moduli
type RoleRow struct {
	Role    models.Role
	Label   string
	Modules map[string]bool
	Custom  bool // administrator je promijenio zadano
}

// RoleMatrix vraća stvarna pravila za sve uloge koje obrazac nudi
func (s *ModuleService) RoleMatrix(ctx context.Context, roles []models.Role) ([]RoleRow, error) {
	rules, err := s.repo.RoleRules(ctx)
	if err != nil {
		return nil, err
	}
	var out []RoleRow
	for _, r := range roles {
		row := RoleRow{Role: r, Label: r.Label(), Modules: map[string]bool{}}
		mods, ok := rules[string(r)]
		if !ok {
			mods = models.DefaultModules(r)
		}
		row.Custom = ok && models.JoinModules(mods) != models.JoinModules(models.DefaultModules(r))
		for _, m := range mods {
			row.Modules[m] = true
		}
		out = append(out, row)
	}
	return out, nil
}

// SetRoleRule sprema pravilo za ulogu; smije samo globalni administrator
func (s *ModuleService) SetRoleRule(ctx context.Context, perms *models.UserPermissions, role models.Role, mods []string) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("vidljivost modula mijenja samo globalni administrator")
	}
	if role.Label() == "" {
		return fmt.Errorf("nepoznata uloga %s", role)
	}
	return s.repo.SetRoleRule(ctx, string(role), mods)
}

// UserOverride vraća iznimku računa (nil kad je nema)
func (s *ModuleService) UserOverride(ctx context.Context, userID string) (*models.UserModules, error) {
	return s.repo.UserOverride(ctx, userID)
}

// SetUserOverride sprema iznimku računa; smije samo globalni administrator
func (s *ModuleService) SetUserOverride(ctx context.Context, perms *models.UserPermissions, userID string, shown, hidden []string) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return fmt.Errorf("iznimke po računu mijenja samo globalni administrator")
	}
	return s.repo.SetUserOverride(ctx, userID, shown, hidden)
}
