package prognoza

// Rekonstrukcija rupe iz susjeda na istoj rijeci. Letve bez para na drugoj
// obali (Zamjenske) ostaju bez mjerenja kad izvor stane: Szentborbás od
// 11. 10. 2026. u 4 h, jer vizugy.hu za njega nije objavio ništa, a
// DanubeHIS ga nema; Mohács od 7. 10. u 11 h do 8. 10. u 9 h. Prognoza je tada
// karike računala iz uzvodnih letvi, a dnevni model je ostao bez dana.
//
// Rupu popuni susjed uzvodno ili nizvodno: letvi se dodaje ono što je susjed
// u isto vrijeme (pomaknuto za vrijeme putovanja vode) promijenio. Rupi s
// poznatim krajevima dodaje se odstupanje susjeda od pravca između njegovih
// krajeva; rupi koja traje — promjena susjeda od zadnjeg mjerenja letve.
//
// Točnost je izmjerena na arhivi od 2015. tako da su na mjestima s mjerenjem
// izrezane rupe, rekonstruirane i uspoređene s izmjerenim (11. 10. 2026.);
// broj je granica unutar koje je 95 % grešaka, u zagradi zadržano zadnje
// mjerenje za rupu koja traje 10/24 h:
//
//	letva           prošla rupa (22–48 h)           rupa koja traje (10–24 h)
//	Szentborbás     Barcs ±8                         Barcs ±10           (±61)
//	Mohács          Bezdan ±2, Dunaszekcső ±3, Baja ±5  Dunaszekcső ±5, Baja ±12 (±49)
//	Dunaszekcső     Bezdan ±1, Baja ±2, Mohács ±2    Baja ±5, Mohács ±5  (±19/±46)
//	Siga            Bezdan ±1, Petreš ±2, Batina ±3  Bezdan ±4, Petreš ±7 (±16/±37)
//	Aljmaš          Petreš ±2, Bogojevo ±3           Petreš ±5           (±15/±36)
//	Mohovo          Sotin ±2, Ilok ±2                Sotin ±3, Ilok ±5   (±12/±27)
//	Vukovar         Bogojevo ±4, Sotin ±6            Bogojevo ±7         (±13/±30)
//	Mursko Središće Goričan ±4, Gornja Radgona ±5    Gornja Radgona ±8   (±17/±33)
//	Vízvár          Novo Virje ±10, Barcs ±15        Novo Virje ±12      (±53/±57)
//	Novo Virje      Vízvár ±10, Botovo ±16           Botovo ±18          (±54/±57)
//	Vrbovka         Moslavina ±8, Barcs ±11          Barcs ±15           (±30/±52)
//	Moslavina       Drávaszabolcs ±6, Vrbovka ±8     Vrbovka ±13, Barcs ±20 (±29/±55)
//
// Terezino Polje bi Szentborbásu bilo jednako dobro na živim podacima, ali je
// u arhivi nepouzdano; kad zastane Barcs, popuni ga ono s druge obale prije
// rekonstrukcije, a Batinu Bezdan, Goričan Letenye i Drávaszabolcs Donji
// Miholjac. Botovo vode elektrane, pa mu susjedi ne pomažu (±36 do ±42);
// Dalj, Sotin, Belišće i Osijek dobili bi tek malo prema zadržanom mjerenju.
//
// Kao i zamjena s druge obale, rekonstruirani sat ulazi samo u račun
// prognoze; mjerenjem se ne pokazuje i u evidenciju ne ide. Uz izdanje stoji
// napomena odakle je i koliko je točan.

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// Susjed je letva na istoj rijeci koja letvi popunjava rupu.
type Susjed struct {
	Letva string
	// Zastoj u satima: letva u satu t prati susjeda u satu t − Zastoj;
	// uzvodni je iznad nule, nizvodni ispod.
	Zastoj int64
	Prosla int // cm, 95 % grešaka za rupu s poznatim krajevima
	Traje  int // cm, isto za rupu koja traje; 0 kad za nju ne služi
}

// Rekonstrukcije su letve kojima rupu popunjavaju susjedi, redom po
// točnosti; za svaki sat uzima se prvi koji ga može dati.
var Rekonstrukcije = map[string][]Susjed{
	"szentborbas": {{Letva: "barcs", Zastoj: 4, Prosla: 8, Traje: 10}},
	"mohacs": {
		{Letva: "bezdan", Zastoj: -5, Prosla: 2},
		{Letva: "dunaszekcso", Prosla: 3, Traje: 5},
		{Letva: "baja", Prosla: 5, Traje: 12},
	},
	"dunaszekcso": {
		{Letva: "bezdan", Zastoj: -7, Prosla: 1},
		{Letva: "baja", Zastoj: 3, Prosla: 2, Traje: 5},
		{Letva: "mohacs", Prosla: 2, Traje: 5},
	},
	"siga": {
		{Letva: "bezdan", Zastoj: 2, Prosla: 1, Traje: 4},
		{Letva: "petres", Prosla: 2, Traje: 7},
		{Letva: "batina", Zastoj: 3, Prosla: 3, Traje: 7},
	},
	"aljmas": {
		{Letva: "petres", Prosla: 2, Traje: 5},
		{Letva: "bogojevo", Zastoj: -3, Prosla: 3},
	},
	"mohovo": {
		{Letva: "sotin", Zastoj: 2, Prosla: 2, Traje: 3},
		{Letva: "ilok", Zastoj: 1, Prosla: 2, Traje: 5},
	},
	"vukovar": {
		{Letva: "bogojevo", Zastoj: 2, Prosla: 4, Traje: 7},
		{Letva: "sotin", Prosla: 6},
	},
	"mursko-sredisce": {
		{Letva: "gorican", Zastoj: -6, Prosla: 4},
		{Letva: "gornja-radgona", Zastoj: 5, Prosla: 5, Traje: 8},
	},
	"vizvar": {
		{Letva: "novo-virje", Zastoj: 3, Prosla: 10, Traje: 12},
		{Letva: "barcs", Zastoj: -7, Prosla: 15},
	},
	"novo-virje": {
		{Letva: "vizvar", Zastoj: -3, Prosla: 10},
		{Letva: "botovo", Zastoj: 4, Prosla: 16, Traje: 18},
	},
	"vrbovka": {
		{Letva: "moslavina", Zastoj: -7, Prosla: 8},
		{Letva: "barcs", Zastoj: 6, Prosla: 11, Traje: 15},
	},
	"moslavina": {
		{Letva: "dravaszabolcs", Zastoj: -6, Prosla: 6},
		{Letva: "vrbovka", Zastoj: 7, Prosla: 8, Traje: 13},
		{Letva: "barcs", Zastoj: 12, Prosla: 15, Traje: 20},
	},
}

// Raspon su razine letve (cm) od..do.
type Raspon struct{ Od, Do int64 }

// RasponProvjere je raspon razina na kojem je rekonstrukcija letve
// provjerena: arhiva od 2015., bez 0,1 % najnižih i najviših vrijednosti.
// Odnos dviju letvi vrijedi samo ondje gdje je izmjeren
// (docs/rekonstrukcija-nizova.md); izvan njega rekonstrukcija i dalje računa,
// ali uz izdanje stoji upozorenje da točnost nije provjerena (odluka
// 11. 10. 2026.). Na rekordno niskoj vodi u listopadu 2026. ispod raspona su
// bile gotovo sve.
var RasponProvjere = map[string]Raspon{
	"szentborbas":     {-107, 569},
	"mohacs":          {-18, 854},
	"dunaszekcso":     {-8, 867},
	"siga":            {19, 1596},
	"aljmas":          {-91, 685},
	"mohovo":          {49, 692},
	"vukovar":         {-66, 982},
	"mursko-sredisce": {113, 429},
	"vizvar":          {199, 636},
	"novo-virje":      {-7, 421},
	"vrbovka":         {29, 608},
	"moslavina":       {-106, 468},
}

const (
	// RekonstrukcijaOd: rekonstruira se rupa dulja od toga. Kraću prognoza
	// premosti sama, bez upozorenja (ZaostatakKarike, DnevniIzSatnog).
	RekonstrukcijaOd = ZaostatakKarike
	// RekonstrukcijaProsla je najdulja rupa s poznatim krajevima (provjereno do 48 h).
	RekonstrukcijaProsla = 48
	// RekonstrukcijaTraje je koliko se sati iza zadnjeg mjerenja popunjava
	// (provjereno do 24 h); poslije toga ostaje upozorenje.
	RekonstrukcijaTraje = 24
	// RekonstrukcijaNapomena je koliko sati unatrag od izdanja rekonstruirana
	// rupa ima napomenu: toliko unatrag gleda dnevni model.
	RekonstrukcijaNapomena = 4 * 24
)

// Susjed stoji kad se u zadnjih StojiSati prije rupe nije pomaknuo više od
// StojiDopust, a letva se u isto vrijeme pomaknula barem SeMaknula — kao
// mjerila koja izostaju s uzdužnog profila. Takav susjed rupu ne popunjava.
const (
	StojiSati   = 144
	StojiDopust = 1
	SeMaknula   = 20
)

// RekonstruiranaRupa je rupa koju su popunili susjedi.
type RekonstruiranaRupa struct {
	Zadnje   int64    // zadnje mjerenje prije rupe
	Sljedece int64    // prvo mjerenje poslije; 0 dok rupa traje
	Do       int64    // zadnji popunjeni sat
	Sati     int      // koliko je sati popunjeno
	Iz       []Susjed // susjedi koji su dali sate, redom
	Tocnost  int      // cm, najslabija točnost među njima
	// Izvan: razina u rupi ili na njezinim krajevima izlazi iz raspona na
	// kojem je rekonstrukcija provjerena (Provjereno), pa točnost nije provjerena.
	Izvan      bool
	Provjereno Raspon
}

// dodaj bilježi sat t od susjeda.
func (r *RekonstruiranaRupa) dodaj(t int64, s Susjed, tocnost int) {
	r.Sati++
	r.Do = max(r.Do, t)
	r.Tocnost = max(r.Tocnost, tocnost)
	if !slices.ContainsFunc(r.Iz, func(x Susjed) bool { return x.Letva == s.Letva }) {
		r.Iz = append(r.Iz, s)
	}
}

// Opis je rečenica za napomenu; šifre letvi stoje kao zasebne riječi, da ih
// stranica zamijeni imenima.
func (r RekonstruiranaRupa) Opis() string {
	tocnost := fmt.Sprintf("na arhivi 95 %% unutar ±%d cm", r.Tocnost)
	if r.Izvan {
		tocnost = fmt.Sprintf("razina izvan raspona na kojem je provjereno (od %d do %d cm), pa točnost nije provjerena",
			r.Provjereno.Od, r.Provjereno.Do)
	}
	if r.Sljedece == 0 {
		return fmt.Sprintf("nema mjerenja od %s; %d h procijenjeno iz kretanja %s; %s",
			satDnevnika(r.Zadnje), r.Sati, opisSusjeda(r.Iz), tocnost)
	}
	sati := fmt.Sprintf("%d h", r.Sati)
	if rupa := int(r.Sljedece - r.Zadnje - 1); r.Sati < rupa {
		sati = fmt.Sprintf("%d od %d h", r.Sati, rupa)
	}
	return fmt.Sprintf("rupa od %s do %s (%s) procijenjena iz %s; %s",
		satDnevnika(r.Zadnje), satDnevnika(r.Sljedece), sati, opisSusjeda(r.Iz), tocnost)
}

// opisSusjeda: „letve barcs 4 h uzvodno” ili „letvi bezdan 5 h nizvodno i
// dunaszekcso u istom satu”; uz šifru nema interpunkcije, da je kartica letve
// zamijeni imenom.
func opisSusjeda(susjedi []Susjed) string {
	dijelovi := make([]string, len(susjedi))
	for i, s := range susjedi {
		dijelovi[i] = s.Letva + " " + opisZastoja(s.Zastoj)
	}
	if len(dijelovi) == 1 {
		return "letve " + dijelovi[0]
	}
	return "letvi " + strings.Join(dijelovi[:len(dijelovi)-1], ", ") + " i " + dijelovi[len(dijelovi)-1]
}

func opisZastoja(z int64) string {
	switch {
	case z > 0:
		return fmt.Sprintf("%d h uzvodno", z)
	case z < 0:
		return fmt.Sprintf("%d h nizvodno", -z)
	}
	return "u istom satu"
}

// Rekonstruiraj popunjava rupe letve iz susjeda (šifra → sati susjeda) i
// vraća što je popunila. Vlastito mjerenje se ne dira.
func Rekonstruiraj(letva map[int64]sirovoOcitanje, susjedi []Susjed, nizovi map[string]map[int64]sirovoOcitanje) []RekonstruiranaRupa {
	sati := make([]int64, 0, len(letva))
	for t, s := range letva {
		if s.cm.Valid || s.q.Valid {
			sati = append(sati, t)
		}
	}
	slices.Sort(sati)
	var out []RekonstruiranaRupa
	for i := 1; i < len(sati); i++ {
		if r, ok := popuniProslu(letva, sati[i-1], sati[i], susjedi, nizovi); ok {
			out = append(out, r)
		}
	}
	if len(sati) > 0 {
		if r, ok := popuniTraje(letva, sati[len(sati)-1], susjedi, nizovi); ok {
			out = append(out, r)
		}
	}
	return out
}

// popuniProslu popunjava rupu između mjerenja a i b.
func popuniProslu(letva map[int64]sirovoOcitanje, a, b int64, susjedi []Susjed, nizovi map[string]map[int64]sirovoOcitanje) (RekonstruiranaRupa, bool) {
	r := RekonstruiranaRupa{Zadnje: a, Sljedece: b}
	if rupa := b - a - 1; rupa <= RekonstrukcijaOd || rupa > RekonstrukcijaProsla || !letva[a].cm.Valid || !letva[b].cm.Valid {
		return r, false
	}
	ziv := zivi(letva, a, susjedi, nizovi)
	for t := a + 1; t < b; t++ {
		for _, s := range ziv {
			if v, ok := prosliSat(letva, a, b, t, s.Zastoj, nizovi[s.Letva]); ok {
				letva[t] = cmSat(v)
				r.dodaj(t, s, s.Prosla)
				break
			}
		}
	}
	return r, r.Sati > 0
}

// prosliSat: pravac između krajeva letve i odstupanje susjeda od pravca
// između njegovih krajeva, u satu pomaknutom za zastoj.
func prosliSat(letva map[int64]sirovoOcitanje, a, b, t, zastoj int64, susjed map[int64]sirovoOcitanje) (float64, bool) {
	na, nb, nt := susjed[a-zastoj].cm, susjed[b-zastoj].cm, susjed[t-zastoj].cm
	if !na.Valid || !nb.Valid || !nt.Valid {
		return 0, false
	}
	udio := float64(t-a) / float64(b-a)
	pravac := func(x, y int64) float64 { return float64(x) + float64(y-x)*udio }
	return pravac(letva[a].cm.Int64, letva[b].cm.Int64) + float64(nt.Int64) - pravac(na.Int64, nb.Int64), true
}

// popuniTraje popunjava sate iza zadnjeg mjerenja a, najviše
// RekonstrukcijaTraje, iz uzvodnih susjeda i nikad dalje od njihova zadnjeg
// mjerenja; popunjava tek kad bi rupa bila dulja od RekonstrukcijaOd.
func popuniTraje(letva map[int64]sirovoOcitanje, a int64, susjedi []Susjed, nizovi map[string]map[int64]sirovoOcitanje) (RekonstruiranaRupa, bool) {
	r := RekonstruiranaRupa{Zadnje: a}
	if !letva[a].cm.Valid {
		return r, false
	}
	var uzvodni []Susjed
	for _, s := range zivi(letva, a, susjedi, nizovi) {
		if s.Traje > 0 && s.Zastoj >= 0 {
			uzvodni = append(uzvodni, s)
		}
	}
	vrijednosti := map[int64]float64{}
	for t := a + 1; t <= a+RekonstrukcijaTraje; t++ {
		for _, s := range uzvodni {
			if v, ok := trajeSat(letva[a].cm.Int64, a, t, s.Zastoj, nizovi[s.Letva]); ok {
				vrijednosti[t] = v
				r.dodaj(t, s, s.Traje)
				break
			}
		}
	}
	if r.Sati <= RekonstrukcijaOd {
		return RekonstruiranaRupa{Zadnje: a}, false
	}
	for t, v := range vrijednosti {
		letva[t] = cmSat(v)
	}
	return r, true
}

// trajeSat: zadnje mjerenje letve i promjena susjeda od tada. Sat koji
// susjed još nije izmjerio je budućnost i ne popunjava se.
func trajeSat(zadnje, a, t, zastoj int64, susjed map[int64]sirovoOcitanje) (float64, bool) {
	n0, nt := susjed[a-zastoj].cm, susjed[t-zastoj].cm
	if !n0.Valid || !nt.Valid || t > zadnjiSat(susjed) {
		return 0, false
	}
	return float64(zadnje + nt.Int64 - n0.Int64), true
}

// zadnjiSat je zadnji sat s izmjerenim centimetrima.
func zadnjiSat(niz map[int64]sirovoOcitanje) int64 {
	var z int64
	for t, s := range niz {
		if s.cm.Valid && t > z {
			z = t
		}
	}
	return z
}

// zivi su susjedi koji pred rupom ne stoje.
func zivi(letva map[int64]sirovoOcitanje, a int64, susjedi []Susjed, nizovi map[string]map[int64]sirovoOcitanje) []Susjed {
	maknula := raspon(letva, a-StojiSati, a) >= SeMaknula
	out := make([]Susjed, 0, len(susjedi))
	for _, s := range susjedi {
		if maknula && raspon(nizovi[s.Letva], a-s.Zastoj-StojiSati, a-s.Zastoj) <= StojiDopust {
			continue
		}
		out = append(out, s)
	}
	return out
}

// raspon je razlika najvećeg i najmanjeg centimetra u satima od..do; bez
// ijednog mjerenja −1.
func raspon(niz map[int64]sirovoOcitanje, od, do int64) int64 {
	var vrijednosti []int64
	for t := od; t <= do; t++ {
		if s := niz[t]; s.cm.Valid {
			vrijednosti = append(vrijednosti, s.cm.Int64)
		}
	}
	if len(vrijednosti) == 0 {
		return -1
	}
	return slices.Max(vrijednosti) - slices.Min(vrijednosti)
}

func cmSat(v float64) sirovoOcitanje {
	return sirovoOcitanje{cm: sql.NullInt64{Int64: int64(math.Round(v)), Valid: true}}
}

// oznaciIzvan označava rupe u kojima letva, rekonstruirana ili na krajevima
// rupe, izlazi iz raspona provjere.
func oznaciIzvan(rupe []RekonstruiranaRupa, letva map[int64]sirovoOcitanje, raspon Raspon) {
	for i := range rupe {
		r := &rupe[i]
		r.Provjereno = raspon
		kraj := max(r.Do, r.Sljedece)
		for t := r.Zadnje; t <= kraj && !r.Izvan; t++ {
			if s := letva[t]; s.cm.Valid && (s.cm.Int64 < raspon.Od || s.cm.Int64 > raspon.Do) {
				r.Izvan = true
			}
		}
	}
}

// rekonstruiraj popunjava letvi rupe iz njezinih susjeda; susjedu prije toga
// sate popuni druga obala.
func (o *Osvjezivac) rekonstruiraj(ctx context.Context, letva string, sirovo map[int64]sirovoOcitanje, od time.Time) []RekonstruiranaRupa {
	susjedi, ima := Rekonstrukcije[letva]
	if !ima {
		return nil
	}
	nizovi := map[string]map[int64]sirovoOcitanje{}
	for _, s := range susjedi {
		n, err := o.ucitajSirovo(ctx, s.Letva, od)
		if err != nil {
			continue
		}
		o.dopuniDrugomObalom(ctx, s.Letva, n, od)
		nizovi[s.Letva] = n
	}
	rupe := Rekonstruiraj(sirovo, susjedi, nizovi)
	oznaciIzvan(rupe, sirovo, RasponProvjere[letva])
	return rupe
}

// dopuniDrugomObalom upisuje letvi sate koje ima samo letva s druge obale.
func (o *Osvjezivac) dopuniDrugomObalom(ctx context.Context, letva string, sirovo map[int64]sirovoOcitanje, od time.Time) {
	druga, ima := DrugaObala(letva)
	if !ima {
		return
	}
	if drugo, err := o.ucitajSirovo(ctx, druga, od); err == nil {
		PopuniSirovo(sirovo, drugo)
	}
}

// popuniRupe popunjava sate koji letvi nedostaju: prvo s druge obale, pa
// dulje rupe iz susjeda. Samo za račun prognoze.
func (o *Osvjezivac) popuniRupe(ctx context.Context, letva string, sirovo map[int64]sirovoOcitanje, od time.Time) []RekonstruiranaRupa {
	o.dopuniDrugomObalom(ctx, letva, sirovo, od)
	return o.rekonstruiraj(ctx, letva, sirovo, od)
}

// napomeneRekonstrukcije bilježi u ishod rupe koje su susjedi popunili u
// zadnja četiri dana prije izdanja, a u izbor njihov opis za karticu letve.
func (o *Osvjezivac) napomeneRekonstrukcije(ctx context.Context, nizovi map[Izvor]Niz, ishod *Ishod, od time.Time) {
	letve := map[string]bool{}
	for iz := range nizovi {
		letve[iz.Letva] = true
	}
	for letva := range letve {
		rupe := o.rekonstrukcijaDo(ctx, letva, od, ishod.Sada)
		if len(rupe) == 0 {
			continue
		}
		if ishod.Rekonstruirano == nil {
			ishod.Rekonstruirano = map[string][]RekonstruiranaRupa{}
		}
		ishod.Rekonstruirano[letva] = rupe
		ishod.Izbor["rekonstrukcija:"+letva] = Izbor{Opis: OpisRekonstrukcije(rupe)}
	}
}

// OpisRekonstrukcije je opis rupa jedne letve, redom.
func OpisRekonstrukcije(rupe []RekonstruiranaRupa) string {
	opisi := make([]string, len(rupe))
	for i, r := range rupe {
		opisi[i] = r.Opis()
	}
	return "rekonstrukcija: " + strings.Join(opisi, "; ")
}

// rekonstrukcijaDo su rupe letve koje su susjedi popunili, a sežu u zadnja
// četiri dana prije sada, redom.
func (o *Osvjezivac) rekonstrukcijaDo(ctx context.Context, letva string, od time.Time, sada int64) []RekonstruiranaRupa {
	if _, ima := Rekonstrukcije[letva]; !ima {
		return nil
	}
	sirovo, err := o.ucitajSirovo(ctx, letva, od)
	if err != nil {
		return nil
	}
	var out []RekonstruiranaRupa
	for _, r := range o.popuniRupe(ctx, letva, sirovo, od) {
		if r.Do >= sada-RekonstrukcijaNapomena && r.Zadnje < sada {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b RekonstruiranaRupa) int { return cmp.Compare(a.Zadnje, b.Zadnje) })
	return out
}
