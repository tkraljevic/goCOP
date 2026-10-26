package web

import (
	"fmt"
	"math"
	"sort"
	"time"

	"gocop/internal/models"
)

// Nula uzdužnog profila je medijan vode svake letve, ali istih dana na svim
// letvama jednog toka. Kad bi svaka letva uzela svojih zadnjih 30 dana, letva
// čiji niz u programu počinje kasnije (naše letve preuzimamo s
// vodostaji.voda.hr tek od 18.–20. 9. 2026.) imala bi nulu samo iz niske
// vode, a mađarska, kojoj imamo cijeli mjesec, i iz vala sredinom rujna — pa
// bi mađarska na mirnoj vodi izgledala kao da je propala (Szentborbás 9. 10.
// −20 cm prema −5 na Terezinom Polju).
//
// S profila ispadaju i letve koje bi crtale grbu koja nije voda:
//   - mjerilo koje stoji dok se susjedi miču: na rekordno niskoj vodi 2026.
//     Siga je od 25. 9. pokazivala točno 19 cm, Petreš od 26. 9. 25–26 cm, a
//     Batina je u isto vrijeme pala 70 cm — voda je ispod mjerila;
//   - letva koja nakon dužeg prekida mjerenja nastavi na drugoj razini
//     (Moslavina prije prekida 3.–8. 10. −95, poslije −127 cm, a susjedi
//     mirni): nula joj se računa samo od prekida, a dok nema dovoljno novih
//     dana, ne crta se.
// Ispod crteža piše koje su letve izostavljene i zašto.

// StojiNajmanjeSati je koliko se sati mjerilo ne smije pomaknuti da bi se
// smatralo da stoji. Tri dana nisu dovoljna: na rekordno niskoj vodi 9. 10.
// 2026. Aljmaš, Dalj, Sotin i Mohovo stajali su četiri dana unutar centimetra
// (Dunav pod usporom), a mjerila su bila ispravna.
const StojiNajmanjeSati = 144

// StojiDopust je koliko se mjerilo koje stoji smije pomaknuti (cm): Petreš je
// „stajao” između 25 i 26 cm.
const StojiDopust = 1.0

// SusjedSeMaknuo je koliko se susjedna letva u istom razdoblju mora pomaknuti
// (cm) da bi mirno mjerilo bilo kvar, a ne mirna voda. Uz Sigu je Batina u
// dva tjedna pala 70 cm; grba manja od dvadesetak centimetara ne kvari crtež.
const SusjedSeMaknuo = 20.0

// PrekidSati je najkraći prekid mjerenja nakon kojega se gleda je li letva
// nastavila na drugoj razini.
const PrekidSati = 48

// SkokNakonPrekida je razlika (cm) između dana prije i dana poslije prekida
// od koje se nula letve računa samo iz mjerenja poslije prekida.
const SkokNakonPrekida = 10.0

// NulaNajmanjeDana je koliko dana unatrag letva mora imati mjerenja da bi
// ušla u zajedničku nulu toka. Kraći niz skratio bi nulu svima na toku.
const NulaNajmanjeDana = 14

// NulaProfila su nule letvi profila i razlozi zbog kojih neke letve nisu na
// crtežu.
type NulaProfila struct {
	Cm     map[string]float64   // letva → nula (cm)
	Razlog map[string]string    // letva → zašto nije na profilu
	Voda   map[string]string    // letva → tok
	Rkm    map[string]float64   // letva → riječni kilometar, za redoslijed napomene
	Naziv  map[string]string    // letva → ime za napomenu
	Od     map[string]time.Time // tok → prvi dan zajedničke nule
	Dan    time.Time            // dan jutra; nula seže do njegove ponoći
}

// satiNiza vraća sate niza uzlazno.
func satiNiza(niz map[int64]float64) []int64 {
	s := make([]int64, 0, len(niz))
	for h := range niz {
		s = append(s, h)
	}
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s
}

// stojiOd vraća od kojega sata mjerilo stoji: koliko unatrag od zadnjeg
// mjerenja sve vrijednosti ostaju unutar StojiDopust od zadnje. ok je kad to
// traje barem StojiNajmanjeSati.
func stojiOd(niz map[int64]float64) (od int64, cm float64, ok bool) {
	sati := satiNiza(niz)
	if len(sati) == 0 {
		return 0, 0, false
	}
	zadnji := sati[len(sati)-1]
	cm, od = niz[zadnji], zadnji
	for i := len(sati) - 1; i >= 0 && math.Abs(niz[sati[i]]-cm) <= StojiDopust; i-- {
		od = sati[i]
	}
	return od, cm, zadnji-od >= StojiNajmanjeSati
}

// rasponNiza je razlika najveće i najmanje vrijednosti u satima [od, do].
func rasponNiza(niz map[int64]float64, od, do int64) (float64, bool) {
	najm, najv, ima := math.Inf(1), math.Inf(-1), false
	for h, v := range niz {
		if h >= od && h <= do {
			najm, najv, ima = math.Min(najm, v), math.Max(najv, v), true
		}
	}
	return najv - najm, ima
}

// medijanOd je medijan vrijednosti od sata od do sata do (bez njega).
func medijanOd(niz map[int64]float64, od, do int64) (float64, bool) {
	var v []float64
	for h, x := range niz {
		if h >= od && h < do {
			v = append(v, x)
		}
	}
	if len(v) == 0 {
		return 0, false
	}
	sort.Float64s(v)
	m := v[len(v)/2]
	if len(v)%2 == 0 {
		m = (v[len(v)/2-1] + m) / 2
	}
	return m, true
}

// Prekid je zadnji dulji prekid mjerenja letve nakon kojega je letva
// nastavila na drugoj razini.
type Prekid struct {
	Od, Do int64   // zadnji sat prije i prvi poslije prekida
	Skok   float64 // cm, dan poslije prema danu prije
}

// zadnjiPrekid nalazi zadnji prekid od barem PrekidSati nakon kojega se
// srednja voda dana prije i dana poslije razlikuje za više od SkokNakonPrekida.
func zadnjiPrekid(niz map[int64]float64) (Prekid, bool) {
	sati := satiNiza(niz)
	for i := len(sati) - 1; i > 0; i-- {
		prije, poslije := sati[i-1], sati[i]
		if poslije-prije < PrekidSati {
			continue
		}
		a, okA := medijanOd(niz, prije-23, prije+1)
		b, okB := medijanOd(niz, poslije, poslije+24)
		if okA && okB && math.Abs(b-a) > SkokNakonPrekida {
			return Prekid{Od: prije, Do: poslije, Skok: b - a}, true
		}
		return Prekid{}, false
	}
	return Prekid{}, false
}

// odSata piše sat od epohe kao lokalni dan: „25. 9.”.
func odSata(h int64) string {
	t := time.Unix(h*3600, 0).In(models.Zagreb)
	return fmt.Sprintf("%d. %d.", t.Day(), int(t.Month()))
}

// susjediMiruju kaže jesu li najbliže letve istog toka (do dvije sa svake
// strane, koje i same ne stoje) u satima [od, do] mirne. Kad nijedna nema
// mjerenja u tom razdoblju, smatra se da miruju: mirna voda nije kvar.
func susjediMiruju(redom []string, i int, nizovi map[string]map[int64]float64, stoje map[string]bool, od, do int64) bool {
	for _, smjer := range []int{-1, 1} {
		nadjeno := 0
		for j := i + smjer; j >= 0 && j < len(redom) && nadjeno < 2; j += smjer {
			if stoje[redom[j]] {
				continue
			}
			if r, ima := rasponNiza(nizovi[redom[j]], od, do); ima {
				nadjeno++
				if r >= SusjedSeMaknuo {
					return false
				}
			}
		}
	}
	return true
}

// sloziNulu računa nule letvi profila iz satnih nizova zadnjih 30 dana.
// akumulacije zadržavaju svoju nulu (razina uz branu nije voda rijeke i u
// krivulju ne ulazi).
func sloziNulu(nizovi map[string]map[int64]float64, n NulaProfila, akumulacije map[string]bool) NulaProfila {
	n.Cm, n.Razlog, n.Od = map[string]float64{}, map[string]string{}, map[string]time.Time{}
	danH := n.Dan.Unix() / 3600
	poVodi := map[string][]string{}
	rijecni := map[string]map[int64]float64{}
	for l, niz := range nizovi {
		if len(niz) == 0 {
			continue
		}
		if akumulacije[l] {
			if m, ok := medijanSati(niz); ok {
				n.Cm[l] = m
			}
			continue
		}
		rijecni[l] = niz
		poVodi[n.Voda[l]] = append(poVodi[n.Voda[l]], l)
	}
	for voda, letve := range poVodi {
		sort.Slice(letve, func(i, j int) bool { return n.Rkm[letve[i]] > n.Rkm[letve[j]] })
		n.nulaToka(voda, letve, rijecni, danH)
	}
	return n
}

// nulaToka računa nule letvi jednog toka (letve redom niz tok).
func (n *NulaProfila) nulaToka(voda string, letve []string, nizovi map[string]map[int64]float64, danH int64) {
	stoje := map[string]bool{}
	for _, l := range letve {
		if _, _, ok := stojiOd(nizovi[l]); ok {
			stoje[l] = true
		}
	}
	prvi := map[string]int64{}
	for i, l := range letve {
		niz := nizovi[l]
		if od, cm, ok := stojiOd(niz); ok && !susjediMiruju(letve, i, nizovi, stoje, od, satiNiza(niz)[len(niz)-1]) {
			n.Razlog[l] = fmt.Sprintf("mjerni uređaj stoji na %.0f cm od %s, a susjedne letve se miču", cm, odSata(od))
			continue
		}
		pocetak := satiNiza(niz)[0]
		if p, ok := zadnjiPrekid(niz); ok {
			pocetak = p.Do
			if p.Do > danH-NulaNajmanjeDana*24 {
				n.Razlog[l] = fmt.Sprintf("nakon prekida mjerenja %s–%s vodostaj je %+.0f cm drukčiji; nula čeka %d dana novih mjerenja",
					odSata(p.Od), odSata(p.Do), p.Skok, NulaNajmanjeDana)
				continue
			}
		}
		if pocetak > danH-NulaNajmanjeDana*24 {
			n.Razlog[l] = fmt.Sprintf("mjerenja tek od %s; nula čeka %d dana mjerenja", odSata(pocetak), NulaNajmanjeDana)
			continue
		}
		prvi[l] = pocetak
	}
	var zajednicki int64
	for _, h := range prvi {
		zajednicki = max(zajednicki, h)
	}
	n.Od[voda] = time.Unix(zajednicki*3600, 0).In(models.Zagreb)
	for l := range prvi {
		dio := map[int64]float64{}
		for h, v := range nizovi[l] {
			if h >= zajednicki {
				dio[h] = v
			}
		}
		if m, ok := medijanSati(dio); ok {
			n.Cm[l] = m
		} else {
			n.Razlog[l] = "premalo mjerenja za nulu"
		}
	}
}

// NulaKratko je razdoblje nule toka za natpise: „zadnjih 30 dana” ili, dok
// zajednički niz toka još nije mjesec dana, „od 20. 9.”.
func (n NulaProfila) NulaKratko(voda string) string {
	od, ima := n.Od[voda]
	if !ima || od.IsZero() || !od.After(n.Dan.AddDate(0, 0, -MjesecDana+1)) {
		return fmt.Sprintf("zadnjih %d dana", MjesecDana)
	}
	return fmt.Sprintf("od %d. %d.", od.Day(), int(od.Month()))
}

// opisi upisuje u profil toka razdoblje nule i letve koje su s njega
// izostavljene, redom niz tok.
func (n NulaProfila) opisi(p *UzduzniProfil, voda string) {
	p.NulaKratko = n.NulaKratko(voda)
	var letve []string
	for l := range n.Razlog {
		if n.Voda[l] == voda {
			letve = append(letve, l)
		}
	}
	sort.Slice(letve, func(i, j int) bool { return n.Rkm[letve[i]] > n.Rkm[letve[j]] })
	for _, l := range letve {
		p.Izostavljeno = append(p.Izostavljeno, n.Naziv[l]+" — "+n.Razlog[l])
	}
}
