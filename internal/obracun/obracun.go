// Package obracun razvrstava sate rada u obrani od poplava onako kako to radi
// obrazac IORS (Izvješće o radnim satima) Hrvatskih voda, i množi ih
// koeficijentima u obračunske sate.
//
// Pravila su prepisana iz formula obrasca, ne iz njegovih naslova — tamo gdje
// se razlikuju, formula je ono što se godinama isplaćivalo. Dan je jedan od
// tri: radni, vikend (subota ili nedjelja koja nije blagdan) ili blagdan.
// Radni dan ima redovno radno vrijeme 8–16, dnevne sate 6–8 i 16–22 te noćne
// 0–6 i 22–24; vikend i blagdan imaju dnevne 6–22 i noćne ostalo.
//
// Sve se računa po zidnom satu u zoni Europe/Zagreb, kao što se i upisuje —
// dežurni ne gleda UTC. Razmak koji prijeđe ponoć dijeli se po danima, jer
// svaki dan nosi svoju vrstu.
package obracun

import (
	"time"

	"gocop/internal/models"
)

// Razred sata: tri vrste dana puta tri (odnosno dva) pojasa. Kratice su iz
// pomoćnog lista obrasca i tako se i zbrajaju.
type Razred string

const (
	RRV Razred = "RRV" // radni dan, redovno radno vrijeme 8–16
	DRD Razred = "DRD" // radni dan, dnevni sati 6–8 i 16–22
	NRD Razred = "NRD" // radni dan, noćni sati 0–6 i 22–24
	VID Razred = "VID" // vikend, dnevni 6–22
	VIN Razred = "VIN" // vikend, noćni
	BLD Razred = "BLD" // blagdan, dnevni 6–22
	BLN Razred = "BLN" // blagdan, noćni
)

// Razredi redom kojim ih obrazac ispisuje
var Razredi = []Razred{RRV, DRD, NRD, VID, VIN, BLD, BLN}

// Naziv razreda za prikaz
func (r Razred) Naziv() string {
	switch r {
	case RRV:
		return "radni dan, redovno (8–16)"
	case DRD:
		return "radni dan, dnevni (6–8 i 16–22)"
	case NRD:
		return "radni dan, noćni (22–6)"
	case VID:
		return "vikend, dnevni (6–22)"
	case VIN:
		return "vikend, noćni (22–6)"
	case BLD:
		return "blagdan, dnevni (6–22)"
	case BLN:
		return "blagdan, noćni (22–6)"
	}
	return string(r)
}

// Mjesto rada: obrazac razlikuje samo ured i teren, s različitim koeficijentima
type Mjesto string

const (
	Ured  Mjesto = "URED"
	Teren Mjesto = "TEREN"
)

// Sati po razredu; nula se ne upisuje
type Sati map[Razred]time.Duration

// Ukupno zbraja sve razrede
func (s Sati) Ukupno() time.Duration {
	var u time.Duration
	for _, d := range s {
		u += d
	}
	return u
}

// Dodaj zbraja druge sate u ove
func (s Sati) Dodaj(o Sati) {
	for r, d := range o {
		if d > 0 {
			s[r] += d
		}
	}
}

// VrstaDana je ono što o danu odlučuje: blagdan ima prednost pred vikendom,
// pa nedjelja koja je blagdan ide u blagdan, kao i u obrascu.
type VrstaDana int

const (
	RadniDan VrstaDana = iota
	Vikend
	Blagdan
)

// Kalendar kaže je li dan blagdan. Zadani je hrvatski; druga organizacija
// ima svoj.
type Kalendar interface {
	Blagdan(dan time.Time) bool
}

// Dan razvrstava datum
func Dan(dan time.Time, k Kalendar) VrstaDana {
	if k != nil && k.Blagdan(dan) {
		return Blagdan
	}
	if wd := dan.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return Vikend
	}
	return RadniDan
}

// pojas je odsječak dana u minutama od ponoći, [od, do)
type pojas struct {
	od, do int
	razred Razred
}

func pojasevi(vrsta VrstaDana) []pojas {
	switch vrsta {
	case RadniDan:
		return []pojas{{0, 6 * 60, NRD}, {6 * 60, 8 * 60, DRD}, {8 * 60, 16 * 60, RRV}, {16 * 60, 22 * 60, DRD}, {22 * 60, 24 * 60, NRD}}
	case Vikend:
		return []pojas{{0, 6 * 60, VIN}, {6 * 60, 22 * 60, VID}, {22 * 60, 24 * 60, VIN}}
	default:
		return []pojas{{0, 6 * 60, BLN}, {6 * 60, 22 * 60, BLD}, {22 * 60, 24 * 60, BLN}}
	}
}

// Razvrstaj dijeli razmak [od, do) po razredima. Razmak koji prijeđe ponoć
// dijeli se po danima; obrnut ili prazan razmak daje prazne sate.
func Razvrstaj(od, do time.Time, k Kalendar) Sati {
	out := Sati{}
	od, do = od.In(models.Zagreb), do.In(models.Zagreb)
	for do.After(od) {
		ponoc := time.Date(od.Year(), od.Month(), od.Day()+1, 0, 0, 0, 0, models.Zagreb)
		kraj := do
		if kraj.After(ponoc) {
			kraj = ponoc
		}
		// Minute zidnog sata, ne stvarne: na dan prijelaza na zimsko vrijeme
		// sat 02:00 dođe dvaput, a obrazac i isplata gledaju sat na zidu.
		odMin := od.Hour()*60 + od.Minute()
		doMin := kraj.Hour()*60 + kraj.Minute()
		if kraj.Equal(ponoc) {
			doMin = 24 * 60
		}
		for _, p := range pojasevi(Dan(od, k)) {
			a, b := max(odMin, p.od), min(doMin, p.do)
			if b > a {
				out[p.razred] += time.Duration(b-a) * time.Minute
			}
		}
		od = ponoc
	}
	return out
}

// Koeficijenti množe stvarne sate u obračunske, po mjestu i razredu.
// To je podatak organizacije, ne pravilo programa; ovo su vrijednosti iz
// obrasca IORS 2026.
type Koeficijenti map[Mjesto]map[Razred]float64

// IORS2026 su koeficijenti iz lista Obrazac_Obračuna. Ured u redovnom
// radnom vremenu nosi 0: to je plaća, ne prekovremeni.
var IORS2026 = Koeficijenti{
	Ured:  {RRV: 0, DRD: 1.5, NRD: 1.85, VID: 1.85, VIN: 2.2, BLD: 2, BLN: 2.35},
	Teren: {RRV: 0.2, DRD: 1.7, NRD: 2.05, VID: 2.05, VIN: 2.4, BLD: 2.2, BLN: 2.55},
}

// Obracunski vraća obračunske sate: stvarni sati puta koeficijent, po
// razredu zaokruženo na dvije decimale kao u obrascu
func (k Koeficijenti) Obracunski(s Sati, m Mjesto) float64 {
	var u float64
	for r, d := range s {
		u += zaokruzi(d.Hours()*k[m][r], 2)
	}
	return zaokruzi(u, 2)
}

func zaokruzi(x float64, dec int) float64 {
	p := 1.0
	for i := 0; i < dec; i++ {
		p *= 10
	}
	if x < 0 {
		return float64(int64(x*p-0.5)) / p
	}
	return float64(int64(x*p+0.5)) / p
}
