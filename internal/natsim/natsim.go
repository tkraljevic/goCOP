// Paket natsim je NAT usmjernik na simnetu iz quic-go-a, za testove WEB
// synca (plan WEB synca, F3a): internet u memoriji, NAT-ovi s javnim
// adresama, računala na internetu i iza NAT-a i dva lažna STUN
// poslužitelja. Objekti nastaju u mjehuru (testing/synctest), pa kašnjenja
// i istek preslikavanja idu po probnom satu.
//
// NAT zna preslikavanje neovisno o odredištu (EIM), po adresi i po adresi i
// portu (simetrični NAT), filtriranje na ista tri načina, istek
// preslikavanja, čuvanje porta, hairpin, blokiran UDP, više javnih adresa i
// NAT iza NAT-a (dvostruki NAT). Računala iza istog NAT-a smiju imati isti
// unutarnji port, a ista privatna adresa smije postojati u dvije kuće, jer
// svaka kuća ima svoju unutarnju mrežu. Računalo može imati vatrozid koji
// baca nenaručene pakete (ufw). Docker most ima objavljene portove i
// conntrack koji, kad prvi paketi toka krenu u oba smjera istodobno,
// odlaznom toku da drugi port (utrka u Linux conntracku, po Nebuli). Kad je
// red NAT-a pun, paket se baca, kao uz simnet.NewSimConn, a ne čeka, kao uz
// NewBlockingSimConn. NAT može i gubiti zadani udio paketa izvana
// (Gubitak), za probe s gubitkom.
//
// Paket je samo za testove: uvoze ga samo datoteke _test.go
// (TestNitkoNeUvoziNatsim), pa ne ulazi u program.
package natsim

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/testutils/simnet"
)

// istekVatrozida je koliko vatrozid na računalu pamti adresu kojoj je
// računalo slalo (UDP u conntracku, kao ufw)
const istekVatrozida = 30 * time.Second

// Mreza je internet u memoriji s NAT-ovima i računalima. Zatvara se s
// Close, u istom mjehuru u kojem je nastala.
type Mreza struct {
	internet  *segment
	promatrac atomic.Pointer[func(simnet.Packet)]

	mu      sync.Mutex
	linkovi []*simnet.SimulatedLink
	natovi  []*NAT
	stunovi []*STUN
}

// PostavkeRacunala su kašnjenje do računala i njegov vatrozid
type PostavkeRacunala struct {
	Kasnjenje time.Duration // za pakete koji stižu računalu
	Vatrozid  bool          // prima samo s adresa kojima je računalo samo slalo u zadnjih 30 s
}

// Nova je prazan internet
func Nova() *Mreza {
	m := &Mreza{}
	m.internet = noviSegment(m, nil)
	return m
}

// Racunalo je računalo s javnom adresom, izravno na internetu. Utičnicu
// zatvara onaj tko ju je dobio.
func (m *Mreza) Racunalo(adresa netip.AddrPort, p PostavkeRacunala) *simnet.SimConn {
	return m.racunalo(m.internet, adresa, p)
}

// NAT je NAT s javnim adresama na internetu; računala i NAT-ovi iza njega
// dodaju se njegovim metodama Racunalo i NAT
func (m *Mreza) NAT(p Postavke, javne ...netip.Addr) *NAT {
	return m.noviNAT(m.internet, p, javne)
}

// Promatraj zove f za svaki paket koji uđe u bilo koju mrežu, prije
// usmjeravanja: iza NAT-a s unutarnjim adresama, a na internetu s javnim.
// f se zove s gorutine pošiljatelja, pa mora biti siguran za usporedne
// pozive.
func (m *Mreza) Promatraj(f func(simnet.Packet)) { m.promatrac.Store(&f) }

func (m *Mreza) promatraj(p simnet.Packet) {
	if f := m.promatrac.Load(); f != nil {
		(*f)(p)
	}
}

// Close zaustavi STUN poslužitelje, NAT-ove i veze. Utičnice računala
// ostaju njihovim vlasnicima; slanje s njih nakon toga ne ide nikamo.
func (m *Mreza) Close() error {
	m.mu.Lock()
	stunovi, natovi, linkovi := m.stunovi, m.natovi, m.linkovi
	m.stunovi, m.natovi, m.linkovi = nil, nil, nil
	m.mu.Unlock()
	for _, s := range stunovi {
		s.zatvori()
	}
	for _, n := range natovi {
		n.zatvori()
	}
	var greske []error
	for _, l := range linkovi {
		greske = append(greske, l.Close())
	}
	return errors.Join(greske...)
}

// racunalo je utičnica računala u mreži s: paketi koje šalje idu ravno u
// mrežu, a paketi koji mu stižu kasne koliko kaže p
func (m *Mreza) racunalo(s *segment, adresa netip.AddrPort, p PostavkeRacunala) *simnet.SimConn {
	r := &racunalo{segment: s, vatrozid: p.Vatrozid, narudzbe: map[netip.AddrPort]time.Time{}}
	r.link = &simnet.SimulatedLink{Latency: p.Kasnjenje, UploadPacket: r}
	c := simnet.NewSimConn(net.UDPAddrFromAddrPort(adresa), r.link)
	r.link.Start()
	s.dodaj(adresa, r)
	m.mu.Lock()
	m.linkovi = append(m.linkovi, r.link)
	m.mu.Unlock()
	return c
}

// segment je jedna mreža: internet ili unutarnja mreža iza NAT-a
type segment struct {
	m     *Mreza
	izlaz *NAT // NAT kroz koji mreža izlazi van; nil za internet

	mu      sync.Mutex
	cvorovi map[netip.AddrPort]simnet.PacketReceiver
	natovi  map[netip.Addr]*NAT // NAT-ovi kojima je ova mreža vanjska, po javnoj adresi
}

func noviSegment(m *Mreza, izlaz *NAT) *segment {
	return &segment{m: m, izlaz: izlaz, cvorovi: map[netip.AddrPort]simnet.PacketReceiver{}, natovi: map[netip.Addr]*NAT{}}
}

// SendPacket usmjeri paket (simnet.Router); greške nema
func (s *segment) SendPacket(p simnet.Packet) error {
	s.usmjeri(p)
	return nil
}

// usmjeri preda paket NAT-u čija je to javna adresa, računalu u ovoj
// mreži, a inače izlaznom NAT-u. Na internetu paket bez primatelja
// nestaje.
func (s *segment) usmjeri(p simnet.Packet) {
	s.m.promatraj(p)
	do := adresa(p.To)
	s.mu.Lock()
	nat, cvor := s.natovi[do.Addr()], s.cvorovi[do]
	s.mu.Unlock()
	switch {
	case nat != nil:
		nat.primi(p)
	case cvor != nil:
		cvor.RecvPacket(p)
	case s.izlaz != nil:
		s.izlaz.posalji(p)
	}
}

// AddNode upiše primatelja na adresi (simnet.Router)
func (s *segment) AddNode(a net.Addr, r simnet.PacketReceiver) { s.dodaj(adresa(a), r) }

func (s *segment) dodaj(a netip.AddrPort, r simnet.PacketReceiver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cvorovi[a] = r
}

// racunalo je računalo u mreži: ono što šalje bilježi za svoj vatrozid, a
// ono što mu stiže predaje svojoj vezi, kad ga vatrozid pusti
type racunalo struct {
	*segment // AddNode; SendPacket je ovdje
	link     *simnet.SimulatedLink
	vatrozid bool

	mu       sync.Mutex
	narudzbe map[netip.AddrPort]time.Time // komu je računalo slalo, i kad zadnji put
}

// SendPacket je paket koji računalo šalje (uplink veze)
func (r *racunalo) SendPacket(p simnet.Packet) error {
	r.mu.Lock()
	r.narudzbe[adresa(p.To)] = time.Now()
	r.mu.Unlock()
	r.usmjeri(p)
	return nil
}

// RecvPacket je paket koji stiže računalu: vatrozid baca pakete s adresa
// kojima računalo nije slalo
func (r *racunalo) RecvPacket(p simnet.Packet) {
	if r.vatrozid && !r.naruceno(adresa(p.From)) {
		return
	}
	r.link.RecvPacket(p)
}

func (r *racunalo) naruceno(od netip.AddrPort) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	kad, ima := r.narudzbe[od]
	return ima && time.Since(kad) < istekVatrozida
}

// adresa je UDP adresa paketa; u simnetu su sve adrese UDP, a druga daje
// praznu adresu
func adresa(a net.Addr) netip.AddrPort {
	u, ok := a.(*net.UDPAddr)
	if !ok {
		return netip.AddrPort{}
	}
	ap := u.AddrPort()
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
