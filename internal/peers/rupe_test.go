package peers

// Jedinični testovi čiste logike rupa u nizu (rupe.go): rasponi, zapis
// ključa, pravilo čuvanja, zapis s početka razgovora i promjena zapisa.

import (
	"fmt"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// r je raspon (od, do] u tablicama testova
func r(od, do string) raspon { return raspon{Od: od, Do: do} }

func TestRaspon(t *testing.T) {
	prazni := []raspon{{}, r("b", "b"), r("c", "b")}
	for _, p := range prazni {
		if !p.prazan() || p.cist() != (raspon{}) || p.ispravan() {
			t.Errorf("%v: prazan %v, čist %v, ispravan %v", p, p.prazan(), p.cist(), p.ispravan())
		}
	}
	if iznad("b") != r("b", beskraj) || r("a", "b").cist() != r("a", "b") {
		t.Error("iznad ili čist")
	}
	slucajevi := []struct {
		a, b             raspon
		uz, presjek, bez raspon
	}{
		{r("b", "d"), r("c", "f"), r("b", "f"), r("c", "d"), r("b", "c")},
		{r("c", "f"), r("b", "d"), r("b", "f"), r("c", "d"), r("d", "f")},
		{r("b", "f"), r("c", "d"), r("b", "f"), r("c", "d"), r("b", "f")}, // sredina: ostaje cijeli
		{r("c", "d"), r("b", "f"), r("b", "f"), r("c", "d"), raspon{}},
		{r("b", "c"), r("d", "f"), r("b", "f"), raspon{}, r("b", "c")},
		{r("b", "c"), raspon{}, r("b", "c"), raspon{}, r("b", "c")},
		{r("c", "c"), r("b", "d"), r("b", "d"), raspon{}, raspon{}},
		{r("", "d"), r("", "b"), r("", "d"), r("", "b"), r("b", "d")},
	}
	for _, sl := range slucajevi {
		if got := sl.a.uz(sl.b); got != sl.uz {
			t.Errorf("%v uz %v = %v, očekivano %v", sl.a, sl.b, got, sl.uz)
		}
		if got := sl.a.presjek(sl.b); got != sl.presjek {
			t.Errorf("%v presjek %v = %v, očekivano %v", sl.a, sl.b, got, sl.presjek)
		}
		if got := sl.a.bez(sl.b); got != sl.bez {
			t.Errorf("%v bez %v = %v, očekivano %v", sl.a, sl.b, got, sl.bez)
		}
	}
	predugo := string(make([]byte, najduljaOznaka+1))
	for _, sl := range []struct {
		r  raspon
		ok bool
	}{{r("a", "b"), true}, {r(predugo, predugo+"1"), false}, {r("", predugo), false}} {
		if sl.r.ispravan() != sl.ok {
			t.Errorf("ispravan %v: %v", sl.r, !sl.ok)
		}
	}
}

func TestRupaKljuca(t *testing.T) {
	rk := rupaKljuca{odbijeno: r("b", "c"), trazi: r("e", "f")}
	if rk.nejamci() != r("b", "f") || rk.prazna() || (rupaKljuca{}).nejamci() != (raspon{}) || !(rupaKljuca{}).prazna() {
		t.Errorf("nejamci %v, prazna %v", rk.nejamci(), rk.prazna())
	}
	if rk.sirenjem() != (rupaKljuca{trazi: r("b", "f")}) {
		t.Errorf("širenjem: %+v", rk.sirenjem())
	}
	if !veci(rupaKljuca{trazi: r("a", "f")}, rk) || veci(rupaKljuca{trazi: r("c", "d")}, rk) || veci(rk, rk) {
		t.Error("veci")
	}
}

func TestPraviloCuvanja(t *testing.T) {
	rub := time.Date(2025, 10, 15, 0, 0, 0, 0, time.UTC)
	staro := praviloCuvanja{mjeseci: 12, pracene: []string{"a", "c"}, rub: rub}
	slucajevi := []struct {
		ime  string
		sada praviloCuvanja
		sire bool
	}{
		{"isto", staro, false},
		{"bez ograde", praviloCuvanja{pracene: []string{"a", "c"}}, true},
		{"dulje razdoblje", praviloCuvanja{mjeseci: 24, pracene: []string{"a", "c"}, rub: rub}, true},
		{"kraće razdoblje", praviloCuvanja{mjeseci: 6, pracene: []string{"a", "c"}, rub: rub}, false},
		{"nova letva", praviloCuvanja{mjeseci: 12, pracene: []string{"a", "b", "c"}, rub: rub}, true},
		{"maknuta letva", praviloCuvanja{mjeseci: 12, pracene: []string{"a"}, rub: rub}, false},
		{"rub dva dana unatrag", praviloCuvanja{mjeseci: 12, pracene: []string{"a", "c"}, rub: rub.AddDate(0, 0, -2)}, false},
		{"rub četiri dana unatrag", praviloCuvanja{mjeseci: 12, pracene: []string{"a", "c"}, rub: rub.Add(-tolerancijaRuba)}, false},
		{"rub deset dana unatrag", praviloCuvanja{mjeseci: 12, pracene: []string{"a", "c"}, rub: rub.AddDate(0, 0, -10)}, true},
		{"rub naprijed", praviloCuvanja{mjeseci: 12, pracene: []string{"a", "c"}, rub: rub.AddDate(0, 1, 0)}, false},
	}
	for _, sl := range slucajevi {
		if got := siriOd(staro, sl.sada); got != sl.sire {
			t.Errorf("%s: širi %v", sl.ime, got)
		}
	}
	if siriOd(praviloCuvanja{}, praviloCuvanja{mjeseci: 12, rub: rub}) {
		t.Error("bez ograde na ogradu nije širenje")
	}
	// rub se sprema najveći dosad, a nakon proširenja sadašnji
	ranije := praviloCuvanja{mjeseci: 12, rub: rub.AddDate(0, 0, -1)}
	if ranije.zapis(staro, false).rub != rub || ranije.zapis(staro, true).rub != ranije.rub || staro.zapis(ranije, false).rub != rub {
		t.Error("rub u zapisu pravila")
	}
}

func TestKanalOcitanja(t *testing.T) {
	for kanal, ocitanja := range map[string]bool{"": true, "ocitanja/16/2024": true, "dnevnici/16/2024": false, "prijave/3/2026": false, "ocitanjax/1/1": false} {
		if kanalOcitanja(kanal) != ocitanja {
			t.Errorf("%q: %v", kanal, !ocitanja)
		}
	}
}

func TestPocetakRupaJamstvo(t *testing.T) {
	tudji, vlastiti, dnevnik := "ured|ocitanja/16/2024", "laptop|ocitanja/16/2024", "ured|dnevnici/16/2024"
	p := pocetakRupa{ja: "laptop", poznato: true, kljucevi: map[string]rupaKljuca{
		tudji:    {odbijeno: r("b", "c"), trazi: r("e", "f")},
		vlastiti: {trazi: r("a", "z")},
		dnevnik:  {trazi: r("a", "b")},
	}}
	if !p.vlastiti(vlastiti) || p.vlastiti(tudji) || p.vlastiti("laptop-drugi|ocitanja/16/2024") {
		t.Error("vlastiti")
	}
	if p.nejamci(tudji, raspon{}) != r("b", "f") || p.nejamci(vlastiti, iznad("")) != (raspon{}) || p.nejamci(tudji, iznad("x")) != r("b", beskraj) {
		t.Errorf("nejamci: %v %v", p.nejamci(tudji, raspon{}), p.nejamci(tudji, iznad("x")))
	}
	for od, jamci := range map[string]bool{"a": true, "b": false, "d": false, "f": true, "g": true} {
		if p.jamciIza(tudji, od) != jamci {
			t.Errorf("jamči iza %q: %v", od, !jamci)
		}
	}
	nepoznato := p
	nepoznato.poznato = false
	if nepoznato.nejamci(tudji, raspon{}) != iznad("") || nepoznato.nejamci(vlastiti, raspon{}) != (raspon{}) || nepoznato.jamciIza(tudji, "z") {
		t.Error("nepročitan zapis jamči")
	}
	virtualno := p
	virtualno.virtualno = true
	if virtualno.nejamci(tudji, raspon{}) != iznad("") || virtualno.nejamci(dnevnik, raspon{}) != r("a", "b") || virtualno.nejamci(vlastiti, raspon{}) != (raspon{}) {
		t.Error("virtualni prvi korak")
	}
}

func TestPocetakRupaTrazim(t *testing.T) {
	p := pocetakRupa{ja: "laptop", poznato: true, kljucevi: map[string]rupaKljuca{
		"ured|ocitanja/16/2024":   {trazi: r("a", "b")},
		"ured|dnevnici/16/2024":   {trazi: r("a", "b")},
		"ured|ocitanja/3/2024":    {odbijeno: r("a", "b")},
		"laptop|ocitanja/16/2024": {trazi: r("a", "b")},
	}}
	if got := p.trazim(nil); len(got) != 2 || got["ured|ocitanja/16/2024"] != r("a", "b") || got["ured|dnevnici/16/2024"] != r("a", "b") {
		t.Errorf("sve: %v", got)
	}
	samoOcitanja := func(kanal string) bool { return kanalOcitanja(kanal) }
	if got := p.trazim(samoOcitanja); len(got) != 1 {
		t.Errorf("samo praćeni: %v", got)
	}
	if got := (pocetakRupa{}).trazim(nil); got == nil || len(got) != 0 {
		t.Errorf("prazan zapis: %#v", got)
	}
	puno := pocetakRupa{ja: "laptop", kljucevi: map[string]rupaKljuca{}}
	for i := range najviseTrazenih + 5 {
		puno.kljucevi[ledger.FrontierKey("ured", fmt.Sprintf("ocitanja/%d/2024", i))] = rupaKljuca{trazi: r("a", "b")}
	}
	if got := puno.trazim(nil); len(got) != najviseTrazenih {
		t.Errorf("ograda traženih: %d", len(got))
	}
}

func TestPocetakRupaZaIzvoz(t *testing.T) {
	p := pocetakRupa{ja: "laptop", broj: 3, poznato: true, kljucevi: map[string]rupaKljuca{
		"ured|ocitanja/16/2024": {odbijeno: r("b", "e")},
	}}
	verzije := []ledger.Version{
		{VersionID: "c", NodeID: "ured", Channel: "ocitanja/16/2024"},
		{VersionID: "d", NodeID: "ured", Channel: "ocitanja/16/2024"},
		{VersionID: "z", NodeID: "ured", Channel: "ocitanja/3/2024"},
		{VersionID: "y", NodeID: "laptop", Channel: "ocitanja/16/2024"},
	}
	if got := p.zaIzvoz(3, verzije); len(got) != 1 || got["ured|ocitanja/16/2024"] != r("b", "d") {
		t.Errorf("bez sumnje: %v", got)
	}
	// brojač se promijenio: tuđe ne jamči do najveće izvezene, vlastito jamči
	if got := p.zaIzvoz(4, verzije); len(got) != 2 || got["ured|ocitanja/16/2024"] != r("", "d") || got["ured|ocitanja/3/2024"] != r("", "z") {
		t.Errorf("uz sumnju: %v", got)
	}
}

func TestPromjenaKljuca(t *testing.T) {
	pk := promjenaKljuca{odbijeno: r("b", "c"), uTrazi: r("x", "y"), nesigurno: r("d", "m"), pokriveno: []raspon{r("", "e"), r("k", "n")}, fs: "j"}
	if pk.nedostaje(r("c", "f")) != r("d", "m") || pk.nedostaje(raspon{}) != r("j", "m") {
		t.Errorf("nedostaje: %v %v", pk.nedostaje(r("c", "f")), pk.nedostaje(raspon{}))
	}
	// prije upisa: odbijeno i ono što nedostaje dodano
	if got := pk.prosiri(rupaKljuca{odbijeno: r("a", "b"), trazi: r("e", "f")}); got != (rupaKljuca{odbijeno: r("a", "c"), trazi: r("e", "y")}) {
		t.Errorf("prosiri: %+v", got)
	}
	// nakon upisa: pokriveno maknuto, a nesigurno i traženo odbijeno ostaju
	samoPokriveno := promjenaKljuca{pokriveno: []raspon{r("", "c"), r("e", "z")}, fs: "z"}
	if got := samoPokriveno.suzi(rupaKljuca{odbijeno: r("a", "b"), trazi: r("b", "f")}); got != (rupaKljuca{odbijeno: r("a", "b"), trazi: r("c", "e")}) {
		t.Errorf("suzi: %+v", got)
	}
	if got := pk.suzi(rupaKljuca{trazi: r("e", "f")}); got != (rupaKljuca{trazi: r("e", "y")}) {
		t.Errorf("suzi uz nesigurno: %+v", got)
	}
	if !pk.mijenjaTrazi(raspon{}) || !(promjenaKljuca{pokriveno: []raspon{r("a", "c")}, fs: "z"}).mijenjaTrazi(r("b", "d")) {
		t.Error("mijenjaTrazi ne vidi promjenu")
	}
	if (promjenaKljuca{pokriveno: []raspon{r("a", "c")}, fs: "z"}).mijenjaTrazi(r("d", "e")) || (promjenaKljuca{}).mijenjaTrazi(raspon{}) {
		t.Error("mijenjaTrazi vidi promjenu koje nema")
	}
}

func TestRasponOdbijenog(t *testing.T) {
	slucajevi := []struct {
		ime       string
		b         baceno
		fs, ispod string
		odbijeno  raspon
	}{
		{"ništa", baceno{}, "c", "", raspon{}},
		{"iznad granice", baceno{najmanja: "e", najveca: "g"}, "c", "", r("c", "g")},
		{"iznad primljenoga", baceno{najmanja: "e", najveca: "g"}, "c", "d", r("d", "g")},
		{"ispod granice", baceno{najmanja: "b", najveca: "g"}, "c", "a", r("a", "g")},
		{"na granici", baceno{najmanja: "c", najveca: "c"}, "c", "", r("", "c")},
	}
	for _, sl := range slucajevi {
		if got := rasponOdbijenog(sl.b, sl.fs, sl.ispod); got != sl.odbijeno {
			t.Errorf("%s: %v", sl.ime, got)
		}
	}
}

func TestPromjenaRupa(t *testing.T) {
	v := []ledger.Version{{VersionID: "a"}}
	prazna := promjenaRupa{}
	if !prazna.nista(nil) || prazna.nista(v) || prazna.ima() {
		t.Error("prazna promjena")
	}
	if (promjenaRupa{pravilo: true}).nista(nil) {
		t.Error("pravilo treba zapisati, a promjena je ništa")
	}
	pokriva := promjenaRupa{kljucevi: map[string]promjenaKljuca{"k": {pokriveno: []raspon{r("a", "c")}, fs: "z"}}}
	if !pokriva.nista(nil) || pokriva.ima() {
		t.Error("pokriveno bez traženoga")
	}
	pokriva.prije = map[string]rupaKljuca{"k": {trazi: r("b", "d")}}
	if pokriva.nista(nil) || !pokriva.ima() {
		t.Error("pokriveno siječe traženo")
	}
	// samo odbijeno ili traženo odbijeno: ne treba bravu, ali treba zapis
	for _, pk := range []promjenaKljuca{{odbijeno: r("a", "b")}, {uTrazi: r("a", "b")}} {
		p := promjenaRupa{kljucevi: map[string]promjenaKljuca{"k": pk}}
		if !p.nista(nil) || !p.ima() {
			t.Errorf("%+v: ništa %v, ima %v", pk, p.nista(nil), p.ima())
		}
	}
}

func TestUzBaceno(t *testing.T) {
	m := uzBaceno(nil, "k", "c")
	m = uzBaceno(m, "k", "a")
	m = uzBaceno(m, "k", "e")
	m = uzBaceno(m, "j", "x")
	if m["k"] != (baceno{najmanja: "a", najveca: "e"}) || m["j"] != (baceno{najmanja: "x", najveca: "x"}) {
		t.Errorf("bačeno: %+v", m)
	}
}
