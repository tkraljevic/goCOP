package peers

// Jedinični testovi rupa u razgovoru (rupe_razgovor.go): što se od tražene
// rupe popunjava, što pošiljatelj uz deltu javi i što primatelj iz delte
// zapiše. Razgovor se slaže izravno, bez veze.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// v je verzija ključa autor|kanal s oznakom id
func v(autor, kanal, id string) ledger.Version {
	return ledger.Version{VersionID: id, Entity: "probni", EntityID: "z-" + id, NodeID: autor, Channel: kanal}
}

func TestValjanaTrazenja(t *testing.T) {
	predug := fmt.Sprintf("%0*d", najduljiKljuc+1, 0)
	got := valjanaTrazenja(map[string]raspon{"ured": r("", "b"), predug: r("", "b"), "prazno": r("b", "b")})
	if len(got) != 1 || got["ured"] != r("", "b") {
		t.Errorf("%v", got)
	}
	if got := valjanaTrazenja(nil); got == nil || len(got) != 0 {
		t.Errorf("bez traženja: %#v", got)
	}
}

func TestPlanPopune(t *testing.T) {
	procitano := []ledger.Version{{VersionID: "b"}, {VersionID: "c"}, {VersionID: "e"}}
	if p, vv := planPopune("a", "d", procitano); p != (popunaKljuca{od: "a", granica: "d", procitano: "d", broj: 2}) || len(vv) != 2 {
		t.Errorf("do granice: %+v %d", p, len(vv))
	}
	if p, vv := planPopune("a", "d", procitano[2:]); p != (popunaKljuca{od: "a", granica: "d", procitano: "d"}) || len(vv) != 0 {
		t.Errorf("prazna popuna, pročitano iza granice: %+v %d", p, len(vv))
	}
	if p, _ := planPopune("a", "c", procitano[:2]); p != (popunaKljuca{od: "a", granica: "c", procitano: "c", broj: 2}) {
		t.Errorf("pročitano točno do granice: %+v", p)
	}
	// knjiga je od početka razgovora izgubila verzije (brisanje kanala):
	// jamči se samo do zadnje pročitane, a bez ijedne ništa
	if p, vv := planPopune("a", "d", nil); p != (popunaKljuca{od: "a", granica: "d", procitano: "a"}) || len(vv) != 0 {
		t.Errorf("ništa pročitano: %+v %d", p, len(vv))
	}
	if p, vv := planPopune("a", "d", procitano[:1]); p != (popunaKljuca{od: "a", granica: "d", procitano: "b", broj: 1}) || len(vv) != 1 {
		t.Errorf("pročitano manje od granice: %+v %d", p, len(vv))
	}
	puno := make([]ledger.Version, NajviseVerzijaPoRazmjeni)
	for i := range puno {
		puno[i].VersionID = fmt.Sprintf("b%05d", i)
	}
	if p, vv := planPopune("a", "z", puno); p.procitano != puno[len(puno)-1].VersionID || p.broj != len(puno) || len(vv) != len(puno) {
		t.Errorf("puna ograda: %+v", p)
	}
	if p, _ := planPopune("a", puno[len(puno)-1].VersionID, puno); p.procitano != puno[len(puno)-1].VersionID {
		t.Errorf("puna ograda do granice: %+v", p)
	}
}

// razgovorZaRupe je razgovor čvora laptop s granicom moja i zapisom rupa
func razgovorZaRupe(t *testing.T, moja map[string]string, rupe map[string]rupaKljuca) *razgovorSCvorom {
	t.Helper()
	s := rubniCvor(t, "laptop")
	return &razgovorSCvorom{s: s, moja: frontierMsg{Frontier: moja}, theirs: frontierMsg{Wants: &Wants{All: true}},
		rupe: pocetakRupa{ja: "laptop", poznato: true, dogovor: true, kljucevi: rupe}}
}

func TestPopunaZa(t *testing.T) {
	k, dnevnik := "ured|ocitanja/16/2024", "ured|dnevnici/16/2024"
	rz := razgovorZaRupe(t, map[string]string{k: "f", dnevnik: "f", "ured|ocitanja/3/2024": "f", "ured|ocitanja/4/2024": "f"},
		map[string]rupaKljuca{"ured|ocitanja/3/2024": {odbijeno: r("a", "c")}})
	rz.theirs.Wants = &Wants{Rules: []Subscription{{Kind: ledger.ChannelReadings}}}
	rz.njihovaTrazenja = map[string]raspon{k: r("a", "z"), dnevnik: r("a", "z"), "ured|ocitanja/3/2024": r("b", "z"), "ured|ocitanja/4/2024": r("a", "z")}
	g := frontierMsg{Frontier: map[string]string{k: "e", dnevnik: "e", "ured|ocitanja/3/2024": "e", "ured|ocitanja/4/2024": "e"}}
	citaj := func(_ context.Context, autor, kanal, od string, limit int) ([]ledger.Version, error) {
		if kanal == "ocitanja/4/2024" {
			return nil, errors.New("namjerni kvar")
		}
		return []ledger.Version{v(autor, kanal, "b"), v(autor, kanal, "d"), v(autor, kanal, "f")}, nil
	}
	got := rz.popunaZa(ctxRub, g, citaj)
	// samo praćen kanal, gdje čvor jamči i koji se dao pročitati: do tuđe granice e
	if len(got) != 2 || got[0].VersionID != "b" || got[1].VersionID != "d" || len(rz.popunjavam.kljucevi) != 1 ||
		rz.popunjavam.kljucevi[k] != (popunaKljuca{od: "a", granica: "e", procitano: "e", broj: 2}) || rz.popunjavam.granica["ured|ocitanja/3/2024"] != "e" {
		t.Errorf("popuna: %v, plan %+v", got, rz.popunjavam)
	}
}

func TestGranicaPopune(t *testing.T) {
	k := "ured|ocitanja/16/2024"
	rz := razgovorZaRupe(t, map[string]string{k: "f"}, map[string]rupaKljuca{k: {trazi: r("c", "d")}})
	slucajevi := []struct {
		t       raspon
		g       string
		granica string
		ok      bool
	}{
		{r("a", "z"), "e", "e", true},
		{r("a", "b"), "e", "b", true},
		{r("a", "z"), "", "", false},
		{r("e", "z"), "e", "e", false},
		{r("c", "z"), "e", "e", false}, // čvor tu ne jamči
	}
	for _, sl := range slucajevi {
		granica, ok := rz.granicaPopune(k, sl.t, map[string]string{k: sl.g}, nil)
		if granica != sl.granica || ok != sl.ok {
			t.Errorf("%v uz %q: %q %v", sl.t, sl.g, granica, ok)
		}
	}
	if _, ok := rz.granicaPopune(k, r("a", "z"), map[string]string{k: "e"}, func(string) bool { return false }); ok {
		t.Error("popuna kanala koji druga strana ne prati")
	}
}

// Jamstvo uz deltu: Rupe su dio omotača poslanoga za koji čvor ne jamči,
// Popuna dokle je koja tuđa rupa popunjena; uz sumnju i sve iznad vlastite
// granice s početka razgovora
func TestJamstvoDelte(t *testing.T) {
	k, m, vl := "ured|ocitanja/16/2024", "ured|ocitanja/3/2024", "laptop|ocitanja/16/2024"
	rz := razgovorZaRupe(t, map[string]string{k: "m", m: "m", vl: "m"}, map[string]rupaKljuca{k: {odbijeno: r("c", "h")}, vl: {trazi: r("a", "z")}})
	rz.popunjavam = popunaDelte{granica: map[string]string{k: "g", m: "g", vl: "a"}, kljucevi: map[string]popunaKljuca{
		k: {od: "b", granica: "g", procitano: "g", broj: 2},
		m: {od: "b", granica: "g", procitano: "g"},
	}}
	poslano := []ledger.Version{v("ured", "ocitanja/16/2024", "i"), v("laptop", "ocitanja/16/2024", "b"), v("ured", "ocitanja/16/2024", "d")}
	neispravne := []ledger.Version{v("ured", "ocitanja/16/2024", "e")}
	rupe, popuna := rz.jamstvoDelte(poslano, neispravne)
	if fmt.Sprint(popuna) != fmt.Sprint(map[string]raspon{k: r("b", "g"), m: r("b", "g")}) || fmt.Sprint(rupe) != fmt.Sprint(map[string]raspon{k: r("c", "h")}) {
		t.Errorf("bez sumnje: rupe %v, popuna %v", rupe, popuna)
	}
	// samo dio popune obrađen: popuna do zadnje obrađene
	rupe, popuna = rz.jamstvoDelte(poslano[:2], nil)
	if fmt.Sprint(popuna) != fmt.Sprint(map[string]raspon{m: r("b", "g")}) || fmt.Sprint(rupe) != fmt.Sprint(map[string]raspon{k: r("g", "h")}) {
		t.Errorf("bez obrađene popune: rupe %v, popuna %v", rupe, popuna)
	}
	rupe, popuna = rz.jamstvoDelte(poslano, nil)
	if popuna[k] != r("b", "d") || rupe[k] != r("c", "h") {
		t.Errorf("djelomična popuna: rupe %v, popuna %v", rupe, popuna)
	}
	// neispravne su iza poslanih: zadnja obrađena popuna je najveća, ne zadnja po redu
	rz.popunjavam.kljucevi[k] = popunaKljuca{od: "b", granica: "g", procitano: "g", broj: 3}
	if _, popuna = rz.jamstvoDelte(poslano[2:], neispravne); popuna[k] != r("b", "e") {
		t.Errorf("neispravna u popuni: %v", popuna)
	}
	if _, popuna = rz.jamstvoDelte([]ledger.Version{v("ured", "ocitanja/16/2024", "e")}, []ledger.Version{v("ured", "ocitanja/16/2024", "d")}); popuna[k] != r("b", "e") {
		t.Errorf("neispravna ispred poslane: %v", popuna)
	}
	// ključ kojemu se ništa nije dalo pročitati (procitano = od) ne javlja popunu
	rz.popunjavam.kljucevi[m] = popunaKljuca{od: "b", granica: "g", procitano: "b"}
	if _, popuna = rz.jamstvoDelte(poslano, neispravne); fmt.Sprint(popuna) != fmt.Sprint(map[string]raspon{k: r("b", "e")}) {
		t.Errorf("popuna bez pročitanoga: %v", popuna)
	}
	rz.popunjavam.kljucevi[m] = popunaKljuca{od: "b", granica: "g", procitano: "g"}
	rz.popunjavam.kljucevi[k] = popunaKljuca{od: "b", granica: "g", procitano: "g", broj: 2}
	rz.s.rupeMijenja.Add(1)
	if rupe, _ = rz.jamstvoDelte(poslano, neispravne); fmt.Sprint(rupe) != fmt.Sprint(map[string]raspon{k: r("c", "i")}) {
		t.Errorf("uz sumnju: %v", rupe)
	}
	rz.rupe.dogovor = false
	if rupe, popuna = rz.jamstvoDelte(poslano, neispravne); rupe != nil || popuna != nil {
		t.Errorf("bez oznake: %v %v", rupe, popuna)
	}
}

func TestSkupKljucevaIBaceno(t *testing.T) {
	if got := skupKljuceva(maps.Keys(map[string]int{"b": 1, "a": 2}), maps.Keys(map[string]bool{"c": true, "a": true})); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("skup: %v", got)
	}
	if got := skupKljuceva(); len(got) != 0 {
		t.Errorf("prazan skup: %v", got)
	}
	var rz razgovorSCvorom
	for _, id := range []string{"d", "b", "f", "c"} {
		rz.zapamtiBaceno("k", id)
	}
	rz.zapamtiBaceno("m", "x")
	if rz.bacio["k"] != (baceno{najmanja: "b", najveca: "f"}) || rz.bacio["m"] != (baceno{najmanja: "x", najveca: "x"}) {
		t.Errorf("bačeno: %+v", rz.bacio)
	}
}

// Primatelj: odbijeno, preskočeno po granici odbijenih, popuštena ograda,
// nesigurno i pokriveno, bez vlastitog ključa i preskočenih ključeva
func TestPromjenaRupaPrimatelja(t *testing.T) {
	// oznake ispravnog oblika, redom slova; neispravnu odbaci ne pamti
	o := func(slovo string) string { return fmt.Sprintf("0190a000-0000-7000-8000-%012d", slovo[0]-'a') }
	k, g4, vl, pr := "ured|ocitanja/16/2024", "ured|ocitanja/3/2024", "laptop|ocitanja/16/2024", "ured|ocitanja/5/2024"
	rz := razgovorZaRupe(t, map[string]string{k: o("c"), g4: o("c"), vl: o("c"), pr: o("c")}, map[string]rupaKljuca{k: {trazi: r(o("a"), o("b"))}})
	rz.rupe.zapisati = true
	rz.odbijene.moje = map[string]string{g4: o("f")}
	rz.preskoceni = map[string]string{pr: o("f")}
	rz.trazeno = map[string]raspon{k: r(o("a"), o("b"))}
	rz.njihovaPopuna = map[string]raspon{k: r(o("a"), o("b")), "ured|ocitanja/9/2024": r(o("a"), o("b"))}
	rz.njihoveRupe = map[string]raspon{k: r(o("d"), o("e"))}
	for _, id := range []string{o("f"), o("h")} {
		rz.odbaci(v("ured", "ocitanja/16/2024", id))
	}
	rz.odbaci(ledger.Version{VersionID: "x"}) // neispravna se ne pamti
	wanted := []ledger.Version{v("ured", "ocitanja/16/2024", o("d")), v("ured", "ocitanja/16/2024", o("g")), v("ured", "ocitanja/3/2024", o("g")),
		v("laptop", "ocitanja/16/2024", o("d")), v("ured", "ocitanja/5/2024", o("g"))}
	p := rz.promjenaRupa(wanted)
	if !p.pravilo || p.prije[k].trazi != r(o("a"), o("b")) || len(p.kljucevi) != 3 {
		t.Fatalf("promjena: %+v", p)
	}
	ocekivano := map[string]promjenaKljuca{
		k:                      {odbijeno: r(o("d"), o("h")), nesigurno: r(o("d"), o("e")), pokriveno: []raspon{r(o("c"), o("h")), r(o("a"), o("b"))}, fs: o("c")},
		g4:                     {odbijeno: r(o("c"), o("f")), pokriveno: []raspon{r(o("f"), o("g")), {}}, fs: o("c")},
		"ured|ocitanja/9/2024": {pokriveno: []raspon{{}, {}}},
	}
	if fmt.Sprint(p.kljucevi) != fmt.Sprint(ocekivano) {
		t.Errorf("ključevi:\n%+v\nočekivano\n%+v", p.kljucevi, ocekivano)
	}
	// ograda s početka razgovora ide uz promjenu, a popuštanje se gleda tek
	// pod bravom (podOgradom), pa promjenaRupa ograde ne čita
	rz.s.PostaviStanjePrihvata(func() (uint64, time.Time) { t.Error("ograda pročitana prije brave"); return 1, time.Time{} })
	rz.odbijene.ograda = stanjeOgrade{narastaj: 7}
	if p := rz.promjenaRupa(wanted); p.ograda != rz.odbijene.ograda || p.kljucevi[k].odbijeno != r(o("d"), o("h")) || !p.kljucevi[k].uTrazi.prazan() {
		t.Errorf("ograda uz promjenu: %+v", p)
	}
}

func TestNesigurnoIPokriveno(t *testing.T) {
	k := "ured|ocitanja/16/2024"
	rz := razgovorZaRupe(t, map[string]string{k: "c"}, nil)
	rz.trazeno = map[string]raspon{k: r("", "c")}
	rz.njihovaPopuna = map[string]raspon{k: r("", "b")}
	slucajevi := []struct {
		rupa  *raspon
		m     string
		nesig raspon
	}{
		{nil, "f", raspon{}},
		{&raspon{Od: "d", Do: "e"}, "f", r("d", "e")},
		{&raspon{Od: "d", Do: beskraj}, "f", r("d", "f")},
		{&raspon{Od: "e", Do: "e"}, "f", r("", "f")},
		{&raspon{Od: "e", Do: "e"}, "", r("", "b")},
	}
	for _, sl := range slucajevi {
		rz.njihoveRupe = map[string]raspon{}
		if sl.rupa != nil {
			rz.njihoveRupe[k] = *sl.rupa
		}
		if got := rz.nesigurnoKljuca(k, sl.m); got != sl.nesig {
			t.Errorf("rupa %v do %q: %v", sl.rupa, sl.m, got)
		}
	}
	if got := rz.pokrivenoKljuca(k, "f"); fmt.Sprint(got) != fmt.Sprint([]raspon{r("c", "f"), r("", "b")}) {
		t.Errorf("pokriveno: %v", got)
	}
	popune := []struct {
		popuna raspon
		ok     bool
	}{{r("", "b"), true}, {r("", "c"), true}, {r("", "d"), false}, {r("a", "b"), false}, {r("", ""), false}}
	for _, sl := range popune {
		rz.njihovaPopuna[k] = sl.popuna
		want := raspon{}
		if sl.ok {
			want = sl.popuna
		}
		if got := rz.valjanaPopuna(k); got != want {
			t.Errorf("popuna %v: %v", sl.popuna, got)
		}
	}
	if rz.valjanaPopuna("netražen") != (raspon{}) {
		t.Error("popuna netraženog ključa")
	}
	rz.rupe.dogovor = false
	if rz.nesigurnoKljuca(k, "f") != (raspon{}) || rz.pokrivenoKljuca(k, "f") != nil {
		t.Error("bez oznake vjeruje se delti")
	}
}

func TestIspodOdbijenih(t *testing.T) {
	bacio := map[string]baceno{"ured|ocitanja/16/2024": {najmanja: "d", najveca: "f"}}
	wanted := []ledger.Version{v("ured", "ocitanja/16/2024", "b"), v("ured", "ocitanja/16/2024", "c"), v("ured", "ocitanja/16/2024", "e"), v("ured", "ocitanja/3/2024", "a")}
	if got := ispodOdbijenih(wanted, bacio); len(got) != 1 || got["ured|ocitanja/16/2024"] != "c" {
		t.Errorf("%v", got)
	}
}

// Upis prođe, a smanjenje zapisa rupa ne: verzije su u knjizi, rupa ostane
// veća (traži se još jednom), a greška ide samo u dnevnik
func TestSuziNakonUpisaGreska(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	trazi := r("", rupaNovo1.VersionID)
	upisiRupuTrazi(t, s, kljucRupe, trazi)
	kvarUpisa(t, s, "DELETE", "rupe_knjige", "")
	dnevnik := brojiDnevnik(t, "zapis rupa nije smanjen")
	p := promjenaRupa{kljucevi: map[string]promjenaKljuca{kljucRupe: {pokriveno: []raspon{trazi}, fs: rupaNovo1.VersionID}},
		prije: map[string]rupaKljuca{kljucRupe: {trazi: trazi}}}
	if n, err := s.primijeniAkoStigne(ctxRub, []ledger.Version{rupaNovo1}, p); n != 1 || err != nil {
		t.Fatalf("upis: %d %v", n, err)
	}
	if n, _ := dnevnik.broj(); n != 1 || rupeCvora(t, s)[kljucRupe].trazi != trazi {
		t.Errorf("dnevnik %d, rupe %+v", n, rupeCvora(t, s))
	}
}
