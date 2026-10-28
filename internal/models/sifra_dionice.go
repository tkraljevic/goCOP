package models

import (
	"sort"
	"strconv"
)

// ManjaSifraDionice uspoređuje šifre dionica kako ih čovjek čita: brojevi
// unutar šifre po vrijednosti, pa B.34.2 dolazi prije B.34.10, a B.15.1
// prije B.34.1. Slova i ostali znakovi uspoređuju se kao tekst.
func ManjaSifraDionice(a, b string) bool {
	for a != "" && b != "" {
		da, ra := komadSifre(a)
		db, rb := komadSifre(b)
		if da != db {
			na, ea := strconv.Atoi(da)
			nb, eb := strconv.Atoi(db)
			if ea == nil && eb == nil && na != nb {
				return na < nb
			}
			if ea != nil || eb != nil {
				return da < db
			}
			// isti broj, drukčiji zapis (npr. 07 i 7): kraći ide prije
			return len(da) < len(db)
		}
		a, b = ra, rb
	}
	return len(a) < len(b)
}

// komadSifre odvaja s početka šifre niz znamenki ili jedan drugi znak
func komadSifre(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		i = 1
	}
	return s[:i], s[i:]
}

// SloziSifreDionica slaže šifre dionica po brojevima, na mjestu
func SloziSifreDionica(sifre []string) {
	sort.SliceStable(sifre, func(i, j int) bool { return ManjaSifraDionice(sifre[i], sifre[j]) })
}
