package prognoza

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// Letve u ovim testovima su izmišljene.

func nacrtPotoka() NacrtLetve {
	return NacrtLetve{Zapis: zapisZaPripremu("potok-vrh"), Tko: "Iva Proba", Kad: "2026-11-21 20:00"}
}

// Nacrt se sprema, čita, zamjenjuje i odbacuje, a model i njegov otisak ga ne vide.
func TestNacrtLetve(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	_, prije, err := SastaviModel(baza)
	if err != nil {
		t.Fatal(err)
	}
	n := nacrtPotoka()
	if err := SpremiNacrt(baza, n, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	n.Zapis.Racun.Rezerve, n.Tko = [][]string{{"potok-drugi"}}, "Marko Primjer"
	if err := SpremiNacrt(baza, n, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	svi, err := Nacrti(baza)
	if err != nil || len(svi) != 1 {
		t.Fatalf("nacrti %v, %v", svi, err)
	}
	if got := svi["potok-dole"]; got.Tko != "Marko Primjer" || got.Kad != n.Kad || len(got.Zapis.Racun.Rezerve) != 1 ||
		got.Zapis.Racun.Ulazi[0] != "potok-vrh" {
		t.Errorf("pročitan nacrt %+v", got)
	}
	paket, poslije, err := SastaviModel(baza)
	if err != nil || poslije != prije {
		t.Errorf("nacrt je promijenio otisak modela: %v", err)
	}
	if _, ima := paket.Tablice["model_nacrt"]; ima {
		t.Error("nacrt putuje s modelom")
	}
	if zapisi, _, err := ZapisiLetvi(baza); err != nil || len(zapisi) != 0 {
		t.Errorf("nacrt je postao zapis: %v, %v", zapisi, err)
	}
	if err := ObrisiNacrt(baza, "potok-dole"); err != nil {
		t.Fatal(err)
	}
	if err := ObrisiNacrt(baza, "nema-je"); err != nil {
		t.Errorf("odbacivanje nacrta kojeg nema: %v", err)
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("nacrt nije odbačen: %v, %v", svi, err)
	}
}

// Nacrt koji se ne bi dao pripremiti ne sprema se, a razlog je isti koji bi dala
// priprema.
func TestNacrtSeProvjerava(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	bezSektora := OkvirModela{ImaSektor: func(string) bool { return false }}
	for ime, c := range map[string]struct {
		izmjena func(*ZapisLetve)
		okvir   OkvirModela
		razlog  string
	}{
		"bez ulaza":   {func(z *ZapisLetve) { z.Racun.Ulazi = nil }, OkvirModela{}, "nijedan ulaz"},
		"brana":       {func(z *ZapisLetve) { z.Letva = "brana-lug" }, OkvirModela{}, "elektrana ili brana"},
		"dva ista":    {func(z *ZapisLetve) { z.Racun.Ulazi = []string{"potok-vrh", "potok-vrh"} }, OkvirModela{}, "naveden dvaput"},
		"bez sektora": {func(*ZapisLetve) {}, bezSektora, "nema sektor"},
		"veličina":    {func(z *ZapisLetve) { z.Velicina = "brzina" }, OkvirModela{}, "nepoznata veličina"},
	} {
		n := nacrtPotoka()
		c.izmjena(&n.Zapis)
		if err := SpremiNacrt(baza, n, c.okvir); !porukaIma(err, c.razlog) {
			t.Errorf("%s: %v, želim %q", ime, err, c.razlog)
		}
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("odbijen nacrt je spremljen: %v, %v", svi, err)
	}
}

// Nacrt koji se ne da zapisati javlja grešku. Baza bez tablice nema nacrta, a redak
// koji se ne da pročitati ili nosi drugu letvu se preskače.
func TestNacrtGreske(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	n := nacrtPotoka()
	n.Zapis.Prethodni = json.RawMessage("{")
	if err := SpremiNacrt(baza, n, OkvirModela{}); !porukaIma(err, "nacrt modela") {
		t.Errorf("nacrt koji se ne da zapisati: %v", err)
	}
	for _, redak := range [][2]string{{"potok-a", "{"}, {"potok-b", `{"zapis":{"letva":"potok-c"}}`}} {
		if _, err := baza.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES (?, ?)`, redak[0], redak[1]); err != nil {
			t.Fatal(err)
		}
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("pokvareni redci: %v, %v", svi, err)
	}
	// tablica drukčijeg oblika: redak bez vrijednosti i, zatim, bez stupca
	for upit, opis := range map[string]string{
		`DROP TABLE model_nacrt; CREATE TABLE model_nacrt (letva TEXT, sadrzaj TEXT); INSERT INTO model_nacrt VALUES (NULL, NULL)`: "redak bez vrijednosti",
		`DROP TABLE model_nacrt; CREATE TABLE model_nacrt (letva TEXT)`:                                                            "tablica bez stupca",
	} {
		if _, err := baza.Exec(upit); err != nil {
			t.Fatal(err)
		}
		if _, err := Nacrti(baza); err == nil {
			t.Errorf("%s: čitanje mora javiti grešku", opis)
		}
		if err := SpremiNacrt(baza, nacrtPotoka(), OkvirModela{}); err == nil {
			t.Errorf("%s: spremanje mora javiti grešku, jer se zatečeni nacrt ne da pročitati", opis)
		}
	}
	if _, err := baza.Exec(`DROP TABLE model_nacrt`); err != nil {
		t.Fatal(err)
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("baza bez tablice: %v, %v", svi, err)
	}
	if err := SpremiNacrt(baza, nacrtPotoka(), OkvirModela{}); err == nil {
		t.Error("spremanje bez tablice mora javiti grešku")
	}
	if err := ObrisiNacrt(baza, "potok-dole"); err == nil {
		t.Error("odbacivanje bez tablice mora javiti grešku")
	}
	zatvorena, err := Otvori(filepath.Join(t.TempDir(), "z.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := zatvorena.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Nacrti(zatvorena); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
	if err := SpremiNacrt(zatvorena, nacrtPotoka(), OkvirModela{}); err == nil {
		t.Error("spremanje u zatvorenu bazu mora javiti grešku")
	}
}

// Priprema nacrta upiše brojeve, doseg i granice razina u isti nacrt, a model ne
// dira. Spremanje iste definicije ih čuva, a promijenjene briše.
func TestPripremiNacrt(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	if _, err := PripremiNacrt(arhiva, baza, "potok-dole", "sada", OkvirModela{}); !porukaIma(err, "nema nacrt") {
		t.Errorf("priprema bez nacrta: %v", err)
	}
	if err := SpremiNacrt(baza, nacrtPotoka(), OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	n, err := PripremiNacrt(arhiva, baza, "potok-dole", "22. 11. 2026. 08:00", OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	if n.Priprema == nil || n.Priprema.Kad != "22. 11. 2026. 08:00" || len(n.Priprema.Cvorovi) != 5 || n.Priprema.Ucenje.Sati == 0 ||
		len(n.Zapis.Brojevi.Inacice) != 1 || len(n.Zapis.Brojevi.Inacice[0]) != 4 || n.Zapis.Racun.Doseg != 5 || n.Tko != "Iva Proba" {
		t.Fatalf("pripremljen nacrt %+v", n)
	}
	procitaj := func() NacrtLetve {
		t.Helper()
		svi, err := Nacrti(baza)
		if err != nil {
			t.Fatal(err)
		}
		return svi["potok-dole"]
	}
	if u := procitaj(); u.Priprema == nil || len(u.Zapis.Brojevi.Inacice) != 1 || u.Zapis.Racun.Doseg != 5 {
		t.Errorf("priprema nije upisana u nacrt: %+v", u)
	}
	if zapisi, _, err := ZapisiLetvi(baza); err != nil || len(zapisi) != 0 {
		t.Errorf("priprema nacrta je dirala model: %v, %v", zapisi, err)
	}
	// ista definicija, drugi čovjek: priprema ostaje
	isti := nacrtPotoka()
	isti.Tko = "Marko Primjer"
	if err := SpremiNacrt(baza, isti, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if u := procitaj(); u.Priprema == nil || u.Zapis.Racun.Doseg != 5 || u.Tko != "Marko Primjer" {
		t.Errorf("ista definicija mora zadržati pripremu: %+v", u)
	}
	// druga definicija: priprema više ne vrijedi
	drugi := nacrtPotoka()
	drugi.Zapis.Racun.Rucno.Kasnjenje = []RucnoKasnjenje{{Razina: 0, Sati: 3}}
	if err := SpremiNacrt(baza, drugi, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if u := procitaj(); u.Priprema != nil || len(u.Zapis.Brojevi.Inacice) != 0 || u.Zapis.Racun.Doseg != 0 {
		t.Errorf("promijenjena definicija mora izgubiti pripremu: %+v", u)
	}
	// priprema koja ne uspije ne mijenja nacrt
	los := nacrtPotoka()
	los.Zapis.Racun.Ulazi = []string{"nema-je-u-arhivi"}
	if err := SpremiNacrt(baza, los, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if _, err := PripremiNacrt(arhiva, baza, "potok-dole", "sada", OkvirModela{}); err == nil {
		t.Error("priprema iz ulaza kojeg nema u arhivi mora javiti grešku")
	}
	if u := procitaj(); u.Priprema != nil {
		t.Errorf("neuspjela priprema je upisana: %+v", u)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := PripremiNacrt(arhiva, baza, "potok-dole", "sada", OkvirModela{}); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}

// Dva zapisa su ista definicija kad se slažu u veličini i računu; doseg i brojevi
// se ne gledaju.
func TestIstaDefinicija(t *testing.T) {
	a, b := zapisZaPripremu("potok-vrh"), zapisZaPripremu("potok-vrh")
	b.Racun.Doseg, b.Brojevi = 12, BrojeviLetve{Razdoblje: []string{"x"}}
	if !istaDefinicija(a, b) {
		t.Error("doseg i brojevi ne mijenjaju definiciju")
	}
	for ime, izmjena := range map[string]func(*ZapisLetve){
		"veličina":   func(z *ZapisLetve) { z.Velicina = "protok" },
		"ulaz":       func(z *ZapisLetve) { z.Racun.Ulazi = []string{"potok-drugi"} },
		"rezerva":    func(z *ZapisLetve) { z.Racun.Rezerve = [][]string{{"potok-drugi"}} },
		"razine":     func(z *ZapisLetve) { z.Racun.Razine.Granice = []float64{100} },
		"isključena": func(z *ZapisLetve) { z.Racun.Razine.Iskljucene = []int{1} },
	} {
		c := zapisZaPripremu("potok-vrh")
		izmjena(&c)
		if istaDefinicija(a, c) {
			t.Errorf("%s: definicija se promijenila", ime)
		}
	}
}

// Proba mjeri pripremljeni račun i upiše dosege s ocjenom u nacrt. Račun koji
// nije pripremljen ne proba se, nova priprema briše probu, a ista definicija je
// čuva.
func TestProbajNacrt(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	if _, err := ProbajNacrt(arhiva, baza, otvoriProbnu(t, "t0.db"), "potok-dole", "sada"); !porukaIma(err, "nije pripremljen") {
		t.Errorf("proba bez nacrta: %v", err)
	}
	if err := SpremiNacrt(baza, nacrtPotoka(), OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajNacrt(arhiva, baza, otvoriProbnu(t, "t1.db"), "potok-dole", "sada"); !porukaIma(err, "nije pripremljen") {
		t.Errorf("proba nepripremljenog nacrta: %v", err)
	}
	if _, err := PripremiNacrt(arhiva, baza, "potok-dole", "08:00", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	n, err := ProbajNacrt(arhiva, baza, otvoriProbnu(t, "t2.db"), "potok-dole", "22. 11. 2026. 08:05")
	if err != nil {
		t.Fatal(err)
	}
	if n.Proba == nil || n.Proba.Kad != "22. 11. 2026. 08:05" || len(n.Proba.Dosezi) != 1 || n.Proba.Dosezi[0].Doseg != 5 ||
		n.Proba.Dosezi[0].Stanje != MjeraBolje || len(n.Proba.Razdoblje) != 2 || len(n.Zapis.Brojevi.Promasaji) == 0 {
		t.Fatalf("proba %+v, promašaji %v", n.Proba, n.Zapis.Brojevi.Promasaji)
	}
	procitaj := func() NacrtLetve {
		t.Helper()
		svi, err := Nacrti(baza)
		if err != nil {
			t.Fatal(err)
		}
		return svi["potok-dole"]
	}
	if u := procitaj(); u.Proba == nil || u.Proba.Dosezi[0].Stanje != MjeraBolje || len(u.Zapis.Brojevi.Promasaji) == 0 {
		t.Errorf("proba nije upisana u nacrt: %+v", u)
	}
	if zapisi, _, err := ZapisiLetvi(baza); err != nil || len(zapisi) != 0 {
		t.Errorf("proba je dirala model: %v, %v", zapisi, err)
	}
	if err := SpremiNacrt(baza, nacrtPotoka(), OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if u := procitaj(); u.Proba == nil || len(u.Zapis.Brojevi.Promasaji) == 0 {
		t.Errorf("ista definicija mora zadržati probu: %+v", u)
	}
	// baza koja već ima model nije privremena: proba se odbija
	puna := otvoriProbnu(t, "puna.db")
	if err := Spremi(puna, []Pojas{{Letva: "potok-drugi", Velicina: "vodostaj", Od: -1000, Do: 1000, Sati: 5000,
		Ulazi: []Ulaz{{Letva: "potok-vrh", Velicina: "vodostaj", PomakH: 1, Sirina: 1, Nagib: 1}}}}, "proba"); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajNacrt(arhiva, baza, puna, "potok-dole", "sada"); !porukaIma(err, "nije nova") {
		t.Errorf("proba u bazu koja nije privremena: %v", err)
	}
	if _, err := PripremiNacrt(arhiva, baza, "potok-dole", "08:10", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if u := procitaj(); u.Proba != nil || len(u.Zapis.Brojevi.Promasaji) != 0 {
		t.Errorf("nova priprema mora obrisati probu: %+v", u)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajNacrt(arhiva, baza, otvoriProbnu(t, "t3.db"), "potok-dole", "sada"); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}

// Nacrt s upisanim nagibom je ručni račun: jedan ulaz, bez rezervi i razina, uz
// jedno kašnjenje. Što od toga ne valja, odbija se već pri spremanju.
func TestNacrtRucnogRacuna(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	nagib := 1.5
	rucni := func(izmjena func(*ZapisLetve)) NacrtLetve {
		n := nacrtPotoka()
		n.Zapis.Racun.Rucno = RucnoLetve{Nagib: &nagib, Kasnjenje: []RucnoKasnjenje{{Razina: 0, Sati: 4}}}
		izmjena(&n.Zapis)
		return n
	}
	if err := SpremiNacrt(baza, rucni(func(*ZapisLetve) {}), OkvirModela{}); err != nil {
		t.Fatalf("valjan ručni račun: %v", err)
	}
	for ime, c := range map[string]struct {
		izmjena func(*ZapisLetve)
		razlog  string
	}{
		"dva ulaza":     {func(z *ZapisLetve) { z.Racun.Ulazi = []string{"potok-vrh", "potok-drugi"} }, "točno jedan ulaz"},
		"rezerva":       {func(z *ZapisLetve) { z.Racun.Rezerve = [][]string{{"potok-drugi"}} }, "nema rezervu"},
		"razine":        {func(z *ZapisLetve) { z.Racun.Razine.Granice = []float64{100} }, "jednu razinu"},
		"bez kašnjenja": {func(z *ZapisLetve) { z.Racun.Rucno.Kasnjenje = nil }, "upiši i kašnjenje"},
	} {
		if err := SpremiNacrt(baza, rucni(c.izmjena), OkvirModela{}); !porukaIma(err, c.razlog) {
			t.Errorf("%s: %v, želim %q", ime, err, c.razlog)
		}
	}
}
