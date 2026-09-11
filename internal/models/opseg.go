package models

// Opseg je doseg dnevnika — ono na što se pravo pisanja odnosi.
//
// Dnevnik usluge vodi se po branjenom području, dnevnik COP-a po centru.
// Centar sektorskog COP-a pokriva sva područja u sektoru i nijedno nije
// "njegovo", pa se pravo pisanja ne može izvesti iz jednog područja: piše
// tko vodi sektor, ali i tko vodi bilo koje područje u njemu. Kad je obrana
// proglašena na više područja odjednom vodi je sektor, a dežurni s područja
// upisuju u isti taj dnevnik — netko iz Osijeka, netko iz Virovitice.
type Opseg struct {
	Sektor   string
	Podrucje int   // 0 kad dnevnik nije vezan na jedno područje
	Podrucja []int // sva područja u dosegu; dežurni s bilo kojeg piše u isti dnevnik
}

// OpsegPodrucja je doseg dnevnika koji se vodi po jednom branjenom području.
func OpsegPodrucja(a Area) Opseg {
	return Opseg{Sektor: a.SectorID, Podrucje: a.ID, Podrucja: []int{a.ID}}
}

// OpsegDnevnika izvodi doseg iz dnevnika: COP po centru, usluga po području.
// Za sektorski COP u doseg ulaze sva područja sektora (iz sva); za COP jednog
// područja samo to područje — sektor piše i ondje, jer mu je nadređen.
func OpsegDnevnika(j Journal, podrucje *Area, sva []Area) Opseg {
	if j.CentarSektor == "" {
		if podrucje == nil {
			return Opseg{}
		}
		return OpsegPodrucja(*podrucje)
	}
	o := Opseg{Sektor: j.CentarSektor}
	if j.CentarPodrucje != nil && *j.CentarPodrucje > 0 {
		o.Podrucje = *j.CentarPodrucje
		o.Podrucja = []int{o.Podrucje}
		return o
	}
	for _, a := range sva {
		if a.SectorID == j.CentarSektor {
			o.Podrucja = append(o.Podrucja, a.ID)
		}
	}
	return o
}

// Prazan javlja da doseg nije određen — nitko u njemu ne piše.
func (o Opseg) Prazan() bool { return o.Sektor == "" && o.Podrucje == 0 && len(o.Podrucja) == 0 }
