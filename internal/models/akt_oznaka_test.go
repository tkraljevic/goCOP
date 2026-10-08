package models

import (
	"testing"
	"time"
)

// Ista oznaka s dva čvora: oba akta su u sukobu dok rješenje ne obuhvati sve
// akte te oznake (Odluke (8. 10. 2026.), pitanje 4)
func TestSukobiOznaka(t *testing.T) {
	akt := func(id, sektor string, broj, godina int) Akt {
		return Akt{ID: id, Sektor: sektor, Broj: broj, Godina: godina, Status: AktOvjeren}
	}
	osijek, unraid := akt("osijek", "B", 7, 2026), akt("unraid", "B", 7, 2026)
	akti := []Akt{
		osijek, unraid,
		akt("druga-godina", "B", 7, 2027), // B-7/2027 nije B-7/2026
		akt("drugi-sektor", "D", 7, 2026),
		akt("nacrt-1", "B", 0, 2026), akt("nacrt-2", "B", 0, 2026), // nacrti nemaju oznaku
	}
	s := SukobiOznaka(akti)
	if len(s) != 2 {
		t.Fatalf("u sukobu su samo dva akta B-7/2026, a dobiveno: %v", s)
	}
	if d := s["osijek"].Drugi; len(d) != 1 || d[0].ID != "unraid" || s["osijek"].Rijesen() {
		t.Errorf("sukob akta s laptopa: %+v", s["osijek"])
	}
	if d := s["unraid"].Drugi; len(d) != 1 || d[0].ID != "osijek" {
		t.Errorf("sukob akta s poslužitelja: %+v", s["unraid"])
	}

	// rješenje na jednom od akata vrijedi za oba
	rj := &RjesenjeOznake{Akti: []string{"osijek", "unraid"}, Napomena: "dopis primateljima", Kad: time.Now()}
	akti[1].RijesenaOznaka = rj
	s = SukobiOznaka(akti)
	if !s["osijek"].Rijesen() || !s["unraid"].Rijesen() || s["osijek"].Rjesenje.Napomena != "dopis primateljima" {
		t.Errorf("rješenje sukoba: %+v", s)
	}

	// treći akt iste oznake koji stigne poslije ponovno otvara sukob
	akti = append(akti, akt("treci", "B", 7, 2026))
	s = SukobiOznaka(akti)
	if s["osijek"].Rijesen() || s["treci"].Rijesen() || len(s["treci"].Drugi) != 2 {
		t.Errorf("novi akt iste oznake mora otvoriti sukob: %+v", s)
	}
	if (*RjesenjeOznake)(nil).Obuhvaca([]string{"osijek"}) {
		t.Error("bez rješenja ništa nije obuhvaćeno")
	}
	if ids := IDAkata([]Akt{{ID: "b"}, {ID: "a"}}); len(ids) != 2 || ids[0] != "a" {
		t.Errorf("identiteti akata: %v", ids)
	}
}
