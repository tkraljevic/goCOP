package web

// Inventura (crtež 19b, 8 i 8a): sažetak iznad dokumenta, uz skupinu koliko
// je redaka u njoj, a u obrascu razlika i zbroj razlika dok se upisuje.

// SazetakPopisa su brojke iznad dokumenta inventure
type SazetakPopisa struct {
	SKolicinom, Manjak, Visak int
}

// SazetakPopisa broji retke s prebrojanom količinom, s manjkom i s viškom
func (d MtsPageData) SazetakPopisa() SazetakPopisa {
	var s SazetakPopisa
	if d.Popis == nil {
		return s
	}
	for _, st := range d.Popis.Stavke {
		if st.Utvrdjeno != 0 {
			s.SKolicinom++
		}
		switch r := st.Razlika(); {
		case r < 0:
			s.Manjak++
		case r > 0:
			s.Visak++
		}
	}
	return s
}

// BrojIma je koliko redaka skupine ima knjižno ili prebrojano
func (g GrupaStavki) BrojIma() int {
	n := 0
	for _, st := range g.Stavke {
		if st.Knjizno != 0 || st.Utvrdjeno != 0 {
			n++
		}
	}
	return n
}

// BrojUKnjizi je koliko redaka skupine ima u knjizi, za obrazac inventure
func (g GrupaStavki) BrojUKnjizi() int {
	n := 0
	for _, st := range g.Stavke {
		if st.Knjizno != 0 {
			n++
		}
	}
	return n
}
