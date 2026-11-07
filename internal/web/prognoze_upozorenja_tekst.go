package web

import (
	"slices"
	"strconv"
	"strings"
)

// Rečenice upozorenja koje dežurni čita u žurbi. Izdavač opis zapisuje šiframa
// i on putuje s izdanjem, pa se ovdje piše običnim riječima, i za već izdane
// prognoze.

const (
	uvodRezerve       = "rezerva: "
	uvodBezUlaza      = "bez svježeg ulaza ("
	opisVrhaBezDnevne = "nema budućnosti vrha ("

	// bezSatne je što znači „drži se zadnje mjerenje”: sama letva tada nema satnu
	// prognozu, a zadnje mjerenje je samo pretpostavka za letve koje je čitaju.
	bezSatne = "Ova letva zato nema satnu prognozu, a letve nizvodno računaju se iz njezina zadnjeg mjerenja"
)

// putRacuna piše opis izdavača „rezerva: a + b umjesto c + d” ili „bez svježeg
// ulaza (a + b): drži se zadnje mjerenje”. Opis drugog oblika ne dira.
func (p pisacUpozorenja) putRacuna(opis string) (string, bool) {
	if s, jest := strings.CutPrefix(opis, uvodRezerve); jest {
		uzeti, glavni, ima := strings.Cut(s, " umjesto ")
		if !ima || praznoIme(ulaziOpisa(uzeti)) || praznoIme(ulaziOpisa(glavni)) {
			return "", false
		}
		return p.izRezerve(ulaziOpisa(uzeti), ulaziOpisa(glavni)), true
	}
	if s, jest := strings.CutPrefix(opis, uvodBezUlaza); jest {
		ulazi, _, ima := strings.Cut(s, ")")
		if !ima || praznoIme(ulaziOpisa(ulazi)) {
			return "", false
		}
		return nabrojiIli(p.imenaUlaza(ulaziOpisa(ulazi))) + " nema svježih mjerenja. " + bezSatne, true
	}
	return "", false
}

// izRezerve kaže koji je ulaz ispao i iz čega se letva sad računa. Rezerva se
// uzima tek kad glavnom putu neki ulaz nije svjež, a svi njezini jesu, pa je
// taj ulaz među onima koje rezerva nema. Opis nosi samo imena letvi: dva puta
// mogu imati istu letvu u drugoj veličini, a tada se ispali ulaz ne vidi. Kad
// su sve letve iste, ne imenuje se nijedna; zapis modela s dijelom istih letvi
// mogao bi imenovati krivu, a računi iz koda tu letvu drže u istoj veličini.
func (p pisacUpozorenja) izRezerve(uzeti, glavni []string) string {
	ispali := slices.DeleteFunc(slices.Clone(glavni), func(l string) bool { return slices.Contains(uzeti, l) })
	iz := "iz letve "
	if len(uzeti) > 1 {
		iz = "iz letvi "
	}
	iz += nabroji(p.imenaUlaza(uzeti))
	if len(ispali) == 0 {
		return "računa se " + iz + ", a to je rezerva"
	}
	return nabrojiIli(p.imenaUlaza(ispali)) + " nema svježih mjerenja, pa se računa " + iz
}

// stoVrha piše vrh lanca kojem dnevni model nije dao prognozu.
func (p pisacUpozorenja) stoVrha(opis string) string {
	if strings.HasPrefix(opis, opisVrhaBezDnevne) {
		return "prva je letva u lancu, a dnevni model za nju nije dao prognozu. " + bezSatne
	}
	return "prva je letva u lancu: " + p.imena(opis)
}

// praznoIme javlja da opis izdavača nije potpun, pa ostaje kakav je napisan.
func praznoIme(sifre []string) bool { return slices.Contains(sifre, "") }

// ulaziOpisa razdvaja šifre ulaza iz opisa izdavača.
func ulaziOpisa(s string) []string { return strings.Split(s, " + ") }

// imenaUlaza piše šifre ulaza nazivima.
func (p pisacUpozorenja) imenaUlaza(sifre []string) []string {
	out := make([]string, len(sifre))
	for i, k := range sifre {
		out[i] = p.ime(k)
	}
	return out
}

// nabrojiIli spaja riječi zarezima, a zadnju s „ili”.
func nabrojiIli(s []string) string {
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " ili " + s[len(s)-1]
}

// zaDoseg je kad prognoza može postati manje točna: 0 je sat izdanja.
func zaDoseg(d int) string {
	if d == 0 {
		return "od sata izdanja"
	}
	return "za " + strconv.Itoa(d) + " h"
}

// najranijeOd je zaDoseg kao donja granica: stvarno može stići i kasnije.
func najranijeOd(d int) string {
	if d == 0 {
		return zaDoseg(d)
	}
	return "najranije " + zaDoseg(d)
}

// stanjeDnevne kaže što je s dnevnom prognozom letve.
type stanjeDnevne int

const (
	dnevnaBezOznake stanjeDnevne = iota
	dnevnaManjeTocna
	dnevnaNijeIzdana
)

// glavaUpitnog je početak rečenice na kartici i natpisu grafa: −1 znači da
// satna nije zahvaćena. Prazno kad nije zahvaćeno ništa.
func glavaUpitnog(od int, dnevna stanjeDnevne) string {
	switch {
	case od >= 0 && dnevna == dnevnaNijeIzdana:
		return "Satna prognoza može biti manje točna " + najranijeOd(od) + ", a dnevna nije izdana"
	case od >= 0 && dnevna == dnevnaManjeTocna:
		return "Satna i dnevna prognoza mogu biti manje točne, satna " + najranijeOd(od)
	case od >= 0:
		return "Prognoza može biti manje točna " + najranijeOd(od)
	case dnevna == dnevnaNijeIzdana:
		return "Dnevna prognoza nije izdana"
	case dnevna == dnevnaManjeTocna:
		return "Dnevna prognoza može biti manje točna"
	}
	return ""
}

// recenicaDnevne slaže rečenicu o dnevnoj i prenesenoj dnevnoj prognozi; prazno
// kad nema nijedne.
func recenicaDnevne(ciljevi, prenesene []string, oCiljevima, oPrenesenima, nastavak string) string {
	switch {
	case len(ciljevi) > 0 && len(prenesene) > 0:
		return oCiljevima + nabroji(ciljevi) + ", a " + nastavak + " za " + nabroji(prenesene) + "."
	case len(ciljevi) > 0:
		return oCiljevima + nabroji(ciljevi) + "."
	case len(prenesene) > 0:
		return oPrenesenima + nabroji(prenesene) + "."
	}
	return ""
}
