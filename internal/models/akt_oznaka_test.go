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
	s := SukobiOznaka(akti, nil)
	if len(s) != 2 {
		t.Fatalf("u sukobu su samo dva akta B-7/2026, a dobiveno: %v", s)
	}
	if d := s["osijek"].Drugi; len(d) != 1 || d[0].ID != "unraid" || s["osijek"].Rijesen() {
		t.Errorf("sukob akta s laptopa: %+v", s["osijek"])
	}
	if d := s["unraid"].Drugi; len(d) != 1 || d[0].ID != "osijek" {
		t.Errorf("sukob akta s poslužitelja: %+v", s["unraid"])
	}

	// rješenje obuhvaća oba akta; rješenje druge oznake ne rješava ovu
	rjesenja := []RjesenjeOznake{
		{Oznaka: "B-3/2026", Akti: []string{"neki", "drugi"}, Napomena: "druga oznaka"},
		{Oznaka: "B-7/2026", Akti: []string{"osijek", "unraid"}, Napomena: "dopis primateljima", Kad: time.Now()},
	}
	s = SukobiOznaka(akti, rjesenja)
	if !s["osijek"].Rijesen() || !s["unraid"].Rijesen() || s["osijek"].Rjesenje.Napomena != "dopis primateljima" {
		t.Errorf("rješenje sukoba: %+v", s)
	}
	if s = SukobiOznaka(akti, rjesenja[:1]); s["osijek"].Rijesen() {
		t.Errorf("rješenje druge oznake riješilo je ovu: %+v", s["osijek"])
	}

	// treći akt iste oznake koji stigne poslije ponovno otvara sukob
	akti = append(akti, akt("treci", "B", 7, 2026))
	s = SukobiOznaka(akti, rjesenja)
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

// Gdje su ovjereni akti iste oznake: na različitim čvorovima, na istom, ili
// se ne zna (čvor nije upisan)
func TestCvoroviSukobaOznake(t *testing.T) {
	akt := func(cvor string) Akt { return Akt{Cvor: cvor} }
	for _, c := range []struct {
		opis  string
		a     Akt
		drugi []Akt
		hoce  string
	}{
		{"dva čvora", akt("laptop"), []Akt{akt("unraid")}, SukobRazlicitiCvorovi},
		{"jedan od trojice s drugog čvora", akt("laptop"), []Akt{akt(""), akt("unraid")}, SukobRazlicitiCvorovi},
		{"isti čvor", akt("laptop"), []Akt{akt("laptop")}, SukobIstiCvor},
		{"čvor drugog akta nije upisan", akt("laptop"), []Akt{akt("laptop"), akt("")}, ""},
		{"čvor akta nije upisan", akt(""), []Akt{akt("unraid")}, ""},
	} {
		if got := (SukobOznake{Drugi: c.drugi}).Cvorovi(c.a); got != c.hoce {
			t.Errorf("%s: %q, očekivano %q", c.opis, got, c.hoce)
		}
	}
}
