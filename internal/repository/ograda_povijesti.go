package repository

import "time"

// StanjeOgradePovijesti je, za razmjenu (peers.Service.PostaviStanjePrihvata),
// ono o čemu ovisi što KeepVersion odbija: narastaj pravila, koji je drugi
// sa svakim SetReadingHistoryPolicy, i rub razdoblja. Očitanje izmjereno
// prije ruba odbija se, osim za praćenu letvu; bez razdoblja rub je nula i
// ne odbija se ništa. Oboje dolazi iz istog postavljanja pravila. Razmjena
// po narastaju zna da je pravilo promijenjeno, a po rubu da je sat vraćen ili
// da je AddDate na kraju mjeseca otišao unatrag: tada bi moglo proći što je
// prije bilo odbijeno, pa ono što je zapamtila da je odbila više ne vrijedi.
func StanjeOgradePovijesti() (narastaj uint64, rub time.Time) {
	p := readingPolicy.Load()
	if p == nil {
		return 0, time.Time{}
	}
	if p.Months > 0 {
		rub = time.Now().AddDate(0, -p.Months, 0)
	}
	return p.narastaj, rub
}
