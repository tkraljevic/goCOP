package natsim

// NAT na simnetu u mjehuru: preslikavanje, filtriranje, istek, čuvanje
// porta, hairpin, blokiran UDP, dvostruki NAT, više javnih adresa, vatrozid
// na računalu, conntrack u Docker mostu, pun red, gubitak i lažni STUN. Računala
// šalju i primaju sirove pakete, a test gleda s koje adrese paket stiže i
// stiže li uopće.

import (
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/testutils/simnet"
)

var (
	javnaNAT = netip.MustParseAddr("203.0.113.1")
	kucaA    = netip.MustParseAddrPort("192.168.1.10:4713")
	kucaB    = netip.MustParseAddrPort("192.168.1.11:4713")
	kucaC    = netip.MustParseAddrPort("192.168.1.12:4713")
	server1  = netip.MustParseAddrPort("198.51.100.1:1000")
	server1b = netip.MustParseAddrPort("198.51.100.1:2000") // ista adresa, drugi port
	server2  = netip.MustParseAddrPort("198.51.100.2:1000")
)

// uMjehuru vodi test u mjehuru; mreža se zatvara na kraju testa
func uMjehuru(t *testing.T, f func(t *testing.T, m *Mreza)) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		m := Nova()
		t.Cleanup(func() {
			if err := m.Close(); err != nil {
				t.Error(err)
			}
		})
		f(t, m)
	})
}

// posalji šalje tekst s utičnice na adresu
func posalji(t *testing.T, c *simnet.SimConn, na netip.AddrPort, tekst string) {
	t.Helper()
	if _, err := c.WriteTo([]byte(tekst), net.UDPAddrFromAddrPort(na)); err != nil {
		t.Fatal(err)
	}
}

// primi čeka paket do 1 s: s koje je adrese stigao i što nosi; ok je laž
// kad nije stigao
func primi(c *simnet.SimConn) (od netip.AddrPort, tekst string, ok bool) {
	_ = c.SetReadDeadline(time.Now().Add(time.Second)) // SimConn rok postavlja bez greške
	b := make([]byte, 1500)
	n, a, err := c.ReadFrom(b)
	if err != nil {
		return netip.AddrPort{}, "", false
	}
	return adresa(a), string(b[:n]), true
}

// vanjska je adresa s koje paket s c stiže do servera s
func vanjska(t *testing.T, c, s *simnet.SimConn, na netip.AddrPort) netip.AddrPort {
	t.Helper()
	posalji(t, c, na, "van")
	od, _, ok := primi(s)
	if !ok {
		t.Fatalf("paket do %s nije stigao", na)
	}
	return od
}

func TestPreslikavanje(t *testing.T) {
	for _, c := range []struct {
		ime     string
		p       Postavke
		s1, s1b string // vanjske adrese prema server1 i server1b
		s2      string
	}{
		{"EIM, čuva port", Postavke{CuvaPort: true}, "203.0.113.1:4713", "203.0.113.1:4713", "203.0.113.1:4713"},
		{"EIM, ne čuva port", Postavke{}, "203.0.113.1:40000", "203.0.113.1:40000", "203.0.113.1:40000"},
		{"po adresi", Postavke{Preslikavanje: PreslikavanjePoAdresi}, "203.0.113.1:40000", "203.0.113.1:40000", "203.0.113.1:40001"},
		{"po adresi i portu", Postavke{Preslikavanje: PreslikavanjePoAdresiIPortu, CuvaPort: true}, "203.0.113.1:4713", "203.0.113.1:40000", "203.0.113.1:40001"},
		{"prvi port", Postavke{PrviPort: 50000}, "203.0.113.1:50000", "203.0.113.1:50000", "203.0.113.1:50000"},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T, m *Mreza) {
				nat := m.NAT(c.p, javnaNAT)
				a := nat.Racunalo(kucaA, PostavkeRacunala{})
				s1, s1b, s2 := m.Racunalo(server1, PostavkeRacunala{}), m.Racunalo(server1b, PostavkeRacunala{}), m.Racunalo(server2, PostavkeRacunala{})
				for _, o := range []struct {
					s    *simnet.SimConn
					na   netip.AddrPort
					want string
				}{{s1, server1, c.s1}, {s1b, server1b, c.s1b}, {s2, server2, c.s2}} {
					if got := vanjska(t, a, o.s, o.na); got.String() != o.want {
						t.Errorf("prema %s: %s, želim %s", o.na, got, o.want)
					}
				}
				if st := nat.Statistika(); st.Van != 3 {
					t.Errorf("statistika %+v", st)
				}
			})
		})
	}
}

func TestFiltriranje(t *testing.T) {
	for _, c := range []struct {
		ime         string
		f           Filtriranje
		s1, s1b, s2 bool // stiže li paket s te adrese
	}{
		{"neovisno", FiltriranjeNeovisno, true, true, true},
		{"po adresi", FiltriranjePoAdresi, true, true, false},
		{"po adresi i portu", FiltriranjePoAdresiIPortu, true, false, false},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T, m *Mreza) {
				nat := m.NAT(Postavke{Filtriranje: c.f, CuvaPort: true}, javnaNAT)
				a := nat.Racunalo(kucaA, PostavkeRacunala{})
				s1, s1b, s2 := m.Racunalo(server1, PostavkeRacunala{}), m.Racunalo(server1b, PostavkeRacunala{}), m.Racunalo(server2, PostavkeRacunala{})
				v := vanjska(t, a, s1, server1)
				filtrirano := 0
				for _, o := range []struct {
					s    *simnet.SimConn
					od   netip.AddrPort
					want bool
				}{{s1, server1, c.s1}, {s1b, server1b, c.s1b}, {s2, server2, c.s2}} {
					posalji(t, o.s, v, "unutra")
					od, tekst, ok := primi(a)
					if ok != o.want || ok && (od != o.od || tekst != "unutra") {
						t.Errorf("s %s: stiglo %v (%s %q), želim %v", o.od, ok, od, tekst, o.want)
					}
					if !o.want {
						filtrirano++
					}
				}
				// na port bez preslikavanja ne stiže ništa
				posalji(t, s1, netip.AddrPortFrom(javnaNAT, 9), "nikome")
				if _, _, ok := primi(a); ok {
					t.Error("paket na port bez preslikavanja je stigao")
				}
				if st := nat.Statistika(); st.Filtrirano != filtrirano || st.BezPreslikavanja != 1 || st.Unutra != 3-filtrirano {
					t.Errorf("statistika %+v", st)
				}
			})
		})
	}
}

// Preslikavanje istekne 30 s nakon zadnjeg paketa van (po probnom satu), a
// održavanje svakih 15 s ga drži; dopuštenje za adresu kojoj se više ne
// šalje istekne i kad preslikavanje živi.
func TestIstekPreslikavanja(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		nat := m.NAT(Postavke{Filtriranje: FiltriranjePoAdresiIPortu}, javnaNAT)
		a := nat.Racunalo(kucaA, PostavkeRacunala{})
		s1, s2 := m.Racunalo(server1, PostavkeRacunala{}), m.Racunalo(server2, PostavkeRacunala{})
		v := vanjska(t, a, s1, server1)
		if vanjska(t, a, s2, server2) != v {
			t.Fatal("EIM je dao drugi port")
		}
		for range 4 { // 60 s održavanja prema server1
			time.Sleep(15 * time.Second)
			if vanjska(t, a, s1, server1) != v {
				t.Fatal("preslikavanje nije preživjelo održavanje")
			}
		}
		posalji(t, s1, v, "s1")
		if _, tekst, ok := primi(a); !ok || tekst != "s1" {
			t.Error("server1 ne stiže uz održavanje")
		}
		posalji(t, s2, v, "s2")
		if _, _, ok := primi(a); ok {
			t.Error("dopuštenje za server2 nije isteklo")
		}
		time.Sleep(31 * time.Second)
		posalji(t, s1, v, "kasno")
		if _, _, ok := primi(a); ok {
			t.Error("preslikavanje nije isteklo")
		}
		if novo := vanjska(t, a, s1, server1); novo == v || novo.Port() != 40001 {
			t.Errorf("nakon isteka %s, prije %s", novo, v)
		}
		if st := nat.Statistika(); st.Filtrirano != 1 || st.BezPreslikavanja != 1 {
			t.Errorf("statistika %+v", st)
		}
	})
}

// Tri računala s istim unutarnjim portom iza istog NAT-a dobiju različite
// vanjske portove, a odgovori stižu pravom računalu. Port koji je zauzelo
// sačuvano preslikavanje drugi ne dobiva.
func TestIstiUnutarnjiPort(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		nat := m.NAT(Postavke{CuvaPort: true}, javnaNAT)
		s1 := m.Racunalo(server1, PostavkeRacunala{})
		racunala := []*simnet.SimConn{nat.Racunalo(kucaA, PostavkeRacunala{}), nat.Racunalo(kucaB, PostavkeRacunala{}), nat.Racunalo(kucaC, PostavkeRacunala{})}
		var vanjske []netip.AddrPort
		for _, r := range racunala {
			vanjske = append(vanjske, vanjska(t, r, s1, server1))
		}
		if vanjske[0].Port() != 4713 || vanjske[1].Port() != 40000 || vanjske[2].Port() != 40001 {
			t.Fatalf("vanjske %v", vanjske)
		}
		for i, r := range racunala {
			posalji(t, s1, vanjske[i], vanjske[i].String())
			if _, tekst, ok := primi(r); !ok || tekst != vanjske[i].String() {
				t.Errorf("računalo %d: %q %v", i, tekst, ok)
			}
		}
		// unutarnji port 40002 je slobodan i sačuva se; iduće računalo bez
		// sačuvanog porta preskoči ga
		d := nat.Racunalo(netip.MustParseAddrPort("192.168.1.13:40002"), PostavkeRacunala{})
		e := nat.Racunalo(netip.MustParseAddrPort("192.168.1.14:4713"), PostavkeRacunala{})
		if v := vanjska(t, d, s1, server1); v.Port() != 40002 {
			t.Errorf("sačuvan port %s", v)
		}
		if v := vanjska(t, e, s1, server1); v.Port() != 40003 {
			t.Errorf("idući slobodan %s", v)
		}
	})
}

// Bez hairpina paket na vlastitu javnu adresu nestaje; s hairpinom stiže
// drugom računalu iza istog NAT-a, s njegove vanjske adrese. U istoj
// kućnoj mreži paket ide izravno.
func TestHairpin(t *testing.T) {
	for _, hairpin := range []bool{false, true} {
		uMjehuru(t, func(t *testing.T, m *Mreza) {
			nat := m.NAT(Postavke{Hairpin: hairpin, CuvaPort: true}, javnaNAT)
			s1 := m.Racunalo(server1, PostavkeRacunala{})
			a, b := nat.Racunalo(kucaA, PostavkeRacunala{}), nat.Racunalo(kucaB, PostavkeRacunala{})
			va, vb := vanjska(t, a, s1, server1), vanjska(t, b, s1, server1)
			posalji(t, a, vb, "kroz javnu")
			od, _, ok := primi(b)
			if ok != hairpin || ok && od != va {
				t.Errorf("hairpin %v: stiglo %v s %s", hairpin, ok, od)
			}
			if st := nat.Statistika(); (st.BezHairpina == 1) == hairpin {
				t.Errorf("hairpin %v: statistika %+v", hairpin, st)
			}
			posalji(t, a, kucaB, "lokalno")
			if od, tekst, ok := primi(b); !ok || od != kucaA || tekst != "lokalno" {
				t.Errorf("lokalno: %v %s %q", ok, od, tekst)
			}
		})
	}
}

func TestBlokiranUDP(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		nat := m.NAT(Postavke{BlokiranUDP: true}, javnaNAT)
		a := nat.Racunalo(kucaA, PostavkeRacunala{})
		s1 := m.Racunalo(server1, PostavkeRacunala{})
		posalji(t, a, server1, "van")
		if _, _, ok := primi(s1); ok {
			t.Error("paket je izašao")
		}
		posalji(t, s1, netip.AddrPortFrom(javnaNAT, 40000), "unutra")
		if _, _, ok := primi(a); ok {
			t.Error("paket je ušao")
		}
		if st := nat.Statistika(); st.Blokirano != 2 || st.Van != 0 {
			t.Errorf("statistika %+v", st)
		}
	})
}

// Dvostruki NAT kod kuće: računalo iza unutarnjeg NAT-a izlazi s javne
// adrese vanjskog, a odgovor prolazi oba.
func TestDvostrukiNAT(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		vanjski := m.NAT(Postavke{CuvaPort: true}, javnaNAT)
		unutarnji := vanjski.NAT(Postavke{CuvaPort: true}, netip.MustParseAddr("192.168.1.2"))
		a := unutarnji.Racunalo(netip.MustParseAddrPort("10.0.0.5:4713"), PostavkeRacunala{})
		s1 := m.Racunalo(server1, PostavkeRacunala{})
		if v := vanjska(t, a, s1, server1); v != netip.AddrPortFrom(javnaNAT, 4713) {
			t.Fatalf("vanjska %s", v)
		}
		posalji(t, s1, netip.AddrPortFrom(javnaNAT, 4713), "kroz oba")
		if od, tekst, ok := primi(a); !ok || od != server1 || tekst != "kroz oba" {
			t.Errorf("odgovor: %v %s %q", ok, od, tekst)
		}
		if vanjski.Statistika().Unutra != 1 || unutarnji.Statistika().Unutra != 1 {
			t.Errorf("statistika %+v, %+v", vanjski.Statistika(), unutarnji.Statistika())
		}
	})
}

// Ured s dvije javne adrese: računalo obično izlazi uvijek s iste, a
// računala se dijele po adresama; uz JavneNaizmjence svako novo
// preslikavanje uzme iduću, pa dva STUN poslužitelja vide dvije adrese.
func TestViseJavnihAdresa(t *testing.T) {
	druga := netip.MustParseAddr("203.0.113.2")
	for _, c := range []struct {
		ime        string
		naizmjence bool
		want       [3]netip.Addr // a prema server1, a prema server2, b prema server1
	}{
		{"uparene", false, [3]netip.Addr{javnaNAT, javnaNAT, druga}},
		{"naizmjence", true, [3]netip.Addr{javnaNAT, druga, javnaNAT}},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T, m *Mreza) {
				nat := m.NAT(Postavke{Preslikavanje: PreslikavanjePoAdresi, JavneNaizmjence: c.naizmjence}, javnaNAT, druga)
				a, b := nat.Racunalo(kucaA, PostavkeRacunala{}), nat.Racunalo(kucaB, PostavkeRacunala{})
				s1, s2 := m.Racunalo(server1, PostavkeRacunala{}), m.Racunalo(server2, PostavkeRacunala{})
				got := [3]netip.Addr{vanjska(t, a, s1, server1).Addr(), vanjska(t, a, s2, server2).Addr(), vanjska(t, b, s1, server1).Addr()}
				if got != c.want {
					t.Errorf("javne %v, želim %v", got, c.want)
				}
				// odgovor na drugu javnu adresu stiže
				vb := vanjska(t, b, s2, server2)
				posalji(t, s2, vb, "druga")
				if _, tekst, ok := primi(b); !ok || tekst != "druga" {
					t.Errorf("odgovor na %s: %q %v", vb, tekst, ok)
				}
			})
		})
	}
}

// Vatrozid na računalu (ufw) prima samo s adresa kojima je računalo samo
// slalo, i to 30 s.
func TestVatrozidNaRacunalu(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		ja := netip.MustParseAddrPort("198.51.100.50:4713")
		a := m.Racunalo(ja, PostavkeRacunala{Vatrozid: true})
		s1, s2 := m.Racunalo(server1, PostavkeRacunala{}), m.Racunalo(server2, PostavkeRacunala{})
		posalji(t, s1, ja, "nenaručeno")
		if _, _, ok := primi(a); ok {
			t.Error("nenaručen paket je prošao")
		}
		posalji(t, a, server1, "pitanje")
		if _, _, ok := primi(s1); !ok {
			t.Fatal("pitanje nije stiglo")
		}
		posalji(t, s1, ja, "odgovor")
		if _, tekst, ok := primi(a); !ok || tekst != "odgovor" {
			t.Errorf("odgovor: %q %v", tekst, ok)
		}
		posalji(t, s2, ja, "tuđi")
		if _, _, ok := primi(a); ok {
			t.Error("paket s adrese kojoj računalo nije slalo je prošao")
		}
		time.Sleep(31 * time.Second)
		posalji(t, s1, ja, "kasno")
		if _, _, ok := primi(a); ok {
			t.Error("vatrozid nije zaboravio adresu nakon 30 s")
		}
	})
}

// docker je spremnik iza Docker mosta s objavljenim portom 4713, conntrack
// s prozorom utrke od 10 ms i filtriranjem po adresi i portu, i udaljeno
// računalo na internetu
func docker(m *Mreza) (nat *NAT, spremnik, daljnji *simnet.SimConn, objavljena, daljnja netip.AddrPort) {
	unutra := netip.MustParseAddrPort("172.17.0.2:4713")
	nat = m.NAT(Postavke{CuvaPort: true, Filtriranje: FiltriranjePoAdresiIPortu, Utrka: 10 * time.Millisecond}, javnaNAT)
	nat.Objavi(4713, unutra)
	daljnja = netip.MustParseAddrPort("198.51.100.7:4713")
	return nat, nat.Racunalo(unutra, PostavkeRacunala{}), m.Racunalo(daljnja, PostavkeRacunala{}), netip.AddrPortFrom(javnaNAT, 4713), daljnja
}

// Conntrack u Docker mostu: kad spremnik i udaljeno računalo krenu slati
// jedno drugome istodobno (ili unutar prozora), odlazni tok spremnika dobije
// drugi vanjski port i drži ga, ma tko bio prvi. Objavljeni port i dalje
// vodi u spremnik, a na novi port udaljeno računalo smije odgovoriti. Kad
// udaljeno računalo krene sekundu ranije, sudara nema.
func TestDockerConntrack(t *testing.T) {
	for _, c := range []struct {
		ime          string
		spremnikPrvi bool
		razmak       time.Duration
		sudar        bool
	}{
		{"istodobno", false, 0, true},
		{"udaljeno pa spremnik unutar prozora", false, 5 * time.Millisecond, true},
		{"spremnik pa udaljeno unutar prozora", true, 5 * time.Millisecond, true},
		{"spremnik sekundu kasnije", false, time.Second, false},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T, m *Mreza) {
				nat, spremnik, daljnji, objavljena, daljnja := docker(m)
				prvi, drugi := func() { posalji(t, daljnji, objavljena, "izvana") }, func() { posalji(t, spremnik, daljnja, "prvi") }
				if c.spremnikPrvi {
					prvi, drugi = drugi, prvi
				}
				prvi()
				time.Sleep(c.razmak)
				drugi()
				if od, _, ok := primi(spremnik); !ok || od != daljnja {
					t.Fatalf("objavljeni port: %v %s", ok, od)
				}
				primi(daljnji) // prvi paket spremnika, s bilo kojeg porta
				time.Sleep(time.Second)
				v := vanjska(t, spremnik, daljnji, daljnja)
				if (v != objavljena) != c.sudar || nat.Statistika().Sudari != map[bool]int{true: 1}[c.sudar] {
					t.Fatalf("drugi paket s %s, statistika %+v", v, nat.Statistika())
				}
				for _, na := range []netip.AddrPort{objavljena, v} {
					posalji(t, daljnji, na, "odgovor")
					if _, tekst, ok := primi(spremnik); !ok || tekst != "odgovor" {
						t.Errorf("na %s: %q %v", na, tekst, ok)
					}
				}
			})
		})
	}
}

// Nakon isteka sudarnog preslikavanja nova utrka da toku novi port. Paket
// izvana na stari, istekli port ne briše novo, živo preslikavanje istog
// toka: tok i dalje izlazi s novog porta.
func TestStaroSudarnoNeBriseNovo(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		_, spremnik, daljnji, objavljena, daljnja := docker(m)
		utrka := func() netip.AddrPort {
			posalji(t, daljnji, objavljena, "izvana")
			synctest.Wait() // paket izvana prvi prođe conntrack, spremnikov je drugi
			posalji(t, spremnik, daljnja, "prvi")
			primi(spremnik)
			primi(daljnji)
			return vanjska(t, spremnik, daljnji, daljnja)
		}
		v1 := utrka()
		time.Sleep(31 * time.Second)
		v2 := utrka()
		if v1 == objavljena || v2 == objavljena || v1 == v2 {
			t.Fatalf("priprema: v1 %s, v2 %s", v1, v2)
		}
		posalji(t, daljnji, v1, "na stari port")
		if _, _, ok := primi(spremnik); ok {
			t.Error("paket na istekli port je stigao")
		}
		if v := vanjska(t, spremnik, daljnji, daljnja); v != v2 {
			t.Errorf("nakon paketa na stari port %s tok izlazi s %s, a ne sa živog %s", v1, v, v2)
		}
	})
}

// Isteklo preslikavanje čiji su ključ i vanjsku adresu preuzela novija
// ostavlja ih novijima; bez njih briše oboje.
func TestMakniOstavljaNovije(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		n := m.NAT(Postavke{}, javnaNAT)
		v := netip.AddrPortFrom(javnaNAT, 40000)
		k1, k2 := kljuc{od: kucaA}, kljuc{od: kucaB}
		staro := n.novo(k1, kucaA, v, time.Now())
		noviKljuc := n.novo(k1, kucaA, netip.AddrPortFrom(javnaNAT, 40001), time.Now())
		novaAdresa := n.novo(k2, kucaB, v, time.Now())
		n.makni(staro)
		if n.odlazna[k1] != noviKljuc || n.vanjska[v] != novaAdresa {
			t.Errorf("makni je obrisao novije: %v, %v", n.odlazna[k1], n.vanjska[v])
		}
		n.makni(novaAdresa)
		if _, ima := n.odlazna[k2]; ima {
			t.Error("ključ nije obrisan")
		}
		if _, ima := n.vanjska[v]; ima {
			t.Error("vanjska adresa nije obrisana")
		}
	})
}

// Uz Gubitak NAT gubi zadani udio paketa izvana, prije reda, i broji ih;
// paketi van ne gube se. Gubitak se smije uključiti i kasnije.
func TestGubitak(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		nat := m.NAT(Postavke{Gubitak: 1, Red: 1024}, javnaNAT)
		a := nat.Racunalo(kucaA, PostavkeRacunala{})
		s1 := m.Racunalo(server1, PostavkeRacunala{})
		v := vanjska(t, a, s1, server1)
		posalji(t, s1, v, "izgubljen")
		if _, _, ok := primi(a); ok {
			t.Error("uz gubitak 1 paket je stigao")
		}
		nat.Gubitak(0)
		posalji(t, s1, v, "stigao")
		if _, _, ok := primi(a); !ok {
			t.Error("bez gubitka paket nije stigao")
		}
		nat.Gubitak(0.2)
		stiglo := 0
		for range 25 { // po 20, da stane u red utičnice
			for range 20 {
				posalji(t, s1, v, "nalet")
			}
			for {
				if _, _, ok := primi(a); !ok {
					break
				}
				stiglo++
			}
		}
		if st := nat.Statistika(); stiglo+st.Izgubljeno != 501 || st.Izgubljeno < 51 || st.Izgubljeno > 151 || st.PunRed != 0 {
			t.Errorf("stiglo %d, statistika %+v", stiglo, st)
		}
	})
}

// Udaljeno računalo pošalje prvo, a spremnik unutar prozora: spremnikov
// paket već ide s drugog porta. Bez objavljenog porta i bez prozora
// conntrack ne radi ništa.
func TestDockerBezUtrke(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		nat := m.NAT(Postavke{CuvaPort: true}, javnaNAT)
		nat.Objavi(4713, kucaA)
		a := nat.Racunalo(kucaA, PostavkeRacunala{})
		s1 := m.Racunalo(server1, PostavkeRacunala{})
		posalji(t, s1, netip.AddrPortFrom(javnaNAT, 4713), "izvana")
		if v := vanjska(t, a, s1, server1); v.Port() != 4713 || nat.Statistika().Sudari != 0 {
			t.Errorf("bez prozora: %s, %+v", v, nat.Statistika())
		}
	})
}

// Pun red NAT-a baca pakete umjesto da čeka: prolaze oni u redu i onaj koji
// NAT upravo obrađuje.
func TestPunRedBacaPaket(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		nat := m.NAT(Postavke{Red: 4, Kasnjenje: 10 * time.Millisecond}, javnaNAT)
		a := nat.Racunalo(kucaA, PostavkeRacunala{})
		s1 := m.Racunalo(server1, PostavkeRacunala{})
		v := vanjska(t, a, s1, server1)
		for range 10 {
			posalji(t, s1, v, "nalet")
		}
		stiglo := 0
		for {
			if _, _, ok := primi(a); !ok {
				break
			}
			stiglo++
		}
		if st := nat.Statistika(); stiglo < 4 || stiglo > 5 || stiglo+st.PunRed != 10 {
			t.Errorf("stiglo %d, statistika %+v", stiglo, st)
		}
	})
}

// Paket koji čeka u redu kad se mreža zatvori ne stiže, a zatvaranje ne
// čeka njegovo kašnjenje.
func TestZatvaranjeSPaketomURedu(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := Nova()
		nat := m.NAT(Postavke{Kasnjenje: time.Minute}, javnaNAT)
		a := nat.Racunalo(kucaA, PostavkeRacunala{})
		s1 := m.Racunalo(server1, PostavkeRacunala{})
		v := vanjska(t, a, s1, server1)
		posalji(t, s1, v, "kasni")
		synctest.Wait()
		pocetak := time.Now()
		if err := m.Close(); err != nil || time.Since(pocetak) != 0 {
			t.Errorf("zatvaranje: %v, trajalo %s", err, time.Since(pocetak))
		}
		if err := m.Close(); err != nil {
			t.Errorf("drugo zatvaranje: %v", err)
		}
		posalji(t, s1, v, "nakon zatvaranja") // ne ide nikamo i ne ruši ništa
	})
}

// Lažni STUN odgovara vanjskom adresom: iza laka NAT-a oba vide istu, iza
// simetričnog različite. Paket koji nije Binding zahtjev ostaje bez
// odgovora.
func TestLazniSTUN(t *testing.T) {
	for _, c := range []struct {
		ime  string
		p    Postavke
		iste bool
	}{
		{"lak", Postavke{CuvaPort: true}, true},
		{"simetrični", Postavke{Preslikavanje: PreslikavanjePoAdresiIPortu, Filtriranje: FiltriranjePoAdresiIPortu}, false},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T, m *Mreza) {
				g, cf := m.DvaSTUN()
				a := m.NAT(c.p, javnaNAT).Racunalo(kucaA, PostavkeRacunala{})
				var vidjeli []netip.AddrPort
				for _, s := range []netip.AddrPort{STUNGoogle, STUNCloudflare} {
					z := zahtjev(byte(len(vidjeli)))
					if _, err := a.WriteTo(z, net.UDPAddrFromAddrPort(s)); err != nil {
						t.Fatal(err)
					}
					od, o, ok := primi(a)
					if !ok || od != s || len(o) != velicinaOdgovor || o[8:20] != string(z[8:20]) {
						t.Fatalf("odgovor %s: %v %x", s, ok, o)
					}
					vidjeli = append(vidjeli, procitajXor([]byte(o)))
				}
				if (vidjeli[0] == vidjeli[1]) != c.iste || vidjeli[0].Addr() != javnaNAT {
					t.Errorf("vidjeli %v", vidjeli)
				}
				posalji(t, a, STUNGoogle, "nije STUN, ali je dulji od dvadeset bajtova")
				if _, _, ok := primi(a); ok {
					t.Error("odgovor na paket koji nije STUN")
				}
				if g.Upiti() != 1 || cf.Upiti() != 1 {
					t.Errorf("upiti %d i %d", g.Upiti(), cf.Upiti())
				}
			})
		})
	}
}

func TestOdgovorSTUNSamoNaZahtjev(t *testing.T) {
	v4 := netip.MustParseAddrPort("198.51.100.7:4713")
	z := zahtjev(1)
	odgovor := append([]byte(nil), z...)
	odgovor[1] = 0x01 // odgovor nije zahtjev
	bezKolacica := append([]byte(nil), z...)
	bezKolacica[4] = 0
	for ime, c := range map[string]struct {
		z  []byte
		od netip.AddrPort
	}{
		"kratak":       {z[:19], v4},
		"odgovor":      {odgovor[1:21], v4},
		"bez kolačića": {bezKolacica, v4},
		"IPv6":         {z, netip.MustParseAddrPort("[2001:db8::1]:4713")},
	} {
		if o := odgovorSTUN(c.z, c.od); o != nil {
			t.Errorf("%s: %x", ime, o)
		}
	}
	if got := procitajXor(odgovorSTUN(z, v4)); got != v4 {
		t.Errorf("adresa %s", got)
	}
}

// zahtjev je Binding zahtjev s ID-om transakcije koji počinje s id
func zahtjev(id byte) []byte {
	z := make([]byte, 20)
	z[1] = 0x01
	z[4], z[5], z[6], z[7] = 0x21, 0x12, 0xA4, 0x42
	z[8] = id
	z[19] = 0x77
	return z
}

// procitajXor čita XOR-MAPPED-ADDRESS iz odgovora lažnog STUN-a
func procitajXor(o []byte) netip.AddrPort {
	port := uint16(o[26])<<8 | uint16(o[27])
	ip := [4]byte{o[28] ^ 0x21, o[29] ^ 0x12, o[30] ^ 0xA4, o[31] ^ 0x42}
	return netip.AddrPortFrom(netip.AddrFrom4(ip), port^0x2112)
}

// Promatrač vidi svaki paket: iza NAT-a s unutarnjom, na internetu s
// vanjskom adresom. Paket na internetu bez primatelja nestaje.
func TestPromatrac(t *testing.T) {
	uMjehuru(t, func(t *testing.T, m *Mreza) {
		var unutra, vani atomic.Int32
		m.Promatraj(func(p simnet.Packet) {
			switch adresa(p.From) {
			case kucaA:
				unutra.Add(1)
			case netip.AddrPortFrom(javnaNAT, 4713):
				vani.Add(1)
			}
		})
		a := m.NAT(Postavke{CuvaPort: true}, javnaNAT).Racunalo(kucaA, PostavkeRacunala{})
		posalji(t, a, netip.MustParseAddrPort("198.51.100.99:1"), "nikome")
		synctest.Wait()
		if unutra.Load() != 1 || vani.Load() != 1 {
			t.Errorf("unutra %d, vani %d", unutra.Load(), vani.Load())
		}
	})
}

// Adresa koja nije UDP je prazna adresa; segment upisuje čvor i preko
// sučelja simnet.Router.
func TestAdresaIUpisCvora(t *testing.T) {
	if a := adresa(&net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1}); a.IsValid() {
		t.Errorf("TCP adresa: %s", a)
	}
	if a := adresa(&net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1}); a != netip.MustParseAddrPort("192.0.2.1:1") {
		t.Errorf("UDP adresa u 16 bajtova: %s", a)
	}
	s := noviSegment(Nova(), nil)
	var r prijemnik
	s.AddNode(&net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1}, &r)
	if err := s.SendPacket(simnet.Packet{To: &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1}}); err != nil || r.n != 1 {
		t.Errorf("preko Routera: %v, primio %d", err, r.n)
	}
}

type prijemnik struct{ n int }

func (r *prijemnik) RecvPacket(simnet.Packet) { r.n++ }
