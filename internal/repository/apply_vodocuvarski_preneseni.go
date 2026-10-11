package repository

import "gocop/internal/models"

// List prenesen iz ranije evidencije (BP16) i list s drugog čvora za isti
// dan (Odluke (8. 10. 2026.), pitanje 7). Oznaka lista izvedena je iz osobe
// i dana i za prenesen list, pa uvoz na jednom čvoru i list koji je drugi
// čvor napisao za isti dan pišu isti zapis. Prenesen list zaključen je
// prijenosom (models.VodocuvarskiList.Zakljucen) i pri slaganju iz povijesti
// vrijedi kao predan: nacrt s drugog čvora i brisanje nacrta ga ne
// zamjenjuju, a bilješke rukovoditelja i prijave s nacrta ostaju na njemu.
// Predan list ima prednost i pred prenesenim, a obilasci (zadaci s
// fotografijama) koje je uvoz prenio ostaju na listu koji vrijedi, kao kad
// uvoz na istom čvoru upiše obilazak na postojeći list dana.

// prednost verzije pri slaganju lista: predan list pred prenesenim, a
// prenesen pred nacrtom
const (
	prednostNacrta = iota
	prednostPrenesenog
	prednostPredanog
)

// prednostVerzije je prednost lista pri slaganju iz povijesti
func prednostVerzije(l models.VodocuvarskiList) int {
	switch {
	case l.Predan():
		return prednostPredanog
	case l.Rekonstrukcija:
		return prednostPrenesenog
	}
	return prednostNacrta
}

// prenesen javlja je li list prenesen iz ranije evidencije, a nije predan
func prenesen(l models.VodocuvarskiList) bool {
	return prednostVerzije(l) == prednostPrenesenog
}

// smijePokriti javlja smije li kasnija verzija w pokriti verziju v: predan
// list pokriva samo verzija istog potpisa (ovjera, parafa, upis na predan
// list), a prenesen samo prenesen list koji nosi sve njegove obilaske (upis
// na prenesen list, kasniji uvoz istog dana). Nacrt i brisanje nacrta
// zaključen list ne pokrivaju; nacrt smije pokriti svaka verzija.
func smijePokriti(w, v models.VodocuvarskiList) bool {
	switch prednostVerzije(v) {
	case prednostPredanog:
		return istiPotpis(w, v)
	case prednostPrenesenog:
		return prenesen(w) && imaSve(w.Zadaci, v.Zadaci, oznakaZadatka) && imaSve(w.Prilozi, v.Prilozi, oznakaPriloga)
	}
	return true
}

// obilasciPrenesenih su zadaci i prilozi lista l uz one iz prenesenih
// verzija koje vrijede, a kojih na listu nema
func obilasciPrenesenih(l models.VodocuvarskiList, zive []verzijaLista) ([]models.ZadatakNaListu, []models.PrilogLista) {
	zadaci, prilozi := l.Zadaci, l.Prilozi
	for _, v := range zive {
		if prenesen(v.list) {
			zadaci = dodajNove(zadaci, v.list.Zadaci, oznakaZadatka)
			prilozi = dodajNove(prilozi, v.list.Prilozi, oznakaPriloga)
		}
	}
	return zadaci, prilozi
}

func oznakaZadatka(z models.ZadatakNaListu) string { return z.ID }

func oznakaPriloga(p models.PrilogLista) string { return p.ID }

// imaSve javlja ima li popis sve stavke drugog popisa, po oznaci
func imaSve[T any](popis, drugi []T, oznaka func(T) string) bool {
	ima := oznake(popis, oznaka)
	for _, x := range drugi {
		if !ima[oznaka(x)] {
			return false
		}
	}
	return true
}

// dodajNove dodaje na kraj popisa stavke drugog popisa čije oznake na njemu
// nema, redom kojim dolaze; kad ništa nije dodano, vraća isti popis. Popis se
// ne mijenja na mjestu, jer ga dijeli verzija iz koje je list složen.
func dodajNove[T any](popis, drugi []T, oznaka func(T) string) []T {
	ima := oznake(popis, oznaka)
	var nove []T
	for _, x := range drugi {
		if k := oznaka(x); !ima[k] {
			ima[k] = true
			nove = append(nove, x)
		}
	}
	if len(nove) == 0 {
		return popis
	}
	return append(append([]T(nil), popis...), nove...)
}

// oznake su oznake stavki popisa
func oznake[T any](popis []T, oznaka func(T) string) map[string]bool {
	ima := make(map[string]bool, len(popis))
	for _, x := range popis {
		ima[oznaka(x)] = true
	}
	return ima
}
