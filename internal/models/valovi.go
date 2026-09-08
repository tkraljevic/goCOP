package models

import (
	"fmt"
	"sort"
	"time"
)

// RazmakSpajanjaValova je koliko voda smije biti ispod pripremnog stanja a da
// se ono što slijedi i dalje računa kao isti val. Obrana koja na dan-dva padne
// ispod praga pa se vrati u praksi se ne prekida i ne proglašava nanovo; bez
// ovoga bi svaki takav pad razbio jedan val na dva zapisa.
const RazmakSpajanjaValova = 3 * 24 * time.Hour

// StupanjVala je koliko je jedan stupanj obrane bio na snazi u jednom valu.
// Stupnjevi se gnijezde: dok traje izvanredna obrana, traje i redovna, i
// pripremno stanje — zato su i granice svakog šire od granica višega.
type StupanjVala struct {
	Stupanj  DefensePhase
	PragCm   int
	Pocelo   time.Time
	Zavrsilo time.Time

	// Trajanje je vrijeme provedeno U TOM STANJU: iznad ovog praga, a ispod
	// sljedećeg višega. Tako se obrana i evidentira — u obrascu Hrvatskih voda
	// za lipanjski val 2024. stoji P.S. 433 h i R.O. 431 h, što zbrojeno daje
	// 864 h cijeloga vala. Ugniježđeno brojanje dalo bi 864 i 431.
	Trajanje time.Duration

	// TrajanjeIznad je vrijeme provedeno iznad praga bez obzira na više
	// stupnjeve. Za pripremno stanje to je cijeli val.
	TrajanjeIznad time.Duration

	// Navrata je koliko je puta voda u ovom valu prešla prag naviše. Više od
	// jednom znači da je stanje bilo prekinuto pa se vratilo.
	Navrata int

	// Korak je razlučivost mjerenja iz kojih je stupanj izračunat. Nosi se i
	// ovdje, a ne samo na valu, da ispis trenutka ne mora tražiti roditelja.
	Korak string

	// Nagadano znači da prijelaz praga nije uhvaćen između dva mjerenja nego
	// pada na sam rub niza: val je počeo prije prvog ili traje iza zadnjeg
	// podatka. Trajanje je tada najmanje toliko, a ne točno toliko.
	PocetakNaRubu bool
	KrajNaRubu    bool
}

// Raspon je razmak od prvog prijelaza naviše do zadnjeg pada ispod praga.
// Kad je voda prag prelazila više puta ili je bila u višem stanju, raspon je
// dulji od trajanja.
func (s StupanjVala) Raspon() time.Duration { return s.Zavrsilo.Sub(s.Pocelo) }

// Prekidan javlja je li stanje unutar vala bilo prekinuto pa se vratilo.
func (s StupanjVala) Prekidan() bool { return s.Navrata > 1 }

// NaRubu javlja dodiruje li stupanj rub niza, pa je trajanje donja granica.
func (s StupanjVala) NaRubu() bool { return s.PocetakNaRubu || s.KrajNaRubu }

// TrajanjeHR je trajanje kako se čita: kratko u satima, dulje u danima.
func (s StupanjVala) TrajanjeHR() string { return trajanjeHR(s.Trajanje) }

// Sati je trajanje u punim satima, kako stoji u obrascu obrane.
func (s StupanjVala) Sati() int { return int(s.Trajanje.Hours() + 0.5) }

// TrajanjeIznadHR je vrijeme iznad praga bez obzira na više stupnjeve.
func (s StupanjVala) TrajanjeIznadHR() string { return trajanjeHR(s.TrajanjeIznad) }

// PoceloHR i ZavrsiloHR su trenuci prijelaza. Dnevni niz zna samo dan, pa se
// sat i ne ispisuje: napisati „7.3.1956. 04h" značilo bi tvrditi točnost koje
// u podacima nema.
func (s StupanjVala) PoceloHR() string   { return trenutakHR(s.Pocelo, s.Korak) }
func (s StupanjVala) ZavrsiloHR() string { return trenutakHR(s.Zavrsilo, s.Korak) }

// Val je jedan prolazak vode iznad pripremnog stanja, od prijelaza naviše do
// povratka ispod. Sve što se u njemu dogodilo — koji su stupnjevi dosegnuti i
// koliko je koji trajao — računa se iz niza, a ne iz proglašenih obrana:
// odgovara na pitanje što bi po vodostaju bilo, ne što je itko odlučio.
type Val struct {
	Pocelo   time.Time
	Zavrsilo time.Time
	VrhCm    float64
	VrhKad   time.Time

	// Stupnjevi idu od najnižeg praga naviše i sadrže samo dosegnute.
	Stupnjevi []StupanjVala

	// Korak je razlučivost podataka u ovom valu: "satni" ili "dnevni". Stariji
	// dio niza ima jednu vrijednost dnevno, pa su i prijelazi na dan točni.
	Korak string
}

// Trajanje je koliko je voda u valu ukupno bila iznad pripremnog stanja,
// zbrojeno kroz sve stupnjeve.
func (v Val) Trajanje() time.Duration {
	if len(v.Stupnjevi) == 0 {
		return 0
	}
	return v.Stupnjevi[0].TrajanjeIznad
}

// Raspon je razmak od početka do kraja vala, uključujući i kratke padove
// ispod pripremnog stanja koji val nisu prekinuli.
func (v Val) Raspon() time.Duration { return v.Zavrsilo.Sub(v.Pocelo) }

// TrajanjeHR je trajanje vala kako se čita.
func (v Val) TrajanjeHR() string { return trajanjeHR(v.Trajanje()) }

// NajviseDosegnuto je najviši stupanj koji je val dosegnuo.
func (v Val) NajviseDosegnuto() DefensePhase {
	if len(v.Stupnjevi) == 0 {
		return PhaseNormal
	}
	return v.Stupnjevi[len(v.Stupnjevi)-1].Stupanj
}

// StupanjZa vraća stupanj vala zadane faze; nil kad val tu fazu nije dosegnuo.
func (v Val) StupanjZa(f DefensePhase) *StupanjVala {
	for i := range v.Stupnjevi {
		if v.Stupnjevi[i].Stupanj == f {
			return &v.Stupnjevi[i]
		}
	}
	return nil
}

// PoceloHR, ZavrsiloHR i VrhKadHR su trenuci vala u istoj razlučivosti.
func (v Val) PoceloHR() string   { return trenutakHR(v.Pocelo, v.Korak) }
func (v Val) ZavrsiloHR() string { return trenutakHR(v.Zavrsilo, v.Korak) }
func (v Val) VrhKadHR() string   { return trenutakHR(v.VrhKad, v.Korak) }

// VrhLabel je vršni vodostaj vala, zaokružen na cijeli centimetar.
func (v Val) VrhLabel() string { return fmt.Sprintf("%+d cm", int(v.VrhCm+0.5)) }

// Godina je hidrološka pripadnost vala — godina u kojoj je počeo.
func (v Val) Godina() int { return v.Pocelo.Year() }

// NaRubu javlja dodiruje li val rub niza.
func (v Val) NaRubu() bool {
	for _, s := range v.Stupnjevi {
		if s.NaRubu() {
			return true
		}
	}
	return false
}

// PragObrane je jedan prag s fazom koju otvara.
type PragObrane struct {
	Faza DefensePhase
	Cm   int
}

// PragoviObrane vraća pragove postaje izražene u centimetrima, od najnižeg.
// Prag zapisan samo tekstom ("206,30 m n. m.") ne ulazi: iz njega se ne da
// računati, a nagađanjem bi se izmislio prijelaz kojeg nema.
func (s Station) PragoviObrane() []PragObrane {
	var out []PragObrane
	for _, p := range []struct {
		f DefensePhase
		t Threshold
	}{
		{PhasePrep, s.Prep},
		{PhaseRegular, s.Regular},
		{PhaseEmergency, s.Emergency},
		{PhaseState, s.State},
	} {
		if p.t.Cm != nil {
			out = append(out, PragObrane{Faza: p.f, Cm: *p.t.Cm})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Cm < out[j].Cm })
	return out
}

// Valovi računa valove iz niza vodostaja i pragova postaje, najnoviji prvi.
// Niz mora biti složen po vremenu uzlazno i izražen u centimetrima na letvi.
//
// Prijelaz praga se ne stavlja na mjerenje nego se pravocrtno interpolira
// između dva susjedna: dnevni niz zna samo da je voda jučer bila ispod a danas
// iznad, pa bi stavljanje prijelaza na mjerenje trajanje sustavno skraćivalo
// ili produljivalo za do jedan cijeli dan.
func Valovi(niz []HidroTocka, pragovi []PragObrane) []Val {
	if len(pragovi) == 0 || len(niz) < 2 {
		return nil
	}
	osnova := float64(pragovi[0].Cm)

	var out []Val
	var od, do int // raspon indeksa jednog vala
	imam := false
	zadnjiIznad := time.Time{}

	zatvori := func() {
		if imam {
			if v, ok := sastaviVal(niz, od, do, pragovi); ok {
				out = append(out, v)
			}
			imam = false
		}
	}
	for i, t := range niz {
		if t.Vrijednost < osnova {
			continue
		}
		if imam && t.Kad.Sub(zadnjiIznad) > RazmakSpajanjaValova {
			zatvori()
		}
		if !imam {
			od, imam = i, true
		}
		do = i
		zadnjiIznad = t.Kad
	}
	zatvori()

	// najnoviji val prvi
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// sastaviVal razrađuje jedan val: vrh, razlučivost i granice svakog stupnja.
func sastaviVal(niz []HidroTocka, od, do int, pragovi []PragObrane) (Val, bool) {
	v := Val{VrhCm: niz[od].Vrijednost, VrhKad: niz[od].Kad}
	for i := od; i <= do; i++ {
		if niz[i].Vrijednost > v.VrhCm {
			v.VrhCm, v.VrhKad = niz[i].Vrijednost, niz[i].Kad
		}
	}
	v.Korak = korakNiza(niz, od, do)

	for _, p := range pragovi {
		if v.VrhCm < float64(p.Cm) {
			continue // val ovaj stupanj nije dosegnuo
		}
		s := StupanjVala{Stupanj: p.Faza, PragCm: p.Cm}
		s.Pocelo, s.PocetakNaRubu = prijelazGore(niz, od, do, float64(p.Cm))
		s.Zavrsilo, s.KrajNaRubu = prijelazDolje(niz, od, do, float64(p.Cm))
		if !s.Zavrsilo.After(s.Pocelo) {
			s.Zavrsilo = s.Pocelo
		}
		s.TrajanjeIznad, s.Navrata = iznadPraga(niz, od, do, float64(p.Cm))
		s.Korak = v.Korak
		v.Stupnjevi = append(v.Stupnjevi, s)
	}
	if len(v.Stupnjevi) == 0 {
		return v, false
	}
	// Vrijeme u stanju je vrijeme iznad ovog praga umanjeno za vrijeme
	// provedeno u višem stanju. Stupnjevi su složeni od najnižega, pa svaki
	// oduzima onaj iznad sebe.
	for i := range v.Stupnjevi {
		v.Stupnjevi[i].Trajanje = v.Stupnjevi[i].TrajanjeIznad
		if i+1 < len(v.Stupnjevi) {
			v.Stupnjevi[i].Trajanje -= v.Stupnjevi[i+1].TrajanjeIznad
		}
		if v.Stupnjevi[i].Trajanje < 0 {
			v.Stupnjevi[i].Trajanje = 0
		}
	}
	v.Pocelo = v.Stupnjevi[0].Pocelo
	v.Zavrsilo = v.Stupnjevi[0].Zavrsilo
	return v, true
}

// iznadPraga zbraja vrijeme koje je voda stvarno provela iznad praga i broji
// koliko je puta prag prešla naviše. Granice svakog navrata traže se jednako
// kao i inače — pravocrtno između dva mjerenja.
func iznadPraga(niz []HidroTocka, od, do int, prag float64) (time.Duration, int) {
	var ukupno time.Duration
	var navrata int
	var pocetak time.Time
	u := false
	for i := od; i <= do; i++ {
		iznad := niz[i].Vrijednost >= prag
		switch {
		case iznad && !u:
			navrata++
			if i > 0 && niz[i-1].Vrijednost < prag {
				pocetak = izmedu(niz[i-1], niz[i], prag)
			} else {
				pocetak = niz[i].Kad
			}
			u = true
		case !iznad && u:
			ukupno += izmedu(niz[i-1], niz[i], prag).Sub(pocetak)
			u = false
		}
	}
	if u {
		kraj := niz[do].Kad
		if do+1 < len(niz) && niz[do+1].Vrijednost < prag {
			kraj = izmedu(niz[do], niz[do+1], prag)
		}
		ukupno += kraj.Sub(pocetak)
	}
	return ukupno, navrata
}

// prijelazGore je trenutak u kojem je voda prvi put prešla prag naviše.
// Traži se par mjerenja koji prag obuhvaća, pa se točka između njih nađe
// pravocrtno. Kad takvog para nema — val počinje na samom rubu niza — vraća se
// prvo mjerenje iznad praga i oznaka da je to donja granica.
func prijelazGore(niz []HidroTocka, od, do int, prag float64) (time.Time, bool) {
	for i := od; i <= do; i++ {
		if niz[i].Vrijednost < prag {
			continue
		}
		if i > 0 && niz[i-1].Vrijednost < prag {
			return izmedu(niz[i-1], niz[i], prag), false
		}
		return niz[i].Kad, true
	}
	return niz[od].Kad, true
}

// prijelazDolje je trenutak u kojem je voda zadnji put pala ispod praga.
func prijelazDolje(niz []HidroTocka, od, do int, prag float64) (time.Time, bool) {
	for i := do; i >= od; i-- {
		if niz[i].Vrijednost < prag {
			continue
		}
		if i+1 < len(niz) && niz[i+1].Vrijednost < prag {
			return izmedu(niz[i], niz[i+1], prag), false
		}
		return niz[i].Kad, true
	}
	return niz[do].Kad, true
}

// izmedu nalazi trenutak u kojem pravac između dva mjerenja siječe prag.
func izmedu(a, b HidroTocka, prag float64) time.Time {
	raspon := b.Vrijednost - a.Vrijednost
	if raspon == 0 {
		return a.Kad
	}
	udio := (prag - a.Vrijednost) / raspon
	if udio < 0 {
		udio = 0
	}
	if udio > 1 {
		udio = 1
	}
	return a.Kad.Add(time.Duration(float64(b.Kad.Sub(a.Kad)) * udio))
}

// korakNiza javlja jesu li mjerenja u ovom valu satna ili dnevna. Gleda se
// medijan razmaka, da jedna rupa u nizu ne prevagne.
func korakNiza(niz []HidroTocka, od, do int) string {
	if do <= od {
		return "dnevni"
	}
	razmaci := make([]time.Duration, 0, do-od)
	for i := od + 1; i <= do; i++ {
		razmaci = append(razmaci, niz[i].Kad.Sub(niz[i-1].Kad))
	}
	sort.Slice(razmaci, func(i, j int) bool { return razmaci[i] < razmaci[j] })
	if razmaci[len(razmaci)/2] <= 3*time.Hour {
		return "satni"
	}
	return "dnevni"
}

// trenutakHR ispisuje trenutak u domaćem vremenu i u razlučivosti podataka:
// satni niz sa satom, dnevni samo s danom.
func trenutakHR(t time.Time, korak string) string {
	if t.IsZero() {
		return "—"
	}
	l := t.In(Zagreb)
	if korak == "satni" {
		return fmt.Sprintf("%d.%d.%d. %02dh", l.Day(), int(l.Month()), l.Year(), l.Hour())
	}
	return fmt.Sprintf("%d.%d.%d.", l.Day(), int(l.Month()), l.Year())
}

// trajanjeHR ispisuje trajanje onako kako se o njemu govori: do dva dana u
// satima, dulje u danima. „37 sati" kaže više nego „1,5 dan", a „42 dana"
// više nego „1008 sati".
func trajanjeHR(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d < 48*time.Hour {
		sati := int(d.Hours() + 0.5)
		if sati < 1 {
			return "<1 sat"
		}
		return fmt.Sprintf("%d %s", sati, sat(sati))
	}
	dani := int(d.Hours()/24 + 0.5)
	return fmt.Sprintf("%d %s", dani, dan(dani))
}

func sat(n int) string {
	if n%100 >= 11 && n%100 <= 14 {
		return "sati"
	}
	switch n % 10 {
	case 1:
		return "sat"
	case 2, 3, 4:
		return "sata"
	}
	return "sati"
}

func dan(n int) string {
	if n%100 >= 11 && n%100 <= 14 {
		return "dana"
	}
	switch n % 10 {
	case 1:
		return "dan"
	case 2, 3, 4:
		return "dana"
	}
	return "dana"
}

// ZbrojStupnja je koliko je jedan stupanj obrane ukupno bio na snazi kroz
// cijeli niz: u koliko valova, koliko ukupno i koliko u najduljem.
type ZbrojStupnja struct {
	Stupanj     DefensePhase
	PragCm      int
	Valova      int
	Ukupno      time.Duration // vrijeme u tom stanju
	UkupnoIznad time.Duration // vrijeme iznad praga, uključivo s višim stanjima
	Najdulji    time.Duration
	NajduljiKad time.Time // početak najduljeg
	NaRubu      bool      // barem jedan val dodiruje rub niza
}

// UkupnoHR i NajduljeHR su trajanja kako se čitaju.
func (z ZbrojStupnja) UkupnoHR() string      { return trajanjeHR(z.Ukupno) }
func (z ZbrojStupnja) UkupnoIznadHR() string { return trajanjeHR(z.UkupnoIznad) }
func (z ZbrojStupnja) NajduljeHR() string    { return trajanjeHR(z.Najdulji) }

// UdioHR je koliki dio promatranog razdoblja je stupanj bio na snazi.
func (z ZbrojStupnja) UdioHR(razdoblje time.Duration) string {
	if razdoblje <= 0 {
		return ""
	}
	p := 100 * z.UkupnoIznad.Hours() / razdoblje.Hours()
	if p > 0 && p < 0.1 {
		return "<0,1 %"
	}
	return zarez(fmt.Sprintf("%.1f %%", p))
}

func zarez(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] == '.' {
			b[i] = ','
		}
	}
	return string(b)
}

// ZbrojValova sažima sve valove po stupnjevima, od najnižeg praga naviše.
// Odgovara na pitanje koliko je koje stanje ukupno trajalo u cijelom nizu.
func ZbrojValova(valovi []Val, pragovi []PragObrane) []ZbrojStupnja {
	if len(pragovi) == 0 {
		return nil
	}
	red := make(map[DefensePhase]int, len(pragovi))
	out := make([]ZbrojStupnja, len(pragovi))
	for i, p := range pragovi {
		out[i] = ZbrojStupnja{Stupanj: p.Faza, PragCm: p.Cm}
		red[p.Faza] = i
	}
	for _, v := range valovi {
		for _, s := range v.Stupnjevi {
			i, ok := red[s.Stupanj]
			if !ok {
				continue
			}
			z := &out[i]
			z.Valova++
			z.Ukupno += s.Trajanje
			z.UkupnoIznad += s.TrajanjeIznad
			if s.Trajanje > z.Najdulji {
				z.Najdulji, z.NajduljiKad = s.Trajanje, s.Pocelo
			}
			if s.NaRubu() {
				z.NaRubu = true
			}
		}
	}
	return out
}

// RazdobljeNiza je što niz pokriva: odakle dokle i koliko mjerenja. Stoji uz
// zbroj, jer „ukupno 3900 dana redovne obrane" ne znači ništa bez podatka na
// kojem je razdoblju to izbrojeno.
type RazdobljeNiza struct {
	Od     time.Time
	Do     time.Time
	Zapisa int
}

// Trajanje je koliko je razdoblje dugo.
func (r RazdobljeNiza) Trajanje() time.Duration { return r.Do.Sub(r.Od) }

// Godina javlja koliko godina niz pokriva.
func (r RazdobljeNiza) Godina() int { return int(r.Trajanje().Hours()/24/365.25 + 0.5) }

// Ima javlja pokriva li razdoblje išta.
func (r RazdobljeNiza) Ima() bool { return !r.Od.IsZero() && r.Do.After(r.Od) }
