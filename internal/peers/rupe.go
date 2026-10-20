package peers

// Rupe u nizu (G4b u planu WEB synca, §3, oznaka rupe/1). Granica
// knjige je najveći version_id po ključu (autor|kanal) i drugima kaže „imam
// sve do toga”. Računalo koje nešto odbije (Accept, ograda povijesti) drži
// niz s rupom. Kad ga proslijedi, treće računalo upiše noviju verziju,
// granica mu prijeđe preko rupe, a autor mu poslije šalje tek iznad nje, pa
// odbijeno ne stigne nikad.
//
// Granica koja se šalje ostaje ista, pa delta, granica odbijenih (G4), ploča
// i stariji čvorovi rade kao prije. Posrednik nikad ništa ne zadržava. Uz
// granicu svako računalo trajno vodi, po tuđem ključu, dva raspona
// version_id (od, do] (tablica rupe_knjige, rupe_baza.go):
//   - odbijeno: ovo računalo je verzije iz raspona samo odbilo. Ne traži
//     ih, ali drugima za njih ne jamči.
//   - trazi: verzija iz raspona možda nema, a treba ih. Traži ih od
//     računala koja za njih jamče.
//
// Invarijanta: svaka verzija tuđeg ključa do granice knjige koje nema u
// knjizi leži u odbijeno ili trazi. Iznimke su odlukom prihvaćene:
// namjerno brisanje povijesti (Odluke (17. 10. 2026.), 2a), neispravne
// verzije (G3), sat autora vraćen unatrag i nizovi starog posrednika (1A).
// Vlastiti ključ nikad nema rupu, ni zapisanu ni javljenu.
//
// Uz oznaku rupe/1 (rupe_razgovor.go):
//   - iza granice odbijenih obje strane javi gdje traže (poruka rupe);
//   - pošiljatelj koji tik iza početka tuđe rupe jamči šalje ključ od
//     početka rupe, na kraju delte (popuna), i to javi (Popuna u delti);
//   - uz deltu pošiljatelj javi dio poslanoga za koji ne jamči (Rupe);
//   - primatelj prije upisa trajno zapiše što je odbio i što mu je stiglo s
//     rupom (prosiri), a tek nakon uspješnog upisa makne ono što mu je
//     pokriveno (suzi). Pad između ostavi rupu veću nego što jest, a to
//     košta samo jedno ponovno slanje. Je li ograda od odluka accepta
//     popustila, gleda tek pod bravom, nakon pravila koje zapisuje
//     (podOgradom): pravilo se može proširiti dok razgovor ili uvoz čeka
//     bravu, a što je odbijeno po užem pravilu, tada se traži.
//
// Raspon je jedan po vrsti i ključu: spajanje uzima najmanji raspon koji
// drži oba, pa je veći nego što treba. To isto košta samo ponovno slanje.
//
// Jamstvo pošiljatelja čita se prije delte. Rupa koja se zapiše usred
// razgovora (drugi razgovor ili uvoz istodobno upiše niz s rupom) poveća
// brojač rupeMijenja, i to nakon zapisa, a prije upisa verzija. Kad se
// brojač od početka razgovora promijenio, pošiljatelj ne jamči ni za što
// iznad svoje granice knjige s početka razgovora.

import (
	"maps"
	"slices"
	"strings"
	"time"

	"gocop/internal/ledger"
)

// kindRupe je poruka rupa, odmah iza granice odbijenih (nakonGranica)
const kindRupe = "rupe"

// beskraj je veće od svakog version_id: UUID ima samo znamenke, a–f i '-'
const beskraj = "~"

// Ograde onoga što druga strana javi o rupama: ključeva u poruci, znakova
// ključa i znakova version_id u rasponu
const (
	najviseTrazenih = 4096
	najduljiKljuc   = 256
	najduljaOznaka  = 64
)

// tolerancijaRuba: AddDate na kraju mjeseca rub ograde pomakne unatrag
// najviše tri dana, pa to još nije sat vraćen unatrag (rubUnatrag)
const tolerancijaRuba = 96 * time.Hour

// raspon su version_id jednog ključa iza Od, do uključivo Do. Prazan je
// kad Do nije veći od Od; prazan Od je od početka niza.
type raspon struct {
	Od string `json:"od"`
	Do string `json:"do"`
}

// iznad je raspon svega iza od
func iznad(od string) raspon { return raspon{Od: od, Do: beskraj} }

func (a raspon) prazan() bool { return a.Do <= a.Od }

// cist je isti raspon, a prazan uvijek kao raspon{}, pa se rasponi smiju
// uspoređivati
func (a raspon) cist() raspon {
	if a.prazan() {
		return raspon{}
	}
	return a
}

// uz je najmanji raspon koji drži oba
func (a raspon) uz(b raspon) raspon {
	if a.prazan() {
		return b.cist()
	}
	if b.prazan() {
		return a
	}
	return raspon{Od: min(a.Od, b.Od), Do: max(a.Do, b.Do)}
}

// presjek je zajednički dio
func (a raspon) presjek(b raspon) raspon {
	return raspon{Od: max(a.Od, b.Od), Do: min(a.Do, b.Do)}.cist()
}

// bez je a bez dijela koji pokriva b, zaokruženo na raspon: kad b pokrije
// samo sredinu, ostaje cijeli a
func (a raspon) bez(b raspon) raspon {
	switch {
	case a.presjek(b).prazan():
		return a.cist()
	case b.Od <= a.Od:
		return raspon{Od: b.Do, Do: a.Do}.cist()
	case b.Do >= a.Do:
		return raspon{Od: a.Od, Do: b.Od}
	}
	return a
}

// ispravan je raspon kakav druga strana smije javiti: neprazan i s
// oznakama ne duljima od version_id
func (a raspon) ispravan() bool {
	return !a.prazan() && len(a.Od) <= najduljaOznaka && len(a.Do) <= najduljaOznaka
}

// rupaKljuca je ono što ovo računalo zna o rupama u nizu jednog ključa
type rupaKljuca struct {
	odbijeno raspon // samo je ovo računalo odbilo: ne traži se
	trazi    raspon // možda nema, a treba: traži se od drugih
}

// nejamci je raspon za koji ovo računalo drugima ne jamči da ga ima cijelog
func (k rupaKljuca) nejamci() raspon { return k.odbijeno.uz(k.trazi) }

func (k rupaKljuca) prazna() bool { return k.odbijeno.prazan() && k.trazi.prazan() }

// sirenjem je zapis kad je pravilo čuvanja prošireno: što je odbijeno po
// užem pravilu, novo možda prima, pa se traži
func (k rupaKljuca) sirenjem() rupaKljuca { return rupaKljuca{trazi: k.trazi.uz(k.odbijeno)} }

// veci javlja da novo ne jamči za nešto za što je staro jamčilo
func veci(novo, staro rupaKljuca) bool {
	return novo.nejamci().uz(staro.nejamci()) != staro.nejamci()
}

// praviloCuvanja je pravilo pod kojim ograda prihvata odbija: koliko
// mjeseci tuđih očitanja računalo prima (0: sva), letve koje prati u
// cijelosti (poredane) i rub razdoblja, prije kojega odbija
type praviloCuvanja struct {
	mjeseci int
	pracene []string
	rub     time.Time
}

// siriOd javlja da sadašnje pravilo možda prima nešto što je staro
// odbijalo. Suženje (manje mjeseci, maknuta letva) ne javlja ništa.
func siriOd(staro, sada praviloCuvanja) bool {
	return siriRazdoblje(staro.mjeseci, sada.mjeseci) || novaPracena(staro.pracene, sada.pracene) || rubUnatrag(staro.rub, sada.rub)
}

// siriRazdoblje: staro je razdoblje ograničeno, a sada nije ili je dulje
func siriRazdoblje(staro, sada int) bool {
	return staro > 0 && (sada == 0 || sada > staro)
}

// novaPracena: sada se prati letva koja se prije nije pratila
func novaPracena(staro, sada []string) bool {
	for _, letva := range sada {
		if !slices.Contains(staro, letva) {
			return true
		}
	}
	return false
}

// rubUnatrag: rub ograde otišao je unatrag više od tolerancijaRuba (sat
// vraćen); bez ruba se ne odbija ništa, pa to javlja siriRazdoblje
func rubUnatrag(staro, sada time.Time) bool {
	return !sada.IsZero() && sada.Before(staro.Add(-tolerancijaRuba))
}

// zapis je pravilo koje se sprema uz zapis rupa: sadašnje, s najvećim
// rubom dosad. Nakon proširenja (sire) rub je sadašnji, pa sljedeće
// proširenje mjeri od njega.
func (sada praviloCuvanja) zapis(staro praviloCuvanja, sire bool) praviloCuvanja {
	if !sire && staro.rub.After(sada.rub) {
		sada.rub = staro.rub
	}
	return sada
}

// kanalOcitanja: kanal očitanja ili zajednički kanal, gdje su očitanja
// letvi izvan dionica; samo njih odbija ograda povijesti
func kanalOcitanja(kanal string) bool {
	return kanal == "" || strings.HasPrefix(kanal, ledger.ChannelReadings+"/")
}

// pocetakRupa je zapis rupa koji razgovor ili izvoz uzme prije čitanja
// verzija (rupe_baza.go)
type pocetakRupa struct {
	ja   string // ovo računalo: vlastiti ključ nikad nema rupu
	broj uint64 // rupeMijenja prije čitanja zapisa
	// kljucevi su zapis rupa, uz prošireno pravilo s odbijenim kao trazi
	kljucevi map[string]rupaKljuca
	poznato  bool // zapis se dao pročitati; inače se ne jamči ni za što
	// virtualno: jednokratni korak (prviKorak) još nije zapisan, a ograda
	// je uključena, pa se računa kao da jest
	virtualno bool
	zapisati  bool // pravilo čuvanja treba zapisati (prvi korak ili proširenje)
	dogovor   bool // obje strane imaju rupe/1 (razmijeniRupe)
}

// vlastiti javlja je li k ključ ovog računala
func (p pocetakRupa) vlastiti(k string) bool {
	autor, _ := ledger.SplitFrontierKey(k)
	return autor == p.ja
}

// nejamci je raspon ključa k za koji ovo računalo ne jamči, uz raspon
// sumnje (rupa zapisana usred razgovora). Vlastiti ključ nema rupu; kad
// zapis nije pročitan, ne jamči se ni za što, a dok jednokratni korak nije
// zapisan, ni za tuđa očitanja.
func (p pocetakRupa) nejamci(k string, sumnja raspon) raspon {
	_, kanal := ledger.SplitFrontierKey(k)
	switch {
	case p.vlastiti(k):
		return raspon{}
	case !p.poznato, p.virtualno && kanalOcitanja(kanal):
		return raspon{Do: beskraj}
	}
	return p.kljucevi[k].nejamci().uz(sumnja)
}

// jamciIza javlja da ovo računalo jamči za verzije tik iza od u ključu k,
// pa drugoj strani kojoj tu nedostaje može dati barem nešto
func (p pocetakRupa) jamciIza(k, od string) bool {
	ne := p.nejamci(k, raspon{})
	return ne.prazan() || od < ne.Od || od >= ne.Do
}

// trazim su rasponi koje ovo računalo traži: trazi tuđih ključeva u
// kanalima koje prati (prati nil: svi), poredano po ključu, najviše
// najviseTrazenih; ostali čekaju red
func (p pocetakRupa) trazim(prati func(string) bool) map[string]raspon {
	out := map[string]raspon{}
	for _, k := range slices.Sorted(maps.Keys(p.kljucevi)) {
		if t := p.kljucevi[k].trazi; !t.prazan() && p.tudjPracen(k, prati) && len(out) < najviseTrazenih {
			out[k] = t
		}
	}
	return out
}

// tudjPracen: ključ drugog autora u kanalu koji se prati (prati nil: svi)
func (p pocetakRupa) tudjPracen(k string, prati func(string) bool) bool {
	_, kanal := ledger.SplitFrontierKey(k)
	return !p.vlastiti(k) && (prati == nil || prati(kanal))
}

// zaIzvoz su, po ključu u izvozu (arhiva knjige, COP paket), rupe do
// najveće izvezene verzije. Zapis se čita prije verzija; kad se brojač
// rupeMijenja otada promijenio (broj), ne jamči se ni za što izvezeno.
func (p pocetakRupa) zaIzvoz(broj uint64, verzije []ledger.Version) map[string]raspon {
	sumnja := raspon{}
	if broj != p.broj {
		sumnja = iznad("")
	}
	out := map[string]raspon{}
	for k, vrh := range pomakniGranicu(nil, verzije) {
		if ne := p.nejamci(k, sumnja).presjek(raspon{Do: vrh}); !ne.prazan() {
			out[k] = ne
		}
	}
	return out
}

// promjenaKljuca je ono što upis primljenog mijenja u rupama jednog ključa
type promjenaKljuca struct {
	odbijeno  raspon   // što je ovo računalo odbilo
	uTrazi    raspon   // odbijeno kad je ograda od odluka accepta popustila: traži se (uzOgradu)
	nesigurno raspon   // dio primljenoga za koji druga strana ne jamči
	pokriveno []raspon // dijelovi za koje druga strana jamči da je dala sve što ima
	fs        string   // granica knjige ovog računala prije upisa
}

// nedostaje je dio nesigurnoga koji ovom računalu možda nedostaje: unutar
// onoga što već traži ili iznad njegove granice knjige; ispod nje mu niz
// i bez toga nije imao rupu
func (pk promjenaKljuca) nedostaje(trazi raspon) raspon {
	return pk.nesigurno.presjek(trazi).uz(pk.nesigurno.presjek(iznad(pk.fs)))
}

// prosiri ide prije upisa verzija: doda odbijeno i ono što nedostaje
func (pk promjenaKljuca) prosiri(k rupaKljuca) rupaKljuca {
	k.odbijeno = k.odbijeno.uz(pk.odbijeno)
	k.trazi = k.trazi.uz(pk.uTrazi).uz(pk.nedostaje(k.trazi))
	return k
}

// suzi ide nakon uspješnog upisa: iz trazi makne pokriveno, osim onoga što
// nedostaje i odbijenoga koje se traži
func (pk promjenaKljuca) suzi(k rupaKljuca) rupaKljuca {
	trazi := k.trazi
	for _, r := range pk.pokriveno {
		trazi = trazi.bez(r)
	}
	k.trazi = trazi.uz(pk.uTrazi).uz(pk.nedostaje(k.trazi))
	return k
}

// mijenjaTrazi: nešto nedostaje ili pokriveno siječe trazi
func (pk promjenaKljuca) mijenjaTrazi(trazi raspon) bool {
	if !pk.nedostaje(trazi).prazan() {
		return true
	}
	for _, r := range pk.pokriveno {
		if !r.presjek(trazi).prazan() {
			return true
		}
	}
	return false
}

// baceno su najmanja i najveća verzija ključa koju je ovo računalo odbilo,
// u razgovoru (zapamtiBaceno) ili uvozu (zaUvoz)
type baceno struct{ najmanja, najveca string }

// uzBaceno doda odbijenu verziju id ključu k: najmanja i najveća po ključu
func uzBaceno(m map[string]baceno, k, id string) map[string]baceno {
	if m == nil {
		m = map[string]baceno{}
	}
	b, ok := m[k]
	if !ok {
		b = baceno{najmanja: id, najveca: id}
	}
	m[k] = baceno{najmanja: min(b.najmanja, id), najveca: max(b.najveca, id)}
	return m
}

// rasponOdbijenog je raspon (od, b] koji drži sve što je odbijeno (b): od
// je veće od granice knjige (fs) i najveće primljene verzije ispod najmanje
// odbijene (ispod), a kad je odbijeno i ispod granice knjige (popuna),
// samo ispod. Bez odbijenoga prazan.
func rasponOdbijenog(b baceno, fs, ispod string) raspon {
	od := max(fs, ispod)
	if b.najmanja <= fs {
		od = ispod
	}
	return raspon{Od: od, Do: b.najveca}.cist()
}

// korakRupa je jedna promjena zapisa ključa: prosiri ili suzi
type korakRupa func(promjenaKljuca, rupaKljuca) rupaKljuca

// promjenaRupa je promjena zapisa rupa po ključu, uz zapis s početka
// razgovora (prije; kod uvoza ga nema) i treba li zapisati pravilo. ograda
// je stanje ograde prihvata prije odluka accepta, a sada pravilo čuvanja
// uz koje se promjena zapisuje (podOgradom, pod bravom upisa).
type promjenaRupa struct {
	kljucevi map[string]promjenaKljuca
	prije    map[string]rupaKljuca
	pravilo  bool
	ograda   stanjeOgrade
	sada     praviloCuvanja
}

// uzOgradu: kad ograda od odluka accepta (p.ograda) više ne odbija isto
// (sada: pravilo promijenjeno ili rub otišao unatrag), odbijeno bi sad možda
// prošlo, pa se traži (uTrazi), a ne samo bilježi. Mapa ključeva je nova,
// jer istu promjenu poslije upisa koristi i smanjenje (suzi).
func (p promjenaRupa) uzOgradu(sada stanjeOgrade) promjenaRupa {
	if p.ograda.josOdbija(sada) {
		return p
	}
	kljucevi := make(map[string]promjenaKljuca, len(p.kljucevi))
	for k, pk := range p.kljucevi {
		pk.uTrazi, pk.odbijeno = pk.uTrazi.uz(pk.odbijeno), raspon{}
		kljucevi[k] = pk
	}
	p.kljucevi = kljucevi
	return p
}

// ima: promjena mijenja zapis nekog ključa, pa treba transakciju
func (p promjenaRupa) ima() bool {
	for k, pk := range p.kljucevi {
		if !pk.odbijeno.prazan() || !pk.uTrazi.prazan() || pk.mijenjaTrazi(p.prije[k].trazi) {
			return true
		}
	}
	return false
}

// nista: upis ne mijenja ni knjigu ni trazi s početka razgovora, a pravilo
// ne treba zapisati, pa ne treba bravu. Vlastita odbijanja sama ne traže
// bravu: granica knjige se tada ne pomiče, a granica odbijenih (G4) ih
// pamti kao i prije.
func (p promjenaRupa) nista(verzije []ledger.Version) bool {
	if len(verzije) > 0 || p.pravilo {
		return false
	}
	for k, pk := range p.kljucevi {
		if pk.mijenjaTrazi(p.prije[k].trazi) {
			return false
		}
	}
	return true
}
