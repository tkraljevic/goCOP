package propisi

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Odluke (25. 11. 2026.): čitač se provjerava i na stvarnim stranicama
// Narodnih novina, ali one ne idu u javni repozitorij. Ispitni skup je
// izvan njega i test ga čita samo kad je zadan varijablom
// GOCOP_PROPISI_ISPIT; bez nje se preskače, pa pokrivenost daju izmišljeni
// primjeri. U porukama su samo brojke i oznake, nikad tekst propisa.
func TestIspitniSkupNarodnihNovina(t *testing.T) {
	dir := os.Getenv("GOCOP_PROPISI_ISPIT")
	if dir == "" {
		t.Skip("ispitni skup je izvan repozitorija; zadaj GOCOP_PROPISI_ISPIT")
	}
	akti := map[string]*procitanAkt{}
	for _, ime := range []string{"dpop", "2019_07_66_1285", "2021_07_84_1560", "2023_05_47_808", "2021_07_84_1557", "2023_05_47_812"} {
		akti[ime] = procitajIspit(t, filepath.Join(dir, ime+".html"))
	}
	zov := akti["2019_07_66_1285"]
	osnova := &models.Propis{Naziv: zov.p.Naziv, NN: zov.p.NN, ELI: zov.p.ELI, Veze: zov.p.Veze, Dijelovi: zov.d.Dijelovi}

	// Državni plan: dijelovi i rimske točke, bez stavaka s oznakom; potpis
	// je u završetku, pa neprepoznatog nema.
	t.Run("dpop", func(t *testing.T) {
		a := akti["dpop"]
		a.izdanje(t, models.IzdanjeNN{Godina: 2010, Broj: 84, Akt: 2389}, "2010-07-07", "2010-07-07")
		a.vrste(t, map[string]int{models.DioPlana: 10, models.DioTocka: 45, models.DioUvod: 1, models.DioZavrsetak: 1})
		a.stavci(t, 0, 109)
		a.tocke(t, map[string]int{models.TockaPodstavak: 126})
		if a.p.ELI != "" {
			t.Errorf("ELI %q, želi prazno", a.p.ELI)
		}
		if a.d.Dijelovi[2].Oznaka != "i" || a.oznaka(models.DioTocka, 45) != "xlv" {
			t.Errorf("oznake točaka %q … %q, želi i … xlv", a.d.Dijelovi[2].Oznaka, a.oznaka(models.DioTocka, 45))
		}
		a.bezNeprepoznatog(t)
	})

	// Zakon o vodama: glave, odjeljci koji se broje u svakoj glavi, članci
	// s naslovom, stavci, točke, podtočke, slova i podstavci; odluka o
	// proglašenju je odvojena, a jedno ćirilično slovo daje upozorenje.
	t.Run("2019_07_66_1285", func(t *testing.T) {
		a := zov
		a.izdanje(t, models.IzdanjeNN{Godina: 2019, Broj: 66, Akt: 1285}, "2019-07-10", "2019-07-18")
		a.vrste(t, map[string]int{models.DioGlava: 16, models.DioOdjeljak: 33, models.DioClanak: 254})
		bezNaslova := 0
		for _, d := range a.d.Dijelovi {
			if d.Vrsta == models.DioClanak && d.Naslov == "" {
				bezNaslova++
			}
		}
		if bezNaslova != 0 {
			t.Errorf("%d članaka bez naslova", bezNaslova)
		}
		a.stavci(t, 781, -1)
		a.tocke(t, map[string]int{models.TockaBroj: 514, models.TockaPodbroj: 40, models.TockaSlovo: 4, models.TockaPodstavak: 108})
		if !a.d.Proglasenje || a.p.ELI != "/eli/sluzbeni/2019/66/1285" {
			t.Errorf("proglašenje %v, ELI %q", a.d.Proglasenje, a.p.ELI)
		}
		a.veze(t, "/eli/sluzbeni/2021/84/1560", "/eli/sluzbeni/2023/47/808")
		pismo := a.upozorenja(UpozorenjePismo)
		if len(pismo) != 1 || pismo[0] != "clanak-2" {
			t.Errorf("upozorenja o pismu %v, želi [clanak-2]", pismo)
		}
		a.bezNeprepoznatog(t)
	})

	t.Run("2021_07_84_1560", func(t *testing.T) {
		a := akti["2021_07_84_1560"]
		a.izdanje(t, models.IzdanjeNN{Godina: 2021, Broj: 84, Akt: 1560}, "2021-07-23", "2021-07-31")
		a.vrste(t, map[string]int{models.DioClanak: 6})
		if a.p.Naziv != "Zakon o izmjenama Zakona o vodama" {
			t.Errorf("naziv %q", a.p.Naziv)
		}
		u := a.upute(t, osnova)
		u.broj(t, 7, 0, 1, 0)
		u.ciljevi(t, []string{"clanak-110", "clanak-141", "clanak-212", "clanak-212", "clanak-212", "clanak-236"}, 1)
		u.stupanjeU(t, "clanak-6")
	})

	t.Run("2023_05_47_808", func(t *testing.T) {
		a := akti["2023_05_47_808"]
		a.izdanje(t, models.IzdanjeNN{Godina: 2023, Broj: 47, Akt: 808}, "2023-05-03", "2023-05-04")
		a.vrste(t, map[string]int{models.DioClanak: 22})
		u := a.upute(t, osnova)
		u.broj(t, 27, 4, 1, 0)
		blago := u.izClanka(func(x models.UputaIzmjene) bool { return x.Blago })
		if len(blago) != 1 || blago[0] != "clanak-17" {
			t.Errorf("blago prepoznate %v, želi [clanak-17]", blago)
		}
		vlastite := u.izClanka(func(x models.UputaIzmjene) bool { return x.Vrsta == models.UputaVlastita })
		if strings.Join(vlastite, ",") != "clanak-18,clanak-19,clanak-20,clanak-21" {
			t.Errorf("vlastite odredbe u %v", vlastite)
		}
		u.stupanjeU(t, "clanak-22")
		zeli := []string{"1", "2", "4", "5", "25", "50", "97", "98", "99", "100", "101", "103", "105", "210", "224", "225", "226"}
		for i, b := range zeli {
			zeli[i] = "clanak-" + b
		}
		sort.Strings(zeli)
		if got := u.razlicitiCiljevi(); strings.Join(got, ",") != strings.Join(zeli, ",") {
			t.Errorf("ciljani članci %v, želi %v", got, zeli)
		}
	})

	t.Run("2021_07_84_1557", func(t *testing.T) {
		a := akti["2021_07_84_1557"]
		a.izdanje(t, models.IzdanjeNN{Godina: 2021, Broj: 84, Akt: 1557}, "2021-07-23", "2021-08-01")
		a.vrste(t, map[string]int{models.DioClanak: 5})
		u := a.upute(t, nil)
		// Plan (iz skripti) kaže 8 uputa koje mijenjaju zakon, ali i skripta
		// i čitač u člancima 1–3 nalaze 7 radnji u 6 rečenica: zamjena broja,
		// dodavanje stavka 5., dva dodavanja riječi u jednoj rečenici,
		// dodavanje stavaka 8. i 9., preoznačavanje i novi članak 99.
		u.broj(t, 7, 1, 1, 0)
	})

	// Pravilnik: prilozi su iza potpisa, s tablicama.
	t.Run("2023_05_47_812", func(t *testing.T) {
		a := akti["2023_05_47_812"]
		a.izdanje(t, models.IzdanjeNN{Godina: 2023, Broj: 47, Akt: 812}, "2023-05-03", "2023-05-11")
		a.vrste(t, map[string]int{models.DioClanak: 16, models.DioPrilog: 4})
		tablica := 0
		for _, d := range a.d.Dijelovi {
			for _, s := range d.Stavci {
				if s.Tablica != nil {
					tablica++
				}
			}
		}
		if tablica != 5 {
			t.Errorf("%d tablica, želi 5", tablica)
		}
		a.stavci(t, 39, -1)
		a.tocke(t, map[string]int{models.TockaBroj: 21, models.TockaZagrada: 65})
		a.bezNeprepoznatog(t)
	})

	// Isti akt kao zalijepljen tekst daje istu podjelu na dijelove.
	for ime, zeli := range map[string]map[string]int{
		"dpop":            {models.DioPlana: 10, models.DioTocka: 45},
		"2019_07_66_1285": {models.DioGlava: 16, models.DioClanak: 254},
	} {
		t.Run(ime+".txt", func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, ime+".txt"))
			if os.IsNotExist(err) {
				t.Skip("u skupu nema teksta akta")
			}
			if err != nil {
				t.Fatal(err)
			}
			a := &procitanAkt{p: ProcitajTekst(string(b))}
			a.d = Podijeli(a.p.Odlomci)
			a.vrste(t, zeli)
		})
	}
}

// procitanAkt je akt iz ispitnog skupa, pročitan i podijeljen.
type procitanAkt struct {
	p *Procitano
	d Podjela
}

func procitajIspit(t *testing.T, put string) *procitanAkt {
	t.Helper()
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ProcitajHTML(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(put), err)
	}
	return &procitanAkt{p: p, d: Podijeli(p.Odlomci)}
}

func (a *procitanAkt) izdanje(t *testing.T, nn models.IzdanjeNN, objava, stupa string) {
	t.Helper()
	if a.p.NN != nn || a.p.DatumObjave != objava {
		t.Errorf("izdanje %+v objava %q, želi %+v %q", a.p.NN, a.p.DatumObjave, nn, objava)
	}
	dan, err := time.Parse("2006-01-02", a.p.DatumObjave)
	if err != nil {
		t.Fatal(err)
	}
	if s, _, ok := StupaNaSnagu(a.d.Dijelovi, dan); !ok || s.Format("2006-01-02") != stupa {
		t.Errorf("stupa na snagu %v (%v), želi %s", s.Format("2006-01-02"), ok, stupa)
	}
}

func (a *procitanAkt) vrste(t *testing.T, zeli map[string]int) {
	t.Helper()
	got := map[string]int{}
	oznake := map[string]bool{}
	for _, d := range a.d.Dijelovi {
		got[d.Vrsta]++
		if oznake[d.Oznaka] {
			t.Errorf("oznaka %q se ponavlja", d.Oznaka)
		}
		oznake[d.Oznaka] = true
	}
	for v, n := range zeli {
		if got[v] != n {
			t.Errorf("%s: %d, želi %d (sve: %v)", v, got[v], n, got)
		}
	}
}

// oznaka je oznaka n-tog dijela zadane vrste.
func (a *procitanAkt) oznaka(vrsta string, n int) string {
	for _, d := range a.d.Dijelovi {
		if d.Vrsta == vrsta {
			if n--; n == 0 {
				return d.Oznaka
			}
		}
	}
	return ""
}

// stavci broji stavke s oznakom i bez nje u člancima, točkama i prilozima;
// -1 znači da se broj ne provjerava.
func (a *procitanAkt) stavci(t *testing.T, sOznakom, bezOznake int) {
	t.Helper()
	s, b := 0, 0
	for _, d := range a.d.Dijelovi {
		if d.Vrsta != models.DioClanak && d.Vrsta != models.DioTocka && d.Vrsta != models.DioPrilog {
			continue
		}
		for _, st := range d.Stavci {
			switch {
			case st.Navod || st.Tablica != nil:
			case st.Oznaka != "":
				s++
			default:
				b++
			}
		}
	}
	if s != sOznakom || (bezOznake >= 0 && b != bezOznake) {
		t.Errorf("stavaka s oznakom %d, bez oznake %d; želi %d i %d", s, b, sOznakom, bezOznake)
	}
}

func (a *procitanAkt) tocke(t *testing.T, zeli map[string]int) {
	t.Helper()
	got := map[string]int{}
	var broji func([]models.TockaPropisa)
	broji = func(tt []models.TockaPropisa) {
		for _, x := range tt {
			got[x.Vrsta]++
			broji(x.Tocke)
		}
	}
	for _, d := range a.d.Dijelovi {
		for _, s := range d.Stavci {
			broji(s.Tocke)
		}
	}
	for v, n := range zeli {
		if got[v] != n {
			t.Errorf("točaka %s: %d, želi %d (sve: %v)", v, got[v], n, got)
		}
	}
}

func (a *procitanAkt) veze(t *testing.T, eli ...string) {
	t.Helper()
	ima := map[string]bool{}
	for _, v := range a.p.Veze {
		ima[v.ELI] = true
	}
	for _, e := range eli {
		if !ima[e] {
			t.Errorf("nema veze na %s", e)
		}
	}
}

func (a *procitanAkt) upozorenja(vrsta string) []string {
	var dio []string
	for _, u := range a.d.Upozorenja {
		if u.Vrsta == vrsta {
			dio = append(dio, u.Dio)
		}
	}
	return dio
}

func (a *procitanAkt) bezNeprepoznatog(t *testing.T) {
	t.Helper()
	if n := a.upozorenja(UpozorenjeNeprepoznato); len(n) != 0 {
		t.Errorf("neprepoznato iza %v", n)
	}
	if n := a.upozorenja(UpozorenjeDvojnik); len(n) != 0 {
		t.Errorf("dvojnici %v", n)
	}
}

type upute []models.UputaIzmjene

// upute prepoznaje upute izmjene i, uz osnovu, povezuje ih s njom: svi
// ciljevi moraju postojati.
func (a *procitanAkt) upute(t *testing.T, osnova *models.Propis) upute {
	t.Helper()
	u := PrepoznajUpute(a.d.Dijelovi)
	if osnova == nil {
		return u
	}
	if !VezeSe(a.p, a.d.Dijelovi, osnova) {
		t.Error("izmjena se ne veže uz osnovu")
	}
	povezane, upozorenja := PoveziSOsnovom(u, osnova)
	for _, up := range upozorenja {
		t.Errorf("povezivanje: %s u %s", up.Sto, up.Dio)
	}
	return povezane
}

// broj provjerava koliko uputa mijenja propis, koliko je odredbi same
// izmjene, stupanja na snagu i neprepoznatih.
func (u upute) broj(t *testing.T, mijenja, vlastite, stupanje, neprepoznate int) {
	t.Helper()
	got := map[string]int{}
	m := 0
	for _, x := range u {
		got[x.Vrsta]++
		if x.MijenjaPropis() {
			m++
		}
	}
	if m != mijenja || got[models.UputaVlastita] != vlastite || got[models.UputaStupanje] != stupanje || got[models.UputaNeprepoznata] != neprepoznate {
		for _, x := range u {
			t.Logf("%s %s %v", x.IzClanka, x.Vrsta, x.Cilj.Clanci)
		}
		t.Errorf("mijenja %d, vlastite %d, stupanje %d, neprepoznate %d (sve: %v); želi %d, %d, %d, %d",
			m, got[models.UputaVlastita], got[models.UputaStupanje], got[models.UputaNeprepoznata], got, mijenja, vlastite, stupanje, neprepoznate)
	}
}

func (u upute) ciljevi(t *testing.T, clanci []string, cijeliTekst int) {
	t.Helper()
	var got []string
	cijeli := 0
	for _, x := range u {
		if x.Vrsta == models.UputaCijeliTekst {
			cijeli++
			continue
		}
		if x.MijenjaPropis() {
			got = append(got, x.Cilj.Clanci...)
		}
	}
	if strings.Join(got, ",") != strings.Join(clanci, ",") || cijeli != cijeliTekst {
		t.Errorf("ciljevi %v i %d u cijelom tekstu, želi %v i %d", got, cijeli, clanci, cijeliTekst)
	}
}

func (u upute) izClanka(uvjet func(models.UputaIzmjene) bool) []string {
	var out []string
	for _, x := range u {
		if uvjet(x) {
			out = append(out, x.IzClanka)
		}
	}
	return out
}

func (u upute) stupanjeU(t *testing.T, clanak string) {
	t.Helper()
	got := u.izClanka(func(x models.UputaIzmjene) bool { return x.Vrsta == models.UputaStupanje })
	if len(got) != 1 || got[0] != clanak {
		t.Errorf("stupanje u %v, želi [%s]", got, clanak)
	}
}

func (u upute) razlicitiCiljevi() []string {
	ima := map[string]bool{}
	for _, x := range u {
		if x.MijenjaPropis() {
			for _, c := range x.Cilj.Clanci {
				ima[c] = true
			}
		}
	}
	var out []string
	for c := range ima {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
