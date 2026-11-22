package prognoza

import (
	"bytes"
	"encoding/json"
	"log"
	"math"
	"slices"
	"strings"
	"testing"
)

// zapisSDnevnim je redak letve s dnevnim blokom; račun je satni kad su zadani ulazi.
func zapisSDnevnim(t *testing.T, letva string, d DnevniLetve, satniUlazi ...string) ZapisLetve {
	t.Helper()
	blok, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	z := ZapisLetve{Oblik: OblikModelaLetve, Letva: letva, Metoda: MetodaDnevni, Velicina: "vodostaj", Dnevni: blok}
	if len(satniUlazi) > 0 {
		z.Metoda, z.Racun = MetodaLanac, RacunLetve{Ulazi: satniUlazi, Doseg: najveciDosegZapisa}
	}
	return z
}

// bezDnevnePostave vraća postavu iz zapisa na praznu nakon testa.
func bezDnevnePostave(t *testing.T) {
	t.Helper()
	dnevnaIzZapisa.Store(nil)
	t.Cleanup(func() { dnevnaIzZapisa.Store(nil) })
}

func pok[T any](v T) *T { return &v }

// Dnevni blok u retku letve mijenja iz čega se uči dnevni model te letve i od
// kojeg dana on vrijedi. Ostale letve i letve bez bloka rade iz programa.
func TestDnevniModelIzZapisa(t *testing.T) {
	bezDnevnePostave(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "gornja", Slivovi: []string{"A"}},
		{Letva: "srednja", Ulazi: []string{"gornja"}, Slivovi: []string{"A", "B"}}, {Letva: "donja", Ulazi: []string{"srednja", "gornja"}, Slivovi: []string{"B"}}})
	postavi(t, &DnevnaOdDana, map[string]int{"srednja": 3, "donja": 4})
	postavi(t, &DnevnaOdDanaNiska, map[string]int{"donja": 2})
	postavi(t, &VisokaVoda, map[string]float64{"donja": 300})
	if !slices.EqualFunc(CiljeviDnevnog(), DnevniCiljevi, func(a, b DnevniCilj) bool { return a.Letva == b.Letva }) || DnevniRacunIzZapisa("donja") {
		t.Fatal("bez zapisa ciljevi su oni iz programa")
	}
	baza := otvoriProbnu(t, "p.db")
	upisiZapis(t, baza, zapisSDnevnim(t, "srednja", DnevniLetve{Ulazi: []string{"gornja", "pritoka"}, Slivovi: []string{"B"}}))
	// isti račun drugim redom nije druga definicija, ali dan prijelaza vrijedi
	upisiZapis(t, baza, zapisSDnevnim(t, "donja", DnevniLetve{Ulazi: []string{"gornja", "srednja"}, Slivovi: []string{"B"},
		OdDana: pok(5), OdDanaNiska: pok(3), VisokaVoda: pok(250.0)}, "srednja"))
	upisiZapis(t, baza, zapisSDnevnim(t, "gornja", DnevniLetve{Slivovi: []string{"A", KisaKisomjera("km-a")}}))
	upisiZapis(t, baza, zapisSDnevnim(t, "rjecica", DnevniLetve{Ulazi: []string{"gornja"}}))
	upisiZapis(t, baza, glavniZapis("bez-bloka", "vodostaj", "gornja"))
	odbijeni, err := UcitajDnevneIzZapisa(baza)
	if err != nil || len(odbijeni) != 1 || !porukaIma(odbijeni["gornja"], "kiša jednog kišomjera ovdje se ne bira") {
		t.Fatalf("odbijeni %v, %v", odbijeni, err)
	}
	c := CiljeviDnevnog()
	if len(c) != 3 || c[0].Slivovi[0] != "A" || !slices.Equal(c[1].Ulazi, []string{"gornja", "pritoka"}) || !slices.Equal(c[1].Slivovi, []string{"B"}) ||
		!slices.Equal(c[2].Ulazi, []string{"srednja", "gornja"}) {
		t.Errorf("ciljevi sa zapisima: %+v", c)
	}
	if !DnevniRacunIzZapisa("srednja") || DnevniRacunIzZapisa("donja") || DnevniRacunIzZapisa("gornja") {
		t.Error("račun iz zapisa ima samo letva kojoj su ulazi ili slivovi drukčiji")
	}
	if !slices.Contains(DnevniUlazi(), "pritoka") {
		t.Errorf("ulaz iz zapisa mora biti među ulazima dnevnog modela: %v", DnevniUlazi())
	}
	if d, ima := DnevnaOdDanaLetve("donja"); !ima || d != 5 || DnevnaOdDanaZa("donja", 240, true) != 3 || DnevnaOdDanaZa("donja", 260, true) != 5 ||
		DnevnaOdDanaZa("srednja", 100, true) != 3 {
		t.Errorf("dan prijelaza iz zapisa: %d", d)
	}
	if ciljevi, bez := dnevniCiljeviZapisa(baza, OkvirModela{}); len(ciljevi) != 1 || ciljevi[0].Letva != "rjecica" || len(bez) != 0 {
		t.Errorf("odvojeni dnevni krug računa samo letve izvan glavnog modela: %+v, %v", ciljevi, bez)
	}
	// postava se mijenja tek kad se redci promijene, a prazna je bez redaka
	if _, err := baza.Exec(`DELETE FROM model_letve`); err != nil {
		t.Fatal(err)
	}
	if _, err := UcitajDnevneIzZapisa(baza); err != nil || DnevniRacunIzZapisa("srednja") || DnevnaOdDanaZa("donja", 240, true) != 2 {
		t.Errorf("nakon brisanja redaka vrijedi program: %v", err)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := UcitajDnevneIzZapisa(baza); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}

// Dnevni blok letve glavnog dnevnog modela odbija se kad letva nije njegov cilj,
// kad traži kišu kišomjera ili kad dan prijelaza i granica nisu razumni.
func TestProvjeraDnevnogBlokaGlavneLetve(t *testing.T) {
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}, Slivovi: []string{"B"}}})
	for ime, p := range map[string]struct {
		letva  string
		d      DnevniLetve
		razlog string
	}{
		"izvan modela":   {"rjecica", DnevniLetve{Ulazi: []string{"gornja"}}, "nije cilj dnevnog modela"},
		"prazan":         {"donja", DnevniLetve{}, "ni ulaz ni sliv"},
		"sama sebi":      {"donja", DnevniLetve{Ulazi: []string{"donja"}}, "sama sebi ulaz"},
		"kišomjer":       {"donja", DnevniLetve{Slivovi: []string{KisaKisomjera("km")}}, "ovdje se ne bira"},
		"dan nula":       {"donja", DnevniLetve{Ulazi: []string{"gornja"}, OdDana: pok(0)}, "između 1 i 6"},
		"dan sedam":      {"donja", DnevniLetve{Ulazi: []string{"gornja"}, OdDanaNiska: pok(7)}, "između 1 i 6"},
		"granica NaN":    {"donja", DnevniLetve{Ulazi: []string{"gornja"}, VisokaVoda: pok(math.NaN())}, "nije razuman broj"},
		"granica golema": {"donja", DnevniLetve{Ulazi: []string{"gornja"}, VisokaVoda: pok(1e12)}, "nije razuman broj"},
	} {
		if err := p.d.provjeriGlavniDnevni(p.letva); !porukaIma(err, p.razlog) {
			t.Errorf("%s: %v, želim %q", ime, err, p.razlog)
		}
	}
	dobar := DnevniLetve{Ulazi: []string{"gornja"}, Slivovi: []string{"A"}, OdDana: pok(6), OdDanaNiska: pok(1), VisokaVoda: pok(-20.0)}
	if err := dobar.provjeriGlavniDnevni("donja"); err != nil {
		t.Errorf("ispravan blok: %v", err)
	}
}

// Letvi čiji se dnevni model uči iz zapisa uzvodni ispravak ne vrijedi; kad
// zapis samo ponavlja program ili mijenja dan prijelaza, vrijedi.
func TestUzvodniIspravakBezRacunaIzZapisa(t *testing.T) {
	bezDnevnePostave(t)
	if !ImaUzvodniIspravak("batina") {
		t.Fatal("Batina u programu ima uzvodni ispravak")
	}
	iz, _ := ciljIzPrograma("batina")
	baza := otvoriProbnu(t, "p.db")
	upisiZapis(t, baza, zapisSDnevnim(t, "batina", DnevniLetve{Ulazi: slices.Clone(iz.Ulazi), Slivovi: slices.Clone(iz.Slivovi), OdDana: pok(4)}))
	if _, err := UcitajDnevneIzZapisa(baza); err != nil || !ImaUzvodniIspravak("batina") {
		t.Errorf("zapis s istim računom ne gasi ispravak: %v", err)
	}
	upisiZapis(t, baza, zapisSDnevnim(t, "batina", DnevniLetve{Ulazi: []string{"mohacs"}, Slivovi: slices.Clone(iz.Slivovi)}))
	if _, err := UcitajDnevneIzZapisa(baza); err != nil || ImaUzvodniIspravak("batina") || !ImaUzvodniIspravak("aljmas") {
		t.Errorf("zapis s drugim ulazima gasi ispravak samo toj letvi: %v", err)
	}
}

// Promjena dnevnih blokova baca naučene dnevne modele, a isti blokovi ne; što
// nije uzeto ide u dnevnik, kao i baza koja se ne da pročitati.
func TestOsvjezavanjeDnevnePostave(t *testing.T) {
	bezDnevnePostave(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}, Slivovi: []string{"B"}}})
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	naucen := func() {
		dnevniModeli.Lock()
		dnevniModeli.dan = "2026-11-22"
		dnevniModeli.Unlock()
	}
	ima := func() bool {
		dnevniModeli.Lock()
		defer dnevniModeli.Unlock()
		return dnevniModeli.dan != ""
	}
	t.Cleanup(ZaboraviDnevneModele)
	baza := otvoriProbnu(t, "p.db")
	naucen()
	OsvjeziDnevnuPostavu(baza)
	if !ima() {
		t.Error("baza bez dnevnih blokova ne baca naučene modele")
	}
	upisiZapis(t, baza, zapisSDnevnim(t, "donja", DnevniLetve{Ulazi: []string{"gornja", "pritoka"}, Slivovi: []string{"B"}}))
	(&Osvjezivac{Baza: baza}).ucitajDnevneIzZapisa()
	if ima() {
		t.Error("novi dnevni blok mora baciti naučene modele")
	}
	naucen()
	OsvjeziDnevnuPostavu(baza)
	if !ima() {
		t.Error("isti blokovi ne bacaju naučene modele")
	}
	upisiZapis(t, baza, zapisSDnevnim(t, "donja", DnevniLetve{Ulazi: []string{"donja"}}))
	OsvjeziDnevnuPostavu(baza)
	if ima() || !strings.Contains(dnevnik.String(), "dnevni model letve donja iz zapisa se ne koristi: letva ne može biti sama sebi ulaz") {
		t.Errorf("odbijeni blok vraća letvu na program i piše u dnevnik: %s", dnevnik.String())
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	OsvjeziDnevnuPostavu(baza)
	if !strings.Contains(dnevnik.String(), "prognoza: dnevni model iz zapisa: ") {
		t.Errorf("baza koja se ne da pročitati mora u dnevnik: %s", dnevnik.String())
	}
}
