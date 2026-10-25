package razmjena

// Udaranje jedne sesije (udaranje.go) na internetu iz natsima, u mjehuru:
// dvije strane se vide, vlastiti udarac i vlastita adresa, odgovor s krive
// adrese, odgovor samo na valjan udarac i samo jednom, ponovljeni udarci,
// granice od 20 u sekundi i 60 po pokušaju (s odgovorima izvan pokušaja),
// javljanje adrese u svakom pokušaju, nalet od 33 paketa i slanje bez rute
// koje ne dira utičnicu.

import (
	"errors"
	"net"
	"net/netip"
	"os"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/testutils/simnet"

	"gocop/internal/natsim"
)

var (
	udaracA = netip.MustParseAddrPort("198.51.100.1:4713")
	udaracB = netip.MustParseAddrPort("198.51.100.2:4713")
	udaracZ = netip.MustParseAddrPort("203.0.113.50:4713") // adresa koju vidi tek ovo računalo
	udaracW = netip.MustParseAddrPort("203.0.113.51:4713")
)

// internetUdaraca je internet iz natsima koji se zatvara na kraju testa
func internetUdaraca(t *testing.T) *natsim.Mreza {
	t.Helper()
	m := natsim.Nova()
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m
}

// uticnicaUdaraca je UDP utičnica WEB synca na utičnici iz natsima
func uticnicaUdaraca(t *testing.T, c net.PacketConn) *UDP {
	t.Helper()
	u := NoviUDP(c)
	t.Cleanup(func() { _ = u.Close() }) // greška zatvaranja na kraju testa nema što reći
	return u
}

// javnoRacunalo je UDP utičnica na računalu s javnom adresom, 5 ms od
// interneta
func javnoRacunalo(t *testing.T, m *natsim.Mreza, a netip.AddrPort) *UDP {
	t.Helper()
	return uticnicaUdaraca(t, m.Racunalo(a, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond}))
}

// noviUdarci su udarci sesije koji se zatvaraju na kraju testa
func noviUdarci(t *testing.T, u *UDP, k KljuceviUdarca, kandidati []KandidatPoruke, vlastite ...netip.AddrPort) *Udarci {
	t.Helper()
	x, err := NoviUdarci(u, k, kandidati, vlastite)
	bezGreske(t, err)
	t.Cleanup(x.Close)
	return x
}

func javniKandidati(a ...netip.AddrPort) []KandidatPoruke {
	var out []KandidatPoruke
	for _, x := range a {
		out = append(out, KandidatPoruke{Vrsta: KandidatJavni, Adresa: x.String()})
	}
	return out
}

// paket je paket koji je prošao internetom
type paket struct {
	od, do netip.AddrPort
	podaci []byte
	kad    time.Time
}

// zapisnik pamti pakete na internetu, kao natsim.Promatraj
type zapisnik struct {
	mu     sync.Mutex
	paketi []paket
}

func zapisuj(m *natsim.Mreza) *zapisnik {
	z := &zapisnik{}
	m.Promatraj(func(p simnet.Packet) {
		od, _ := adresaPosluzitelja(p.From) // u natsimu su sve adrese UDP
		do, _ := adresaPosluzitelja(p.To)   // isto
		z.mu.Lock()
		z.paketi = append(z.paketi, paket{od: od, do: do, podaci: slices.Clone(p.Data), kad: time.Now()})
		z.mu.Unlock()
	})
	return z
}

// od su paketi koje je poslala adresa a
func (z *zapisnik) od(a netip.AddrPort) []paket {
	z.mu.Lock()
	defer z.mu.Unlock()
	var out []paket
	for _, p := range z.paketi {
		if p.od == a {
			out = append(out, p)
		}
	}
	return out
}

// procitaj čeka paket na sirovoj utičnici do 1 s
func procitaj(c *simnet.SimConn) ([]byte, netip.AddrPort, bool) {
	_ = c.SetReadDeadline(time.Now().Add(time.Second)) // SimConn rok postavlja bez greške
	b := make([]byte, 1500)
	n, od, err := c.ReadFrom(b)
	if err != nil {
		return nil, netip.AddrPort{}, false
	}
	a, _ := adresaPosluzitelja(od) // u natsimu su sve adrese UDP
	return b[:n], a, true
}

func posaljiSirovo(t *testing.T, c *simnet.SimConn, b []byte, na netip.AddrPort) {
	t.Helper()
	if _, err := c.WriteTo(b, net.UDPAddrFromAddrPort(na)); err != nil {
		t.Fatal(err)
	}
}

// Dvije strane udaraju jedna drugu: svaka dobije valjan paket druge s
// njezine adrese, adresa ulazi u vrata za QUIC i ispada iz njih 30 s nakon
// zadnjeg valjanog paketa.
func TestUdarciSeVide(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, javniKandidati(udaracB), udaracA)
		xb := noviUdarci(t, javnoRacunalo(t, m, udaracB), kb, javniKandidati(udaracA), udaracB)
		sad := time.Now()
		xa.Pokusaj(sad, sad.Add(trajanjeUdaraca))
		xb.Pokusaj(sad, sad.Add(trajanjeUdaraca))
		va, vb := <-xa.Valjani(), <-xb.Valjani()
		if va.Adresa != udaracB || vb.Adresa != udaracA || va.Lokalni || va.Kad.Sub(sad) != 5*time.Millisecond {
			t.Fatalf("valjani %+v, %+v", va, vb)
		}
		if !xa.Pusta(net.UDPAddrFromAddrPort(udaracB)) || !xb.Pusta(net.UDPAddrFromAddrPort(udaracA)) || xa.Pusta(net.UDPAddrFromAddrPort(udaracZ)) || xa.Pusta(nil) {
			t.Error("vrata za QUIC")
		}
		time.Sleep(time.Second)
		xa.Stani()
		xb.Stani()
		time.Sleep(rokVrata + time.Second)
		if xa.Pusta(net.UDPAddrFromAddrPort(udaracB)) {
			t.Error("vrata otvorena 30 s nakon zadnjeg paketa")
		}
		s := xa.Stanje()
		if s.Udaraca < 5 || s.Odgovora < 5 || s.Valjanih < 10 || len(s.Odbijeno) != 0 || s.GresakaSlanja != 0 || !slices.Equal(xa.AdreseDruge(), []netip.AddrPort{udaracB}) {
			t.Errorf("stanje %+v, adrese %v", s, xa.AdreseDruge())
		}
	})
}

// Vlastiti udarac vraćen pošiljatelju (zrcalo na javnoj mreži) se odbija:
// nema odgovora, nove adrese ni vrata.
func TestVlastitiUdaracVracen(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, _ := kljuceviPara(t)
		zrcalo := m.Racunalo(udaracZ, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond})
		defer zrcalo.Close()
		z := zapisuj(m)
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, javniKandidati(udaracZ), udaracA)
		sad := time.Now()
		xa.Pokusaj(sad, sad.Add(10*time.Millisecond)) // jedan udarac
		p, _, ok := procitaj(zrcalo)
		if !ok {
			t.Fatal("udarac nije stigao")
		}
		posaljiSirovo(t, zrcalo, p, udaracA)
		time.Sleep(time.Second)
		s := xa.Stanje()
		if s.Odbijeno[OdbijenVlastiti] != 1 || s.Valjanih != 0 || s.Odgovora != 0 || len(xa.AdreseDruge()) != 0 || xa.Pusta(net.UDPAddrFromAddrPort(udaracZ)) {
			t.Errorf("stanje %+v", s)
		}
		if n := len(z.od(udaracA)); n != 1 {
			t.Errorf("A je poslao %d paketa, želim samo udarac", n)
		}
		select {
		case v := <-xa.Valjani():
			t.Errorf("valjan %+v", v)
		default:
		}
	})
}

// Kandidat druge strane jednak vlastitoj adresi ne dobiva udarac.
func TestVlastitaAdresaSeNeUdara(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		z := zapisuj(m)
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, javniKandidati(udaracA, udaracB), udaracA)
		noviUdarci(t, javnoRacunalo(t, m, udaracB), kb, nil, udaracB)
		sad := time.Now()
		xa.Pokusaj(sad, sad.Add(trajanjeUdaraca))
		time.Sleep(4 * time.Second)
		for _, p := range z.od(udaracA) {
			if p.do != udaracB {
				t.Errorf("udarac na %s", p.do)
			}
		}
		if s := xa.Stanje(); s.Udaraca == 0 || s.Odbijeno[OdbijenVlastiti] != 0 {
			t.Errorf("stanje %+v", s)
		}
	})
}

// Odgovor s adrese na koju nije poslan udarac se ne prima. Na valjan udarac
// s adrese koja nije potpisani kandidat odgovor ide samo na tu adresu i
// samo jednom po udarcu, a ta adresa postaje adresa druge strane. Ponovljen
// udarac se odbacuje, a na adresu bez valjanog udarca ne ide ništa.
func TestOdgovorSamoNaValjanUdarac(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		nat := m.Racunalo(udaracZ, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond}) // druga strana iza NAT-a
		defer nat.Close()
		tudji := m.Racunalo(udaracW, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond})
		defer tudji.Close()
		z := zapisuj(m)
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, javniKandidati(udaracB), udaracA)

		posaljiSirovo(t, nat, kb.sastavi(vrstaOdgovor, 1), udaracA)
		udarac := kb.sastavi(vrstaUdarac, 2)
		posaljiSirovo(t, nat, udarac, udaracA)
		odgovor, od, ok := procitaj(nat)
		if !ok || od != udaracA {
			t.Fatalf("odgovor: %v s %s", ok, od)
		}
		if v, _, err := kb.provjeri(odgovor); err != nil || v != vrstaOdgovor || len(odgovor) != len(udarac) {
			t.Errorf("odgovor %x: %v", odgovor, err)
		}
		posaljiSirovo(t, nat, udarac, udaracA) // ponovljen
		pokvaren := kb.sastavi(vrstaUdarac, 3)
		pokvaren[40] ^= 1
		posaljiSirovo(t, tudji, pokvaren, udaracA)
		for _, c := range []*simnet.SimConn{nat, tudji} {
			if p, _, ok := procitaj(c); ok {
				t.Errorf("drugi paket: %x", p)
			}
		}
		s := xa.Stanje()
		if s.Odbijeno[OdbijenAdresa] != 1 || s.Odbijeno[OdbijenPonovljen] != 1 || s.Odbijeno[OdbijenPotpis] != 1 || s.Valjanih != 1 || s.Odgovora != 1 || s.Udaraca != 0 {
			t.Errorf("stanje %+v", s)
		}
		if v := <-xa.Valjani(); v.Adresa != udaracZ || v.Odgovor || v.Lokalni {
			t.Errorf("valjan %+v", v)
		}
		if !slices.Equal(xa.AdreseDruge(), []netip.AddrPort{udaracZ}) || !xa.Pusta(net.UDPAddrFromAddrPort(udaracZ)) || xa.Pusta(net.UDPAddrFromAddrPort(udaracW)) {
			t.Errorf("adrese %v", xa.AdreseDruge())
		}
		if n := len(z.od(udaracA)); n != 1 {
			t.Errorf("A je poslao %d paketa", n)
		}
	})
}

// Odgovor na udarac s adrese na koju je poslan udarac se prima i javlja
// kao Valjan s oznakom odgovora; adresa lokalnog kandidata nosi oznaku.
func TestOdgovorNaUdarac(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		b := m.Racunalo(udaracB, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond})
		defer b.Close()
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, []KandidatPoruke{{Vrsta: KandidatLokalni, Adresa: udaracB.String()}}, udaracA)
		sad := time.Now()
		xa.Pokusaj(sad, sad.Add(trajanjeUdaraca))
		if _, _, ok := procitaj(b); !ok {
			t.Fatal("udarac nije stigao")
		}
		posaljiSirovo(t, b, kb.sastavi(vrstaOdgovor, 1), udaracA)
		if v := <-xa.Valjani(); v.Adresa != udaracB || !v.Odgovor || !v.Lokalni {
			t.Errorf("valjan %+v", v)
		}
		if s := xa.Stanje(); s.Odgovora != 0 || s.Valjanih != 1 {
			t.Errorf("na odgovor se ne odgovara: %+v", s)
		}
	})
}

// najviseUSekundi je najveći broj paketa u bilo kojoj sekundi
func najviseUSekundi(p []paket) int {
	n := 0
	for i := range p {
		j := i
		for j < len(p) && p[j].kad.Sub(p[i].kad) < time.Second {
			j++
		}
		n = max(n, j-i)
	}
	return n
}

// Osam kandidata dobiva udarce naizmjence, svaki otprilike svakih 400 ms:
// najviše 20 u sekundi i 60 po pokušaju, a novi pokušaj ima novih 60.
// Odgovor ulazi u istu granicu.
func TestGraniceUdaraca(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		var ciljevi []netip.AddrPort
		for i := range 8 {
			a := netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, byte(i + 1)}), 4713)
			ciljevi = append(ciljevi, a)
			defer m.Racunalo(a, natsim.PostavkeRacunala{}).Close()
		}
		nat := m.Racunalo(udaracZ, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond})
		defer nat.Close()
		z := zapisuj(m)
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, javniKandidati(ciljevi...), udaracA)
		pocetak := time.Now()
		xa.Pokusaj(pocetak, pocetak.Add(trajanjeUdaraca))
		time.Sleep(3500 * time.Millisecond)
		poslano := z.od(udaracA)
		if len(poslano) != najviseUdaraca || najviseUSekundi(poslano) != 20 {
			t.Fatalf("poslano %d, najviše %d u sekundi", len(poslano), najviseUSekundi(poslano))
		}
		for i, p := range poslano {
			if p.do != ciljevi[i%8] || p.kad.Sub(pocetak) != time.Duration(i)*razmakSlanja {
				t.Errorf("%d. udarac na %s u %s", i, p.do, p.kad.Sub(pocetak))
			}
		}
		// granica je potrošena: valjan udarac druge strane čeka odgovor do
		// idućeg pokušaja
		posaljiSirovo(t, nat, kb.sastavi(vrstaUdarac, 1), udaracA)
		if _, _, ok := procitaj(nat); ok {
			t.Error("odgovor mimo granice")
		}
		drugi := time.Now()
		xa.Pokusaj(drugi, drugi.Add(trajanjeUdaraca))
		if _, _, ok := procitaj(nat); !ok {
			t.Error("odgovor nije otišao u novom pokušaju")
		}
		time.Sleep(4 * time.Second)
		if n := len(z.od(udaracA)) - len(poslano); n != najviseUdaraca {
			t.Errorf("drugi pokušaj: %d paketa", n)
		}
	})
}

// Odgovori poslani prije prvog pokušaja ulaze u njegovu granicu od 60, a
// odgovori nakon Stani u granicu idućeg; pokušaj koji slijedi pokušaj ima
// novih 60. Tako ni pozvani kojemu udarci stignu prije sinkra ne pošalje u
// pokušaju više od 60 paketa.
func TestGranicaSOdgovorimaIzvanPokusaja(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		var ciljevi []netip.AddrPort // osam kandidata koji ne odgovaraju: udarci pune granicu
		for i := range 8 {
			a := netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, byte(i + 1)}), 4713)
			ciljevi = append(ciljevi, a)
			defer m.Racunalo(a, natsim.PostavkeRacunala{}).Close()
		}
		nat := m.Racunalo(udaracZ, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond})
		defer nat.Close()
		z := zapisuj(m)
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, javniKandidati(ciljevi...), udaracA)
		brojac := uint64(0)
		odgovori := func(n int) {
			for i := range n {
				brojac++
				posaljiSirovo(t, nat, kb.sastavi(vrstaUdarac, brojac), udaracA)
				if _, _, ok := procitaj(nat); !ok {
					t.Fatalf("%d. odgovor nije stigao", i)
				}
			}
		}
		pokusaj := func() int {
			prije, sad := len(z.od(udaracA)), time.Now()
			xa.Pokusaj(sad, sad.Add(trajanjeUdaraca))
			time.Sleep(4 * time.Second)
			return len(z.od(udaracA)) - prije
		}
		odgovori(10)
		if n := pokusaj(); n != najviseUdaraca-10 {
			t.Errorf("1. pokušaj nakon 10 odgovora: %d paketa", n)
		}
		xa.Stani()
		xa.Stani() // drugi Stani ne briše odgovore izvan pokušaja
		odgovori(5)
		if n := pokusaj(); n != najviseUdaraca-5 {
			t.Errorf("2. pokušaj nakon 5 odgovora: %d paketa", n)
		}
		if n := pokusaj(); n != najviseUdaraca {
			t.Errorf("3. pokušaj odmah nakon 2.: %d paketa", n)
		}
	})
}

// Prvi valjan paket s jedne adrese javlja se jednom u pokušaju, a u idućem
// ponovno. Kad je kanal pun (stroj nije pročitao javljanja iz prošlog
// pokušaja), slanje ne čeka, a adresa se javi s idućim valjanim paketom.
func TestValjanUSvakomPokusaju(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, nil, udaracA)
		var izvori []*simnet.SimConn
		var adrese []netip.AddrPort
		for i := range najviseAdresaDruge {
			a := netip.AddrPortFrom(netip.AddrFrom4([4]byte{203, 0, 113, byte(100 + i)}), 4713)
			c := m.Racunalo(a, natsim.PostavkeRacunala{})
			defer c.Close()
			izvori, adrese = append(izvori, c), append(adrese, a)
		}
		brojac := uint64(0)
		udari := func(i int) {
			brojac++
			posaljiSirovo(t, izvori[i], kb.sastavi(vrstaUdarac, brojac), udaracA)
			time.Sleep(100 * time.Millisecond)
		}
		nista := func(kad string) {
			select {
			case v := <-xa.Valjani():
				t.Errorf("%s: valjan %+v", kad, v)
			default:
			}
		}
		novi := func() { xa.Pokusaj(time.Now(), time.Now()) } // bez kandidata: samo novi pokušaj
		udari(0)
		udari(0)
		if v := <-xa.Valjani(); v.Adresa != adrese[0] {
			t.Errorf("valjan %+v", v)
		}
		nista("ista adresa u istom pokušaju")
		novi()
		udari(0)
		if v := <-xa.Valjani(); v.Adresa != adrese[0] {
			t.Errorf("u idućem pokušaju %+v", v)
		}
		novi()
		for i := range izvori {
			udari(i)
		}
		novi()
		udari(0) // kanal je pun
		for i := range najviseAdresaDruge {
			if v := <-xa.Valjani(); v.Adresa != adrese[i] {
				t.Errorf("%d. javljanje %+v", i, v)
			}
		}
		nista("uz pun kanal")
		udari(0)
		if v := <-xa.Valjani(); v.Adresa != adrese[0] {
			t.Errorf("nakon punog kanala %+v", v)
		}
		if s := xa.Stanje(); s.Valjanih != 21 || s.Odgovora != 21 {
			t.Errorf("stanje %+v", s)
		}
	})
}

// Jednom kandidatu razmak raste 50, 100, 200 ms i ostaje 200 ms; Stani
// zaustavi udarce.
func TestRazmakUdaraca(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, _ := kljuceviPara(t)
		defer m.Racunalo(udaracB, natsim.PostavkeRacunala{}).Close()
		z := zapisuj(m)
		xa := noviUdarci(t, javnoRacunalo(t, m, udaracA), ka, javniKandidati(udaracB), udaracA)
		pocetak := time.Now().Add(time.Second) // prozor kreće za sekundu
		xa.Pokusaj(pocetak, pocetak.Add(trajanjeUdaraca))
		time.Sleep(5 * time.Second)
		poslano := z.od(udaracA)
		want := []time.Duration{0, 50, 150, 350, 550}
		if len(poslano) != 17 {
			t.Fatalf("poslano %d", len(poslano))
		}
		for i, w := range want {
			if got := poslano[i].kad.Sub(pocetak); got != w*time.Millisecond {
				t.Errorf("%d. udarac u %s, želim %d ms", i, got, w)
			}
		}
		xa.Pokusaj(time.Now(), time.Now().Add(trajanjeUdaraca))
		time.Sleep(100 * time.Millisecond)
		xa.Stani()
		prije := len(z.od(udaracA))
		time.Sleep(time.Second)
		if prije != len(poslano)+2 || len(z.od(udaracA)) != prije {
			t.Errorf("nakon Stani: %d, prije %d", len(z.od(udaracA)), prije)
		}
	})
}

// 33 paketa odjednom, s 33 adrese (jedan više nego što stane u red
// utičnice): odgovori idu samo na adrese druge strane do granice od 16, i
// ne brže od 20 u sekundi; nalet od 33 pokvarena udarca ne dobiva nijedan
// odgovor. Utičnica nakon naleta radi.
func Test33PaketaNaglo(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		z := zapisuj(m)
		ua := javnoRacunalo(t, m, udaracA)
		xa := noviUdarci(t, ua, ka, javniKandidati(udaracB), udaracA)
		var izvori []*simnet.SimConn
		for i := range 33 {
			c := m.Racunalo(netip.AddrPortFrom(netip.AddrFrom4([4]byte{203, 0, 113, byte(100 + i)}), 4713), natsim.PostavkeRacunala{})
			defer c.Close()
			izvori = append(izvori, c)
		}
		for _, c := range izvori {
			pokvaren := kb.sastavi(vrstaUdarac, 1)
			pokvaren[59] ^= 1
			posaljiSirovo(t, c, pokvaren, udaracA)
		}
		time.Sleep(time.Second)
		if n := len(z.od(udaracA)); n != 0 {
			t.Fatalf("na pokvarene udarce otišlo %d paketa", n)
		}
		for i, c := range izvori {
			posaljiSirovo(t, c, kb.sastavi(vrstaUdarac, uint64(10+i)), udaracA)
		}
		time.Sleep(2 * time.Second)
		odgovori := z.od(udaracA)
		vidjene := map[netip.AddrPort]bool{}
		for _, p := range odgovori {
			if vidjene[p.do] || !slices.Contains(xa.AdreseDruge(), p.do) {
				t.Errorf("odgovor na %s", p.do)
			}
			vidjene[p.do] = true
		}
		s := xa.Stanje()
		if len(odgovori) != najviseAdresaDruge || najviseUSekundi(odgovori) > 20 || s.Valjanih != najviseAdresaDruge || s.Odbijeno[OdbijenPrevise] == 0 ||
			s.Valjanih+s.Odbijeno[OdbijenPrevise]+s.Odbijeno[OdbijenPotpis] > 66 || !ua.Radi() {
			t.Errorf("odgovora %d (najviše %d u sekundi), stanje %+v", len(odgovori), najviseUSekundi(odgovori), s)
		}
		posaljiSirovo(t, izvori[0], kb.sastavi(vrstaUdarac, 100), udaracA)
		if _, _, ok := procitaj(izvori[0]); !ok {
			t.Error("nakon naleta nema odgovora")
		}
	})
}

// spavaConn je utičnica čije slanje uvijek javi da odredište ne odgovara
// (EHOSTDOWN na macOS-u i Linuxu): tako izgleda udarac na lokalnog
// kandidata računala koje spava
type spavaConn struct{ net.PacketConn }

func (spavaConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendto", greskeBezRute[len(greskeBezRute)-1])}
}

// Udarac koji ne ode zbog greške bez rute ne traži novu utičnicu: to je
// odredište koje spava, a ne ruta ovog računala (STUN bi je tražio).
func TestUdaracBezRuteNeMijenjaUticnicu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, _ := kljuceviPara(t)
		u := uticnicaUdaraca(t, spavaConn{m.Racunalo(udaracA, natsim.PostavkeRacunala{})})
		xa := noviUdarci(t, u, ka, javniKandidati(udaracB), udaracA)
		sad := time.Now()
		xa.Pokusaj(sad, sad.Add(trajanjeUdaraca))
		time.Sleep(time.Second)
		if s := xa.Stanje(); s.GresakaSlanja == 0 || s.GresakaSlanja != s.Udaraca || u.BezRute() {
			t.Errorf("stanje %+v, bez rute %v", s, u.BezRute())
		}
		if !BezRute((spavaConn{}).greska()) {
			t.Error("ista greška kod STUN-a ne bi tražila novu utičnicu")
		}
	})
}

func (c spavaConn) greska() error {
	_, err := c.WriteTo(nil, nil)
	return err
}

// Ista sesija na istoj utičnici smije biti samo jednom; nakon Close opet
// smije. Udarac nepoznate sesije broji se kao odbačen i ne ide STUN-u.
func TestSesijaUdaracaNaUticnici(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := kljuceviPara(t)
		u := javnoRacunalo(t, m, udaracA)
		x := noviUdarci(t, u, ka, nil)
		if _, err := NoviUdarci(u, ka, nil, nil); !errors.Is(err, ErrSesijaUdaraca) {
			t.Errorf("druga ista sesija: %v", err)
		}
		x.Close()
		x.Close()
		noviUdarci(t, u, ka, nil)
		_, nepoznata := kljuceviPara(t)
		nepoznata.sesija[0] ^= 1
		izvor := m.Racunalo(udaracB, natsim.PostavkeRacunala{})
		defer izvor.Close()
		posaljiSirovo(t, izvor, nepoznata.sastavi(vrstaUdarac, 1), udaracA)
		synctest.Wait()
		time.Sleep(10 * time.Millisecond)
		if u.Odbaceni() != 1 {
			t.Errorf("odbačeno %d", u.Odbaceni())
		}
		// udarac bez adrese pošiljatelja ne stiže do sesije
		if u.predajUdarac([]byte{0x10}, nil, time.Now()) || !u.predajUdarac(kb.sastavi(vrstaUdarac, 1), &net.IPAddr{}, time.Now()) || u.Odbaceni() != 2 {
			t.Errorf("prepoznavanje udarca, odbačeno %d", u.Odbaceni())
		}
	})
}
