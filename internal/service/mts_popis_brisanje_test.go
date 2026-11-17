package service

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Prazan nacrt inventure briše onaj tko smije pisati u skladište, i samo dok
// je prazan (Odluke 16. 11. 2026., 10).
func TestObrisiPrazanPopis(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	dan := time.Date(2025, 12, 31, 0, 0, 0, 0, models.Zagreb)
	prazan := &models.Popis{SkladisteID: sk.ID, Dan: dan}
	if err := s.SpremiPopis(ctx, u, uprava, prazan); err != nil {
		t.Fatal(err)
	}
	upisan := &models.Popis{SkladisteID: sk.ID, Dan: dan.AddDate(0, 0, -1), Stavke: []models.PopisnaStavka{{VrstaID: "lopata", Oblik: models.OblikOsnovni, Utvrdjeno: 3}}}
	if err := s.SpremiPopis(ctx, u, uprava, upisan); err != nil {
		t.Fatal(err)
	}
	for _, k := range []struct {
		ime    string
		u      *models.User
		perms  *models.UserPermissions
		id     string
		greska string
	}{
		{"nepostojeća", u, uprava, "nema-je", "nije pronađena"},
		{"bez prijave", nil, uprava, prazan.ID, "briše skladištar"},
		{"drugi sektor", u, &models.UserPermissions{AdminSectors: map[string]bool{"F": true}}, prazan.ID, "briše skladištar"},
		{"s upisom", u, uprava, upisan.ID, "samo prazan nacrt"},
	} {
		if _, err := s.ObrisiPrazanPopis(ctx, k.u, k.perms, k.id); err == nil || !strings.Contains(err.Error(), k.greska) {
			t.Errorf("%s: %v", k.ime, err)
		}
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, upisan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ObrisiPrazanPopis(ctx, u, uprava, upisan.ID); err == nil || !strings.Contains(err.Error(), "Zaključena") {
		t.Errorf("zaključena: %v", err)
	}

	p, err := s.ObrisiPrazanPopis(ctx, u, uprava, prazan.ID)
	if err != nil || p.ID != prazan.ID {
		t.Fatalf("prazan nacrt: %v", err)
	}
	if ostao, err := s.Popis(ctx, prazan.ID); err != nil || ostao != nil {
		t.Errorf("nacrt je ostao: %+v %v", ostao, err)
	}
	// isti dan se može otvoriti iznova
	if err := s.SpremiPopis(ctx, u, uprava, &models.Popis{SkladisteID: sk.ID, Dan: dan}); err != nil {
		t.Errorf("novi nacrt na isti dan: %v", err)
	}
}
