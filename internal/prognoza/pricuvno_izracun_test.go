package prognoza

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Pričuvni izračun na izmišljenim podacima: 400 dana arhive Batine i
// Bezdana, dvadeset dana operativnih očitanja Aljmaša, Ilok i Bačka Palanka
// bez današnjeg dana, i tuđe prognoze Bezdana (srpska, tri dana) i Botova
// (mađarska, šest dana). Sat je zadan, pa račun ne ovisi o satu računala.

var pricuvnoSada = time.Date(2026, 9, 30, 5, 30, 0, 0, time.UTC)

// pricuvnoPocetak je prvi dan arhive; 399. dan je danas.
var pricuvnoPocetak = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -399)

func pricuvnoBezdan(d int) float64 { return 100 + 80*math.Sin(float64(d)/9) }

// pricuvnoBatina je 21 cm iznad Bezdana, a danas 31: mjerenje odstupa od
// pravca 10 cm, više nego što pomak smije.
func pricuvnoBatina(d int) float64 {
	if d == 399 {
		return pricuvnoBezdan(d) + 31
	}
	return pricuvnoBezdan(d) + 21
}

// pricuvnoAljmas je 50 cm ispod Batine, s malim kolebanjem.
func pricuvnoAljmas(d int) int { return int(math.Round(pricuvnoBatina(d))) - 50 + d%3 }

func pricuvniPodaci(t *testing.T) (arhiva, ocitanja, prognoze *sql.DB) {
	t.Helper()
	nizovi := map[string]map[int]float64{"batina": {}, "bezdan": {}, "ilok": {}, "backa-palanka": {}, "botovo": {399: 150}}
	for d := 0; d <= 399; d++ {
		nizovi["batina"][d], nizovi["bezdan"][d] = pricuvnoBatina(d), pricuvnoBezdan(d)
	}
	// Ilok i Bačka Palanka: 49 dana do jučer, Ilok točno cm niže.
	for d := 350; d <= 398; d++ {
		nizovi["backa-palanka"][d] = 40 + float64(d%7)
		nizovi["ilok"][d] = 39 + float64(d%7)
	}
	arhiva = arhivaDnevnihOdDana(t, pricuvnoPocetak, nizovi)

	ocitanja = praznaOcitanjaRacuna(t)
	dan := func(d int, sat int) time.Time {
		return pricuvnoPocetak.AddDate(0, 0, d).Add(time.Duration(sat) * time.Hour)
	}
	for d := 380; d <= 399; d++ {
		dodajOcitanje(t, ocitanja, "aljmas", dan(d, 1), pricuvnoAljmas(d), nil)
		dodajOcitanje(t, ocitanja, "aljmas", dan(d, 3), pricuvnoAljmas(d), nil)
	}
	dodajOcitanje(t, ocitanja, "aljmas", dan(379, 2), 0, nil) // jedno očitanje dana ne daje srednjak
	dodajOcitanje(t, ocitanja, "aljmas", dan(300, 1), 0, nil) // starije od 90 dana
	dodajOcitanje(t, ocitanja, "aljmas", dan(300, 2), 0, nil)
	// Batinu danas ima i arhiva, pa očitanja ne smiju prepisati njezin dan.
	dodajOcitanje(t, ocitanja, "batina", dan(399, 1), 999, nil)
	dodajOcitanje(t, ocitanja, "batina", dan(399, 2), 999, nil)

	prognoze = praznaBazaPrognoza(t)
	sat := pricuvnoSada.Unix() / 3600 // 5 h UTC = 7 h u Zagrebu
	tuda := func(izvor, letva string, izdano int64, tocke map[int]float64) {
		for d, v := range tocke {
			if _, err := prognoze.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES (?,?,?,?,?)`,
				izvor, letva, izdano, sat+int64(24*d), v); err != nil {
				t.Fatal(err)
			}
		}
	}
	tuda(PodrijetloHidmet, "bezdan", sat-2, map[int]float64{0: 200, 3: 230})
	tuda(Podrijetlo, "botovo", sat-10, map[int]float64{0: 150, 1: 155, 2: 160, 3: 165, 4: 170, 5: 175, 6: 180})
	tuda(Podrijetlo, "mohacs", sat-100, map[int]float64{0: 10, 6: 40}) // starija od tri dana
	tuda(PodrijetloHidmet, "apatin", sat-1, map[int]float64{1: 77})    // jedna točka nije prognoza
	return arhiva, ocitanja, prognoze
}

func TestPricuvniIzracunUciVezeIPredupisuje(t *testing.T) {
	arhiva, ocitanja, prognoze := pricuvniPodaci(t)
	p := PricuvniIzracun(context.Background(), arhiva, ocitanja, prognoze, pricuvnoSada, models.Zagreb)

	if !p.Sada.Equal(pricuvnoSada) || len(p.Dani) != PricuvniDana+1 {
		t.Fatalf("sada %v, %d dana", p.Sada, len(p.Dani))
	}
	for d, kad := range p.Dani {
		if want := time.Date(2026, 9, 30+d, 7, 0, 0, 0, models.Zagreb); !kad.Equal(want) {
			t.Errorf("dan %d: %v, želim %v", d, kad, want)
		}
	}
	if len(p.Veze) != len(PricuvniPutovi) {
		t.Errorf("veze za %d letvi, želim %d", len(p.Veze), len(PricuvniPutovi))
	}
	for _, pp := range PricuvniPutovi {
		if len(p.Veze[pp.Letva]) != len(pp.Putovi) {
			t.Errorf("%s: %d veza za %d putova", pp.Letva, len(p.Veze[pp.Letva]), len(pp.Putovi))
		}
	}

	// Batina iz Bezdana: pravac naučen na 400 dana, a današnji pomak od 10
	// cm ograničen je na četiri rasapa (rasap ispod 1 cm vrijedi kao 1).
	b := p.Veze["batina"][0]
	if b.Greska != "" || len(b.Koef) != 2 || math.Abs(b.Koef[0]-21) > 0.2 || math.Abs(b.Koef[1]-1) > 0.002 {
		t.Fatalf("Batina iz Bezdana: %+v", b)
	}
	if b.Rasap >= 1 || b.Dana != 400 || b.Od != pricuvnoPocetak.Format("2006-01-02") || b.Pomak != PomakPrijenosaNajvise {
		t.Errorf("Batina iz Bezdana: rasap %v, %d dana od %s, pomak %v", b.Rasap, b.Dana, b.Od, b.Pomak)
	}
	if m := p.Veze["batina"][1]; m.Koef != nil || m.Greska != "batina: premalo zajedničkih dana s mohacs (0)" {
		t.Errorf("Batina iz Mohácsa bez podataka: %+v", m)
	}
	if v := p.Nase["batina"]; v != math.Round(pricuvnoBatina(399)) {
		t.Errorf("današnja Batina %v, želim %v iz arhive, ne iz očitanja", v, math.Round(pricuvnoBatina(399)))
	}

	// Aljmaš: dvadeset dana iz očitanja (dan s jednim očitanjem i dan stariji
	// od 90 dana ne ulaze), pa samo pomak uz nagib 1 i rasap barem 2 cm.
	for i, greska := range []string{"aljmas: premalo zajedničkih dana s apatin, bogojevo (0)", "aljmas: premalo zajedničkih dana s bogojevo (0)"} {
		if v := p.Veze["aljmas"][i]; v.Greska != greska || v.Koef != nil {
			t.Errorf("Aljmaš, put %d: %+v", i, v)
		}
	}
	var zbroj float64
	for d := 380; d <= 399; d++ {
		zbroj += float64(pricuvnoAljmas(d)) - pricuvnoBatina(d)
	}
	odsjecak := zbroj / 20
	a := p.Veze["aljmas"][2]
	if a.Greska != "" || a.Dana != 20 || a.Rasap != 2 || math.Abs(a.Koef[0]-odsjecak) > 1e-9 || a.Koef[1] != 1 {
		t.Fatalf("Aljmaš iz Batine: %+v, želim odsječak %v, 20 dana, rasap 2", a, odsjecak)
	}
	if want := math.Round(float64(pricuvnoAljmas(399)) - (odsjecak + pricuvnoBatina(399))); a.Pomak != want {
		t.Errorf("Aljmaš: pomak %v, želim %v", a.Pomak, want)
	}
	if p.Nase["aljmas"] != float64(pricuvnoAljmas(399)) {
		t.Errorf("današnji Aljmaš %v", p.Nase["aljmas"])
	}

	// Ilok bez današnjeg mjerenja: veza postoji, pomaka nema.
	i := p.Veze["ilok"][0]
	if i.Greska != "" || i.Dana != 49 || i.Koef[0] != -1 || i.Koef[1] != 1 || i.Pomak != 0 {
		t.Errorf("Ilok iz Bačke Palanke: %+v", i)
	}
	if _, ima := p.Nase["ilok"]; ima {
		t.Error("Ilok danas nije mjeren, a ima današnju vrijednost")
	}

	// Mađarska prognoza iste letve: jedinični pravac, bez učenja.
	for _, letva := range []string{"botovo", "terezino-polje", "belisce", "osijek"} {
		if v := p.Veze[letva][0]; v.Greska != "" || len(v.Koef) != 2 || v.Koef[0] != 0 || v.Koef[1] != 1 || v.Izvori[0] != Madjarska+letva {
			t.Errorf("%s iz mađarske prognoze: %+v", letva, v)
		}
	}
	if v := p.Veze["donji-miholjac"][1]; v.Koef[1] != 1 || v.Izvori[0] != Madjarska+"donji-miholjac" {
		t.Errorf("Donji Miholjac iz mađarske prognoze: %+v", v)
	}

	// Ulazi: današnje mjerenje i tuđa prognoza izdana u zadnja tri dana.
	if len(p.Ulazi) != len(PricuvniUlazi) {
		t.Errorf("%d ulaza, želim %d", len(p.Ulazi), len(PricuvniUlazi))
	}
	vr := func(x *float64) any {
		if x == nil {
			return nil
		}
		return *x
	}
	provjeri := func(letva string, want []any) {
		t.Helper()
		red := p.Ulazi[letva]
		for d := range red {
			if got := vr(red[d]); got != want[d] {
				t.Errorf("%s, dan %d: %v, želim %v", letva, d, got, want[d])
			}
		}
	}
	provjeri("bezdan", []any{math.Round(pricuvnoBezdan(399)), 210.0, 220.0, 230.0, nil, nil, nil})
	provjeri(Madjarska+"botovo", []any{150.0, 155.0, 160.0, 165.0, 170.0, 175.0, 180.0})
	provjeri("mohacs", []any{nil, nil, nil, nil, nil, nil, nil})
	provjeri("apatin", []any{nil, nil, nil, nil, nil, nil, nil})
	want := map[string]string{"bezdan": PodrijetloHidmet, Madjarska + "botovo": Podrijetlo}
	if len(p.Izvori) != len(want) || p.Izvori["bezdan"] != want["bezdan"] || p.Izvori[Madjarska+"botovo"] != want[Madjarska+"botovo"] {
		t.Errorf("izvori ulaza %v, želim %v", p.Izvori, want)
	}

	// Iz toga Excel računa: Batina iz Bezdana dok ga ima, Aljmaš iz već
	// izračunate Batine (prva dva puta nemaju vezu), Botovo iz mađarske.
	vrijednosti, putovi := PricuvnoRacunaj(p)
	for d := 0; d <= 3; d++ {
		bez := *p.Ulazi["bezdan"][d]
		batina := math.Round(b.Koef[0] + b.Pomak + b.Koef[1]*bez)
		if got := vrijednosti["batina"][d]; got == nil || *got != batina || putovi["batina"][d] != 0 {
			t.Errorf("Batina, dan %d: %v (put %d), želim %v", d, vr(got), putovi["batina"][d], batina)
		}
		aljmas := math.Round(a.Koef[0] + a.Pomak + batina)
		if got := vrijednosti["aljmas"][d]; got == nil || *got != aljmas || putovi["aljmas"][d] != 2 {
			t.Errorf("Aljmaš, dan %d: %v (put %d), želim %v", d, vr(got), putovi["aljmas"][d], aljmas)
		}
	}
	for d := 4; d <= PricuvniDana; d++ {
		if vrijednosti["batina"][d] != nil || putovi["batina"][d] != -1 || vrijednosti["aljmas"][d] != nil {
			t.Errorf("dan %d bez srpske prognoze: Batina %v (put %d), Aljmaš %v", d,
				vr(vrijednosti["batina"][d]), putovi["batina"][d], vr(vrijednosti["aljmas"][d]))
		}
	}
	if got := vrijednosti["botovo"][6]; got == nil || *got != 180 {
		t.Errorf("Botovo 6. dan %v, želim 180", vr(got))
	}
	if vrijednosti["ilok"][0] != nil || putovi["ilok"][0] != -1 {
		t.Errorf("Ilok bez ulaza dobio je %v", vr(vrijednosti["ilok"][0]))
	}
}

// Bez baze prognoza ulazi nose samo današnje mjerenje, a izvora nema.
func TestPricuvniIzracunBezBazePrognoza(t *testing.T) {
	arhiva, ocitanja, _ := pricuvniPodaci(t)
	p := PricuvniIzracun(context.Background(), arhiva, ocitanja, nil, pricuvnoSada, time.UTC)
	if len(p.Izvori) != 0 {
		t.Errorf("izvori bez baze prognoza: %v", p.Izvori)
	}
	red := p.Ulazi["bezdan"]
	if red[0] == nil || *red[0] != math.Round(pricuvnoBezdan(399)) {
		t.Errorf("današnji Bezdan %v", red[0])
	}
	for d := 1; d <= PricuvniDana; d++ {
		if red[d] != nil {
			t.Errorf("dan %d bez prognoze: %v", d, *red[d])
		}
	}
	// zona UTC: danas u 7 h UTC
	if want := time.Date(2026, 9, 30, 7, 0, 0, 0, time.UTC); !p.Dani[0].Equal(want) {
		t.Errorf("prvi dan %v, želim %v", p.Dani[0], want)
	}
}

// Bez arhive i očitanja nijedan put nema vezu, a svaka greška to kaže.
func TestPricuvniIzracunBezIcega(t *testing.T) {
	p := PricuvniIzracun(context.Background(), nil, nil, nil, pricuvnoSada, models.Zagreb)
	if len(p.Nase) != 0 {
		t.Errorf("mjerenja bez baza: %v", p.Nase)
	}
	for letva, veze := range p.Veze {
		for _, v := range veze {
			if strings.HasPrefix(v.Izvori[0], Madjarska) {
				continue
			}
			if v.Koef != nil || !strings.Contains(v.Greska, "premalo zajedničkih dana") {
				t.Errorf("%s iz %v: %+v", letva, v.Izvori, v)
			}
		}
	}
}

// Ulaz koji nije ni upisan ni izračunat ne daje vrijednost, pa letva ide
// sljedećim putem; veza bez pravca se preskače.
func TestPricuvnoRacunajNepoznatUlaz(t *testing.T) {
	p := PricuvniPodaci{
		Veze: map[string][]PricuvnaVeza{
			"batina": {
				{Letva: "batina", Izvori: []string{"nepoznata"}, Koef: []float64{0, 1}},
				{Letva: "batina", Izvori: []string{"mohacs"}, Greska: "nema"},
				{Letva: "batina", Izvori: []string{"bezdan"}, Koef: []float64{21, 1}},
			},
		},
		Ulazi: map[string][PricuvniDana + 1]*float64{"bezdan": {f(-150)}},
	}
	vr, putovi := PricuvnoRacunaj(p)
	if b := vr["batina"][0]; b == nil || *b != -129 || putovi["batina"][0] != 2 {
		t.Fatalf("Batina: %v, put %d; želim −129 trećim putem", b, putovi["batina"][0])
	}
	if vr["batina"][1] != nil || putovi["batina"][1] != -1 {
		t.Errorf("drugi dan bez ulaza: %v, put %d", vr["batina"][1], putovi["batina"][1])
	}
}
