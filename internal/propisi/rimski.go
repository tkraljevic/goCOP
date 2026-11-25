package propisi

import "strings"

// rimskeZnamenke su vrijednosti rimskih brojeva, od najveće.
var rimskeZnamenke = []struct {
	vrijednost int
	znak       string
}{
	{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"},
	{100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"},
	{10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"},
}

// Rimski piše broj od 1 do 3999 rimskim brojem velikim slovima (23 je
// „XXIII”). Za broj izvan toga vraća prazno.
func Rimski(n int) string {
	if n < 1 || n > 3999 {
		return ""
	}
	var b strings.Builder
	for _, z := range rimskeZnamenke {
		for n >= z.vrijednost {
			b.WriteString(z.znak)
			n -= z.vrijednost
		}
	}
	return b.String()
}

// vrijednostZnaka su vrijednosti pojedinih rimskih znakova.
var vrijednostZnaka = map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}

// IzRimskog čita rimski broj od 1 do 3999, napisan samo velikim ili samo
// malim slovima, bez točke. Prihvaća samo pravilan zapis: „IIII”, „VX” i
// „IC” nisu rimski brojevi.
func IzRimskog(s string) (int, bool) {
	veliko := strings.ToUpper(s)
	if s != veliko && s != strings.ToLower(s) {
		return 0, false
	}
	n := 0
	for i := 0; i < len(veliko); i++ {
		v, ok := vrijednostZnaka[veliko[i]]
		if !ok {
			return 0, false
		}
		if i+1 < len(veliko) && v < vrijednostZnaka[veliko[i+1]] {
			v = -v
		}
		n += v
	}
	if n < 1 || Rimski(n) != veliko {
		return 0, false
	}
	return n, true
}
