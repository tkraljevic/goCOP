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

// Vrh lanca nema svoj račun, pa se za sate koji dolaze drži njegova zadnja
// vrijednost. Prije toga lanac je na tom satu jednostavno stao — a stao bi
// prerano: doseg određuje najranija voda koja stigne, ne prosječna, pa je
// prigušenje prozorom doseg prepolovilo. Dokle pretpostavka vrijedi ne
// postavlja se ovdje nego se mjeri provjerom.
func TestVrhLancaSeDrziZadnjeVrijednosti(t *testing.T) {
	PoluvijekIspravka = 0
	sada := int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	nizovi := map[Izvor]Niz{gornja: ravanNiz(900, sada, 100, 1)}
	r := NovoRacunalo(lanac(1, 0, 5, 3), nizovi, sada)
	izdane, err := r.Prognoziraj("donja", 12, "proba")
	if err != nil {
		t.Fatal(err)
	}
	// Niz nosi i sat izdavanja, pa ih je trinaest.
	if len(izdane) != 13 {
		t.Fatalf("doseg %d sati umjesto 13", len(izdane))
	}
	if izdane[0].Ciljni != sada {
		t.Errorf("niz počinje u %d umjesto u satu izdavanja %d", izdane[0].Ciljni, sada)
	}
	zadnja, _ := nizovi[gornja].U(sada)
	// Dok ima izmjerenog, prognoza ga slijedi.
	if d := math.Abs(izdane[1].Vrijednost - (zadnja - 4)); d > 1e-9 {
		t.Errorf("na +1 h %g umjesto %g", izdane[1].Vrijednost, zadnja-4)
	}
	// Od +5 h nadalje gornja letva stoji, pa stoji i donja.
	for i := 5; i < len(izdane); i++ {
		if d := math.Abs(izdane[i].Vrijednost - zadnja); d > 1e-9 {
			t.Fatalf("na +%d h %g umjesto %g", i, izdane[i].Vrijednost, zadnja)
		}
	}
}

// Raspon mora rasti kroz lanac: rasap dionice i promašaj onoga što u nju ulazi
// nisu isti promašaj, pa se zbrajaju kvadratno.
func TestRasponNosiNagib(t *testing.T) {
	PoluvijekIspravka = 0
	sada := int64(1000)
	nizovi := map[Izvor]Niz{
		{Letva: "gornja", Velicina: "vodostaj"}: ravanNiz(900, sada, 100, 0),
		{Letva: "donja", Velicina: "vodostaj"}:  ravanNiz(900, sada, 100, 0),
	}
	r := NovoRacunalo(lanac(1, 0, 5, 4), nizovi, sada)
	izdane, _ := r.Prognoziraj("donja", 96, "proba")
	if len(izdane) == 0 {
		t.Fatal("nijedan sat")
	}
	// U satu izdavanja cilj je izmjeren, pa granice padaju na samu vrijednost.
	if izdane[0].Raspon() != 0 {
		t.Errorf("u satu izdavanja raspon %g", izdane[0].Raspon())
	}
	// Sat poslije ulaz je još izmjeren, pa raspon nosi samo rasap same dionice.
	if d := math.Abs(izdane[1].Raspon() - 4); d > 1e-9 {
		t.Errorf("raspon %g umjesto 4", izdane[1].Raspon())
	}
	// Granice moraju stajati oko vrijednosti, ne bilo gdje.
	if g, v, d := izdane[1].Gore, izdane[1].Vrijednost, izdane[1].Dolje; g <= v || v <= d {
		t.Errorf("granice %g … %g ne obuhvaćaju %g", d, g, v)
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
	if len(izdane) < 6 {
		t.Fatalf("doseg %d sati", len(izdane))
	}
	// Na +1 h ispravak je gotovo cijeli, na +24 h upravo polovica.
	if d := math.Abs(izdane[1].Vrijednost - (100 + 30*math.Exp2(-1.0/24))); d > 1e-9 {
		t.Errorf("na +1 h %g", izdane[1].Vrijednost)
	}
	if d := math.Abs(izdane[5].Vrijednost - (100 + 30*math.Exp2(-5.0/24))); d > 1e-9 {
		t.Errorf("na +5 h %g", izdane[5].Vrijednost)
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

// Račun smije posegnuti samo za onim što je u trenutku izdavanja bilo poznato.
// Kad se prognoza pušta unatrag po arhivi, niz ima i kasnija očitanja — i ako
// se uzme zadnje od njih, čita se iz budućnosti. Aljmašu je tako ispao pomak od
// 287 cm, a izgledalo je kao da produžetak uzvodne letve ne valja.
func TestVrhLancaNeCitaIzBuducnosti(t *testing.T) {
	PoluvijekIspravka = 0
	sada := int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	// Niz ide i daleko poslije sata izdavanja, i ondje je posve drugačiji.
	n := ravanNiz(900, sada+500, 100, 1)
	r := NovoRacunalo(lanac(1, 0, 5, 3), map[Izvor]Niz{gornja: n}, sada)
	izdane, err := r.Prognoziraj("donja", 12, "proba")
	if err != nil || len(izdane) != 13 {
		t.Fatalf("doseg %d, %v", len(izdane), err)
	}
	uIzdanju, _ := n.U(sada)
	for i := 5; i < len(izdane); i++ {
		if izdane[i].Vrijednost > uIzdanju+1e-9 {
			t.Fatalf("na +%d h %g, iznad %g poznatog u trenutku izdavanja",
				i, izdane[i].Vrijednost, uIzdanju)
		}
	}
}

// Letva kojoj mjerenje otkaže ne smije zaustaviti lanac: račun je izračuna iz
// njezinih uzvodnih i nastavi dalje. Zato gusti lanac nije samo doseg nego i
// zaliha — kad jedna letva stane, susjedna je pokriva.
func TestLetvaBezMjerenjaNeZaustavljaLanac(t *testing.T) {
	PoluvijekIspravka = 0
	sada := int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	srednja := Izvor{Letva: "srednja", Velicina: "vodostaj"}
	donja := Izvor{Letva: "donja", Velicina: "vodostaj"}

	pojasi := map[string][]Pojas{
		"srednja": {{Letva: "srednja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 2,
			Ulazi: []Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}},
		"donja": {{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []Ulaz{{Letva: "srednja", Velicina: "vodostaj", PomakH: 4, Sirina: 1, Nagib: 1}}}},
	}
	// Srednja letva ne šalje ništa — telemetrija joj je stala.
	nizovi := map[Izvor]Niz{
		gornja: ravanNiz(900, sada, 100, 1),
		donja:  ravanNiz(900, sada, 100, 1),
	}
	r := NovoRacunalo(pojasi, nizovi, sada)
	izdane, err := r.Prognoziraj("donja", 12, "proba")
	if err != nil {
		t.Fatal(err)
	}
	// Doseg je zbroj kašnjenja obiju karika, iako srednja ne mjeri ništa.
	if len(izdane) < 8 {
		t.Fatalf("doseg %d sati; srednja letva je zaustavila lanac", len(izdane))
	}
	if _, ok := nizovi[srednja]; ok {
		t.Fatal("proba je pogrešno postavljena: srednja letva ipak ima niz")
	}
	// I sama srednja letva mora dati vrijednost, računatu iz gornje.
	v, ok := r.U(srednja, sada)
	if !ok {
		t.Fatal("srednja letva bez mjerenja nije izračunata")
	}
	uGornjoj, _ := nizovi[gornja].U(sada - 3)
	if math.Abs(v.Iznos-uGornjoj) > 1e-9 {
		t.Errorf("srednja izračunata %g, a gornja prije tri sata je %g", v.Iznos, uGornjoj)
	}
	if !v.Prognoza {
		t.Error("izračunata vrijednost nije označena kao računata")
	}
}

// Vrh koji kasni do ZaostatakVrha ne vuče izdanje unatrag: prognoza se izda
// za sat najsvježijeg vrha, a vrhu koji kasni drži se zadnja vrijednost. Vrh
// koji kasni više od toga i dalje određuje sat izdanja.
func TestVrhKojiKasniNeVuceIzdanje(t *testing.T) {
	PoluvijekIspravka = 0
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	druga := Izvor{Letva: "druga", Velicina: "vodostaj"}
	vrhovi := map[Izvor]bool{gornja: true, druga: true}
	nizovi := map[Izvor]Niz{gornja: ravanNiz(900, 998, 100, 1), druga: ravanNiz(900, 1000, 50, 0)}
	sada, ok := ZadnjiZajednicki(nizovi, vrhovi)
	if !ok || sada != 1000 {
		t.Fatalf("izdano za %d umjesto 1000", sada)
	}
	r := NovoRacunalo(lanac(1, 0, 1, 3), nizovi, sada)
	izdane, err := r.Prognoziraj("donja", 6, "proba")
	if err != nil || len(izdane) != 7 {
		t.Fatalf("doseg %d, %v", len(izdane), err)
	}
	zadnja, _ := nizovi[gornja].U(998)
	if d := math.Abs(izdane[0].Vrijednost - zadnja); d > 1e-9 {
		t.Errorf("u satu izdanja %g umjesto zadnje javljene %g", izdane[0].Vrijednost, zadnja)
	}

	nizovi[gornja] = ravanNiz(900, 990, 100, 1)
	if sada, _ := ZadnjiZajednicki(nizovi, vrhovi); sada != 990+ZaostatakVrha {
		t.Errorf("s vrhom deset sati iza izdano za %d umjesto %d", sada, 990+ZaostatakVrha)
	}
}

// Vrh s tuđom prognozom ne stoji na zadnjem mjerenju: zadnjem mjerenju dodaje
// se hod tuđe prognoze od sata izdavanja, a njezina razina ne smeta.
func TestVrhSlijediTuduPrognozu(t *testing.T) {
	PoluvijekIspravka = 0
	sada := int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	nizovi := map[Izvor]Niz{gornja: ravanNiz(900, sada, 100, 0)} // stoji na 100
	r := NovoRacunalo(lanac(1, 0, 2, 3), nizovi, sada)
	// Tuđa prognoza je 40 cm viša od našeg mjerenja i raste 2 cm na sat.
	r.PostaviBuducnostVrha(gornja, NizIzTocaka(map[int64]float64{sada - 2: 136, sada + 24: 188}))
	izdane, err := r.Prognoziraj("donja", 12, "proba")
	if err != nil {
		t.Fatal(err)
	}
	// donja(t) = gornja(t-2); gornja(sada+k) = 100 + 2k
	for _, i := range izdane {
		k := i.Ciljni - 2 - sada
		treba := 100.0
		if k > 0 {
			treba += 2 * float64(k)
		}
		if math.Abs(i.Vrijednost-treba) > 1e-9 {
			t.Fatalf("u %+d h %g umjesto %g", i.Ciljni-sada, i.Vrijednost, treba)
		}
	}
}
