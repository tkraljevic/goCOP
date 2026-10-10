package models

import (
	"testing"
	"time"
)

// Ista oznaka s dva čvora: oba akta su u sukobu dok rješenje ne obuhvati sve
// akte te oznake (Odluke (8. 10. 2026.), pitanje 4)
func TestSukobiOznaka(t *testing.T) {
	akt := func(id, sektor string, broj, godina int) Akt {
		return Akt{ID: id, Sektor: sektor, Broj: broj, Godina: godina, Status: AktOvjeren, OvjeraKod: "K-" + id}
	}
	osijek, unraid := akt("osijek", "B", 7, 2026), akt("unraid", "B", 7, 2026)
	akti := []Akt{
		osijek, unraid,
		akt("druga-godina", "B", 7, 2027), // B-7/2027 nije B-7/2026
		akt("drugi-sektor", "D", 7, 2026),
		akt("nacrt-1", "B", 0, 2026), akt("nacrt-2", "B", 0, 2026), // nacrti nemaju oznaku
	}
	s := SukobiOznaka(akti, nil, nil)
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
		{Oznaka: "B-3/2026", Akti: []string{"neki", "drugi"}, Kodovi: []string{"K-neki", "K-drugi"}, Napomena: "druga oznaka"},
		{Oznaka: "B-7/2026", Akti: []string{"osijek", "unraid"}, Kodovi: []string{"K-osijek", "K-unraid"}, Napomena: "dopis primateljima", Kad: time.Now()},
	}
	s = SukobiOznaka(akti, nil, rjesenja)
	if !s["osijek"].Rijesen() || !s["unraid"].Rijesen() || s["osijek"].Rjesenje.Napomena != "dopis primateljima" {
		t.Errorf("rješenje sukoba: %+v", s)
	}
	if s = SukobiOznaka(akti, nil, rjesenja[:1]); s["osijek"].Rijesen() {
		t.Errorf("rješenje druge oznake riješilo je ovu: %+v", s["osijek"])
	}

	// treći akt iste oznake koji stigne poslije ponovno otvara sukob
	akti = append(akti, akt("treci", "B", 7, 2026))
	s = SukobiOznaka(akti, nil, rjesenja)
	if s["osijek"].Rijesen() || s["treci"].Rijesen() || len(s["treci"].Drugi) != 2 {
		t.Errorf("novi akt iste oznake mora otvoriti sukob: %+v", s)
	}
	if (*RjesenjeOznake)(nil).Obuhvaca([]Akt{osijek}) {
		t.Error("bez rješenja ništa nije obuhvaćeno")
	}
	// rješenje pamti i kodove: ista oznaka s drugom ovjerom nije obuhvaćena
	if (&RjesenjeOznake{Akti: []string{"osijek"}}).Obuhvaca([]Akt{osijek}) {
		t.Error("rješenje bez koda ovjere ne obuhvaća ovjeru")
	}
	if ids, kodovi := KljuceviRjesenja([]Akt{{ID: "b", OvjeraKod: "K2"}, {ID: "a", OvjeraKod: "K1"}, {ID: "b", OvjeraKod: "K3"}}); len(ids) != 2 || ids[0] != "a" ||
		len(kodovi) != 3 || kodovi[0] != "K1" {
		t.Errorf("ključevi rješenja: %v %v", ids, kodovi)
	}
}

// Isti nacrt ovjeren na dva čvora: laptop ga ovjeri kao B-1/2026, a
// poslužitelj, koji već ima svoj B-1/2026, kao B-2/2026. Na površini stoji
// ovjera poslužitelja, ovjera laptopa samo u knjizi; obje su označene, i akt
// poslužitelja B-1 uz ovjeru laptopa. Svaki sukob rješava se zasebno
// (Odluke (8. 10. 2026.), pitanje 4).
func TestDrugaOvjeraIstogNacrta(t *testing.T) {
	x := Akt{ID: "x", Sektor: "B", Broj: 2, Godina: 2026, Status: AktOvjeren, OvjeraKod: "806A", Cvor: "unraid"}
	xLaptop := Akt{ID: "x", Sektor: "B", Broj: 1, Godina: 2026, Status: AktOvjeren, OvjeraKod: "CCA9", Cvor: "laptop"}
	y := Akt{ID: "y", Sektor: "B", Broj: 1, Godina: 2026, Status: AktOvjeren, OvjeraKod: "77B0", Cvor: "unraid"}
	z := Akt{ID: "z", Sektor: "B", Broj: 3, Godina: 2026, Status: AktOvjeren, OvjeraKod: "1234", Cvor: "unraid"}
	druge := []Akt{xLaptop}
	s := SukobiOznaka([]Akt{x, y, z}, druge, nil)
	if len(s) != 2 {
		t.Fatalf("u sukobu su akt s dvije ovjere i akt iste oznake kao druga ovjera: %+v", s)
	}
	if o := s["x"].Ovjere; !s["x"].Ima() || len(o) != 1 || o[0].OvjeraKod != "CCA9" || len(s["x"].Drugi) != 0 || s["x"].Rijesen() {
		t.Errorf("akt s dvije ovjere: %+v", s["x"])
	}
	if d := s["y"].Drugi; len(d) != 1 || d[0].ID != "x" || d[0].OvjeraKod != "CCA9" || !s["y"].IzvanPopisa(d[0]) || s["y"].IzvanPopisa(y) {
		t.Errorf("akt iste oznake kao druga ovjera: %+v", s["y"])
	}
	if (SukobOznake{}).Ima() {
		t.Error("prazan sukob")
	}

	// rješenje sukoba akta x ne rješava sukob akta y, i obratno
	akti, kodovi := KljuceviRjesenja(s["x"].SveOvjere(x))
	rjX := RjesenjeOznake{Akti: akti, Kodovi: kodovi, Napomena: "dvije ovjere"}
	s = SukobiOznaka([]Akt{x, y, z}, druge, []RjesenjeOznake{rjX})
	if !s["x"].Rijesen() || s["y"].Rijesen() {
		t.Errorf("rješenje dviju ovjera akta: %+v", s)
	}
	akti, kodovi = KljuceviRjesenja(s["y"].SveOvjere(y))
	rjY := RjesenjeOznake{Akti: akti, Kodovi: kodovi, Napomena: "ista oznaka"}
	s = SukobiOznaka([]Akt{x, y, z}, druge, []RjesenjeOznake{rjY})
	if s["x"].Rijesen() || !s["y"].Rijesen() {
		t.Errorf("rješenje iste oznake: %+v", s)
	}

	// isti broj na oba čvora: druga ovjera iste oznake nije „drugi akt”
	xIsti := xLaptop
	xIsti.Broj = 2
	s = SukobiOznaka([]Akt{x, y, z}, []Akt{xIsti}, nil)
	if len(s) != 1 || len(s["x"].Drugi) != 0 || len(s["x"].Ovjere) != 1 {
		t.Errorf("isti nacrt s istim brojem na dva čvora: %+v", s)
	}
	// akt u popisu još je nacrt (npr. čvor sa starijim programom): druga
	// ovjera ga svejedno označava, a rješenje se veže na nacrt bez koda
	nacrt := Akt{ID: "x", Sektor: "B", Status: AktNacrt}
	s = SukobiOznaka(nil, druge, nil)
	akti, kodovi = KljuceviRjesenja(s["x"].SveOvjere(nacrt))
	if s = SukobiOznaka(nil, druge, []RjesenjeOznake{{Akti: akti, Kodovi: kodovi}}); !s["x"].Rijesen() {
		t.Errorf("rješenje druge ovjere nacrta: %+v", s)
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
