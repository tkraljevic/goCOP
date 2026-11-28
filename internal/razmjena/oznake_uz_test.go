package razmjena

import (
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// staroOcisceneOznake i staroNepoznateOznake su doslovne kopije onoga
// što je bilo prije nego što su poznate oznake postale argument (N04).
// Ne mijenjaju se: po njima se vidi da omotači i funkcije „Uz” s
// NaseMogucnosti daju isto što i prije.
func staroOcisceneOznake(oznake []string) []string {
	sve := ispravne(oznake)
	var out []string
	for _, oznaka := range NaseMogucnosti() {
		if sve[oznaka] {
			out = append(out, oznaka)
		}
	}
	for _, oznaka := range slices.Sorted(maps.Keys(sve)) {
		if len(out) < najviseOznaka && !slices.Contains(out, oznaka) {
			out = append(out, oznaka)
		}
	}
	slices.Sort(out)
	return out
}

func staroNepoznateOznake(njihove []string) []string {
	znam := map[string]bool{}
	for _, o := range append(NaseMogucnosti(), OznakaWeb) {
		ime, _, _ := procitajOznaku(o)
		znam[ime] = true
	}
	var out []string
	for _, o := range staroOcisceneOznake(njihove) {
		if ime, _, _ := procitajOznaku(o); !znam[ime] {
			out = append(out, o)
		}
	}
	return out
}

// popisiOznaka su ulazi za usporedbu starog i novog: prazni, s
// neispravnima i ponavljanjem, i dulji od ograde (40 i 1000 oznaka), sa i
// bez naših oznaka i web/1, uz slučajne popise iz istog bazena
func popisiOznaka() [][]string {
	var bazen []string
	for i := range 45 {
		bazen = append(bazen, fmt.Sprintf("oznaka-%02d/1", i))
	}
	bazen = append(bazen, NaseMogucnosti()...)
	bazen = append(bazen, OznakaWeb, "web/2", "razmjena/2", "rupe/3", "probni-korak/1", "zzz/1",
		"losa oznaka", "", "razmjena/01", "Razmjena/1", strings.Repeat("a", 33)+"/1")

	cetrdeset := func(dodaj ...string) []string {
		var out []string
		for i := range 40 {
			out = append(out, fmt.Sprintf("oznaka-%02d/1", i))
		}
		return append(out, dodaj...)
	}
	var tisucu []string
	for i := range 1000 {
		tisucu = append(tisucu, fmt.Sprintf("oznaka-%04d/1", i))
	}
	popisi := [][]string{
		nil,
		{},
		{"losa oznaka"},
		NaseMogucnosti(),
		append(NaseMogucnosti(), NaseMogucnosti()...),
		{OznakaRazmjena, OznakaRupe, "web/1", "web/2", "stranice/2", "losa oznaka", "razmjena/2"},
		cetrdeset(),
		cetrdeset(NaseMogucnosti()...),
		cetrdeset(OznakaWeb, OznakaDijeloviKarte),
		cetrdeset(OznakaWeb, "zzz/1", "web/2"),
		append(tisucu, OznakaRazmjena),
	}
	r := rand.New(rand.NewPCG(4, 4))
	for range 300 {
		var p []string
		for range r.IntN(len(bazen) + 10) {
			p = append(p, bazen[r.IntN(len(bazen))])
		}
		popisi = append(popisi, p)
	}
	return popisi
}

// Omotači i funkcije „Uz” s NaseMogucnosti daju bajt po bajt isto što i
// kod prije N04, i uz više od 32 oznake: spremljeno znanje
// (peer_sync.mogucnosti) i Zapis odbijenog ostaju isti.
func TestOznakeUzIstoKaoPrije(t *testing.T) {
	uzWeb := append(NaseMogucnosti(), OznakaWeb)
	for _, p := range popisiOznaka() {
		staro := staroOcisceneOznake(p)
		for ime, got := range map[string][]string{
			"Ocisceno":                       Znanje{Mogucnosti: p}.Ocisceno().Mogucnosti,
			"OciscenoUz(NaseMogucnosti)":     Znanje{Mogucnosti: p}.OciscenoUz(NaseMogucnosti()).Mogucnosti,
			"ocisceneOznake(NaseMogucnosti)": ocisceneOznake(p, NaseMogucnosti()),
			// web/1 u prednosti ne dobiva prednost u ogradi, pa ne mijenja ništa
			"OciscenoUz(NaseMogucnosti uz web/1)":     Znanje{Mogucnosti: p}.OciscenoUz(uzWeb).Mogucnosti,
			"ocisceneOznake(NaseMogucnosti uz web/1)": ocisceneOznake(p, uzWeb),
		} {
			if !reflect.DeepEqual(got, staro) {
				t.Fatalf("%s(%q) = %q, prije %q", ime, p, got, staro)
			}
		}
		staro = staroNepoznateOznake(p)
		if got := NepoznateOznake(p); !reflect.DeepEqual(got, staro) {
			t.Fatalf("NepoznateOznake(%q) = %q, prije %q", p, got, staro)
		}
		if got := NepoznateOznakeUz(p, NaseMogucnosti()); !reflect.DeepEqual(got, staro) {
			t.Fatalf("NepoznateOznakeUz(%q, NaseMogucnosti) = %q, prije %q", p, got, staro)
		}
		// web/1 u znam ne dobiva prednost u ogradi, pa ne mijenja ništa
		if got := NepoznateOznakeUz(p, uzWeb); !reflect.DeepEqual(got, staro) {
			t.Fatalf("NepoznateOznakeUz(%q, NaseMogucnosti uz web/1) = %q, prije %q", p, got, staro)
		}
	}
	// prednost ne mijenja ostala polja znanja: ona se čiste kao i bez nje
	z := Znanje{Mogucnosti: []string{"rupe/1"}, Vrste: map[string]int{"akt": 2, "Losa": 1}, CekaNadogradnju: -3,
		Uloge: []string{"preuzima", "nepoznata"}, Karta: &ZnanjeKarte{Otisak: strings.Repeat("a", 64), Slozena: true, Dijelova: 9, Svih: 5}}
	want := Znanje{Mogucnosti: []string{"rupe/1"}, Vrste: map[string]int{"akt": 2}, CekaNadogradnju: 0,
		Uloge: []string{"preuzima"}, Karta: &ZnanjeKarte{Otisak: strings.Repeat("a", 64), Slozena: true, Dijelova: 5, Svih: 5}}
	for _, prednost := range [][]string{nil, NaseMogucnosti(), {"rupe/1", "probni-korak/1"}} {
		if got := z.OciscenoUz(prednost); !reflect.DeepEqual(got, want) {
			t.Errorf("OciscenoUz(%q) = %+v, želim %+v", prednost, got, want)
		}
	}
}

// Oznaka koju prijavi aplikacija dobiva u ogradi od 32 istu prednost kao
// oznake biblioteke, pa uz 40 tuđih oznaka ostaje spremljena i ne javlja
// se kao nepoznata, a nepoznate su točno spremljene oznake bez poznatih
func TestOznakeUzPrijavljenu(t *testing.T) {
	var njihove []string
	for i := range 40 {
		njihove = append(njihove, fmt.Sprintf("oznaka-%02d/1", i))
	}
	njihove = append(njihove, "probni-korak/1", OznakaRazmjena)
	prednost := append(NaseMogucnosti(), "probni-korak/1")

	got := Znanje{Mogucnosti: njihove}.OciscenoUz(prednost).Mogucnosti
	if len(got) != najviseOznaka || !slices.IsSorted(got) || !slices.Contains(got, "probni-korak/1") || !slices.Contains(got, OznakaRazmjena) {
		t.Errorf("uz prednost: %d oznaka %q", len(got), got)
	}
	if got := (Znanje{Mogucnosti: njihove}).Ocisceno().Mogucnosti; slices.Contains(got, "probni-korak/1") {
		t.Errorf("bez prednosti probni-korak/1 ne stane u ogradu, a ima ga: %q", got)
	}

	// isti popis od 42 oznake: nepoznate su oznaka-00 … oznaka-29, upravo
	// one koje OciscenoUz spremi, bez poznatih; i kad je web/1 u znam
	var ocekujem []string
	for _, o := range got {
		if o != "probni-korak/1" && o != OznakaRazmjena {
			ocekujem = append(ocekujem, o)
		}
	}
	for _, znam := range [][]string{prednost, append(slices.Clone(prednost), OznakaWeb)} {
		nepoznate := NepoznateOznakeUz(njihove, znam)
		if len(nepoznate) != 30 || !slices.Equal(nepoznate, ocekujem) {
			t.Errorf("znam %q: %d nepoznatih %q, želim %q", znam, len(nepoznate), nepoznate, ocekujem)
		}
	}

	if got := NepoznateOznakeUz([]string{"probni-korak/1", "probni-korak/2", OznakaWeb, OznakaRupe, "stranice/2"}, prednost); !slices.Equal(got, []string{"stranice/2"}) {
		t.Errorf("nepoznate uz prijavljenu: %q", got)
	}
	if got := NepoznateOznakeUz([]string{"probni-korak/1", OznakaWeb}, NaseMogucnosti()); !slices.Equal(got, []string{"probni-korak/1"}) {
		t.Errorf("neprijavljena oznaka nije nepoznata: %q", got)
	}
	// web je poznat i bez popisa; neispravna oznaka u popisu ne smeta
	if got := NepoznateOznakeUz([]string{OznakaWeb, "web/2", OznakaRupe}, []string{"losa oznaka", ""}); !slices.Equal(got, []string{OznakaRupe}) {
		t.Errorf("uz prazan popis: %q", got)
	}
}

// web/1 u prednosti uz 40 tuđih oznaka: OciscenoUz spremi isto što i 0.0.71
// (web/1 ne stane u ogradu, a oznaka-30/1 stane), a NepoznateOznakeUz
// javi upravo spremljene tuđe oznake. Prije ispravka web/1 je u OciscenoUz
// dobivao prednost i istiskivao oznaka-30/1.
func TestOznakeUzWebUPrednosti(t *testing.T) {
	var njihove []string
	for i := range 40 {
		njihove = append(njihove, fmt.Sprintf("oznaka-%02d/1", i))
	}
	njihove = append(njihove, OznakaWeb, OznakaRazmjena)
	prednost := append(NaseMogucnosti(), OznakaWeb)

	var tudje []string
	for i := range 31 {
		tudje = append(tudje, fmt.Sprintf("oznaka-%02d/1", i))
	}
	ocekujem := append(slices.Clone(tudje), OznakaRazmjena)

	for _, p := range [][]string{prednost, {OznakaWeb, OznakaRazmjena}, {OznakaWeb, "web/2", OznakaWeb, OznakaRazmjena}} {
		got := Znanje{Mogucnosti: njihove}.OciscenoUz(p).Mogucnosti
		if !slices.Equal(got, ocekujem) {
			t.Errorf("OciscenoUz(%q) = %q, želim %q", p, got, ocekujem)
		}
		if staro := staroOcisceneOznake(njihove); !slices.Equal(got, staro) {
			t.Errorf("OciscenoUz(%q) = %q, 0.0.71 %q", p, got, staro)
		}
		if got := ocisceneOznake(njihove, p); !slices.Equal(got, ocekujem) {
			t.Errorf("ocisceneOznake(%q) = %q, želim %q", p, got, ocekujem)
		}
	}
	if got := NepoznateOznakeUz(njihove, prednost); !slices.Equal(got, tudje) {
		t.Errorf("NepoznateOznakeUz = %q, želim %q", got, tudje)
	}
	if staro := staroNepoznateOznake(njihove); !slices.Equal(staro, tudje) {
		t.Errorf("0.0.71 nepoznate %q, želim %q", staro, tudje)
	}
	// samo web/1 u prednosti: kao bez prednosti, prve po abecedi
	if got := ocisceneOznake(njihove, []string{OznakaWeb}); !slices.Equal(got, ocisceneOznake(njihove, nil)) || slices.Contains(got, OznakaWeb) {
		t.Errorf("uz prednost samo web/1: %q", got)
	}
}

// Prednost s ponavljanjem, neispravnim oznakama ili više od 32 oznake ne
// probija ogradu: popis ostaje poredan, bez ponavljanja i najviše 32
func TestOznakeUzOgradaPrednosti(t *testing.T) {
	var njihove, prednost []string
	for i := range 40 {
		njihove = append(njihove, fmt.Sprintf("oznaka-%02d/1", i))
		prednost = append(prednost, fmt.Sprintf("oznaka-%02d/1", 39-i), fmt.Sprintf("oznaka-%02d/1", 39-i), "losa oznaka")
	}
	got := ocisceneOznake(njihove, prednost)
	if len(got) != najviseOznaka || !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Fatalf("%d oznaka %q", len(got), got)
	}
	// prednost ide redom iz popisa: zadrže se oznake 08–39, a ne prve po abecedi
	if got[0] != "oznaka-08/1" || got[len(got)-1] != "oznaka-39/1" {
		t.Errorf("zadržane %q … %q", got[0], got[len(got)-1])
	}
	if got := ocisceneOznake([]string{"losa oznaka"}, prednost); got != nil {
		t.Errorf("bez ispravnih oznaka %q, želim nil", got)
	}
}

// ProvjeriOznaku prihvaća isto što i pravilo mogućnosti (procitajOznaku),
// a ostalo odbija s ErrNeispravnaOznaka
func TestProvjeriOznaku(t *testing.T) {
	for _, c := range []struct {
		oznaka string
		ime    string
		n      int
	}{
		{"probni-korak/1", "probni-korak", 1},
		{"probni-korak/12", "probni-korak", 12},
		{strings.Repeat("a", 32) + "/1", strings.Repeat("a", 32), 1},
	} {
		ime, n, err := ProvjeriOznaku(c.oznaka)
		if err != nil || ime != c.ime || n != c.n {
			t.Errorf("%q: %q %d %v", c.oznaka, ime, n, err)
		}
	}
	for _, oznaka := range []string{"", "probni", "probni/", "/1", "probni/0", "probni/01", "Probni/1", "probni korak/1",
		"probni_korak/1", "čvor/1", strings.Repeat("a", 33) + "/1"} {
		ime, n, err := ProvjeriOznaku(oznaka)
		if !errors.Is(err, ErrNeispravnaOznaka) || ime != "" || n != 0 {
			t.Errorf("%q: %q %d %v", oznaka, ime, n, err)
		}
		if _, _, ok := procitajOznaku(oznaka); ok {
			t.Errorf("%q: pravilo je prihvaća", oznaka)
		}
	}
}
