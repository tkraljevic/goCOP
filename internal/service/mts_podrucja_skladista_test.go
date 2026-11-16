package service

import (
	"testing"

	"gocop/internal/models"
)

// Novo skladište upisuje uprava sektora u bilo koje područje, a rukovoditelj
// branjenog područja samo u svoje; tko nema ni jedno, ne upisuje.
func TestPodrucjaZaSkladista(t *testing.T) {
	s := &MtsService{}
	podrucja := []models.Area{{ID: 34, SectorID: "B"}, {ID: 35, SectorID: "B"}}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}}
	rukovoditelj := &models.UserPermissions{AdminAreas: map[int]bool{35: true}}
	promatrac := &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}}

	if got := s.PodrucjaZaSkladista(uprava, "B", podrucja); len(got) != 2 {
		t.Errorf("uprava sektora: %v", got)
	}
	if got := s.PodrucjaZaSkladista(rukovoditelj, "B", podrucja); len(got) != 1 || got[0].ID != 35 {
		t.Errorf("rukovoditelj područja: %v", got)
	}
	if !s.SmijeDodatiSkladiste(rukovoditelj, "B", podrucja) || !s.SmijeDodatiSkladiste(uprava, "B", nil) {
		t.Error("uprava i rukovoditelj područja smiju dodati skladište")
	}
	if s.SmijeDodatiSkladiste(promatrac, "B", podrucja) || s.SmijeDodatiSkladiste(nil, "B", podrucja) {
		t.Error("promatrač i neprijavljeni ne dodaju skladište")
	}
}
