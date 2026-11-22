package prognoza

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"
)

// glavniZapis je redak letve glavnog lanca s jednim skupom ulaza.
func glavniZapis(letva, velicina string, ulazi ...string) ZapisLetve {
	return ZapisLetve{Oblik: OblikModelaLetve, Letva: letva, Metoda: MetodaLanac, Velicina: velicina,
		Racun: RacunLetve{Ulazi: ulazi, Doseg: najveciDosegZapisa}}
}

// namjestiProbu namješta lanac probe i vraća inačice cilja uz ispis.
func namjestiProbu(t *testing.T, redak *ZapisLetve) ([][]Pojas, string) {
	t.Helper()
	baza := otvoriProbnu(t, "p.db")
	if redak != nil {
		upisiZapis(t, baza, *redak)
	}
	var w bytes.Buffer
	if _, err := NamjestiLanac(arhivaLanca(t), baza, OpcijeNamjestanja{Dnevnik: &w}); err != nil {
		t.Fatalf("%v\n%s", err, w.String())
	}
	in, err := InaciceLetve(baza, "cilj")
	if err != nil {
		t.Fatal(err)
	}
	return in, w.String()
}

// Redak letve glavnog lanca mijenja iz čega se njezin račun namješta. Redak koji
// ponavlja račun iz programa daje iste brojeve, drugi ulaz daje drugi račun, a
// rezerva iz retka dodaje inačicu.
func TestPripremiModelCitaRedakGlavneLetve(t *testing.T) {
	postaviLanacProbe(t)
	izPrograma, ispis := namjestiProbu(t, nil)
	if len(izPrograma) != 1 || strings.Contains(ispis, "iz zapisa") {
		t.Fatalf("bez retka: %d inačica\n%s", len(izPrograma), ispis)
	}
	isti := glavniZapis("cilj", "protok", "ulaz")
	izRetka, ispis := namjestiProbu(t, &isti)
	if !strings.Contains(ispis, "cilj ← ulaz (iz zapisa)") {
		t.Errorf("ispis ne kaže da je račun iz zapisa:\n%s", ispis)
	}
	if !reflect.DeepEqual(izRetka, izPrograma) {
		t.Errorf("redak jednak programu mora dati iste brojeve:\n%+v\n%+v", izRetka, izPrograma)
	}
	drugi := glavniZapis("cilj", "protok", "drugi")
	drugi.Racun.Rezerve = [][]string{{"ulaz"}}
	in, _ := namjestiProbu(t, &drugi)
	if len(in) != 2 || in[0][0].Ulazi[0].Letva != "drugi" || in[1][0].Ulazi[0].Letva != "ulaz" || in[1][0].Inacica != 1 {
		t.Errorf("račun iz retka s rezervom: %+v", in)
	}
	dvije := glavniZapis("cilj", "protok", "ulaz")
	dvije.Racun.Razine = RazineLetve{Postoci: []float64{0, 50, 100}}
	if in, _ := namjestiProbu(t, &dvije); len(in) != 1 || len(in[0]) != 2 {
		t.Errorf("dvije razine iz retka: %+v", in)
	}
}

// Redak koji ne prolazi provjeru ili se ne da namjestiti samo se napiše, a
// letva dobiva račun iz programa. Redak se pritom ne računa kao odvojen račun.
func TestOdbijenRedakGlavneLetve(t *testing.T) {
	postaviLanacProbe(t)
	kriva := glavniZapis("cilj", "vodostaj", "ulaz")
	in, ispis := namjestiProbu(t, &kriva)
	if len(in) != 1 || in[0][0].Ulazi[0].Letva != "ulaz" ||
		!strings.Contains(ispis, "zapis letve cilj se ne koristi (letva se u glavnom lancu vodi kao protok; to se ovdje ne mijenja); račun ide iz programa") {
		t.Errorf("redak s krivom veličinom: %d inačica\n%s", len(in), ispis)
	}
	bezNiza := glavniZapis("cilj", "protok", "nema-niza")
	in, ispis = namjestiProbu(t, &bezNiza)
	if len(in) != 1 || in[0][0].Ulazi[0].Letva != "ulaz" || !strings.Contains(ispis, "cilj ← nema-niza (iz zapisa)") ||
		!strings.Contains(ispis, "račun ide iz programa") {
		t.Errorf("redak koji se ne da namjestiti: %d inačica\n%s", len(in), ispis)
	}

	baza := otvoriProbnu(t, "p.db")
	upisiRedak(t, baza, "cilj", "{nije json")
	upisiZapis(t, baza, glavniZapis("nije-u-lancu", "protok", "ulaz"))
	glavni, odbijeni, err := GlavniZapisi(baza, OkvirModela{})
	if err != nil || len(glavni) != 0 || len(odbijeni) != 1 || !porukaIma(odbijeni["cilj"], "ne da pročitati") {
		t.Errorf("glavni %v, odbijeni %v, %v", glavni, odbijeni, err)
	}
	zapisi, pokvareni, err := zapisiZaRacun(baza, nil)
	if _, ima := zapisi["cilj"]; err != nil || ima || pokvareni["cilj"] != nil {
		t.Errorf("redak glavne letve ne ide u odvojeni račun: %v %v %v", zapisi, pokvareni, err)
	}
	upisiZapis(t, baza, glavniZapis("cilj", "protok", "ulaz"))
	if p := OdvojeniZaPrikaz(baza); len(p.Pojasi) != 0 {
		t.Errorf("redak glavne letve ne ide na prikaz odvojenih: %+v", p.Pojasi)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := GlavniZapisi(baza, OkvirModela{}); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
	if _, err := NamjestiLanac(arhivaLanca(t), baza, OpcijeNamjestanja{}); err == nil {
		t.Error("namještanje uz zatvorenu bazu mora javiti grešku")
	}
}

// Letva glavnog lanca ne smije mijenjati veličinu, upisati nagib, čitati sebe ni
// letvu s odvojenim računom, niti zatvoriti krug kroz lanac.
func TestProvjeraRetkaGlavneLetve(t *testing.T) {
	tokoviProbe(t, Racun{Letva: "gornja", Ulazi: []string{"vrh"}}, Racun{Letva: "donja", Ulazi: []string{"gornja"}},
		Racun{Letva: "ne-zna-se", Ulazi: []string{"vrh"}})
	postavi(t, &OdvojeniRacuni, []Racun{{Letva: "potok", Ulazi: []string{"vrh"}}})
	postavi(t, &Rezerve, map[string][][]string{"donja": {{"vrh:vodostaj"}}})
	postavi(t, &Velicine, map[string]string{"gornja": "vodostaj", "donja": "vodostaj", "vrh": "vodostaj", "potok": "vodostaj"})
	redci := map[string]ZapisLetve{"rjecica": glavniZapis("rjecica", "vodostaj", "vrh")}

	if z, ima := ZapisIzLanca("donja"); !ima || z.Velicina != "vodostaj" || z.Racun.Ulazi[0] != "gornja" ||
		len(z.Racun.Rezerve) != 1 || z.Racun.Rezerve[0][0] != "vrh" || z.ProvjeriGlavni(OkvirModela{}, redci) != nil {
		t.Errorf("zapis iz lanca: %+v", z)
	}
	if _, ima := ZapisIzLanca("potok"); ima || GlavnaLetva("potok") || !GlavnaLetva("gornja") || velicinaULancu("potok") != "" ||
		velicinaULancu("ne-zna-se") != "" {
		t.Error("letva s odvojenim računom nije u glavnom lancu")
	}
	nagib := 0.8
	sNagibom := glavniZapis("gornja", "vodostaj", "vrh")
	sNagibom.Racun.Rucno.Nagib = &nagib
	iskljucena := glavniZapis("donja", "vodostaj", "gornja")
	iskljucena.Racun.Razine.Iskljucene, iskljucena.Racun.Rezerve = []int{0}, [][]string{{"vrh"}}
	for ime, p := range map[string]struct {
		z      ZapisLetve
		razlog string
	}{
		"izvan lanca":   {glavniZapis("rjecica", "vodostaj", "vrh"), "nije u glavnom lancu"},
		"veličina":      {glavniZapis("gornja", "protok", "vrh"), "vodi kao vodostaj"},
		"nagib":         {sNagibom, "nagib se ne upisuje"},
		"sama sebi":     {glavniZapis("gornja", "vodostaj", "gornja"), "sama sebi ulaz"},
		"odvojen račun": {glavniZapis("gornja", "vodostaj", "potok"), "potok ima odvojen račun"},
		"redak izvan":   {glavniZapis("gornja", "vodostaj", "rjecica"), "rjecica ima odvojen račun"},
		"krug":          {glavniZapis("gornja", "vodostaj", "donja"), "vrti u krug"},
		"bez ulaza":     {glavniZapis("gornja", "vodostaj"), "nema nijedan ulaz"},
		"isključena":    {iskljucena, "ne može imati rezervu"},
	} {
		if err := p.z.ProvjeriGlavni(OkvirModela{}, redci); !porukaIma(err, p.razlog) {
			t.Errorf("%s: %v, želim %q", ime, err, p.razlog)
		}
	}
	// redak druge letve lanca ulazi u provjeru kruga
	redci["donja"] = glavniZapis("donja", "vodostaj", "vrh")
	if err := glavniZapis("gornja", "vodostaj", "donja").ProvjeriGlavni(OkvirModela{}, redci); err != nil {
		t.Errorf("kad donja više ne čita gornju, kruga nema: %v", err)
	}
	bezSektora := OkvirModela{ImaSektor: func(string) bool { return false }}
	if err := glavniZapis("gornja", "vodostaj", "vrh").ProvjeriGlavni(bezSektora, redci); !porukaIma(err, "nema sektor") {
		t.Errorf("letva bez sektora: %v", err)
	}
}

// Rijetko očitavana letva iz retka namješta se sa spuštenim granicama sati, kao
// i iz programa, a granice se poslije vrate. Greška čitanja arhive prekida.
func TestRedakRijetkeLetve(t *testing.T) {
	batina, rijetka := map[int64]float64{}, map[int64]float64{}
	for h := int64(0); h < 6000; h++ {
		batina[h] = 300 + 120*math.Sin(float64(h)/37) + 40*math.Sin(float64(h)/211)
	}
	for h := int64(10); h < 6000; h += 10 {
		rijetka[h] = 0.95*batina[h-4] + 162
	}
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{"batina:vodostaj": batina, "dunav-zmajevac:vodostaj": rijetka})
	postavi(t, &NajveciPomak, 8)
	z := glavniZapis("dunav-zmajevac", "vodostaj", "batina")
	zapisi := map[string]ZapisLetve{"dunav-zmajevac": z}
	var w bytes.Buffer
	pojasi, err := namjestiRacunLanca(arhiva, Racun{Letva: "dunav-zmajevac", Ulazi: []string{"batina"}}, zapisi, nil, &w)
	if err != nil || len(pojasi) == 0 || pojasi[0].Ulazi[0].PomakH != 4 {
		t.Fatalf("rijetka letva iz retka: %+v, %v\n%s", pojasi, err, w.String())
	}
	if NajmanjeSati != 5000 || NajmanjeSatiPojasa != 2000 {
		t.Errorf("granice nakon rijetke letve %d i %d", NajmanjeSati, NajmanjeSatiPojasa)
	}
	if err := arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := namjestiRacunLanca(arhiva, Racun{Letva: "dunav-zmajevac", Ulazi: []string{"batina"}}, zapisi, nil, &w); err == nil {
		t.Error("arhiva koja se ne da pročitati mora prekinuti namještanje")
	}
}
