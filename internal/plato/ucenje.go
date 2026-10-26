package plato

import (
	"context"
	"database/sql"
	"math"
	"slices"
	"time"
)

// Što je za letvu normalno uči se iz satnog spojenog niza arhive od
// OdGodine, po godinama, a od godišnjih vrijednosti uzima se medijan:
// godina u kojoj je mjerilo dugo bilo u kvaru tako ne pomakne granicu (isto
// kao prag skoka u paketu skok).
const (
	// OdGodine: od 2012. satni nizovi postoje za gotovo sve letve.
	OdGodine = 2012

	// NajkraceTrajanje je najkraće trajanje iste vrijednosti koje se može
	// smatrati zaglavljenim uređajem.
	NajkraceTrajanje = 6 * time.Hour
	// ZadanoTrajanje vrijedi za letvu bez arhive (radari na potocima): pola
	// dana. Orešac u kvaru stoji na −372 cm po 11 do 30 sati.
	ZadanoTrajanje = 12 * time.Hour
	// PercentilTrajanja nizova iste vrijednosti u godini: 99,9. Najdulji niz
	// nije dobar jer je i u arhivi kvarova (Orahovica 1496 h u 2013.).
	PercentilTrajanja = 0.999
	// RezervaTrajanja množi medijan godišnjih percentila: niz iste
	// vrijednosti dulji od toga u normalnom radu se gotovo ne događa.
	RezervaTrajanja = 1.5
	// NajmanjeNizova u godini da godina da svoj percentil.
	NajmanjeNizova = 500

	// NajmanjeVrijednosti u godini da godina da svoj raspon.
	NajmanjeVrijednosti = 2000
	// DonjiPercentil i GornjiPercentil razina u godini.
	DonjiPercentil  = 0.001
	GornjiPercentil = 0.999
	// NiskiPercentil: niska voda letve (Raspon.Nisko) je medijan godišnjih
	// 10. percentila; samo tako niska zaglavljena vrijednost može biti dno
	// mjerila.
	NiskiPercentil = 0.10
	// NajmanjaSirina raspona: letvi kojoj voda godišnje ide manje od metra
	// „daleko” je ipak barem metar dalje.
	NajmanjaSirina = 100.0

	// MirnoOd: za prag susjeda gledaju se nizovi iste vrijednosti letve
	// od barem šest sati.
	MirnoOd = 6 * time.Hour
	// NajmanjeMirnih nizova s očitanjima susjeda za prag susjeda.
	NajmanjeMirnih = 20
	// PercentilSusjeda pomaka susjeda dok letva miruje.
	PercentilSusjeda = 0.99
	// RezervaSusjeda množi percentil, kao Rezerva praga skoka.
	RezervaSusjeda = 3.0
	// NajmanjiPomakSusjeda: susjed potvrđuje zaglavljen uređaj tek kad se
	// pomaknuo više od 20 cm; manje je valić, vjetar, zaokruživanje, pa i
	// obična promjena mirne vode kroz dan-dva.
	NajmanjiPomakSusjeda = 20.0
)

// BezPraga je prag susjeda kad arhiva para nema dovoljno zajedničkih
// mirnih nizova (radari na potocima, strane letve bez arhive): ne zna se
// kako dvije letve idu zajedno, pa pomak takvog susjeda ne potvrđuje
// zaglavljen uređaj. Na živim očitanjima 2026. radar Orešca u kvaru inače
// bi „potvrdio” da miruje Pivnica Slatinska, potok koji je doista bio suh.
var BezPraga = math.Inf(1)

// godina je godina trenutka po UTC-u.
func godina(t time.Time) int { return t.UTC().Year() }

// TrajanjeIzNiza je najdulje normalno trajanje iste vrijednosti letve:
// medijan godišnjih 99,9. percentila trajanja nizova iste vrijednosti
// (unutar 1 cm), puta RezervaTrajanja, na sat naviše, a najmanje
// NajkraceTrajanje. Bez nijedne godine s dovoljno nizova ZadanoTrajanje.
//
// Naučeno 25. 10. 2026.: Batina 29 h, Vukovar 66 h, Donji Miholjac 26 h,
// Botovo 26 h, Donja Dubrava 35 h, a potoci koji pri maloj vodi stoje po
// tjedan i više dulje (Ludbreg 212 h, Orahovica 479 h).
func TrajanjeIzNiza(t []Tocka) time.Duration {
	po := map[int][]float64{}
	for i := 0; i < len(t); {
		j := krajNiza(t, i)
		g := godina(t[i].Kad)
		po[g] = append(po[g], t[j].Kad.Sub(t[i].Kad).Hours())
		i = j + 1
	}
	var god []float64
	for _, d := range po {
		if len(d) >= NajmanjeNizova {
			god = append(god, percentil(d, PercentilTrajanja))
		}
	}
	if len(god) == 0 {
		return ZadanoTrajanje
	}
	h := math.Ceil(RezervaTrajanja * medijan(god))
	return max(NajkraceTrajanje, time.Duration(h)*time.Hour)
}

// RasponIzNiza je granica „daleko izvan uobičajenog”: od godišnjih 0,1. i
// 99,9. percentila razine medijani D i G, a daleko je ispod D ili iznad G
// za još jednu širinu G − D (najmanje NajmanjaSirina). Velika voda koja
// premaši uobičajeni godišnji vrh za cijeli raspon gotovo se ne događa
// (Batina: D −31, G 481, daleko iznad 993 cm; najviši u arhivi 772), a
// kvar je redovito daleko izvan (Donja Dubrava 1385 cm, iznad 700). Nil kad
// nijedna godina nema dovoljno vrijednosti.
func RasponIzNiza(t []Tocka) *Raspon {
	po := map[int][]float64{}
	for _, x := range t {
		po[godina(x.Kad)] = append(po[godina(x.Kad)], x.Cm)
	}
	var lo, hi, nisko []float64
	najniza := math.Inf(1)
	for _, v := range po {
		najniza = math.Min(najniza, slices.Min(v))
		if len(v) >= NajmanjeVrijednosti {
			lo, hi = append(lo, percentil(v, DonjiPercentil)), append(hi, percentil(v, GornjiPercentil))
			nisko = append(nisko, percentil(v, NiskiPercentil))
		}
	}
	if len(lo) == 0 {
		return nil
	}
	d, g := medijan(lo), medijan(hi)
	s := math.Max(g-d, NajmanjaSirina)
	return &Raspon{Od: d - s, Do: g + s, Najniza: najniza, Nisko: medijan(nisko)}
}

// PragSusjeda je pomak susjeda koji potvrđuje zaglavljen uređaj letve:
// koliko se susjed pomakne dok letva normalno miruje (nizovi iste
// vrijednosti od barem MirnoOd), 99. percentil puta RezervaSusjeda, na cm
// naviše, a najmanje NajmanjiPomakSusjeda. Pomak susjeda tako ne potvrđuje
// kvar ondje gdje letve ne idu zajedno: Donja Dubrava ispod brane stoji
// na najmanjem protoku dok se Botovo, ispod odvodnog kanala elektrane,
// diže i spušta 30 cm na dan (prag 96 cm); Siga i Bezdan na istom Dunavu
// idu zajedno (prag 20 cm). Bez dovoljno mirnih nizova BezPraga.
func PragSusjeda(letva, susjed []Tocka) float64 {
	var m []float64
	for i := 0; i < len(letva); {
		j := krajNiza(letva, i)
		if letva[j].Kad.Sub(letva[i].Kad) >= MirnoOd {
			if p, ima := PomakU(susjed, nil, letva[i].Kad, letva[j].Kad); ima {
				m = append(m, p)
			}
		}
		i = j + 1
	}
	if len(m) < NajmanjeMirnih {
		return BezPraga
	}
	return math.Max(NajmanjiPomakSusjeda, math.Ceil(RezervaSusjeda*percentil(m, PercentilSusjeda)))
}

// percentil p (0..1) vrijednosti (ne praznih), najbliži rang naviše.
func percentil(v []float64, p float64) float64 {
	s := slices.Sorted(slices.Values(v))
	k := int(math.Ceil(p*float64(len(s)))) - 1
	return s[min(max(k, 0), len(s)-1)]
}

// NizIzArhive je satni spojeni vodostaj letve u arhivi od OdGodine, na
// pune sate, po vremenu.
func NizIzArhive(ctx context.Context, arhiva *sql.DB, letva string) ([]Tocka, error) {
	od := time.Date(OdGodine, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	rows, err := arhiva.QueryContext(ctx, `SELECT vrijeme, vrijednost FROM spoj
		WHERE letva = ? AND velicina = 'vodostaj' AND korak = 'satni' AND vrijeme >= ? AND vrijeme % 3600 = 0
		ORDER BY vrijeme`, letva, od)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tocka
	for rows.Next() {
		var t int64
		var v float64
		if err := rows.Scan(&t, &v); err != nil {
			return nil, err
		}
		out = append(out, Tocka{Kad: time.Unix(t, 0).UTC(), Cm: v})
	}
	return out, rows.Err()
}
