package web

import (
	"testing"

	"gocop/internal/models"
)

// Sažetak inventure broji retke s količinom, manjkom i viškom, a skupina
// koliko njezinih redaka ima knjižno ili prebrojano.
func TestSazetakInventure(t *testing.T) {
	stavke := []models.PopisnaStavka{
		{Knjizno: 7, Utvrdjeno: 7}, {Knjizno: 10}, {Utvrdjeno: 4}, {}, {Knjizno: 2, Utvrdjeno: 1},
	}
	s := MtsPageData{Popis: &models.Popis{Stavke: stavke}}.SazetakPopisa()
	if s.SKolicinom != 3 || s.Manjak != 2 || s.Visak != 1 {
		t.Errorf("sažetak: %+v", s)
	}
	if s := (MtsPageData{}).SazetakPopisa(); s != (SazetakPopisa{}) {
		t.Errorf("bez inventure: %+v", s)
	}
	g := GrupaStavki{Stavke: stavke}
	if g.BrojIma() != 4 || g.BrojUKnjizi() != 3 {
		t.Errorf("skupina: ima %d, u knjizi %d", g.BrojIma(), g.BrojUKnjizi())
	}
}
