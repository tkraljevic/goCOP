package service

import (
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

// Područje akta je ono s najviše dionica; kad ih dva imaju jednako (Dalj,
// Ilok: 15 i 34 po jednu), odlučuje prva dionica po šifri, a ne slučajni
// redoslijed obilaska mape. O području ovisi tko akt potpisuje.
func TestPodrucjeAktaJeOdredeno(t *testing.T) {
	dionice := func(podrucja ...int) []models.Section {
		var out []models.Section
		for _, p := range podrucja {
			out = append(out, models.Section{AreaID: p})
		}
		return out
	}
	for _, x := range []struct {
		podrucja []int
		zeli     int
	}{
		{[]int{15, 15, 34}, 15},
		{[]int{15, 34, 34, 34, 34}, 34},
		{[]int{15, 34}, 15},
		{[]int{17, 34}, 17},
		{[]int{16, 16, 34, 34}, 16},
		{[]int{34}, 34},
		{nil, 0},
	} {
		for i := 0; i < 50; i++ {
			if got := podrucjeAkta(dionice(x.podrucja...)); got != x.zeli {
				t.Fatalf("podrucjeAkta(%v) = %d, želi %d", x.podrucja, got, x.zeli)
			}
		}
	}
}
