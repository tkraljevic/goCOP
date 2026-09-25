package prognoza

import (
	"math"
	"testing"
)

// Na izmišljenoj rijeci cilj ponavlja ulaz dva dana kasnije. Model naučen na
// prvom dijelu mora na drugom pogađati bolje od postojanosti, i na 1. i na
// 3. dan.
func TestDnevniPobjedujePostojanost(t *testing.T) {
	ulaz, cilj := DnevniNiz{}, DnevniNiz{}
	val := func(d int64) float64 {
		return 300 + 150*math.Sin(float64(d)/9) + 60*math.Sin(float64(d)/3.7)
	}
	for d := int64(0); d < 8000; d++ {
		ulaz[d] = val(d)
		cilj[d] = val(d-2) + 20
	}
	c := DnevniCilj{Letva: "cilj", Ulazi: []string{"ulaz"}}
	nizovi := map[string]DnevniNiz{"cilj": cilj, "ulaz": ulaz}
	m, err := NamjestiDnevni(nizovi, c, 6000)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []int{1, 3} {
		var mi, post float64
		n := 0
		for d := int64(6010); d < 7990; d++ {
			x, ok := DnevneZnacajke(c, nizovi, d)
			if !ok {
				continue
			}
			p, _ := m.Prognoziraj(x)
			stvarno := cilj[d+int64(k)] - cilj[d]
			mi += (p[k] - stvarno) * (p[k] - stvarno)
			post += stvarno * stvarno
			n++
		}
		mi, post = math.Sqrt(mi/float64(n)), math.Sqrt(post/float64(n))
		if mi > post/3 {
			t.Errorf("%d. dan: promašaj %.1f cm, postojanost %.1f — model ne zna ništa", k, mi, post)
		}
	}
}

// Dan 0 je srednjak 24 sata do sata izdavanja; dan kojem fali više od šest
// sati izostaje, da prognoza ne krene iz pola dana.
func TestDnevniIzSatnog(t *testing.T) {
	v := map[int64]float64{}
	for h := int64(1000); h <= 1100; h++ {
		v[h] = float64(h)
	}
	for h := int64(1030); h <= 1045; h++ {
		delete(v, h) // rupa preko NajveciRazmak ne premošćuje se
	}
	d := DnevniIzSatnog(NoviNiz(v), 1100, 4)
	if got, want := d[0], float64(1077+1100)/2; math.Abs(got-want) > 1e-9 {
		t.Errorf("dan 0: %g umjesto %g", got, want)
	}
	if _, ima := d[-1]; !ima {
		t.Errorf("dan -1 je pun, a izostao je")
	}
	if _, ima := d[-2]; ima {
		t.Errorf("dan -2 ima rupu od 16 sati, a ipak je izračunat: %g", d[-2])
	}
}

// Ulazi dnevnog modela obuhvaćaju i rezerve, a inačice idu glavna pa rezerve.
func TestDnevneRezerveUlazeUUlaze(t *testing.T) {
	ulazi := DnevniUlazi()
	ima := func(l string) bool {
		for _, u := range ulazi {
			if u == l {
				return true
			}
		}
		return false
	}
	if !ima("borl-i") || !ima("varazdin") {
		t.Errorf("ulazi dnevnog modela: %v", ulazi)
	}
	for _, c := range DnevniCiljevi {
		if c.Letva == "botovo" {
			in := c.Inacice()
			if len(in) != 2 || in[0].Ulazi[1] != "borl-i" || in[1].Ulazi[1] != "varazdin" {
				t.Errorf("inačice Botova: %+v", in)
			}
		}
	}
}
