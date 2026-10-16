package peers

// Granica odbijenih (G4 u docs/plan-web-sync.md §3). Računalo koje ne drži
// stara očitanja (Accept, u programu repository.KeepVersion) primljena
// odbije prije upisa, pa mu se granica knjige ne pomakne. Druga strana mu
// ih je zato slala svakim razgovorom iznova, a ključ s 5000 takvih zaredom
// zapeo je zauvijek: ono iza njih nije stizalo nikad.
//
// Sad računalo za svako računalo koje mu šalje posebno pamti najveći
// version_id po ključu granice (ledger.FrontierKey) koji je od njega primilo
// i odbilo. Kad obje strane imaju oznaku granica-odbijenih/1, javi mu ga
// odmah iza granica (poruka granica-odbijenih), a ono slaže deltu od većega
// od dvoga: granice knjige i granice odbijenih.
//
// Zašto smije: za svaki ključ pošiljatelj šalje neprekinut niz svojih
// verzija iznad granice primatelja (ledger.Recorder.Delta i SinceByNode,
// redom nastanka; skraćenje reže samo kraj niza, a neispravne ne putuju).
// Svaku verziju iz niza primatelj je upisao ili odbio, pa do najveće
// odbijene nema ničega što bi htio, a nije dobio. To vrijedi samo za niz
// baš tog pošiljatelja, pa se granica vodi po pošiljatelju, po javnom
// ključu iz veze (ne po imenu, koje druga strana sama tvrdi). Zajednička
// granica sakrila bi verziju koju jedan pošiljatelj nije imao, a drugi bi
// je donio.
//
// Granica odbijenih nikad ne ide u knjigu, u granicu koju ovo računalo
// šalje ni u deltu prema trećima: treće računalo dobiva sve kao i prije.
// Pošiljatelj je koristi samo za deltu tog razgovora i, ograđenu vlastitom
// granicom, za ploču (zapamtiDelte).
//
// Pamti se samo u memoriji, pa je ponovno pokretanje zaboravi. Cijela se
// zaboravi i kad se promijeni što računalo drži ili prati: pravilo čuvanja
// (narastaj iz stanjePrihvata), pretplate, praćenje svega, brisanje kanala i
// zaboravljeno računalo. Zapis jednog pošiljatelja ne vrijedi ni kad taj
// više ne javlja oznaku, kad rub razdoblja ode unatrag (sat vraćen, kraj
// mjeseca) i kad prođe najduljeOdbijene. Zaboraviti previše košta jedno
// ponovno slanje, a zaboraviti premalo gubi podatke.

import (
	"context"
	"encoding/json"
	"maps"
	"sync"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// kindGranicaOdbijenih je poruka granice odbijenih, odmah iza granica. Nije
// "odbijeno": to je odbijenica (razmjena.VrstaOdbijeno).
const kindGranicaOdbijenih = "granica-odbijenih"

// najduljeOdbijene je koliko zapis od jednog pošiljatelja vrijedi; zatim se
// izgradi iznova, uz jedno ponovno slanje. Pošiljatelj može poslije dobiti
// verziju ispod granice koju prije nije imao (npr. od posrednika s užim
// pravilom čuvanja), a ona bi inače ovamo stigla tek nakon ponovnog
// pokretanja.
const najduljeOdbijene = 24 * time.Hour

// odbijeneMsg je poruka granice odbijenih: ključ granice → najveći
// version_id koji je pošiljatelj poruke odbio od primatelja. Šalje se i
// prazna, jer je druga strana čeka.
type odbijeneMsg struct {
	Granica map[string]string `json:"granica"`
}

// stanjeOgrade je ono o čemu ovisi što ograda prihvata (accept) odbija:
// narastaj pravila, koji se mijenja sa svakom promjenom pravila, i rub
// razdoblja, prije kojega se odbija (repository.StanjeOgradePovijesti)
type stanjeOgrade struct {
	narastaj uint64
	rub      time.Time
}

// odbijenoOd je granica odbijenih od jednog pošiljatelja
type odbijenoOd struct {
	ograda  stanjeOgrade // stanje ograde pri zadnjem upisu
	nastalo time.Time
	granica map[string]string
}

// pamcenjeOdbijenih je ono što je ovaj čvor odbio od drugih
type pamcenjeOdbijenih struct {
	mu sync.Mutex
	// zaborava raste sa svakim zaboravom svega: razgovor koji je počeo
	// prije zaborava ne upisuje ništa
	zaborava uint64
	odCvora  map[string]odbijenoOd // javni ključ pošiljatelja → odbijeno
}

// pocetakOdbijenih je ono što razgovor uzme prije prve poruke: s kim
// razgovara, dokle je zaborav stigao, stanje ograde i što drugoj strani
// javlja da je od nje odbio
type pocetakOdbijenih struct {
	kljuc    string
	zaborava uint64
	ograda   stanjeOgrade
	moje     map[string]string
}

// PostaviStanjePrihvata daje razmjeni stanje ograde prihvata (Accept), u
// programu repository.StanjeOgradePovijesti. Bez njega se granica odbijenih
// zaboravlja samo na promjene koje razmjena sama vidi.
func (s *Service) PostaviStanjePrihvata(fn func() (narastaj uint64, rub time.Time)) {
	s.stanjePrihvata = fn
}

// ogradaSada je stanje ograde prihvata sada
func (s *Service) ogradaSada() stanjeOgrade {
	if s.stanjePrihvata == nil {
		return stanjeOgrade{}
	}
	narastaj, rub := s.stanjePrihvata()
	return stanjeOgrade{narastaj: narastaj, rub: rub}
}

// sadOdbijenih je sat granice odbijenih
func (s *Service) sadOdbijenih() time.Time {
	if s.satOdbijenih != nil {
		return s.satOdbijenih()
	}
	return time.Now()
}

// zaboraviOdbijene zaboravi sve što je ovaj čvor odbio od drugih. Zove se
// bez uvjeta, i kad radnja koja ga traži ne uspije.
func (s *Service) zaboraviOdbijene() {
	s.odbijene.mu.Lock()
	defer s.odbijene.mu.Unlock()
	s.odbijene.zaborava++
	s.odbijene.odCvora = nil
}

// zaboraviOdbijeneOd zaboravi što je ovaj čvor odbio od jednog pošiljatelja
func (s *Service) zaboraviOdbijeneOd(kljuc string) {
	s.odbijene.mu.Lock()
	defer s.odbijene.mu.Unlock()
	delete(s.odbijene.odCvora, kljuc)
}

// vrijedi javlja da se zapisu još smije vjerovati: nastao je pod istim
// pravilom ograde, rub razdoblja nije otišao unatrag (sat vraćen, AddDate
// na kraju mjeseca), pa bi odbijeno i sada bilo odbijeno, i nije stariji od
// najduljeOdbijene. Prazan zapis ne vrijedi nikad.
func (z odbijenoOd) vrijedi(ograda stanjeOgrade, sad time.Time) bool {
	return z.ograda.narastaj == ograda.narastaj && !ograda.rub.Before(z.ograda.rub) && sad.Sub(z.nastalo) < najduljeOdbijene
}

// odbijeneOd je dokle je zaborav stigao i važeća granica odbijenih od
// pošiljatelja kljuc (bez nje nil), oboje pod istom bravom: razgovor koji
// pročita granicu iz vremena prije zaborava ne smije poslije upisati novu
func (s *Service) odbijeneOd(kljuc string, ograda stanjeOgrade, sad time.Time) (uint64, map[string]string) {
	s.odbijene.mu.Lock()
	defer s.odbijene.mu.Unlock()
	if z := s.odbijene.odCvora[kljuc]; z.vrijedi(ograda, sad) {
		return s.odbijene.zaborava, maps.Clone(z.granica)
	}
	return s.odbijene.zaborava, nil
}

// pocetakOdbijenih uzme, prije prve poruke razgovora s pošiljateljem kljuc,
// stanje ograde, dokle je zaborav stigao i važeću granicu odbijenih od
// njega. Drugoj strani ide samo ono iznad vlastite granice knjige (mine):
// ostalo ionako ne šalje.
func (s *Service) pocetakOdbijenih(kljuc string, mine map[string]string) pocetakOdbijenih {
	ograda := s.ogradaSada()
	zaborava, granica := s.odbijeneOd(kljuc, ograda, s.sadOdbijenih())
	p := pocetakOdbijenih{kljuc: kljuc, zaborava: zaborava, ograda: ograda, moje: map[string]string{}}
	for k, v := range granica {
		if v > mine[k] {
			p.moje[k] = v
		}
	}
	return p
}

// zapamtiOdbijene upiše što je ovaj čvor u razgovoru odbio od pošiljatelja.
// Zove se tek kad je upis primljenog prošao (i kad nije bilo ničega za
// upis): kod zauzetog ili palog upisa zadržane verzije nisu u knjizi, pa ih
// granica ne smije pokriti. Ne upisuje ništa kad je od početka razgovora
// nešto zaboravljeno ili se pravilo ograde promijenilo, jer je accept možda
// sudio po drugom pravilu. Rub se uzima sada, nakon svih odluka accepta.
func (s *Service) zapamtiOdbijene(p pocetakOdbijenih, odbijeno map[string]string) {
	ograda, sad := s.ogradaSada(), s.sadOdbijenih()
	s.odbijene.mu.Lock()
	defer s.odbijene.mu.Unlock()
	if len(odbijeno) == 0 || s.odbijene.zaborava != p.zaborava || ograda.narastaj != p.ograda.narastaj {
		return
	}
	s.odbijene.upisi(p.kljuc, ograda, sad, odbijeno)
}

// upisi spoji odbijeno u zapis pošiljatelja; zapis koji više ne vrijedi
// počinje iznova. Zove se pod bravom.
func (o *pamcenjeOdbijenih) upisi(kljuc string, ograda stanjeOgrade, sad time.Time, odbijeno map[string]string) {
	z := o.odCvora[kljuc]
	if !z.vrijedi(ograda, sad) {
		z = odbijenoOd{nastalo: sad}
	}
	z.ograda = ograda
	z.granica = spojiGranice(z.granica, odbijeno)
	if o.odCvora == nil {
		o.odCvora = map[string]odbijenoOd{}
	}
	o.odCvora[kljuc] = z
}

// odbaci zapamti verziju koju je odbila ograda prihvata u kanalu koji ovaj
// čvor prati (zaUpis). Neispravna (ledger.Ispravna) ne ulazi: nju ne odbija
// ograda, nego je knjiga ne prima (G3).
func (r *razgovorSCvorom) odbaci(v ledger.Version) {
	if !ledger.Ispravna(v) {
		return
	}
	if r.odbijeno == nil {
		r.odbijeno = map[string]string{}
	}
	k := ledger.FrontierKey(v.NodeID, v.Channel)
	r.odbijeno[k] = max(r.odbijeno[k], v.VersionID)
}

// razmijeniOdbijene: kad obje strane imaju oznaku granica-odbijenih/1
// (Dogovor nad živim granicama, pa obje odluče isto), svaka javi dokle je
// odbila ono što joj je druga poslala, redom uloge. Bez oznake druga strana
// ne dobije ništa i razgovor ide bajt po bajt kao prije, a ovaj čvor
// zaboravi što je od nje odbio i zapamti iznova iz njezine delte.
func (r *razgovorSCvorom) razmijeniOdbijene() error {
	if razmjena.Dogovor(r.moja.Mogucnosti, r.theirs.Mogucnosti)[razmjena.ImeGranicaOdbijenih] != 1 {
		r.s.zaboraviOdbijeneOd(r.odbijene.kljuc)
		return nil
	}
	return r.redom(
		func() error { return r.posalji(kindGranicaOdbijenih, odbijeneMsg{Granica: r.odbijene.moje}) },
		r.primiOdbijene,
	)
}

// primiOdbijene prima granicu odbijenih druge strane. Druga vrsta poruke je
// greška, kao u ocekuj. Pokvaren sadržaj nije: zanemari se i šalje se sve,
// kao bez oznake, jer djelomično pročitanoj granici ne može se vjerovati.
func (r *razgovorSCvorom) primiOdbijene() error {
	var sirovo json.RawMessage
	if err := r.ocekuj(kindGranicaOdbijenih, &sirovo); err != nil {
		return err
	}
	var stigla odbijeneMsg
	if json.Unmarshal(sirovo, &stigla) == nil {
		r.njihoveOdbijene = stigla.Granica
	}
	return nil
}

// granicaZaDeltu je tuđa granica po kojoj se slaže delta: po ključu veće od
// granice knjige druge strane i onoga što je javila da je od ovog čvora
// odbila. Zapamćena tuđa granica (theirs) ostaje granica knjige.
func (r *razgovorSCvorom) granicaZaDeltu() frontierMsg {
	g := r.theirs
	g.Frontier = spojiGranice(r.theirs.Frontier, r.njihoveOdbijene)
	return g
}

// spojiGranice je granica u kojoj je svaki ključ veći od dviju vrijednosti;
// bez dodanoga vraća istu granicu
func spojiGranice(granica, dodano map[string]string) map[string]string {
	if len(dodano) == 0 {
		return granica
	}
	out := make(map[string]string, len(granica)+len(dodano))
	maps.Copy(out, granica)
	for k, v := range dodano {
		out[k] = max(out[k], v)
	}
	return out
}

// doVlastite je tuđa granica odbijenih ograđena vlastitom granicom knjige:
// ključ koji ovaj čvor nema otpada, a ostali idu najviše do njegove najveće
// verzije. Ploča pošiljatelja tako ne misli da druga strana ima verzije koje
// je odbila od trećega, a ovaj čvor ih nema.
func doVlastite(odbijene, vlastita map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range odbijene {
		if m, ok := vlastita[k]; ok {
			out[k] = min(v, m)
		}
	}
	return out
}

// PrimaJos je „prima se još” na pločici razmjene: zna li drugo računalo za
// verzije koje ovaj čvor nema, a prati ih. Broje se samo ključevi njegove
// zapamćene granice u kanalima koje ovaj čvor prati (zajednički uvijek),
// veći od vlastite granice knjige i od onoga što je ovaj čvor od njega
// odbio. Kanal koji se ne prati ne broji se, jer ga druga strana ni ne
// šalje, a odbijeno ne, jer bi stiglo samo da se opet odbije. Vlastita
// granica i pretplate čitaju se jednom za cijelu pločicu; kad se ne daju
// pročitati, broji se sve, kao prije G4.
func (s *Service) PrimaJos(ctx context.Context) func(PeerStatus) bool {
	moja, _ := s.rec.Frontier(ctx)
	wants, err := s.CurrentWants(ctx)
	prati := s.wantsFunc(ctx, &wants)
	if err != nil {
		prati = nil
	}
	ograda, sad := s.ogradaSada(), s.sadOdbijenih()
	return func(ps PeerStatus) bool {
		_, odbijene := s.odbijeneOd(ps.PublicKey, ograda, sad)
		return josPrima(ps.State.Frontier, moja, odbijene, prati)
	}
}

// josPrima: neki ključ tuđe granice u kanalu koji se prati (prati nil: svi)
// veći je od vlastite granice i od granice odbijenih
func josPrima(njihova, moja, odbijene map[string]string, prati func(string) bool) bool {
	for k, v := range njihova {
		_, kanal := ledger.SplitFrontierKey(k)
		if (prati == nil || prati(kanal)) && v > max(moja[k], odbijene[k]) {
			return true
		}
	}
	return false
}
