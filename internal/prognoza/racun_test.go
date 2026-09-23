package prognoza

import (
	"math"
	"testing"
)

// lanac slaže dvije letve: gornja je ulaz, donja se računa iz nje s kašnjenjem.
func lanac(nagib, odsjecak float64, pomak int, rasap float64) map[string][]Pojas {
	return map[string][]Pojas{
		"donja": {{
			Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000,
			Ulazi: []Ulaz{{Letva: "gornja", Velicina: "vodostaj",
				PomakH: pomak, Nagib: nagib}},
			Odsjecak: odsjecak, Rasap: rasap, R: 0.99, Sati: 10000,
		}},
	}
}

func ravanNiz(od, do int64, pocetak, korak float64) Niz {
	v := map[int64]float64{}
	for t := od; t <= do; t++ {
		v[t] = pocetak + korak*float64(t-od)
	}
	return NoviNiz(v)
}

// Lanac mora stati ondje gdje mu ponestane ulaza, i ni sat dalje. To nije
// nedostatak nego doseg: bez oborine dalje od toga nema što reći.
func TestLanacStaneKadNestaneUlaza(t *testing.T) {
	PoluvijekIspravka = 0
	sada := int64(1000)
	nizovi := map[Izvor]Niz{
		{Letva: "gornja", Velicina: "vodostaj"}: ravanNiz(900, sada, 100, 1),
	}
	r := NovoRacunalo(lanac(1, 0, 5, 3), nizovi, sada)
	izdane, err := r.Prognoziraj("donja", 96, "proba")
	if err != nil {
		t.Fatal(err)
	}
	if len(izdane) != 5 {
		t.Fatalf("doseg %d sati umjesto 5", len(izdane))
	}
	// Zadnja prognoza računa se iz gornje letve u samom satu izdavanja.
	zadnja := izdane[len(izdane)-1]
	gornja, _ := nizovi[Izvor{Letva: "gornja", Velicina: "vodostaj"}].U(sada)
	if math.Abs(zadnja.Vrijednost-gornja) > 1e-9 {
		t.Errorf("na +5 h %g umjesto %g", zadnja.Vrijednost, gornja)
	}
}

// Raspon mora rasti kroz lanac: rasap dionice i promašaj onoga što u nju ulazi
// nisu isti promašaj, pa se zbrajaju kvadratno.
func TestRasponNosiNagib(t *testing.T) {
	PoluvijekIspravka = 0
	sada := int64(1000)
	nizovi := map[Izvor]Niz{
		{Letva: "gornja", Velicina: "vodostaj"}: ravanNiz(900, sada, 100, 0),
	}
	r := NovoRacunalo(lanac(1, 0, 5, 4), nizovi, sada)
	izdane, _ := r.Prognoziraj("donja", 96, "proba")
	if len(izdane) == 0 {
		t.Fatal("nijedan sat")
	}
	// Ulaz je izmjeren, pa raspon nosi samo rasap same dionice.
	if d := math.Abs(izdane[0].Raspon - 4); d > 1e-9 {
		t.Errorf("raspon %g umjesto 4", izdane[0].Raspon)
	}
}

// Model koji je u trenutku izdavanja bio prenizak bit će prenizak i poslije,
// pa se ta razlika nosi naprijed — sve slabije.
func TestIspravakNosiRazlikuPremaMjerenju(t *testing.T) {
	PoluvijekIspravka = 24
	defer func() { PoluvijekIspravka = 48 }()
	sada := int64(1000)
	nizovi := map[Izvor]Niz{
		{Letva: "gornja", Velicina: "vodostaj"}: ravanNiz(900, sada, 100, 0),
		// Donja stoji 30 cm više nego što model kaže.
		{Letva: "donja", Velicina: "vodostaj"}: ravanNiz(900, sada, 130, 0),
	}
	r := NovoRacunalo(lanac(1, 0, 5, 3), nizovi, sada)
	izdane, _ := r.Prognoziraj("donja", 96, "proba")
	if len(izdane) < 5 {
		t.Fatalf("doseg %d sati", len(izdane))
	}
	// Na +1 h ispravak je gotovo cijeli, na +24 h upravo polovica.
	if d := math.Abs(izdane[0].Vrijednost - (100 + 30*math.Exp2(-1.0/24))); d > 1e-9 {
		t.Errorf("na +1 h %g", izdane[0].Vrijednost)
	}
	if d := math.Abs(izdane[4].Vrijednost - (100 + 30*math.Exp2(-5.0/24))); d > 1e-9 {
		t.Errorf("na +5 h %g", izdane[4].Vrijednost)
	}
}

// Rupa dulja od dopuštene ne premošćuje se: u međuvremenu val može i doći i
// otići, pa bi pravac kroz nju bio izmišljen.
func TestPrevelikaRupaSeNePremoscuje(t *testing.T) {
	n := NoviNiz(map[int64]float64{100: 10, 100 + NajveciRazmak: 20,
		200: 30, 200 + NajveciRazmak + 1: 40})
	if v, ima := n.U(100 + NajveciRazmak/2); !ima || math.Abs(v-15) > 1e-9 {
		t.Errorf("kratka rupa: %g, %v", v, ima)
	}
	if _, ima := n.U(200 + NajveciRazmak/2); ima {
		t.Error("duga rupa je premoštena")
	}
}
