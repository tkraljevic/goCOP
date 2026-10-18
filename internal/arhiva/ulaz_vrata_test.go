package arhiva

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Zaključava popis slivova iz stabla: svaka mapa sliva u kojoj stoji barem
// jedna letva, poredano i bez ponavljanja. Datoteke uz slivove, mapa pristupa
// i datoteke unutar sliva nisu ni sliv ni letva.
func TestSlivoviIzStabla(t *testing.T) {
	koren := t.TempDir()
	for _, m := range []string{
		filepath.Join("dunav", "vukovar"),
		filepath.Join("dunav", "batina"),
		filepath.Join("drava", "osijek"),
		filepath.Join("PRISTUP-cvorovi", "osijek"),
		"sava",
	} {
		if err := os.MkdirAll(filepath.Join(koren, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"promjene-kote.csv", filepath.Join("dunav", "napomena.txt")} {
		if err := os.WriteFile(filepath.Join(koren, d), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	slivovi, err := Slivovi(koren)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"drava", "dunav"}; !slices.Equal(slivovi, want) {
		t.Errorf("slivovi %q, očekivano %q", slivovi, want)
	}
	letve, err := Letve(koren)
	if err != nil {
		t.Fatal(err)
	}
	if len(letve) != 3 || letve["vukovar"] != "dunav" || letve["batina"] != "dunav" || letve["osijek"] != "drava" {
		t.Errorf("letve sa slivom: %v", letve)
	}

	// bez zadanog stabla nema ni slivova, i to nije greška
	if s, err := Slivovi(""); err != nil || len(s) != 0 {
		t.Errorf("bez stabla: %q, %v", s, err)
	}
	// stablo kojeg nema jest greška
	if _, err := Slivovi(filepath.Join(koren, "nema")); err == nil {
		t.Error("nepostojeće stablo prošlo je bez greške")
	}
}

// Zaključava provjeru dijelova naziva prije upisa: svaki dio mora biti zadan,
// ne smije razlomiti ugovor o nazivu ni izaći iz mape, a veličina i vrsta
// moraju biti one koje gradnja razumije. Greška kaže koji dio ne valja.
func TestProvjeraDijelovaNaziva(t *testing.T) {
	for _, s := range []struct {
		ime                           string
		letva, izvor, velicina, vrsta string
		greska                        string
	}{
		{"sve ispravno", "vukovar", "cop", "vodostaj", "jutarnji", ""},
		{"kišomjer", "osijek", "letva-dhmz", "oborina", "satni", ""},
		{"bez letve", "", "cop", "vodostaj", "jutarnji", "letva nije zadana"},
		{"izvor samo razmaci", "vukovar", "   ", "vodostaj", "jutarnji", "izvor nije zadana"},
		{"donja crta u letvi", "vukovar_novi", "cop", "vodostaj", "jutarnji", "letva \"vukovar_novi\" ne smije"},
		{"kosa crta u izvoru", "vukovar", "cop/stari", "vodostaj", "jutarnji", "izvor \"cop/stari\" ne smije"},
		{"obrnuta kosa crta", "vukovar", `cop\stari`, "vodostaj", "jutarnji", "ne smije"},
		{"razmak u vrsti", "vukovar", "cop", "vodostaj", "jutarnji dnevni", "vrsta \"jutarnji dnevni\" ne smije"},
		{"nepoznata veličina", "vukovar", "cop", "kota", "jutarnji", "veličina \"kota\" nije jedna od"},
		{"nepoznata vrsta", "vukovar", "cop", "vodostaj", "tjedni", "vrsta \"tjedni\" nije jedna od"},
	} {
		err := ProvjeriDjelove(s.letva, s.izvor, s.velicina, s.vrsta)
		switch {
		case s.greska == "" && err != nil:
			t.Errorf("%s: odbijeno: %v", s.ime, err)
		case s.greska != "" && err == nil:
			t.Errorf("%s: prošlo, a nije smjelo", s.ime)
		case s.greska != "" && !strings.Contains(err.Error(), s.greska):
			t.Errorf("%s: greška %q ne sadrži %q", s.ime, err, s.greska)
		}
	}
}

// Zaključava zaglavlje datoteke: svaka veličina koju vrata primaju ima stupac
// s jedinicom, prvi stupac je datum kad izvor daje samo dan, a veličina koju
// program ne poznaje piše se svojim imenom.
func TestZaglavljeDatotekeNiza(t *testing.T) {
	for _, v := range Velicine {
		z := zaglavlje(v, nil)
		drugi := strings.TrimPrefix(z, "vrijeme_utc;")
		if drugi == z || drugi == v || strings.Contains(drugi, "-") {
			t.Errorf("%s: zaglavlje %q nema stupac s jedinicom", v, z)
		}
	}
	for _, s := range []struct {
		velicina string
		redci    []Redak
		want     string
	}{
		{"vodostaj", []Redak{{Vrijednost: 120}}, "vrijeme_utc;vodostaj_cm"},
		{"protok", []Redak{{PoDanu: true}}, "datum;protok_m3s"},
		{"visina-snijega", nil, "vrijeme_utc;visina_snijega_cm"},
		{"kota", nil, "vrijeme_utc;kota"},
	} {
		if got := zaglavlje(s.velicina, s.redci); got != s.want {
			t.Errorf("%s: %q, očekivano %q", s.velicina, got, s.want)
		}
	}
}

// Zaključava da upis koji ne uspije zapisati novu datoteku ne dira staru:
// mjerenje se ne može ponoviti, pa zatečeni niz mora ostati kakav je bio.
func TestNeuspjeliUpisCuvaStaruDatoteku(t *testing.T) {
	koren := t.TempDir()
	redci := []Redak{
		{Vrijeme: time.Date(2024, 3, 1, 7, 0, 0, 0, time.UTC), Vrijednost: 212},
		{Vrijeme: time.Date(2024, 3, 2, 7, 0, 0, 0, time.UTC), Vrijednost: 215},
	}
	put, err := Upisi(koren, "drava", "osijek", "cop", "vodostaj", "jutarnji", redci)
	if err != nil {
		t.Fatal(err)
	}
	prije, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	// Na mjestu privremene datoteke stoji mapa, pa se nova ne da napraviti.
	if err := os.Mkdir(put+".nova", 0o755); err != nil {
		t.Fatal(err)
	}
	novi := []Redak{{Vrijeme: redci[0].Vrijeme, Vrijednost: 999}, redci[1]}
	if _, err := Upisi(koren, "drava", "osijek", "cop", "vodostaj", "jutarnji", novi); err == nil {
		t.Fatal("upis je javio uspjeh iako nova datoteka nije zapisana")
	}
	poslije, err := os.ReadFile(put)
	if err != nil {
		t.Fatalf("stara datoteka je nestala: %v", err)
	}
	if string(poslije) != string(prije) {
		t.Errorf("stara datoteka promijenjena:\n%q\numjesto\n%q", poslije, prije)
	}
}

// Zaključava da upis javlja stari niz koji se nije dao maknuti. Novi je
// zapisan, ali bi gradnja stari pročitala uz njega kao dodatne godine istog
// niza; prije je to prolazilo bez ikakve poruke. Poruka kaže samo imena
// datoteka, bez mape i bez teksta sustava, koji ostaje u izvornoj grešci.
func TestStariNizKojiOstajeSeJavlja(t *testing.T) {
	koren := t.TempDir()
	// Na mjestu starog niza stoji mapa s datotekom, pa je brisanje ne miče.
	stari := filepath.Join(koren, "drava", "osijek", "osijek_cop_vodostaj_jutarnji_2023.csv")
	zapisiDatoteku(t, filepath.Join(stari, "unutra.txt"), "x")
	redci := []Redak{{Vrijeme: time.Date(2024, 3, 1, 7, 0, 0, 0, time.UTC), Vrijednost: 212}}
	put, err := Upisi(koren, "drava", "osijek", "cop", "vodostaj", "jutarnji", redci)
	greskaSadrzi(t, "stari niz", err, "stari osijek_cop_vodostaj_jutarnji_2023.csv nije se dao maknuti")
	var o *StariNizOstao
	if !errors.As(err, &o) || o.Err == nil {
		t.Fatalf("greška %v nije StariNizOstao s razlogom sustava", err)
	}
	if strings.Contains(err.Error(), koren) || strings.Contains(err.Error(), o.Err.Error()) {
		t.Errorf("poruka nosi mapu ili tekst sustava: %q", err)
	}
	if _, e := os.Stat(put); put == "" || e != nil {
		t.Errorf("novi niz nije zapisan: %q, %v", put, e)
	}
	if err := makniStari(filepath.Join(koren, "nema.csv"), put); err != nil {
		t.Errorf("niz kojeg više nema javljen je kao greška: %v", err)
	}
}

// Zaključava da stari niz koji je ostao ne zaustavlja posao: JaviOstatak ga
// ispiše kao upozorenje, SkupiOstatak ga skupi, a obje vrate nil. Svaka
// druga greška upisa vraća se kakva jest.
func TestOstatakNijeNeuspjehUpisa(t *testing.T) {
	ostao := errors.Join(&StariNizOstao{Novi: "b_2024.csv", Stari: "b_2023.csv", Err: fs.ErrPermission},
		&StariNizOstao{Novi: "b_2024.csv", Stari: "b_2022.csv", Err: fs.ErrPermission})
	kvar := errors.New("disk je pun")
	var dnevnik strings.Builder
	if err := JaviOstatak(ostao, &dnevnik); err != nil {
		t.Errorf("stari niz koji je ostao zaustavio je posao: %v", err)
	}
	if d := dnevnik.String(); !strings.HasPrefix(d, "upozorenje: novi niz b_2024.csv je zapisan") || !strings.Contains(d, "b_2022.csv") {
		t.Errorf("upozorenje u dnevniku: %q", d)
	}
	if err := JaviOstatak(ostao, nil); err != nil {
		t.Errorf("bez dnevnika: %v", err)
	}
	var ostaci error
	if err := SkupiOstatak(ostao, &ostaci); err != nil || !errors.Is(ostaci, fs.ErrPermission) {
		t.Errorf("skupljeno %v, vraćeno %v", ostaci, err)
	}
	for _, err := range []error{nil, kvar} {
		if got := JaviOstatak(err, &dnevnik); got != err {
			t.Errorf("JaviOstatak(%v) = %v", err, got)
		}
		if got := SkupiOstatak(err, &ostaci); got != err {
			t.Errorf("SkupiOstatak(%v) = %v", err, got)
		}
	}
}

// Zaključava da dopuna odbija neispravan naziv prije nego išta dira, i da
// zatečeni niz koji se ne da pročitati zaustavlja dopunu umjesto da se
// prepiše kao da ga nema.
func TestDopunaOdbijaPrijeUpisa(t *testing.T) {
	koren := t.TempDir()
	r := []Redak{{Vrijeme: time.Date(2024, 3, 1, 7, 0, 0, 0, time.UTC), Vrijednost: 212}}
	if _, err := Dopuni(koren, "drava", "osijek", "cop", "vodostaj", "tjedni", r); err == nil {
		t.Error("dopuna s nepoznatom vrstom je prošla")
	}
	if ima, _ := os.ReadDir(koren); len(ima) != 0 {
		t.Errorf("odbijena dopuna ostavila je trag u stablu: %v", ima)
	}

	// Pod imenom niza stoji mapa umjesto datoteke: čitanje puca.
	mapa := filepath.Join(koren, "drava", "osijek", "osijek_cop_vodostaj_jutarnji_2023.csv")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Dopuni(koren, "drava", "osijek", "cop", "vodostaj", "jutarnji", r); err == nil {
		t.Error("dopuna je prošla iako se zatečeni niz ne da pročitati")
	}
	if _, err := os.Stat(mapa); err != nil {
		t.Errorf("zatečeno je maknuto: %v", err)
	}
}

// Zaključava dopunu niza koji izvor daje samo po danu: zatečeni dani čitaju
// se natrag kao isti dani i ostaju uz nove, a datoteka zadržava stupac datum.
func TestDopunaDnevnogNizaCuvaDane(t *testing.T) {
	koren := t.TempDir()
	dan := func(g int, m time.Month, d int) time.Time { return time.Date(g, m, d, 0, 0, 0, 0, time.UTC) }
	staro := []Redak{
		{Vrijeme: dan(2023, 12, 30), PoDanu: true, Vrijednost: 8.66},
		{Vrijeme: dan(2023, 12, 31), PoDanu: true, Vrijednost: 9.1},
	}
	if _, err := Upisi(koren, "drava", "osijek", "cop", "koncentracija", "srednjak", staro); err != nil {
		t.Fatal(err)
	}
	put, err := Dopuni(koren, "drava", "osijek", "cop", "koncentracija", "srednjak",
		[]Redak{{Vrijeme: dan(2024, 1, 1), PoDanu: true, Vrijednost: 7}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	want := "datum;koncentracija_gl\n2023-12-30;8.66\n2023-12-31;9.1\n2024-01-01;7\n"
	if string(b) != want {
		t.Errorf("nakon dopune:\n%q\nočekivano\n%q", b, want)
	}
	if filepath.Base(put) != "osijek_cop_koncentracija_srednjak_2023-2024.csv" {
		t.Errorf("ime nakon dopune: %q", filepath.Base(put))
	}
}

// Zaključava da vrata odbijaju po istom pravilu po kojem gradnja odbija:
// vodostaj u zadanim granicama, protok i oborina ne smiju biti negativni, a
// veličina bez pravila prolazi.
func TestMogucaVrijednostNaVratima(t *testing.T) {
	for _, s := range []struct {
		velicina string
		v        float64
		moguca   bool
	}{
		{"vodostaj", -102, true},
		{"vodostaj", 2500, false},
		{"protok", -1, false},
		{"protok", 1850, true},
		{"oborina", 42.5, true},
		{"oborina", -0.1, false},
		{"temperatura-zraka", -60, false},
		{"nesto-novo", -1e9, true},
	} {
		if got := MogucaVrijednost(s.velicina, s.v); got != s.moguca {
			t.Errorf("%s %v: moguća %v, očekivano %v", s.velicina, s.v, got, s.moguca)
		}
	}
}
