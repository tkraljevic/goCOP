package prognoza

// Letve s druge obale. Neke letve stoje jedna nasuprot drugoj, na dvije obale
// iste rijeke, pa je voda na njima ista, a razlikuju se samo nule. Kad jednoj
// nedostaje sat — rupa u sredini ili zakašnjeli kraj — popuni ga druga,
// pomaknuta za razliku nula izmjerenu na zajedničkim satima zadnjih dana.
// Nagiba nema, ista je voda.
//
// Povod: vizugy.hu zna večerima i noću stati po nekoliko sati (29. 9. 2026. od
// 18 h za svih 35 mađarskih postaja), a Komárom je, dok ga vodi mađarska
// prognoza, vrh lanca i drži cijelu prognozu na satu svojeg zadnjeg mjerenja —
// iako val od njega do Batine putuje dva do tri dana. Slovačko Komárno s druge
// obale javlja dalje.
//
// Razlike na zajedničkim satima 1.–29. 9. 2026. (niska voda):
//
//	Komárom − Komárno            −46 cm  (±1,0; na dnevnim 2004.–2026. −46 do −52)
//	Batina − Bezdan              +21 cm  (±1,6; 740 m)
//	Goričan − Letenye             −8 cm  (±0,8)
//	Terezino Polje − Barcs      −201 cm  (±1,8; 1,8 km)
//	Donji Miholjac − Drávaszabolcs −81 cm (±1,6; 2,9 km, od −77 do −92)
//	Ilok − Bačka Palanka          −1 cm  (±1,2)
//
// Popunjava se u centimetrima, prije preračuna u protok, pa i letve koje se
// vode u protoku (Donji Miholjac, Terezino Polje) dobivaju sat preko svoje
// krivulje. Popunjeni sat ulazi samo u račun prognoze; mjerenjem se ne
// pokazuje i u evidenciju ne ide.

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

// ParoviObala su letve jedna nasuprot drugoj; vrijedi u oba smjera.
var ParoviObala = [][2]string{
	{"komarom", "komarno"},
	{"batina", "bezdan"},
	{"gorican", "letenye"},
	{"terezino-polje", "barcs"},
	{"donji-miholjac", "dravaszabolcs"},
	{"ilok", "backa-palanka"},
}

// DrugaObala vraća letvu s druge obale, ako je ima.
func DrugaObala(letva string) (string, bool) {
	for _, p := range ParoviObala {
		switch letva {
		case p[0]:
			return p[1], true
		case p[1]:
			return p[0], true
		}
	}
	return "", false
}

// ObalaSati je koliko se sati unatrag od zadnjeg mjerenja letve traže
// zajednički sati za razliku nula.
const ObalaSati = 14 * 24

// ObalaNajmanjeSati je koliko zajedničkih sati razlika traži; s manje se
// letva ne popunjava.
const ObalaNajmanjeSati = 24

// Zamjena opisuje sate koje je letvi na kraju niza dala druga obala.
type Zamjena struct {
	Iz    string // letva s druge obale
	Pomak int64  // cm koji se dodaju letvi s druge obale
	Sati  int    // koliko je sati na kraju niza popunjeno
	Zajed int    // na koliko zajedničkih sati je izmjeren pomak
}

// Opis je rečenica za karticu; šifra letve stoji zasebno, da je stranica
// zamijeni imenom.
func (z Zamjena) Opis() string {
	return fmt.Sprintf("zamjena: mjerenje kasni %d h; ti su sati uzeti s druge obale, s letve %s uz pomak %+d cm (razlika nula na %d zajedničkih sati)",
		z.Sati, z.Iz, z.Pomak, z.Zajed)
}

// pomakObala je medijan razlike letva − druga na satima koje imaju obje, u
// zadnjih ObalaSati sati letve, zaokružen na centimetar.
func pomakObala(letva, druga map[int64]sirovoOcitanje) (int64, int, bool) {
	var zadnji int64
	for t, s := range letva {
		if s.cm.Valid && t > zadnji {
			zadnji = t
		}
	}
	var razlike []float64
	for t, s := range letva {
		if !s.cm.Valid || t < zadnji-ObalaSati {
			continue
		}
		if d, ima := druga[t]; ima && d.cm.Valid {
			razlike = append(razlike, float64(s.cm.Int64-d.cm.Int64))
		}
	}
	if len(razlike) < ObalaNajmanjeSati {
		return 0, len(razlike), false
	}
	sort.Float64s(razlike)
	m := razlike[len(razlike)/2]
	if len(razlike)%2 == 0 {
		m = (razlike[len(razlike)/2-1] + razlike[len(razlike)/2]) / 2
	}
	return int64(math.Round(m)), len(razlike), true
}

// PopuniSirovo upisuje u letvu sate koje ima samo druga obala, s drugom
// pomaknutom za razliku nula. Vraća koliko je sati upisano i pomak.
func PopuniSirovo(letva, druga map[int64]sirovoOcitanje) (int, Zamjena) {
	pomak, zajed, ok := pomakObala(letva, druga)
	if !ok {
		return 0, Zamjena{}
	}
	n := 0
	for t, d := range druga {
		if !d.cm.Valid {
			continue
		}
		if s, ima := letva[t]; ima && (s.cm.Valid || s.q.Valid) {
			continue
		}
		letva[t] = sirovoOcitanje{cm: sql.NullInt64{Int64: d.cm.Int64 + pomak, Valid: true}}
		n++
	}
	return n, Zamjena{Pomak: pomak, Zajed: zajed}
}

// zamjenaNaKraju javlja je li kraj niza letve došao s druge obale — tada je
// njezino vlastito mjerenje zakasnilo, i to piše na kartici.
func (o *Osvjezivac) zamjenaNaKraju(ctx context.Context, letva string, od time.Time) (Zamjena, bool) {
	druga, ima := DrugaObala(letva)
	if !ima {
		return Zamjena{}, false
	}
	vlastito, err := o.ucitajSirovo(ctx, letva, od)
	if err != nil {
		return Zamjena{}, false
	}
	drugo, err := o.ucitajSirovo(ctx, druga, od)
	if err != nil {
		return Zamjena{}, false
	}
	var zadnji int64
	for t, s := range vlastito {
		if s.cm.Valid && t > zadnji {
			zadnji = t
		}
	}
	pomak, zajed, ok := pomakObala(vlastito, drugo)
	if !ok {
		return Zamjena{}, false
	}
	sati := 0
	for t, d := range drugo {
		if d.cm.Valid && t > zadnji {
			sati++
		}
	}
	if sati == 0 {
		return Zamjena{}, false
	}
	return Zamjena{Iz: druga, Pomak: pomak, Sati: sati, Zajed: zajed}, true
}
