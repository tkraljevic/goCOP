package prognoza

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
)

// Upozorenja uz izdanje: što ne radi i koji su rezultati prognoze zbog toga
// upitni. Slaže ih izdavač iz onoga što je pri izdavanju imao, zapisuju se s
// izdanjem i putuju s njim, pa svaki čvor pokazuje isto što je izdavač vidio,
// a ne ono što bi sam iz svojih očitanja i svoje inačice zaključio.

// Vrste upozorenja.
const (
	VrstaUlaz           = "ulaz"           // letva bez mjerenja: karika lanca ili ulaz dnevnog modela
	VrstaDnevni         = "dnevni"         // dnevni model nije računao iz drugog razloga
	VrstaRezerva        = "rezerva"        // satni račun iz rezerve
	VrstaDrzi           = "drzi"           // bez svježeg ulaza, drži se zadnje mjerenje
	VrstaVrh            = "vrh"            // vrh lanca bez budućnosti koju inače dobiva
	VrstaKisa           = "kisa"           // dnevni model računa bez kiše
	VrstaDnevnaRezerva  = "dnevna-rezerva" // dnevni model iz rezerve
	VrstaZamjena        = "zamjena"        // kraj niza s druge obale
	VrstaRekonstrukcija = "rekonstrukcija" // rupu su popunili susjedi
	VrstaBezPrognoze    = "bez-prognoze"   // letva bez ijednog sata prognoze
	VrstaCeka           = "ceka"           // sat izdavanja drži vrh lanca koji kasni
	VrstaKrivulja       = "krivulja"       // vrijednost izvan krivulje protoka
	VrstaTudaDatumi     = "tuda-datumi"    // tuđa prognoza s neurednim datumima (NeuredniDatumi)
	VrstaUzvodno        = "uzvodno"        // uzvodni ispravak bez letve ili digao prognozu (upozorenjaUzvodnog)
	VrstaSkok           = "skok"           // lažni skok letve nije ušao u prognozu (upozorenjaSkokova)
	VrstaKvarMjerila    = "kvar-mjerila"   // zaglavljen ili odskočen uređaj (upozorenjaKvarova)
)

// Težine: upozorenje stoji otvoreno, napomena je sklopljena.
const (
	TezinaNapomena   = 1
	TezinaUpozorenje = 2
)

// ZaostatakKarike je koliko sati smije kasniti mjerenje karike lanca (letve
// sa svojim računom koja je ulaz drugoj) prije nego što se javi. Dok mjerenja
// nema, karika se tiho računa iz svojih ulaza; šest sati je i granica
// dnevnog modela, kojemu dan s više od šest sati bez mjerenja izostaje
// (DnevniIzSatnog). Mjereno na 219 izdanja od 28. 9. 2026.: s tri sata
// Budimpešta bi se javila 21 put, Mohács 18 i Dalj 7; sa šest 11, 15 i 4.
const ZaostatakKarike = 6

// Upozorenje je jedna stvar koja pri izdavanju nije radila. Sati su UTC sati
// od epohe; Letva i šifre u Opisu su šifre letvi, a imena slaže stranica.
type Upozorenje struct {
	Vrsta    string   `json:"vrsta"`
	Tezina   int      `json:"tezina"`
	Letva    string   `json:"letva,omitempty"`    // što ne radi, kad je to letva
	Zadnje   int64    `json:"zadnje,omitempty"`   // zadnje očitanje prije rupe
	Sljedece int64    `json:"sljedece,omitempty"` // prvo očitanje poslije rupe; 0 dok traje
	Opis     string   `json:"opis,omitempty"`     // razlog kako ga program zna; šifre letvi kao zasebne riječi
	Upitno   []Upitno `json:"upitno,omitempty"`
}

// Upitno je letva čija je prognoza zbog upozorenja upitna.
type Upitno struct {
	Letva    string `json:"letva"`
	Velicina string `json:"velicina,omitempty"` // samo uz krivulju
	OdSata   int    `json:"od_sata"`            // satna upitna od tog dosega; −1 satna nije upitna
	Dnevno   bool   `json:"dnevno,omitempty"`   // dnevna nije izdana ili je upitna
}

// Upozori slaže upozorenja izdanja iz onoga što je osvježavanje već imalo.
// Popis nije nil ni kad je sve u redu, da se zapisan prazan popis razlikuje
// od izdanja koje upozorenja ne bilježi. Redom: upozorenja prije napomena,
// pa po vrsti i letvi.
func Upozori(ishod *Ishod, pojasi map[string][]Pojas, nizovi map[Izvor]Niz, vrhovi map[Izvor]bool) []Upozorenje {
	ima := dnevneLetve(ishod.Dnevne)
	ulazi, pokriveno := upozorenjaUlaza(ishod, pojasi, nizovi, ima)
	u := append([]Upozorenje{}, ulazi...)
	u = append(u, upozorenjaDnevnog(ishod, pokriveno, ima)...)
	u = append(u, upozorenjaIzbora(ishod, pojasi, ima)...)
	u = append(u, upozorenjaVrhova(ishod, vrhovi, pojasi)...)
	u = append(u, upozorenjaOstalog(ishod)...)
	u = append(u, upozorenjeKrivulje(ishod.Izdane, ishod.Sada)...)
	u = append(u, upozorenjaRekonstrukcije(ishod, pojasi, ima)...)
	u = append(u, ishod.NeuredneTude...)
	u = append(u, upozorenjaUzvodnog(ishod.Uzvodno)...)
	u = append(u, upozorenjaSkokova(ishod.Skokovi, ishod.Sada)...)
	u = append(u, upozorenjaKvarova(ishod.Kvarovi, ishod.Sada)...)
	slices.SortStableFunc(u, func(a, b Upozorenje) int {
		return cmp.Or(cmp.Compare(b.Tezina, a.Tezina), cmp.Compare(a.Vrsta, b.Vrsta),
			cmp.Compare(a.Letva, b.Letva), cmp.Compare(a.Opis, b.Opis))
	})
	return u
}

// karikeBezMjerenja su karike lanca (ulazi koji imaju svoj račun) kojima
// zadnje mjerenje do sata izdavanja kasni više od ZaostatakKarike: letva →
// sat zadnjeg mjerenja, 0 kad ga nema. Krajnje letve (bez nizvodnih) ovdje
// ne ulaze: tri ih nema mjerenja tjednima, pa bi se javljale svaki sat.
func karikeBezMjerenja(pojasi map[string][]Pojas, nizovi map[Izvor]Niz, sada int64) map[string]int64 {
	out := map[string]int64{}
	for _, ps := range pojasi {
		for _, p := range ps {
			for _, u := range p.Ulazi {
				if len(pojasi[u.Letva]) == 0 {
					continue
				}
				z, ima := nizovi[Izvor{Letva: u.Letva, Velicina: u.Velicina}].ZadnjiSatDo(sada)
				if !ima || sada-z > ZaostatakKarike {
					out[u.Letva] = z
				}
			}
		}
	}
	return out
}

// skupUpitnog skuplja upitne letve jednog upozorenja.
type skupUpitnog map[string]*Upitno

func (s skupUpitnog) letva(l string) *Upitno {
	if u, ima := s[l]; ima {
		return u
	}
	u := &Upitno{Letva: l, OdSata: -1}
	s[l] = u
	return u
}

// satna označava satnu prognozu upitnom od zadanih dosega.
func (s skupUpitnog) satna(dosezi map[string]int) {
	for l, d := range dosezi {
		s.letva(l).OdSata = d
	}
}

// dnevna označava dnevnu prognozu letvi upitnom.
func (s skupUpitnog) dnevna(letve ...string) {
	for _, l := range letve {
		s.letva(l).Dnevno = true
	}
}

// dnevne su letve kojima je dnevna prognoza upitna.
func (s skupUpitnog) dnevne() []string {
	var out []string
	for l, u := range s {
		if u.Dnevno {
			out = append(out, l)
		}
	}
	return out
}

// popis je skup poredan po dosegu satne, a letve kojima je upitna samo
// dnevna idu na kraj.
func (s skupUpitnog) popis() []Upitno {
	if len(s) == 0 {
		return nil
	}
	out := make([]Upitno, 0, len(s))
	for _, u := range s {
		out = append(out, *u)
	}
	slices.SortFunc(out, func(a, b Upitno) int {
		return cmp.Or(cmp.Compare(redSatne(a), redSatne(b)), cmp.Compare(a.Letva, b.Letva))
	})
	return out
}

// redSatne je ključ poretka po dosegu satne; bez satne ide iza svih.
func redSatne(u Upitno) int {
	if u.OdSata < 0 {
		return int(^uint(0) >> 1)
	}
	return u.OdSata
}

// skupineUlaza drže po jedno upozorenje za svaku letvu bez mjerenja.
type skupineUlaza struct {
	po     map[string]*Upozorenje
	upitno map[string]skupUpitnog
}

func (g skupineUlaza) uzmi(letva string) (*Upozorenje, skupUpitnog) {
	if u, ima := g.po[letva]; ima {
		return u, g.upitno[letva]
	}
	u := &Upozorenje{Vrsta: VrstaUlaz, Tezina: TezinaUpozorenje, Letva: letva}
	g.po[letva], g.upitno[letva] = u, skupUpitnog{}
	return u, g.upitno[letva]
}

// upozorenjaUlaza slaže po jedno upozorenje za svaku letvu bez mjerenja i
// spaja ono što je zbog nje upitno: satnu prognozu niz lanac kad je karika,
// dnevnu ciljeva čiji je dnevni model zbog nje pao. pokriveno su dnevni
// ciljevi koje je time objasnilo.
func upozorenjaUlaza(ishod *Ishod, pojasi map[string][]Pojas, nizovi map[Izvor]Niz, ima map[string]bool) ([]Upozorenje, map[string]bool) {
	g := skupineUlaza{po: map[string]*Upozorenje{}, upitno: map[string]skupUpitnog{}}
	for letva, z := range karikeBezMjerenja(pojasi, nizovi, ishod.Sada) {
		u, s := g.uzmi(letva)
		u.Zadnje = z
		s.satna(satnaNizvodno(ishod, pojasi, letva, z+1))
	}
	pokriveno := map[string]bool{}
	for cilj, nd := range dnevniBezUlaza(ishod, ima) {
		for _, f := range nd.Fali {
			if strings.HasPrefix(f, "oborina ") {
				continue
			}
			u, s := g.uzmi(f)
			s.dnevna(cilj)
			pokriveno[cilj] = true
			if r, nadjena := rupaLetve(nd.Rupe, f); nadjena && u.Zadnje == 0 {
				u.Zadnje, u.Sljedece = r.Zadnje, r.Sljedece
			}
		}
	}
	out := make([]Upozorenje, 0, len(g.po))
	for letva, u := range g.po {
		s := g.upitno[letva]
		s.dnevna(preneseneBezDnevne(s.dnevne(), ima)...)
		u.Upitno = s.popis()
		out = append(out, *u)
	}
	return out, pokriveno
}

// satnaNizvodno su dosezi od kojih je satna prognoza upitna kad nevolja
// počne na letvi u satu prvi: ona i letve niz lanac (Nizvodno). Letva bez
// ijednog sata prognoze nije upitna od nekog dosega, nego je nema, pa ovdje
// ne ulazi; za nju stoji napomena VrstaBezPrognoze.
func satnaNizvodno(ishod *Ishod, pojasi map[string][]Pojas, letva string, prvi int64) map[string]int {
	d := doseziUpitnog(Nizvodno(pojasi, map[string]int64{letva: prvi}, ishod.Sada), ishod.Sada)
	for l := range ishod.BezPrognoze {
		delete(d, l)
	}
	return d
}

// satnaNizVrha su dosezi niz lanac od vrha koji drži zadnje mjerenje, od
// idućeg sata. Sam vrh nema svoju satnu prognozu, samo mjerenje u satu
// izdanja, pa među upitnima nije.
func satnaNizVrha(ishod *Ishod, pojasi map[string][]Pojas, vrh string) map[string]int {
	d := satnaNizvodno(ishod, pojasi, vrh, ishod.Sada+1)
	delete(d, vrh)
	return d
}

// rupaLetve nalazi rupu zadane letve.
func rupaLetve(rupe []RupaUlaza, letva string) (RupaUlaza, bool) {
	i := slices.IndexFunc(rupe, func(r RupaUlaza) bool { return r.Letva == letva })
	if i < 0 {
		return RupaUlaza{}, false
	}
	return rupe[i], true
}

// dnevneLetve su letve koje u izdanju imaju dnevnu prognozu.
func dnevneLetve(d []DnevnaIzdana) map[string]bool {
	out := map[string]bool{}
	for _, x := range d {
		out[x.Letva] = true
	}
	return out
}

// dnevniBezUlaza su dnevni ciljevi bez ijednog dana kojima je model pao jer
// ulazu fale dani: cilj → greška. Cilj koji ima dane ne ulazi ni kad je u
// BezDnevne: greška učenja glavnog modela ostaje i kad rezerva računa.
func dnevniBezUlaza(ishod *Ishod, ima map[string]bool) map[string]*NepotpuniDani {
	out := map[string]*NepotpuniDani{}
	for _, c := range DnevniCiljevi {
		var nd *NepotpuniDani
		if ima[c.Letva] || !errors.As(ishod.BezDnevne[c.Letva], &nd) {
			continue
		}
		out[c.Letva] = nd
	}
	return out
}

// upozorenjaDnevnog slaže upozorenja za dnevne ciljeve bez ijednog dana koje
// letva bez mjerenja nije objasnila, po jedno za svaki razlog.
func upozorenjaDnevnog(ishod *Ishod, pokriveno, ima map[string]bool) []Upozorenje {
	po := map[string][]string{}
	var razlozi []string
	for _, c := range DnevniCiljevi {
		if ima[c.Letva] || pokriveno[c.Letva] {
			continue
		}
		r := razlogBezDnevne(c.Letva, ishod.BezDnevne[c.Letva])
		if _, bilo := po[r]; !bilo {
			razlozi = append(razlozi, r)
		}
		po[r] = append(po[r], c.Letva)
	}
	out := make([]Upozorenje, 0, len(razlozi))
	for _, r := range razlozi {
		s := skupUpitnog{}
		s.dnevna(po[r]...)
		s.dnevna(preneseneBezDnevne(po[r], ima)...)
		out = append(out, Upozorenje{Vrsta: VrstaDnevni, Tezina: TezinaUpozorenje, Opis: r, Upitno: s.popis()})
	}
	return out
}

// razlogBezDnevne je greška dnevnog modela bez šifre cilja na početku, da se
// ciljevi s istim razlogom skupe; bez greške model nije ni pokušao, jer
// naučenog modela nema (osvježavanje bez arhive).
func razlogBezDnevne(letva string, err error) string {
	if err == nil {
		return "nema naučenog dnevnog modela"
	}
	return strings.TrimPrefix(err.Error(), letva+": ")
}

// preneseneBezDnevne su srpske letve koje bi imale prenesenu dnevnu
// prognozu, a nemaju je jer je pao neki od zadanih ciljeva. Letva s izvorom
// koji dnevni model nema (Bogojevo uz Dalj) dnevnu ni inače nema.
func preneseneBezDnevne(ciljevi []string, ima map[string]bool) []string {
	var out []string
	for _, p := range PreneseneLetve {
		if ima[p.Letva] || !sviDnevniCiljevi(p.Izvori) || !slices.ContainsFunc(p.Izvori, func(l string) bool { return slices.Contains(ciljevi, l) }) {
			continue
		}
		out = append(out, p.Letva)
	}
	return out
}

// sviDnevniCiljevi javlja jesu li sve letve ciljevi dnevnog modela.
func sviDnevniCiljevi(letve []string) bool {
	return !slices.ContainsFunc(letve, func(l string) bool {
		return !slices.ContainsFunc(DnevniCiljevi, func(c DnevniCilj) bool { return c.Letva == l })
	})
}

// upozorenjaIzbora prevodi izbor puta u upozorenja: satni račun iz rezerve
// (upitna letva i niz lanac od idućeg sata) ili sa zadržanim mjerenjem (letva
// je vrh bez svoje satne, upitno je niz lanac), dnevni model iz rezerve, kraj
// niza s druge obale i dnevni model bez kiše. Rupu koju su popunili susjedi
// javlja upozorenjaRekonstrukcije. Ključevi vrh: i preneseno: su
// redovan rad i ne javljaju se.
func upozorenjaIzbora(ishod *Ishod, pojasi map[string][]Pojas, ima map[string]bool) []Upozorenje {
	var out []Upozorenje
	for k, iz := range ishod.Izbor {
		vrsta, letva := vrstaIzbora(k, iz)
		if vrsta == "" {
			continue
		}
		u := Upozorenje{Vrsta: vrsta, Tezina: TezinaUpozorenje, Letva: letva, Opis: iz.Opis}
		s := skupUpitnog{}
		switch vrsta {
		case VrstaRezerva:
			s.satna(satnaNizvodno(ishod, pojasi, letva, ishod.Sada+1))
		case VrstaDrzi:
			s.satna(satnaNizVrha(ishod, pojasi, letva))
		case VrstaDnevnaRezerva:
			u.Tezina = TezinaNapomena
			s.dnevna(letva)
		case VrstaZamjena:
			u.Tezina = TezinaNapomena
		case VrstaKisa:
			s.dnevna(ciljeviSKisom(ima)...)
		}
		u.Upitno = s.popis()
		out = append(out, u)
	}
	return out
}

// vrstaIzbora kaže koje upozorenje daje ključ izbora i na koju se letvu
// odnosi; prazna vrsta znači da je to redovan rad.
func vrstaIzbora(kljuc string, iz Izbor) (vrsta, letva string) {
	if v, jest := vrstaIzboraBezLetve(kljuc, iz); jest {
		return v, ""
	}
	prefiks, letva, slozen := strings.Cut(kljuc, ":")
	if !slozen {
		return vrstaSatnog(iz.Inacica), kljuc
	}
	switch prefiks {
	case "dnevni":
		if iz.Inacica > 0 {
			return VrstaDnevnaRezerva, letva
		}
	case "zamjena":
		return VrstaZamjena, letva
	}
	return "", letva
}

// vrstaIzboraBezLetve su ključevi izbora koji nisu letva: dnevni model bez
// kiše je upozorenje, a napomene uzvodnog ispravka slaže upozorenjaUzvodnog.
// jest je false za ključ koji je letva ili ima prefiks.
func vrstaIzboraBezLetve(kljuc string, iz Izbor) (vrsta string, jest bool) {
	switch kljuc {
	case "oborina":
		if iz.Inacica == 2 {
			return VrstaKisa, true
		}
		return "", true
	case KljucUzvodno:
		return "", true
	}
	return "", false
}

// vrstaSatnog: inačica iznad nule je rezerva, −1 zadržano mjerenje.
func vrstaSatnog(inacica int) string {
	switch {
	case inacica > 0:
		return VrstaRezerva
	case inacica < 0:
		return VrstaDrzi
	}
	return ""
}

// ciljeviSKisom su ciljevi dnevnog modela s kišom koji u izdanju imaju dane.
func ciljeviSKisom(ima map[string]bool) []string {
	var out []string
	for _, c := range DnevniCiljevi {
		if len(c.Slivovi) > 0 && ima[c.Letva] {
			out = append(out, c.Letva)
		}
	}
	return out
}

// upozorenjaRekonstrukcije: rupa koju su susjedi popunili unutar raspona
// provjere je napomena. Izvan njega točnost nije provjerena, pa je to
// upozorenje: upitna je dnevna ciljeva kojima je letva ulaz, a za rupu koja
// traje i satna niz lanac od prvog rekonstruiranog sata, kao da mjerenja
// nema.
func upozorenjaRekonstrukcije(ishod *Ishod, pojasi map[string][]Pojas, ima map[string]bool) []Upozorenje {
	out := make([]Upozorenje, 0, len(ishod.Rekonstruirano))
	for letva, rupe := range ishod.Rekonstruirano {
		u := Upozorenje{Vrsta: VrstaRekonstrukcija, Tezina: TezinaNapomena, Letva: letva, Opis: OpisRekonstrukcije(rupe)}
		s := skupUpitnog{}
		for _, r := range rupe {
			if !r.Izvan {
				continue
			}
			u.Tezina = TezinaUpozorenje
			if r.Sljedece == 0 {
				s.satna(satnaNizvodno(ishod, pojasi, letva, r.Zadnje+1))
			}
			s.dnevna(dnevniCiljeviSUlazom(letva, ima)...)
		}
		u.Upitno = s.popis()
		out = append(out, u)
	}
	return out
}

// dnevniCiljeviSUlazom su ciljevi dnevnog modela s dnevnom prognozom u
// izdanju kojima je letva ulaz.
func dnevniCiljeviSUlazom(letva string, ima map[string]bool) []string {
	var out []string
	for _, c := range DnevniCiljevi {
		if ima[c.Letva] && slices.Contains(c.Ulazi, letva) {
			out = append(out, c.Letva)
		}
	}
	return out
}

// upozorenjaVrhova javljaju vrh lanca koji budućnost inače dobiva iz našeg
// dnevnog modela (VrhoviIzDnevnog), a u ovom izdanju je nije dobio: za njega
// se drži zadnje mjerenje, pa je satna niz lanac upitna od idućeg sata.
func upozorenjaVrhova(ishod *Ishod, vrhovi map[Izvor]bool, pojasi map[string][]Pojas) []Upozorenje {
	imaju := map[string]bool{}
	for _, l := range slices.Concat(ishod.NasiVrhovi, ishod.OperaterVrhovi) {
		imaju[l] = true
	}
	var out []Upozorenje
	for iz := range vrhovi {
		if !VrhoviIzDnevnog[iz.Letva] || imaju[iz.Letva] {
			continue
		}
		imaju[iz.Letva] = true // vrh u obje veličine javlja se jednom
		s := skupUpitnog{}
		s.satna(satnaNizVrha(ishod, pojasi, iz.Letva))
		out = append(out, Upozorenje{Vrsta: VrstaVrh, Tezina: TezinaUpozorenje, Letva: iz.Letva,
			Opis: "nema budućnosti vrha (dnevni model); drži se zadnje mjerenje", Upitno: s.popis()})
	}
	return out
}

// upozorenjaOstalog su napomene o letvama bez ijednog sata prognoze i o
// vrhovima koji kasne i drže sat izdavanja.
func upozorenjaOstalog(ishod *Ishod) []Upozorenje {
	var out []Upozorenje
	for letva, err := range ishod.BezPrognoze {
		out = append(out, Upozorenje{Vrsta: VrstaBezPrognoze, Tezina: TezinaNapomena, Letva: letva,
			Opis: strings.TrimPrefix(err.Error(), letva+": ")})
	}
	for letva, z := range ishod.Ceka {
		out = append(out, Upozorenje{Vrsta: VrstaCeka, Tezina: TezinaNapomena, Letva: letva, Zadnje: z})
	}
	return out
}

// upozorenjeKrivulje je jedna napomena o vrijednostima izvan krivulje
// protoka: za svaku letvu i veličinu prvi doseg na kojem je krivulja
// produljena preko svojeg ruba.
func upozorenjeKrivulje(izdane []Izdana, sada int64) []Upozorenje {
	prvi := map[Izvor]int{}
	for _, x := range izdane {
		iz := Izvor{Letva: x.Letva, Velicina: x.Velicina}
		if d, bilo := prvi[iz]; x.Izvan && (!bilo || int(x.Ciljni-sada) < d) {
			prvi[iz] = int(x.Ciljni - sada)
		}
	}
	if len(prvi) == 0 {
		return nil
	}
	u := Upozorenje{Vrsta: VrstaKrivulja, Tezina: TezinaNapomena, Upitno: make([]Upitno, 0, len(prvi))}
	for iz, d := range prvi {
		u.Upitno = append(u.Upitno, Upitno{Letva: iz.Letva, Velicina: iz.Velicina, OdSata: d})
	}
	slices.SortFunc(u.Upitno, func(a, b Upitno) int {
		return cmp.Or(cmp.Compare(a.Letva, b.Letva), cmp.Compare(a.Velicina, b.Velicina))
	})
	return []Upozorenje{u}
}

// String je upozorenje u šiframa, za dnevnik kruga i alate, npr. „ulaz
// mohacs zadnje 7.10. u 11:00, traje; upitno: mohacs +0 h, batina +1 h,
// osijek dnevna”.
func (u Upozorenje) String() string {
	dijelovi := []string{strings.TrimSpace(u.Vrsta + " " + u.Letva)}
	if u.Zadnje > 0 {
		dijelovi = append(dijelovi, "zadnje "+satDnevnika(u.Zadnje)+", "+sljedeceDnevnika(u.Sljedece))
	}
	if u.Opis != "" {
		dijelovi = append(dijelovi, u.Opis)
	}
	if len(u.Upitno) > 0 {
		dijelovi = append(dijelovi, "upitno: "+upitnoDnevnika(u.Upitno))
	}
	return strings.Join(dijelovi, "; ")
}

// satDnevnika je sat od epohe u mjesnom vremenu, kao u KoCeka.
func satDnevnika(sat int64) string {
	return time.Unix(sat*3600, 0).In(models.Zagreb).Format("2.1. u 15:04")
}

// sljedeceDnevnika kaže kad je rupa završila, ili da traje.
func sljedeceDnevnika(sat int64) string {
	if sat == 0 {
		return "traje"
	}
	return "sljedeće " + satDnevnika(sat)
}

// upitnoDnevnika nabraja upitne letve: satna od dosega, dnevna.
func upitnoDnevnika(upitno []Upitno) string {
	s := make([]string, 0, len(upitno))
	for _, x := range upitno {
		d := strings.TrimSpace(x.Letva + " " + x.Velicina)
		if x.OdSata >= 0 {
			d += fmt.Sprintf(" +%d h", x.OdSata)
		}
		if x.Dnevno {
			d += " dnevna"
		}
		s = append(s, d)
	}
	return strings.Join(s, ", ")
}
