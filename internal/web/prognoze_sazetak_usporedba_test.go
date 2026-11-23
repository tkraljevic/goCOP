package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// Sažetak uz mađarsku ima i usporedbu sa srpskom prognozom na srpskim
// letvama. Srpska seže četiri dana, pa drugi stupac je zadnji dan koji ima
// obje. Letva bez srpske prognoze ne ulazi, a bez ijedne tablice nema.
// Brojevi su izmišljeni.
func TestUsporedbaSaSrpskomPrognozom(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	dani := func(nase []float64, rs map[int]float64) []CelijaDana {
		naslovi := []string{"pet 25.9.", "sub 26.9.", "ned 27.9.", "pon 28.9.", "uto 29.9.", "sri 30.9."}
		var out []CelijaDana
		for i, n := range nase {
			d := CelijaDana{CmV: v(n), Naslov: naslovi[i]}
			if r, ima := rs[i]; ima {
				d.Tude = []TudaCelija{{Oznaka: "RS", CmV: r}}
			}
			out = append(out, d)
		}
		return out
	}
	data := PrognozePageData{Tablice: []TablicaPrognoza{{Naslov: "Dunav", Letve: []LetvaPrognoze{
		{Kod: "batina", Naziv: "Batina", Dani: dani([]float64{100, 101, 102, 103, 104, 105}, nil)},
		{Kod: "gornje-selo", Naziv: "Gornje Selo (Srbija)", Dani: dani([]float64{200, 201, 202, 203, 204, 205}, map[int]float64{2: 199, 3: 215})},
		{Kod: "donje-selo", Naziv: "Donje Selo (Srbija)", Dani: dani([]float64{300, 301, 302, 303, 304, 305}, nil)},
	}}}}
	rs := srpskeZaUsporedbu(data)
	if len(rs) != 2 || rs[0].Ime != "Gornje Selo" {
		t.Fatalf("srpske letve %+v: trebaju Gornje i Donje Selo", rs)
	}
	if z := zadnjiTude(rs, 5); z != 3 {
		t.Errorf("zadnji dan s objema prognozama %d, treba 3", z)
	}

	k := &xlsxw.Knjiga{}
	l := k.NoviList("Sažetak")
	var naslovi, odlomci []string
	naslov := func(s string) { naslovi = append(naslovi, s); l.Dodaj(xlsxw.T(s, xlsxw.Podnaslov)) }
	odlomak := func(s string, stil int) { odlomci = append(odlomci, s); l.Dodaj(xlsxw.T(s, stil)) }
	dodajUsporedbu(l, naslov, odlomak, "RS", "Usporedba sa srpskom prognozom (hidmet.gov.rs)", "RS je srpska prognoza.", rs, zadnjiTude(rs, 5))
	txt := tekstKnjige(t, k)
	for _, want := range []string{">Gornje Selo<", "Za 3 dana (27. 9.)", "Za 4 dana (28. 9.)", ">202<", ">199<", ">203<", ">215<", ">RS<", ">razlika<"} {
		if !strings.Contains(txt, want) {
			t.Errorf("u usporedbi nema %q", want)
		}
	}
	if strings.Contains(txt, "Donje Selo") || len(naslovi) != 1 || len(odlomci) != 1 {
		t.Errorf("Donje Selo nema srpske prognoze, a tablica je jedna: naslovi %v", naslovi)
	}

	// Bez srpskih letvi s prognozom tablice nema.
	prazna := &xlsxw.Knjiga{}
	l = prazna.NoviList("Sažetak")
	naslovi = nil
	dodajUsporedbu(l, naslov, odlomak, "RS", "x", "y", rs[1:], zadnjiTude(rs[1:], 5))
	dodajUsporedbu(l, naslov, odlomak, "RS", "x", "y", rs, 1)
	if len(naslovi) != 0 {
		t.Errorf("tablica bez ijedne srpske vrijednosti: %v", naslovi)
	}
}

// Srpska prognoza preračunava se na naše postaje prvim putom pričuvnog
// izračuna kojemu su svi ulazi srpske letve; put preko mađarske letve se
// preskače, a postaja bez srpskog puta ne ulazi. Brojevi su izmišljeni.
func TestPreracunSrpskeNaNase(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	pocetak := time.Date(2026, 9, 25, 5, 0, 0, 0, time.UTC) // 7 h po našem
	var ciljevi []int64
	dani := []time.Time{pocetak.Add(-24 * time.Hour)}
	for d := 0; d < 6; d++ {
		t := pocetak.AddDate(0, 0, d)
		ciljevi = append(ciljevi, t.Unix()/3600)
		dani = append(dani, t)
	}
	p := prognoza.PricuvniPodaci{Dani: dani,
		Veze: map[string][]prognoza.PricuvnaVeza{
			"batina": {{Izvori: []string{"bezdan"}, Koef: []float64{20, 1}, Pomak: -2}, {Izvori: []string{"mohacs"}, Koef: []float64{0, 1}}},
			"aljmas": {{Izvori: []string{"mohacs"}, Koef: []float64{5, 1}}, {Izvori: []string{"apatin", "bogojevo"}, Koef: []float64{3, 0.5, 0.5}}},
			"ilok":   {{Izvori: []string{"backa-palanka"}, Greska: "premalo dana"}},
		},
		Ulazi: map[string][prognoza.PricuvniDana + 1]*float64{
			"bezdan":   {nil, v(-60), v(-58), v(-55), v(-50), nil, nil},
			"apatin":   {nil, v(10), v(12), nil, nil, nil, nil},
			"bogojevo": {nil, v(20), v(22), v(24), nil, nil, nil},
			"mohacs":   {nil, v(1), v(2), v(3), v(4), v(5), v(6)},
		}}
	nase := []postajaSazetka{
		{Kod: "batina", Ime: "Batina", Dani: []*float64{v(-40), v(-38)}},
		{Kod: "aljmas", Ime: "Aljmaš", Dani: []*float64{v(10)}},
		{Kod: "ilok", Ime: "Ilok", Dani: []*float64{v(5)}},
		{Kod: "vukovar", Ime: "Vukovar"},
	}
	out := preracunSrpske(p, ciljevi, nase)
	if len(out) != 2 {
		t.Fatalf("preračun %+v: trebaju Batina i Aljmaš", out)
	}
	if out[0].Ime != "Batina" {
		t.Errorf("ime %q", out[0].Ime)
	}
	if got := out[0].Tuda; got[0] == nil || *got[0] != -42 || *got[3] != -32 || got[4] != nil {
		t.Errorf("Batina iz Bezdana: %v", got)
	}
	if got := out[1].Tuda; out[1].Ime != "Aljmaš" || got[0] == nil || *got[0] != 18 || got[2] != nil {
		t.Errorf("Aljmaš iz Apatina i Bogojeva: %q %v", out[1].Ime, got)
	}
}

// Sažetak uzima srpski preračun iz pričuvnog izračuna kad ga poslužitelj ima;
// bez njega ili kad izračun ne radi preračuna nema. Kratak niz dana daje nil.
func TestSrpskaNaNaseIzPricuvnog(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	izdano := time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC)
	_, ciljevi := daniPregleda(izdano)
	dani := []time.Time{time.Unix(ciljevi[0]*3600, 0).Add(-24 * time.Hour)}
	for _, c := range ciljevi {
		dani = append(dani, time.Unix(c*3600, 0))
	}
	data := PrognozePageData{IzdanoSat: izdano.Unix() / 3600}
	nase := []postajaSazetka{{Kod: "batina", Ime: "Batina", Dani: []*float64{v(1)}}}
	h := &PrognozeHandler{}
	if out := h.srpskaNaNase(context.Background(), data, nase); out != nil {
		t.Errorf("bez pričuvnog izračuna: %+v", out)
	}
	h.pricuvno = func(context.Context) (prognoza.PricuvniPodaci, bool) { return prognoza.PricuvniPodaci{}, false }
	if out := h.srpskaNaNase(context.Background(), data, nase); out != nil {
		t.Errorf("izračun ne radi: %+v", out)
	}
	h.pricuvno = func(context.Context) (prognoza.PricuvniPodaci, bool) {
		return prognoza.PricuvniPodaci{Dani: dani,
			Veze:  map[string][]prognoza.PricuvnaVeza{"batina": {{Izvori: []string{"bezdan"}, Koef: []float64{20, 1}}}},
			Ulazi: map[string][prognoza.PricuvniDana + 1]*float64{"bezdan": {nil, v(-60)}}}, true
	}
	out := h.srpskaNaNase(context.Background(), data, nase)
	if len(out) != 1 || out[0].Tuda[0] == nil || *out[0].Tuda[0] != -40 {
		t.Errorf("Batina iz Bezdana: %+v", out)
	}
	if out[0].u(out[0].Nasa, 3) != nil || out[0].u(out[0].Nasa, 0) == nil {
		t.Error("dan izvan niza mora biti nil, a prvi dan vrijednost")
	}
}
