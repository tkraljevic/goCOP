package service

import (
	"slices"
	"strings"
	"testing"

	"gocop/internal/models"
)

func TestPotpisNosiNajvisuFunkciju(t *testing.T) {
	b := "B"
	u := &models.User{FullName: "Ivo Ivić", Duties: []models.Duty{
		{Title: "Rukovoditelj dionice B.34.1", Role: models.RoleSectionLeader, SectorID: &b, IsActive: true, IsPrimary: true},
		{Title: "Zamjenik rukovoditelja obrane Sektora B", Role: models.RoleSectorDeputy, SectorID: &b, IsActive: true},
		{Title: "Bivši rukovoditelj sektora", Role: models.RoleSectorLeader, SectorID: &b, IsActive: false},
	}}
	if d := najvisaDuznost(u); d == nil || d.Role != models.RoleSectorDeputy {
		t.Fatalf("najviša dužnost: %+v", d)
	}
	if d := najvisaDuznost(&models.User{Duties: []models.Duty{{Title: "Vodočuvar", Role: models.RoleWaterGuard, IsActive: true}}}); d == nil || d.Title != "Vodočuvar" {
		t.Fatalf("jedina dužnost: %+v", d)
	}
}

// Akt se nikad ne izdaje za dionice različitih branjenih područja (Odluke
// (9. 11. 2026.)). Letva mjerodavna za dva područja (Batina: 16 i 34) bez
// izabranog područja ne daje akt; s izabranim daje samo njegove dionice, a
// zadana dionica drugog područja je greška unosa.
func TestAktJednoPodrucje(t *testing.T) {
	letva := []models.Section{
		{Code: "B.16.2", AreaID: 16}, {Code: "B.16.4", AreaID: 16},
		{Code: "B.34.1", AreaID: 34}, {Code: "B.34.2", AreaID: 34}, {Code: "B.34.7", AreaID: 34},
	}
	sifre := func(d []models.Section) []string {
		var out []string
		for _, s := range d {
			out = append(out, s.Code)
		}
		return out
	}
	if _, err := jednoPodrucje(letva, 0, false, "Batina"); err == nil || !strings.Contains(err.Error(), "BP 16 i BP 34") {
		t.Errorf("bez izabranog područja: %v", err)
	}
	if d, err := jednoPodrucje(letva, 16, false, "Batina"); err != nil || !slices.Equal(sifre(d), []string{"B.16.2", "B.16.4"}) {
		t.Errorf("BP 16: %v (%v)", sifre(d), err)
	}
	if _, err := jednoPodrucje(letva[1:4], 16, true, "Batina"); err == nil || !strings.Contains(err.Error(), "B.34.1") {
		t.Errorf("zadana dionica drugog područja: %v", err)
	}
	if _, err := jednoPodrucje(letva, 17, false, "Batina"); err == nil || !strings.Contains(err.Error(), "BP 17") {
		t.Errorf("područje bez dionica letve: %v", err)
	}
	if d, err := jednoPodrucje(letva[2:], 0, false, "Batina"); err != nil || len(d) != 3 {
		t.Errorf("letva jednog područja: %v (%v)", sifre(d), err)
	}
}
