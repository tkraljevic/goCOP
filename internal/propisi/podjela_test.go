package propisi

import (
	"reflect"
	"strings"
	"testing"

	"gocop/internal/models"
)

func od(klasa, tekst string) Odlomak { return Odlomak{Klasa: klasa, Tekst: tekst} }

// podijeliPrimjer čita izmišljenu stranicu i dijeli je.
func podijeliPrimjer(t *testing.T, ime string) Podjela {
	t.Helper()
	return Podijeli(procitajPrimjer(t, ime).Odlomci)
}

func oznake(d []models.DioPropisa) []string {
	var out []string
	for _, x := range d {
		out = append(out, x.Oznaka)
	}
	return out
}

// dio je dio s oznakom; test pada kad ga nema.
func dio(t *testing.T, p Podjela, oznaka string) models.DioPropisa {
	t.Helper()
	for _, d := range p.Dijelovi {
		if d.Oznaka == oznaka {
			return d
		}
	}
	t.Fatalf("nema dijela %q u %v", oznaka, oznake(p.Dijelovi))
	return models.DioPropisa{}
}

// Odluke (25. 11. 2026.): zakon se dijeli na glave, odjeljke i članke.
// Glava i odjeljak imaju istu klasu NN, a razlikuju se po broju (rimski ili
// arapski); članak pamti roditelja, a naslov glave je bez broja.
func TestZakonGlaveOdjeljciClanci(t *testing.T) {
	p := podijeliPrimjer(t, "izmisljeni_zakon.html")
	zeli := []string{"glava-i", "glava-i-1", "clanak-1", "clanak-2", "glava-i-2", "clanak-3", "glava-ii", "glava-ii-1", "clanak-4", "clanak-5", "zavrsetak"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) {
		t.Fatalf("oznake %v, želi %v", got, zeli)
	}
	g, o, c := p.Dijelovi[0], p.Dijelovi[1], p.Dijelovi[2]
	if g.Vrsta != models.DioGlava || g.Broj != "I." || g.Naslov != "OPĆE ODREDBE" {
		t.Errorf("glava %+v", g)
	}
	if o.Vrsta != models.DioOdjeljak || o.Broj != "1." || o.Naslov != "Predmet zakona" || o.Roditelj != "glava-i" {
		t.Errorf("odjeljak %+v", o)
	}
	if c.Vrsta != models.DioClanak || c.Broj != "1." || c.Roditelj != "glava-i-1" || len(c.Stavci) != 2 {
		t.Errorf("članak %+v", c)
	}
	if got := OpsegIz(p.Dijelovi).Opis(); got != "2 glave, 3 odjeljka, 5 članaka" {
		t.Errorf("opseg %q", got)
	}
}

// Odluke (25. 11. 2026.): odjeljci se broje iznova u svakoj glavi (Zakon o
// vodama), pa ključ odjeljka nosi glavu: „glava-vii-2”.
func TestOdjeljciIznovaUSvakojGlavi(t *testing.T) {
	p := Podijeli([]Odlomak{
		od("t-10-9-sred", "VI. PRVA"), od("t-10-9-sred", "1. Prvi odjeljak"), od("clanak", "Članak 1."), od("t-9-8", "Tekst."),
		od("t-10-9-sred", "VII. DRUGA"), od("t-10-9-sred", "1. Prvi odjeljak"), od("clanak", "Članak 2."), od("t-9-8", "Tekst."),
		od("t-10-9-sred", "2. Drugi odjeljak"), od("clanak", "Članak 3."), od("t-9-8", "Tekst."),
	})
	zeli := []string{"glava-vi", "glava-vi-1", "clanak-1", "glava-vii", "glava-vii-1", "clanak-2", "glava-vii-2", "clanak-3"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) || len(p.Upozorenja) != 0 {
		t.Errorf("oznake %v (upozorenja %v), želi %v", got, p.Upozorenja, zeli)
	}
	if dio(t, p, "clanak-3").Roditelj != "glava-vii-2" {
		t.Error("članak 3. nije u odjeljku 2. glave VII.")
	}
}

// Odluke (25. 11. 2026.): naslov iznad članka pripada članku ispod njega.
// Naslov iza kojeg ne dođe članak nije bio naslov, pa ostaje tekst.
func TestNaslovIznadClankaIdeClanku(t *testing.T) {
	p := podijeliPrimjer(t, "izmisljeni_zakon.html")
	if got := dio(t, p, "clanak-1").Naslov; got != "Predmet uređivanja" {
		t.Errorf("naslov članka 1. %q", got)
	}
	if got := dio(t, p, "clanak-5").Naslov; got != "Stupanje na snagu" {
		t.Errorf("naslov članka 5. %q", got)
	}
	p = Podijeli([]Odlomak{
		od("clanak", "Članak 1."), od("t-9-8", "Tekst."), od("t-10-9-kurz-s", "Napomena"), od("t-10-9-kurz-s", "u dva retka"),
		od("klasa2", "Klasa: 1"),
	})
	c := dio(t, p, "clanak-1")
	if len(c.Stavci) != 3 || c.Stavci[1].Tekst != "Napomena" || c.Naslov != "" {
		t.Errorf("naslov bez članka: %+v", c)
	}
	p = Podijeli([]Odlomak{od("clanak", "Članak 1."), od("t-10-9-kurz-s", "Visi na kraju")})
	if c := dio(t, p, "clanak-1"); len(c.Stavci) != 1 || c.Stavci[0].Tekst != "Visi na kraju" {
		t.Errorf("naslov na kraju: %+v", c)
	}
}

// Odluke (25. 11. 2026.): odluka o proglašenju zakona nije dio zakona.
// Odvaja se od „Na temelju članka 89. Ustava” do potpisa i ne sprema se.
// „ODLUKU” bez potpisa iza sebe nije odluka o proglašenju.
func TestProglasenjeSeOdvaja(t *testing.T) {
	p := podijeliPrimjer(t, "izmisljeni_zakon.html")
	if !p.Proglasenje {
		t.Error("odluka o proglašenju nije prepoznata")
	}
	for _, d := range p.Dijelovi {
		if strings.Contains(TekstDijela(d), "Proglašavam") || d.Vrsta == models.DioUvod {
			t.Errorf("odluka o proglašenju je u dijelu %s", d.Oznaka)
		}
	}
	p = Podijeli([]Odlomak{od("tb-na16", "ODLUKU"), od("t-12-9-fett-s", "O PROGLAŠENJU NEČEGA"), od("t-9-8", "Tekst bez potpisa.")})
	if p.Proglasenje {
		t.Error("odluka bez potpisa je odvojena")
	}
	if p := Podijeli([]Odlomak{od("tb-na16", "ODLUKU"), od("t-12-9-fett-s", "O DRUGOM")}); p.Proglasenje {
		t.Error("odluka o drugom je odvojena")
	}
}

// Odluke (25. 11. 2026.): Državni plan ima dijelove bez broja (t-11-9-sred)
// i točke s rimskim brojem; točka ima adresu „xxiii” i pripada dijelu.
func TestPlanDijeloviIRimskeTocke(t *testing.T) {
	p := podijeliPrimjer(t, "izmisljeni_plan.html")
	zeli := []string{"uvod", "dio-1", "i", "ii", "dio-2", "iii", "zavrsetak"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) {
		t.Fatalf("oznake %v, želi %v", got, zeli)
	}
	d, i := p.Dijelovi[1], p.Dijelovi[2]
	if d.Vrsta != models.DioPlana || d.Naslov != "UVODNE ODREDBE" || i.Vrsta != models.DioTocka || i.Broj != "I." || i.Roditelj != "dio-1" {
		t.Errorf("dio %+v, točka %+v", d, i)
	}
	if p.Proglasenje || len(p.Upozorenja) != 0 {
		t.Errorf("proglašenje %v, upozorenja %v", p.Proglasenje, p.Upozorenja)
	}
	if got := OpsegIz(p.Dijelovi).Opis(); got != "2 dijela, 3 točke" {
		t.Errorf("opseg %q", got)
	}
	// „IIII.” nije pravilan rimski broj, pa nije točka.
	if p := Podijeli([]Odlomak{od("clanak", "IIII."), od("t-9-8", "Tekst.")}); len(p.Dijelovi) != 1 || p.Dijelovi[0].Vrsta != models.DioOdlomak {
		t.Errorf("nepravilan rimski broj: %v", oznake(p.Dijelovi))
	}
}

// Odluke (25. 11. 2026.): stavak „(1)” otvara stavak; točke „1.”, podtočke
// „2.1.”, slova „a)” i podstavci „–” idu u nabrajanje, a odlomak bez oznake
// iza nabrajanja u označenom stavku rečenica je iza nabrajanja. Bez oznaka
// stavaka (Plan) odlomak bez oznake je novi stavak.
func TestStavciTockePodtockeSlovaPodstavciNastavak(t *testing.T) {
	p := podijeliPrimjer(t, "izmisljeni_zakon.html")
	c2 := dio(t, p, "clanak-2").Stavci
	if len(c2) != 1 || c2[0].Oznaka != "" || len(c2[0].Tocke) != 3 {
		t.Fatalf("članak 2. %+v", c2)
	}
	dva := c2[0].Tocke[1]
	if dva.Vrsta != models.TockaBroj || dva.Oznaka != "2." || len(dva.Tocke) != 2 || !reflect.DeepEqual(dva.Tocke[1], models.TockaPropisa{Vrsta: models.TockaPodbroj, Oznaka: "2.2.", Tekst: "bilježi stanje"}) {
		t.Errorf("točka 2. %+v", dva)
	}
	c3 := dio(t, p, "clanak-3").Stavci
	if len(c3) != 2 || c3[0].Oznaka != "(1)" || c3[0].Tekst != "Čuvar nasipa dužan je:" || len(c3[0].Tocke) != 2 ||
		c3[0].Tocke[0].Vrsta != models.TockaSlovo || c3[0].Nastavak != "Za kvar javlja i mjesto udionici." {
		t.Errorf("članak 3. %+v", c3)
	}
	plan := podijeliPrimjer(t, "izmisljeni_plan.html")
	i := dio(t, plan, "i").Stavci
	if len(i) != 2 || len(i[0].Tocke) != 2 || i[0].Tocke[0].Vrsta != models.TockaPodstavak || i[0].Tocke[0].Oznaka != "" || i[1].Tekst != "Plan sadrži i mjere za obranu od leda." {
		t.Errorf("točka I. %+v", i)
	}
	s := slozStavke([]Odlomak{
		od("", "– podstavak bez uvoda"), od("", "(1) Uvod:"), od("", "1) prvo:"), od("", "a) slovo:"), od("", "– pod slovom"),
		od("", "b) drugo slovo"), od("", "– nije pod slovom bez dvotočke"), od("", "2) drugo"), od("", "Nastavak."), od("", "Još nastavka."),
	})
	if len(s) != 2 || len(s[0].Tocke) != 1 || s[1].Nastavak != "Nastavak.\nJoš nastavka." {
		t.Fatalf("stavci %+v", s)
	}
	prvo := s[1].Tocke[0]
	if len(s[1].Tocke) != 2 || prvo.Vrsta != models.TockaZagrada || len(prvo.Tocke) != 3 || len(prvo.Tocke[0].Tocke) != 1 {
		t.Errorf("nabrajanje %+v", s[1].Tocke)
	}
}

// Odluke (25. 11. 2026.): „Na temelju …” je uvod, a klasa, urbroj, mjesto
// i potpis su završetak; klasa s prijelomima daje stavak po retku.
func TestUvodIZavrsetak(t *testing.T) {
	plan := podijeliPrimjer(t, "izmisljeni_plan.html")
	uvod := dio(t, plan, "uvod")
	if len(uvod.Stavci) != 1 || !strings.HasPrefix(uvod.Stavci[0].Tekst, "Na temelju članka 10.") {
		t.Errorf("uvod %+v", uvod)
	}
	if z := dio(t, plan, "zavrsetak"); len(z.Stavci) != 4 || z.Stavci[3].Tekst != "Predsjednica\nIva Ivić, v. r." {
		t.Errorf("završetak plana %+v", z.Stavci)
	}
	zakon := podijeliPrimjer(t, "izmisljeni_zakon.html")
	z := dio(t, zakon, "zavrsetak")
	if len(z.Stavci) != 4 || z.Stavci[0].Tekst != "Klasa: 022-03/01-01/01" || z.Stavci[1].Tekst != "Zagreb, 2. ožujka 2001." || z.Stavci[2].Tekst != "HRVATSKI SABOR" {
		t.Errorf("završetak zakona %+v", z.Stavci)
	}
}

// Odluke (25. 11. 2026.): prilozi pravilnika su iza potpisa, pa se čitanje
// ne zaustavlja na potpisu. Naslov ispod „PRILOG 1.” naslov je priloga, a
// tablica je stavak priloga.
func TestPriloziIzaPotpisa(t *testing.T) {
	p := podijeliPrimjer(t, "izmisljeni_pravilnik.html")
	zeli := []string{"uvod", "clanak-1", "clanak-2", "zavrsetak", "prilog-1", "prilog-2"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) || len(p.Upozorenja) != 0 {
		t.Fatalf("oznake %v (upozorenja %v), želi %v", got, p.Upozorenja, zeli)
	}
	p1 := dio(t, p, "prilog-1")
	if p1.Naslov != "OPREMA ČUVARA" || p1.Broj != "1." || len(p1.Stavci) != 3 || p1.Stavci[1].Tablica == nil {
		t.Errorf("prilog 1. %+v", p1)
	}
	if p2 := dio(t, p, "prilog-2"); p2.Naslov != "" || len(p2.Stavci) != 2 {
		t.Errorf("prilog 2. %+v", p2)
	}
	if got := OpsegIz(p.Dijelovi).Opis(); got != "2 članka, 2 priloga" {
		t.Errorf("opseg %q", got)
	}
}

// Odluke (25. 11. 2026.): u zakonu o izmjenama „Članak 3.a” unutar bloka u
// navodnicima nije članak izmjene, nego novi tekst. Blok počinje iza
// „… koji glase:” odlomkom s » i traje dok se navodnici ne zatvore.
func TestBlokUNavodnicimaNijeClanak(t *testing.T) {
	p := podijeliPrimjer(t, "izmisljena_izmjena.html")
	zeli := []string{"clanak-1", "clanak-2", "clanak-3", "glava-1", "clanak-4", "clanak-5", "zavrsetak"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) {
		t.Fatalf("oznake %v, želi %v", got, zeli)
	}
	s := dio(t, p, "clanak-2").Stavci
	if len(s) != 4 || s[0].Navod || !s[1].Navod || s[2].Tekst != "Članak 3.a" || !s[3].Navod {
		t.Errorf("članak 2. izmjene %+v", s)
	}
	// Blok koji se ne zatvori je samo prvi odlomak; odlomak s » odmah iza
	// zatvorenog bloka nastavlja ga.
	o := []Odlomak{od("t-9-8", "Stavak 2. mijenja se i glasi:"), od("t-9-8", "»(2) Prvi."), od("t-9-8", "Drugi.")}
	if got := blokUNavodnicima(o, 1); got != 2 {
		t.Errorf("nezatvoren blok do %d", got)
	}
	o = []Odlomak{od("t-9-8", "dodaju se stavci 2. i 3. koji glase:"), od("t-9-8", "»(2) Prvi.«"), od("t-9-8", "»(3) Drugi.«."), od("t-9-8", "Dalje.")}
	if got := blokUNavodnicima(o, 1); got != 3 {
		t.Errorf("blok od dva navoda do %d", got)
	}
	if got := blokUNavodnicima(o, 0); got != 0 {
		t.Errorf("blok na prvom odlomku do %d", got)
	}
}

// Odluke (25. 11. 2026.): odlomak koji nije ni dio ni tekst dijela ostaje
// kao zaseban dio „odlomak-N”, na mjestu gdje je u tekstu, uz upozorenje
// „iza …”. Susjedni neprepoznati odlomci su jedan dio.
func TestNeprepoznatoOstajeSMjestom(t *testing.T) {
	p := Podijeli([]Odlomak{
		od("t-9-8", "Tekst prije svega."),
		od("t-11-9-sred", "PRVI DIO"), od("t-9-8", "Tekst ispod naslova."), {Klasa: "c-all", Tablica: &models.TablicaPropisa{Retci: [][]models.CelijaTablice{{{Tekst: "A"}, {Tekst: "B"}}}}},
		od("clanak", "XLV."), od("t-9-8", "Točka."),
		od("t-9-8-potpis", "Potpis"), od("t-9-8", "Iza potpisa."),
	})
	zeli := []string{"odlomak-1", "dio-1", "odlomak-2", "xlv", "zavrsetak", "odlomak-3"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) {
		t.Fatalf("oznake %v, želi %v", got, zeli)
	}
	var sto []string
	for _, u := range p.Upozorenja {
		sto = append(sto, u.Dio+": "+u.Sto)
	}
	if !reflect.DeepEqual(sto, []string{"odlomak-1: na početku teksta", "odlomak-2: iza dijela „PRVI DIO”", "odlomak-3: iza završetka"}) {
		t.Errorf("upozorenja %q", sto)
	}
	if u := p.Upozorenja[1]; !reflect.DeepEqual(u.Retci, []string{"Tekst ispod naslova.", "A | B"}) || !p.Dijelovi[2].Neprepoznato || p.Dijelovi[2].Roditelj != "dio-1" {
		t.Errorf("drugi neprepoznat odlomak %+v, %+v", u, p.Dijelovi[2])
	}
	if got := nazivDijela(models.DioPropisa{Vrsta: "nepoznato", Broj: "3"}, genitiv); got != "odlomka 3" {
		t.Errorf("naziv nepoznate vrste %q", got)
	}
}

// Odluke (25. 11. 2026.): dva dijela s istom oznakom ne smiju imati istu
// adresu; drugi dobiva „-2”, uz upozorenje.
func TestDvojnikOznakeDobivaNastavak(t *testing.T) {
	p := Podijeli([]Odlomak{
		od("clanak", "Članak 1."), od("t-9-8", "Prvi."), od("clanak", "Članak 1."), od("t-9-8", "Drugi."),
		od("clanak", "Članak 1."), od("klasa2", "Klasa: 1"), od("prilog", "PRILOG 1."), od("klasa2", "Klasa: 2"),
	})
	zeli := []string{"clanak-1", "clanak-1-2", "clanak-1-3", "zavrsetak", "prilog-1", "zavrsetak-2"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) {
		t.Fatalf("oznake %v, želi %v", got, zeli)
	}
	if len(p.Upozorenja) != 3 || p.Upozorenja[0].Vrsta != UpozorenjeDvojnik || p.Upozorenja[0].Sto != "Članak 1. se ponavlja, pa ponovljeni ima stalnu adresu „clanak-1-2”." {
		t.Errorf("upozorenja %+v", p.Upozorenja)
	}
}

// Odluke (25. 11. 2026.): ćirilično slovo u latiničnom tekstu (Zakon o
// vodama, članak 2.) daje jedno upozorenje uz taj članak.
func TestUpozorenjeOMijesanomPismu(t *testing.T) {
	p := podijeliPrimjer(t, "izmisljeni_zakon.html")
	var pismo []Upozorenje
	for _, u := range p.Upozorenja {
		if u.Vrsta == UpozorenjePismo {
			pismo = append(pismo, u)
		}
	}
	if len(pismo) != 1 || pismo[0].Dio != "clanak-2" || !strings.HasPrefix(pismo[0].Sto, "U članku 2. je slovo iz ćirilice") {
		t.Errorf("upozorenja o pismu %+v", pismo)
	}
}
