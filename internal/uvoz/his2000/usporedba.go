// Usporedba zatečenog niza s novim izvozom. Stoji uz čitanje jer je odgovor
// na isto pitanje: smije li ovaj izvoz zamijeniti ono što već imamo. Traže je
// i naredba u terminalu i uvoz iz preglednika, pa ne živi ni u jednoj od njih.
package his2000

import (
	"strconv"
	"strings"
	"time"
)

// Usporedi javlja koliko se vrijednosti razlikuje i koliko ih zatečeni niz
// ima, a novi izvoz nema. Uspoređuje se broj, ne zapis: 1919,000 i 1919 isto
// su mjerenje, a razlikuju se samo po tome koliko je decimala izvoz ispisao.
func Usporedi(stari map[time.Time]string, novi []Vrijednost) (sukoba, samoStari, nepostojeci int) {
	imaNovi := make(map[time.Time]bool, len(novi))
	for _, v := range novi {
		imaNovi[v.Kad] = true
		if s, ok := stari[v.Kad]; ok && !istiBroj(s, v.V) {
			sukoba++
		}
	}
	for k := range stari {
		if imaNovi[k] {
			continue
		}
		// sat koji u našoj zoni ne postoji nismo ni htjeli: zatečena datoteka
		// ga ima jer je nastala prije nego što se to znalo, pa njegov izostanak
		// nije gubitak nego ispravak
		if NepostojeciSat(k) {
			nepostojeci++
			continue
		}
		samoStari++
	}
	return sukoba, samoStari, nepostojeci
}

// istiBroj javlja govore li dva zapisa isti broj; kad se ijedan ne čita kao
// broj, ostaje usporedba zapisa.
func istiBroj(a, b string) bool {
	if a == b {
		return true
	}
	x, err1 := strconv.ParseFloat(strings.Replace(strings.TrimSpace(a), ",", ".", 1), 64)
	y, err2 := strconv.ParseFloat(strings.Replace(strings.TrimSpace(b), ",", ".", 1), 64)
	if err1 != nil || err2 != nil {
		return false
	}
	return x == y
}
