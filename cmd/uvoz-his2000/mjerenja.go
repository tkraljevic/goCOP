// Vodomjerenja: pojedinačna mjerenja protoka na letvi. HIS ih daje kao
// radnu tablicu u SYLK obliku, s nastavkom .xls, ali to nije Excel nego
// tekst: redci C;K… nose vrijednost ćelije, a Y i X red i stupac.
//
// Njima se krivulje ne postavljaju — to radi DHMZ — ali služe za provjeru i
// za razdoblja u kojima službene krivulje nema.
package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Mjerenje je jedno vodomjerenje: kad, na kojem vodostaju i koliki protok.
type Mjerenje struct {
	Datum    time.Time
	Vodostaj string // cm, cijeli broj
	Brzina   string // srednja brzina, m/s
	Protok   string // m3/s
	Metoda   string // ADCP, krilo, iz komentara mjeritelja
}

var reCelija = regexp.MustCompile(`^C;(?:.*?;)??K(.*)$`)
var rePolozaj = regexp.MustCompile(`[YX](\d+)`)

// JeSylk javlja je li datoteka radna tablica u SYLK obliku.
func JeSylk(sirovo []byte) bool {
	return strings.HasPrefix(string(sirovo), "ID;P")
}

// ProcitajMjerenja razlaže SYLK tablicu vodomjerenja. Stupci se traže po
// naslovu, jer im redoslijed nije zajamčen.
func ProcitajMjerenja(sirovo []byte) ([]Mjerenje, error) {
	celije := map[[2]int]string{}
	red, stupac := 0, 0
	najveciRed := 0
	for _, r := range razloziRedke(sirovo) {
		if strings.HasPrefix(r, "F;") || strings.HasPrefix(r, "C;") {
			for _, m := range rePolozaj.FindAllString(r, -1) {
				n, _ := strconv.Atoi(m[1:])
				if m[0] == 'Y' {
					red = n
				} else {
					stupac = n
				}
			}
		}
		if !strings.HasPrefix(r, "C;") {
			continue
		}
		m := reCelija.FindStringSubmatch(r)
		if m == nil {
			continue
		}
		celije[[2]int{red, stupac}] = strings.Trim(m[1], `"`)
		if red > najveciRed {
			najveciRed = red
		}
	}
	stupci := map[string]int{}
	for p, v := range celije {
		if p[0] != 1 {
			continue
		}
		stupci[strings.ToLower(strings.TrimSpace(v))] = p[1]
	}
	datum, imaDatum := stupci["datum mjerenja"]
	protok, imaProtok := stupci["protok"]
	if !imaDatum || !imaProtok {
		return nil, fmt.Errorf("tablica nema stupce „Datum mjerenja” i „Protok”")
	}
	var out []Mjerenje
	for y := 2; y <= najveciRed; y++ {
		dan, err := izSerijskog(celije[[2]int{y, datum}])
		if err != nil {
			continue
		}
		q := celije[[2]int{y, protok}]
		if strings.TrimSpace(q) == "" {
			continue
		}
		out = append(out, Mjerenje{
			Datum:    dan,
			Vodostaj: zaokruzi(celije[[2]int{y, stupci["vodostaj"]}], 0),
			Brzina:   zaokruzi(celije[[2]int{y, stupci["srednja brzina"]}], 2),
			Protok:   zaokruzi(q, 1),
			Metoda:   strings.TrimSpace(celije[[2]int{y, stupci["komentar"]}]),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("u tablici nema nijednog mjerenja")
	}
	return out, nil
}

// izSerijskog pretvara datum kakav radne tablice pamte: broj dana od
// 30.12.1899. Godina 1900 ondje je pogrešno prijestupna, pa brojevi manji od
// 61 ne bi bili pouzdani; vodomjerenja su ionako iz ovog stoljeća.
func izSerijskog(s string) (time.Time, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 61 {
		return time.Time{}, fmt.Errorf("%q nije datum radne tablice", s)
	}
	return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n), nil
}

// zaokruzi ispisuje broj na zadani broj decimala, s zarezom, jer radna
// tablica pamti punu preciznost stroja: 0,839999973773956 je 0,84.
func zaokruzi(s string, decimala int) string {
	f, err := strconv.ParseFloat(strings.Replace(strings.TrimSpace(s), ",", ".", 1), 64)
	if err != nil {
		return ""
	}
	return strings.Replace(strconv.FormatFloat(f, 'f', decimala, 64), ".", ",", 1)
}
