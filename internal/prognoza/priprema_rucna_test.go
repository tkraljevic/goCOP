package prognoza

import (
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Upisano kašnjenje razine vrijedi umjesto namještenog, a ostale razine se
// namještaju kao i dosad. Kašnjenje za razinu koje nema, ili dvaput za istu,
// odbija se.
func TestRucnoKasnjenjeRazine(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	z := zapisZaPripremu("potok-vrh")
	z.Racun.Rucno.Kasnjenje = []RucnoKasnjenje{{Razina: 0, Sati: 7}}
	p, err := PripremiLetvu(arhiva, baza, z, OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	razine := p.Zapis.Brojevi.Inacice[0]
	if razine[0].Ulazi[0].PomakH != 7 {
		t.Errorf("prva razina: kašnjenje %d, upisano 7", razine[0].Ulazi[0].PomakH)
	}
	for _, r := range razine[1:] {
		if r.Ulazi[0].PomakH != 5 {
			t.Errorf("razina od %.0f: kašnjenje %d, namješteno je 5", r.Od, r.Ulazi[0].PomakH)
		}
	}
	if err := SpremiZapisLetve(baza, p.Zapis, "otisak", OkvirModela{}); err != nil {
		t.Errorf("rezultat se ne da spremiti: %v", err)
	}

	for poruka, kasnjenja := range map[string][]RucnoKasnjenje{
		"razinu 9, a račun ima 4": {{Razina: 8, Sati: 3}},
		"upisano je dvaput":       {{Razina: 1, Sati: 3}, {Razina: 1, Sati: 4}},
	} {
		z.Racun.Rucno.Kasnjenje = kasnjenja
		if _, err := PripremiLetvu(arhiva, baza, z, OkvirModela{}); !porukaIma(err, poruka) {
			t.Errorf("%s: %v", poruka, err)
		}
	}
}

// Letva s malo arhive dobiva račun iz upisanog kašnjenja i nagiba: jedna razina,
// odsječak iz zajedničkih sati i napomena da račun nije provjeren.
func TestRucniRacunBezDostaArhive(t *testing.T) {
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{
		"potok-vrh:vodostaj":  satniNiz(0, 300, valUlaza),
		"potok-dole:vodostaj": satniNiz(100, 300, func(h int64) float64 { return 2*valUlaza(h-5) + 3 }),
	})
	baza := otvoriProbnu(t, "p.db")
	p, err := PripremiLetvu(arhiva, baza, rucniZapis(5, 2), OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	in := p.Zapis.Brojevi.Inacice
	if len(in) != 1 || len(in[0]) != 1 {
		t.Fatalf("inačice %+v, želim jednu razinu", in)
	}
	r := in[0][0]
	if math.Abs(r.Odsjecak-3) > 0.01 || r.Ulazi[0].PomakH != 5 || r.Ulazi[0].Nagib != 2 || r.Sati != 200 || r.Rasap > 0.01 {
		t.Errorf("razina %+v", r)
	}
	if len(p.Napomene) != 1 || !strings.Contains(p.Napomene[0], "Račun nije provjeren") || !strings.Contains(p.Napomene[0], "200 sati") {
		t.Errorf("napomene %v", p.Napomene)
	}
	if p.Ucenje.Sati != 200 || p.Zapis.Racun.Doseg != 5 {
		t.Errorf("učenje %+v, doseg %d", p.Ucenje, p.Zapis.Racun.Doseg)
	}
	if err := SpremiZapisLetve(baza, p.Zapis, "otisak", OkvirModela{}); err != nil {
		t.Errorf("rezultat se ne da spremiti: %v", err)
	}
}

func rucniZapis(kasnjenje int, nagib float64) ZapisLetve {
	z := zapisZaPripremu("potok-vrh")
	z.Racun.Rucno = RucnoLetve{Kasnjenje: []RucnoKasnjenje{{Razina: 0, Sati: kasnjenje}}, Nagib: &nagib}
	return z
}

// Ručni račun traži jedan ulaz, bez rezervi i razina, s jednim kašnjenjem, i
// barem jedan sat u kojem su izmjerene obje letve.
func TestRucniRacunOdbija(t *testing.T) {
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{
		"potok-vrh:vodostaj":   satniNiz(0, 100, valUlaza),
		"potok-drugi:vodostaj": satniNiz(0, 100, valUlaza),
		"potok-dole:vodostaj":  satniNiz(500, 600, valUlaza),
	})
	baza := otvoriProbnu(t, "p.db")
	slucajevi := map[string]func(*ZapisLetve){
		"točno jedan ulaz":      func(z *ZapisLetve) { z.Racun.Ulazi = append(z.Racun.Ulazi, "potok-drugi") },
		"nema rezervu":          func(z *ZapisLetve) { z.Racun.Rezerve = [][]string{{"potok-drugi"}} },
		"makni razine":          func(z *ZapisLetve) { z.Racun.Razine.Postoci = []float64{0, 50, 100} },
		"jedno za cijeli račun": func(z *ZapisLetve) { z.Racun.Rucno.Kasnjenje = nil },
		"nemaju nijedan sat":    func(*ZapisLetve) {},
	}
	for poruka, izmijeni := range slucajevi {
		z := rucniZapis(5, 2)
		izmijeni(&z)
		if _, err := PripremiLetvu(arhiva, baza, z, OkvirModela{}); !porukaIma(err, poruka) {
			t.Errorf("%s: %v", poruka, err)
		}
	}
}

// Arhiva koja se ne da pročitati prekida ručni račun greškom arhive.
func TestRucniRacunBezArhive(t *testing.T) {
	arhiva, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = arhiva.Close() }() // samo čitanje u testu
	if _, err := PripremiLetvu(arhiva, otvoriProbnu(t, "p.db"), rucniZapis(5, 2), OkvirModela{}); !errors.Is(err, ErrArhiva) {
		t.Errorf("arhiva bez spojenih nizova: %v", err)
	}
}
