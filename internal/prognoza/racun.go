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

// ZadnjiDo vraća zadnje očitanje koje nije kasnije od zadanog sata. Račun smije
// posegnuti samo za onim što je u trenutku izdavanja već bilo poznato; uzeti
// zadnje očitanje cijelog niza značilo bi, pri puštanju unatrag, čitati iz
// budućnosti.
func (n Niz) ZadnjiDo(t int64) (float64, bool) {
	i := sort.Search(len(n.Sati), func(i int) bool { return n.Sati[i] > t }) - 1
	if i < 0 {
		return 0, false
	}
	return n.Iznosi[i], true
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
	pojasi     map[string][]Pojas
	poVelicini map[Izvor][]Pojas
	mjereno    map[Izvor]Niz
	sada       int64
	zapamceno  map[kljuc]upamceno
	uTijeku    map[kljuc]bool
	ostaci     map[Izvor]ostatak
	uOstatku   map[Izvor]bool
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
	po := map[Izvor][]Pojas{}
	for letva, ps := range pojasi {
		for _, x := range ps {
			if len(x.Ulazi) > 0 {
				iz := Izvor{Letva: letva, Velicina: x.Velicina}
				po[iz] = append(po[iz], x)
			}
		}
	}
	return &Racunalo{
		pojasi: pojasi, poVelicini: po, mjereno: mjereno, sada: sada,
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
	// Letva bez vlastitog računa je vrh lanca: uzvodno od nje nemamo ništa.
	// Za sate koji dolaze drži se zadnja izmjerena vrijednost. To nije
	// prognoza nego pretpostavka da se gore ništa neće promijeniti, i ona s
	// vremenom postaje sve slabija — ali granica dokle vrijedi ne postavlja se
	// ovdje, nego je mjeri provjera: ondje gdje prognoza prestane pobjeđivati
	// postojanost, prestaje i smisao izdavanja.
	if t > r.sada && len(r.poVelicini[iz]) == 0 {
		if v, ima := r.mjereno[iz].ZadnjiDo(r.sada); ima {
			return r.zapamti(k, Vrijednost{Iznos: v, Prognoza: true}, true)
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

func (r *Racunalo) zapamti(k kljuc, v Vrijednost, ok bool) (Vrijednost, bool) {
	r.zapamceno[k] = upamceno{v, ok}
	return v, ok
}

// izracunaj vrti sam model, bez ispravka i bez posezanja za mjerenjem cilja.
func (r *Racunalo) izracunaj(iz Izvor, t int64) (Vrijednost, bool) {
	svi := r.poVelicini[iz]
	if len(svi) == 0 {
		return Vrijednost{}, false
	}
	prvi := svi[0].Ulazi[0]
	glavni := Izvor{Letva: prvi.Letva, Velicina: prvi.Velicina}

	// Kašnjenje ovisi o vodnosti, a vodnost se čita tek kad se zna kašnjenje.
	// Krene se od kašnjenja prvog pojasa pa se popravi; dva kruga su dosta jer
	// je kašnjenje po vodnosti blago.
	pomak := float64(prvi.PomakH)
	var glavna Vrijednost
	for krug := 0; krug < 3; krug++ {
		v, ok := r.uPomaku(glavni, t, pomak, prvi.Sirina)
		if !ok {
			return Vrijednost{}, false
		}
		glavna = v
		novi := Kasnjenje(svi, v.Iznos)
		if math.Abs(novi-pomak) < 0.01 {
			break
		}
		pomak = novi
	}

	p, ima := ZaVrijednost(svi, glavna.Iznos)
	if !ima {
		return Vrijednost{}, false
	}
	iznosi := make([]float64, len(p.Ulazi))
	raspon := p.Rasap * p.Rasap
	iznosi[0] = glavna.Iznos
	raspon += (p.Ulazi[0].Nagib * glavna.Raspon) * (p.Ulazi[0].Nagib * glavna.Raspon)
	// Sporedni ulazi imaju jedno kašnjenje za sve pojase, pa se čitaju ravno.
	for i := 1; i < len(p.Ulazi); i++ {
		u := p.Ulazi[i]
		v, ok := r.uPomaku(Izvor{Letva: u.Letva, Velicina: u.Velicina},
			t, float64(u.PomakH), u.Sirina)
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

// Kasnjenje je koliko val putuje pri zadanoj vodnosti glavnog ulaza. Između
// pojasa se prelijeva pravocrtno, jer se kašnjenje s vodnošću mijenja polako, a
// po pojasu bi skakalo: na Terezinu Polju ide s 10 na 16 sati, i taj skok na
// hidroelektranskom valu pomakne očitanje za pola vala — prognoza mu je iz sata
// u sat poskakivala i po 50 m³/s, a Belišću i po pola metra.
func Kasnjenje(pojasi []Pojas, vrijednost float64) float64 {
	if len(pojasi) == 0 {
		return 0
	}
	pomak := func(p Pojas) float64 { return float64(p.Ulazi[0].PomakH) }
	sredina := func(p Pojas) float64 { return (p.Od + p.Do) / 2 }
	if vrijednost <= sredina(pojasi[0]) {
		return pomak(pojasi[0])
	}
	for i := 1; i < len(pojasi); i++ {
		a, b := pojasi[i-1], pojasi[i]
		if sirina := sredina(b) - sredina(a); vrijednost <= sredina(b) && sirina > 0 {
			u := (vrijednost - sredina(a)) / sirina
			return pomak(a) + u*(pomak(b)-pomak(a))
		}
	}
	return pomak(pojasi[len(pojasi)-1])
}

// uPomaku čita ulaz s kašnjenjem koje ne mora biti cijeli broj sati, kao
// prosjek prozora koji mu prethodi. Kašnjenje se zaokruživanjem opet lomi, pa
// se prelijeva između dva susjedna sata; prozor guši kratke valove koje rijeka
// na putu izgubi.
func (r *Racunalo) uPomaku(iz Izvor, t int64, pomak float64, sirina int) (Vrijednost, bool) {
	dolje := int64(math.Floor(pomak))
	gore := int64(math.Ceil(pomak))
	blize, ok := r.prosjek(iz, t, dolje, sirina)
	if !ok {
		return Vrijednost{}, false
	}
	if gore == dolje {
		return blize, true
	}
	dalje, ok := r.prosjek(iz, t, gore, sirina)
	if !ok {
		return Vrijednost{}, false
	}
	u := pomak - float64(dolje)
	return Vrijednost{
		Iznos:    blize.Iznos + u*(dalje.Iznos-blize.Iznos),
		Raspon:   blize.Raspon + u*(dalje.Raspon-blize.Raspon),
		Prognoza: blize.Prognoza || dalje.Prognoza,
	}, true
}

// prosjek je srednja vrijednost prozora koji završava u satu t-pomak. Promašaji
// susjednih sati nisu neovisni — isti val ih nosi — pa se raspon ne smanjuje
// prosjekom, nego ostaje najveći od njih.
func (r *Racunalo) prosjek(iz Izvor, t, pomak int64, sirina int) (Vrijednost, bool) {
	if sirina < 1 {
		sirina = 1
	}
	var zbroj, najRaspon float64
	prognoza := false
	for k := int64(0); k < int64(sirina); k++ {
		v, ok := r.U(iz, t-pomak-k)
		if !ok {
			return Vrijednost{}, false
		}
		zbroj += v.Iznos
		if v.Raspon > najRaspon {
			najRaspon = v.Raspon
		}
		prognoza = prognoza || v.Prognoza
	}
	return Vrijednost{Iznos: zbroj / float64(sirina), Raspon: najRaspon, Prognoza: prognoza}, true
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

// Prognoziraj računa niz po satu unaprijed, počevši od sata izdavanja.
// Zaustavi se na prvom satu za koji ulaza više nema.
func (r *Racunalo) Prognoziraj(letva string, najdalje int, model string) ([]Izdana, error) {
	pojasi := r.pojasi[letva]
	if len(pojasi) == 0 {
		return nil, fmt.Errorf("%s: nema namještenih pojasa", letva)
	}
	iz := Izvor{letva, pojasi[0].Velicina}
	// Niz počinje u samom satu izdavanja, gdje je vrijednost izmjerena. Tako
	// prognoza nosi i svoje sidro: prikaz ne mora nikamo drugamo po ono od
	// čega je krenula, a pogrešnik na tom satu mjeri nulu, kako i treba.
	var out []Izdana
	for sat := int64(0); sat <= int64(najdalje); sat++ {
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
