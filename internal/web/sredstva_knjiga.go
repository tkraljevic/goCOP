package web

import "net/url"

// filtriKnjige su filtri knjige prometa koji su sad odabrani, imenima
// parametara koje čitaju stranica i njezin Excel
func (d MtsPageData) filtriKnjige() url.Values {
	q := url.Values{}
	postavi := func(ime, v string) {
		if v != "" {
			q.Set(ime, v)
		}
	}
	postavi("sektor", d.Sektor)
	if d.Skladiste != nil {
		postavi("skladiste", d.Skladiste.ID)
	}
	postavi("vrsta", d.VrstaID)
	postavi("od", d.Od)
	postavi("do", d.Do)
	return q
}

// URLKnjige je knjiga prometa s jednim filtrom promijenjenim (prazna
// vrijednost ga briše), a ostali ostaju kakvi jesu
func (d MtsPageData) URLKnjige(ime, vrijednost string) string {
	q := d.filtriKnjige()
	q.Del(ime)
	if vrijednost != "" {
		q.Set(ime, vrijednost)
	}
	return adresaSIzborom("/sredstva/promet", q)
}

// ExcelKnjige je Excel iste knjige, po istim filtrima
func (d MtsPageData) ExcelKnjige() string {
	return adresaSIzborom("/sredstva/promet.xlsx", d.filtriKnjige())
}

// NazivVrsteKnjige je naziv odabranog sredstva za čip filtra
func (d MtsPageData) NazivVrsteKnjige() string {
	for _, v := range d.Vrste {
		if v.ID == d.VrstaID {
			return v.Naziv
		}
	}
	return "sva"
}

// DatumKnjige je čip dana od kojeg ili do kojeg knjiga prometa ide
type DatumKnjige struct {
	Ime, Natpis, Vrijednost string
	Podaci                  MtsPageData
}

// DatumiKnjige su čipovi „Od” i „Do” s trenutnim izborom
func (d MtsPageData) DatumiKnjige() []DatumKnjige {
	return []DatumKnjige{{"od", "Od", d.Od, d}, {"do", "Do", d.Do, d}}
}
