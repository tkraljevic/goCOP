package natsim

// NAT (plan WEB synca, F3a): preslikava unutarnju adresu i port u vanjsku
// za pakete koji idu van, a pakete izvana pušta unutra samo na živo
// preslikavanje i samo kad ih filtriranje pusti. Paketi izvana čekaju u
// redu NAT-a koliko kaže Kasnjenje; pun red baca paket, a uz Gubitak NAT
// izgubi zadani udio paketa izvana.

import (
	"maps"
	"math/rand/v2"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/quic-go/quic-go/testutils/simnet"
)

// Preslikavanje kaže kad NAT za isti unutarnji port daje novi vanjski
type Preslikavanje int

// Vrste preslikavanja (RFC 4787)
const (
	PreslikavanjeNeovisno       Preslikavanje = iota // EIM: jedan vanjski port za sva odredišta
	PreslikavanjePoAdresi                            // novi vanjski port za svaku adresu odredišta
	PreslikavanjePoAdresiIPortu                      // novi za svaku adresu i port odredišta: simetrični NAT
)

// Filtriranje kaže koje pakete izvana NAT pušta na živo preslikavanje
type Filtriranje int

// Vrste filtriranja (RFC 4787)
const (
	FiltriranjeNeovisno       Filtriranje = iota // od bilo koga
	FiltriranjePoAdresi                          // samo s adrese kojoj je unutarnje računalo slalo
	FiltriranjePoAdresiIPortu                    // samo s adrese i porta kojima je slalo
)

// Zadane vrijednosti postavki NAT-a
const (
	zadaniIstek    = 30 * time.Second
	zadaniRed      = 128
	zadaniPrviPort = 40000
)

// Postavke su ponašanje jednog NAT-a; nula je EIM s neovisnim filtriranjem,
// bez čuvanja porta i hairpina
type Postavke struct {
	Preslikavanje Preslikavanje
	Filtriranje   Filtriranje
	Istek         time.Duration // preslikavanje i dopuštenje bez prometa van; 0: 30 s
	CuvaPort      bool          // vanjski port jednak unutarnjem kad je slobodan
	Hairpin       bool          // paket na vlastitu javnu adresu vraća se unutra
	BlokiranUDP   bool          // baca sav UDP
	// JavneNaizmjence: svako novo preslikavanje uzme iduću javnu adresu.
	// Inače unutarnje računalo uvijek izlazi s iste javne adrese.
	JavneNaizmjence bool
	Kasnjenje       time.Duration // za pakete izvana, prije NAT-a
	Gubitak         float64       // udio paketa izvana koje NAT izgubi, od 0 do 1
	Red             int           // koliko paketa izvana čeka u redu; 0: 128
	PrviPort        uint16        // prvi vanjski port koji NAT dijeli; 0: 40000
	// Utrka je prozor conntracka u Docker mostu (samo za objavljene
	// portove): kad prvi paketi toka u oba smjera stignu unutar njega,
	// odlazni tok dobije novi vanjski port. 0: bez utrke.
	Utrka time.Duration
}

func (p Postavke) zadane() Postavke {
	p.Istek = ili(p.Istek, zadaniIstek)
	p.Red = ili(p.Red, zadaniRed)
	p.PrviPort = ili(p.PrviPort, zadaniPrviPort)
	return p
}

// ili je v, a zadano kad je v nula
func ili[T comparable](v, zadano T) T {
	var nula T
	if v == nula {
		return zadano
	}
	return v
}

// Statistika broji što je NAT propustio i bacio
type Statistika struct {
	Van, Unutra      int // propušteni paketi
	BezPreslikavanja int // izvana na port bez živog preslikavanja
	Filtrirano       int // izvana s adrese koju filtriranje ne pušta
	Blokirano        int // UDP blokiran
	BezHairpina      int // na vlastitu javnu adresu, bez hairpina
	PunRed           int // red paketa izvana bio je pun
	Izgubljeno       int // izvana, izgubljeni po Gubitak
	Sudari           int // conntrack je odlaznom toku dao drugi port
}

// NAT je jedan NAT; nastaje s Mreza.NAT ili NAT.NAT
type NAT struct {
	m        *Mreza
	p        Postavke
	javne    []netip.Addr
	van, lan *segment
	red      chan uRedu
	kraj     chan struct{}
	gotov    sync.WaitGroup

	mu                 sync.Mutex
	odlazna            map[kljuc]*preslikavanje
	vanjska            map[netip.AddrPort]*preslikavanje
	objavljeni         map[uint16]netip.AddrPort
	uparene            map[netip.Addr]netip.Addr // unutarnja adresa → javna
	tokovi             map[kljuc]*zapisToka      // conntrack za objavljene portove
	dodijeljeno, iduca int                       // koliko je portova i javnih adresa podijeljeno
	stat               Statistika
}

// kljuc preslikavanja: unutarnja adresa i, ovisno o vrsti, odredište;
// sudar je tok koji je conntrack preslikao drukčije
type kljuc struct {
	od, do netip.AddrPort
	sudar  bool
}

type preslikavanje struct {
	unutarnja, vanjska netip.AddrPort
	kljuc              kljuc
	zadnje             time.Time
	dozvole            map[netip.AddrPort]time.Time // komu je unutarnje računalo slalo
}

// zapisToka je tok kroz conntrack: kad je krenuo, u kojem smjeru i je li
// već viđen u oba
type zapisToka struct {
	prvi, zadnje time.Time
	van          bool
	potvrdjen    bool
}

type uRedu struct {
	p   simnet.Packet
	kad time.Time
}

// NAT je NAT iza ovog NAT-a (dvostruki NAT): njegove javne adrese su u
// unutarnjoj mreži ovog
func (n *NAT) NAT(p Postavke, javne ...netip.Addr) *NAT { return n.m.noviNAT(n.lan, p, javne) }

// Racunalo je računalo iza ovog NAT-a
func (n *NAT) Racunalo(a netip.AddrPort, p PostavkeRacunala) *simnet.SimConn {
	return n.m.racunalo(n.lan, a, p)
}

// Objavi pusti pakete izvana na vanjski port ravno na unutarnju adresu,
// od bilo koga (objavljen port u Dockeru); odlazni paketi s te adrese
// izlaze s istog porta
func (n *NAT) Objavi(port uint16, unutarnja netip.AddrPort) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.objavljeni[port] = unutarnja
}

// Gubitak: od sada NAT gubi udio paketa izvana (kao Postavke.Gubitak), na
// primjer tek nakon što računala izmjere mrežu
func (n *NAT) Gubitak(udio float64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.p.Gubitak = udio
}

// Statistika su brojila NAT-a
func (n *NAT) Statistika() Statistika {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.stat
}

func (m *Mreza) noviNAT(van *segment, p Postavke, javne []netip.Addr) *NAT {
	p = p.zadane()
	n := &NAT{m: m, p: p, javne: javne, van: van, red: make(chan uRedu, p.Red), kraj: make(chan struct{}),
		odlazna: map[kljuc]*preslikavanje{}, vanjska: map[netip.AddrPort]*preslikavanje{},
		objavljeni: map[uint16]netip.AddrPort{}, uparene: map[netip.Addr]netip.Addr{}, tokovi: map[kljuc]*zapisToka{}}
	n.lan = noviSegment(m, n)
	van.mu.Lock()
	for _, j := range javne {
		van.natovi[j] = n
	}
	van.mu.Unlock()
	n.gotov.Go(n.vrti)
	m.mu.Lock()
	m.natovi = append(m.natovi, n)
	m.mu.Unlock()
	return n
}

func (n *NAT) zatvori() {
	close(n.kraj)
	n.gotov.Wait()
}

// broji poveća brojilo pod bravom
func (n *NAT) broji(polje *int) {
	n.mu.Lock()
	*polje++
	n.mu.Unlock()
}

// posalji pusti paket iz unutarnje mreže van: izvor postaje vanjska adresa
// preslikavanja. Paket na vlastitu javnu adresu vraća se unutra samo uz
// hairpin.
func (n *NAT) posalji(p simnet.Packet) {
	vanjska, ok := n.odlazno(adresa(p.From), adresa(p.To), time.Now())
	if !ok {
		return
	}
	p.From = net.UDPAddrFromAddrPort(vanjska)
	switch {
	case !slices.Contains(n.javne, adresa(p.To).Addr()):
		n.van.usmjeri(p)
	case n.p.Hairpin:
		n.primi(p)
	default:
		n.broji(&n.stat.BezHairpina)
	}
}

// odlazno je vanjska adresa za paket od → do; laž kad je UDP blokiran
func (n *NAT) odlazno(od, do netip.AddrPort, sad time.Time) (netip.AddrPort, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.p.BlokiranUDP {
		n.stat.Blokirano++
		return netip.AddrPort{}, false
	}
	pr := n.preslikavanjeZa(od, do, sad)
	pr.zadnje, pr.dozvole[do] = sad, sad
	n.stat.Van++
	return pr.vanjska, true
}

// preslikavanjeZa je živo preslikavanje za paket od → do, ili novo
func (n *NAT) preslikavanjeZa(od, do netip.AddrPort, sad time.Time) *preslikavanje {
	if n.objavljen(od) && n.tok(kljuc{od: od, do: do}, true, sad) {
		n.sudar(od, do, sad)
	}
	for _, k := range []kljuc{{od: od, do: do, sudar: true}, n.kljucZa(od, do)} {
		if pr := n.odlazna[k]; n.ziv(pr, sad) {
			return pr
		}
	}
	return n.novo(n.kljucZa(od, do), od, n.vanjskaZa(od, sad), sad)
}

// kljucZa je ključ preslikavanja po vrsti NAT-a
func (n *NAT) kljucZa(od, do netip.AddrPort) kljuc {
	switch n.p.Preslikavanje {
	case PreslikavanjePoAdresi:
		return kljuc{od: od, do: netip.AddrPortFrom(do.Addr(), 0)}
	case PreslikavanjePoAdresiIPortu:
		return kljuc{od: od, do: do}
	}
	return kljuc{od: od}
}

// ziv: preslikavanje postoji i nije isteklo; isteklo se briše, pa je
// njegov port opet slobodan
func (n *NAT) ziv(pr *preslikavanje, sad time.Time) bool {
	if pr == nil {
		return false
	}
	if sad.Sub(pr.zadnje) < n.p.Istek {
		return true
	}
	n.makni(pr)
	return false
}

// makni briše isteklo preslikavanje. Ključ ili vanjsku adresu koje je već
// preuzelo novije preslikavanje (sudar u conntracku, objavljen port)
// ostavlja novijem.
func (n *NAT) makni(pr *preslikavanje) {
	if n.odlazna[pr.kljuc] == pr {
		delete(n.odlazna, pr.kljuc)
	}
	if n.vanjska[pr.vanjska] == pr {
		delete(n.vanjska, pr.vanjska)
	}
}

func (n *NAT) novo(k kljuc, od, vanjska netip.AddrPort, sad time.Time) *preslikavanje {
	pr := &preslikavanje{unutarnja: od, vanjska: vanjska, kljuc: k, zadnje: sad, dozvole: map[netip.AddrPort]time.Time{}}
	n.odlazna[k], n.vanjska[vanjska] = pr, pr
	return pr
}

// vanjskaZa je vanjska adresa novog preslikavanja: objavljen port ili
// sačuvan unutarnji kad su slobodni, inače idući slobodan
func (n *NAT) vanjskaZa(od netip.AddrPort, sad time.Time) netip.AddrPort {
	ip := n.javnaZa(od.Addr())
	for _, port := range n.zeljeniPortovi(od) {
		if v := netip.AddrPortFrom(ip, port); !n.zauzet(v, sad) || n.objavljeni[port] == od {
			return v
		}
	}
	return n.sljedeciSlobodan(ip, sad)
}

// zeljeniPortovi su portovi koje novo preslikavanje proba prije idućeg
// slobodnog: objavljeni port te adrese, pa sačuvani unutarnji
func (n *NAT) zeljeniPortovi(od netip.AddrPort) []uint16 {
	var out []uint16
	for port, un := range n.objavljeni {
		if un == od {
			out = append(out, port)
		}
	}
	if n.p.CuvaPort {
		out = append(out, od.Port())
	}
	return out
}

// javnaZa je javna adresa novog preslikavanja: uparena s unutarnjom
// adresom, ili iduća kad se adrese daju naizmjence
func (n *NAT) javnaZa(unutarnja netip.Addr) netip.Addr {
	if j, ima := n.uparene[unutarnja]; ima && !n.p.JavneNaizmjence {
		return j
	}
	j := n.javne[n.iduca%len(n.javne)]
	n.iduca++
	n.uparene[unutarnja] = j
	return j
}

// zauzet: na vanjskoj adresi je živo preslikavanje ili objavljen port
func (n *NAT) zauzet(v netip.AddrPort, sad time.Time) bool {
	_, objavljen := n.objavljeni[v.Port()]
	return objavljen || n.ziv(n.vanjska[v], sad)
}

// sljedeciSlobodan je idući slobodan vanjski port od PrviPort naviše, u
// krug
func (n *NAT) sljedeciSlobodan(ip netip.Addr, sad time.Time) netip.AddrPort {
	raspon := 65536 - int(n.p.PrviPort)
	for {
		v := netip.AddrPortFrom(ip, uint16(int(n.p.PrviPort)+n.dodijeljeno%raspon))
		n.dodijeljeno++
		if !n.zauzet(v, sad) {
			return v
		}
	}
}

// primi stavi paket izvana u red; pun red ga baca, a uz Gubitak se paket
// izgubi prije reda
func (n *NAT) primi(p simnet.Packet) {
	if n.gubi() {
		return
	}
	select {
	case n.red <- uRedu{p: p, kad: time.Now().Add(n.p.Kasnjenje)}:
	default:
		n.broji(&n.stat.PunRed)
	}
}

// gubi: paket izvana se gubi, po Gubitak
func (n *NAT) gubi() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if rand.Float64() >= n.p.Gubitak {
		return false
	}
	n.stat.Izgubljeno++
	return true
}

// vrti pušta pakete iz reda unutra kad im prođe kašnjenje
func (n *NAT) vrti() {
	for {
		select {
		case <-n.kraj:
			return
		case d := <-n.red:
			if !n.cekaj(d.kad) {
				return
			}
			n.uvedi(d.p)
		}
	}
}

// cekaj čeka do trenutka do; laž kad se NAT zatvori prije toga
func (n *NAT) cekaj(do time.Time) bool {
	if !time.Now().Before(do) {
		return true
	}
	t := time.NewTimer(time.Until(do))
	defer t.Stop()
	select {
	case <-n.kraj:
		return false
	case <-t.C:
		return true
	}
}

// uvedi pusti paket izvana na unutarnju adresu, kad ga preslikavanje i
// filtriranje puštaju
func (n *NAT) uvedi(p simnet.Packet) {
	unutarnja, ok := n.dolazno(adresa(p.From), adresa(p.To), time.Now())
	if ok {
		p.To = net.UDPAddrFromAddrPort(unutarnja)
		n.lan.usmjeri(p)
	}
}

// dolazno je unutarnja adresa za paket izvana od → do; laž kad ga NAT baca
func (n *NAT) dolazno(od, do netip.AddrPort, sad time.Time) (netip.AddrPort, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.p.BlokiranUDP {
		n.stat.Blokirano++
		return netip.AddrPort{}, false
	}
	if un, ima := n.objavljeni[do.Port()]; ima {
		if n.tok(kljuc{od: un, do: od}, false, sad) {
			n.sudar(un, od, sad)
		}
		n.stat.Unutra++
		return un, true
	}
	return n.naPreslikavanje(od, do, sad)
}

// naPreslikavanje pusti paket izvana na živo preslikavanje, kad ga
// filtriranje pušta
func (n *NAT) naPreslikavanje(od, do netip.AddrPort, sad time.Time) (netip.AddrPort, bool) {
	pr := n.vanjska[do]
	switch {
	case !n.ziv(pr, sad):
		n.stat.BezPreslikavanja++
		return netip.AddrPort{}, false
	case !n.pusta(pr, od, sad):
		n.stat.Filtrirano++
		return netip.AddrPort{}, false
	}
	n.stat.Unutra++
	return pr.unutarnja, true
}

// pusta: filtriranje pušta paket s adrese od na preslikavanje pr
func (n *NAT) pusta(pr *preslikavanje, od netip.AddrPort, sad time.Time) bool {
	for do, kad := range pr.dozvole {
		if sad.Sub(kad) < n.p.Istek && n.odgovara(do, od) {
			return true
		}
	}
	return false
}

// odgovara: adresa od smije poslati paket na preslikavanje koje je slalo
// na adresu do
func (n *NAT) odgovara(do, od netip.AddrPort) bool {
	switch n.p.Filtriranje {
	case FiltriranjePoAdresi:
		return do.Addr() == od.Addr()
	case FiltriranjePoAdresiIPortu:
		return do == od
	}
	return true
}

// objavljen: s unutarnje adrese ide objavljen port (Docker)
func (n *NAT) objavljen(od netip.AddrPort) bool {
	return slices.Contains(slices.Collect(maps.Values(n.objavljeni)), od)
}

// tok zapiše paket toka unutarnja ↔ udaljena u conntrack i javlja sudar:
// prvi paketi u oba smjera stigli su unutar prozora Utrka. Zapis koji je
// istekao kreće iznova; bez Utrka sudara nema.
func (n *NAT) tok(k kljuc, van bool, sad time.Time) bool {
	if n.p.Utrka <= 0 {
		return false
	}
	z, ima := n.tokovi[k]
	if !ima || sad.Sub(z.zadnje) >= n.p.Istek {
		n.tokovi[k] = &zapisToka{prvi: sad, zadnje: sad, van: van}
		return false
	}
	z.zadnje = sad
	sudar := !z.potvrdjen && z.van != van && sad.Sub(z.prvi) <= n.p.Utrka
	z.potvrdjen = z.potvrdjen || z.van != van
	return sudar
}

// sudar da odlaznom toku unutarnja → udaljena novi vanjski port, kao
// conntrack kad ne može upisati drugi zapis istog toka. Dok to
// preslikavanje živi, živi i zapis toka (oba osvježava promet van), pa
// drugog sudara istog toka nema.
func (n *NAT) sudar(unutarnja, udaljena netip.AddrPort, sad time.Time) {
	k := kljuc{od: unutarnja, do: udaljena, sudar: true}
	pr := n.novo(k, unutarnja, n.sljedeciSlobodan(n.javnaZa(unutarnja.Addr()), sad), sad)
	pr.dozvole[udaljena] = sad
	n.stat.Sudari++
}
