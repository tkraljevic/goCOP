// HIS istu veličinu zna dati na dva načina: kao popis redaka i kao ispis za
// čitanje, u kojem su dani redci a mjeseci stupci. Ispis dolazi s nastavkom
// .txt i izgleda ovako:
//
//	2018      I     II    III     IV      V     VI    VII   VIII ...
//	   1                               20.6   37.5   38.6   27.7 ...
//
// Vrijednosti su poravnate desno prema nazivu mjeseca, a ne razdvojene
// razmakom istog broja, pa se čitaju po stupcima. Ispod dana stoje sažeci
// (NK, SK, VK, Ekstrem), koji nisu mjerenja i preskaču se.
package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reGodinaIspisa = regexp.MustCompile(`^\s+((?:19|20)\d{2})\s+I\s+II\s+III\b`)
	reDanIspisa    = regexp.MustCompile(`^\s{0,12}(\d{1,2})\s`)
	mjeseciIspisa  = []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII"}
)

// citajIspisDnevni čita dnevne vrijednosti iz ispisa po mjesecima. Vraća
// prazno kad datoteka nije takav ispis, pa pozivatelj može pokušati drugačije.
func citajIspisDnevni(redci []string) []Vrijednost {
	var out []Vrijednost
	godina := 0
	var kraj []int // stupac u kojem završava vrijednost svakog mjeseca
	for _, r := range redci {
		if m := reGodinaIspisa.FindStringSubmatch(r); m != nil {
			godina, _ = strconv.Atoi(m[1])
			kraj = krajeviMjeseci(r)
			continue
		}
		if godina == 0 || len(kraj) != 12 {
			continue
		}
		m := reDanIspisa.FindStringSubmatch(r)
		if m == nil {
			// sažeci ispod dana počinju slovom, pa ovdje ispadaju sami
			continue
		}
		dan, _ := strconv.Atoi(m[1])
		if dan < 1 || dan > 31 {
			continue
		}
		pocetak := len(m[0])
		for i, k := range kraj {
			v := isjecak(r, pocetak, k)
			pocetak = k
			if v == "" || !decimalan(v) {
				continue
			}
			mjesec := time.Month(i + 1)
			kad := time.Date(godina, mjesec, dan, 0, 0, 0, 0, time.UTC)
			if kad.Day() != dan || kad.Month() != mjesec {
				continue // 31. u mjesecu koji ga nema
			}
			out = append(out, Vrijednost{Kad: kad, Dan: true, V: strings.Replace(v, ".", ",", 1)})
		}
	}
	poredajPoVremenu(out)
	return out
}

// imaIspisPoMjesecima javlja je li datoteka ispis za čitanje. Satni ispis
// ima dane u redcima i sate u stupcima; njega ne čitamo, jer se iz praznog
// ispisa ne vidi počinje li stupac na ponoći ili na jedan sat, a pogrešno
// poravnanje pomaknulo bi cijeli niz.
func imaIspisPoMjesecima(redci []string) bool {
	for _, r := range redci {
		if reGodinaIspisa.MatchString(r) || strings.HasPrefix(strings.TrimSpace(r), "1.  1.") {
			return true
		}
	}
	return false
}

// krajeviMjeseci nalazi na kojem stupcu završava naziv svakog mjeseca; do tog
// stupca poravnate su i vrijednosti ispod njega.
func krajeviMjeseci(zaglavlje string) []int {
	var out []int
	od := 0
	for _, mj := range mjeseciIspisa {
		i := nadjiRijec(zaglavlje, mj, od)
		if i < 0 {
			return nil
		}
		od = i + len(mj)
		out = append(out, od)
	}
	return out
}

// nadjiRijec traži riječ omeđenu razmacima, da III ne pogodi u sredini VIII.
func nadjiRijec(s, rijec string, od int) int {
	for i := od; i+len(rijec) <= len(s); i++ {
		if s[i:i+len(rijec)] != rijec {
			continue
		}
		if i > 0 && s[i-1] != ' ' {
			continue
		}
		if i+len(rijec) < len(s) && s[i+len(rijec)] != ' ' {
			continue
		}
		return i
	}
	return -1
}

// isjecak vadi vrijednost iz stupca; redak zna biti kraći od zaglavlja kad
// zadnji mjeseci nemaju podatak.
func isjecak(r string, od, do int) string {
	if od >= len(r) {
		return ""
	}
	if do > len(r) {
		do = len(r)
	}
	if od < 0 || do <= od {
		return ""
	}
	return strings.TrimSpace(r[od:do])
}

func poredajPoVremenu(v []Vrijednost) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j].Kad.Before(v[j-1].Kad); j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
