package peers

// Rupe u razgovoru (rupe.go). Uz oznaku rupe/1 obje strane odmah iza
// granice odbijenih javi što traže (poruka rupe). Pošiljatelj na kraj delte
// doda popunu tuđih rupa za koje jamči, a uz deltu javi za koji dio
// poslanoga ne jamči (Rupe) i dokle je koju tuđu rupu popunio (Popuna).
// Primatelj vjeruje da je niz cijel samo pošiljatelju s oznakom: od
// pošiljatelja bez nje (0.0.38) trazi se ne smanjuje, kao i prije.
//
// Primljeno o rupama vrijedi samo u tom razgovoru i samo za ono što je
// stiglo; pokvareno ili lažno košta samo ponovno slanje.

import (
	"context"
	"encoding/json"
	"iter"
	"maps"
	"slices"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// rupeMsg je poruka rupa: ključ granice → raspon koji pošiljatelj poruke
// traži. Šalje se i prazna, jer je druga strana čeka.
type rupeMsg struct {
	Trazim map[string]raspon `json:"trazim"`
}

// popunaKljuca je popuna jedne tuđe rupe (od, granica]: dokle je ključ
// pročitan (procitano) i koliko je verzija u popuni (broj)
type popunaKljuca struct {
	od, granica, procitano string
	broj                   int
}

// popunaDelte je ono po čemu je delta složena: tuđa granica (granicaZaDeltu)
// i popuna po ključu
type popunaDelte struct {
	granica  map[string]string
	kljucevi map[string]popunaKljuca
}

// nakonGranica: uz zajedničke oznake obje strane odmah iza granica javi
// dokle su odbile (G4) i što traže
func (r *razgovorSCvorom) nakonGranica() error {
	if err := r.razmijeniOdbijene(); err != nil {
		return err
	}
	return r.razmijeniRupe()
}

// razmijeniRupe: kad obje strane imaju oznaku rupe/1 (Dogovor nad živim
// granicama, pa obje odluče isto), svaka javi što traži, redom uloge. Bez
// oznake ne ide ništa i razgovor je bajt po bajt kao prije.
func (r *razgovorSCvorom) razmijeniRupe() error {
	r.rupe.dogovor = razmjena.Dogovor(r.moja.Mogucnosti, r.theirs.Mogucnosti)[razmjena.ImeRupe] == 1
	if !r.rupe.dogovor {
		return nil
	}
	return r.redom(
		func() error { return r.posalji(kindRupe, rupeMsg{Trazim: r.trazeno}) },
		r.primiRupe,
	)
}

// primiRupe prima što druga strana traži. Pokvaren sadržaj ili previše
// ključeva zanemari se cijelo, a predug ključ ili neispravan raspon samo
// taj unos: tada se tu ne popunjava ništa, a to ne gubi ništa.
func (r *razgovorSCvorom) primiRupe() error {
	stigla, err := ocekujIliNista[rupeMsg](r, kindRupe)
	if len(stigla.Trazim) <= najviseTrazenih {
		r.njihovaTrazenja = valjanaTrazenja(stigla.Trazim)
	}
	return err
}

// valjanaTrazenja su tražene rupe bez predugih ključeva i neispravnih raspona
func valjanaTrazenja(trazim map[string]raspon) map[string]raspon {
	out := map[string]raspon{}
	for k, t := range trazim {
		if len(k) <= najduljiKljuc && t.ispravan() {
			out[k] = t
		}
	}
	return out
}

// ocekujIliNista prima poruku vrste kind (ocekuj). Druga vrsta poruke,
// odbijenica i pad veze su greška razgovora; pokvaren sadržaj nije, nego
// daje praznu poruku, jer djelomično pročitanoj ne može se vjerovati.
func ocekujIliNista[T any](r *razgovorSCvorom, kind string) (T, error) {
	var sirovo json.RawMessage
	var stiglo, prazno T
	if err := r.ocekuj(kind, &sirovo); err != nil {
		return prazno, err
	}
	if json.Unmarshal(sirovo, &stiglo) != nil {
		return prazno, nil
	}
	return stiglo, nil
}

// deltaSPopunom je delta za drugu stranu (deltaZa) s popunom njezinih rupa
// na kraju: rez delte (ogradiDeltu) tako uvijek prvo reže popunu, a
// redovna dostava ostaje kakva je i bez nje
func (r *razgovorSCvorom) deltaSPopunom(ctx context.Context, g frontierMsg) ([]ledger.Version, error) {
	delta, err := r.s.deltaZa(ctx, g)
	if err != nil {
		return nil, err
	}
	return append(delta, r.popunaZa(ctx, g, r.s.rec.SinceByNode)...), nil
}

// popunaZa su verzije koje popunjavaju ono što je druga strana zatražila,
// redom ključa. Ključ se čita od početka tražene rupe, a zapamti se samo
// kad je čitanje uspjelo; ključ koji se ne da pročitati ide bez popune.
func (r *razgovorSCvorom) popunaZa(ctx context.Context, g frontierMsg, citaj citacKljuca) []ledger.Version {
	r.popunjavam = popunaDelte{granica: g.Frontier, kljucevi: map[string]popunaKljuca{}}
	prati := r.s.wantsFunc(ctx, r.theirs.Wants)
	var out []ledger.Version
	for _, k := range slices.Sorted(maps.Keys(r.njihovaTrazenja)) {
		t := r.njihovaTrazenja[k]
		granica, ok := r.granicaPopune(k, t, g.Frontier, prati)
		if !ok {
			continue
		}
		autor, kanal := ledger.SplitFrontierKey(k)
		procitano, err := citaj(ctx, autor, kanal, t.Od, NajviseVerzijaPoRazmjeni)
		if err != nil {
			continue
		}
		plan, verzije := planPopune(t.Od, granica, procitano)
		r.popunjavam.kljucevi[k] = plan
		out = append(out, verzije...)
	}
	return out
}

// granicaPopune je dokle se tražena rupa t popunjava: najviše do kraja
// rupe, do tuđe granice po kojoj ide delta (iznad nje ide redovno) i do
// vlastite granice. Popunjava se samo kanal koji druga strana prati, i samo
// kad ovo računalo tik iza početka rupe jamči (jamciIza).
func (r *razgovorSCvorom) granicaPopune(k string, t raspon, g map[string]string, prati func(string) bool) (string, bool) {
	_, kanal := ledger.SplitFrontierKey(k)
	granica := min(t.Do, g[k], r.moja.Frontier[k])
	return granica, (prati == nil || prati(kanal)) && r.rupe.jamciIza(k, t.Od) && granica > t.Od
}

// planPopune: od pročitanoga iza od popunjava se ono do granice, a dokle se
// za to jamči, kaže dokleProcitano
func planPopune(od, granica string, procitano []ledger.Version) (popunaKljuca, []ledger.Version) {
	n := 0
	for n < len(procitano) && procitano[n].VersionID <= granica {
		n++
	}
	return popunaKljuca{od: od, granica: granica, procitano: dokleProcitano(od, granica, procitano[:n], n < len(procitano)), broj: n}, procitano[:n]
}

// dokleProcitano je dokle je ključ stvarno pročitan: do granice samo kad je
// pročitana i verzija iza nje (iza), a inače do zadnje pročitane, bez
// ijedne nigdje (od). Granica nije veća od vlastite granice s početka
// razgovora, a zadnju verziju ključa sažimanje i prorjeđivanje ostavljaju u
// knjizi, pa čitanje kraće od pune ograde (NajviseVerzijaPoRazmjeni) inače
// dođe do granice. Kad ne dođe, knjiga je od početka razgovora izgubila
// verzije (npr. brisanje kanala usred razgovora), a za ono čega više nema
// ovo računalo ne smije jamčiti.
func dokleProcitano(od, granica string, popuna []ledger.Version, iza bool) string {
	switch {
	case iza:
		return granica
	case len(popuna) > 0:
		return popuna[len(popuna)-1].VersionID
	}
	return od
}

// jamstvoDelte je ono što pošiljatelj s oznakom rupe/1 javi uz deltu: po
// ključu dio poslanoga za koji ne jamči (Rupe) i popunu (Popuna). Obrađeno
// je poslano i neispravno: rez delte uzima početak, pa je obrađena popuna
// ključa uvijek njezin početak. Bez oznake ne javlja ništa.
func (r *razgovorSCvorom) jamstvoDelte(poslano, neispravne []ledger.Version) (map[string]raspon, map[string]raspon) {
	if !r.rupe.dogovor {
		return nil, nil
	}
	redovno, popunjeno := r.popunjavam.rasporedi(slices.Concat(poslano, neispravne))
	popuna := r.popunjavam.jamstvo(popunjeno)
	return r.rupeDelte(redovno, popuna), popuna
}

// obradjenaPopuna je koliko je verzija popune ključa obrađeno i najveća od
// njih; obrađena popuna je početak popune ključa, pa je to i zadnja
type obradjenaPopuna struct {
	broj   int
	zadnja string
}

// rasporedi dijeli obrađeno na redovni dio (najveća verzija po ključu) i
// popunu (po ključu koliko je verzija i koja je zadnja). Popuna ključa je
// ispod granice po kojoj ide delta, a redovni dio iznad nje. Neispravne su
// iza poslanih, pa se zadnja uzima kao najveća, a ne po redu.
func (p popunaDelte) rasporedi(obradjeno []ledger.Version) (map[string]string, map[string]obradjenaPopuna) {
	redovno, popunjeno := map[string]string{}, map[string]obradjenaPopuna{}
	for _, v := range obradjeno {
		k := ledger.FrontierKey(v.NodeID, v.Channel)
		if plan, ok := p.kljucevi[k]; ok && v.VersionID <= plan.granica {
			popunjeno[k] = obradjenaPopuna{broj: popunjeno[k].broj + 1, zadnja: max(popunjeno[k].zadnja, v.VersionID)}
			continue
		}
		redovno[k] = max(redovno[k], v.VersionID)
	}
	return redovno, popunjeno
}

// jamstvo je Popuna u delti: po ključu ono što popuna pokriva (pokriva).
// Ključ s praznim rasponom ne javlja se.
func (p popunaDelte) jamstvo(popunjeno map[string]obradjenaPopuna) map[string]raspon {
	out := map[string]raspon{}
	for k, plan := range p.kljucevi {
		if r := plan.pokriva(popunjeno[k]); !r.prazan() {
			out[k] = r
		}
	}
	return out
}

// pokriva je (od, e]: e je dokle je ključ pročitan (dokleProcitano) kad je
// obrađena sva njegova popuna, a inače zadnja obrađena verzija popune. Kad
// nije obrađeno ništa, a popuna nije prazna, ne pokriva ništa.
func (plan popunaKljuca) pokriva(o obradjenaPopuna) raspon {
	switch {
	case o.broj == plan.broj:
		return raspon{Od: plan.od, Do: plan.procitano}
	case o.broj > 0:
		return raspon{Od: plan.od, Do: o.zadnja}
	}
	return raspon{}
}

// rupeDelte su Rupe u delti: po ključu dio omotača poslanoga (redovni dio od
// tuđe granice do najveće obrađene i popuna) za koji ovo računalo ne
// jamči. Kad je od početka razgovora zapisana nova rupa (brojač
// rupeMijenja), ne jamči ni za što iznad svoje granice s početka razgovora.
func (r *razgovorSCvorom) rupeDelte(redovno map[string]string, popuna map[string]raspon) map[string]raspon {
	sumnja := r.s.rupeMijenja.Load() != r.rupe.broj
	out := map[string]raspon{}
	for _, k := range skupKljuceva(maps.Keys(redovno), maps.Keys(popuna)) {
		omotac := raspon{Od: r.popunjavam.granica[k], Do: redovno[k]}.uz(popuna[k])
		if ne := r.rupe.nejamci(k, r.sumnjaIznad(k, sumnja)).presjek(omotac); !ne.prazan() {
			out[k] = ne
		}
	}
	return out
}

// sumnjaIznad je, uz sumnju, sve iznad vlastite granice s početka razgovora
func (r *razgovorSCvorom) sumnjaIznad(k string, sumnja bool) raspon {
	if !sumnja {
		return raspon{}
	}
	return iznad(r.moja.Frontier[k])
}

// skupKljuceva su poredani ključevi koji su u barem jednom nizu
func skupKljuceva(nizovi ...iter.Seq[string]) []string {
	skup := map[string]bool{}
	for _, niz := range nizovi {
		for k := range niz {
			skup[k] = true
		}
	}
	return slices.Sorted(maps.Keys(skup))
}

// zapamtiJamstvo zapamti što je druga strana uz deltu javila o rupama
func (r *razgovorSCvorom) zapamtiJamstvo(stigla deltaMsg) {
	r.njihoveRupe, r.njihovaPopuna = stigla.Rupe, stigla.Popuna
}

// zapamtiBaceno zapamti najmanju i najveću verziju ključa k koju je ovo
// računalo u razgovoru odbilo
func (r *razgovorSCvorom) zapamtiBaceno(k, id string) {
	r.bacio = uzBaceno(r.bacio, k, id)
}

// promjenaRupa složi, prije upisa, što upis mijenja u rupama: za svaki
// ključ u koji je stiglo nešto što ovo računalo prati (wanted, poslije
// bezPreskocenog), koji je odbilo ili koji je druga strana popunila. Bez
// vlastitog ključa i bez ključeva koje bezPreskocenog nije upisao. Uz
// promjenu ide stanje ograde s početka razgovora: ako je ograda od tada
// popustila, odbijeno se traži, a to se gleda tek pod bravom (podOgradom).
func (r *razgovorSCvorom) promjenaRupa(wanted []ledger.Version) promjenaRupa {
	p := promjenaRupa{kljucevi: map[string]promjenaKljuca{}, prije: r.rupe.kljucevi, pravilo: r.rupe.zapisati, ograda: r.odbijene.ograda}
	vrh, ispod := pomakniGranicu(nil, wanted), ispodOdbijenih(wanted, r.bacio)
	for _, k := range skupKljuceva(maps.Keys(vrh), maps.Keys(r.bacio), maps.Keys(r.njihovaPopuna)) {
		if _, preskocen := r.preskoceni[k]; !preskocen && !r.rupe.vlastiti(k) {
			p.kljucevi[k] = r.promjenaKljuca(k, max(vrh[k], r.bacio[k].najveca), ispod[k])
		}
	}
	return p
}

// promjenaKljuca je promjena ključa k, u koji je stiglo sve do m
// (prihvaćeno, odbijeno ili neispravno)
func (r *razgovorSCvorom) promjenaKljuca(k, m, ispod string) promjenaKljuca {
	return promjenaKljuca{fs: r.moja.Frontier[k], odbijeno: r.odbijenoKljuca(k, ispod), nesigurno: r.nesigurnoKljuca(k, m), pokriveno: r.pokrivenoKljuca(k, m)}
}

// odbijenoKljuca je raspon koji ovo računalo u ključu k samo odbija: što
// je u razgovoru odbilo (rasponOdbijenog) i, kad je druga strana po granici
// odbijenih (G4) preskakala, sve od granice knjige s početka razgovora do
// granice odbijenih
func (r *razgovorSCvorom) odbijenoKljuca(k, ispod string) raspon {
	fs := r.moja.Frontier[k]
	odbijeno := rasponOdbijenog(r.bacio[k], fs, ispod)
	if moje, preskakala := r.odbijene.moje[k]; preskakala {
		odbijeno = odbijeno.uz(raspon{Od: fs, Do: moje})
	}
	return odbijeno
}

// ispodOdbijenih je po ključu najveća primljena verzija ispod najmanje
// koju je ovo računalo u razgovoru odbilo
func ispodOdbijenih(wanted []ledger.Version, bacio map[string]baceno) map[string]string {
	out := map[string]string{}
	for _, v := range wanted {
		k := ledger.FrontierKey(v.NodeID, v.Channel)
		if b, ok := bacio[k]; ok && v.VersionID < b.najmanja {
			out[k] = max(out[k], v.VersionID)
		}
	}
	return out
}

// pokrivenoKljuca su dijelovi ključa k za koje druga strana s oznakom
// rupe/1 jamči da je dala sve što ima: redovni dio iznad granice s početka
// razgovora (i iznad granice odbijenih kad ju je poslala) do m, i valjana
// popuna. Pošiljatelju bez oznake ne vjeruje se da je niz cijel.
func (r *razgovorSCvorom) pokrivenoKljuca(k, m string) []raspon {
	if !r.rupe.dogovor {
		return nil
	}
	return []raspon{{Od: max(r.moja.Frontier[k], r.odbijene.moje[k]), Do: m}, r.valjanaPopuna(k)}
}

// nesigurnoKljuca je dio primljenoga u ključu k za koji druga strana s
// oznakom rupe/1 ne jamči, najviše do najveće primljene verzije (m i
// popuna). Neispravan raspon znači da ne jamči ni za što primljeno.
func (r *razgovorSCvorom) nesigurnoKljuca(k, m string) raspon {
	if !r.rupe.dogovor {
		return raspon{}
	}
	primljeno := raspon{Do: max(m, r.valjanaPopuna(k).Do)}
	rupa, javljena := r.njihoveRupe[k]
	if javljena && !rupa.ispravan() {
		return primljeno.cist()
	}
	return rupa.presjek(primljeno)
}

// valjanaPopuna je popuna ključa k koju je druga strana javila, kad
// vrijedi: ključ je tražen u ovom razgovoru, popuna počinje gdje i tražena
// rupa i ne ide iza njezina kraja
func (r *razgovorSCvorom) valjanaPopuna(k string) raspon {
	p, t := r.njihovaPopuna[k], r.trazeno[k]
	if t.prazan() || p.Od != t.Od || p.prazan() || p.Do > t.Do {
		return raspon{}
	}
	return p
}
