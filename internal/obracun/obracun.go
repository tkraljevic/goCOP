// Package obracun razvrstava sate rada u obrani od poplava onako kako to radi
// obrazac IORS (Izvješće o radnim satima) Hrvatskih voda, i množi ih
// koeficijentima u obračunske sate.
//
// Dan je jedan od tri: radni, subota, ili nedjelja i blagdan. Radni dan ima
// redovno radno vrijeme 8–16, dnevne sate 6–8 i 16–22 te noćne 0–6 i 22–24;
// subota i blagdan imaju dnevne 6–22 i noćne ostalo.
//
// Nedjelja se obračunava kao blagdan, kako i piše u stupcu obrasca
// ("NEDJELJA I BLAGDAN"). Formule u proračunskoj tablici obrasca nedjelju
// su svrstavale u vikend, s koeficijentom subote — to je greška tablice, ne
// pravilo, i ovdje se ne ponavlja.
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
	VID Razred = "VID" // subota, dnevni 6–22
	VIN Razred = "VIN" // subota, noćni
	BLD Razred = "BLD" // nedjelja i blagdan, dnevni 6–22
	BLN Razred = "BLN" // nedjelja i blagdan, noćni
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
		return "subota, dnevni (6–22)"
	case VIN:
		return "subota, noćni (22–6)"
	case BLD:
		return "nedjelja i blagdan, dnevni (6–22)"
	case BLN:
		return "nedjelja i blagdan, noćni (22–6)"
	}
	return string(r)
}

// Dan je vrsta dana razreda, za zaglavlje tablice
func (r Razred) Dan() string {
	switch r {
	case RRV, DRD, NRD:
		return "radni dan"
	case VID, VIN:
		return "subota"
	}
	return "nedjelja i blagdan"
}

// Pojas je dio dana razreda, za zaglavlje tablice
func (r Razred) Pojas() string {
	switch r {
	case RRV:
		return "redovno 8–16"
	case DRD:
		return "dnevni 6–8 i 16–22"
	case NRD:
		return "noćni 22–6"
	case VID, BLD:
		return "dnevni 6–22"
	}
	return "noćni 22–6"
}

// Kratko je naziv razreda za popis u jednom retku: "subota noćni"
func (r Razred) Kratko() string {
	switch r {
	case RRV:
		return "radni dan redovno"
	case DRD:
		return "radni dan dnevni"
	case NRD:
		return "radni dan noćni"
	case VID:
		return "subota dnevni"
	case VIN:
		return "subota noćni"
	case BLD:
		return "nedj./blagdan dnevni"
	}
	return "nedj./blagdan noćni"
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

// VrstaDana je ono što o danu odlučuje: nedjelja i blagdan su jedno, subota
// je svoje.
type VrstaDana int

const (
	RadniDan VrstaDana = iota
	Subota
	Blagdan // i nedjelja
)

// Kalendar kaže je li dan blagdan. Zadani je hrvatski; druga organizacija
// ima svoj.
type Kalendar interface {
	Blagdan(dan time.Time) bool
}

// Dan razvrstava datum
func Dan(dan time.Time, k Kalendar) VrstaDana {
	if dan.Weekday() == time.Sunday || (k != nil && k.Blagdan(dan)) {
		return Blagdan
	}
	if dan.Weekday() == time.Saturday {
		return Subota
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
	case Subota:
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

// Korak je na što se obračunski sati zaokružuju: pola sata, kao u obračunu
// koji se isplaćuje (72, 49, 52,5, 77,5).
const Korak = 0.5

// ObracunskiPoRazredu vraća obračunske sate po razredu: stvarni sati puta
// koeficijent, zaokruženo na Korak. Zbroj se radi iz zaokruženih, da ono što
// piše po stupcima daje ono što piše u zbroju.
func (k Koeficijenti) ObracunskiPoRazredu(s Sati, m Mjesto) map[Razred]float64 {
	out := map[Razred]float64{}
	for r, d := range s {
		if d > 0 {
			out[r] = Zaokruzi(d.Hours()*k[m][r], Korak)
		}
	}
	return out
}

// Obracunski vraća zbroj obračunskih sati po razredima
func (k Koeficijenti) Obracunski(s Sati, m Mjesto) float64 {
	var u float64
	for _, v := range k.ObracunskiPoRazredu(s, m) {
		u += v
	}
	return Zaokruzi(u, Korak)
}

// Zaokruzi zaokružuje na najbliži višekratnik koraka; kad je točno na
// sredini, ide gore — djelatniku: 0,25 na 0,5, 0,75 na 1.
//
// Pravilo "na parni broj" bilo bi pošteno tek u prosjeku: s koeficijentom
// 1,5 sredina pada na svaki neparni polusat, pa bi tko uvijek odradi istih
// 1,5 h uvijek gubio četvrt sata, a tko odradi 0,5 h uvijek dobivao. Ovako
// nitko ne gubi sustavno, pravilo stane u jednu rečenicu, a ustanovu košta
// najviše četvrt sata po razredu po osobi po obračunu.
func Zaokruzi(x, korak float64) float64 {
	if korak <= 0 {
		return x
	}
	q := x / korak
	dolje := float64(int64(q))
	if q < 0 && dolje != q {
		dolje--
	}
	if q-dolje < 0.5-1e-9 {
		return dolje * korak
	}
	return (dolje + 1) * korak
}
