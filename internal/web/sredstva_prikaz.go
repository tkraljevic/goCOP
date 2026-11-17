package web

// Čipovi prikaza iznad tablica Sredstava (crteži 19a i 19b; plan, novo
// ponašanje 1): danas se uvijek prikazuje cijeli katalog, i retci bez
// ičega. Čip sakrije retke bez sadržaja; broj uz čip kaže koliko ih ostaje.
// Retci nose data-ima i data-razlika, a skriva ih CSS po data-prikaz
// okvira (sredstva-prikaz.js).

// CipPrikaza je jedan izbor: vrijednost za data-prikaz, natpis i broj redaka
type CipPrikaza struct {
	Vrijednost, Natpis string
	Broj               int
}

// PrikazRedaka su čipovi jedne tablice i izbor s kojim se otvara
type PrikazRedaka struct {
	Cilj   string // id okvira tablice
	Zadano string
	Cipovi []CipPrikaza
}

// prikazRedaka otvara tablicu na zadanom čipu, osim kad u njemu nema
// nijednog retka: tada na zadnjem, koji pokazuje sve
func prikazRedaka(cilj, zadano string, cipovi ...CipPrikaza) PrikazRedaka {
	p := PrikazRedaka{Cilj: cilj, Zadano: cipovi[len(cipovi)-1].Vrijednost, Cipovi: cipovi}
	for _, c := range cipovi {
		if c.Vrijednost == zadano && c.Broj > 0 {
			p.Zadano = zadano
		}
	}
	return p
}

// PrikazStanja su čipovi „ima / sve vrste” na pregledu i skladištu
func (d MtsPageData) PrikazStanja() PrikazRedaka {
	ima := 0
	for _, sv := range d.Stanje {
		if sv.Ima() {
			ima++
		}
	}
	return prikazRedaka("sr-stanje", "ima", CipPrikaza{"ima", "ima", ima}, CipPrikaza{"sve", "sve vrste", len(d.Stanje)})
}

// PrikazPopisa su čipovi „samo razlike / ima / sve” na dokumentu inventure
func (d MtsPageData) PrikazPopisa() PrikazRedaka {
	razlike, ima, sve := 0, 0, 0
	if d.Popis != nil {
		for _, st := range d.Popis.Stavke {
			sve++
			if st.Razlika() != 0 {
				razlike++
			}
			if st.Knjizno != 0 || st.Utvrdjeno != 0 {
				ima++
			}
		}
	}
	return prikazRedaka("sr-stavke", "ima", CipPrikaza{"razlike", "samo razlike", razlike}, CipPrikaza{"ima", "ima", ima}, CipPrikaza{"sve", "sve", sve})
}

// PrikazObrascaPopisa su čipovi „ima u knjizi / svi redci” u obrascu
// inventure; skriveni redci se i dalje spremaju
func (d MtsPageData) PrikazObrascaPopisa() PrikazRedaka {
	ima, sve := 0, 0
	if d.Popis != nil {
		for _, st := range d.Popis.Stavke {
			sve++
			if st.Knjizno != 0 {
				ima++
			}
		}
	}
	return prikazRedaka("sr-stavke", "ima", CipPrikaza{"ima", "ima u knjizi", ima}, CipPrikaza{"sve", "svi redci", sve})
}

// PrikazPotreba su čipovi „s potrebom / sve vrste” u obrascu potreba
func (d MtsPageData) PrikazPotreba() PrikazRedaka {
	ima := 0
	for _, sv := range d.Stanje {
		if d.Potrebe[sv.Vrsta.ID].Kolicina > 0 {
			ima++
		}
	}
	return prikazRedaka("sr-potrebe", "ima", CipPrikaza{"ima", "s potrebom", ima}, CipPrikaza{"sve", "sve vrste", len(d.Stanje)})
}
