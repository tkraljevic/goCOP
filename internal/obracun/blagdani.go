package obracun

import (
	"time"

	"gocop/internal/models"
)

// Hrvatski je kalendar blagdana Republike Hrvatske po Zakonu o blagdanima,
// spomendanima i neradnim danima (NN 110/2019): jedanaest stalnih datuma i
// tri pomična — Uskrs, Uskrsni ponedjeljak i Tijelovo. Isti popis nosi i
// obrazac IORS u imenovanom rasponu Praznik, samo upisan ručno po godinama.
type Hrvatski struct{}

// stalni blagdani: mjesec i dan
var stalni = [][2]int{
	{1, 1},   // Nova godina
	{1, 6},   // Bogojavljenje ili Sveta tri kralja
	{5, 1},   // Praznik rada
	{5, 30},  // Dan državnosti
	{6, 22},  // Dan antifašističke borbe
	{8, 5},   // Dan pobjede i domovinske zahvalnosti i Dan hrvatskih branitelja
	{8, 15},  // Velika Gospa
	{11, 1},  // Svi sveti
	{11, 18}, // Dan sjećanja na žrtve Domovinskog rata
	{12, 25}, // Božić
	{12, 26}, // Sveti Stjepan
}

// Blagdan javlja je li dan blagdan; gleda se zidni datum u zoni Zagreb
func (Hrvatski) Blagdan(dan time.Time) bool {
	dan = dan.In(models.Zagreb)
	for _, s := range stalni {
		if int(dan.Month()) == s[0] && dan.Day() == s[1] {
			return true
		}
	}
	u := Uskrs(dan.Year())
	for _, pomak := range []int{0, 1, 60} { // Uskrs, Uskrsni ponedjeljak, Tijelovo
		p := u.AddDate(0, 0, pomak)
		if p.Month() == dan.Month() && p.Day() == dan.Day() {
			return true
		}
	}
	return false
}

// Blagdani vraća sve blagdane godine, redom
func (h Hrvatski) Blagdani(godina int) []time.Time {
	var out []time.Time
	for d := time.Date(godina, 1, 1, 0, 0, 0, 0, models.Zagreb); d.Year() == godina; d = d.AddDate(0, 0, 1) {
		if h.Blagdan(d) {
			out = append(out, d)
		}
	}
	return out
}

// Uskrs računa datum Uskrsa po gregorijanskom kalendaru (Meeusov algoritam)
func Uskrs(godina int) time.Time {
	a := godina % 19
	b := godina / 100
	c := godina % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	mjesec := (h + l - 7*m + 114) / 31
	dan := (h+l-7*m+114)%31 + 1
	return time.Date(godina, time.Month(mjesec), dan, 0, 0, 0, 0, models.Zagreb)
}
