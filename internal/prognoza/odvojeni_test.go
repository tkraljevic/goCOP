package prognoza

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/plato"
)

// Letve u ovim testovima su izmišljene: glavni lanac gornja → donja i potok
// potok-vrh → potok-sredina → potok-dole.

func zapisPotoka(letva, ulaz string, pomak int) ZapisLetve {
	return ZapisLetve{Oblik: OblikModelaLetve, Letva: letva, Metoda: MetodaLanac, Velicina: "vodostaj",
		Racun:    RacunLetve{Ulazi: []string{ulaz}, Doseg: 12},
		Brojevi:  BrojeviLetve{Inacice: [][]Pojas{{pojasPotoka(letva, ulaz, pomak)}}},
		Promjene: []PromjenaLetve{{Kad: "2026-11-06 10:00", Tko: "Ivana Probić", Razlog: "proba"}}}
}

func pojasPotoka(letva, ulaz string, pomak int) Pojas {
	return Pojas{Letva: letva, Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []Ulaz{{Letva: ulaz, Velicina: "vodostaj", PomakH: pomak, Sirina: 1, Nagib: 1}}}
}

// upisiRedak upisuje redak mimo provjera, kao što bi došao s pokvarenog diska
// ili novijeg programa.
func upisiRedak(t *testing.T, db *sql.DB, letva, sadrzaj string) {
	t.Helper()
	if _, err := db.Exec(`INSERT OR REPLACE INTO model_letve (letva, sadrzaj) VALUES (?, ?)`, letva, sadrzaj); err != nil {
		t.Fatal(err)
	}
}

func upisiZapis(t *testing.T, db *sql.DB, z ZapisLetve) {
	t.Helper()
	b, err := json.Marshal(z)
	if err != nil {
		t.Fatal(err)
	}
	upisiRedak(t, db, z.Letva, string(b))
}

func dodajLetvuPotoka(t *testing.T, db *sql.DB, id, kod string, zadnji time.Time, sati int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO stations VALUES (?,?)`, id, kod); err != nil {
		t.Fatal(err)
	}
	for i := range sati {
		if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES (?,?,?)`,
			id, zadnji.Add(-time.Duration(i)*time.Hour), 50+i); err != nil {
			t.Fatal(err)
		}
	}
}

// glavneLetve su letve glavnog lanca u probnom osvježivaču.
var glavneLetve = map[string]bool{"gornja": true, "donja": true}

// otisakGlavnog je sve što glavni račun izda, bez novih letvi.
func otisakGlavnog(i *Ishod) string {
	var out []string
	for _, x := range i.Izdane {
		if glavneLetve[x.Letva] {
			out = append(out, fmt.Sprintf("%+v", x))
		}
	}
	for k, v := range i.Izbor {
		if glavneLetve[k] {
			out = append(out, fmt.Sprintf("izbor %s %+v", k, v))
		}
	}
	for k, v := range i.BezPrognoze {
		if glavneLetve[k] {
			out = append(out, "bez "+k+" "+v.Error())
		}
	}
	for k, v := range i.Vrhovi {
		out = append(out, fmt.Sprintf("vrh %+v %d", k, v))
	}
	for k, v := range i.Ceka {
		out = append(out, fmt.Sprintf("ceka %s %d", k, v))
	}
	sort.Strings(out)
	return fmt.Sprint(i.Sada, i.Preskoceno, otisakUpozorenja(i.Upozorenja), out)
}

func otisakUpozorenja(u []Upozorenje) []string {
	var out []string
	for _, x := range u {
		if x.Vrsta != VrstaOdvojeni && glavneLetve[x.Letva] {
			out = append(out, x.String())
		}
	}
	return out
}

// otisakPuta vodi izdanje do kraja puta (Zapisi, SastaviIzdanje, Zamotaj,
// Odmotaj) i vraća retke glavnih letvi kako bi stigli primatelju.
func otisakPuta(t *testing.T, o *Osvjezivac, i *Ishod) string {
	t.Helper()
	if err := o.Zapisi(i); err != nil {
		t.Fatalf("Zapisi: %v", err)
	}
	paket, err := SastaviIzdanje(o.Baza, i)
	if err != nil {
		t.Fatalf("SastaviIzdanje: %v", err)
	}
	om, err := Zamotaj(Omotnica{Izdano: i.Sada}, paket)
	if err != nil {
		t.Fatalf("Zamotaj: %v", err)
	}
	sirovo, err := json.Marshal(om)
	if err != nil {
		t.Fatal(err)
	}
	var natrag PaketIzdanja
	if _, err := Odmotaj(sirovo, &natrag); err != nil {
		t.Fatalf("Odmotaj: %v", err)
	}
	var out []string
	for ime, tb := range map[string]Tablica{"izdane": natrag.Izdane, "dnevne": natrag.Dnevne, "izbor": natrag.Izbor} {
		for _, r := range tb.Redci {
			if j := slices.Index(tb.Stupci, "letva"); glavneLetve[fmt.Sprint(r[j])] {
				out = append(out, ime+" "+fmt.Sprint(r))
			}
		}
	}
	sort.Strings(out)
	return fmt.Sprint(natrag.Zapis.Izdano, natrag.Zapis.Model, otisakUpozorenja(natrag.Zapis.Upozorenja), out)
}

// Glavni račun je isti do kraja puta u svim stanjima potoka, ni za zadnju
// znamenku, a sat izdanja se ne pomiče.
func TestOdvojeniNeDiraGlavni(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	for _, s := range []struct {
		ime         string
		vrh, karika time.Duration // koliko je zadnje očitanje staro; <0 bez očitanja
		daje, drzi  bool
	}{
		{"svježe", 0, 0, true, false},
		{"bez ijednog očitanja", -1, -1, false, false},
		{"oba stala pet dana", 120 * time.Hour, 120 * time.Hour, false, false},
		{"oba kasne 4 h", 4 * time.Hour, 4 * time.Hour, false, false},
		{"stao samo vrh", 120 * time.Hour, 0, true, true},
		{"vrh kasni 3 h", 3 * time.Hour, 0, true, false},
		{"vrh kasni 4 h", 4 * time.Hour, 0, true, true},
	} {
		t.Run(s.ime, func(t *testing.T) {
			oc := probneOcitanja(t, 200, zadnji)
			o := probniOsvjezivac(t, oc)
			o.Iznova = true
			bez, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			glavniBez, putBez := otisakGlavnog(bez), otisakPuta(t, o, bez)
			if s.vrh >= 0 {
				dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji.Add(-s.vrh), 48)
			}
			if s.karika >= 0 {
				dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji.Add(-s.karika), 48)
			}
			upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
			sa, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatalf("glavni račun je pao: %v", err)
			}
			if otisakGlavnog(sa) != glavniBez {
				t.Fatalf("glavni račun se promijenio:\n%s\n%s", glavniBez, otisakGlavnog(sa))
			}
			if put := otisakPuta(t, o, sa); put != putBez {
				t.Fatalf("na kraju puta glavni redci nisu isti:\n%s\n%s", putBez, put)
			}
			n, _ := izdanihLetve(sa, "potok-dole")
			if s.daje && n == 0 {
				t.Errorf("potok-dole bez prognoze; bez: %v", sa.BezPrognoze)
			}
			if !s.daje && n != 0 {
				t.Errorf("potok-dole ima %d redaka, a nema ni očitanja ni svježeg ulaza", n)
			}
			if s.drzi != (sa.Izbor["potok-dole"].Inacica == -1) {
				t.Errorf("drži zadnje mjerenje: %+v", sa.Izbor["potok-dole"])
			}
		})
	}
}

// Prazna tablica ne dodaje ništa, ni retke ni upozorenja.
func TestOdvojeniPraznaTablicaNistaNeDodaje(t *testing.T) {
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := probniOsvjezivac(t, probneOcitanja(t, 48, zadnji))
	o.Iznova = true
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaOdvojeni }) {
		t.Errorf("upozorenja: %v", i.Upozorenja)
	}
	if n, _ := izdanihLetve(i, "potok-dole"); n != 0 {
		t.Errorf("redaka %d", n)
	}
}

// Prvi vodotok: tri izmišljene letve redom, s rezervom i promašajima, i letva
// koja čita glavni lanac. Brojevi su izračunati rukom.
func TestPrviVodotok(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sada := zadnji.Unix() / 3600
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-sredina", zadnji, 48)
	dodajLetvuPotoka(t, oc, "5", "potok-dole", zadnji, 48)
	dodajLetvuPotoka(t, oc, "6", "potok-uz-glavni", zadnji, 48)

	sredina := zapisPotoka("potok-sredina", "potok-vrh", 2)
	sredina.Brojevi.Inacice[0][0].Odsjecak = 10
	sredina.Brojevi.Inacice[0][0].Ulazi[0].Nagib = 2
	dole := zapisPotoka("potok-dole", "potok-sredina", 3)
	dole.Racun.Rezerve = [][]string{{"potok-vrh"}}
	dole.Racun.Doseg = 6
	dole.Brojevi.Inacice = append(dole.Brojevi.Inacice, []Pojas{pojasPotoka("potok-dole", "potok-vrh", 5)})
	dole.Brojevi.Promasaji = map[int]Promasaj{4: {Letva: "potok-dole", Velicina: "vodostaj", DosegH: 4, Pomak: 1.5, Rasap: 2, Slucaja: 200}}
	uzGlavni := zapisPotoka("potok-uz-glavni", "donja", 1)
	okvir := OkvirModela{Glavne: map[string][][]Pojas{"donja": {{pojasPotoka("donja", "gornja", 3)}}}}
	for _, z := range []ZapisLetve{sredina, dole, uzGlavni} {
		if err := SpremiZapisLetve(o.Baza, z, "otisak-probe", okvir); err != nil {
			t.Fatalf("%s: %v", z.Letva, err)
		}
	}
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	po := map[string]map[int64]Izdana{}
	for _, x := range i.Izdane {
		if po[x.Letva] == nil {
			po[x.Letva] = map[int64]Izdana{}
		}
		po[x.Letva][x.Ciljni-sada] = x
	}
	// Sredina u +1 h čita vrh u −1 h: 51, pa 2·51 + 10.
	if x := po["potok-sredina"][1]; x.Vrijednost != 112 {
		t.Errorf("sredina +1 h: %v", x.Vrijednost)
	}
	// Dole u +4 h čita sredinu u +1 h, 112, uz promašaj 1,5 i njegov rasap.
	if x := po["potok-dole"][4]; x.Vrijednost != 110.5 || x.Dolje != 108.5 || x.Gore != 112.5 {
		t.Errorf("dole +4 h: %+v", x)
	}
	// U satu izdanja stoji mjerenje, a doseg 6 h je granica.
	if x := po["potok-dole"][0]; x.Vrijednost != 50 || x.Dolje != 50 || x.Gore != 50 {
		t.Errorf("dole +0 h: %+v", x)
	}
	if _, ima := po["potok-dole"][7]; ima {
		t.Error("račun ide preko dosega")
	}
	// Vrh ima samo mjerenje u satu izdanja.
	if len(po["potok-vrh"]) != 1 || po["potok-vrh"][0].Vrijednost != 50 {
		t.Errorf("vrh: %+v", po["potok-vrh"])
	}
	if len(po["potok-uz-glavni"]) < 2 {
		t.Errorf("letva koja čita glavni lanac: %+v", po["potok-uz-glavni"])
	}
	if err := o.Zapisi(i); err != nil {
		t.Fatal(err)
	}
	// Zapis ostaje nakon pripreme modela (Spremi) i ne ulazi u otisak modela.
	_, otisakPrije, err := SastaviModel(o.Baza)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Baza.Exec(`DELETE FROM model_letve`); err != nil {
		t.Fatal(err)
	}
	_, otisakBez, err := SastaviModel(o.Baza)
	if err != nil {
		t.Fatal(err)
	}
	if otisakPrije != otisakBez {
		t.Error("zapis je ušao u otisak modela prije nego što putuje")
	}
}

// Glavni ulaz kasni: račun ide iz rezerve i to kaže, a promašaji glavne inačice
// na rezervu se ne primjenjuju.
func TestOdvojeniRezerva(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji.Add(-100*time.Hour), 48)
	dodajLetvuPotoka(t, oc, "4", "potok-druga", zadnji, 48)
	dodajLetvuPotoka(t, oc, "5", "potok-dole", zadnji, 48)
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	z.Racun.Rezerve = [][]string{{"potok-druga"}}
	z.Brojevi.Inacice = append(z.Brojevi.Inacice, []Pojas{pojasPotoka("potok-dole", "potok-druga", 2)})
	z.Brojevi.Promasaji = map[int]Promasaj{1: {Pomak: 500, Rasap: 1}}
	upisiZapis(t, o.Baza, z)
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if i.Izbor["potok-dole"].Inacica != 1 {
		t.Fatalf("izbor: %+v", i.Izbor["potok-dole"])
	}
	for _, x := range i.Izdane {
		if x.Letva == "potok-dole" && x.Vrijednost < -100 {
			t.Errorf("promašaj glavne inačice primijenjen na rezervu: %+v", x)
		}
	}
}

// Sedam vrsta pokvarenog zapisa, i još dvije: izdanje ide, glavni račun je isti
// do kraja puta, ispada samo ta letva i ono što iz nje računa, a dobra letva
// računa dalje.
func TestPokvarenZapisIskljucujeSamoSvojeLetve(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	bez, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	glavniBez, putBez := otisakGlavnog(bez), otisakPuta(t, o, bez)
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-kvar", zadnji, 48)
	dodajLetvuPotoka(t, oc, "5", "potok-ispod", zadnji, 48)
	dodajLetvuPotoka(t, oc, "6", "potok-dobar", zadnji, 48)

	mijenjaj := func(izmjena func(*ZapisLetve)) string {
		z := zapisPotoka("potok-kvar", "potok-vrh", 2)
		izmjena(&z)
		return mustJSONZapis(t, z)
	}
	for ime, slucaj := range map[string]struct{ letva, sadrzaj string }{
		"nije json":        {"potok-kvar", `{{{`},
		"viši oblik":       {"potok-kvar", mijenjaj(func(z *ZapisLetve) { z.Oblik = 99 })},
		"nepoznata metoda": {"potok-kvar", mijenjaj(func(z *ZapisLetve) { z.Metoda = "samo-kisa" })},
		"glavna letva":     {"donja", mustJSONZapis(t, zapisPotoka("donja", "potok-vrh", 1))},
		"ulaz glavnom":     {"gornja", mustJSONZapis(t, zapisPotoka("gornja", "potok-vrh", 1))},
		"nagib ogroman":    {"potok-kvar", mijenjaj(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Ulazi[0].Nagib = 1e308 })},
		"rasap ogroman":    {"potok-kvar", mijenjaj(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Rasap = 1e200 })},
		"tuđa šifra":       {"potok-kvar", `{"oblik":1,"letva":"potok-dobar"}`},
		"bez brojeva":      {"potok-kvar", mijenjaj(func(z *ZapisLetve) { z.Brojevi.Inacice = nil })},
	} {
		t.Run(ime, func(t *testing.T) {
			if _, err := o.Baza.Exec(`DELETE FROM model_letve`); err != nil {
				t.Fatal(err)
			}
			upisiRedak(t, o.Baza, slucaj.letva, slucaj.sadrzaj)
			upisiZapis(t, o.Baza, zapisPotoka("potok-dobar", "potok-vrh", 2))
			if slucaj.letva == "potok-kvar" {
				upisiZapis(t, o.Baza, zapisPotoka("potok-ispod", "potok-kvar", 2))
			}
			i, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatalf("izdanje je palo: %v", err)
			}
			if otisakGlavnog(i) != glavniBez {
				t.Fatalf("glavni račun se promijenio")
			}
			if put := otisakPuta(t, o, i); put != putBez {
				t.Fatalf("na kraju puta glavni redci nisu isti")
			}
			if n, _ := izdanihLetve(i, "potok-dobar"); n == 0 {
				t.Errorf("dobra letva ne računa: %v", i.BezPrognoze)
			}
			if slucaj.letva != "potok-kvar" {
				// Letva glavnog računa radi dalje, pa upozorenje ne smije stajati na njoj.
				if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool {
					return u.Vrsta == VrstaOdvojeni && u.Tezina == TezinaNapomena && u.Letva == "" && strings.Contains(u.Opis, slucaj.letva)
				}) || slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaOdvojeni && u.Letva == slucaj.letva }) {
					t.Errorf("upozorenje za %s: %v", slucaj.letva, i.Upozorenja)
				}
				return
			}
			if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool {
				return u.Vrsta == VrstaOdvojeni && u.Tezina == TezinaUpozorenje && u.Letva == slucaj.letva
			}) {
				t.Errorf("nema upozorenja za %s: %v", slucaj.letva, i.Upozorenja)
			}
			for _, l := range []string{"potok-kvar", "potok-ispod"} {
				if n, _ := izdanihLetve(i, l); n != 0 {
					t.Errorf("%s ima %d redaka", l, n)
				}
				if i.BezPrognoze[l] == nil {
					t.Errorf("%s nije među letvama bez prognoze", l)
				}
			}
		})
	}
}

func mustJSONZapis(t *testing.T, z ZapisLetve) string {
	t.Helper()
	b, err := json.Marshal(z)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Krug samo kroz rezervu: a računa iz c (stoji), rezerva iz b, b iz a. S
// kašnjenjem od dva sata račun ga sam ne hvata.
func TestKrugKrozRezervuIskljucujeObje(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-a", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-b", zadnji, 48)
	dodajLetvuPotoka(t, oc, "5", "potok-c", zadnji.Add(-240*time.Hour), 48)
	a := zapisPotoka("potok-a", "potok-c", 2)
	a.Racun.Rezerve = [][]string{{"potok-b"}}
	a.Brojevi.Inacice = append(a.Brojevi.Inacice, []Pojas{pojasPotoka("potok-a", "potok-b", 2)})
	upisiZapis(t, o.Baza, a)
	upisiZapis(t, o.Baza, zapisPotoka("potok-b", "potok-a", 2))
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"potok-a", "potok-b"} {
		if n, _ := izdanihLetve(i, l); n != 0 {
			t.Errorf("%s ima %d redaka", l, n)
		}
		if err := i.BezPrognoze[l]; err == nil || !strings.Contains(err.Error(), "krug") {
			t.Errorf("%s: %v", l, err)
		}
	}
}

// Letva koja kasni: u satu izdanja nema računatog broja, a prognoza unaprijed ima.
// Letva izmjerena u tom satu ima mjerenje.
func TestSatIzdanjaSamoIzmjeren(t *testing.T) {
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sada := zadnji.Unix() / 3600
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji.Add(-5*time.Hour), 48)
	dodajLetvuPotoka(t, oc, "5", "potok-tu", zadnji, 48)
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	z.Brojevi.Inacice[0][0].Odsjecak = 77
	upisiZapis(t, o.Baza, z)
	upisiZapis(t, o.Baza, zapisPotoka("potok-tu", "potok-vrh", 2))
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var dole, tu int
	for _, x := range i.Izdane {
		switch {
		case x.Letva == "potok-dole" && x.Ciljni == sada:
			t.Errorf("računat broj u satu izdanja: %+v", x)
		case x.Letva == "potok-dole":
			dole++
		case x.Letva == "potok-tu" && x.Ciljni == sada:
			tu++
			if x.Vrijednost != 50 || x.Dolje != 50 || x.Gore != 50 {
				t.Errorf("mjerenje: %+v", x)
			}
		}
	}
	if dole == 0 || tu != 1 {
		t.Errorf("potok-dole %d, potok-tu %d", dole, tu)
	}
}

// Vrh koji kasni nema redak u satu izdanja: mjerenje od prije sat vremena ne
// smije stajati kao sadašnje, a premošćena rupa isto tako.
func TestSidroSamoAkoJeIzmjereno(t *testing.T) {
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sada := zadnji.Unix() / 3600
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji.Add(-2*time.Hour), 48)
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji, 48)
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := izdanihLetve(i, "potok-vrh"); n != 0 {
		t.Errorf("vrh koji kasni ima %d redaka u satu %d", n, sada)
	}
	if n, _ := izdanihLetve(i, "potok-dole"); n == 0 {
		t.Error("nema prognoze")
	}
}

// Letva koja drži zadnje mjerenje ima samo mjerenje i kaže to, a nizvodna letva
// računa iz njega i piše da je upitna.
func TestDrziZadnjeMjerenjeIUpozorava(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji.Add(-120*time.Hour), 48)
	dodajLetvuPotoka(t, oc, "4", "potok-sredina", zadnji, 48)
	dodajLetvuPotoka(t, oc, "5", "potok-dole", zadnji, 48)
	upisiZapis(t, o.Baza, zapisPotoka("potok-sredina", "potok-vrh", 2))
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-sredina", 2))
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := izdanihLetve(i, "potok-sredina"); n != 1 {
		t.Errorf("potok-sredina ima %d redaka, a treba samo mjerenje", n)
	}
	if n, _ := izdanihLetve(i, "potok-dole"); n < 2 {
		t.Errorf("potok-dole ima %d redaka", n)
	}
	k := slices.IndexFunc(i.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaOdvojeni && u.Letva == "potok-sredina" })
	if k < 0 || !strings.Contains(i.Upozorenja[k].Opis, "drži se zadnje mjerenje") {
		t.Fatalf("upozorenja: %v", i.Upozorenja)
	}
	if !slices.ContainsFunc(i.Upozorenja[k].Upitno, func(u Upitno) bool { return u.Letva == "potok-dole" }) {
		t.Errorf("nizvodna letva nije upitna: %+v", i.Upozorenja[k].Upitno)
	}
}

// Letva bez ijednog mjerenja sa svježim ulazom računa se, ali bez sata izdanja
// i uz napomenu.
func TestLetvaBezMjerenjaNosiNapomenu(t *testing.T) {
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool {
		return u.Vrsta == VrstaOdvojeni && u.Letva == "potok-dole" && strings.Contains(u.Opis, "nema mjerenja")
	}) {
		t.Errorf("upozorenja: %v", i.Upozorenja)
	}
}

// prepoznavacPotoka javlja kvar samo na zadanoj letvi, a ostalima ne.
type prepoznavacPotoka struct {
	letva string
	kvar  plato.Kvar
}

func (p prepoznavacPotoka) Kvarovi(_ context.Context, letva string, _ []plato.Tocka) ([]plato.Kvar, error) {
	if letva != p.letva {
		return nil, nil
	}
	return []plato.Kvar{p.kvar}, nil
}

// Kvar mjernog uređaja nove letve ulazi u napomene i ne ulazi u račun, a glavni
// račun je isti.
func TestKvarNoveLetve(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	bez, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	if _, err := oc.Exec(`INSERT INTO stations VALUES ('4', 'potok-dole')`); err != nil {
		t.Fatal(err)
	}
	for k := range 48 {
		if _, err := oc.Exec(`INSERT INTO readings (station_id, measured_at, level_cm, source) VALUES ('4', ?, 412, ?)`,
			zadnji.Add(-time.Duration(k)*time.Hour), models.ReadingSourceImport); err != nil {
			t.Fatal(err)
		}
	}
	o.Kvarovi = prepoznavacPotoka{letva: "potok-dole", kvar: plato.Kvar{Vrsta: plato.Zaglavljen, Od: zadnji.Add(-10 * time.Hour), Do: zadnji,
		Cm: 412, Traje: true, Susjed: &plato.Pomak{Letva: "potok-vrh", Cm: 40}}}
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if otisakGlavnog(bez) != otisakGlavnog(i) {
		t.Fatal("glavni račun se promijenio")
	}
	if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaKvarMjerila && u.Letva == "potok-dole" }) {
		t.Errorf("nema napomene o kvaru: %v", i.Upozorenja)
	}
	for _, x := range i.Izdane {
		if x.Letva == "potok-dole" && x.Ciljni == i.Sada {
			t.Errorf("sat izdanja iz kvara: %+v", x)
		}
	}
}

// Lažni skok nove letve ne ulazi u račun, a stoji u napomeni.
func TestSkokNoveLetve(t *testing.T) {
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	if _, err := oc.Exec(`INSERT INTO stations VALUES ('3', 'potok-vrh')`); err != nil {
		t.Fatal(err)
	}
	for k := range 48 {
		cm := 50
		if k == 5 {
			cm = 830
		}
		if _, err := oc.Exec(`INSERT INTO readings (station_id, measured_at, level_cm, source) VALUES ('3', ?, ?, ?)`,
			zadnji.Add(-time.Duration(k)*time.Hour), cm, models.ReadingSourceImport); err != nil {
			t.Fatal(err)
		}
	}
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji, 48)
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaSkok && u.Letva == "potok-vrh" }) {
		t.Errorf("nema napomene o skoku: %v", i.Upozorenja)
	}
	for _, x := range i.Izdane {
		if x.Letva == "potok-dole" && x.Vrijednost > 400 {
			t.Errorf("skok je ušao u račun: %+v", x)
		}
	}
}

// prepoznavacKojiStane stoji na letvi potoka: s kontekstom ili sasvim bez njega.
type prepoznavacKojiStane struct {
	letva     string
	nePazi    bool
	pusti     chan struct{}
	prvoStalo chan struct{}
}

func (p *prepoznavacKojiStane) Kvarovi(ctx context.Context, letva string, _ []plato.Tocka) ([]plato.Kvar, error) {
	if letva != p.letva {
		return nil, nil
	}
	close(p.prvoStalo)
	if p.nePazi {
		<-p.pusti
		return nil, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// Sporo čitanje nove letve: glavni brojevi su na vrijeme i isti, izdanje ide bez
// novih letvi i to kaže. Vrijedi i kad čitanje ne pazi na kontekst.
func TestSporoCitanjeNeZadrzavaIzdanje(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	postavi(t, &RokOdvojenih, 100*time.Millisecond)
	for _, nePazi := range []bool{false, true} {
		t.Run(fmt.Sprintf("ne pazi na kontekst %v", nePazi), func(t *testing.T) {
			zadnji := time.Now().UTC().Truncate(time.Hour)
			oc := probneOcitanja(t, 200, zadnji)
			o := probniOsvjezivac(t, oc)
			o.Iznova = true
			bez, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
			dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji, 48)
			if _, err := oc.Exec(`UPDATE readings SET source = ? WHERE station_id = '3'`, models.ReadingSourceImport); err != nil {
				t.Fatal(err)
			}
			upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
			p := &prepoznavacKojiStane{letva: "potok-vrh", nePazi: nePazi, pusti: make(chan struct{}), prvoStalo: make(chan struct{})}
			t.Cleanup(func() { close(p.pusti) })
			o.Kvarovi = p
			pocetak := time.Now()
			i, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if d := time.Since(pocetak); d > 10*time.Second {
				t.Errorf("izdanje je čekalo %s", d)
			}
			if otisakGlavnog(bez) != otisakGlavnog(i) {
				t.Fatal("glavni račun se promijenio")
			}
			if n, _ := izdanihLetve(i, "potok-dole"); n != 0 {
				t.Errorf("potok-dole ima %d redaka", n)
			}
			if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool {
				return u.Vrsta == VrstaOdvojeni && u.Tezina == TezinaUpozorenje && strings.Contains(u.Opis, "nisu računate")
			}) {
				t.Errorf("upozorenja: %v", i.Upozorenja)
			}
			if err := o.Zapisi(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// prepoznavacKojiPuca pada na letvi potoka.
type prepoznavacKojiPuca struct{ letva string }

func (p prepoznavacKojiPuca) Kvarovi(_ context.Context, letva string, _ []plato.Tocka) ([]plato.Kvar, error) {
	if letva == p.letva {
		panic("kvar u računu")
	}
	return nil, nil
}

// Panika u odvojenom računu ostaje u njemu.
func TestPanikaOstajeUOdvojenomRacunu(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	bez, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	if _, err := oc.Exec(`UPDATE readings SET source = ? WHERE station_id = '3'`, models.ReadingSourceImport); err != nil {
		t.Fatal(err)
	}
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	o.Kvarovi = prepoznavacKojiPuca{letva: "potok-vrh"}
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if otisakGlavnog(bez) != otisakGlavnog(i) {
		t.Fatal("glavni račun se promijenio")
	}
	if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaOdvojeni && strings.Contains(u.Opis, "nisu računate") }) {
		t.Errorf("upozorenja: %v", i.Upozorenja)
	}
}

// Svježa tuđa prognoza za ulaz ne mijenja nijedan redak: lanac tuđe ne čita.
func TestTudaPrognozaNeMijenjaOdvojeni(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sada := zadnji.Unix() / 3600
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji, 48)
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	otisakSvih := func(i *Ishod) string {
		out := make([]string, 0, len(i.Izdane))
		for _, x := range i.Izdane {
			out = append(out, fmt.Sprintf("%+v", x))
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	prije, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for k := range int64(48) {
		for _, l := range []string{"potok-vrh", "potok-dole", "gornja"} {
			if _, err := o.Baza.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES ('probni-izvor', ?, ?, ?, 900)`, l, sada, sada+k); err != nil {
				t.Fatal(err)
			}
		}
	}
	poslije, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if otisakSvih(prije) != otisakSvih(poslije) {
		t.Error("tuđa prognoza je promijenila izdane retke")
	}
}

// Račun koji iz dopuštenih brojeva da nemoguću vrijednost ne ulazi u izdanje.
func TestNemoguciBrojeviNeUlazeUIzdanje(t *testing.T) {
	for _, x := range []struct {
		i  Izdana
		ok bool
	}{
		{Izdana{Vrijednost: 100, Dolje: 90, Gore: 110}, true},
		{Izdana{Vrijednost: math.NaN()}, false},
		{Izdana{Vrijednost: 1, Gore: math.Inf(1)}, false},
		{Izdana{Vrijednost: 1e9}, false},
	} {
		if razumneIzdane([]Izdana{x.i}) != x.ok {
			t.Errorf("%+v", x.i)
		}
	}
}

// arhivaKrivulje je arhiva s jednom krivuljom protoka izmišljene letve.
func arhivaKrivulje(t *testing.T, letva, odsjecak string) *sql.DB {
	t.Helper()
	a, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "arhiva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.Exec(`
		CREATE TABLE hq_krivulje (id INTEGER, letva TEXT, vrijedi_od TEXT, vrijedi_do TEXT);
		CREATE TABLE hq_odsjecci (krivulja INTEGER, od_cm INTEGER, do_cm INTEGER, oblik TEXT,
			p1 REAL, p2 REAL, p3 REAL, p4 REAL);
		CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT);
		INSERT INTO hq_krivulje VALUES (1, '` + letva + `', '2000-01-01', '');
		INSERT INTO hq_odsjecci VALUES ` + odsjecak); err != nil {
		t.Fatal(err)
	}
	return a
}

// Nemoguć broj na vrhu novog računa ne smije zaustaviti izdanje. Sidro ide mimo
// ograde računa: pokvarena krivulja da beskonačnost ili NaN, a ogromno očitanje
// protoka zaokruživanjem postane beskonačnost tek u paketu.
func TestNemoguceSidroNeRusiIzdanje(t *testing.T) {
	for ime, s := range map[string]struct {
		krivulja string
		protok   bool
	}{
		"eksponent 1500, beskonačnost": {krivulja: `(1, -200, 900, 'potencija', 12.5, 1500, 1, 0);`},
		"eksponent 1500, NaN":          {krivulja: `(1, -200, 900, 'potencija', 0, 1500, 1, 0);`},
		"polinom, NaN":                 {krivulja: `(1, 0, 500, 'polinom', 1e308, -1e308, 0, 0);`},
		"izmjereni protok 1e306":       {protok: true},
	} {
		t.Run(ime, func(t *testing.T) {
			postavi(t, &PoluvijekIspravka, 0)
			zadnji := time.Now().UTC().Truncate(time.Hour)
			oc := probneOcitanja(t, 200, zadnji)
			o := probniOsvjezivac(t, oc)
			o.Iznova = true
			bez, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			glavni, put := otisakGlavnog(bez), otisakPuta(t, o, bez)
			dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
			dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji, 48)
			z := zapisPotoka("potok-dole", "potok-vrh", 2)
			if s.protok {
				if _, err := oc.Exec(`UPDATE readings SET level_cm = NULL, flow_m3s = 1e306 WHERE station_id = '3'`); err != nil {
					t.Fatal(err)
				}
				z.Velicina = "protok"
				z.Brojevi.Inacice[0][0].Velicina = "protok"
				z.Brojevi.Inacice[0][0].Ulazi[0].Velicina = "protok"
			} else {
				o.Arhiva = arhivaKrivulje(t, "potok-vrh", s.krivulja)
				if _, err := oc.Exec(`UPDATE readings SET level_cm = 301, source = ? WHERE station_id = '3'`, models.ReadingSourceImport); err != nil {
					t.Fatal(err)
				}
			}
			upisiZapis(t, o.Baza, z)
			i, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if otisakGlavnog(i) != glavni {
				t.Errorf("glavni račun se promijenio")
			}
			if p := otisakPuta(t, o, i); p != put {
				t.Errorf("na kraju puta glavni redci nisu isti")
			}
			for _, x := range i.Izdane {
				if !razumnaIzdana(x) {
					t.Errorf("nemoguć redak u izdanju: %+v", x)
				}
			}
			if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool {
				return u.Vrsta == VrstaOdvojeni && u.Letva == "potok-vrh" && strings.Contains(u.Opis, "nemoguć")
			}) {
				t.Errorf("nema napomene: %v", i.Upozorenja)
			}
		})
	}
}

// Letva odbijena zbog nemogućih brojeva povuče i letvu koja iz nje računa, s
// razlogom. U Racunalu još stoji njezin sirovi broj, pa bi nizvodna izdala
// prognozu koja izgleda uvjerljivo.
func TestNizvodnoOdOdbijeneLetve(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-sredina", zadnji.Add(-6*time.Hour), 48)
	dodajLetvuPotoka(t, oc, "5", "potok-dole", zadnji, 48)
	dodajLetvuPotoka(t, oc, "6", "potok-ispod", zadnji, 48)
	if _, err := oc.Exec(`UPDATE readings SET level_cm = 20000 WHERE station_id = '3'`); err != nil {
		t.Fatal(err)
	}
	sredina := zapisPotoka("potok-sredina", "potok-vrh", 2)
	sredina.Brojevi.Inacice[0][0].Ulazi[0].Nagib = 1000
	sredina.Brojevi.Inacice[0][0].Od, sredina.Brojevi.Inacice[0][0].Do = -1e6, 1e6
	dole := zapisPotoka("potok-dole", "potok-sredina", 2)
	dole.Brojevi.Inacice[0][0].Ulazi[0].Nagib = 1e-4
	dole.Brojevi.Inacice[0][0].Od, dole.Brojevi.Inacice[0][0].Do = -1e6, 1e6
	for _, z := range []ZapisLetve{sredina, dole, zapisPotoka("potok-ispod", "potok-dole", 1)} {
		upisiZapis(t, o.Baza, z)
	}
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"potok-sredina", "potok-dole", "potok-ispod"} {
		if n, _ := izdanihLetve(i, l); n != 0 {
			t.Errorf("%s ima %d redaka", l, n)
		}
		if i.BezPrognoze[l] == nil {
			t.Errorf("%s nije među letvama bez prognoze", l)
		}
	}
	if e := i.BezPrognoze["potok-dole"]; e == nil || !strings.Contains(e.Error(), "potok-sredina") {
		t.Errorf("razlog: %v", e)
	}
}

// Sporedni ulaz koji stoji pet dana u višoj razini: račun ne smije tiho držati
// njegov stari broj.
func TestStariUlazUVisojRazini(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji, 48)
	dodajLetvuPotoka(t, oc, "5", "potok-stari", zadnji.Add(-120*time.Hour), 48)
	z := zapisPotoka("potok-dole", "potok-vrh", 0)
	z.Racun.Ulazi = []string{"potok-vrh", "potok-stari"}
	niza := pojasPotoka("potok-dole", "potok-vrh", 0)
	niza.Od, niza.Do = -1000, 40
	visa := pojasPotoka("potok-dole", "potok-vrh", 0)
	visa.Od = 40
	visa.Ulazi = append(visa.Ulazi, Ulaz{Letva: "potok-stari", Velicina: "vodostaj", Sirina: 1, Nagib: 1})
	z.Brojevi.Inacice = [][]Pojas{{niza, visa}}
	upisiZapis(t, o.Baza, z)
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if i.Izbor["potok-dole"].Inacica != -1 || !strings.Contains(i.Izbor["potok-dole"].Opis, "potok-stari") {
		t.Errorf("izbor: %+v", i.Izbor["potok-dole"])
	}
	for _, x := range i.Izdane {
		if x.Letva == "potok-dole" && x.Ciljni != i.Sada {
			t.Errorf("redak računat iz starog ulaza: %+v", x)
		}
	}
}

// Pokvaren redak na letvi glavnog lanca ne gasi letvu koja je samo čita: glavna
// računa dalje s istim brojevima.
func TestPokvarenRedakGlavneNeGasiCitatelja(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji, 48)
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "donja", 2))
	prije, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	nPrije, _ := izdanihLetve(prije, "potok-dole")
	upisiRedak(t, o.Baza, "donja", `{"oblik":2,"letva":"donja"}`)
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n, _ := izdanihLetve(i, "potok-dole")
	if nPrije == 0 || n != nPrije {
		t.Errorf("potok-dole prije %d, poslije %d: %v", nPrije, n, i.BezPrognoze["potok-dole"])
	}
}

// Nova letva preko ruba krivulje nosi vlastitu napomenu, jer stranica polje Izvan
// ne čita, a upozorenje glavnog računa je već složeno.
func TestIzvanKrivuljeNapomena(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	o.Arhiva = arhivaKrivulje(t, "potok-dole", `(1, 0, 50, 'polinom', 0, 100, 0, 0);`)
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji, 48)
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	i, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(i.Izdane, func(x Izdana) bool { return x.Letva == "potok-dole" && x.Izvan }) {
		t.Fatal("nijedan redak nije izvan krivulje")
	}
	if !slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool {
		return u.Vrsta == VrstaOdvojeni && u.Letva == "potok-dole" && strings.Contains(u.Opis, "izvan krivulje")
	}) {
		t.Errorf("nema napomene: %v", i.Upozorenja)
	}
}

// Traka na vrhu stranice i Sažetak izvoza uzimaju prvo upozorenje, a ono ne smije
// biti o rječici dok glavni račun ima svoje.
func TestUpozorenjeGlavnogOstajePrvo(t *testing.T) {
	glavno := Upozorenje{Vrsta: VrstaVrh, Tezina: TezinaUpozorenje, Letva: "gornja", Opis: "x"}
	for ime, izvod := range map[string]izvodOdvojenih{
		"pokvaren zapis": {upozorenja: []Upozorenje{{Vrsta: VrstaOdvojeni, Tezina: TezinaUpozorenje, Letva: "potok", Opis: "x"}}},
		"prošao rok":     {greska: fmt.Errorf("x")},
	} {
		i := &Ishod{Upozorenja: []Upozorenje{glavno}, Izbor: map[string]Izbor{}, BezPrognoze: map[string]error{}}
		i.primiOdvojene(izvod, nil)
		if len(i.Upozorenja) != 2 || i.Upozorenja[0].Vrsta != VrstaVrh {
			t.Errorf("%s: %v", ime, i.Upozorenja)
		}
	}
}

// Baza modela koja se ne da pročitati ne ruši izdanje, nego se javlja.
func TestDodatniKrugBazaNeRadi(t *testing.T) {
	db := bazaModela(t)
	db.Close()
	i := &Ishod{Izbor: map[string]Izbor{}, BezPrognoze: map[string]error{}}
	(&Osvjezivac{Baza: db}).dodatniKrug(context.Background(), i, glavniRacun{})
	if len(i.Upozorenja) != 1 || i.Upozorenja[0].Vrsta != VrstaOdvojeni {
		t.Errorf("%v", i.Upozorenja)
	}
}

// Račun kojem je rok prošao ne daje ništa.
func TestOdvojeniRacunOtkazan(t *testing.T) {
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 48, zadnji)
	o := probniOsvjezivac(t, oc)
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	r := NovoRacunalo(nil, nil, zadnji.Unix()/3600)
	izvod := o.odvojeniRacun(ctx, ulazOdvojenih{glavniRacun: glavniRacun{racunalo: r, nizovi: map[Izvor]Niz{}},
		zapisi: map[string]ZapisLetve{"potok-dole": zapisPotoka("potok-dole", "potok-vrh", 2)}, sada: zadnji.Unix() / 3600})
	if izvod.greska == nil || len(izvod.izdane) != 0 {
		t.Errorf("%+v", izvod)
	}
}

// Letve glavnog računa su i one koje imaju samo dnevne retke ili su ostale bez
// prognoze, ne samo one sa satnim recima.
func TestLetveGlavnog(t *testing.T) {
	i := &Ishod{Izdane: []Izdana{{Letva: "a"}}, Dnevne: []DnevnaIzdana{{Letva: "b"}},
		BezPrognoze: map[string]error{"c": errPrimjer}, BezDnevne: map[string]error{"d": errPrimjer}}
	g := letveGlavnog(i, map[string][][]Pojas{"e": {{pojasPotoka("e", "f", 1)}}})
	for _, l := range []string{"a", "b", "c", "d", "e", "f"} {
		if !g[l] {
			t.Errorf("%s nije među glavnima", l)
		}
	}
	if g["potok-dole"] {
		t.Error("nova letva je među glavnima")
	}
}

var errPrimjer = fmt.Errorf("primjer")

// Niz koji se ne da pročitati ostaje prazan, pa njegova letva nije svježa.
func TestNizOdvojenihNeDaSeProcitati(t *testing.T) {
	oc := probneOcitanja(t, 48, time.Now().UTC().Truncate(time.Hour))
	o := probniOsvjezivac(t, oc)
	oc.Close()
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	n := o.nizoviOdvojenih(context.Background(), map[string]ZapisLetve{z.Letva: z}, glavniRacun{nizovi: map[Izvor]Niz{}})
	if len(n[Izvor{Letva: "potok-vrh", Velicina: "vodostaj"}].Sati) != 0 {
		t.Errorf("%v", n)
	}
}

// Letva bez računa ili bez ijednog ulaza u dosegu javlja razlog, a ne prazno.
func TestIzdaneLetveBezRedaka(t *testing.T) {
	o := &Osvjezivac{Model: ModelLanac}
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	u := ulazOdvojenih{sada: 100}
	u.najdalje = 12
	if _, err := o.izdaneLetve(context.Background(), NovoRacunalo(nil, nil, 100), z, true, Niz{}, u); err == nil {
		t.Error("letva bez računa")
	}
	r := NovoRacunalo(map[string][]Pojas{z.Letva: z.Brojevi.Inacice[0]}, nil, 100)
	if _, err := o.izdaneLetve(context.Background(), r, z, true, Niz{}, u); err == nil {
		t.Error("letva bez ulaza")
	}
}

// Sidra su uvijek istim redom, a letva glavnog računa ga ne dobiva.
func TestIzvoriSidara(t *testing.T) {
	dobri := map[string]ZapisLetve{"b": {Velicina: "vodostaj"}, "a": {Velicina: "vodostaj"}, "c": {Velicina: "protok"}}
	izbor := map[string]Izbor{"b": {Inacica: -1}, "a": {Inacica: -1}, "c": {Inacica: 0}}
	odabrano := map[string][]Pojas{"c": {pojasPotoka("c", "potok-vrh", 1), pojasPotoka("c", "zauzeta", 1), pojasPotoka("c", "he-proba", 1)}}
	got := izvoriSidara(dobri, odabrano, izbor, map[string]string{"zauzeta": "x"})
	var s []string
	for _, iz := range got {
		s = append(s, iz.Letva)
	}
	if strings.Join(s, " ") != "a b potok-vrh" {
		t.Errorf("%v", got)
	}
}

// Redak na letvi glavnog računa i ponovljeni redak ne ulaze, a nemoguć se javlja.
func TestPrimiRetke(t *testing.T) {
	i := &Ishod{}
	nap := i.primiRetke([]Izdana{{Letva: "glavna", Ciljni: 1}, {Letva: "nova", Ciljni: 1}, {Letva: "nova", Ciljni: 1},
		{Letva: "kriva", Ciljni: 1, Vrijednost: math.Inf(1)}}, map[string]bool{"glavna": true})
	if len(i.Izdane) != 1 || i.Izdane[0].Letva != "nova" {
		t.Errorf("%+v", i.Izdane)
	}
	if len(nap) != 1 || nap[0].Letva != "kriva" {
		t.Errorf("%+v", nap)
	}
}
