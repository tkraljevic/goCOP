package razmjena

// Udaranje jedne sesije WEB synca (plan WEB synca, F3a, „Pravila
// udaraca”). Udarci idu samo potpisanim kandidatima druge strane, bez
// vlastitih adresa, naizmjence, a razmak između dva udarca istom kandidatu
// raste od 50 do 200 ms. Granica je zajednička za sve kandidate i odgovore
// jednog pokušaja: najviše 20 paketa u sekundi (najmanje 50 ms između dva)
// i 60 po pokušaju, pa s 8 kandidata svaki dobije udarac otprilike svakih
// 400 ms. Odgovori poslani prije prvog pokušaja i između dva (nakon Stani)
// ulaze u granicu idućeg.
//
// Na valjan udarac druge strane računalo odgovara samo na adresu s koje je
// udarac stigao, jednom po udarcu, paketom iste veličine i unutar iste
// granice. To je jedina iznimka od pravila „samo potpisanim kandidatima”:
// tako radi par u kojem drugu stranu vidi tek NAT (simetrični ili
// dvostruki NAT). Ta adresa se pamti kao nova adresa druge strane i ulazi u
// vrata za QUIC (Pusta, za ConnContext). Odgovor druge strane prima se samo
// s adrese na koju je poslan udarac (po pionu). Bez valjanog udarca s neke
// adrese na nju ne ide ništa.
//
// Prvi valjan paket sa svake adrese javlja se stroju probijanja (Valjani) u
// svakom pokušaju iznova: tako idući pokušaj vidi i adrese koje su se javile
// prije, kad prvi QUIC nije uspio ili je udarac stigao prije sinkra, a stroj
// ih ne mora pamtiti (kao Tailscale, iroh i libp2p, koji u svakom pokušaju
// gledaju sve putove).
//
// Udarci ne idu kroz UDP.posalji: udarac na lokalnog kandidata računala
// koje spava javi EHOSTDOWN, a to ne znači da ovo računalo nema rutu, pa ne
// smije pokrenuti novu utičnicu (F2b).

import (
	"errors"
	"maps"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"
)

const (
	razmakSlanja       = 50 * time.Millisecond // najviše 20 paketa u sekundi
	najviseUdaraca     = 60                    // po pokušaju, s odgovorima
	prviRazmakUdarca   = 50 * time.Millisecond
	najveciRazmak      = 200 * time.Millisecond
	rokVrata           = 30 * time.Second // ConnContext pušta adresu s valjanim paketom iz zadnjih 30 s
	najviseAdresaDruge = 16
	redUdaraca         = 64 // primljeni udarci koji čekaju sesiju; više od 60 po pokušaju
	bezPosla           = time.Hour
)

// Razlozi odbijenog paketa, u StanjeUdaraca.Odbijeno
const (
	OdbijenOblik     = "oblik"
	OdbijenVlastiti  = "vlastiti-smjer"
	OdbijenPotpis    = "potpis"
	OdbijenPonovljen = "ponovljen"
	OdbijenAdresa    = "odgovor-s-krive-adrese"
	OdbijenPrevise   = "previse-adresa"
)

var razlogUdarca = map[error]string{errOblikUdarca: OdbijenOblik, errVlastitiUdarac: OdbijenVlastiti, errPotpisUdarca: OdbijenPotpis}

// ErrSesijaUdaraca: na utičnici već udara ista sesija
var ErrSesijaUdaraca = errors.New("sesija udaraca je već prijavljena na utičnici")

// Valjan je prvi valjan paket druge strane s jedne adrese
type Valjan struct {
	Adresa  netip.AddrPort
	Lokalni bool // adresa je lokalni kandidat druge strane
	Odgovor bool // stigao je odgovor na udarac ovog računala, a ne udarac
	Kad     time.Time
}

// StanjeUdaraca su brojila jedne sesije
type StanjeUdaraca struct {
	Udaraca, Odgovora int // koliko je ovo računalo poslalo
	Valjanih          int // valjanih paketa druge strane
	GresakaSlanja     int
	Odbijeno          map[string]int // paketi koji nisu prošli, po razlogu
}

// Udarci su udaranje jedne sesije na UDP utičnici WEB synca
type Udarci struct {
	u       *UDP
	k       KljuceviUdarca
	prijem  chan primljeniUdarac
	budi    chan struct{}
	valjani chan Valjan
	kraj    chan struct{}
	gotov   sync.WaitGroup
	jednom  sync.Once

	mu       sync.Mutex
	ciljevi  []ciljUdarca
	od, do   time.Time // prozor udaraca tekućeg pokušaja
	brojac   uint64
	okno     oknoUdaraca
	granica  granicaUdaraca
	naRedu   []netip.AddrPort             // adrese koje čekaju odgovor, svaka jednom
	druge    map[netip.AddrPort]time.Time // adrese druge strane i zadnji valjan paket s njih
	redom    []netip.AddrPort             // iste adrese, redom kojim su se javile
	javljene map[netip.AddrPort]bool      // adrese koje su se u ovom pokušaju javile kao Valjan
	udareni  map[netip.AddrPort]bool
	stanje   StanjeUdaraca
}

type primljeniUdarac struct {
	paket []byte
	od    netip.AddrPort
	kad   time.Time
}

// NoviUdarci prijave sesiju na utičnici u i pokrenu je. Kandidati su
// potpisani kandidati druge strane; vlastite su adrese ovog računala (svi
// njegovi kandidati), koje se ne udaraju. Udarci kreću tek s Pokusaj, a
// odgovori na valjane udarce odmah.
func NoviUdarci(u *UDP, k KljuceviUdarca, kandidati []KandidatPoruke, vlastite []netip.AddrPort) (*Udarci, error) {
	x := &Udarci{u: u, k: k, prijem: make(chan primljeniUdarac, redUdaraca), budi: make(chan struct{}, 1),
		valjani: make(chan Valjan, najviseAdresaDruge), kraj: make(chan struct{}),
		ciljevi: ciljeviUdaraca(kandidati, vlastite), druge: map[netip.AddrPort]time.Time{}, javljene: map[netip.AddrPort]bool{},
		udareni: map[netip.AddrPort]bool{}, stanje: StanjeUdaraca{Odbijeno: map[string]int{}}}
	if err := u.prijaviSesiju(k.sesija, x.prijem); err != nil {
		return nil, err
	}
	x.gotov.Go(x.vrti)
	return x, nil
}

// Pokusaj otvori prozor udaraca novog pokušaja, od od do do: svaki
// kandidat kreće ispočetka, s razmakom od 50 ms, i svaka adresa druge
// strane javlja se ponovno (Valjani). Granica od 60 paketa vrijedi iznova
// kad je prije bio pokušaj u tijeku; odgovori poslani prije prvog pokušaja
// ili nakon Stani ulaze u ovaj.
func (x *Udarci) Pokusaj(od, do time.Time) {
	x.mu.Lock()
	x.zavrsiPokusaj()
	x.od, x.do = od, do
	clear(x.javljene)
	for i := range x.ciljevi {
		x.ciljevi[i].sljedeci, x.ciljevi[i].razmak = time.Time{}, prviRazmakUdarca
	}
	x.mu.Unlock()
	select {
	case x.budi <- struct{}{}:
	default: // budilica već čeka
	}
}

// Stani zaustavi udarce; odgovori na valjane udarce i dalje idu, a broje
// se u granicu idućeg pokušaja
func (x *Udarci) Stani() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.zavrsiPokusaj()
}

// zavrsiPokusaj zatvori prozor udaraca. Pokušaj koji je bio u tijeku
// odnese svoj brojač granice; paketi poslani dok pokušaja nema (prije
// prvoga ili nakon Stani) ostaju u brojaču i ulaze u idući pokušaj.
func (x *Udarci) zavrsiPokusaj() {
	if !x.do.IsZero() {
		x.granica.poslano = 0
	}
	x.do = time.Time{}
}

// Valjani javlja prvi valjan paket druge strane sa svake adrese, u svakom
// pokušaju iznova. Slanje na kanal ne čeka: kad stroj još nije pročitao
// ranije, adresa se javi s idućim valjanim paketom s nje.
func (x *Udarci) Valjani() <-chan Valjan { return x.valjani }

// Pusta javlja smije li adresa do QUIC-a: s nje je u zadnjih 30 s stigao
// valjan udarac ili odgovor druge strane
func (x *Udarci) Pusta(a net.Addr) bool {
	ap, _ := adresaPosluzitelja(a) // adresa koja nije IP i port je prazna, a prazne nema među adresama druge strane
	x.mu.Lock()
	defer x.mu.Unlock()
	kad, ima := x.druge[ap]
	return ima && time.Since(kad) < rokVrata
}

// AdreseDruge su adrese s kojih je stigao valjan paket druge strane, redom
// kojim su se javile: potpisani kandidati i adrese koje vidi tek ovo
// računalo, iza NAT-a druge strane
func (x *Udarci) AdreseDruge() []netip.AddrPort {
	x.mu.Lock()
	defer x.mu.Unlock()
	return slices.Clone(x.redom)
}

// Stanje su brojila sesije
func (x *Udarci) Stanje() StanjeUdaraca {
	x.mu.Lock()
	defer x.mu.Unlock()
	s := x.stanje
	s.Odbijeno = maps.Clone(s.Odbijeno)
	return s
}

// Close odjavi sesiju s utičnice i zaustavi udaranje
func (x *Udarci) Close() {
	x.jednom.Do(func() {
		x.u.odjaviSesiju(x.k.sesija)
		close(x.kraj)
	})
	x.gotov.Wait()
}

// vrti prima pakete druge strane i šalje ono što je na redu
func (x *Udarci) vrti() {
	t := time.NewTimer(bezPosla)
	defer t.Stop()
	for {
		select {
		case <-x.kraj:
			return
		case p := <-x.prijem:
			x.primi(p)
		case <-t.C:
		case <-x.budi:
		}
		t.Reset(x.posaljiNaRedu(time.Now()))
	}
}

// primi provjeri paket druge strane i zapamti ga kad je valjan
func (x *Udarci) primi(p primljeniUdarac) {
	x.mu.Lock()
	defer x.mu.Unlock()
	vrsta, razlog := x.prihvati(p)
	if razlog != "" {
		x.stanje.Odbijeno[razlog]++
		return
	}
	x.zapamti(p, vrsta == vrstaOdgovor)
}

// prihvati je vrsta valjanog paketa, ili razlog odbijanja: krivi oblik,
// vlastiti smjer ili potpis, odgovor s adrese na koju nije poslan udarac,
// previše adresa druge strane i ponovljen brojač
func (x *Udarci) prihvati(p primljeniUdarac) (byte, string) {
	vrsta, brojac, err := x.k.provjeri(p.paket)
	_, poznata := x.druge[p.od]
	switch {
	case err != nil:
		return 0, razlogUdarca[err]
	case vrsta == vrstaOdgovor && !x.udareni[p.od]:
		return 0, OdbijenAdresa
	case !poznata && len(x.druge) >= najviseAdresaDruge:
		return 0, OdbijenPrevise
	case !x.okno.primi(brojac):
		return 0, OdbijenPonovljen
	}
	return vrsta, ""
}

// zapamti valjan paket: adresa ulazi u vrata za QUIC i među adrese druge
// strane, javlja se Valjan kad se u ovom pokušaju još nije javila, a na
// udarac ide odgovor na tu adresu, jednom dok čeka
func (x *Udarci) zapamti(p primljeniUdarac, odgovor bool) {
	x.stanje.Valjanih++
	if _, poznata := x.druge[p.od]; !poznata {
		x.redom = append(x.redom, p.od)
	}
	x.druge[p.od] = p.kad
	x.javi(Valjan{Adresa: p.od, Lokalni: x.lokalni(p.od), Odgovor: odgovor, Kad: p.kad})
	if !odgovor && !slices.Contains(x.naRedu, p.od) {
		x.naRedu = append(x.naRedu, p.od)
	}
}

// javi preda v stroju, kad se ta adresa u ovom pokušaju još nije javila.
// Slanje ne čeka, jer ide pod bravom: kad je kanal pun (ranija javljanja
// koja stroj još nije pročitao), adresa ostaje nejavljena i javi se s
// idućim valjanim paketom.
func (x *Udarci) javi(v Valjan) {
	if x.javljene[v.Adresa] {
		return
	}
	select {
	case x.valjani <- v:
		x.javljene[v.Adresa] = true
	default: // kanal je pun; vidi gore
	}
}

// lokalni: adresa je lokalni kandidat druge strane
func (x *Udarci) lokalni(a netip.AddrPort) bool {
	return slices.ContainsFunc(x.ciljevi, func(c ciljUdarca) bool { return c.adresa == a && c.lokalni })
}

// posaljiNaRedu pošalje paket koji je na redu, kad granica pušta, i vrati
// koliko do idućeg pogleda
func (x *Udarci) posaljiNaRedu(sad time.Time) time.Duration {
	x.mu.Lock()
	b, na, cekaj := x.uzmiPaket(sad)
	x.mu.Unlock()
	if b != nil && x.u.posaljiUdarac(b, na) != nil {
		x.mu.Lock()
		x.stanje.GresakaSlanja++
		x.mu.Unlock()
	}
	return cekaj
}

// uzmiPaket je paket koji smije ići sada (odgovor prije udarca), kamo, i
// koliko do idućeg pogleda
func (x *Udarci) uzmiPaket(sad time.Time) ([]byte, netip.AddrPort, time.Duration) {
	kada, ima := x.kadaSljedeci(sad)
	if ima {
		kada, ima = x.granica.najranije(kada)
	}
	switch {
	case !ima:
		return nil, netip.AddrPort{}, bezPosla
	case kada.After(sad):
		return nil, netip.AddrPort{}, kada.Sub(sad)
	}
	x.granica.zabiljezi(sad)
	x.brojac++
	if len(x.naRedu) > 0 {
		na := x.naRedu[0]
		x.naRedu = x.naRedu[1:]
		x.stanje.Odgovora++
		return x.k.sastavi(vrstaOdgovor, x.brojac), na, razmakSlanja
	}
	b, na := x.udarac(sad)
	return b, na, razmakSlanja
}

// kadaSljedeci je kad je na redu idući paket: odgovor odmah, a udarac kad
// dođe red na kandidata, samo unutar prozora pokušaja
func (x *Udarci) kadaSljedeci(sad time.Time) (time.Time, bool) {
	if len(x.naRedu) > 0 {
		return sad, true
	}
	i := x.prviCilj()
	if i < 0 {
		return time.Time{}, false
	}
	kada := kasnije(x.ciljevi[i].sljedeci, x.od)
	return kada, kada.Before(x.do)
}

// prviCilj je kandidat koji najdulje čeka na udarac; -1 kad ih nema
func (x *Udarci) prviCilj() int {
	i := -1
	for j, c := range x.ciljevi {
		if i < 0 || c.sljedeci.Before(x.ciljevi[i].sljedeci) {
			i = j
		}
	}
	return i
}

// udarac je udarac kandidatu koji je na redu; njegov razmak raste do 200 ms
func (x *Udarci) udarac(sad time.Time) ([]byte, netip.AddrPort) {
	c := &x.ciljevi[x.prviCilj()]
	c.sljedeci, c.razmak = sad.Add(c.razmak), min(2*c.razmak, najveciRazmak)
	x.udareni[c.adresa] = true
	x.stanje.Udaraca++
	return x.k.sastavi(vrstaUdarac, x.brojac), c.adresa
}

// granicaUdaraca je granica paketa jednog pokušaja: razmak od 50 ms i
// najviše 60, s odgovorima poslanima prije njega (zavrsiPokusaj)
type granicaUdaraca struct {
	zadnje  time.Time
	poslano int
}

// najranije je kad smije ići idući paket, najranije u kada; laž kad je
// pokušaj potrošio svih 60
func (g granicaUdaraca) najranije(kada time.Time) (time.Time, bool) {
	if g.poslano >= najviseUdaraca {
		return time.Time{}, false
	}
	return kasnije(kada, g.zadnje.Add(razmakSlanja)), true
}

func (g *granicaUdaraca) zabiljezi(sad time.Time) { g.zadnje, g.poslano = sad, g.poslano+1 }

func kasnije(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// prijaviSesiju: udarci sesije s utičnice idu na kanal c
func (u *UDP) prijaviSesiju(s [16]byte, c chan<- primljeniUdarac) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ima := u.sesije[s]; ima {
		return ErrSesijaUdaraca
	}
	if u.sesije == nil {
		u.sesije = map[[16]byte]chan<- primljeniUdarac{}
	}
	u.sesije[s] = c
	return nil
}

func (u *UDP) odjaviSesiju(s [16]byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.sesije, s)
}

// predajUdarac preda paket oblika udarca sesiji kojoj pripada i javlja je
// li paket bio takav. Udarac nepoznate sesije, bez adrese pošiljatelja
// (nema kamo odgovoriti ni što pustiti kroz vrata) i kad sesija ne stigne
// čitati broji se kao odbačen.
func (u *UDP) predajUdarac(p []byte, od net.Addr, kad time.Time) bool {
	if !jeUdarac(p) {
		return false
	}
	u.mu.Lock()
	c := u.sesije[sesijaUdarca(p)]
	u.mu.Unlock()
	a, ok := adresaPosluzitelja(od)
	if !ok {
		c = nil
	}
	select {
	case c <- primljeniUdarac{paket: p, od: a, kad: kad}:
	default: // nitko ne čeka (nil kanal), ili je red sesije pun
		u.odbaceni.Add(1)
	}
	return true
}

// posaljiUdarac šalje udarac ili odgovor s utičnice; slanje bez rute ne
// mijenja utičnicu (vidi zaglavlje)
func (u *UDP) posaljiUdarac(b []byte, na netip.AddrPort) error {
	_, err := u.tr.WriteTo(b, net.UDPAddrFromAddrPort(na))
	return err
}
