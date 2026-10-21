package prognoza

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// Očitanje pripada najbližem punom satu, a ono s pola sata sljedećem. I prije
// 1970. sat se traži prema dolje, a ne prema nuli.
func TestSatOcitanja(t *testing.T) {
	for _, c := range []struct{ s, sat int64 }{
		{10 * 3600, 10},
		{10*3600 + 29*60 + 59, 10},
		{10*3600 + 30*60, 11},
		{10*3600 + 45*60, 11},
		{9*3600 + 31*60, 10},
		{-1, 0},
		{-1800, 0},
		{-1801, -1},
		{-3600, -1},
	} {
		if got := satOcitanja(c.s); got != c.sat {
			t.Errorf("satOcitanja(%d) = %d, želim %d", c.s, got, c.sat)
		}
	}
}

// izmisljenaOcitanja su očitanja jedne letve u minuti mjerenja: pobjednik
// svakog sata ima troznamenkastu vrijednost, a ono što mora otpasti
// jednoznamenkastu.
var izmisljenaOcitanja = []struct {
	sat, minuta int
	cm          int
}{
	{10, 0, 100},
	{10, 2, 1},    // zalutalo, ne smije pregaziti 10:00
	{10, 15, 2},   // 10 h, 15 min poslije
	{10, 45, 3},   // 11 h, 15 min prije
	{11, 10, 111}, // 11 h, 10 min poslije: bliže
	{12, 50, 113}, // 13 h, 10 min prije
	{13, 10, 4},   // 13 h, 10 min poslije: jednako daleko, ostaje ranije
	{14, 30, 115}, // 14 h nema bližeg; 15 h ima svoje
	{15, 0, 116},
	// 12 h ostaje prazan: najbliže je 11:10, pedeset minuta prije
}

// Satu ostaje očitanje najbliže punoj uri, kojim god redom očitanja stižu.
func TestPoSatuNajblizePunojUri(t *testing.T) {
	want := map[int64]float64{10: 100, 11: 111, 13: 113, 14: 115, 15: 116}
	for _, obrnuto := range []bool{false, true} {
		p := noviPoSatu[float64]()
		for i := range izmisljenaOcitanja {
			o := izmisljenaOcitanja[i]
			if obrnuto {
				o = izmisljenaOcitanja[len(izmisljenaOcitanja)-1-i]
			}
			p.dodaj(int64(o.sat*3600+o.minuta*60), float64(o.cm))
		}
		if !reflect.DeepEqual(p.vrijednosti, want) {
			t.Errorf("obrnutim redom %v: %v, želim %v", obrnuto, p.vrijednosti, want)
		}
	}
}

// Letva koja se očitava samo u pola sata ne ostaje bez ijednog sata: sat
// između dva očitanja jednako udaljena uzima ranije.
func TestPoSatuSamoUPolaSata(t *testing.T) {
	p := noviPoSatu[float64]()
	p.dodaj(21*3600+1800, 2)
	p.dodaj(20*3600+1800, 1)
	if want := map[int64]float64{20: 1, 21: 1, 22: 2}; !reflect.DeepEqual(p.vrijednosti, want) {
		t.Errorf("%v, želim %v", p.vrijednosti, want)
	}
}

// Namještanje iz arhive i živa prognoza od istih očitanja dobivaju iste sate.
// Spojeni niz vraća očitanja poredana po vremenu, pa je prije zadnje u satu
// pregazilo ono u :00, a model je učio na nizu pomaknutom prema ranije.
func TestNizIzArhiveKaoZivaPrognoza(t *testing.T) {
	dan := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	arhiva, _ := arhivaUDatoteci(t)
	ocitanja := praznaOcitanjaRacuna(t)
	for _, o := range izmisljenaOcitanja {
		kad := dan.Add(time.Duration(o.sat)*time.Hour + time.Duration(o.minuta)*time.Minute)
		if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('proba', 'vodostaj', 'satni', ?, ?)`,
			kad.Unix(), o.cm); err != nil {
			t.Fatal(err)
		}
		dodajOcitanje(t, ocitanja, "proba", kad, o.cm, nil)
	}

	izArhive, err := NizIzArhive(arhiva, "proba", "vodostaj")
	if err != nil {
		t.Fatal(err)
	}
	sirovo, err := (&Osvjezivac{Ocitanja: ocitanja}).ucitajSirovo(context.Background(), "proba", dan)
	if err != nil {
		t.Fatal(err)
	}
	zivo := map[int64]float64{}
	for sat, s := range sirovo {
		zivo[sat] = float64(s.cm.Int64)
	}
	h := func(hh int64) int64 { return dan.Unix()/3600 + hh }
	want := map[int64]float64{h(10): 100, h(11): 111, h(13): 113, h(14): 115, h(15): 116}
	if !reflect.DeepEqual(izArhive, want) {
		t.Errorf("iz arhive %v, želim %v", izArhive, want)
	}
	if !reflect.DeepEqual(zivo, want) {
		t.Errorf("živo %v, želim %v", zivo, want)
	}
}
