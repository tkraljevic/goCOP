package repository

import "gocop/internal/models"

// Brisanje nacrta i bilješka s drugog čvora (Odluke (8. 10. 2026.),
// pitanje 7). Brisanje nacrta odnosi ono što je čvor koji briše znao, a
// bilješke rukovoditelja iz druge verzije ne gube se. Kad drugi čvor na
// kopiju istog nacrta upiše bilješku, a vodočuvar nacrt obriše, verzija s
// bilješkom ne nestaje (brisanje za bilješku nije znalo), ali ni
// vodočuvarov tekst koji je brisanje vidjelo ne vraća se: od nje ostaje
// nacrt kakav nastane rukovoditeljevim „Upiši”, s bilješkama i prijavama
// za koje brisanje nije znalo.

// ostatakNakonBrisanja je i-ta verzija bez onoga što su kasnija brisanja
// nacrta znala (brisanjeZnaTekst, bezPoznatog); javlja ostaje li išta od
// nje. Verzija koju nijedno brisanje nije vidjelo ostaje kakva jest.
func ostatakNakonBrisanja(verzije []verzijaLista, i int) (verzijaLista, bool) {
	v, smanjena := verzije[i], false
	for _, w := range verzije[i+1:] {
		if brisanjeZnaTekst(w, verzije[i]) {
			v.list, smanjena = bezPoznatog(v.list, w.list), true
		}
	}
	return v, !smanjena || len(v.list.Upisi)+len(v.list.Prijave) > 0
}

// brisanjeZnaTekst javlja je li w brisanje nacrta koje je vidjelo
// vodočuvarov tekst nacrta v: v ga ima, a nije noviji od teksta u brisanju.
// Zaključen list (predan, prenesen) brisanje nacrta ne dira.
func brisanjeZnaTekst(w, v verzijaLista) bool {
	return w.arhivirana && prednostVerzije(w.list) == prednostNacrta && prednostVerzije(v.list) == prednostNacrta &&
		v.list.NapisanoAt != nil && !v.list.NapisanoAt.After(napisano(w.list))
}

// bezPoznatog je nacrt v bez vodočuvarova teksta (prilike, naredbe, opis,
// zapažanja, stanje zadataka i prilozi) i bez bilješki i prijava koje ima
// brisanje w. Ostaju oznaka, broj i radno vrijeme lista i zadaci, otvoreni,
// kao na nacrtu koji nastane upisom rukovoditelja.
func bezPoznatog(v, w models.VodocuvarskiList) models.VodocuvarskiList {
	v.Prilike, v.Naredbe, v.Opis, v.Zapazanja, v.NapisanoAt, v.Prilozi = "", "", "", "", nil, nil
	v.Zadaci = otvoreniZadaci(v.Zadaci)
	v.Upisi = bezPoznatih(v.Upisi, w.Upisi, kljucUpisa)
	v.Prijave = bezPoznatih(v.Prijave, w.Prijave, oznakaPrijave)
	return v
}

// otvoreniZadaci su zadaci lista bez vodočuvarova stanja: otvoreni, kako
// su zadani
func otvoreniZadaci(zadaci []models.ZadatakNaListu) []models.ZadatakNaListu {
	var out []models.ZadatakNaListu
	for _, z := range zadaci {
		out = append(out, models.ZadatakNaListu{ID: z.ID, Tekst: z.Tekst, Zadao: z.Zadao, ZadanoAt: z.ZadanoAt, Status: models.ZadatakOtvoren})
	}
	return out
}

func oznakaPrijave(p models.PrijavaNaListu) string { return p.ID }

// bezPoznatih su stavke popisa čijih oznaka među poznatima nema, redom
func bezPoznatih[T any](popis, poznati []T, oznaka func(T) string) []T {
	ima := oznake(poznati, oznaka)
	var out []T
	for _, x := range popis {
		if !ima[oznaka(x)] {
			out = append(out, x)
		}
	}
	return out
}
