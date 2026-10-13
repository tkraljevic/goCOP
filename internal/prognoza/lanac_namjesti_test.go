package prognoza

import (
	"bytes"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"
)

// tokoviProbe zamjenjuje lanac za jedan test zadanim računima, svima na
// jednom toku.
func tokoviProbe(t *testing.T, racuni ...Racun) {
	t.Helper()
	postavi(t, &Tokovi, []struct {
		Ime    string
		Racuni []Racun
	}{{"Proba", racuni}})
}

// granicaOcjeneElektrane je sat od kojega namještanje lanca ocjenjuje model
// ispuštanja kad je NamjestiDo postavljen na granicuElektrane.
const (
	granicaOcjeneElektrane = 1200
	granicaElektrane       = granicaOcjeneElektrane + 2*365*24
)

// istjecanjeElektrane je satni protok izmišljene elektrane: dnevni ritam
// (navečer više, noću manje) na neprekinutom bloku od 1500 sati, pa
// pojedinačni sati svakih 13 sati, da arhiva ima godinu dana sati, a učenje
// ostane kratko — sat bez susjeda tri i 24 sata prije ne daje uzorak.
func istjecanjeElektrane() map[int64]float64 {
	q := map[int64]float64{}
	ritam := func(h int64) float64 { return 300 + 100*math.Sin(2*math.Pi*float64((h+1)%24)/24) }
	for h := int64(0); h < 1500; h++ {
		q[h] = ritam(h)
	}
	for i := int64(0); i < 7300; i++ {
		h := 1600 + 13*i
		q[h] = ritam(h)
	}
	return q
}

// arhivaLanca je arhiva s izmišljenom vezom cilj ← ulaz (vidi
// arhivaIzmisljeneVeze), drugim ulazom iste vode i izmišljenom elektranom s
// dvije uzvodne elektrane.
func arhivaLanca(t *testing.T) *sql.DB {
	t.Helper()
	nizovi := arhivaIzmisljeneVeze(t)
	drugi := map[int64]float64{}
	for h, v := range nizovi["ulaz:protok"] {
		drugi[h] = v + 10
	}
	nizovi["drugi:protok"] = drugi
	nizovi["elektrana:protok"] = istjecanjeElektrane()
	nizovi["gornja:protok"] = satniNiz(0, 1500, func(h int64) float64 { return 200 + 30*math.Sin(float64(h)/17) })
	nizovi["srednja:protok"] = satniNiz(0, 1500, func(h int64) float64 { return 150 + 20*math.Cos(float64(h)/29) })
	return arhivaSatnih(t, nizovi)
}

// postaviLanacProbe priprema lanac s jednim računom koji se da namjestiti i
// jednim kojem se ne zna veličina te dvije elektrane, jednu s nizom i jednu
// bez njega.
func postaviLanacProbe(t *testing.T) {
	t.Helper()
	postavi(t, &NajmanjeSatiPojasa, 200)
	postavi(t, &NajveciPomak, 8)
	postavi(t, &NamjestiDo, int64(granicaElektrane))
	tokoviProbe(t,
		Racun{Letva: "cilj:protok", Ulazi: []string{"ulaz:protok"}},
		Racun{Letva: "nepoznata", Ulazi: []string{"ulaz:protok"}})
	postavi(t, &Rezerve, map[string][][]string{})
	postavi(t, &Operateri, []Operater{{Letva: "elektrana", Ulazi: []string{"gornja", "srednja"}}, {Letva: "bez-niza"}})
}

// Namještanje lanca namjesti svaki račun i njegove rezerve, spremi sve
// pojase odjednom i nauči modele ispuštanja elektrana. Račun koji se ne da
// namjestiti, rezerva bez niza i elektrana bez niza samo se napišu; tijek se
// javlja prije svakog koraka. Proba ništa ne sprema, a greška spremanja se
// vraća.
func TestNamjestiLanac(t *testing.T) {
	postaviLanacProbe(t)
	arhiva := arhivaLanca(t)

	t.Run("sprema pojase i modele ispuštanja", func(t *testing.T) {
		postavi(t, &Rezerve, map[string][][]string{"cilj": {{"drugi:protok"}, {"nema-niza:protok"}}})
		baza := otvoriProbnu(t, "p.db")
		var w bytes.Buffer
		var koraci []string
		n, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{Dnevnik: &w,
			Korak: func(sto string, gotovo, ukupno int) {
				koraci = append(koraci, fmt.Sprintf("%s %d/%d", sto, gotovo, ukupno))
			}})
		if err != nil {
			t.Fatalf("%v\n%s", err, w.String())
		}
		ispis := w.String()

		in, err := InaciceLetve(baza, "cilj")
		if err != nil || len(in) != 2 {
			t.Fatalf("inačice cilja: %d, %v\n%s", len(in), err, ispis)
		}
		if n != len(in[0])+len(in[1]) {
			t.Errorf("vraćeno %d pojasa, u bazi %d + %d", n, len(in[0]), len(in[1]))
		}
		if in[0][0].Ulazi[0].Letva != "ulaz" || in[1][0].Ulazi[0].Letva != "drugi" || in[1][0].Inacica != 1 {
			t.Errorf("glavna %+v, rezerva %+v", in[0][0], in[1][0])
		}
		if p, _ := SviPojasi(baza); len(p) != 1 {
			t.Errorf("spremljene letve %v, želim samo cilj", p)
		}

		for _, dio := range []string{
			"\n— Proba —\n",
			"cilj ← ulaz (m³/s)",
			"za nepoznata se ne zna u čemu se vodi",
			"   rezerva 1:\n",
			"   rezerva 2 (nema-niza:protok): cilj: nema-niza ima samo 0 satnih vrijednosti u protok",
			fmt.Sprintf("\nspremljeno %d pojasa\n", n),
			"\nmodel ispuštanja elektrana ← gornja + srednja, prag velike vode ",
			"  obična voda ",
			"  velika voda ",
			"model ispuštanja bez-niza: bez-niza: samo 0 sati protoka u arhivi",
		} {
			if !strings.Contains(ispis, dio) {
				t.Errorf("u ispisu nema %q:\n%s", dio, ispis)
			}
		}
		// Obična voda dnevnog ritma: model zna sat, postojanost ne, pa se
		// model koristi i redak to označi zvjezdicom.
		var obicna string
		for _, r := range strings.Split(ispis, "\n") {
			if strings.HasPrefix(r, "  obična voda") {
				obicna = r
			}
		}
		if !strings.Contains(obicna, "*") {
			t.Errorf("obična voda bez ijednog dosega na kojem model pobjeđuje: %q", obicna)
		}

		modeli, err := UcitajOperatere(baza)
		if err != nil || len(modeli) != 1 || modeli["elektrana"] == nil || !modeli["elektrana"].Koristi[0][12] ||
			strings.Join(modeli["elektrana"].Ulazi, ",") != "gornja,srednja" {
			t.Errorf("modeli ispuštanja iz baze: %+v, %v", modeli, err)
		}

		want := []string{"namještam cilj:protok 0/2", "namještam nepoznata 1/2",
			"model ispuštanja elektrana 2/2", "model ispuštanja bez-niza 2/2"}
		if strings.Join(koraci, "|") != strings.Join(want, "|") {
			t.Errorf("koraci %q, želim %q", koraci, want)
		}
	})

	t.Run("proba ništa ne sprema", func(t *testing.T) {
		baza := otvoriProbnu(t, "p.db")
		var w bytes.Buffer
		n, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{Probno: true, Dnevnik: &w})
		if err != nil || n != 0 {
			t.Fatalf("proba: %d, %v", n, err)
		}
		if !strings.Contains(w.String(), "\nproba — ništa nije spremljeno; ") || !strings.Contains(w.String(), " pojasa bi ušlo\n") {
			t.Errorf("ispis probe:\n%s", w.String())
		}
		if strings.Contains(w.String(), "model ispuštanja") {
			t.Errorf("proba je učila modele ispuštanja:\n%s", w.String())
		}
		if p, err := SviPojasi(baza); err != nil || len(p) != 0 {
			t.Errorf("proba je spremila pojase: %v, %v", p, err)
		}
	})

	// Kad se pojasi ne daju spremiti, elektrane se ne uče.
	t.Run("greška spremanja pojasa", func(t *testing.T) {
		baza := otvoriProbnu(t, "p.db")
		if _, err := baza.Exec(`DROP TABLE ulazi`); err != nil {
			t.Fatal(err)
		}
		var w bytes.Buffer
		n, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{Dnevnik: &w})
		if err == nil || n != 0 {
			t.Fatalf("bez tablice ulaza: %d, %v", n, err)
		}
		if strings.Contains(w.String(), "spremljeno") || strings.Contains(w.String(), "model ispuštanja") {
			t.Errorf("nakon neuspjelog spremanja:\n%s", w.String())
		}
	})

	// Kad se ne da spremiti model ispuštanja, pojasi ostaju spremljeni, a
	// vraća se i njihov broj i greška.
	t.Run("greška spremanja modela ispuštanja", func(t *testing.T) {
		baza := otvoriProbnu(t, "p.db")
		if _, err := baza.Exec(`DROP TABLE operater`); err != nil {
			t.Fatal(err)
		}
		n, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{})
		if err == nil {
			t.Fatal("model ispuštanja bez tablice spremljen je bez greške")
		}
		p, perr := ZaLetvu(baza, "cilj")
		if perr != nil || n == 0 || n != len(p) {
			t.Errorf("vraćeno %d pojasa, u bazi %d (%v)", n, len(p), perr)
		}
	})
}

// Kad se nijedan račun ne da namjestiti, zatečeni pojasi ostaju: lanac
// bez ijednog pojasa bio bi gori od staroga.
func TestNamjestiLanacBezIjednogRacunaNeBriseStaro(t *testing.T) {
	tokoviProbe(t, Racun{Letva: "nepoznata", Ulazi: []string{"ulaz:protok"}})
	arhiva, _ := arhivaUDatoteci(t)
	baza := otvoriProbnu(t, "p.db")
	if err := Spremi(baza, dvaPojasa(), "staro"); err != nil {
		t.Fatal(err)
	}
	n, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{})
	if err == nil || n != 0 || !strings.Contains(err.Error(), "nijedan račun lanca se nije dao namjestiti") {
		t.Fatalf("lanac bez računa: %d, %v", n, err)
	}
	if p, err := SviPojasi(baza); err != nil || len(p["aljmas"]) != 2 || len(p["batina"]) != 1 {
		t.Errorf("zatečeni pojasi nakon neuspjeha: %+v, %v", p, err)
	}
}

// Dok namještanje lanca traje, drugo ne počinje.
func TestNamjestiLanacNePocinjeDvaput(t *testing.T) {
	namjestanjeTraje.Lock()
	n, err := NamjestiLanac(nil, nil, OpcijeNamjestanja{})
	namjestanjeTraje.Unlock()
	if err == nil || n != 0 || !strings.Contains(err.Error(), "već traje") {
		t.Errorf("drugo namještanje usred prvoga: %d, %v", n, err)
	}
}

// Rijetkim letvama, koje se očitavaju jednom dnevno, namještanje spušta
// granice samo za njihov račun i vraća ih kad završi. Isti niz pod
// imenom koje nije rijetko ne prolazi.
func TestNamjestiRacunRijetkeLetve(t *testing.T) {
	batina, rijetka := map[int64]float64{}, map[int64]float64{}
	for h := int64(0); h < 6000; h++ {
		batina[h] = 300 + 120*math.Sin(float64(h)/37) + 40*math.Sin(float64(h)/211)
	}
	for h := int64(10); h < 6000; h += 10 {
		rijetka[h] = 0.95*batina[h-4] + 162
	}
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{
		"batina:vodostaj": batina, "dunav-zmajevac:vodostaj": rijetka, "obicna:vodostaj": rijetka})
	postavi(t, &NajveciPomak, 8)

	var w bytes.Buffer
	pojasi := namjestiRacun(arhiva, Racun{Letva: "dunav-zmajevac", Ulazi: []string{"batina"}}, &w)
	if len(pojasi) == 0 {
		t.Fatalf("rijetka letva bez pojasa:\n%s", w.String())
	}
	for _, p := range pojasi {
		if p.Ulazi[0].PomakH != 4 || math.Abs(p.Ulazi[0].Nagib-0.95) > 0.01 {
			t.Errorf("pojas %.0f–%.0f: pomak %d, nagib %.3f — želim 4 i 0,95", p.Od, p.Do, p.Ulazi[0].PomakH, p.Ulazi[0].Nagib)
		}
	}
	if NajmanjeSati != 5000 || NajmanjeSatiPojasa != 2000 {
		t.Errorf("granice nakon rijetke letve %d i %d, želim vraćene 5000 i 2000", NajmanjeSati, NajmanjeSatiPojasa)
	}

	w.Reset()
	if p := namjestiRacun(arhiva, Racun{Letva: "obicna", Ulazi: []string{"obicna:vodostaj"}}, &w); p != nil {
		t.Errorf("letva kojoj se veličina ne zna dala je pojase: %+v", p)
	}
	if !strings.Contains(w.String(), "za obicna se ne zna u čemu se vodi") {
		t.Errorf("ispis: %q", w.String())
	}
	w.Reset()
	if p := namjestiRacun(arhiva, Racun{Letva: "obicna:vodostaj", Ulazi: []string{"batina"}}, &w); p != nil {
		t.Errorf("obična letva s rijetkim nizom dala je pojase: %+v", p)
	}
	if !strings.Contains(w.String(), "obicna: samo 599 satnih vrijednosti u vodostaj") {
		t.Errorf("ispis: %q", w.String())
	}
	w.Reset()
	if p := namjestiRacun(arhiva, Racun{Letva: "obicna:vodostaj", Ulazi: []string{"nepoznati-ulaz"}}, &w); p != nil {
		t.Errorf("ulaz kojem se veličina ne zna dao je pojase: %+v", p)
	}
	if !strings.Contains(w.String(), "za nepoznati-ulaz se ne zna u čemu se vodi") {
		t.Errorf("ispis: %q", w.String())
	}
}
