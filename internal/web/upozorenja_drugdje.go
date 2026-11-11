package web

import (
	"cmp"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"gocop/internal/dhmz"
	"gocop/internal/models"
)

// Upozorenja DHMZ-a drugdje u Hrvatskoj (goCOP-m3, 13a · Prognoze, odjeljak
// „Vrijeme · upozorenja drugdje u Hrvatskoj”, i 11a · kartica Vrijeme).
// DHMZ isto upozorenje izdaje za svako područje i svako razdoblje posebno,
// pa je jedno područje jedan redak. Broj je zbroj po područjima, isti na
// Naslovnoj i na Prognozama › Vrijeme. Klik na županiju mijenja mjesto, a
// pomorsko područje otvara svoja upozorenja ispod retka (Odluke
// (11. 11. 2026.)).

// UpozorenjaDrugdje su upozorenja izvan županije mjesta, po područjima.
type UpozorenjaDrugdje struct {
	Podrucja []PodrucjeUpozorenja
	Ukupno   int    // zbroj po područjima
	Razina   int    // najviša: 2 žuto, 3 narančasto, 4 crveno
	Najvise  string // „najviše narančasto: vjetar”
	Izvor    string // „DHMZ, 07:30”
	Otvoreno bool   // popis je otvoren (dolazak s Naslovne)
}

// PodrucjeUpozorenja je jedan redak popisa: županija ili pomorsko područje.
type PodrucjeUpozorenja struct {
	Naziv      string // „Ličko-senjska”, „Velebitski kanal”
	Razina     int
	Broj       int
	Sazetak    string              // „narančasto: vjetar · žuto: kiša, vjetar”
	URL        string              // mjesto u toj županiji; prazno za pomorsko područje
	Upozorenja []UpozorenjeVremena // pomorsko područje: otvaraju se ispod retka
}

// Naslov: „Drugdje u Hrvatskoj · 18 upozorenja u 7 područja”
func (d UpozorenjaDrugdje) Naslov() string {
	return d.NaslovUsko() + " u " + uPodrucja(len(d.Podrucja))
}

// NaslovUsko je naslov za mobitel: „Drugdje u Hrvatskoj · 18 upozorenja”
func (d UpozorenjaDrugdje) NaslovUsko() string {
	return "Drugdje u Hrvatskoj · " + brojUpozorenjaRijecima(d.Ukupno)
}

// BrojRijecima: „1 upozorenje”, „6 upozorenja”
func (p PodrucjeUpozorenja) BrojRijecima() string { return brojUpozorenjaRijecima(p.Broj) }

func brojUpozorenjaRijecima(n int) string {
	return strconv.Itoa(n) + " " + models.UzBrojHR(n, "upozorenje", "upozorenja", "upozorenja")
}

// uPodrucja: „1 području”, „7 područja”
func uPodrucja(n int) string {
	return strconv.Itoa(n) + " " + models.UzBrojHR(n, "području", "područja", "područja")
}

// upozorenjaDrugdje slaže upozorenja drugih područja po područjima: najviša
// razina prva, pa ono s više upozorenja. Bez upozorenja vraća nil.
func upozorenjaDrugdje(ostala []dhmz.Upozorenje, sada time.Time) *UpozorenjaDrugdje {
	type kljuc struct {
		dogadjaj string
		razina   int
		od, do   int64
	}
	var redoslijed []string
	po := map[string][]dhmz.Upozorenje{}
	vidjeno := map[string]map[kljuc]bool{}
	for _, u := range ostala {
		ime := strings.TrimSpace(u.Podrucje)
		if u.Razina < 2 || ime == "" {
			continue
		}
		if vidjeno[ime] == nil {
			vidjeno[ime] = map[kljuc]bool{}
			redoslijed = append(redoslijed, ime)
		}
		k := kljuc{u.Dogadjaj, u.Razina, u.Od.Unix(), u.Do.Unix()}
		if !vidjeno[ime][k] {
			vidjeno[ime][k] = true
			po[ime] = append(po[ime], u)
		}
	}
	if len(redoslijed) == 0 {
		return nil
	}
	d := &UpozorenjaDrugdje{}
	for _, ime := range redoslijed {
		p := podrucjeUpozorenja(ime, po[ime], sada)
		d.Podrucja = append(d.Podrucja, p)
		d.Ukupno += p.Broj
		d.Razina = max(d.Razina, p.Razina)
	}
	slices.SortStableFunc(d.Podrucja, func(a, b PodrucjeUpozorenja) int {
		return cmp.Or(cmp.Compare(b.Razina, a.Razina), cmp.Compare(b.Broj, a.Broj))
	})
	d.Najvise = "najviše " + razineUpozorenja[d.Razina] + ": " + strings.Join(pojaveRazine(ostala, d.Razina), ", ")
	return d
}

// podrucjeUpozorenja je redak jednog područja; županija vodi na svoje mjesto
func podrucjeUpozorenja(ime string, uu []dhmz.Upozorenje, sada time.Time) PodrucjeUpozorenja {
	p := PodrucjeUpozorenja{Naziv: ime, Broj: len(uu)}
	var dijelovi []string
	for razina := 4; razina >= 2; razina-- {
		if pojave := pojaveRazine(uu, razina); len(pojave) > 0 {
			p.Razina = max(p.Razina, razina)
			dijelovi = append(dijelovi, razineUpozorenja[razina]+": "+strings.Join(pojave, ", "))
		}
	}
	p.Sazetak = strings.Join(dijelovi, " · ")
	if z, ok := zupanijaUpozorenja(uu[0]); ok {
		p.URL = "/prognoze/vrijeme?" + url.Values{"zupanija": {z.Oznaka}}.Encode()
		return p
	}
	for _, u := range uu {
		p.Upozorenja = append(p.Upozorenja, upozorenjeVremena(u, sada))
	}
	return p
}

// pojaveRazine su pojave upozorenja jedne razine, bez ponavljanja
func pojaveRazine(uu []dhmz.Upozorenje, razina int) []string {
	var out []string
	for _, u := range uu {
		if p := pojavaUpozorenja(u.Dogadjaj); u.Razina == razina && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// pojavaUpozorenja je pojava iz naziva upozorenja: „Žuto upozorenje za
// kišu” → „kiša”. Pojava koju DHMZ piše drukčije ostaje kakva jest.
func pojavaUpozorenja(dogadjaj string) string {
	s := strings.TrimSpace(dogadjaj)
	if i := strings.Index(strings.ToLower(s), "upozorenje za "); i >= 0 {
		s = s[i+len("upozorenje za "):]
	}
	if n, ok := pojaveUNominativu[strings.ToLower(s)]; ok {
		return n
	}
	return s
}

// pojaveUNominativu: DHMZ pojavu piše u akuzativu („upozorenje za kišu”)
var pojaveUNominativu = map[string]string{
	"kišu": "kiša", "obilnu kišu": "obilna kiša", "grmljavinu": "grmljavina", "maglu": "magla",
	"poledicu": "poledica", "tuču": "tuča", "visoku temperaturu": "visoka temperatura",
	"nisku temperaturu": "niska temperatura",
}

// zupanijaUpozorenja je županija na koju se upozorenje odnosi; pomorsko
// područje nema županiju
func zupanijaUpozorenja(u dhmz.Upozorenje) (dhmz.Zupanija, bool) {
	for _, z := range dhmz.Zupanije {
		if z.Vrijedi(u) {
			return z, true
		}
	}
	return dhmz.Zupanija{}, false
}

// zupanijaPoOznaci je županija iz adrese (?zupanija=LS)
func zupanijaPoOznaci(oznaka string) (dhmz.Zupanija, bool) {
	for _, z := range dhmz.Zupanije {
		if oznaka != "" && z.Oznaka == oznaka {
			return z, true
		}
	}
	return dhmz.Zupanija{}, false
}
