package prognoza

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// Letve u ovim testovima su izmišljene: glavni lanac gornja → donja i potok
// potok-vrh → potok-dole → potok-usce. Ušće čita i glavnu letvu donju, kao Jelengrad
// Osijek. Jedino TestGlavniLanacNeCitaOdvojene gleda šifre iz koda.

const potokUsce = "potok-usce"

// racuniPotoka zamjenjuje odvojene račune izmišljenima.
func racuniPotoka(t *testing.T) {
	t.Helper()
	postavi(t, &OdvojeniRacuni, []Racun{
		{"potok-dole", []string{"potok-vrh"}},
		{potokUsce, []string{"donja", "potok-dole"}},
	})
}

// spremiBrojevePotoka upisuje brojeve glavne letve i potoka u iste tablice, kao
// Pripremi model.
func spremiBrojevePotoka(t *testing.T, baza *sql.DB) {
	t.Helper()
	usce := Pojas{Letva: potokUsce, Velicina: "vodostaj", Od: -1000, Do: 1000, Odsjecak: 5, Rasap: 3, Ulazi: []Ulaz{
		{Letva: "donja", Velicina: "vodostaj", PomakH: 1, Sirina: 1, Nagib: 0.5},
		{Letva: "potok-dole", Velicina: "vodostaj", PomakH: 2, Sirina: 1, Nagib: 0.25}}}
	pojasi := []Pojas{pojasPotoka("donja", "gornja", 3), pojasPotoka("potok-dole", "potok-vrh", 2), usce}
	if err := Spremi(baza, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	if err := SpremiPromasaje(baza, []Promasaj{{Letva: "potok-dole", Velicina: "vodostaj", DosegH: 4, Pomak: 1.5, Rasap: 2, Slucaja: 200}}, "proba"); err != nil {
		t.Fatal(err)
	}
}

// retciLetvi su redci zadanih letvi, poredani.
func retciLetvi(i *Ishod, letve ...string) []string {
	var out []string
	for _, x := range i.Izdane {
		if slices.Contains(letve, x.Letva) {
			out = append(out, fmt.Sprintf("%+v", x))
		}
	}
	sort.Strings(out)
	return out
}

// Glavni lanac u kodu ne čita nijednu odvojenu letvu ni njezin vrh, a Pripremi
// model zna u čemu se svaka vodi.
func TestGlavniLanacNeCitaOdvojene(t *testing.T) {
	zauzete := zauzeteLetve(nil)
	vrhovi := map[string]bool{}
	odvojen := func(letva string) bool {
		return slices.ContainsFunc(OdvojeniRacuni, func(r Racun) bool { return r.Letva == letva })
	}
	for _, r := range OdvojeniRacuni {
		if _, zna := Velicine[r.Letva]; !zna {
			t.Errorf("%s: ne zna se veličina", r.Letva)
		}
		if razlog, zauzeta := zauzete[r.Letva]; zauzeta {
			t.Errorf("%s: %s", r.Letva, razlog)
		}
		for _, u := range r.Ulazi {
			if _, zna := Velicine[u]; !zna {
				t.Errorf("%s: ne zna se veličina ulaza %s", r.Letva, u)
			}
			if _, glavna := zauzete[u]; !glavna && !odvojen(u) {
				vrhovi[u] = true
			}
		}
	}
	if !vrhovi["kapelna"] || !vrhovi["benicanci-prkos"] || len(vrhovi) != 2 {
		t.Errorf("vrhovi odvojenih računa: %v", vrhovi)
	}
	if !odvojen("jelengrad") || odvojen("osijek") {
		t.Error("Jelengrad se računa odvojeno, a Osijek u glavnom lancu")
	}
}

// Vrh odvojenog računa bez očitanja ili u kašnjenju ne zaustavlja izdanje i ne
// pomiče mu sat. Dok su računi bili u glavnom lancu, isti vrh ga je rušio.
func TestVrhOdvojenogNeZaustavljaIzdanje(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	for _, s := range []struct {
		ime     string
		vrh     time.Duration // koliko je zadnje očitanje staro; <0 bez očitanja
		zeliBez bool          // potok-dole ostaje bez prognoze
		pada    bool          // račun u glavnom lancu pada
	}{
		{ime: "vrh bez ijednog očitanja", vrh: -1, zeliBez: true, pada: true},
		{ime: "vrh stoji pet dana", vrh: 120 * time.Hour, zeliBez: true},
		{ime: "vrh kasni 8 h", vrh: 8 * time.Hour, zeliBez: true},
		{ime: "vrh kasni 3 h", vrh: 3 * time.Hour},
		{ime: "vrh svjež"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			oc := probneOcitanja(t, 200, zadnji)
			o := probniOsvjezivac(t, oc)
			o.Iznova = true
			prije, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			glavniPrije := otisakGlavnog(prije)
			putPrije := otisakPuta(t, o, prije)

			spremiBrojevePotoka(t, o.Baza)
			if s.vrh >= 0 {
				dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji.Add(-s.vrh), 48)
			}
			postavi(t, &OdvojeniRacuni, []Racun{})
			uLancu, errLanca := o.Osvjezi(context.Background())
			if s.pada && errLanca == nil {
				t.Errorf("u glavnom lancu vrh bez očitanja nije srušio izdanje: %+v", uLancu.Sada)
			}

			racuniPotoka(t)
			i, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatalf("izdanje je stalo: %v", err)
			}
			if i.Sada != zadnji.Unix()/3600 {
				t.Errorf("sat izdanja se pomaknuo: %d", i.Sada)
			}
			if g := otisakGlavnog(i); g != glavniPrije {
				t.Errorf("glavni račun se promijenio:\n%s\n%s", glavniPrije, g)
			}
			if p := otisakPuta(t, o, i); p != putPrije {
				t.Errorf("na kraju puta glavni redci nisu isti:\n%s\n%s", putPrije, p)
			}
			if _, bez := i.BezPrognoze["potok-dole"]; bez != s.zeliBez {
				t.Errorf("potok-dole bez prognoze = %v: %v", bez, i.BezPrognoze)
			}
			n, _ := izdanihLetve(i, potokUsce)
			if n == 0 && !s.zeliBez {
				t.Errorf("ušće bez prognoze: %v", i.BezPrognoze)
			}
		})
	}
}

// Isti brojevi kao dok je račun bio u glavnom lancu, s promašajima i kad čita
// glavnu letvu, i kad potok kasni za satom izdanja. Tako je u redovnom izdanju:
// sat izdanja stigne prije mjerenja potoka, pa računata letva ima računat redak, a
// vrh zadnje mjerenje.
func TestOdvojeniIstiBrojeviKaoGlavni(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	for _, kasni := range []time.Duration{0, time.Hour, ZaostatakVrha * time.Hour} {
		t.Run(kasni.String(), func(t *testing.T) {
			oc := probneOcitanja(t, 200, zadnji)
			o := probniOsvjezivac(t, oc)
			o.Iznova = true
			dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji.Add(-kasni), 48)
			dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji.Add(-kasni), 48)
			dodajLetvuPotoka(t, oc, "5", potokUsce, zadnji.Add(-kasni), 48)
			spremiBrojevePotoka(t, o.Baza)

			postavi(t, &OdvojeniRacuni, []Racun{})
			uLancu, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			racuniPotoka(t)
			odvojeno, err := o.Osvjezi(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if odvojeno.Sada != zadnji.Unix()/3600 {
				t.Errorf("sat izdanja se pomaknuo: %d", odvojeno.Sada)
			}
			letve := []string{"potok-vrh", "potok-dole", potokUsce}
			if a, b := retciLetvi(uLancu, letve...), retciLetvi(odvojeno, letve...); !slices.Equal(a, b) || len(a) == 0 {
				t.Errorf("redci se razlikuju:\n%v\n%v", a, b)
			}
			if a, b := retciLetvi(uLancu, "gornja", "donja"), retciLetvi(odvojeno, "gornja", "donja"); !slices.Equal(a, b) {
				t.Errorf("glavni redci se razlikuju:\n%v\n%v", a, b)
			}
			for _, l := range letve {
				if !slices.ContainsFunc(odvojeno.Izdane, func(x Izdana) bool { return x.Letva == l && x.Ciljni == odvojeno.Sada }) {
					t.Errorf("%s nema redak u satu izdanja", l)
				}
			}
			// Promašaj ide uz račun iz koda: 1,5 oduzeto i njegov rasap.
			x := odvojeno.Izdane[slices.IndexFunc(odvojeno.Izdane, func(x Izdana) bool {
				return x.Letva == "potok-dole" && x.Ciljni == odvojeno.Sada+4
			})]
			if x.Gore-x.Dolje != 4 {
				t.Errorf("rasap promašaja nije primijenjen: %+v", x)
			}
		})
	}
}

// Letva iz retka modela i dalje u satu izdanja ima samo mjerenje, a letva iz koda
// kao u glavnom lancu.
func TestSatIzdanjaIzRetkaStroziOdKoda(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	oc := probneOcitanja(t, 200, zadnji)
	o := probniOsvjezivac(t, oc)
	o.Iznova = true
	dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji.Add(-time.Hour), 48)
	dodajLetvuPotoka(t, oc, "4", "potok-dole", zadnji.Add(-time.Hour), 48)
	spremiBrojevePotoka(t, o.Baza)
	racuniPotoka(t)
	uSatu := func(i *Ishod, letva string) bool {
		return slices.ContainsFunc(i.Izdane, func(x Izdana) bool { return x.Letva == letva && x.Ciljni == i.Sada })
	}
	izKoda, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !uSatu(izKoda, "potok-dole") || !uSatu(izKoda, "potok-vrh") {
		t.Errorf("iz koda: %v", retciLetvi(izKoda, "potok-dole", "potok-vrh"))
	}
	upisiZapis(t, o.Baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	izRetka, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if uSatu(izRetka, "potok-dole") {
		t.Errorf("iz retka: računat broj u satu izdanja: %v", retciLetvi(izRetka, "potok-dole"))
	}
}

// Glavni račun ne vidi brojeve odvojenih letvi, a provjera unatrag ih vidi, pa im
// promašaji ne nestaju pri pripremi modela.
func TestBrojeviOdvojenihOstajuUTablicama(t *testing.T) {
	racuniPotoka(t)
	baza := otvoriProbnu(t, "p.db")
	spremiBrojevePotoka(t, baza)
	glavne, err := SveInacice(baza)
	if err != nil {
		t.Fatal(err)
	}
	if len(glavne) != 1 || len(glavne["donja"]) != 1 {
		t.Errorf("glavni račun: %v", glavne)
	}
	sve, err := sveNamjesteno(baza)
	if err != nil {
		t.Fatal(err)
	}
	if len(sve) != 3 {
		t.Errorf("provjera unatrag vidi: %v", sve)
	}
	if p, err := SviPojasi(baza); err != nil || len(p) != 3 {
		t.Errorf("stranica vidi: %v, %v", p, err)
	}
	if _, otisak, err := SastaviModel(baza); err != nil || otisak == "" {
		t.Errorf("otisak modela: %q, %v", otisak, err)
	}
}

// Račun iz koda koji se nije dao namjestiti ne računa se i ne upozorava, kao ni dok
// je bio u glavnom lancu.
func TestOdvojeniBezBrojeva(t *testing.T) {
	racuniPotoka(t)
	baza := otvoriProbnu(t, "p.db")
	zapisi, pokvareni, err := zapisiZaRacun(baza, nil)
	if err != nil || len(zapisi)+len(pokvareni) != 0 {
		t.Errorf("%v %v %v", zapisi, pokvareni, err)
	}
}

// Redak vrijedi umjesto računa iz koda. Pokvaren redak isključuje letvu i ono što iz
// nje računa, a ne vraća račun iz koda.
func TestRedakVrijediUmjestoKoda(t *testing.T) {
	racuniPotoka(t)
	baza := otvoriProbnu(t, "p.db")
	spremiBrojevePotoka(t, baza)
	zapisi, _, err := zapisiZaRacun(baza, nil)
	if err != nil {
		t.Fatal(err)
	}
	if z := zapisi["potok-dole"]; !z.Ugradjen || z.Brojevi.Inacice[0][0].Ulazi[0].PomakH != 2 {
		t.Errorf("iz koda: %+v", z)
	}
	upisiZapis(t, baza, zapisPotoka("potok-dole", "potok-vrh", 7))
	zapisi, _, err = zapisiZaRacun(baza, nil)
	if err != nil {
		t.Fatal(err)
	}
	if z := zapisi["potok-dole"]; z.Ugradjen || z.Brojevi.Inacice[0][0].Ulazi[0].PomakH != 7 {
		t.Errorf("iz retka: %+v", z)
	}

	upisiRedak(t, baza, "potok-dole", "{pokvaren")
	zapisi, pokvareni, err := zapisiZaRacun(baza, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ima := zapisi["potok-dole"]; ima || pokvareni["potok-dole"] == nil {
		t.Errorf("pokvaren redak: %+v %v", zapisi["potok-dole"], pokvareni)
	}
	isk := ProvjeriZapise(zapisi, pokvareni, OkvirModela{})
	if isk[potokUsce] == nil {
		t.Errorf("ušće računa iz isključene letve: %v", isk)
	}
}

// Račun iz koda ne traži sektor u registru: računao je i bez njega. Redak ga traži.
func TestRacunIzKodaBezSektora(t *testing.T) {
	okvir := OkvirModela{ImaSektor: func(string) bool { return false }}
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	if err := z.Provjeri(okvir); err == nil {
		t.Error("redak bez sektora prolazi")
	}
	z.Ugradjen = true
	if err := z.Provjeri(okvir); err != nil {
		t.Errorf("račun iz koda: %v", err)
	}
}

// Promašaj preko dosega izdanja ne isključuje račun iz koda: provjera unatrag smije
// mjeriti i dalje, a takav se promašaj ni dosad nije koristio.
func TestPromasajPrekoDosegaIzKoda(t *testing.T) {
	racuniPotoka(t)
	baza := otvoriProbnu(t, "p.db")
	spremiBrojevePotoka(t, baza)
	promasaji := map[Izvor]map[int]Promasaj{{Letva: "potok-dole", Velicina: "vodostaj"}: {
		96:  {Pomak: 1, Rasap: 2},
		120: {Pomak: 1, Rasap: 2},
	}}
	zapisi, _, err := zapisiZaRacun(baza, promasaji)
	if err != nil {
		t.Fatal(err)
	}
	if err := zapisi["potok-dole"].Provjeri(OkvirModela{}); err != nil {
		t.Errorf("račun iz koda: %v", err)
	}
	if p := zapisi["potok-dole"].Brojevi.Promasaji; len(p) != 1 || p[96].Rasap != 2 {
		t.Errorf("promašaji: %v", p)
	}
	if len(promasaji[Izvor{Letva: "potok-dole", Velicina: "vodostaj"}]) != 2 {
		t.Error("izvorni promašaji su promijenjeni")
	}
}

// Baza koja se ne da pročitati ne ruši izdanje: greška ide pozivatelju, koji je
// pretvara u upozorenje.
func TestZapisiZaRacunGreskeBaze(t *testing.T) {
	racuniPotoka(t)
	baza := otvoriProbnu(t, "p.db")
	spremiBrojevePotoka(t, baza)
	if _, err := baza.Exec(`DROP TABLE ulazi`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := zapisiZaRacun(baza, nil); err == nil {
		t.Error("brojevi se ne daju pročitati, a greške nema")
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := zapisiZaRacun(baza, nil); err == nil {
		t.Error("baza je zatvorena, a greške nema")
	}
}

// Staro pravilo sata izdanja vrijedi za ulaze računa iz koda i za letvu iz koda koja
// drži zadnje mjerenje, a ne za letve iz retka.
func TestIzvoriIzKoda(t *testing.T) {
	dobri := map[string]ZapisLetve{"kod": {Ugradjen: true, Velicina: "vodostaj"}, "drzi": {Ugradjen: true, Velicina: "protok"},
		"rezerva": {Ugradjen: true, Velicina: "vodostaj"}, "redak": {Velicina: "vodostaj"}, "redak-drzi": {Velicina: "vodostaj"}}
	ulaz := func(letva, ime string) []Pojas {
		return []Pojas{{Letva: letva, Velicina: "vodostaj", Ulazi: []Ulaz{{Letva: ime, Velicina: "vodostaj"}}}}
	}
	odabrano := map[string][]Pojas{"kod": ulaz("kod", "vrh-koda"), "redak": ulaz("redak", "vrh-retka"), "rezerva": ulaz("rezerva", "vrh-rezerve")}
	izbor := map[string]Izbor{"drzi": {Inacica: -1}, "redak-drzi": {Inacica: -1}, "rezerva": {Inacica: 1}}
	out := izvoriIzKoda(dobri, odabrano, izbor)
	for _, iz := range []Izvor{{"vrh-koda", "vodostaj"}, {"vrh-rezerve", "vodostaj"}, {"drzi", "protok"}} {
		if !out[iz] {
			t.Errorf("nema %v: %v", iz, out)
		}
	}
	for _, iz := range []Izvor{{"vrh-retka", "vodostaj"}, {"redak-drzi", "vodostaj"}} {
		if out[iz] {
			t.Errorf("ima %v: %v", iz, out)
		}
	}
}

// Letva koja čita upitnu glavnu letvu i sama je upitna, od istog dosega kao da je
// u glavnom lancu. Krivulja i dnevni model ne idu niz lanac.
func TestDopuniUpitno(t *testing.T) {
	citatelji := map[string][]Pojas{potokUsce: {{Letva: potokUsce, Ulazi: []Ulaz{
		{Letva: "donja", PomakH: 5}, {Letva: "potok-dole", PomakH: 1}}}}}
	const sada = 1000
	for ime, s := range map[string]struct {
		u    Upozorenje
		zeli []Upitno
	}{
		"letva u popisu": {Upozorenje{Vrsta: VrstaUlaz, Letva: "gornja", Upitno: []Upitno{{Letva: "donja", OdSata: 4}}},
			[]Upitno{{Letva: "donja", OdSata: 4}, {Letva: potokUsce, OdSata: 9}}},
		"letva drži, nije u popisu": {Upozorenje{Vrsta: VrstaDrzi, Letva: "donja"},
			[]Upitno{{Letva: potokUsce, OdSata: 6}}},
		"nevolja prije izdanja": {Upozorenje{Vrsta: VrstaRekonstrukcija, Letva: "donja", Upitno: []Upitno{{Letva: "donja"}}},
			[]Upitno{{Letva: "donja"}, {Letva: potokUsce, OdSata: 1}}},
		"dnevno ide iza": {Upozorenje{Vrsta: VrstaUlaz, Letva: "donja", Upitno: []Upitno{{Letva: "donja", OdSata: 1}, {Letva: "zzz", OdSata: -1, Dnevno: true}}},
			[]Upitno{{Letva: "donja", OdSata: 1}, {Letva: potokUsce, OdSata: 6}, {Letva: "zzz", OdSata: -1, Dnevno: true}}},
		"samo dnevno": {Upozorenje{Vrsta: VrstaUlaz, Letva: "donja", Upitno: []Upitno{{Letva: "zzz", OdSata: -1, Dnevno: true}}},
			[]Upitno{{Letva: "zzz", OdSata: -1, Dnevno: true}}},
		"krivulja": {Upozorenje{Vrsta: VrstaKrivulja, Upitno: []Upitno{{Letva: "donja", Velicina: "protok", OdSata: 2}}},
			[]Upitno{{Letva: "donja", Velicina: "protok", OdSata: 2}}},
		"ništa nizvodno": {Upozorenje{Vrsta: VrstaUlaz, Letva: "gornja", Upitno: []Upitno{{Letva: "gornja", OdSata: 1}}},
			[]Upitno{{Letva: "gornja", OdSata: 1}}},
	} {
		t.Run(ime, func(t *testing.T) {
			u := []Upozorenje{s.u}
			dopuniUpitno(u, citatelji, sada)
			if !slices.Equal(u[0].Upitno, s.zeli) {
				t.Errorf("%+v, a treba %+v", u[0].Upitno, s.zeli)
			}
		})
	}
}

// Glavna upozorenja nose letvu iz koda koja je računala, a ne letvu iz retka ni
// letvu koja je pala.
func TestRacunatiIzKoda(t *testing.T) {
	dobri := map[string]ZapisLetve{"a": {Ugradjen: true}, "b": {}, "c": {Ugradjen: true}}
	odabrano := map[string][]Pojas{"a": nil, "b": nil, "c": nil}
	out := racunatiIzKoda(dobri, odabrano, map[string]error{"c": fmt.Errorf("pala")})
	if _, ima := out["a"]; !ima || len(out) != 1 {
		t.Errorf("%v", out)
	}
}

// Vrh koji mjeri i protok u toj veličini stoji kako je izmjeren, ne kroz krivulju.
// Bez protoka i bez krivulje redak u toj veličini ne postoji.
func TestSidroVrhaMjereniProtok(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	racuniPotoka(t)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	osvjezi := func(t *testing.T, protok bool) *Ishod {
		oc := probneOcitanja(t, 200, zadnji)
		o := probniOsvjezivac(t, oc)
		o.Iznova = true
		o.Arhiva = arhivaKrivulje(t, "neka-druga-letva", `(1, 0, 500, 'polinom', 100, 0, 0, 0);`)
		spremiBrojevePotoka(t, o.Baza)
		dodajLetvuPotoka(t, oc, "3", "potok-vrh", zadnji, 48)
		if protok {
			if _, err := oc.Exec(`UPDATE readings SET flow_m3s = 7 WHERE station_id = '3'`); err != nil {
				t.Fatal(err)
			}
		}
		i, err := o.Osvjezi(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	jeProtokVrha := func(x Izdana) bool { return x.Letva == "potok-vrh" && x.Velicina == "protok" }
	t.Run("izmjeren", func(t *testing.T) {
		i := osvjezi(t, true)
		k := slices.IndexFunc(i.Izdane, jeProtokVrha)
		if k < 0 {
			t.Fatalf("vrh nema protok: %v", retciLetvi(i, "potok-vrh"))
		}
		if x := i.Izdane[k]; x.Vrijednost != 7 || x.Racunata {
			t.Errorf("%+v", x)
		}
	})
	t.Run("bez protoka i krivulje", func(t *testing.T) {
		if i := osvjezi(t, false); slices.ContainsFunc(i.Izdane, jeProtokVrha) {
			t.Errorf("%v", retciLetvi(i, "potok-vrh"))
		}
	})
}

// Kad račun padne, poruka kaže i za Karašicu i Vučicu.
func TestPorukaKadOdvojeniNisuRacunati(t *testing.T) {
	i := &Ishod{BezPrognoze: map[string]error{}, Izbor: map[string]Izbor{}}
	i.primiOdvojene(izvodOdvojenih{greska: fmt.Errorf("računanje traje dulje od 20 s")}, nil)
	if len(i.Upozorenja) != 1 || !strings.HasPrefix(i.Upozorenja[0].Opis, "Karašica, Vučica i letve s novim modelom nisu računate") {
		t.Errorf("%+v", i.Upozorenja)
	}
}
