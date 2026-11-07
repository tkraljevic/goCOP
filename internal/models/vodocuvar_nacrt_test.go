package models

import (
	"testing"
	"time"
)

// Riječi stanja lista pišu se kao na crtežu: čeka ovjeru i ovjeren.
func TestStanjeLista(t *testing.T) {
	sad := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		l      VodocuvarskiList
		zelimo string
	}{
		{VodocuvarskiList{ID: "l1"}, "u pisanju"},
		{VodocuvarskiList{ID: "l1", PredanoAt: &sad}, "čeka ovjeru"},
		{VodocuvarskiList{ID: "l1", PredanoAt: &sad, PotvrdenoAt: &sad}, "ovjeren"},
		{VodocuvarskiList{ID: "l1", Rekonstrukcija: true}, "ovjereno prijenosom"},
	} {
		if got := s.l.Stanje(); got != s.zelimo {
			t.Errorf("Stanje = %q, očekivano %q", got, s.zelimo)
		}
	}
}

// Spremljen list bez rednog broja je nacrt za dan koji još nije počeo
// (Odluke (8. 10. 2026.), pitanja 5 i 6). Nespremljen list i prenesen list
// nisu takav nacrt, a list s brojem nije bez broja.
func TestNacrtBezBroja(t *testing.T) {
	for _, s := range []struct {
		ime string
		l   VodocuvarskiList
		bez bool
	}{
		{"spremljen nacrt bez broja", VodocuvarskiList{ID: "l1"}, true},
		{"list s brojem", VodocuvarskiList{ID: "l1", Broj: 4}, false},
		{"još nespremljen list", VodocuvarskiList{}, false},
		{"prenesen iz ranije evidencije", VodocuvarskiList{ID: "l1", Rekonstrukcija: true}, false},
	} {
		if got := s.l.NacrtBezBroja(); got != s.bez {
			t.Errorf("%s: NacrtBezBroja = %v, očekivano %v", s.ime, got, s.bez)
		}
	}
}

// Zaslon piše kratko: sati bez suvišne decimale i dan bez godine, po našem
// vremenu. SatiTekst ostaje kakav je, jer ide na papir.
func TestKratkiZapisNaZaslonu(t *testing.T) {
	for _, s := range []struct {
		od, do, zelimo, papir string
	}{
		{"07:00", "15:00", "8", "8,0"},
		{"07:00", "14:30", "7,5", "7,5"},
		{"22:00", "06:00", "8", "8,0"},
		{"", "", "0", "0,0"},
	} {
		l := VodocuvarskiList{Od: s.od, Do: s.do}
		if got := l.SatiKratko(); got != s.zelimo {
			t.Errorf("SatiKratko %s–%s = %q, očekivano %q", s.od, s.do, got, s.zelimo)
		}
		if got := l.SatiTekst(); got != s.papir {
			t.Errorf("SatiTekst %s–%s = %q, očekivano %q", s.od, s.do, got, s.papir)
		}
	}
	for _, s := range []struct {
		dan    time.Time
		zelimo string
	}{
		{time.Date(2026, 10, 26, 0, 0, 0, 0, Zagreb), "26. 10."},
		{time.Date(2026, 8, 5, 0, 0, 0, 0, Zagreb), "5. 8."},
		// iza ponoći po našem vremenu, a u UTC-u je još prethodni dan
		{time.Date(2026, 8, 4, 22, 30, 0, 0, time.UTC), "5. 8."},
	} {
		if got := (VodocuvarskiList{Datum: s.dan}).DatumKratko(); got != s.zelimo {
			t.Errorf("DatumKratko %v = %q, očekivano %q", s.dan, got, s.zelimo)
		}
	}
}
