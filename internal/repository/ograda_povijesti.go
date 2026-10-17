package repository

import (
	"slices"
	"time"
)

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

// PraviloCuvanjaOcitanja je, za razmjenu (peers.Service.PostaviPraviloPrihvata),
// pravilo po kojem KeepVersion odbija: koliko mjeseci tuđih očitanja čvor
// prima (0: sva) i letve koje prati u cijelosti, poredane. Razmjena ga
// sprema uz zapis rupa i po njemu, i nakon ponovnog pokretanja, vidi da je
// pravilo prošireno (dulje razdoblje, nova praćena letva), pa traži ono što
// je dotad odbijala.
func PraviloCuvanjaOcitanja() (mjeseci int, pracene []string) {
	p := readingPolicy.Load()
	if p == nil {
		return 0, nil
	}
	for letva, prati := range p.Followed {
		if prati {
			pracene = append(pracene, letva)
		}
	}
	slices.Sort(pracene)
	return max(p.Months, 0), pracene
}
