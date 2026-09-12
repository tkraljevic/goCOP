package obracun

import (
	"time"

	"gocop/internal/models"
)

// Hrvatski je kalendar po Zakonu o blagdanima kakav program nosi u sebi —
// ono čime se baza puni i čime se testira. U radu se kalendar čita iz baze
// (Pravila), jer ga organizacija uređuje.
type Hrvatski struct{}

// Blagdan javlja je li dan blagdan; gleda se zidni datum u zoni Zagreb
func (Hrvatski) Blagdan(dan time.Time) bool { return ZakonskiBlagdani().Blagdan(dan.In(models.Zagreb)) }

// Blagdani vraća sve blagdane godine, redom
func (Hrvatski) Blagdani(godina int) []time.Time { return ZakonskiBlagdani().Blagdani(godina) }

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
