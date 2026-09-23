package prognoza

import (
	"fmt"
	"math"
	"sort"
)

// Račun prognoze: uzme se zadnji sat, pa se niz lancem prenosi nizvodno.
//
// Vrijednost se traži rekurzivno. Za sat koji je prošao uzima se izmjereno; za
// sat koji tek dolazi računa se iz uzvodnih letvi, a one opet iz svojih. Lanac
// se sam zaustavi ondje gdje mu ponestane ulaza — i upravo to je doseg
// prognoze. Bez oborine dalje od toga nema što reći.
//
// Uz svaku vrijednost ide raspon: koliko se očekuje da promaši. Za izmjereno
// je nula, za računato je rasap pojasa, a kroz lanac se raspon prenosi
// nagibom i zbraja kvadratno — promašaji uzvodnih letvi nisu isti promašaj, pa
// se ne zbrajaju kao da jesu.

// PoluvijekIspravka je koliko brzo se zaboravlja razlika između modela i
// mjerenja u trenutku izdavanja. Model koji je u tom trenutku bio 20 cm
// prenizak bit će prenizak i sat poslije, pa se ta razlika nosi naprijed — ali
// sve slabije, jer se s vremenom gubi u onome što tek dolazi. Bez toga lanac
// na donjoj Dravi na 48 sati sustavno dodaje vodu: promašaj na Moslavini bio
// je +51 cm, na Donjem Miholjcu +37.
var PoluvijekIspravka = 48.0

// NajveciRazmak je koliko sati smije premostiti između dva očitanja. Letve
// koje se očitavaju triput na dan ostavljaju rupe od osam sati; preko toga se
// ne premošćuje, jer se u međuvremenu val može i popeti i spustiti.
const NajveciRazmak = 12

// Najranije je koliko se unatrag smije tražiti vrijednost koje nema. Dublje
// od toga nije prognoza nego rekonstrukcija, a za nju postoji arhiva.
const Najranije = 240

// Niz su izmjerene satne vrijednosti jedne letve u jednoj veličini, poredane.
type Niz struct {
	Sati   []int64
	Iznosi []float64
}

// NoviNiz slaže niz iz nepoređanih parova.
func NoviNiz(vrijednosti map[int64]float64) Niz {
	n := Niz{Sati: make([]int64, 0, len(vrijednosti))}
	for t := range vrijednosti {
		n.Sati = append(n.Sati, t)
	}
	sort.Slice(n.Sati, func(i, j int) bool { return n.Sati[i] < n.Sati[j] })
	n.Iznosi = make([]float64, len(n.Sati))
	for i, t := range n.Sati {
		n.Iznosi[i] = vrijednosti[t]
	}
	return n
}

// U vraća vrijednost u zadanom satu, po potrebi premošćenu između dva
// očitanja. Premošćuje se pravocrtno jer se unutar nekoliko sati vodostaj i
// ponaša pravocrtno; preko NajveciRazmak se ne premošćuje.
func (n Niz) U(t int64) (float64, bool) {
	i := sort.Search(len(n.Sati), func(i int) bool { return n.Sati[i] >= t })
	if i < len(n.Sati) && n.Sati[i] == t {
		return n.Iznosi[i], true
	}
	if i == 0 || i == len(n.Sati) {
		return 0, false
	}
	prije, poslije := n.Sati[i-1], n.Sati[i]
	if poslije-prije > NajveciRazmak {
		return 0, false
	}
	udio := float64(t-prije) / float64(poslije-prije)
	return n.Iznosi[i-1] + udio*(n.Iznosi[i]-n.Iznosi[i-1]), true
}

// Zadnji vraća sat zadnjeg očitanja.
func (n Niz) Zadnji() (int64, bool) {
	if len(n.Sati) == 0 {
		return 0, false
	}
	return n.Sati[len(n.Sati)-1], true
}

// Vrijednost je jedan broj u nizu, izmjeren ili izračunat.
type Vrijednost struct {
	Iznos    float64
	Raspon   float64 // koliko se očekuje da promaši; nula za izmjereno
	Prognoza bool
}

// Racunalo drži sve što treba za jedan prolaz: namještene pojase, izmjerene
// nizove i sat do kojeg se mjerenju vjeruje.
type Racunalo struct {
	pojasi    map[string][]Pojas
	mjereno   map[Izvor]Niz
	sada      int64
	zapamceno map[kljuc]upamceno
	uTijeku   map[kljuc]bool
	ostaci    map[Izvor]ostatak
	uOstatku  map[Izvor]bool
}

// ostatak je razlika između mjerenja i modela u trenutku izdavanja.
type ostatak struct {
	iznos float64
	ima   bool
}

// upamceno pamti i neuspjeh: sat za koji ulaza nema neće ih dobiti ni kad se
// za njim posegne drugi put, a lanac za njim poseže iz svakog nizvodnog sata.
type upamceno struct {
	v  Vrijednost
	ok bool
}

type kljuc struct {
	Izvor
	sat int64
}

// NovoRacunalo slaže račun. Pojasi dolaze iz baze prognoza, nizovi iz
// očitanja, a sada je zadnji sat kojem se vjeruje kao izmjerenom.
func NovoRacunalo(pojasi map[string][]Pojas, mjereno map[Izvor]Niz, sada int64) *Racunalo {
	return &Racunalo{
		pojasi: pojasi, mjereno: mjereno, sada: sada,
		zapamceno: map[kljuc]upamceno{},
		uTijeku:   map[kljuc]bool{},
		ostaci:    map[Izvor]ostatak{},
		uOstatku:  map[Izvor]bool{},
	}
}

// Sada je sat do kojeg se mjerenju vjeruje.
func (r *Racunalo) Sada() int64 { return r.sada }

// U vraća vrijednost letve u zadanom satu — izmjerenu ako je ima, inače
// izračunatu iz uzvodnih letvi.
func (r *Racunalo) U(iz Izvor, t int64) (Vrijednost, bool) {
	k := kljuc{iz, t}
	if u, ima := r.zapamceno[k]; ima {
		return u.v, u.ok
	}
	if t < r.sada-Najranije {
		return Vrijednost{}, false
	}
	if r.uTijeku[k] {
		return Vrijednost{}, false // lanac se vrti u krug
	}
	if t <= r.sada {
		if v, ima := r.mjereno[iz].U(t); ima {
			return r.zapamti(k, Vrijednost{Iznos: v}, true)
		}
	}
	r.uTijeku[k] = true
	defer delete(r.uTijeku, k)

	v, ok := r.izracunaj(iz, t)
	if !ok {
		return r.zapamti(k, Vrijednost{}, false)
	}
	// Ispravak se nosi samo naprijed: za sat koji je prošao mjerenje je već
	// rečeno svoje, a ondje gdje ga nema nemamo ni s čim usporediti.
	if t > r.sada && PoluvijekIspravka > 0 {
		if o := r.ostatakZa(iz); o.ima {
			v.Iznos += o.iznos * math.Exp2(-float64(t-r.sada)/PoluvijekIspravka)
		}
	}
	return r.zapamti(k, v, true)
}

// ostatakZa mjeri koliko je model promašio u samom trenutku izdavanja.
func (r *Racunalo) ostatakZa(iz Izvor) ostatak {
	if o, ima := r.ostaci[iz]; ima {
		return o
	}
	if r.uOstatku[iz] {
		return ostatak{}
	}
	r.uOstatku[iz] = true
	defer delete(r.uOstatku, iz)

	o := ostatak{}
	if mj, ima := r.mjereno[iz].U(r.sada); ima {
		k := kljuc{iz, r.sada}
		bilo := r.uTijeku[k]
		r.uTijeku[k] = true
		if v, ok := r.izracunaj(iz, r.sada); ok {
			o = ostatak{iznos: mj - v.Iznos, ima: true}
		}
		if !bilo {
			delete(r.uTijeku, k)
		}
	}
	r.ostaci[iz] = o
	return o
}

// izracunaj vrti sam model, bez ispravka i bez posezanja za mjerenjem cilja.
func (r *Racunalo) izracunaj(iz Izvor, t int64) (Vrijednost, bool) {
	p, ima := r.pojasZa(iz, t)
	if !ima {
		return Vrijednost{}, false
	}
	iznosi := make([]float64, len(p.Ulazi))
	raspon := p.Rasap * p.Rasap
	for i, u := range p.Ulazi {
		v, ok := r.U(Izvor{u.Letva, u.Velicina}, t-int64(u.PomakH))
		if !ok {
			return Vrijednost{}, false
		}
		iznosi[i] = v.Iznos
		raspon += (u.Nagib * v.Raspon) * (u.Nagib * v.Raspon)
	}
	iznos, err := p.Racunaj(iznosi)
	if err != nil {
		return Vrijednost{}, false
	}
	return Vrijednost{Iznos: iznos, Raspon: math.Sqrt(raspon), Prognoza: true}, true
}

func (r *Racunalo) zapamti(k kljuc, v Vrijednost, ok bool) (Vrijednost, bool) {
	r.zapamceno[k] = upamceno{v, ok}
	return v, ok
}

// pojasZa bira pojas kojem pripada vrijednost glavnog ulaza. Kašnjenje se
// razlikuje po pojasu, pa se glavni ulaz čita onoliko unatrag koliko taj pojas
// traži — i tek se onda gleda pripada li mu.
func (r *Racunalo) pojasZa(iz Izvor, t int64) (Pojas, bool) {
	svi := r.pojasi[iz.Letva]
	var moguci []Pojas
	for _, p := range svi {
		if p.Velicina != iz.Velicina || len(p.Ulazi) == 0 {
			continue
		}
		g := p.Ulazi[0]
		v, ok := r.U(Izvor{g.Letva, g.Velicina}, t-int64(g.PomakH))
		if !ok {
			continue
		}
		if p.Vrijedi(v.Iznos) {
			return p, true
		}
		moguci = append(moguci, p)
	}
	// Voda kakvu nismo vidjeli: uzima se najbliži rub. Prognoza ondje nije
	// pouzdana, ali šutjeti o njoj bilo bi gore — raspon uz nju to i kaže.
	if len(moguci) == 0 {
		return Pojas{}, false
	}
	g := moguci[0].Ulazi[0]
	v, _ := r.U(Izvor{g.Letva, g.Velicina}, t-int64(g.PomakH))
	if v.Iznos < moguci[0].Od {
		return moguci[0], true
	}
	return moguci[len(moguci)-1], true
}

// Izdana je jedna prognozirana vrijednost, onakva kakva ide u zapis.
type Izdana struct {
	Letva      string
	Velicina   string
	Izdano     int64
	Ciljni     int64
	Vrijednost float64
	Raspon     float64
	Model      string
}

// Prognoziraj računa niz po satu unaprijed, dokle lanac seže. Zaustavi se na
// prvom satu za koji ulaza više nema: dalje od toga nije prognoza nego
// nagađanje.
func (r *Racunalo) Prognoziraj(letva string, najdalje int, model string) ([]Izdana, error) {
	pojasi := r.pojasi[letva]
	if len(pojasi) == 0 {
		return nil, fmt.Errorf("%s: nema namještenih pojasa", letva)
	}
	iz := Izvor{letva, pojasi[0].Velicina}
	var out []Izdana
	for sat := int64(1); sat <= int64(najdalje); sat++ {
		t := r.sada + sat
		v, ok := r.U(iz, t)
		if !ok {
			break
		}
		out = append(out, Izdana{
			Letva: letva, Velicina: iz.Velicina, Izdano: r.sada, Ciljni: t,
			Vrijednost: v.Iznos, Raspon: v.Raspon, Model: model,
		})
	}
	return out, nil
}
