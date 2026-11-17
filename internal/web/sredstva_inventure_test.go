package web

import (
	"testing"

	"gocop/internal/models"
)

// Godine inventura idu od najnovije, a odabrana godina filtrira popis.
func TestPopisiGodine(t *testing.T) {
	sve := []models.Popis{{ID: "a", Godina: 2025}, {ID: "b", Godina: 2026}, {ID: "c", Godina: 2025}}
	godine, popisi := popisiGodine(sve, 0)
	if len(godine) != 2 || godine[0] != 2026 || godine[1] != 2025 || len(popisi) != 3 {
		t.Errorf("sve: %v %d", godine, len(popisi))
	}
	godine, popisi = popisiGodine(sve, 2025)
	if len(godine) != 2 || len(popisi) != 2 || popisi[0].ID != "a" || popisi[1].ID != "c" {
		t.Errorf("2025: %v %+v", godine, popisi)
	}
}
