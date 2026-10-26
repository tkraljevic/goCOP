// Paket plato prepoznaje kvar mjerila koji traje satima, a ne sat: uređaj
// koji je zaglavio na jednoj vrijednosti i mjerilo koje je odskočilo daleko
// od prave razine i ondje se drži. Pojedinačni lažni skok (sat pa natrag)
// hvata paket skok; ovaj paket ono što ostane kad kvar potraje.
//
// Povod: u siječnju 2026. Donja Dubrava je po 4 do 15 sati javljala oko
// 1385 cm umjesto oko −120, a Siga i Dalj oko 1620 cm; Orešac (radar na
// potoku) u rujnu i listopadu 2026. satima stoji na −372 cm umjesto oko
// 48 cm. Svaki sat takvog niza blizu je prethodnom, pa ga filtar skoka
// pušta.
//
// Pravilo (Odluke (25. 10. 2026.)):
//
//   - Zaglavljen uređaj: automatska očitanja ostaju unutar 1 cm dulje od
//     najduljeg normalnog trajanja iste vrijednosti na toj letvi (Trajanje,
//     naučeno iz arhive), i barem jedno od: (a) susjedna letva iste rijeke
//     se u to vrijeme pomaknula više od svojeg praga (Susjed.Prag), (b) u
//     tu se vrijednost ušlo skokom većim od praga skoka letve, (c)
//     vrijednost je daleko izvan uobičajenog raspona letve.
//   - Odskočilo mjerilo: razina je skočila više od praga skoka letve i
//     drži se daleko od stare razine. Kvar je kad je barem dva očitanja
//     daleko izvan uobičajenog raspona, a u to se ušlo skokom većim od
//     dvostrukog praga (ili nakon rupe dulje od dana), ili kad se razina
//     unutar dana vratila na staru skokom, a susjedna letva se u to vrijeme
//     pomaknula manje od pola skoka. Zaglavljena ne mora biti: Donja
//     Dubrava je u kvaru skakala ±40 cm oko 1385. Kvar je i dio daleko
//     izvan raspona u koji se ušlo polako, a izašlo skokom većim od
//     dvostrukog praga (izlazni): Donja Dubrava se 2012.–2014. penjala po
//     stotinjak cm u satu do 1080 cm, ondje stajala i u satu pala za
//     500–1100 cm.
//
// Očitanja se zbog toga ne brišu ni ne odbacuju. Prognoza ne uzima sate
// kvara samo kad ga potvrđuje susjedna letva (Kvar.Susjed); inače stoji
// napomena, a vrijednosti ostaju u računu.
package plato

import (
	"math"
	"slices"
	"sort"
	"time"
)

const (
	// Pojas: vrijednosti unutar 1 cm su ista vrijednost (zaokruživanje,
	// valić).
	Pojas = 1.0
	// NajvecaRupa je najdulji razmak između dva očitanja istog niza; dulja
	// rupa prekida niz iste vrijednosti.
	NajvecaRupa = 3 * time.Hour
	// PrijeKvara: razina prije kvara traži se najviše toliko unatrag. Ako
	// je prethodno očitanje starije, ne zna se je li se u kvar ušlo skokom.
	PrijeKvara = 24 * time.Hour
	// RupaOdskoka je najdulji razmak unutar niza koji je odskočio.
	RupaOdskoka = 6 * time.Hour
	// NajduljiPovratak: odskok se računa kao vraćen kad se razina vrati
	// na staru unutar dana od skoka.
	NajduljiPovratak = 24 * time.Hour
	// DonjiPomak i GornjiPomak su percentili vrijednosti susjeda između
	// kojih se mjeri njegov pomak (PomakU).
	DonjiPomak, GornjiPomak = 0.05, 0.95
	// BezSusjeda: odskok letve bez susjeda s očitanjima kvar je kad su skok
	// u nj i povratak veći od praga puta 1,5 (ostarBezSusjeda).
	BezSusjeda = 1.5
	// UlazDna: razina susjeda „s početka” kvara su njegova očitanja do šest
	// sati prije i poslije početka (naDnu).
	UlazDna = 6 * time.Hour
	// DizanjeDna u cm: susjed koji se za kvara digne više od toga iznad
	// razine s početka kaže da je voda kod letve narasla, pa vrijednost nije
	// dno mjerila.
	DizanjeDna = 5.0
)

// Tocka je jedno automatsko očitanje letve u cm.
type Tocka struct {
	Kad time.Time
	Cm  float64
}

// Raspon je granica „daleko izvan uobičajenog”: vrijednost ispod Od ili
// iznad Do (RasponIzNiza). Najniza je najniža vrijednost u arhivi, a Nisko
// granica niske vode (medijan godišnjih 10. percentila).
type Raspon struct{ Od, Do, Najniza, Nisko float64 }

// Daleko javlja je li vrijednost daleko izvan uobičajenog raspona; letva
// bez raspona (nil) nema takvih.
func (r *Raspon) Daleko(cm float64) bool { return r != nil && (cm < r.Od || cm > r.Do) }

// Susjed je letva iste rijeke s kojom se kvar provjerava.
type Susjed struct {
	Letva string
	// Prag je pomak susjeda u cm (najviši minus najniži u vremenu kvara)
	// koji potvrđuje zaglavljen uređaj (PragSusjeda).
	Prag float64
	// Raspon susjeda: njegove vrijednosti daleko izvan raspona (njegov
	// kvar) ne broje se u pomak.
	Raspon *Raspon
}

// Svojstva letve iz kojih se prepoznaje kvar.
type Svojstva struct {
	Trajanje time.Duration // najdulje normalno trajanje iste vrijednosti
	Prag     float64       // prag skoka letve u cm u satu (paket skok)
	Raspon   *Raspon       // nil kad letva nema arhive
	Susjedi  []Susjed
	// sredina je uobičajena razina letve bez arhive: medijan očitanja koja
	// se gledaju (Prepoznaj). Po njoj se zna koja je od dvije razine
	// kvar.
	sredina    float64
	imaSredinu bool
}

// NajmanjeZaSredinu: letva bez arhive dobiva sredinu tek iz dana očitanja.
const NajmanjeZaSredinu = 24

// dalje javlja je li vrijednost v dalje od uobičajene razine letve nego
// razina z prije nje, pa je kvar v, a ne z. Radar Orešca bez arhive
// izmjenjuje 48 cm (prava razina, većina sati) i −372 cm (kvar): skok s
// −372 na 48 cm je povratak, a ne kvar. Letva s arhivom ima raspon, pa
// vrijedi uvijek.
func (s Svojstva) dalje(v, z float64) bool {
	return !s.imaSredinu || math.Abs(v-s.sredina) > math.Abs(z-s.sredina)
}

// saSredinom su svojstva sa sredinom iz očitanja, za letvu bez raspona.
func (s Svojstva) saSredinom(t []Tocka) Svojstva {
	if s.Raspon == nil && len(t) >= NajmanjeZaSredinu {
		s.sredina, s.imaSredinu = medijanOd(t, 0, len(t)-1), true
	}
	return s
}

// Vrsta kvara.
type Vrsta int

const (
	// Zaglavljen: uređaj stoji na istoj vrijednosti.
	Zaglavljen Vrsta = iota
	// Odskocio: mjerilo je skočilo daleko od prave razine i ondje se drži.
	Odskocio
)

// Pomak je koliko se susjedna letva pomaknula u vremenu kvara.
type Pomak struct {
	Letva string
	Cm    float64
}

// Kvar je dio niza za koji pravilo kaže da nije mjerenje nego kvar mjerila.
type Kvar struct {
	Vrsta  Vrsta
	Od, Do time.Time // prvo i zadnje očitanje u kvaru
	// Cm je vrijednost na kojoj je uređaj zaglavio, odnosno srednja
	// vrijednost odskočenog niza.
	Cm float64
	// Prije je razina zadnjeg očitanja prije kvara, kad ga ima unutar
	// PrijeKvara (ImaPrije).
	Prije    float64
	ImaPrije bool
	// Natrag je očitanje kojim se odskok vratio na staru razinu (Vratio).
	Natrag time.Time
	// Izlaz je skok u cm kojim je mjerilo izašlo iz dijela daleko izvan
	// raspona u koji je ušlo polako (izlazni); nula za ostale kvarove.
	Izlaz float64
	// BaremOd: kvar je počeo prije najstarijeg pročitanog očitanja, pa je
	// Od samo granica (Procjenitelj.Kvarovi čita najviše NajdaljeUnatrag).
	BaremOd bool
	// Susjed: susjedna letva potvrđuje kvar (zaglavljen: pomaknula se više
	// od svojeg praga; odskočio: pomaknula se manje od pola skoka). Samo
	// tada prognoza sate kvara ne uzima.
	Susjed *Pomak
	Skokom bool // u kvar se ušlo skokom većim od praga letve
	Izvan  bool // vrijednost je daleko izvan uobičajenog raspona
	// NaDnu: zaglavljena vrijednost je vjerojatno dno mjerila, a ne kvar:
	// voda je ispod dosega mjerila (naDnu). DnoSusjed: to kažu susjedi
	// (nijedan se za kvara nije digao iznad razine s početka); inače je to
	// samo najniža vrijednost u arhivi, pa je dno tek moguće.
	NaDnu     bool
	DnoSusjed bool
	Vratio    bool // odskok se unutar dana vratio na staru razinu
	Traje     bool // zadnje očitanje niza je u kvaru
}

// BezPrognoze javlja smije li prognoza ne uzeti sate kvara: kvar potvrđuje
// susjedna letva, a vrijednost nije dno mjerila. Na dnu mjerila (Siga 19 cm
// pri rekordno niskoj vodi u rujnu i listopadu 2026.) prava razina je
// najviše toliko, a bez nje bi letva tjednima bila bez mjerenja, dok
// rekonstrukcija iz susjeda popuni najviše dan.
func (k Kvar) BezPrognoze() bool { return k.Susjed != nil && !k.NaDnu }

// Sati je koliko kvar traje, od prvog do zadnjeg očitanja.
func (k Kvar) Sati() float64 { return k.Do.Sub(k.Od).Hours() }

// SatiDoPovratka je koliko je odskok trajao do očitanja kojim se vratio;
// Sati kad se nije vratio.
func (k Kvar) SatiDoPovratka() float64 {
	if k.Natrag.IsZero() {
		return k.Sati()
	}
	return k.Natrag.Sub(k.Od).Hours()
}

// U javlja je li trenutak t unutar kvara.
func (k Kvar) U(t time.Time) bool { return !t.Before(k.Od) && !t.After(k.Do) }

// Prepoznaj traži kvarove u automatskim očitanjima letve (poredanima po
// vremenu, bez lažnih skokova) uz očitanja susjeda po šifri.
func Prepoznaj(tocke []Tocka, s Svojstva, susjedi map[string][]Tocka) []Kvar {
	s = s.saSredinom(tocke)
	z := zaglavljeni(tocke, s, susjedi)
	o := odskoci(tocke, s, susjedi, z)
	o = append(o, izlazni(tocke, s, susjedi, o)...)
	out := slices.Concat(o, bezObuhvacenih(z, o))
	sort.SliceStable(out, func(i, j int) bool { return out[i].Od.Before(out[j].Od) })
	return out
}

// BezKvarova je niz susjeda u kojem su očitanja u kvarovima koje letva
// pokazuje sama, bez susjeda (skokom, izvan raspona, oštar odskok),
// označena kao nepoznata (NaN): susjed koji je u to vrijeme i sam bio u
// kvaru ne potvrđuje ni ne opovrgava tuđi kvar (PomakU). Izbaciti ih ne bi
// bilo dosta: Botovo bi 2013. „potvrdila” Donja Dubrava s ostatkom svojeg
// kvara.
func BezKvarova(niz []Tocka, s Svojstva) []Tocka {
	s.Susjedi = nil
	kvarovi := Prepoznaj(niz, s, nil)
	if len(kvarovi) == 0 {
		return niz
	}
	out := slices.Clone(niz)
	for i := range out {
		if slices.ContainsFunc(kvarovi, func(k Kvar) bool { return k.U(out[i].Kad) }) {
			out[i].Cm = math.NaN()
		}
	}
	return out
}

// bezObuhvacenih su zaglavljeni kvarovi koji ne leže cijeli u odskoku
// (radar koji odskoči na −372 i ondje stoji opisan je jednom, kao odskok),
// osim onih koje potvrđuje susjed, a odskok ne.
func bezObuhvacenih(z, o []Kvar) []Kvar {
	return slices.DeleteFunc(z, func(k Kvar) bool {
		return slices.ContainsFunc(o, func(x Kvar) bool {
			return x.U(k.Od) && x.U(k.Do) && (k.Susjed == nil || x.Susjed != nil)
		})
	})
}

// krajNiza je zadnja točka niza iste vrijednosti koji počinje u i.
func krajNiza(t []Tocka, i int) int {
	lo, hi := t[i].Cm, t[i].Cm
	j := i
	for j+1 < len(t) && t[j+1].Kad.Sub(t[j].Kad) <= NajvecaRupa {
		v := t[j+1].Cm
		if math.Max(hi, v)-math.Min(lo, v) > Pojas {
			break
		}
		lo, hi = math.Min(lo, v), math.Max(hi, v)
		j++
	}
	return j
}

// zaglavljeni su nizovi iste vrijednosti dulji od Trajanja koje nešto
// potvrđuje.
func zaglavljeni(t []Tocka, s Svojstva, susjedi map[string][]Tocka) []Kvar {
	var out []Kvar
	for i := 0; i < len(t); {
		j := krajNiza(t, i)
		if dovoljnoDug(t, i, j, s) {
			if k := zaglavljen(t, i, j, s, susjedi); k.Susjed != nil || k.Skokom || k.Izvan {
				out = append(out, k)
			}
		}
		i = j + 1
	}
	return out
}

// dovoljnoDug javlja je li niz iste vrijednosti od i do j dulji od
// najduljeg normalnog trajanja letve; daleko izvan raspona dosta je pola
// toga (najmanje NajkraceTrajanje): Čačinci-Vojlovica je 2014. dva dana
// stajala na 1339 cm, a pri maloj vodi normalno stoji i tri i pol dana.
// Samo NajkraceTrajanje bilo bi premalo: Břeclav ispod akumulacija u rujnu
// 2024. pri velikoj vodi stoji na 415 ± 1 cm sedam sati.
func dovoljnoDug(t []Tocka, i, j int, s Svojstva) bool {
	d := t[j].Kad.Sub(t[i].Kad)
	return d >= s.Trajanje || d >= max(NajkraceTrajanje, s.Trajanje/2) && s.Raspon.Daleko(t[i].Cm)
}

// zaglavljen opisuje niz iste vrijednosti od i do j.
func zaglavljen(t []Tocka, i, j int, s Svojstva, susjedi map[string][]Tocka) Kvar {
	k := Kvar{Vrsta: Zaglavljen, Od: t[i].Kad, Do: t[j].Kad, Cm: t[i].Cm, Traje: j == len(t)-1}
	k.Prije, k.ImaPrije = prije(t, i)
	k.Skokom = k.ImaPrije && math.Abs(k.Cm-k.Prije) > s.Prag && stalnaPrije(t, i, s.Prag) && s.dalje(k.Cm, k.Prije)
	k.Izvan = s.Raspon.Daleko(k.Cm)
	k.NaDnu, k.DnoSusjed = naDnu(k, s, susjedi)
	k.Susjed = najviseMaknut(s.Susjedi, susjedi, k.Od, k.Do)
	return k
}

// naDnu javlja je li zaglavljena vrijednost vjerojatno dno mjerila (voda
// ispod njegova dosega), a ne kvar. Kad susjedi imaju očitanja, to kažu
// oni: vrijednost je niska (ispod Raspon.Nisko), a nijedan se susjed za
// kvara nije digao iznad razine s početka; spuštao se smije (Siga 19 cm u
// rujnu i listopadu 2026.: Batina je pala još 40 cm, a nijednom se nije
// vratila na razinu s kojom je kvar počeo). Bez susjeda moguće je dno samo
// najniža vrijednost u arhivi. Vrijednost daleko izvan raspona nije dno.
func naDnu(k Kvar, s Svojstva, nizovi map[string][]Tocka) (dno, poSusjedu bool) {
	if s.Raspon == nil || k.Izvan {
		return false, false
	}
	gledano := false
	for _, n := range s.Susjedi {
		digao, ima := digaoSe(nizovi[n.Letva], k.Od, k.Do)
		if digao {
			return false, false
		}
		gledano = gledano || ima
	}
	if gledano {
		return k.Cm <= s.Raspon.Nisko, k.Cm <= s.Raspon.Nisko
	}
	return k.Cm <= s.Raspon.Najniza+Pojas, false
}

// digaoSe javlja je li se niz od od+UlazDna do do digao više od DizanjeDna
// iznad najviše razine oko početka (od ± UlazDna); ima je false kad
// očitanja nema dosta ili je neko od njih nepoznato (NaN).
func digaoSe(niz []Tocka, od, do time.Time) (digao, ima bool) {
	ulaz, poslije, ok := oPocetku(niz, od, do)
	if !ok || len(ulaz) == 0 || len(poslije) < 2 {
		return false, false
	}
	return percentil(poslije, GornjiPomak) > slices.Max(ulaz)+DizanjeDna, true
}

// oPocetku dijeli očitanja niza od od−UlazDna do do na ona oko početka i
// ona poslije; ok je false kad je neko nepoznato (NaN).
func oPocetku(niz []Tocka, od, do time.Time) (ulaz, poslije []float64, ok bool) {
	a := sort.Search(len(niz), func(i int) bool { return !niz[i].Kad.Before(od.Add(-UlazDna)) })
	for _, x := range niz[a:] {
		if x.Kad.After(do) {
			break
		}
		if math.IsNaN(x.Cm) {
			return nil, nil, false
		}
		if x.Kad.After(od.Add(UlazDna)) {
			poslije = append(poslije, x.Cm)
		} else {
			ulaz = append(ulaz, x.Cm)
		}
	}
	return ulaz, poslije, true
}

// StalnihPrije: skok u zaglavljenu vrijednost računa se tek kad su tri
// očitanja prije njega bila stalna (unutar praga): radar na suhom potoku
// (Teodorovac, 1 cm) koji dva sata javi 145 i 127 cm pa se vrati na 1 cm
// nije u 1 cm ušao skokom, nego iz kvara.
const StalnihPrije = 3

// stalnaPrije javlja jesu li StalnihPrije očitanja prije točke i unutar
// praga od zadnjeg i unutar PrijeKvara od i.
func stalnaPrije(t []Tocka, i int, prag float64) bool {
	if i < StalnihPrije || t[i].Kad.Sub(t[i-StalnihPrije].Kad) > PrijeKvara {
		return false
	}
	for _, x := range t[i-StalnihPrije : i-1] {
		if math.Abs(x.Cm-t[i-1].Cm) > prag {
			return false
		}
	}
	return true
}

// prije je razina očitanja prije točke i, kad nije starije od PrijeKvara.
func prije(t []Tocka, i int) (float64, bool) {
	if i == 0 || t[i].Kad.Sub(t[i-1].Kad) > PrijeKvara {
		return 0, false
	}
	return t[i-1].Cm, true
}

// najviseMaknut je susjed koji se od od do do pomaknuo najviše, a više od
// svojeg praga; nil kad takvog nema.
func najviseMaknut(susjedi []Susjed, nizovi map[string][]Tocka, od, do time.Time) *Pomak {
	var best *Pomak
	for _, s := range susjedi {
		m, ima := PomakU(nizovi[s.Letva], s.Raspon, od, do)
		if ima && m > s.Prag && (best == nil || m > best.Cm) {
			best = &Pomak{Letva: s.Letva, Cm: m}
		}
	}
	return best
}

// najmanjeMaknut je susjed koji se od od do do pomaknuo najmanje, a manje
// od granice; nil kad takvog nema.
func najmanjeMaknut(susjedi []Susjed, nizovi map[string][]Tocka, od, do time.Time, granica float64) *Pomak {
	var best *Pomak
	for _, s := range susjedi {
		m, ima := PomakU(nizovi[s.Letva], s.Raspon, od, do)
		if ima && m < granica && (best == nil || m < best.Cm) {
			best = &Pomak{Letva: s.Letva, Cm: m}
		}
	}
	return best
}

// PomakU je koliko se niz pomaknuo od od do do: razlika 95. i 5.
// percentila vrijednosti, bez vrijednosti daleko izvan raspona. Percentili,
// a ne najviša i najniža, jer i susjed ima pojedinačna kriva očitanja
// (Szentborbás 12. 10. 2026. javi 0 cm usred −126). Niz mora pokriti
// barem pola sati tog vremena (i barem dva očitanja), a nijedno očitanje u
// tom vremenu ne smije biti nepoznato (NaN, susjed u vlastitom kvaru:
// BezKvarova), inače se ne zna (ima = false).
func PomakU(niz []Tocka, r *Raspon, od, do time.Time) (float64, bool) {
	a := sort.Search(len(niz), func(i int) bool { return !niz[i].Kad.Before(od) })
	var v []float64
	for _, x := range niz[a:] {
		if x.Kad.After(do) {
			break
		}
		if math.IsNaN(x.Cm) {
			return 0, false
		}
		if !r.Daleko(x.Cm) {
			v = append(v, x.Cm)
		}
	}
	if len(v) < 2 || float64(len(v)) < do.Sub(od).Hours()/2 {
		return 0, false
	}
	return percentil(v, GornjiPomak) - percentil(v, DonjiPomak), true
}

// odskoci su nizovi koji su odskočili od stare razine, kad je to kvar.
// Razina u zaglavljenom kvaru nije stara razina: radar koji stoji na
// −372 cm pa se vrati na 49 cm nije odskočio na 49.
func odskoci(t []Tocka, s Svojstva, susjedi map[string][]Tocka, zaglavljeni []Kvar) []Kvar {
	var out []Kvar
	for i := 0; i < len(t); i++ {
		if i > 0 && slices.ContainsFunc(zaglavljeni, func(k Kvar) bool { return k.U(t[i-1].Kad) }) {
			continue
		}
		j, kvarovi := odskok(t, i, s, susjedi)
		out = append(out, kvarovi...)
		if j > i {
			i = j + 1 // očitanje kojim se vratio nije novi skok
		}
	}
	return out
}

// odskok gleda počinje li u i niz koji je odskočio od razine prije sebe; j
// je njegova zadnja točka (i kad ga nema). Niz koji se unutar dana vratio,
// a susjed se nije pomaknuo ni za pola skoka, cijeli je kvar, a i kad
// letva nema susjeda s očitanjima, ako je i skok u nj i povratak barem pola
// veći od praga (Lanžhot u lipnju 2021.: 35, 688, 36 cm); inače su kvar
// njegovi dijelovi daleko izvan raspona, kad se u njih skočilo više od
// dvostrukog praga. Od razine koja je i sama daleko izvan raspona ne
// počinje novi odskok: to je nastavak kvara ili povratak iz njega, a velika
// voda koja polako prijeđe granicu raspona nije skok.
func odskok(t []Tocka, i int, s Svojstva, susjedi map[string][]Tocka) (int, []Kvar) {
	z, imaPrije := prije(t, i)
	if !imaPrije {
		return odskokNakonRupe(t, i, s, susjedi)
	}
	j, ima := krajOdskoka(t, i, z, s)
	if !ima {
		return j, nil
	}
	return j, kvaroviOdskoka(t, i, j, z, s, susjedi)
}

// krajOdskoka je zadnja točka niza koji je od točke i odskočio od razine
// z; ima je false kad to nije odskok (nema skoka, z je i sam daleko izvan
// raspona, odskočila je samo jedna točka, ili je niz bliži uobičajenoj
// razini od z).
func krajOdskoka(t []Tocka, i int, z float64, s Svojstva) (int, bool) {
	if math.Abs(t[i].Cm-z) <= s.Prag || s.Raspon.Daleko(z) {
		return i, false
	}
	j := i
	for j+1 < len(t) && t[j+1].Kad.Sub(t[j].Kad) <= RupaOdskoka && math.Abs(t[j+1].Cm-z) > s.Prag {
		j++
	}
	return j, j > i && s.dalje(medijanOd(t, i, j), z)
}

// kvaroviOdskoka su kvarovi u nizu od i do j koji je odskočio od z.
func kvaroviOdskoka(t []Tocka, i, j int, z float64, s Svojstva, susjedi map[string][]Tocka) []Kvar {
	if k := odskocen(t, i, j, z, s, susjedi); k.Vratio && (k.Susjed != nil || ostarBezSusjeda(t, i, j, z, s, susjedi)) {
		return []Kvar{k}
	}
	if najveciKorak(t, i, j) <= 2*s.Prag {
		return nil
	}
	return dalekiDijelovi(t, i, j, z, s, susjedi)
}

// ostarBezSusjeda javlja je li niz od i do j odskočio od z i vratio se
// skokovima većim od praga i pol, a nijedan susjed u to vrijeme nema
// očitanja (ili ih letva nema). Pola praga više od pravila skoka, jer ovdje
// nema susjeda koji bi rekao je li to bila prava voda: na arhivi 2012.–2026.
// tako se ne javlja nijedan manevar elektrane ni val.
func ostarBezSusjeda(t []Tocka, i, j int, z float64, s Svojstva, susjedi map[string][]Tocka) bool {
	for _, n := range s.Susjedi {
		if _, ima := PomakU(susjedi[n.Letva], n.Raspon, t[i-1].Kad, t[j].Kad); ima {
			return false
		}
	}
	return math.Abs(t[i].Cm-z) > BezSusjeda*s.Prag && math.Abs(t[j+1].Cm-t[j].Cm) > BezSusjeda*s.Prag
}

// odskokNakonRupe: niz koji počinje bez razine prije sebe (prvi ili nakon
// rupe dulje od dana) kvar je samo kad je daleko izvan raspona. Skok se ne
// vidi, pa susjed kvar potvrđuje kad se u to vrijeme pomaknuo manje od
// praga skoka letve: toliko se letva u satu ni ne mijenja.
func odskokNakonRupe(t []Tocka, i int, s Svojstva, susjedi map[string][]Tocka) (int, []Kvar) {
	j := krajDalekog(t, i, len(t)-1, s.Raspon)
	if j == i {
		return i, nil
	}
	k := Kvar{Vrsta: Odskocio, Od: t[i].Kad, Do: t[j].Kad, Cm: medijanOd(t, i, j), Izvan: true, Traje: j == len(t)-1}
	k.Susjed = najmanjeMaknut(s.Susjedi, susjedi, t[i].Kad, t[j].Kad, s.Prag)
	return j, []Kvar{k}
}

// krajDalekog je zadnja točka niza vrijednosti daleko izvan raspona koji
// počinje u i, najdalje do kraja; i kad i nije daleko.
func krajDalekog(t []Tocka, i, kraj int, r *Raspon) int {
	if !r.Daleko(t[i].Cm) {
		return i
	}
	j := i
	for j < kraj && t[j+1].Kad.Sub(t[j].Kad) <= RupaOdskoka && r.Daleko(t[j+1].Cm) {
		j++
	}
	return j
}

// odskocen opisuje niz od i do j koji je odskočio od razine z.
func odskocen(t []Tocka, i, j int, z float64, s Svojstva, susjedi map[string][]Tocka) Kvar {
	k := Kvar{Vrsta: Odskocio, Od: t[i].Kad, Do: t[j].Kad, Cm: medijanOd(t, i, j), Prije: z, ImaPrije: true, Skokom: true,
		Traje: j == len(t)-1}
	k.vrati(t, i, j, z, s.Prag)
	k.Izvan = s.Raspon.Daleko(k.Cm)
	k.Susjed = najmanjeMaknut(s.Susjedi, susjedi, t[i-1].Kad, t[j].Kad, math.Abs(k.Cm-z)/2)
	return k
}

// dalekiDijelovi su kvarovi u nizu od i do j koji je odskočio od z: svaki
// dio od barem dva očitanja daleko izvan raspona.
func dalekiDijelovi(t []Tocka, i, j int, z float64, s Svojstva, susjedi map[string][]Tocka) []Kvar {
	var out []Kvar
	for a := i; a <= j; a++ {
		b := krajDalekog(t, a, j, s.Raspon)
		if b > a {
			k := odskocen(t, a, b, z, s, susjedi)
			k.Vratio, k.Natrag = false, time.Time{}
			if b == j {
				k.vrati(t, i, j, z, s.Prag)
			}
			out = append(out, k)
		}
		a = b
	}
	return out
}

// vrati označi kvar koji se od i do j odskočio od z kao vraćen, s
// očitanjem kojim se vratio, kad se vratio (vratioSe).
func (k *Kvar) vrati(t []Tocka, i, j int, z, prag float64) {
	if k.Vratio = vratioSe(t, i, j, z, prag); k.Vratio {
		k.Natrag = t[j+1].Kad
	}
}

// izlazni su kvarovi u dijelovima niza daleko izvan raspona (barem dva
// očitanja) koje još ne pokriva nijedan odskok, a iz kojih je mjerilo
// izašlo skokom većim od dvostrukog praga na vrijednost unutar raspona.
// Ulaz u njih ne mora biti skok: mjerilo se može polako penjati do svoje
// granice i ondje stajati. Prava velika voda tako ne završava: na arhivi
// 2012.–2026. nijedna nije pala za više od 30 cm u satu, a svi takvi kvarovi
// više od dvostrukog praga. Susjed kvar potvrđuje kad se od početka do
// izlaza pomaknuo manje od pola izlaznog skoka.
func izlazni(t []Tocka, s Svojstva, susjedi map[string][]Tocka, vec []Kvar) []Kvar {
	var out []Kvar
	for a := 0; a < len(t); a++ {
		b := krajDalekog(t, a, len(t)-1, s.Raspon)
		if b > a && izlazIzDalekog(t, b, s) && !preklapa(vec, t[a].Kad, t[b].Kad) {
			out = append(out, izlazniKvar(t, a, b, s, susjedi))
		}
		a = b
	}
	return out
}

// izlazIzDalekog javlja je li se iz točke b (daleko izvan raspona) idućim
// očitanjem, unutar RupaOdskoka, skočilo više od dvostrukog praga na
// vrijednost unutar raspona.
func izlazIzDalekog(t []Tocka, b int, s Svojstva) bool {
	if b+1 >= len(t) || t[b+1].Kad.Sub(t[b].Kad) > RupaOdskoka || s.Raspon.Daleko(t[b+1].Cm) {
		return false
	}
	return math.Abs(t[b+1].Cm-t[b].Cm) > 2*s.Prag
}

// izlazniKvar opisuje dio od a do b iz kojeg se izašlo skokom.
func izlazniKvar(t []Tocka, a, b int, s Svojstva, susjedi map[string][]Tocka) Kvar {
	k := Kvar{Vrsta: Odskocio, Od: t[a].Kad, Do: t[b].Kad, Cm: medijanOd(t, a, b), Izvan: true, Izlaz: t[b+1].Cm - t[b].Cm}
	k.Prije, k.ImaPrije = prije(t, a)
	k.Susjed = najmanjeMaknut(s.Susjedi, susjedi, t[a].Kad, t[b+1].Kad, math.Abs(k.Izlaz)/2)
	return k
}

// preklapa javlja dira li koji od kvarova vrijeme od od do do.
func preklapa(kvarovi []Kvar, od, do time.Time) bool {
	return slices.ContainsFunc(kvarovi, func(k Kvar) bool { return !k.Od.After(do) && !k.Do.Before(od) })
}

// medijanOd je srednja vrijednost točaka od i do j.
func medijanOd(t []Tocka, i, j int) float64 {
	v := make([]float64, 0, j-i+1)
	for _, x := range t[i : j+1] {
		v = append(v, x.Cm)
	}
	return medijan(v)
}

// najveciKorak je najveća promjena između dva uzastopna očitanja od i−1
// do j.
func najveciKorak(t []Tocka, i, j int) float64 {
	m := 0.0
	for k := i; k <= j; k++ {
		m = math.Max(m, math.Abs(t[k].Cm-t[k-1].Cm))
	}
	return m
}

// vratioSe javlja je li se niz koji je od i do j odskočio od razine z
// vratio na nju unutar NajduljiPovratak: iduće očitanje je blizu z, a do
// njega se skočilo više od praga.
func vratioSe(t []Tocka, i, j int, z, prag float64) bool {
	if j+1 >= len(t) || t[j+1].Kad.Sub(t[j].Kad) > RupaOdskoka || t[j+1].Kad.Sub(t[i].Kad) > NajduljiPovratak {
		return false
	}
	n := t[j+1].Cm
	return math.Abs(n-z) <= prag && math.Abs(n-t[j].Cm) > prag
}

// medijan je srednja vrijednost (ne praznih).
func medijan(v []float64) float64 {
	p := slices.Sorted(slices.Values(v))
	n := len(p)
	return (p[(n-1)/2] + p[n/2]) / 2
}
