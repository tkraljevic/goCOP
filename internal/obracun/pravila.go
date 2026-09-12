package obracun

import (
	"strconv"
	"time"

	"gocop/internal/models"
)

// Blagdani su podatak organizacije, ne pravilo programa: zakon se mijenja
// (2020. je ukinut 25.6., a uvedeni 30.5. i 18.11.), a druga organizacija u
// drugoj državi ima drugi popis. Zato je kalendar popis pravila koji stoji u
// bazi i uređuje se u Administraciji; program dolazi napunjen hrvatskim
// zakonom (ZakonskiBlagdani) i u kodu zadržava samo računanje Uskrsa.

// VrstaPravila kaže kako se iz pravila dobiva datum
type VrstaPravila string

const (
	// Stalni je isti dan svake godine: mjesec i dan
	Stalni VrstaPravila = "STALNI"
	// PoUskrsu je pomak u danima od Uskrsa: 0 Uskrs, 1 Uskrsni ponedjeljak, 60 Tijelovo
	PoUskrsu VrstaPravila = "USKRS"
	// Jednokratni je jedan datum: dan žalosti, neradni dan koji Vlada proglasi
	Jednokratni VrstaPravila = "JEDNOKRATNI"
)

// Pravilo je jedan blagdan; vrijedi od godine OdGodine do DoGodine
// (0 = bez granice), pa se ukinut blagdan ne briše nego mu se zatvori
// razdoblje — prošli obračuni ostaju točni.
type Pravilo struct {
	ID       string       `json:"id"`
	Naziv    string       `json:"naziv"`
	Vrsta    VrstaPravila `json:"vrsta"`
	Mjesec   int          `json:"mjesec,omitempty"` // Stalni
	Dan      int          `json:"dan,omitempty"`    // Stalni
	Pomak    int          `json:"pomak,omitempty"`  // PoUskrsu: dana od Uskrsa
	Datum    string       `json:"datum,omitempty"`  // Jednokratni: 2006-01-02
	OdGodine int          `json:"od_godine,omitempty"`
	DoGodine int          `json:"do_godine,omitempty"`
}

// Vrijedi javlja je li pravilo na snazi te godine
func (p Pravilo) Vrijedi(godina int) bool {
	return (p.OdGodine == 0 || godina >= p.OdGodine) && (p.DoGodine == 0 || godina <= p.DoGodine)
}

// Pada javlja je li dan ovaj blagdan
func (p Pravilo) Pada(dan time.Time) bool {
	if !p.Vrijedi(dan.Year()) {
		return false
	}
	switch p.Vrsta {
	case Stalni:
		return int(dan.Month()) == p.Mjesec && dan.Day() == p.Dan
	case PoUskrsu:
		u := Uskrs(dan.Year()).AddDate(0, 0, p.Pomak)
		return u.Month() == dan.Month() && u.Day() == dan.Day()
	case Jednokratni:
		return dan.Format("2006-01-02") == p.Datum
	}
	return false
}

// Opis pravila za prikaz: "1. 1.", "Uskrs + 60 dana", "18.11.2026."
func (p Pravilo) Opis() string {
	switch p.Vrsta {
	case Stalni:
		return time.Date(2000, time.Month(p.Mjesec), p.Dan, 0, 0, 0, 0, time.UTC).Format("2. 1.")
	case PoUskrsu:
		switch p.Pomak {
		case 0:
			return "Uskrs"
		case 1:
			return "dan poslije Uskrsa"
		}
		return "Uskrs + " + strconv.Itoa(p.Pomak) + " dana"
	case Jednokratni:
		if t, err := time.Parse("2006-01-02", p.Datum); err == nil {
			return t.Format("2.1.2006.")
		}
		return p.Datum
	}
	return ""
}

// Pravila je kalendar iz popisa pravila
type Pravila []Pravilo

// Blagdan javlja je li dan blagdan po ijednom pravilu
func (ps Pravila) Blagdan(dan time.Time) bool {
	dan = dan.In(models.Zagreb)
	for _, p := range ps {
		if p.Pada(dan) {
			return true
		}
	}
	return false
}

// Blagdani vraća sve blagdane godine, redom
func (ps Pravila) Blagdani(godina int) []time.Time {
	var out []time.Time
	for d := time.Date(godina, 1, 1, 0, 0, 0, 0, models.Zagreb); d.Year() == godina; d = d.AddDate(0, 0, 1) {
		if ps.Blagdan(d) {
			out = append(out, d)
		}
	}
	return out
}

// ZakonskiBlagdani su blagdani Republike Hrvatske po Zakonu o blagdanima,
// spomendanima i neradnim danima: stanje od 2020. (NN 110/2019), a za ranije
// godine ono što je tada vrijedilo — Dan državnosti 25.6. i Dan neovisnosti
// 8.10. do 2019. Time se program puni kad popis u bazi još ne postoji, i
// tako obračun za stari dnevnik ne dobije blagdan koji tada nije postojao.
func ZakonskiBlagdani() Pravila {
	s := func(id, naziv string, m, d, od, do int) Pravilo {
		return Pravilo{ID: id, Naziv: naziv, Vrsta: Stalni, Mjesec: m, Dan: d, OdGodine: od, DoGodine: do}
	}
	u := func(id, naziv string, pomak int) Pravilo {
		return Pravilo{ID: id, Naziv: naziv, Vrsta: PoUskrsu, Pomak: pomak}
	}
	return Pravila{
		s("nova-godina", "Nova godina", 1, 1, 0, 0),
		s("sveta-tri-kralja", "Bogojavljenje ili Sveta tri kralja", 1, 6, 0, 0),
		u("uskrs", "Uskrs", 0),
		u("uskrsni-ponedjeljak", "Uskrsni ponedjeljak", 1),
		s("praznik-rada", "Praznik rada", 5, 1, 0, 0),
		s("dan-drzavnosti", "Dan državnosti", 5, 30, 2020, 0),
		u("tijelovo", "Tijelovo", 60),
		s("dan-antifasisticke-borbe", "Dan antifašističke borbe", 6, 22, 0, 0),
		s("dan-drzavnosti-do-2019", "Dan državnosti (do 2019.)", 6, 25, 0, 2019),
		s("dan-pobjede", "Dan pobjede i domovinske zahvalnosti i Dan hrvatskih branitelja", 8, 5, 0, 0),
		s("velika-gospa", "Velika Gospa", 8, 15, 0, 0),
		s("dan-neovisnosti-do-2019", "Dan neovisnosti (do 2019.)", 10, 8, 0, 2019),
		s("svi-sveti", "Svi sveti", 11, 1, 0, 0),
		s("dan-sjecanja", "Dan sjećanja na žrtve Domovinskog rata i Dan sjećanja na žrtvu Vukovara i Škabrnje", 11, 18, 2020, 0),
		s("bozic", "Božić", 12, 25, 0, 0),
		s("sveti-stjepan", "Sveti Stjepan", 12, 26, 0, 0),
	}
}
