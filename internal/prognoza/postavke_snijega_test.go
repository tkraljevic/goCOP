package prognoza

import (
	"database/sql"
	"math"
	"reflect"
	"slices"
	"testing"
)

// Brojevi postavki moraju biti u razumnim granicama, i rubovi vrijede.
func TestProvjeraPostavkiSnijega(t *testing.T) {
	for _, p := range []PostavkeSnijega{ZadanePostavkeSnijega, {Topljenje: 1, Prag: -2}, {Ukljucen: true, Topljenje: 8, Prag: 3}} {
		if err := p.Provjeri(); err != nil {
			t.Errorf("%+v: %v", p, err)
		}
	}
	for _, s := range []struct {
		p    PostavkeSnijega
		zeli string
	}{
		{PostavkeSnijega{Topljenje: 0.5}, "brzina topljenja"},
		{PostavkeSnijega{Topljenje: 8.5}, "brzina topljenja"},
		{PostavkeSnijega{Topljenje: math.NaN()}, "brzina topljenja"},
		{PostavkeSnijega{Topljenje: 3, Prag: -2.5}, "temperatura topljenja"},
		{PostavkeSnijega{Topljenje: 3, Prag: 3.1}, "temperatura topljenja"},
		{PostavkeSnijega{Topljenje: 3, Prag: math.NaN()}, "temperatura topljenja"},
	} {
		if err := s.p.Provjeri(); !porukaIma(err, s.zeli) {
			t.Errorf("%+v: %v", s.p, err)
		}
	}
}

// temperaturaIzmisljena je srednja dnevna temperatura s godišnjim hodom:
// zimi ispod nule, ljeti iznad.
func temperaturaIzmisljena(d int64) float64 {
	return 9*math.Sin(2*math.Pi*float64(d-110)/365) + 3*math.Sin(float64(d)/2.3)
}

// arhivaSnijega je arhiva s izmišljenim slivom B na kojem zimi pada snijeg:
// letva „donja” prima kišu i snijeg koji se topi faktorom 6 iznad nule, dan
// kasnije, uz val s uzvodne „gornja”.
func arhivaSnijega(t *testing.T) *sql.DB {
	t.Helper()
	arhiva, _ := arhivaUDatoteci(t)
	const dana = 4500
	pravo := PostavkeSnijega{Ukljucen: true, Topljenje: 6}
	otjece := make([]float64, dana)
	zaliha := 0.0
	snijeg := func(d int64) float64 {
		if temperaturaIzmisljena(d) < 0 {
			return kisaIzmisljena(d)
		}
		return 0
	}
	for d := range int64(dana) {
		dan := DanOborine{Oborina: kisaIzmisljena(d), Snijeg: snijeg(d), Temperatura: temperaturaIzmisljena(d), ImaSnijega: true}
		var palo, topi float64
		zaliha, palo, topi = pravo.Korak(zaliha, dan)
		otjece[d] = dan.Oborina - palo + topi
	}
	upisiDnevne(t, arhiva, "gornja", "vodostaj", dana, valIzmisljeni)
	upisiDnevne(t, arhiva, "donja", "vodostaj", dana, func(d int64) float64 {
		if d == 0 {
			return valIzmisljeni(0)
		}
		return valIzmisljeni(d-2) + 4*otjece[d-1] + 20
	})
	upisiDnevne(t, arhiva, "tocka-b", "oborina", dana, kisaIzmisljena)
	upisiDnevne(t, arhiva, "tocka-b", "snijeg", dana, snijeg)
	upisiDnevne(t, arhiva, "tocka-b", "temperatura-zraka", dana, temperaturaIzmisljena)
	return arhiva
}

// Postavke snijega idu od nacrta preko probe do primjene, kao model letve:
// nacrt ne mijenja prognozu, proba mjeri primijenjene i nove postavke na
// istim danima snježne sezone i ne dira postavke u radu, primjena traži
// probu prema sadašnjim postavkama i razlog, a primijenjene vrijede odmah i
// bacaju naučene dnevne modele.
func TestNacrtProbaIPrimjenaSnijega(t *testing.T) {
	bezDnevnePostave(t)
	postaviSnijeg(t, ZadanePostavkeSnijega)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}, Slivovi: []string{"B"}}, {Letva: "bez-kise"}})
	postavi(t, &CiljeviProbeSnijega, []string{"donja", "bez-kise", "nije-cilj"})
	arhiva, baza := arhivaSnijega(t), otvoriProbnu(t, "p.db")

	if z, ima, err := PrimijenjeneSnijega(baza); err != nil || ima || z.Postavke != ZadanePostavkeSnijega {
		t.Fatalf("bez retka vrijede zadane: %+v, %v, %v", z, ima, err)
	}
	promjena := PromjenaLetve{Kad: "25. 11. 2026. 10:00", Tko: "Iva Proba", Razlog: "proljeće je došlo ranije"}
	if err := PrimijeniNacrtSnijega(baza, promjena); !porukaIma(err, "nije proban") {
		t.Errorf("primjena bez nacrta: %v", err)
	}
	if _, err := ProbajNacrtSnijega(arhiva, baza, tockeB, "10:00"); !porukaIma(err, "nema nacrta") {
		t.Errorf("proba bez nacrta: %v", err)
	}
	if err := SpremiNacrtSnijega(baza, NacrtSnijega{Postavke: PostavkeSnijega{Ukljucen: true, Topljenje: 12}}); !porukaIma(err, "brzina topljenja") {
		t.Errorf("nacrt izvan granica: %v", err)
	}
	nove := PostavkeSnijega{Ukljucen: true, Topljenje: 6}
	if err := SpremiNacrtSnijega(baza, NacrtSnijega{Postavke: nove, Tko: "Iva Proba", Kad: "09:55"}); err != nil {
		t.Fatal(err)
	}
	if nacrti, err := Nacrti(baza); err != nil || len(nacrti) != 0 {
		t.Errorf("nacrt snijega nije nacrt računa letve: %v, %v", nacrti, err)
	}
	if err := PrimijeniNacrtSnijega(baza, promjena); !porukaIma(err, "nije proban") {
		t.Errorf("primjena bez probe: %v", err)
	}

	dnevniModeli.Lock()
	dnevniModeli.dan = "naučeno"
	dnevniModeli.Unlock()
	n, err := ProbajNacrtSnijega(arhiva, baza, tockeB, "10:00")
	if err != nil {
		t.Fatal(err)
	}
	if PostavkeSnijegaURadu() != ZadanePostavkeSnijega || dnevniModeli.dan != "naučeno" {
		t.Errorf("proba ne smije dirati postavke u radu ni naučene modele: %+v, %q", PostavkeSnijegaURadu(), dnevniModeli.dan)
	}
	p := n.Proba
	if p == nil || p.Primijenjene != ZadanePostavkeSnijega || len(p.Razdoblje) != 2 || len(p.Letve) != 1 || p.Natpisi[0] != NatpisSnjezneZime {
		t.Fatalf("proba: %+v", p)
	}
	l := p.Letve[0]
	if l.Letva != "donja" || len(l.Sada) != DnevniDosezi || len(l.Nacrt) != DnevniDosezi || l.Greska != "" || l.Sada[0].Slucaja == 0 {
		t.Fatalf("proba letve: %+v", l)
	}
	// letva je složena s faktorom 6, pa ga nacrt pogađa bolje od zadanog 3
	if l.Ocjena != SnijegBolje || l.Nacrt[0].Promasaj >= l.Sada[0].Promasaj {
		t.Errorf("faktor 6 mora biti bolji: %s, 1. dan %.2f prema %.2f", l.Ocjena, l.Nacrt[0].Promasaj, l.Sada[0].Promasaj)
	}
	// samo dani od studenoga do lipnja: dvije godine imaju ih oko 485
	if s := l.Sada[0].Slucaja; s < 450 || s > 490 {
		t.Errorf("mjereno na %d dana, želim samo snježnu sezonu", s)
	}

	// isti nacrt spremljen iznova zadržava probu, promijenjen je gubi
	if err := SpremiNacrtSnijega(baza, NacrtSnijega{Postavke: nove, Tko: "Iva Proba"}); err != nil {
		t.Fatal(err)
	}
	if n, _, err := NacrtSnijegaIz(baza); err != nil || n.Proba == nil {
		t.Error("isti nacrt izgubio je probu")
	}

	// proba prema drugim primijenjenim postavkama ne vrijedi
	staro := *n.Proba
	n.Proba.Primijenjene.Topljenje = 4
	if err := upisiNacrtSnijega(baza, n); err != nil {
		t.Fatal(err)
	}
	if err := PrimijeniNacrtSnijega(baza, promjena); !porukaIma(err, "isprobaj ga iznova") {
		t.Errorf("primjena stare probe: %v", err)
	}
	n.Proba = &staro
	if err := upisiNacrtSnijega(baza, n); err != nil {
		t.Fatal(err)
	}
	if err := PrimijeniNacrtSnijega(baza, PromjenaLetve{Razlog: " "}); !porukaIma(err, "upiši razlog") {
		t.Errorf("primjena bez razloga: %v", err)
	}
	if err := PrimijeniNacrtSnijega(baza, promjena); err != nil {
		t.Fatal(err)
	}
	z, ima, err := PrimijenjeneSnijega(baza)
	if err != nil || !ima || z.Postavke != nove || !reflect.DeepEqual(z.Promjene, []PromjenaLetve{promjena}) || z.Zadnja() != promjena {
		t.Errorf("primijenjeno: %+v, %v, %v", z, ima, err)
	}
	if _, ima, err := NacrtSnijegaIz(baza); err != nil || ima {
		t.Error("nacrt se nakon primjene briše")
	}
	if PostavkeSnijegaURadu() != nove || dnevniModeli.dan != "" {
		t.Errorf("primijenjene postavke vrijede odmah i bacaju naučene modele: %+v, %q", PostavkeSnijegaURadu(), dnevniModeli.dan)
	}

	// odbačen nacrt nestaje, a primijenjene ostaju
	if err := SpremiNacrtSnijega(baza, NacrtSnijega{Postavke: ZadanePostavkeSnijega}); err != nil {
		t.Fatal(err)
	}
	if err := ObrisiNacrtSnijega(baza); err != nil {
		t.Fatal(err)
	}
	if _, ima, err := NacrtSnijegaIz(baza); err != nil || ima {
		t.Error("odbačen nacrt ostao je")
	}
	if z, _, err := PrimijenjeneSnijega(baza); err != nil || z.Postavke != nove {
		t.Errorf("odbacivanje nacrta promijenilo je primijenjeno: %+v", z)
	}
}

// Proba koja se ne da izmjeriti ni na jednoj letvi javlja zašto i ne upisuje
// ništa u nacrt.
func TestProbaSnijegaBezPodataka(t *testing.T) {
	bezDnevnePostave(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}, Slivovi: []string{"B"}}})
	postavi(t, &CiljeviProbeSnijega, []string{"donja"})
	arhiva, _ := arhivaUDatoteci(t)
	baza := otvoriProbnu(t, "p.db")
	if err := SpremiNacrtSnijega(baza, NacrtSnijega{Postavke: ZadanePostavkeSnijega}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajNacrtSnijega(arhiva, baza, tockeB, "10:00"); !porukaIma(err, "ni na jednoj letvi (donja:") {
		t.Errorf("proba bez arhive: %v", err)
	}
	if n, _, err := NacrtSnijegaIz(baza); err != nil || n.Proba != nil {
		t.Errorf("neuspjela proba upisala je ishod: %+v", n.Proba)
	}
}

// Primijenjene postavke putuju s modelom izdavača; prazna tablica ne mijenja
// otisak modela, a primljena vrijedi nakon osvježavanja kao i na izdavaču.
func TestPostavkeSnijegaPutujuSModelom(t *testing.T) {
	postaviSnijeg(t, ZadanePostavkeSnijega)
	izdavac, primatelj := otvoriProbnu(t, "izdavac.db"), otvoriProbnu(t, "primatelj.db")
	_, prazan, err := SastaviModel(izdavac)
	if err != nil {
		t.Fatal(err)
	}
	// otisak je isti kao kod starijeg programa koji tablicu ne zna, pa
	// nadogradnja izdavača ne objavljuje model iznova
	postavi(t, &tabliceModela, slices.DeleteFunc(slices.Clone(tabliceModela), func(s string) bool { return s == tablicaPostavki }))
	if _, stari, err := SastaviModel(izdavac); err != nil || stari != prazan {
		t.Errorf("prazna tablica postavki mijenja otisak: %v", err)
	}
	postavi(t, &tabliceModela, append(slices.Clone(tabliceModela), tablicaPostavki))
	upisiPrimijenjeno(t, izdavac, PostavkeSnijega{Ukljucen: false, Topljenje: 2, Prag: 1})
	paket, otisak, err := SastaviModel(izdavac)
	if err != nil || otisak == prazan {
		t.Fatalf("primijenjene postavke moraju promijeniti otisak: %v", err)
	}
	if err := PrimiModel(primatelj, paket); err != nil {
		t.Fatal(err)
	}
	OsvjeziDnevnuPostavu(primatelj)
	if want := (PostavkeSnijega{Ukljucen: false, Topljenje: 2, Prag: 1}); PostavkeSnijegaURadu() != want {
		t.Errorf("primatelj računa s %+v, želim %+v", PostavkeSnijegaURadu(), want)
	}
}

// upisiPrimijenjeno upiše postavke snijega kroz nacrt, probu i primjenu,
// bez arhive: proba je složena rukom.
func upisiPrimijenjeno(t *testing.T, baza *sql.DB, p PostavkeSnijega) {
	t.Helper()
	z, _, err := PrimijenjeneSnijega(baza)
	if err != nil {
		t.Fatal(err)
	}
	if err := upisiNacrtSnijega(baza, NacrtSnijega{Postavke: p, Proba: &ProbaSnijega{Primijenjene: z.Postavke}}); err != nil {
		t.Fatal(err)
	}
	if err := PrimijeniNacrtSnijega(baza, PromjenaLetve{Kad: "25. 11. 2026. 10:00", Razlog: "proba"}); err != nil {
		t.Fatal(err)
	}
}

// Pokvaren redak postavki ne zaustavlja račun: vrijede zadane, uz grešku.
func TestPokvarenePostavkeSnijega(t *testing.T) {
	postaviSnijeg(t, PostavkeSnijega{Ukljucen: true, Topljenje: 5})
	baza := otvoriProbnu(t, "p.db")
	if _, err := baza.Exec(`INSERT INTO model_postavke (kljuc, sadrzaj) VALUES ('snijeg', '{"postavke":{"ukljucen":true,"topljenje":99}}')`); err != nil {
		t.Fatal(err)
	}
	if err := UcitajPostavkeSnijega(baza); !porukaIma(err, "brzina topljenja") || PostavkeSnijegaURadu() != ZadanePostavkeSnijega {
		t.Errorf("pokvaren redak: %v, %+v", err, PostavkeSnijegaURadu())
	}
	if _, err := baza.Exec(`UPDATE model_postavke SET sadrzaj = 'nije json'`); err != nil {
		t.Fatal(err)
	}
	if err := UcitajPostavkeSnijega(baza); err == nil {
		t.Error("redak koji nije JSON bez greške")
	}
}

// Isključen model snijega daje dnevnom modelu oborinu kakva je pala, u učenju
// i uživo, kao prije modela snijega.
func TestIskljucenSnijegDajeUkupnuOborinu(t *testing.T) {
	postaviSnijeg(t, PostavkeSnijega{Ukljucen: false, Topljenje: 3})
	arhiva, _ := arhivaUDatoteci(t)
	upisiDnevne(t, arhiva, "tocka-b", "oborina", 400, kisaIzmisljena)
	upisiDnevne(t, arhiva, "tocka-b", "snijeg", 400, kisaIzmisljena)
	upisiDnevne(t, arhiva, "tocka-b", "temperatura-zraka", 400, temperaturaIzmisljena)
	s, err := DnevneOborine(arhiva, tockeB)
	uk, errUk := UkupneDnevneOborine(arhiva, tockeB)
	if err != nil || errUk != nil || !reflect.DeepEqual(s, uk) {
		t.Errorf("učenje s isključenim snijegom mora dobiti ukupnu oborinu: %v, %v", err, errUk)
	}
	iz := izvorSaSnijegom()
	uzivo, err := OborineOkoSada(iz, sadaIzvora, 2, 1)
	bez, errBez := OborineOkoSada(iz.BezSnijega(), sadaIzvora, 2, 1)
	if err != nil || errBez != nil || !reflect.DeepEqual(uzivo, bez) {
		t.Errorf("uživo s isključenim snijegom %v, želim %v (%v, %v)", uzivo, bez, err, errBez)
	}
}

// Uživo se topi po primijenjenim postavkama.
func TestUzivoPoPrimijenjenimPostavkama(t *testing.T) {
	postaviSnijeg(t, PostavkeSnijega{Ukljucen: true, Topljenje: 5, Prag: 1})
	got, err := OborineOkoSada(izvorSaSnijegom(), sadaIzvora, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	// zaliha 2 + 24 = 26; drugi dan (3 °C) otopi 5 × 2 = 10, treći (10 °C)
	// ostalih 16 uz 12 mm kiše
	if want := (map[string]DnevniNiz{OborinaKljuc("S"): {-1: 0, 0: 10, 1: 28}}); !reflect.DeepEqual(zaokruzeno(got), want) {
		t.Errorf("%v, želim %v", got, want)
	}
}

// danIzvora i sadaIzvora su dan i sat izvora izvorSaSnijegom.
const (
	danIzvora  = int64(20000)
	sadaIzvora = danIzvora*24 + 23
)

// izvorSaSnijegom je izvor jedne točke: hladan dan sa snijegom, pa topao i
// vruć dan, uz zalihu 2 mm na početku.
func izvorSaSnijegom() *OborinskiIzvor {
	dan := danIzvora
	return &OborinskiIzvor{
		Tocke: []OborinskaTocka{{Code: "a", Sliv: "S", Tezina: 1}},
		Satne: func(od, do int64) (map[string]map[int64]float64, error) {
			return map[string]map[int64]float64{"a": spoji(satiDana(dan-1, 1), satiDana(dan, 0), satiDana(dan+1, 0.5))}, nil
		},
		Snijeg: func(od, do int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			return map[string]map[int64]float64{"a": spoji(satiDana(dan-1, 1), satiDana(dan, 0), satiDana(dan+1, 0))},
				map[string]map[int64]float64{"a": spoji(satiDana(dan-1, -5), satiDana(dan, 3), satiDana(dan+1, 10))}, nil
		},
		ZalihaSnijega: func(string, int64) (float64, error) { return 2, nil },
	}
}

// Zaliha snijega koju živi rad pamti računa se iznova kad se postavke
// promijene.
func TestZalihaSnijegaPratiPostavke(t *testing.T) {
	postaviSnijeg(t, ZadanePostavkeSnijega)
	arhiva, _ := arhivaUDatoteci(t)
	ob := []float64{10, 0, 0}
	te := []float64{-5, 2, -1}
	for _, v := range []struct {
		velicina string
		niz      []float64
	}{{"oborina", ob}, {"snijeg", ob}, {"temperatura-zraka", te}} {
		upisiDnevne(t, arhiva, "a", v.velicina, 3, func(d int64) float64 { return v.niz[d] })
	}
	z := &ZalihaSnijega{Arhiva: arhiva}
	// sezona 1969./70. počinje 1. 8. 1969., pa dan 3 vidi dane 0–2
	if v, err := z.NaPocetku("a", 3); err != nil || v != 4 {
		t.Errorf("faktor 3: zaliha %v, %v; želim 4", v, err)
	}
	postaviSnijeg(t, PostavkeSnijega{Ukljucen: true, Topljenje: 4})
	if v, err := z.NaPocetku("a", 3); err != nil || v != 2 {
		t.Errorf("faktor 4: zaliha %v, %v; želim 2", v, err)
	}
}

// Čitanje postavki i nacrta: baza koja se ne da čitati je greška, baza bez
// tablice i redak koji nije nacrt nisu, a tablica krivog oblika jest.
func TestCitanjePostavkiSnijega(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	if _, err := baza.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES ('postavke:snijeg', 'nije json')`); err != nil {
		t.Fatal(err)
	}
	if _, ima, err := NacrtSnijegaIz(baza); ima || err != nil {
		t.Errorf("nacrt koji nije JSON se preskače: %v, %v", ima, err)
	}
	if _, err := baza.Exec(`DROP TABLE model_postavke; CREATE TABLE model_postavke (kljuc TEXT)`); err != nil {
		t.Fatal(err)
	}
	if z, ima, err := PrimijenjeneSnijega(baza); err == nil || ima || z.Postavke != ZadanePostavkeSnijega {
		t.Errorf("tablica krivog oblika: %+v, %v, %v", z, ima, err)
	}
	if _, err := baza.Exec(`DROP TABLE model_postavke`); err != nil {
		t.Fatal(err)
	}
	if z, ima, err := PrimijenjeneSnijega(baza); err != nil || ima || z.Postavke != ZadanePostavkeSnijega {
		t.Errorf("baza bez tablice: %+v, %v, %v", z, ima, err)
	}
	if z := (ZapisSnijega{}); z.Zadnja() != (PromjenaLetve{}) {
		t.Errorf("bez promjena zadnja je prazna: %+v", z.Zadnja())
	}
	zatvorena := otvoriProbnu(t, "z.db")
	zatvorena.Close()
	if _, _, err := PrimijenjeneSnijega(zatvorena); err == nil {
		t.Error("zatvorena baza bez greške")
	}
	if err := SpremiNacrtSnijega(zatvorena, NacrtSnijega{Postavke: ZadanePostavkeSnijega}); err == nil {
		t.Error("spremanje u zatvorenu bazu bez greške")
	}
	for ime, f := range map[string]func() error{
		"proba":    func() error { _, err := ProbajNacrtSnijega(zatvorena, zatvorena, nil, ""); return err },
		"primjena": func() error { return PrimijeniNacrtSnijega(zatvorena, PromjenaLetve{Razlog: "proba"}) },
	} {
		if err := f(); err == nil {
			t.Errorf("%s na zatvorenoj bazi bez greške", ime)
		}
	}
	if err := upisiNacrtSnijega(baza, NacrtSnijega{Proba: &ProbaSnijega{Letve: []ProbaSnijegaLetve{{Sada: []MjeraDana{{Promasaj: math.NaN()}}}}}}); !porukaIma(err, "nacrt postavki snijega") {
		t.Errorf("nacrt koji se ne da zapisati: %v", err)
	}
}

// Nacrt koji ne prolazi provjeru ne proba se i ne primjenjuje, a ni nacrt uz
// pokvarene primijenjene postavke.
func TestNacrtSnijegaKojiNeValja(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	if err := upisiNacrtSnijega(baza, NacrtSnijega{Postavke: PostavkeSnijega{Topljenje: 20}, Proba: &ProbaSnijega{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajNacrtSnijega(baza, baza, nil, ""); !porukaIma(err, "brzina topljenja") {
		t.Errorf("proba nacrta izvan granica: %v", err)
	}
	if err := PrimijeniNacrtSnijega(baza, PromjenaLetve{Razlog: "proba"}); !porukaIma(err, "brzina topljenja") {
		t.Errorf("primjena nacrta izvan granica: %v", err)
	}
	if err := upisiNacrtSnijega(baza, NacrtSnijega{Postavke: ZadanePostavkeSnijega, Proba: &ProbaSnijega{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO model_postavke (kljuc, sadrzaj) VALUES ('snijeg', 'nije json')`); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajNacrtSnijega(baza, baza, nil, ""); err == nil {
		t.Error("proba uz pokvarene primijenjene postavke bez greške")
	}
}

// Kad se primijenjene ne daju upisati, nacrt ostaje; uz postavke se čuva
// zadnjih dvadeset promjena.
func TestPrimjenaSnijegaUpisIPovijest(t *testing.T) {
	postaviSnijeg(t, ZadanePostavkeSnijega)
	baza := otvoriProbnu(t, "p.db")
	for i := range najvisePromjenaSnijega + 2 {
		p := PostavkeSnijega{Ukljucen: true, Topljenje: float64(1 + i%2)}
		upisiPrimijenjeno(t, baza, p)
	}
	z, _, err := PrimijenjeneSnijega(baza)
	if err != nil || len(z.Promjene) != najvisePromjenaSnijega {
		t.Errorf("čuva se %d promjena, %v", len(z.Promjene), err)
	}
	if _, err := baza.Exec(`CREATE TRIGGER zakljucano BEFORE INSERT ON model_postavke BEGIN SELECT RAISE(ABORT, 'zaključano'); END`); err != nil {
		t.Fatal(err)
	}
	if err := upisiNacrtSnijega(baza, NacrtSnijega{Postavke: ZadanePostavkeSnijega, Proba: &ProbaSnijega{Primijenjene: z.Postavke}}); err != nil {
		t.Fatal(err)
	}
	if err := PrimijeniNacrtSnijega(baza, PromjenaLetve{Razlog: "proba"}); !porukaIma(err, "zaključano") {
		t.Errorf("upis koji ne prolazi: %v", err)
	}
	if _, ima, err := NacrtSnijegaIz(baza); err != nil || !ima {
		t.Error("nacrt mora ostati kad primjena ne uspije")
	}
}

// Ocjena probe gleda samo dane s dovoljno slučajeva i razliku veću od 2 %.
func TestOcjenaProbeSnijega(t *testing.T) {
	dani := func(promasaj float64, slucaja int) []MjeraDana {
		return []MjeraDana{{Dan: 1, Promasaj: promasaj, Slucaja: slucaja}, {Dan: 2, Promasaj: 2 * promasaj, Slucaja: slucaja}}
	}
	for _, s := range []struct {
		sada, nacrt []MjeraDana
		zeli        StanjeMjere
	}{
		{dani(10, 500), dani(9, 500), SnijegBolje},
		{dani(10, 500), dani(11, 500), SnijegLosije},
		{dani(10, 500), dani(10.1, 500), SnijegIsto},
		{dani(10, 50), dani(5, 50), MjeraNijeProvjereno},
		{nil, dani(5, 500), MjeraNijeProvjereno},
	} {
		if got := ocijeniProbuSnijega(s.sada, s.nacrt); got != s.zeli {
			t.Errorf("%v → %v: %s, želim %s", s.sada, s.nacrt, got, s.zeli)
		}
	}
	letve := []ProbaSnijegaLetve{{Letva: "a", Razdoblje: []string{"2024-11-01", "2026-06-30"}}, {Letva: "b", Greska: "nema niza"},
		{Letva: "c", Razdoblje: []string{"2025-01-01", "2026-09-30"}}}
	if got := ukupnoRazdoblje(letve); !reflect.DeepEqual(got, []string{"2024-11-01", "2026-09-30"}) {
		t.Errorf("razdoblje %v", got)
	}
	if got := ukupnoRazdoblje(letve[1:2]); got != nil {
		t.Errorf("bez razdoblja %v", got)
	}
	if got := greskeProbe(letve); got != " (b: nema niza)" {
		t.Errorf("greške %q", got)
	}
	if got := greskeProbe(letve[:1]); got != "" {
		t.Errorf("bez grešaka %q", got)
	}
}

// Letva čiji se snijeg ne da pročitati ili kojoj arhiva ne seže dovoljno
// ostaje u probi s greškom.
func TestProbaSnijegaLetveSGreskom(t *testing.T) {
	c := DnevniCilj{Letva: "donja", Ulazi: []string{"gornja"}, Slivovi: []string{"B"}}
	arhiva := arhivaSnijega(t)
	zamijeniStupac(t, arhiva, "spoj", "vrijednost", pucaNa("velicina = 'snijeg'", "vrijednost"), "letva", "vrijeme")
	if l := probajLetvuSnijega(arhiva, tockeB, c, ZadanePostavkeSnijega, ZadanePostavkeSnijega); l.Greska == "" || l.Ocjena != MjeraNijeProvjereno {
		t.Errorf("snijeg koji se ne da pročitati: %+v", l)
	}
	kratka, _ := arhivaUDatoteci(t)
	upisiDnevne(t, kratka, "gornja", "vodostaj", 300, valIzmisljeni)
	upisiDnevne(t, kratka, "donja", "vodostaj", 300, valIzmisljeni)
	upisiDnevne(t, kratka, "tocka-b", "oborina", 300, kisaIzmisljena)
	if l := probajLetvuSnijega(kratka, tockeB, c, ZadanePostavkeSnijega, ZadanePostavkeSnijega); l.Greska == "" {
		t.Errorf("kratka arhiva: %+v", l)
	}
}
