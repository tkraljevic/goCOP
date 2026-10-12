package prognoza

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"
)

// RazaberiRacun čita "letva = ulaz + ulaz": razmaci oko imena ne smetaju,
// prazni ulazi se preskaču, a zapis bez znaka jednakosti ili bez ijednog
// ulaza je greška.
func TestRazaberiRacun(t *testing.T) {
	for _, s := range []struct {
		zapis  string
		want   Racun
		greska bool
	}{
		{zapis: "aljmas = batina + belisce", want: Racun{Letva: "aljmas", Ulazi: []string{"batina", "belisce"}}},
		{zapis: " vrbovka:vodostaj=terezino-polje:protok ", want: Racun{Letva: "vrbovka:vodostaj", Ulazi: []string{"terezino-polje:protok"}}},
		{zapis: "dalj = aljmas + + ", want: Racun{Letva: "dalj", Ulazi: []string{"aljmas"}}},
		{zapis: "aljmas batina", greska: true},
		{zapis: "aljmas = + ", greska: true},
	} {
		got, err := RazaberiRacun(s.zapis)
		if s.greska {
			if err == nil {
				t.Errorf("%q: bez greške, dobiveno %+v", s.zapis, got)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, s.want) {
			t.Errorf("%q: %+v, %v; želim %+v", s.zapis, got, err, s.want)
		}
	}
}

// RastaviLetvu uzima veličinu iz zapisa kad je navedena, inače iz Velicine;
// letva kojoj se veličina ne zna je greška.
func TestRastaviLetvu(t *testing.T) {
	if l, v, err := RastaviLetvu("vrbovka:protok"); err != nil || l != "vrbovka" || v != "protok" {
		t.Errorf("navedena veličina: %q %q %v", l, v, err)
	}
	if l, v, err := RastaviLetvu("batina"); err != nil || l != "batina" || v == "" || v != Velicine["batina"] {
		t.Errorf("veličina iz Velicine: %q %q %v", l, v, err)
	}
	if _, _, err := RastaviLetvu("nepoznata-letva"); err == nil || !strings.Contains(err.Error(), "nepoznata-letva:vodostaj") {
		t.Errorf("nepoznata letva: %v — greška mora reći kako se veličina navodi", err)
	}
}

// Jedinica granica i nagiba: protok je u m³/s, sve ostalo u cm; nagib nosi
// jedinicu samo kad cilj i ulaz nisu iste veličine.
func TestJediniceIspisa(t *testing.T) {
	for _, s := range []struct{ cilj, ulaz, want string }{
		{"vodostaj", "vodostaj", ""},
		{"protok", "protok", ""},
		{"vodostaj", "protok", "cm/m³/s"},
		{"protok", "vodostaj", "m³/s/cm"},
	} {
		if got := poJedinici(s.cilj, s.ulaz); got != s.want {
			t.Errorf("%s ← %s: %q, želim %q", s.cilj, s.ulaz, got, s.want)
		}
	}
}

// ispisiPojase letvi koja slijedi ulaz samo pri dovoljnoj vodi označi
// nepovezanima pojase sa slabim slaganjem — u samim pojasima, jer se tako i
// spremaju — i to napiše; ostalim letvama slabo slaganje ne dira.
func TestIspisiPojaseOznaciNepovezane(t *testing.T) {
	pojasi := func(letva string) []Pojas {
		ulaz := []Ulaz{{Letva: "batina", Velicina: "vodostaj", PomakH: 4, Sirina: 3, Nagib: 0.8}}
		return []Pojas{
			{Letva: letva, Velicina: "vodostaj", Od: 0, Do: 120, R: 0.15, Rasap: 9, Sati: 3000, Ulazi: ulaz},
			{Letva: letva, Velicina: "vodostaj", Od: 120, Do: 600, R: 0.95, Rasap: 4, Sati: 2500, Ulazi: ulaz},
		}
	}
	var w bytes.Buffer
	tikves := pojasi("tikves")
	ispisiPojase(&w, tikves, "tikves")
	if !tikves[0].Nepovezan || tikves[1].Nepovezan {
		t.Errorf("Tikveš: nepovezan %v/%v, želim samo donji pojas", tikves[0].Nepovezan, tikves[1].Nepovezan)
	}
	redci := strings.Split(strings.TrimRight(w.String(), "\n"), "\n")
	if len(redci) != 4 {
		t.Fatalf("ispis od %d redaka, želim pojas i ulaz za oba pojasa:\n%s", len(redci), w.String())
	}
	if !strings.Contains(redci[0], "nepovezan — ne prognozira se") || strings.Contains(redci[2], "nepovezan") {
		t.Errorf("oznaka nepovezanog pojasa:\n%s", w.String())
	}
	if !strings.Contains(redci[0], "cm") || !strings.Contains(redci[0], "(3000 sati)") || !strings.Contains(redci[1], "batina") ||
		!strings.Contains(redci[1], "- 4 h") || !strings.Contains(redci[1], "prozor  3 h") {
		t.Errorf("ispis pojasa:\n%s", w.String())
	}

	w.Reset()
	aljmas := pojasi("aljmas")
	ispisiPojase(&w, aljmas, "aljmas")
	if aljmas[0].Nepovezan || strings.Contains(w.String(), "nepovezan") {
		t.Errorf("Aljmaš nije među letvama koje se odvajaju od ulaza:\n%s", w.String())
	}
}

// arhivaIzmisljeneVeze je satna arhiva u kojoj "cilj" slijedi "ulaz" pet sati
// kasnije: cilj = 2·ulaz + 3, oboje u protoku, šest tisuća sati.
func arhivaIzmisljeneVeze(t *testing.T) map[string]map[int64]float64 {
	t.Helper()
	ulaz, cilj := map[int64]float64{}, map[int64]float64{}
	for h := int64(0); h < 6000; h++ {
		ulaz[h] = 100 + 40*math.Sin(float64(h)/37) + 15*math.Sin(float64(h)/211)
	}
	for h := int64(5); h < 6000; h++ {
		cilj[h] = 2*ulaz[h-5] + 3
	}
	return map[string]map[int64]float64{"ulaz:protok": ulaz, "cilj:protok": cilj}
}

// Rezervna inačica namješta se kao i glavna, ali svaki njezin pojas nosi
// svoj redni broj inačice; ulaz kojem se ne zna veličina ili cilj bez niza
// javljaju grešku umjesto pojasa.
func TestNamjestiInacicu(t *testing.T) {
	postavi(t, &NajmanjeSatiPojasa, 200)
	arhiva := arhivaSatnih(t, arhivaIzmisljeneVeze(t))

	pojasi, err := namjestiInacicu(arhiva, "cilj", "protok", []string{"ulaz:protok"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(pojasi) == 0 {
		t.Fatal("nijedan pojas")
	}
	for _, p := range pojasi {
		if p.Inacica != 2 || p.Letva != "cilj" || len(p.Ulazi) != 1 || p.Ulazi[0].Letva != "ulaz" {
			t.Errorf("pojas %.0f–%.0f: %+v", p.Od, p.Do, p)
			continue
		}
		if p.Ulazi[0].PomakH != 5 || math.Abs(p.Ulazi[0].Nagib-2) > 0.01 || p.R < 0.999 {
			t.Errorf("pojas %.0f–%.0f: pomak %d, nagib %.3f, r %.4f — želim 5, 2, 1",
				p.Od, p.Do, p.Ulazi[0].PomakH, p.Ulazi[0].Nagib, p.R)
		}
	}

	if _, err := namjestiInacicu(arhiva, "cilj", "protok", []string{"nepoznata-letva"}, 1); err == nil {
		t.Error("ulaz bez poznate veličine prošao je bez greške")
	}
	if _, err := namjestiInacicu(arhiva, "nema-niza", "protok", []string{"ulaz:protok"}, 1); err == nil {
		t.Error("cilj bez niza prošao je bez greške")
	}
}

// ProbajRacun namjesti i ispiše jednu postavku, a ništa ne sprema. Dok traje
// namještanje lanca, ne počinje; krivo napisanu postavku vraća kao grešku, a
// letvu koju ne može namjestiti samo napiše.
func TestProbajRacun(t *testing.T) {
	postavi(t, &NajmanjeSatiPojasa, 200)
	arhiva := arhivaSatnih(t, arhivaIzmisljeneVeze(t))

	var w bytes.Buffer
	if err := ProbajRacun(arhiva, "cilj:protok = ulaz:protok", &w); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.String(), "cilj ← ulaz (m³/s)") || !strings.Contains(w.String(), "ulaz") || !strings.Contains(w.String(), "- 5 h") {
		t.Errorf("ispis probe:\n%s", w.String())
	}

	if err := ProbajRacun(arhiva, "cilj ulaz", &w); err == nil {
		t.Error("postavka bez znaka jednakosti prošla je bez greške")
	}

	w.Reset()
	if err := ProbajRacun(arhiva, "nepoznata-letva = ulaz:protok", &w); err != nil {
		t.Errorf("letva bez veličine: %v — treba je samo napisati", err)
	}
	if !strings.Contains(w.String(), "nepoznata-letva") {
		t.Errorf("letva bez veličine nije napisana: %q", w.String())
	}

	w.Reset()
	if err := ProbajRacun(arhiva, "nema-niza:protok = ulaz:protok", &w); err != nil {
		t.Errorf("letva bez niza: %v", err)
	}
	if !strings.Contains(w.String(), "samo 0 satnih vrijednosti") {
		t.Errorf("letva bez niza: %q", w.String())
	}

	namjestanjeTraje.Lock()
	err := ProbajRacun(arhiva, "cilj:protok = ulaz:protok", &w)
	namjestanjeTraje.Unlock()
	if err == nil || !strings.Contains(err.Error(), "već traje") {
		t.Errorf("proba usred namještanja lanca: %v", err)
	}
}
