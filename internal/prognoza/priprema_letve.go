package prognoza

// Priprema računa nove letve iz parametara zapisa: ulazi, rezerve, veličina i
// razine dolaze od stručnjaka, a ostalo namješta isti postupak kao i glavni lanac.
// Ništa se ne sprema. Rezultat prolazi iste provjere kao i upis, pa ga stranica
// može pokazati i poslije upisati. Glavnom lancu se ne mijenja ništa: Pripremi
// model ne prolazi ovuda.
//
// Ručne vrijednosti (Racun.Rucno) ovdje se još ne primjenjuju.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Letva izvan lanca najčešće ima kraću arhivu nego letve glavnog računa. Ove
// granice nisu izmjerene: dovoljne su da se razina ne namješta na šumu, a natpis
// uz račun kaže na kojoj je vodi učio.
const (
	najmanjeSatiZapisa = 1000
	najmanjeSatiRazine = 500
)

// PripremaLetve je ono što je priprema dala.
type PripremaLetve struct {
	Zapis    ZapisLetve   // zadani zapis s brojevima i dosegom
	Ucenje   UcenjeRacuna // voda i vrijeme na kojima je učila glavna inačica
	Napomene []string     // što nije dobilo račun, a ne ruši pripremu
}

// pripremaRacuna drži ono što priprema jedne letve čita više puta.
type pripremaRacuna struct {
	arhiva    *sql.DB
	z         ZapisLetve
	zapisi    map[string]ZapisLetve
	pokvareni map[string]error
	okvir     OkvirModela
}

// PripremiLetvu namjesti račun iz zapisa i vrati ga, bez spremanja. Baza prognoza
// se samo čita. Granice sati vrijede dok traje namještanje, kao za rijetke letve.
func PripremiLetvu(arhiva, baza *sql.DB, z ZapisLetve, o OkvirModela) (PripremaLetve, error) {
	if !namjestanjeTraje.TryLock() {
		return PripremaLetve{}, errors.New("namještanje modela već traje; pokušaj kad završi")
	}
	defer namjestanjeTraje.Unlock()
	defer postaviGraniceSati(najmanjeSatiZapisa, najmanjeSatiRazine)()
	s, err := novaPriprema(arhiva, baza, z, o)
	if err != nil {
		return PripremaLetve{}, err
	}
	return s.pripremi()
}

// novaPriprema čita model iz baze i odbija zapis koji se ne bi dao spremiti, prije
// nego što se pročita arhiva.
func novaPriprema(arhiva, baza *sql.DB, z ZapisLetve, o OkvirModela) (*pripremaRacuna, error) {
	var err error
	if o.Glavne, err = SveInacice(baza); err != nil {
		return nil, err
	}
	if o.ImaSatniProtok == nil {
		o.ImaSatniProtok = func(letva string) bool { return ImaSatniProtok(context.Background(), arhiva, letva) }
	}
	zapisi, pokvareni, err := zapisiZaRacun(baza, nil)
	if err != nil {
		return nil, err
	}
	if err := z.provjeriPrijePripreme(o); err != nil {
		return nil, err
	}
	return &pripremaRacuna{arhiva: arhiva, z: z, zapisi: zapisi, pokvareni: pokvareni, okvir: o}, nil
}

// provjeriPrijePripreme gleda sve što se zna prije računa. Doseg i brojevi tek
// nastaju, pa se ne gledaju; z je ovdje kopija.
func (z ZapisLetve) provjeriPrijePripreme(o OkvirModela) error {
	z.Racun.Doseg = 1
	z.Brojevi = BrojeviLetve{}
	for _, korak := range []func() error{
		z.provjeriOblik, z.provjeriRacun, z.provjeriIskljuceneIRezerve,
		func() error { return z.provjeriMjesto(o) },
		func() error { return z.provjeriRegistar(o) },
	} {
		if err := korak(); err != nil {
			return err
		}
	}
	return nil
}

// provjeriIskljuceneIRezerve odbija isključenu razinu uz rezervu. Razine rezerve
// mjere se u drugoj letvi, pa se isključena razina ne da prenijeti na nju, a
// rezerva bi računala i onda kad je stručnjak rekao da računa nema.
func (z ZapisLetve) provjeriIskljuceneIRezerve() error {
	if len(z.Racun.Razine.Iskljucene) > 0 && len(z.Racun.Rezerve) > 0 {
		return errors.New("račun s isključenom razinom ne može imati rezervu; makni jedno od toga")
	}
	return nil
}

func (s *pripremaRacuna) pripremi() (PripremaLetve, error) {
	izvori := s.izvori(s.z.Racun.Ulazi)
	if err := s.provjeriProtokUlaza(izvori); err != nil {
		return PripremaLetve{}, err
	}
	razine := &razdiobaRazina{razine: s.z.Racun.Razine}
	var ucenje UcenjeRacuna
	glavna, err := namjestiPoRazinama(s.arhiva, s.z.Letva, s.z.Velicina, izvori, &ucenje, razine.razdijeli)
	if err != nil {
		return PripremaLetve{}, err
	}
	if err := razine.oznaciIskljucene(glavna); err != nil {
		return PripremaLetve{}, err
	}
	inacice, napomene, err := s.dodajRezerve(glavna)
	if err != nil {
		return PripremaLetve{}, err
	}
	z := s.z
	z.Brojevi = BrojeviLetve{Inacice: inacice}
	z.Racun.Doseg = s.doseg(glavna)
	if err := provjeriUpis(s.zapisi, s.pokvareni, z, s.okvir); err != nil {
		return PripremaLetve{}, err
	}
	return PripremaLetve{Zapis: z, Ucenje: ucenje, Napomene: append(razine.napomene(glavna), napomene...)}, nil
}

// izvori su ulazi s veličinom. Ulaz koji ima račun uzima se u veličini tog računa,
// ulaz iz koda u svojoj veličini iz koda, a nova letva u veličini ove letve.
func (s *pripremaRacuna) izvori(ulazi []string) []Izvor {
	out := make([]Izvor, 0, len(ulazi))
	for _, u := range ulazi {
		out = append(out, Izvor{Letva: u, Velicina: s.velicinaUlaza(u)})
	}
	return out
}

func (s *pripremaRacuna) velicinaUlaza(letva string) string {
	if v, ima := velicinaRacuna(letva, s.zapisi, s.okvir.Glavne); ima {
		return v
	}
	if v, ima := Velicine[letva]; ima {
		return v
	}
	return s.z.Velicina
}

func (s *pripremaRacuna) provjeriProtokUlaza(izvori []Izvor) error {
	for _, iz := range izvori {
		if iz.Velicina == "protok" && !s.okvir.ImaSatniProtok(iz.Letva) {
			return fmt.Errorf("letva %s nema satni protok u arhivi; računaj u vodostaju", iz.Letva)
		}
	}
	return nil
}

// dodajRezerve namješta rezerve iza glavne inačice. Rezerva koja se ne da namjestiti
// samo se javi, a greška čitanja arhive prekida. Ručne granice rezervi ne vrijede,
// jer se njezine razine mjere u drugoj letvi, možda i u drugoj jedinici.
func (s *pripremaRacuna) dodajRezerve(glavna []Pojas) ([][]Pojas, []string, error) {
	inacice := [][]Pojas{glavna}
	var napomene []string
	for _, ulazi := range s.z.Racun.Rezerve {
		razine := &razdiobaRazina{razine: RazineLetve{Postoci: s.z.Racun.Razine.Postoci}}
		p, err := namjestiPoRazinama(s.arhiva, s.z.Letva, s.z.Velicina, s.izvori(ulazi), &UcenjeRacuna{}, razine.razdijeli)
		if errors.Is(err, ErrArhiva) {
			return nil, nil, err
		}
		if err != nil {
			napomene = append(napomene, "Rezerva "+strings.Join(ulazi, " + ")+" nema račun: "+err.Error())
			continue
		}
		for i := range p {
			p[i].Inacica = len(inacice)
		}
		inacice = append(inacice, p)
	}
	return inacice, napomene, nil
}

// doseg je dokle račun ima što reći: na najvišoj razini koja računa, najkraći zbroj
// kašnjenja i dosega ulaza. Dalje od toga bar jedan ulaz samo drži zadnje mjerenje. Na
// nižim razinama kašnjenje na malim vodama često izađe na rub pretrage, jer vala nema,
// pa bi potok držao zadnje mjerenje danima. Barem jedan sat, jer račun s kašnjenjem
// nula i dalje daje sljedeći sat.
func (s *pripremaRacuna) doseg(glavna []Pojas) int {
	racuna := slices.DeleteFunc(slices.Clone(glavna), func(p Pojas) bool { return p.Nepovezan })
	doseg := najveciDosegZapisa
	for _, u := range racuna[len(racuna)-1].Ulazi {
		doseg = min(doseg, u.PomakH+s.dosegUlaza(u.Letva))
	}
	return max(doseg, 1)
}

// dosegUlaza je dokle ulaz ima svoj račun. Ulaz bez računa je vrh, pa je to nula, i
// kad ga glavni lanac čita kao ulaz.
func (s *pripremaRacuna) dosegUlaza(letva string) int {
	if u, ima := s.zapisi[letva]; ima {
		return u.Racun.Doseg
	}
	if len(s.okvir.Glavne[letva]) > 0 {
		return najveciDosegZapisa
	}
	return 0
}

// razdiobaRazina pretvara razine iz zapisa u čvorove i pamti ih, da se poslije zna
// koji je pojas koja razina.
type razdiobaRazina struct {
	razine  RazineLetve
	cvorovi []float64
}

func (d *razdiobaRazina) razdijeli(poredani []float64) ([]float64, error) {
	var err error
	d.cvorovi, err = cvoroviRazina(poredani, d.razine)
	return d.cvorovi, err
}

// oznaciIskljucene označi isključene razine: u njima se prognoza ne izdaje, kao u
// nepovezanim pojasima letvi iz SamoPovezane. Razina bez računa ne može se označiti,
// a bez toga bi na njoj računala susjedna, pa to prekida pripremu.
func (d *razdiobaRazina) oznaciIskljucene(pojasi []Pojas) error {
	for _, i := range d.razine.Iskljucene {
		if i >= len(d.cvorovi)-1 {
			return fmt.Errorf("isključena razina %d ne postoji: račun ima %d razina", i+1, len(d.cvorovi)-1)
		}
		j := slices.IndexFunc(pojasi, func(p Pojas) bool { return p.Od == d.cvorovi[i] })
		if j < 0 {
			return fmt.Errorf("razina %d nema dosta izmjerenih sati, pa se ne da isključiti; spoji je sa susjednom", i+1)
		}
		pojasi[j].Nepovezan = true
	}
	if !slices.ContainsFunc(pojasi, func(p Pojas) bool { return !p.Nepovezan }) {
		return errors.New("sve razine su isključene; ostavi barem jednu")
	}
	return nil
}

// napomene kažu koja razina nije dobila račun. Na njoj računa susjedna.
func (d *razdiobaRazina) napomene(pojasi []Pojas) []string {
	var out []string
	for i := 0; i+1 < len(d.cvorovi); i++ {
		if !slices.ContainsFunc(pojasi, func(p Pojas) bool { return p.Od == d.cvorovi[i] }) {
			out = append(out, fmt.Sprintf("Razina od %.0f do %.0f nema račun: premalo je sati u kojima su izmjereni svi ulazi. Na njoj računa susjedna razina.",
				d.cvorovi[i], d.cvorovi[i+1]))
		}
	}
	return out
}

// cvoroviRazina daje čvorove iz zapisa. Razine bez postotaka i granica su zadane,
// spojene koliko traži duljina niza.
func cvoroviRazina(poredani []float64, r RazineLetve) ([]float64, error) {
	var c []float64
	var err error
	switch {
	case len(r.Granice) > 0:
		c, err = cvoroviIzGranica(poredani, r.Granice)
	case len(r.Postoci) > 0:
		c, err = cvoroviIzPostotaka(poredani, r.Postoci)
	default:
		return cvoroviZadanih(poredani), nil
	}
	if err != nil {
		return nil, err
	}
	return c, provjeriSatiRazina(poredani, c)
}

// cvoroviIzGranica su upisane granice između razina; krajeve daje sam niz.
func cvoroviIzGranica(poredani, granice []float64) ([]float64, error) {
	prvi, zadnji := poredani[0], poredani[len(poredani)-1]
	for _, g := range granice {
		if g <= prvi || g >= zadnji {
			return nil, fmt.Errorf("granica %.0f je izvan vode u arhivi (od %.0f do %.0f); upiši granicu unutar tog raspona", g, prvi, zadnji)
		}
	}
	return slices.Concat([]float64{prvi}, granice, []float64{zadnji}), nil
}

// cvoroviIzPostotaka su percentili. Dva ista broja znače razinu bez širine, a to se
// javlja umjesto da se tiho izbaci, jer bi se pomaknuli redni brojevi razina.
func cvoroviIzPostotaka(poredani, postoci []float64) ([]float64, error) {
	c := make([]float64, 0, len(postoci))
	for i, p := range postoci {
		v := percentil(poredani, p/100)
		if i > 0 && v <= c[i-1] {
			return nil, fmt.Errorf("razina %d nema širine, jer voda u arhivi dugo stoji na %.0f; spoji je sa susjednom", i, v)
		}
		c = append(c, v)
	}
	return c, nil
}

// provjeriSatiRazina odbija razinu koju je stručnjak sam zadao, a nema dosta sati.
func provjeriSatiRazina(poredani, c []float64) error {
	for i := 0; i+1 < len(c); i++ {
		od, do := sort.SearchFloat64s(poredani, c[i]), sort.SearchFloat64s(poredani, c[i+1])
		if i+2 == len(c) {
			do = len(poredani)
		}
		if do-od < NajmanjeSatiPojasa {
			return fmt.Errorf("razina od %.0f do %.0f ima %d sati, a treba barem %d; spoji je sa susjednom", c[i], c[i+1], do-od, NajmanjeSatiPojasa)
		}
	}
	return nil
}

// cvoroviZadanih su zadane razine (Pojasi), spojene odozgo dok svaka nema dosta
// sati. Kratkom nizu tako ostaje manje razina, a nema razine koja otpadne, pa i
// najviša voda iz niza ima svoj račun.
func cvoroviZadanih(poredani []float64) []float64 {
	n := float64(len(poredani))
	najmanje := float64(NajmanjeSatiPojasa)
	udjeli := []float64{1}
	gornji := 1.0
	for i := len(Pojasi) - 1; i > 0; i-- {
		donji := Pojasi[i][0]
		if (gornji-donji)*n >= najmanje && donji*n >= najmanje {
			udjeli = append(udjeli, donji)
			gornji = donji
		}
	}
	udjeli = append(udjeli, Pojasi[0][0])
	slices.Reverse(udjeli)
	c := make([]float64, 0, len(udjeli))
	for _, u := range udjeli {
		if v := percentil(poredani, u); len(c) == 0 || v > c[len(c)-1] {
			c = append(c, v)
		}
	}
	return c
}
