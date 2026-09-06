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
