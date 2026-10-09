package models

import "testing"

// Potvrda uz akt prepoznaje se po vrsti: potvrda „Vrijedi od” (Odluke
// (8. 10. 2026.), pitanje 17) nije potvrda popisa primatelja (pitanje 19)
func TestImaPotvrdu(t *testing.T) {
	if (Akt{}).ImaPotvrdu(PotvrdaVrijedi) {
		t.Error("akt bez potvrda")
	}
	a := Akt{Potvrde: []PotvrdaAkta{{Vrsta: PotvrdaVrijedi, Tekst: "„Vrijedi od” je 1. 10. 2026. u 08:00 – jeste li sigurni? Potvrđeno.", Tko: "Pero Perić"}}}
	if !a.ImaPotvrdu(PotvrdaVrijedi) || a.ImaPotvrdu(PotvrdaPrimatelji) {
		t.Errorf("potvrda „Vrijedi od”: %+v", a.Potvrde)
	}
	a.Potvrde = append(a.Potvrde, PotvrdaAkta{Vrsta: PotvrdaPrimatelji, Tko: "Ana Anić"})
	if !a.ImaPotvrdu(PotvrdaPrimatelji) {
		t.Errorf("potvrda popisa primatelja: %+v", a.Potvrde)
	}
}
