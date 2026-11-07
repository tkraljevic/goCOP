package prognoza

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// Letve u ovim testovima su izmišljene: potok-vrh → potok-dole, uz potok-drugi kao
// rezervu.

const satiPotoka = int64(6000)

// arhivaPotoka ima niz dobrog cilja koji kasni za vrhom pet sati i druge nizove
// po imenima, u vodostaju.
func arhivaPotoka(t *testing.T, dodatni map[string]map[int64]float64) *sql.DB {
	t.Helper()
	nizovi := map[string]map[int64]float64{
		"potok-vrh:vodostaj":   satniNiz(0, satiPotoka, valUlaza),
		"potok-drugi:vodostaj": satniNiz(0, satiPotoka, func(h int64) float64 { return valUlaza(h) + 10 }),
		"potok-dole:vodostaj":  satniNiz(0, satiPotoka, func(h int64) float64 { return 2*valUlaza(h-5) + 3 }),
	}
	for k, n := range dodatni {
		nizovi[k] = n
	}
	return arhivaSatnih(t, nizovi)
}

func zapisZaPripremu(ulazi ...string) ZapisLetve {
	return ZapisLetve{Oblik: OblikModelaLetve, Letva: "potok-dole", Metoda: MetodaLanac, Velicina: "vodostaj",
		Racun: RacunLetve{Ulazi: ulazi}}
}

func porukaIma(err error, dio string) bool { return err != nil && strings.Contains(err.Error(), dio) }

// Zapis bez razina dobiva zadane razine, spojene odozgo koliko traži duljina niza:
// svaka ima dosta sati, a najviša voda iz niza ima svoj račun.
func TestKratkomNizuManjeRazina(t *testing.T) {
	postavi(t, &NajmanjeSatiPojasa, najmanjeSatiRazine)
	for sati, razina := range map[int]int{600: 1, 1000: 1, 1800: 3, 5000: 4, 10000: 5, 100000: 5} {
		poredani := make([]float64, sati)
		for i := range poredani {
			poredani[i] = float64(i)
		}
		c := cvoroviZadanih(poredani)
		if len(c)-1 != razina {
			t.Errorf("%d sati: %d razina (%v), želim %d", sati, len(c)-1, c, razina)
		}
		if c[0] != 0 || c[len(c)-1] != float64(sati-1) {
			t.Errorf("%d sati: razine ne pokrivaju niz: %v", sati, c)
		}
		if err := provjeriSatiRazina(poredani, c); err != nil {
			t.Errorf("%d sati: %v", sati, err)
		}
	}
	if c := cvoroviZadanih(make([]float64, 10000)); len(c) != 1 {
		t.Errorf("voda koja stoji: %v", c)
	}
}

// Priprema iz parametara daje račun koji se da spremiti, a ništa ne sprema.
func TestPripremiLetvu(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	p, err := PripremiLetvu(arhiva, baza, zapisZaPripremu("potok-vrh"), OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	if NajmanjeSati != 5000 || NajmanjeSatiPojasa != 2000 {
		t.Errorf("granice poslije pripreme: %d i %d", NajmanjeSati, NajmanjeSatiPojasa)
	}
	in := p.Zapis.Brojevi.Inacice
	if len(in) != 1 || len(in[0]) != 4 {
		t.Fatalf("inačice %+v, želim jednu s četiri razine", in)
	}
	for _, r := range in[0] {
		if r.Ulazi[0].PomakH != 5 || math.Abs(r.Ulazi[0].Nagib-2) > 0.01 || r.Velicina != "vodostaj" || r.Ulazi[0].Velicina != "vodostaj" {
			t.Errorf("razina %+v", r)
		}
	}
	if p.Zapis.Racun.Doseg != 5 {
		t.Errorf("doseg %d, želim 5: vrh nema svoj račun", p.Zapis.Racun.Doseg)
	}
	u := p.Ucenje
	sati := 0
	for _, r := range in[0] {
		sati += r.Sati
	}
	if u.Sati != sati || u.Od >= u.Do || u.Od < 2*(100-55)+3 || u.Do > 2*(100+55)+3 || u.OdSata >= u.DoSata || u.DoSata != satiPotoka-1 {
		t.Errorf("učenje %+v, sati razina %d", u, sati)
	}
	if len(p.Napomene) != 0 {
		t.Errorf("napomene %v", p.Napomene)
	}
	if z, _, err := ZapisiLetvi(baza); err != nil || len(z) != 0 {
		t.Errorf("priprema je spremila zapis: %v, %v", z, err)
	}
	if err := SpremiZapisLetve(baza, p.Zapis, "otisak", OkvirModela{}); err != nil {
		t.Errorf("rezultat se ne da spremiti: %v", err)
	}
}

// Kratak niz, i cilj i ulaz, prolazi cijeli put s manje razina. Glavni lanac i dalje
// traži 5.000 sati, a preko 1.000 sati ne prolazi ni priprema.
func TestPripremaKratkogNiza(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	niz := func(sati int64) *sql.DB {
		return arhivaSatnih(t, map[string]map[int64]float64{
			"potok-vrh:vodostaj":  satniNiz(0, sati, valUlaza),
			"potok-dole:vodostaj": satniNiz(0, sati, func(h int64) float64 { return 2*valUlaza(h-5) + 3 }),
		})
	}
	baza := otvoriProbnu(t, "p.db")
	p, err := PripremiLetvu(niz(1800), baza, zapisZaPripremu("potok-vrh"), OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(p.Zapis.Brojevi.Inacice[0]); n != 3 {
		t.Errorf("%d razina, želim 3", n)
	}
	if p.Ucenje.Sati > 1800 || p.Ucenje.Sati < 1700 {
		t.Errorf("učio %d sati", p.Ucenje.Sati)
	}
	if _, err := PripremiLetvu(niz(900), baza, zapisZaPripremu("potok-vrh"), OkvirModela{}); !porukaIma(err, "samo 900") {
		t.Errorf("premalo sati: %v", err)
	}
	if _, err := NamjestiLetvu(niz(1800), "potok-dole", "vodostaj", []Izvor{{Letva: "potok-vrh", Velicina: "vodostaj"}}); !porukaIma(err, "samo 1800") {
		t.Errorf("glavni lanac s kratkim nizom: %v", err)
	}
}

// Stručnjak zadaje razine postocima ili granicama. Razina koja se ne da namjestiti
// odbija se s razlogom, a ne nestaje tiho.
func TestPripremaRazineIzZapisa(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	stoji := satniNiz(0, satiPotoka, func(h int64) float64 { return math.Max(100, math.Round(valUlaza(h))) })
	arhiva := arhivaPotoka(t, map[string]map[int64]float64{"stoji:vodostaj": stoji})
	baza := otvoriProbnu(t, "p.db")
	for ime, s := range map[string]struct {
		razine RazineLetve
		ulaz   string
		razina int
		od     []float64
		greska string
	}{
		"dva postotka":    {razine: RazineLetve{Postoci: []float64{0, 100}}, razina: 1},
		"tri postotka":    {razine: RazineLetve{Postoci: []float64{0, 50, 100}}, razina: 2},
		"granica":         {razine: RazineLetve{Granice: []float64{100}}, razina: 2, od: []float64{1, 100}},
		"izvan arhive":    {razine: RazineLetve{Granice: []float64{9999}}, greska: "izvan vode"},
		"ispod arhive":    {razine: RazineLetve{Granice: []float64{-9999}}, greska: "izvan vode"},
		"premala razina":  {razine: RazineLetve{Postoci: []float64{0, 1, 100}}, greska: "treba barem 500"},
		"premala granica": {razine: RazineLetve{Granice: []float64{152}}, greska: "treba barem 500"},
		"bez širine":      {razine: RazineLetve{Postoci: []float64{0, 10, 100}}, ulaz: "stoji", greska: "nema širine"},
		"i jedno i drugo": {razine: RazineLetve{Postoci: []float64{0, 100}, Granice: []float64{100}}, greska: "ne jednim i drugim"},
		"postoci bez 100": {razine: RazineLetve{Postoci: []float64{0, 50}}, greska: "od 0 do 100"},
	} {
		t.Run(ime, func(t *testing.T) {
			ulaz := s.ulaz
			if ulaz == "" {
				ulaz = "potok-vrh"
			}
			z := zapisZaPripremu(ulaz)
			z.Racun.Razine = s.razine
			p, err := PripremiLetvu(arhiva, baza, z, OkvirModela{})
			if s.greska != "" {
				if !porukaIma(err, s.greska) {
					t.Fatalf("želim %q, a dobio %v", s.greska, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ps := p.Zapis.Brojevi.Inacice[0]
			if len(ps) != s.razina {
				t.Fatalf("%d razina: %+v", len(ps), ps)
			}
			if s.od != nil && ps[1].Od != s.od[1] {
				t.Errorf("granica %g", ps[1].Od)
			}
		})
	}
}

// Isključena razina nema prognozu. Ona koja ne postoji ili nema račun odbija se, jer
// bi na njoj računala susjedna, pa i kad je isključena sva voda ili uz rezervu.
func TestPripremaIskljuceneRazine(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	razine := func(isk ...int) RazineLetve {
		return RazineLetve{Postoci: []float64{0, 50, 100}, Iskljucene: isk}
	}
	z := zapisZaPripremu("potok-vrh")
	z.Racun.Razine = razine(0)
	p, err := PripremiLetvu(arhiva, baza, z, OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	ps := p.Zapis.Brojevi.Inacice[0]
	if !ps[0].Nepovezan || ps[1].Nepovezan {
		t.Errorf("isključena je prva razina: %+v", ps)
	}
	for ime, s := range map[string]struct {
		razine  RazineLetve
		rezerve [][]string
		greska  string
	}{
		"ne postoji": {razine: razine(2), greska: "ne postoji"},
		"sve":        {razine: razine(0, 1), greska: "sve razine"},
		"uz rezervu": {razine: razine(0), rezerve: [][]string{{"potok-drugi"}}, greska: "rezervu"},
	} {
		t.Run(ime, func(t *testing.T) {
			z := zapisZaPripremu("potok-vrh")
			z.Racun.Razine, z.Racun.Rezerve = s.razine, s.rezerve
			if _, err := PripremiLetvu(arhiva, baza, z, OkvirModela{}); !porukaIma(err, s.greska) {
				t.Errorf("želim %q, a dobio %v", s.greska, err)
			}
		})
	}

	// Razina koja nema dosta sati sa svim ulazima ne dobije račun.
	d := &razdiobaRazina{razine: RazineLetve{Iskljucene: []int{0}}, cvorovi: []float64{0, 50, 100}}
	if err := d.oznaciIskljucene([]Pojas{{Od: 50, Do: 100}}); !porukaIma(err, "spoji je") {
		t.Errorf("isključena razina bez računa: %v", err)
	}
	if n := d.napomene([]Pojas{{Od: 50, Do: 100}}); len(n) != 1 || !strings.Contains(n[0], "od 0 do 50") {
		t.Errorf("napomene %v", n)
	}
}

// Rezerva se namješta na svojim razinama i zaseban je broj inačice. Ona koja se ne
// da namjestiti ostaje napomena, a greška čitanja arhive prekida pripremu.
func TestPripremaRezerve(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	z := zapisZaPripremu("potok-vrh")
	z.Racun.Razine = RazineLetve{Granice: []float64{100}}
	z.Racun.Rezerve = [][]string{{"nema-niza"}, {"potok-drugi"}}
	p, err := PripremiLetvu(arhiva, baza, z, OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	in := p.Zapis.Brojevi.Inacice
	if len(in) != 2 || in[1][0].Inacica != 1 || in[1][0].Ulazi[0].Letva != "potok-drugi" {
		t.Fatalf("inačice %+v", in)
	}
	if len(in[0]) != 2 || len(in[1]) != 4 {
		t.Errorf("ručne granice vrijede samo za glavnu inačicu: %d i %d razina", len(in[0]), len(in[1]))
	}
	if len(p.Napomene) != 1 || !strings.Contains(p.Napomene[0], "nema-niza nema račun") {
		t.Errorf("napomene %v", p.Napomene)
	}

	z.Racun.Razine = RazineLetve{Postoci: []float64{0, 50, 100}}
	z.Racun.Rezerve = [][]string{{"potok-drugi"}}
	p, err = PripremiLetvu(arhiva, baza, z, OkvirModela{})
	if err != nil || len(p.Zapis.Brojevi.Inacice[1]) != 2 {
		t.Errorf("postoci vrijede i za rezervu: %+v, %v", p.Zapis.Brojevi.Inacice, err)
	}

	if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('potok-drugi', 'vodostaj', 'satni', 3600000000, 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := PripremiLetvu(arhiva, baza, z, OkvirModela{}); !errors.Is(err, ErrArhiva) {
		t.Errorf("greška arhive u rezervi: %v", err)
	}
}

// Veličina ulaza: ulaz s računom u veličini tog računa, ulaz iz koda u svojoj, a nova
// letva u veličini cilja. Protok se traži samo uz satni protok u arhivi.
func TestPripremaVelicine(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	postavi(t, &Velicine, map[string]string{"potok-vrh": "protok"})
	cilj := satniNiz(0, satiPotoka, func(h int64) float64 { return 2*valUlaza(h-5) + 3 })
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{
		"potok-vrh:protok":      satniNiz(0, satiPotoka, valUlaza),
		"potok-nova:protok":     satniNiz(0, satiPotoka, valUlaza),
		"potok-nova:vodostaj":   satniNiz(0, satiPotoka, valUlaza),
		"potok-gore:vodostaj":   satniNiz(0, satiPotoka, valUlaza),
		"potok-dole:vodostaj":   cilj,
		"potok-dole:protok":     cilj,
		"potok-bez-q:vodostaj":  satniNiz(0, satiPotoka, valUlaza),
		"potok-cilj-h:vodostaj": cilj,
	})
	baza := otvoriProbnu(t, "p.db")
	upisiZapis(t, baza, zapisPotoka("potok-gore", "potok-nova", 2))

	velicinaUlaza := func(t *testing.T, vel, letva, ulaz string) (string, error) {
		t.Helper()
		z := zapisZaPripremu(ulaz)
		z.Letva, z.Velicina = letva, vel
		p, err := PripremiLetvu(arhiva, baza, z, OkvirModela{})
		if err != nil {
			return "", err
		}
		return p.Zapis.Brojevi.Inacice[0][0].Ulazi[0].Velicina, nil
	}
	for _, s := range []struct {
		ime, vel, letva, ulaz, zeli, greska string
	}{
		{ime: "nova letva kao cilj", vel: "protok", letva: "potok-dole", ulaz: "potok-nova", zeli: "protok"},
		{ime: "nova letva u vodostaju", vel: "vodostaj", letva: "potok-dole", ulaz: "potok-nova", zeli: "vodostaj"},
		{ime: "letva iz koda", vel: "vodostaj", letva: "potok-dole", ulaz: "potok-vrh", zeli: "protok"},
		{ime: "ulaz s računom", vel: "protok", letva: "potok-dole", ulaz: "potok-gore", zeli: "vodostaj"},
		{ime: "cilj bez protoka", vel: "protok", letva: "potok-cilj-h", ulaz: "potok-nova", greska: "potok-cilj-h nema satni protok"},
		{ime: "ulaz bez protoka", vel: "protok", letva: "potok-dole", ulaz: "potok-bez-q", greska: "potok-bez-q nema satni protok"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			v, err := velicinaUlaza(t, s.vel, s.letva, s.ulaz)
			if s.greska != "" {
				if !porukaIma(err, s.greska) {
					t.Fatalf("želim %q, a dobio %v", s.greska, err)
				}
				return
			}
			if err != nil || v != s.zeli {
				t.Errorf("veličina %q, %v; želim %q", v, err, s.zeli)
			}
		})
	}
}

// Doseg je najkraći zbroj kašnjenja i dosega ulaza na najvišoj razini koja računa. Ulaz
// bez računa nema dosega, pa i kad ga glavni lanac čita, a račun s kašnjenjem nula daje
// barem sat. Redoslijed ulaza ne mijenja ništa.
func TestPripremaDoseg(t *testing.T) {
	s := &pripremaRacuna{zapisi: map[string]ZapisLetve{
		"potok-sredina": {Racun: RacunLetve{Doseg: 20}},
		// Račun iz koda izdaje se do kraja izdanja, ali dalje od vrha drži njegovo
		// zadnje mjerenje, pa mu vrijedi lanac: 3 h do vrha, odnosno 5 + 3 h.
		"potok-karasica": {Racun: RacunLetve{Doseg: najveciDosegZapisa}, Ugradjen: true,
			Brojevi: BrojeviLetve{Inacice: [][]Pojas{{pojasPotoka("potok-karasica", "potok-vrh", 3)}}}},
		"potok-vucica": {Racun: RacunLetve{Doseg: najveciDosegZapisa}, Ugradjen: true,
			Brojevi: BrojeviLetve{Inacice: [][]Pojas{{pojasPotoka("potok-vucica", "potok-karasica", 5)}}}},
		"potok-uz-glavni": {Racun: RacunLetve{Doseg: najveciDosegZapisa}, Ugradjen: true,
			Brojevi: BrojeviLetve{Inacice: [][]Pojas{{pojasPotoka("potok-uz-glavni", "donja", 2)}}}},
		// Krug u brojevima iz baze ne smije zavrtjeti račun dosega.
		"potok-krug-a": {Ugradjen: true, Brojevi: BrojeviLetve{Inacice: [][]Pojas{{pojasPotoka("potok-krug-a", "potok-krug-b", 1)}}}},
		"potok-krug-b": {Ugradjen: true, Brojevi: BrojeviLetve{Inacice: [][]Pojas{{pojasPotoka("potok-krug-b", "potok-krug-a", 1)}}}}},
		okvir: OkvirModela{Glavne: map[string][][]Pojas{"donja": {{pojasPotoka("donja", "gornja", 3)}}}}}
	ulaz := func(letva string, pomak int) Ulaz { return Ulaz{Letva: letva, PomakH: pomak} }
	for _, c := range []struct {
		opis  string
		ulazi []Ulaz
		zeli  int
	}{
		{"vrh", []Ulaz{ulaz("potok-vrh", 7)}, 7},
		{"kašnjenje nula", []Ulaz{ulaz("potok-vrh", 0)}, 1},
		{"ulaz s računom", []Ulaz{ulaz("potok-sredina", 4)}, 24},
		{"glavni lanac", []Ulaz{ulaz("donja", 4)}, 96},
		{"ulaz glavnog lanca bez računa", []Ulaz{ulaz("gornja", 4)}, 4},
		{"račun iz koda", []Ulaz{ulaz("potok-karasica", 4)}, 7},
		{"dva računa iz koda", []Ulaz{ulaz("potok-vucica", 4)}, 12},
		{"račun iz koda na glavnom lancu", []Ulaz{ulaz("potok-uz-glavni", 4)}, 96},
		{"sporedni vrh skraćuje", []Ulaz{ulaz("donja", 4), ulaz("potok-vrh", 2)}, 2},
		{"obrnut redoslijed", []Ulaz{ulaz("potok-vrh", 2), ulaz("donja", 4)}, 2},
		{"sporedni s računom", []Ulaz{ulaz("donja", 4), ulaz("potok-sredina", 30)}, 50},
	} {
		// Niža razina s dugim kašnjenjem ne računa se, a ni isključena viša.
		iskljucena := pojasPotoka("x", "potok-vrh", 50)
		iskljucena.Nepovezan = true
		racuna := pojasPotoka("x", "potok-vrh", 0)
		racuna.Ulazi = c.ulazi
		glavna := []Pojas{pojasPotoka("x", "potok-vrh", 60), racuna, iskljucena}
		if d := s.doseg(glavna); d != c.zeli {
			t.Errorf("%s: doseg %d, želim %d", c.opis, d, c.zeli)
		}
	}
	krug := pojasPotoka("x", "potok-krug-a", 2)
	if d := s.doseg([]Pojas{krug}); d >= najveciDosegZapisa {
		t.Errorf("krug u brojevima: doseg %d", d)
	}
	iskljucena := pojasPotoka("potok-sve-iskljuceno", "potok-vrh", 2)
	iskljucena.Nepovezan = true
	if d := s.dosegPojasa([]Pojas{iskljucena}, 1); d != 0 {
		t.Errorf("račun bez ijedne razine koja računa: doseg %d, želim 0", d)
	}
}

// Račun iz koda je i priprema i spremanje vide kao račun: krug kroz njega odbijaju, a
// ulaz koji se tako računa seže do vrha svog lanca, ne do kraja izdanja.
func TestPripremaIRacunIzKoda(t *testing.T) {
	racuniPotoka(t)
	postavi(t, &NajveciPomak, 8)
	nova := satniNiz(0, satiPotoka, func(h int64) float64 { return 2*valUlaza(h-8) + 3 })
	arhiva := arhivaPotoka(t, map[string]map[int64]float64{"potok-nova:vodostaj": nova})
	baza := otvoriProbnu(t, "p.db")
	spremiBrojevePotoka(t, baza)

	krug := zapisZaPripremu("potok-dole")
	krug.Letva = "potok-vrh"
	if _, err := PripremiLetvu(arhiva, baza, krug, OkvirModela{}); !porukaIma(err, "krug") {
		t.Errorf("priprema, krug kroz račun iz koda: %v", err)
	}
	if err := SpremiZapisLetve(baza, zapisPotoka("potok-vrh", "potok-dole", 2), "", OkvirModela{}); !porukaIma(err, "krug") {
		t.Errorf("spremanje, krug kroz račun iz koda: %v", err)
	}

	z := zapisZaPripremu("potok-dole")
	z.Letva = "potok-nova"
	p, err := PripremiLetvu(arhiva, baza, z, OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	ps := p.Zapis.Brojevi.Inacice[0]
	if want := ps[len(ps)-1].Ulazi[0].PomakH + 2; p.Zapis.Racun.Doseg != want {
		t.Errorf("doseg %d, želim %d: kašnjenje do ulaza i 2 h od ulaza do vrha", p.Zapis.Racun.Doseg, want)
	}
}

// Krug kroz račun iz koda odbija se i kad taj račun još nema brojeve u bazi: inače bi
// se upisao, a čim Pripremi model upiše brojeve, ispale bi sve letve u krugu.
func TestKrugKrozRacunIzKodaBezBrojeva(t *testing.T) {
	racuniPotoka(t)
	postavi(t, &NajveciPomak, 8)
	sRezervom := zapisPotoka("potok-vrh", "potok-drugi", 2)
	sRezervom.Racun.Rezerve = [][]string{{"potok-dole"}}
	for ime, z := range map[string]ZapisLetve{
		"izravno":       zapisPotoka("potok-vrh", "potok-dole", 2),
		"preko dva":     zapisPotoka("potok-vrh", potokUsce, 2),
		"kroz rezervu":  sRezervom,
		"redak za kod":  zapisPotoka("potok-dole", potokUsce, 2),
		"letva iz sebe": zapisPotoka("potok-dole", "potok-dole", 2),
	} {
		t.Run(ime, func(t *testing.T) {
			baza := otvoriProbnu(t, "p.db")
			if err := SpremiZapisLetve(baza, z, "", OkvirModela{}); !porukaIma(err, "krug") {
				t.Errorf("spremanje: %v", err)
			}
		})
	}
	z := zapisZaPripremu("potok-dole")
	z.Letva = "potok-vrh"
	if _, err := PripremiLetvu(arhivaPotoka(t, nil), otvoriProbnu(t, "p.db"), z, OkvirModela{}); !porukaIma(err, "krug") {
		t.Errorf("priprema: %v", err)
	}
	// Bez kruga se upisuje: novi račun iz letve iz koda i redak umjesto koda.
	baza := otvoriProbnu(t, "p.db")
	for _, z := range []ZapisLetve{zapisPotoka("potok-nova", "potok-dole", 2), zapisPotoka("potok-dole", "potok-drugi", 2)} {
		if err := SpremiZapisLetve(baza, z, "", OkvirModela{}); err != nil {
			t.Errorf("%s: %v", z.Letva, err)
		}
	}
	// Krug koji je već u bazi, a nema veze s novim računom, ne brani upis.
	upisiZapis(t, baza, zapisPotoka("krug-a", "krug-b", 1))
	upisiZapis(t, baza, zapisPotoka("krug-b", "krug-a", 1))
	if err := SpremiZapisLetve(baza, zapisPotoka("potok-treca", "potok-drugi", 2), "", OkvirModela{}); err != nil {
		t.Errorf("stari krug brani upis: %v", err)
	}
	// Pokvaren redak vrijedi umjesto koda i ne računa se, pa kroz njegov račun iz koda
	// nema kruga.
	pokvarena := otvoriProbnu(t, "k.db")
	if _, err := pokvarena.Exec(`INSERT INTO model_letve (letva, proba, sadrzaj) VALUES ('potok-dole', '', 'x')`); err != nil {
		t.Fatal(err)
	}
	if err := SpremiZapisLetve(pokvarena, zapisPotoka("potok-vrh", potokUsce, 2), "", OkvirModela{}); err != nil {
		t.Errorf("uz pokvaren redak: %v", err)
	}
}

// Isti ulaz dvaput namještanje primi i podijeli mu nagib na dva besmislena broja, pa
// se odbija i pri pripremi i pri spremanju.
func TestRacunBezPonavljanja(t *testing.T) {
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	for ime, r := range map[string]struct {
		ulazi   []string
		rezerve [][]string
		greska  string
	}{
		"dvaput isti ulaz":   {ulazi: []string{"potok-vrh", "potok-vrh"}, greska: "dvaput"},
		"dvaput u rezervi":   {ulazi: []string{"potok-vrh"}, rezerve: [][]string{{"potok-drugi", "potok-drugi"}}, greska: "dvaput"},
		"rezerva kao glavni": {ulazi: []string{"potok-vrh"}, rezerve: [][]string{{"potok-vrh"}}, greska: "ponavlja"},
		"druga redom":        {ulazi: []string{"potok-vrh", "potok-drugi"}, rezerve: [][]string{{"potok-drugi", "potok-vrh"}}, greska: "ponavlja"},
		"dvije iste rezerve": {ulazi: []string{"potok-vrh"}, rezerve: [][]string{{"nema"}, {"nema"}}, greska: "ponavlja"},
	} {
		t.Run(ime, func(t *testing.T) {
			z := zapisZaPripremu(r.ulazi...)
			z.Racun.Rezerve = r.rezerve
			if _, err := PripremiLetvu(arhiva, baza, z, OkvirModela{}); !porukaIma(err, r.greska) {
				t.Errorf("priprema: želim %q, a dobio %v", r.greska, err)
			}
			upis := zapisPotoka("potok-dole", r.ulazi[0], 2)
			upis.Racun.Ulazi, upis.Racun.Rezerve = r.ulazi, r.rezerve
			if err := SpremiZapisLetve(baza, upis, "", OkvirModela{}); !porukaIma(err, r.greska) {
				t.Errorf("spremanje: želim %q, a dobio %v", r.greska, err)
			}
		})
	}
	// Ista letva u rezervi s drugim društvom nije ponavljanje.
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	z.Racun.Rezerve = [][]string{{"potok-vrh", "potok-drugi"}}
	if err := z.Provjeri(OkvirModela{}); err != nil {
		t.Errorf("rezerva s drugim društvom: %v", err)
	}
}

// Kad je voda u isključenoj razini, a letva u satu izdanja nije izmjerena, poruka to
// kaže, a ne da nema ulaza.
func TestIskljucenaRazinaKazeZastoNemaPrognoze(t *testing.T) {
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji.Add(-5*time.Hour), 48)
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	racuna, iskljucena := pojasPotoka("potok-dole", "potok-vrh", 2), pojasPotoka("potok-dole", "potok-vrh", 2)
	racuna.Do, iskljucena.Od, iskljucena.Nepovezan = 0, 0, true
	z.Brojevi.Inacice = [][]Pojas{{racuna, iskljucena}}
	upisiZapis(t, o.Baza, z)
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := i.BezPrognoze["potok-dole"]; !porukaIma(err, "isključen") {
		t.Errorf("razlog: %v", err)
	}
	if err := bezRedaka([]Pojas{racuna}); !porukaIma(err, "nema ulaza") {
		t.Errorf("letva bez isključene razine: %v", err)
	}
}

// Učenje broji samo sate koji su ušli u račun. Sporedni ulaz kraći od glavnog sužava i
// vrijeme i vodu: visoka voda prije njegova prvog sata ne smije stajati u rasponu.
func TestUcenjeSamoSatiRacuna(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	const pocetak = int64(3000)
	arhiva := arhivaPotoka(t, map[string]map[int64]float64{
		"potok-kratki:vodostaj": satniNiz(pocetak, satiPotoka, valPritoka),
		"potok-dole:vodostaj": satniNiz(0, satiPotoka, func(h int64) float64 {
			if h < pocetak {
				return 2*valUlaza(h-5) + 1003
			}
			return 2*valUlaza(h-5) + 3
		}),
	})
	p, err := PripremiLetvu(arhiva, otvoriProbnu(t, "p.db"), zapisZaPripremu("potok-vrh", "potok-kratki"), OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	sati := 0
	for _, r := range p.Zapis.Brojevi.Inacice[0] {
		sati += r.Sati
	}
	u := p.Ucenje
	if u.Sati != sati || u.Sati > int(satiPotoka-pocetak) {
		t.Errorf("učio %d sati, razine %d, a oba niza imaju najviše %d", u.Sati, sati, satiPotoka-pocetak)
	}
	if u.OdSata < pocetak || u.Do >= 1000 {
		t.Errorf("učenje %+v seže preko sati sporednog ulaza", u)
	}
}

// Letva glavnog lanca i račun u krugu odbijaju se kao i pri spremanju. Dok traje drugo
// namještanje priprema se ne pokreće, a greška se javlja.
func TestPripremaOdbija(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	upisiZapis(t, baza, zapisPotoka("potok-vrh", "potok-dole", 2))

	if _, err := PripremiLetvu(arhiva, baza, zapisZaPripremu("potok-vrh"), OkvirModela{}); !porukaIma(err, "krug") {
		t.Errorf("krug kroz drugi zapis: %v", err)
	}
	z := zapisZaPripremu("potok-drugi")
	z.Letva = Tokovi[0].Racuni[0].Letva
	if _, err := PripremiLetvu(nil, baza, z, OkvirModela{}); !porukaIma(err, "glavnom lancu") {
		t.Errorf("letva glavnog lanca: %v", err)
	}
	z = zapisZaPripremu("potok-drugi")
	if _, err := PripremiLetvu(arhiva, baza, z, OkvirModela{ImaSektor: func(string) bool { return false }}); !porukaIma(err, "sektor") {
		t.Errorf("bez sektora: %v", err)
	}
	z.Racun.Ulazi = nil
	if _, err := PripremiLetvu(arhiva, baza, z, OkvirModela{}); !porukaIma(err, "nijedan ulaz") {
		t.Errorf("bez ulaza: %v", err)
	}
	z.Racun.Ulazi = []string{"kratki"}
	if _, err := PripremiLetvu(arhiva, baza, z, OkvirModela{}); !porukaIma(err, "samo 0") {
		t.Errorf("ulaz bez niza: %v", err)
	}

	namjestanjeTraje.Lock()
	_, err := PripremiLetvu(arhiva, baza, zapisZaPripremu("potok-drugi"), OkvirModela{})
	namjestanjeTraje.Unlock()
	if !porukaIma(err, "već traje") {
		t.Errorf("dok traje namještanje: %v", err)
	}

	zatvorena := otvoriProbnu(t, "z.db")
	if err := zatvorena.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := PripremiLetvu(arhiva, zatvorena, zapisZaPripremu("potok-drugi"), OkvirModela{}); err == nil {
		t.Error("baza prognoza se ne da pročitati, a greške nema")
	}
	glavne := otvoriProbnu(t, "m.db")
	if _, err := glavne.Exec(`DROP TABLE model_letve; CREATE TABLE model_letve (letva TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := PripremiLetvu(arhiva, glavne, zapisZaPripremu("potok-drugi"), OkvirModela{}); err == nil {
		t.Error("zapis modela se ne da pročitati, a greške nema")
	}
}

// Isključena razina znači da letva ne slijedi ulaz pri svakoj vodi, kao letve iz
// SamoPovezane, pa joj se pogreška ne nosi kao stalna.
func TestIskljucenaRazinaBezStalnogDijela(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 24)
	postavi(t, &StalniIspravakSati, 72)
	postavi(t, &StalniSamoSkladan, true)
	const sada = int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	donja := Izvor{Letva: "donja", Velicina: "vodostaj"}
	razine := []Pojas{pojasIz("donja", "gornja", 0, 0, 100), pojasIz("donja", "gornja", 0, 100, 300)}
	mjereno := map[Izvor]Niz{gornja: ravanNiz(800, sada, 150, 0), donja: ravanNiz(800, sada, 180, 0)}
	na48 := func(ps []Pojas) float64 {
		t.Helper()
		izdane, err := NovoRacunalo(map[string][]Pojas{"donja": ps}, mjereno, sada).Prognoziraj("donja", 48, "proba")
		if err != nil || len(izdane) != 49 {
			t.Fatalf("doseg %d, %v", len(izdane), err)
		}
		return izdane[48].Vrijednost
	}
	if x := na48(razine); math.Abs(x-180) > 1e-9 {
		t.Errorf("bez isključene razine na +48 h %g, želim 180: pogreška je stalna", x)
	}
	razine[0].Nepovezan = true
	if want, x := 150+30*math.Exp2(-48.0/24), na48(razine); math.Abs(x-want) > 1e-9 {
		t.Errorf("s isključenom razinom na +48 h %g, želim %g: ispravak blijedi", x, want)
	}
}

// Zapis i pri čitanju traži postotke od 0 do 100.
func TestPostociRazina(t *testing.T) {
	for _, s := range []struct {
		p    []float64
		zeli bool
	}{
		{nil, true}, {[]float64{0, 100}, true}, {[]float64{0, 33, 67, 100}, true},
		{[]float64{50}, false}, {[]float64{10, 100}, false}, {[]float64{0, 90}, false}, {[]float64{0, 50, 50, 100}, false},
	} {
		if postociRazina(s.p) != s.zeli {
			t.Errorf("%v: želim %v", s.p, s.zeli)
		}
	}
}

// Priprema prima zapis u obliku u kojem stoji u bazi.
func TestPripremaPrimaDoslovanOblik(t *testing.T) {
	var z ZapisLetve
	if err := json.Unmarshal([]byte(`{"oblik":1,"letva":"potok-dole","metoda":"lanac","velicina":"vodostaj",
		"racun":{"ulazi":["potok-vrh"],"razine":{"postoci":[0,50,100],"granice":null,"iskljucene":[]}}}`), &z); err != nil {
		t.Fatal(err)
	}
	postavi(t, &NajveciPomak, 8)
	p, err := PripremiLetvu(arhivaPotoka(t, nil), otvoriProbnu(t, "p.db"), z, OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	if in := p.Zapis.Brojevi.Inacice; len(in) != 1 || len(in[0]) != 2 {
		t.Errorf("inačice %+v", in)
	}
}
