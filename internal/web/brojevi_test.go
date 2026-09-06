package web

import "testing"

// Izvoz piše decimalni zarez, uvoz ga vraća na točku. Bez toga se površina
// uređena u Excelu vrati kao nula, a stupac je tiho izgubljen.
func TestCsvFloatPiseZarez(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{34.5, "34,5"},
		{1234.56, "1234,56"},
		{-3.25, "-3,25"},
		{12, "12"},
		{0, "0"},
	}
	for _, c := range cases {
		if got := csvFloat(c.in); got != c.want {
			t.Errorf("csvFloat(%v) = %q, očekivano %q", c.in, got, c.want)
		}
	}
}

func TestAtofCitaSvaPisanja(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"34,5", 34.5},      // izvoz nakon ove promjene
		{"34.5", 34.5},      // stariji izvoz
		{" 34,5 ", 34.5},    // razmaci oko vrijednosti
		{"1.234,5", 1234.5}, // hrvatski Excel s razdjelnikom tisućica
		{"1,234.5", 1234.5}, // engleski Excel
		{"1 234,5", 1234.5}, // razmak kao razdjelnik tisućica
		{"1 234,5", 1234.5}, // tvrdi razmak
		{"1.234.567,5", 1234567.5},
		{"-3,25", -3.25},
		{"1234", 1234},
		{"", 0},
		{"nije broj", 0},
		{"12,5,7", 0}, // promašen unos: skupina tisućica nema tri znamenke
	}
	for _, c := range cases {
		if got := atof(c.in); got != c.want {
			t.Errorf("atof(%q) = %v, očekivano %v", c.in, got, c.want)
		}
	}
}

func TestAtoiPodnosiExcelovoOblikovanje(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"1234", 1234},
		{" 1234 ", 1234},
		{"1.234", 1234},
		{"1 234", 1234},
		{"1234,00", 1234},
		{"1.234.567", 1234567},
		{"-1.234", -1234},
		{"-12", -12},
		{"", 0},
		{"nije broj", 0},
	}
	for _, c := range cases {
		if got := atoi(c.in); got != c.want {
			t.Errorf("atoi(%q) = %d, očekivano %d", c.in, got, c.want)
		}
	}
}

// Krug izvoz → uvoz mora vratiti isti broj, jer se tablica upravo tako
// održava: izvezi, uredi u Excelu, uvezi natrag.
func TestKrugIzvozUvoz(t *testing.T) {
	for _, v := range []float64{0, 12, 34.5, 1234.56, 0.125, -3.25} {
		if got := atof(csvFloat(v)); got != v {
			t.Errorf("krug za %v dao %v", v, got)
		}
	}
}

func TestBrojHRGrupiraTisucice(t *testing.T) {
	for _, c := range []struct {
		in   int
		want string
	}{
		{758941, "758.941"}, {17676, "17.676"}, {967, "967"}, {1000, "1.000"},
		{-1234567, "-1.234.567"}, {0, "0"},
	} {
		if got := brojHR(c.in); got != c.want {
			t.Errorf("brojHR(%d) = %q, očekivano %q", c.in, got, c.want)
		}
	}
}

func TestBrojHRfSpajaTisuciceIZarez(t *testing.T) {
	for _, c := range []struct {
		in   float64
		dec  int
		want string
	}{
		{1234.5, 1, "1.234,5"},
		{34.5, 1, "34,5"},
		{967, 1, "967,0"},
		{1234567.89, 2, "1.234.567,89"},
		{620, 0, "620"},
		{-1234.5, 1, "-1.234,5"},
	} {
		if got := brojHRf(c.in, c.dec); got != c.want {
			t.Errorf("brojHRf(%v, %d) = %q, očekivano %q", c.in, c.dec, got, c.want)
		}
	}
}

// Prikaz pokazuje crticu za neupisan podatak, obrazac ostavlja polje prazno —
// crtica u polju bi se pri spremanju pokušala pročitati kao broj.
func TestNeupisanPodatak(t *testing.T) {
	if got := brojHRd(nil, 2); got != "-" {
		t.Errorf("brojHRd(nil) = %q, očekivano %q", got, "-")
	}
	if got := unosD(nil, 2); got != "" {
		t.Errorf("unosD(nil) = %q, očekivano prazno", got)
	}
	v := 1234.5
	if got := brojHRd(&v, 1); got != "1.234,5" {
		t.Errorf("brojHRd = %q", got)
	}
	if got := unosD(&v, 1); got != "1234,5" {
		t.Errorf("unosD = %q, u polju obrasca nema razdjelnika tisućica", got)
	}
}

// Krug polje → spremanje → polje mora vratiti isti broj, inače se vrijednost
// gubi svakim otvaranjem obrasca.
func TestKrugObrazac(t *testing.T) {
	for _, v := range []float64{0, 34.5, 82.06, 1234.5, 0.125} {
		got, ok := parseBroj(unos(v, 3))
		if !ok || got != v {
			t.Errorf("krug za %v dao %v (ok=%v)", v, got, ok)
		}
	}
}

func TestParseBrojOdbijaSmece(t *testing.T) {
	for _, s := range []string{"", "   ", "nije broj", "-", "12,5,7"} {
		if _, ok := parseBroj(s); ok {
			t.Errorf("parseBroj(%q) je prihvaćen, a ne bi smio", s)
		}
	}
}
