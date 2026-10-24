package razmjena

// Probijanje kroz natsim (udaranje.go, probijanje.go), u mjehuru: dvije
// strane iza zadanih NAT-ova izmjere mrežu lažnim STUN-om, razmijene
// poziv, odziv i sinkro preko lažnog sastajališta (zadano 20 ms u svakom
// smjeru) i udaraju. QUIC-a ovdje još nema (F3a-2): vozač testa ga glumi.
// Kad su obje strane u istom trenutku u radnji QUIC, svaka pošalje probne
// pakete kroz natsim na adresu koju je odabrala, a veza uspije samo kad do
// svake stigne paket druge, s adrese koju ona pušta kroz vrata
// (Udarci.Pusta). Tako „uspjeh” znači da put stvarno radi u oba smjera.

import (
	"bytes"
	"crypto/ed25519"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"gocop/internal/natsim"
)

const (
	kasnjenjeSignala = 20 * time.Millisecond
	probnihPaketa    = 3                      // lažni QUIC ponavlja probu, kao rukovanje
	rokProbe         = 120 * time.Millisecond // koliko lažni QUIC čeka probne pakete
)

var (
	posluziteljiNatsima = []PosluziteljSTUN{
		{Ime: "Google", Adresa: net.UDPAddrFromAddrPort(natsim.STUNGoogle)},
		{Ime: "Cloudflare", Adresa: net.UDPAddrFromAddrPort(natsim.STUNCloudflare)},
	}
	eimNeovisno = natsim.Postavke{CuvaPort: true, Kasnjenje: 10 * time.Millisecond}
	eimPoPortu  = natsim.Postavke{CuvaPort: true, Filtriranje: natsim.FiltriranjePoAdresiIPortu, Kasnjenje: 10 * time.Millisecond}
	simetricni  = natsim.Postavke{Preslikavanje: natsim.PreslikavanjePoAdresiIPortu, Filtriranje: natsim.FiltriranjePoAdresiIPortu, Kasnjenje: 10 * time.Millisecond}
	blokiran    = natsim.Postavke{BlokiranUDP: true}
	dockerMost  = natsim.Postavke{CuvaPort: true, Filtriranje: natsim.FiltriranjePoAdresiIPortu, Utrka: 10 * time.Millisecond, Kasnjenje: 10 * time.Millisecond}
)

// stranaProbe je računalo u probi: utičnica, izmjerena mreža i kandidati
type stranaProbe struct {
	kljuc     ed25519.PublicKey
	u         *UDP
	nat       *NATPoruke
	kandidati []KandidatPoruke
	vlastite  []netip.AddrPort
}

// stranaIza je računalo na lokalnoj adresi iza NAT-a n: izmjeri mrežu i
// nudi lokalni kandidat, a javni samo uz lak NAT (SJavnim)
func stranaIza(t *testing.T, n *natsim.NAT, lokalna string, p natsim.PostavkeRacunala) *stranaProbe {
	t.Helper()
	a := netip.MustParseAddrPort(lokalna)
	p.Kasnjenje = max(p.Kasnjenje, time.Millisecond)
	s := &stranaProbe{kljuc: newKey(t).Public().(ed25519.PublicKey), u: uticnicaUdaraca(t, n.Racunalo(a, p))}
	var stanje StanjeMreze
	m := s.u.ProvjeriMrezu(t.Context(), posluziteljiNatsima)
	stanje.Dodaj(m)
	s.nat = &NATPoruke{Vrsta: stanje.Vrsta, UDPBlokiran: stanje.Vrsta == UDPBlokiran}
	for _, k := range SJavnim([]Kandidat{{Vrsta: KandidatLokalni, Adresa: a}}, m, nil) {
		s.kandidati = append(s.kandidati, KandidatPoruke{Vrsta: k.Vrsta, Adresa: k.Adresa.String()})
		s.vlastite = append(s.vlastite, k.Adresa)
	}
	return s
}

// parIza su A (192.168.1.10) i B (192.168.2.20) iza svojih NAT-ova
func parIza(t *testing.T, m *natsim.Mreza, pa, pb natsim.Postavke) (a, b *stranaProbe, na, nb *natsim.NAT) {
	t.Helper()
	na, nb = m.NAT(pa, netip.MustParseAddr("203.0.113.1")), m.NAT(pb, netip.MustParseAddr("203.0.113.2"))
	return stranaIza(t, na, "192.168.1.10:4713", natsim.PostavkeRacunala{}), stranaIza(t, nb, "192.168.2.20:4713", natsim.PostavkeRacunala{}), na, nb
}

// uvjetiProbe su uvjeti jedne probe; nula je sastajalište od 20 ms u
// svakom smjeru i QUIC koji uspije kad put radi
type uvjetiProbe struct {
	doA, doB   time.Duration // koliko poruke sastajališta putuju do A (odziv) i do B (poziv, sinkro); 0: 20 ms
	sinkroVise time.Duration // sinkro do B putuje još toliko (kolebanje sastajališta)
	padni      int           // koliko prvih susreta QUIC-a ne uspije, kao izgubljeno rukovanje
}

// vozac vozi stroj jedne strane: događaje iz sata, sastajališta, udaraca i
// lažnog QUIC-a, a radnje predaje udarcima, drugoj strani i lažnom QUIC-u
type vozac struct {
	ime         string
	p           Probijanje
	s           *stranaProbe
	x           *Udarci
	druga       *vozac
	kasnjenje   time.Duration // koliko poruke sastajališta putuju do ove strane
	signal      chan Dogadjaj
	quic        chan ishodQUIC
	d           *dogovorQUIC
	posta       *sync.WaitGroup
	quicova     int            // radnji QUIC dosad
	adresa      netip.AddrPort // adresa zadnje radnje QUIC
	prijeSinkra int            // valjanih paketa koji su stigli dok je strana čekala sinkro
}

// ishodQUIC je ishod lažnog QUIC-a za jednu radnju QUIC
type ishodQUIC struct {
	radnja int
	uspjeh bool
}

func noviVozac(ime string, s *stranaProbe, x *Udarci, kasnjenje time.Duration, d *dogovorQUIC, posta *sync.WaitGroup) *vozac {
	return &vozac{ime: ime, s: s, x: x, kasnjenje: kasnjenje, signal: make(chan Dogadjaj, 8), quic: make(chan ishodQUIC, 4), d: d, posta: posta}
}

// dostavi preda poruku ovoj strani preko sastajališta, nakon njezina
// kašnjenja i još vise
func (v *vozac) dostavi(vrsta VrstaDogadjaja, t TijeloPoruke, vise time.Duration) {
	v.posta.Go(func() {
		time.Sleep(v.kasnjenje + vise)
		v.signal <- Dogadjaj{Vrsta: vrsta, Sad: time.Now(), Poruka: t} // kanal ima mjesta za sve poruke jedne probe
	})
}

func (v *vozac) vozi() {
	for !v.p.Zavrseno() {
		t := time.NewTimer(time.Until(v.p.Budjenje()))
		var d Dogadjaj
		select {
		case <-t.C:
			d = sat(time.Now())
		case d = <-v.signal:
		case val := <-v.x.Valjani():
			d = val.Dogadjaj()
			if v.p.Faza == FazaCekaSinkro {
				v.prijeSinkra++
			}
		case r := <-v.quic:
			if r.radnja == v.quicova { // ishod starije radnje QUIC ne znači ništa
				d = Dogadjaj{Vrsta: DogadjajQUIC, Sad: time.Now(), Uspjeh: r.uspjeh}
			}
		}
		t.Stop()
		var radnje []Radnja
		v.p, radnje = v.p.Prijelaz(d)
		for _, r := range radnje {
			v.izvrsi(r)
		}
	}
}

func (v *vozac) izvrsi(r Radnja) {
	switch r.Vrsta {
	case RadnjaSinkro:
		v.druga.dostavi(DogadjajSinkro, TijeloPoruke{Sesija: sesijaUdaraca, Od: v.ime, Pokusaj: r.Pokusaj, RTTMs: r.RTTMs}, v.d.uvjeti.sinkroVise)
	case RadnjaUdaraj:
		v.x.Pokusaj(r.Od, r.Do)
	case RadnjaStani:
		v.x.Stani()
	case RadnjaQUIC:
		v.quicova++
		v.adresa = r.Adresa
		v.d.javi(v, r.Adresa)
	case RadnjaPrekiniQUIC:
		v.d.odustani(v)
	}
}

// dogovorQUIC glumi QUIC: kad su obje strane u radnji QUIC, svaka do tri
// puta pošalje probni paket na adresu svoje radnje, a veza uspije kad do
// svake stigne paket druge, s adrese koju pušta kroz vrata. Prvih padni
// susreta ne uspije.
type dogovorQUIC struct {
	uvjeti uvjetiProbe

	mu      sync.Mutex
	ceka    map[*vozac]cekaQUIC
	susreta int
	probe   sync.WaitGroup
}

// cekaQUIC je strana u radnji QUIC: koja je to radnja i na koju adresu
type cekaQUIC struct {
	v      *vozac
	radnja int
	adresa netip.AddrPort
}

func (d *dogovorQUIC) javi(v *vozac, a netip.AddrPort) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ova := cekaQUIC{v: v, radnja: v.quicova, adresa: a}
	druga, ima := d.ceka[v.druga]
	if !ima {
		d.ceka[v] = ova
		return
	}
	delete(d.ceka, v.druga)
	d.susreta++
	n, padne := d.susreta, d.susreta <= d.uvjeti.padni
	d.probe.Go(func() {
		uspjeh := !padne && probaPuta(ova, druga, n)
		ova.javi(uspjeh)
		druga.javi(uspjeh)
	})
}

func (d *dogovorQUIC) odustani(v *vozac) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.ceka, v)
}

func (c cekaQUIC) javi(uspjeh bool) {
	select {
	case c.v.quic <- ishodQUIC{radnja: c.radnja, uspjeh: uspjeh}:
	default: // strana je gotova i više ne čita
	}
}

// probaPuta: probni paketi obiju strana, svaki na adresu radnje QUIC svoje
// strane, stignu do druge, s adrese koju ona pušta kroz vrata. Probni paket
// je oblika udarca, sa sesijom samo tog susreta, pa ga utičnica preda
// probi zajedno s adresom pošiljatelja.
func probaPuta(a, b cekaQUIC, n int) bool {
	sesija := [16]byte{0xee, byte(n >> 8), byte(n)}
	pa, pb := make(chan primljeniUdarac, probnihPaketa), make(chan primljeniUdarac, probnihPaketa)
	_ = a.v.s.u.prijaviSesiju(sesija, pa) // sesija je nova za svaki susret, pa greške nema
	_ = b.v.s.u.prijaviSesiju(sesija, pb) // isto
	defer a.v.s.u.odjaviSesiju(sesija)
	defer b.v.s.u.odjaviSesiju(sesija)
	paket := make([]byte, velicinaUdarca)
	paket[0] = vrstaUdarac
	copy(paket[4:20], sesija[:])
	for range probnihPaketa {
		_ = a.v.s.u.posaljiUdarac(paket, a.adresa) // paket koji ne ode je put koji ne radi
		_ = b.v.s.u.posaljiUdarac(paket, b.adresa) // isto
		time.Sleep(rokProbe / probnihPaketa)
	}
	return stigla(pb, b.v.x) && stigla(pa, a.v.x)
}

// stigla: među primljenim probnim paketima je jedan s adrese koju strana
// pušta kroz vrata
func stigla(c <-chan primljeniUdarac, x *Udarci) bool {
	for {
		select {
		case p := <-c:
			if x.Pusta(net.UDPAddrFromAddrPort(p.od)) {
				return true
			}
		default:
			return false
		}
	}
}

// ishodProbe je stanje obiju strana na kraju probe
type ishodProbe struct {
	a, b             Probijanje
	quicA, quicB     netip.AddrPort
	stanjeA, stanjeB StanjeUdaraca
	prijeSinkraB     int
}

// probaj vodi probijanje od poziva strane a do kraja obiju strana
func probaj(t *testing.T, a, b *stranaProbe, u uvjetiProbe) ishodProbe {
	t.Helper()
	tajna := bytes.Repeat([]byte{7}, 32)
	ka, err := NoviKljuceviUdarca("testproto", sesijaUdaraca, tajna, a.kljuc, b.kljuc)
	bezGreske(t, err)
	kb, err := NoviKljuceviUdarca("testproto", sesijaUdaraca, tajna, b.kljuc, a.kljuc)
	bezGreske(t, err)
	var posta sync.WaitGroup
	d := &dogovorQUIC{uvjeti: u, ceka: map[*vozac]cekaQUIC{}}
	va := noviVozac("A", a, noviUdarci(t, a.u, ka, b.kandidati, a.vlastite...), ili(u.doA, kasnjenjeSignala), d, &posta)
	vb := noviVozac("B", b, noviUdarci(t, b.u, kb, a.kandidati, b.vlastite...), ili(u.doB, kasnjenjeSignala), d, &posta)
	va.druga, vb.druga = vb, va
	poziv := TijeloPoruke{Sesija: sesijaUdaraca, Od: "A", Za: "B", Kandidati: a.kandidati, NAT: a.nat}
	va.p = NovoProbijanjePozivatelja(poziv, ka.A(), time.Now())
	var strane sync.WaitGroup
	strane.Go(va.vozi)
	strane.Go(func() {
		time.Sleep(vb.kasnjenje) // poziv putuje do B
		vb.p = NovoProbijanjePozvanog(poziv, kb.A(), b.nat, time.Now())
		va.dostavi(DogadjajOdziv, TijeloPoruke{Sesija: sesijaUdaraca, Od: "B", Za: "A", Odluka: OdlukaProbaj, Kandidati: b.kandidati, NAT: b.nat}, 0)
		vb.vozi()
	})
	strane.Wait()
	posta.Wait()
	d.probe.Wait()
	return ishodProbe{a: va.p, b: vb.p, quicA: va.adresa, quicB: vb.adresa, stanjeA: va.x.Stanje(), stanjeB: vb.x.Stanje(), prijeSinkraB: vb.prijeSinkra}
}

// ili je v, a zadano kad je v nula
func ili(v, zadano time.Duration) time.Duration {
	if v == 0 {
		return zadano
	}
	return v
}

// provjeriIshod: obje strane su uspjele, ili obje nisu, nakon zadanog
// broja pokušaja; vlastiti udarac nije stigao nijednoj
func provjeriIshod(t *testing.T, i ishodProbe, uspjeh bool, pokusaja int) {
	t.Helper()
	want := map[bool]Faza{true: FazaUspjeh, false: FazaNeuspjeh}[uspjeh]
	if i.a.Faza != want || i.b.Faza != want || i.a.Pokusaj != pokusaja || i.b.Pokusaj != pokusaja {
		t.Errorf("A: faza %d u %d. pokušaju (%s), B: faza %d u %d. pokušaju (%s); želim fazu %d u %d.; valjanih A %d, B %d",
			i.a.Faza, i.a.Pokusaj, i.a.Razlog, i.b.Faza, i.b.Pokusaj, i.b.Razlog, want, pokusaja, i.stanjeA.Valjanih, i.stanjeB.Valjanih)
	}
	if i.stanjeA.Odbijeno[OdbijenVlastiti] != 0 || i.stanjeB.Odbijeno[OdbijenVlastiti] != 0 {
		t.Errorf("vlastiti udarac: A %+v, B %+v", i.stanjeA, i.stanjeB)
	}
}

// Tablica NAT parova iz plana (F3a): par uspije kad se udarci međusobno
// vide, i kad adresu druge strane vidi tek ova strana (simetrični NAT);
// simetrični NAT s obje strane završi nakon 1 pokušaja, ostali neuspjesi
// nakon 3, a blokiran UDP bez ijednog.
func TestProbijanjeKrozNAT(t *testing.T) {
	for _, c := range []struct {
		ime      string
		a, b     natsim.Postavke
		uspjeh   bool
		pokusaja int
	}{
		{"EIM i EIM", eimNeovisno, eimNeovisno, true, 1},
		{"EIM i EIM po portu", eimNeovisno, eimPoPortu, true, 1},
		{"EIM i simetrični", eimNeovisno, simetricni, true, 1},
		{"EIM i blokiran", eimNeovisno, blokiran, false, 0},
		{"EIM po portu i EIM", eimPoPortu, eimNeovisno, true, 1},
		{"EIM po portu i EIM po portu", eimPoPortu, eimPoPortu, true, 1},
		{"EIM po portu i simetrični", eimPoPortu, simetricni, false, 3},
		{"EIM po portu i blokiran", eimPoPortu, blokiran, false, 0},
		{"simetrični i EIM", simetricni, eimNeovisno, true, 1},
		{"simetrični i EIM po portu", simetricni, eimPoPortu, false, 3},
		{"simetrični i simetrični", simetricni, simetricni, false, 1},
		{"simetrični i blokiran", simetricni, blokiran, false, 0},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				m := internetUdaraca(t)
				m.DvaSTUN()
				a, b, _, _ := parIza(t, m, c.a, c.b)
				i := probaj(t, a, b, uvjetiProbe{})
				provjeriIshod(t, i, c.uspjeh, c.pokusaja)
				if c.uspjeh && (!i.quicA.Addr().IsGlobalUnicast() || i.quicA.Addr().IsPrivate() || i.quicB.Addr().IsPrivate()) {
					t.Errorf("QUIC na %s i %s, želim javne adrese", i.quicA, i.quicB)
				}
			})
		})
	}
}

// Prvi QUIC ne uspije (izgubljeno rukovanje): adresa koja se javila u 1.
// pokušaju javi se ponovno u 2., pa probijanje uspije u 2. pokušaju. Kad
// ne uspiju ni prva dva, uspije u 3.
func TestProbijanjeNakonNeuspjelogQUICa(t *testing.T) {
	for padni := 1; padni <= 2; padni++ {
		uMjehuru(t, func(t *testing.T) {
			m := internetUdaraca(t)
			m.DvaSTUN()
			a, b, _, _ := parIza(t, m, eimNeovisno, eimPoPortu)
			provjeriIshod(t, probaj(t, a, b, uvjetiProbe{padni: padni}), true, padni+1)
		})
	}
}

// Sinkro do pozvanoga putuje dulje od poziva i odziva (kolebanje
// sastajališta preko Cloudflarea, slanje s LTE-a): prvi udarac stigne
// pozvanome dok još čeka sinkro, a probijanje ipak uspije u 1. pokušaju,
// jer se ta adresa javi ponovno nakon sinkra.
func TestProbijanjeSinkroSporijiOdUdarca(t *testing.T) {
	for _, c := range []struct {
		ime  string
		a, b natsim.Postavke
		u    uvjetiProbe
	}{
		{"EIM i EIM, sinkro 15 ms sporiji", eimNeovisno, eimNeovisno, uvjetiProbe{sinkroVise: 15 * time.Millisecond}},
		{"EIM po portu i EIM, sinkro 15 ms sporiji", eimPoPortu, eimNeovisno, uvjetiProbe{sinkroVise: 15 * time.Millisecond}},
		{"EIM po portu i EIM, sinkro 30 ms sporiji", eimPoPortu, eimNeovisno, uvjetiProbe{sinkroVise: 30 * time.Millisecond}},
		{"EIM i EIM, do B 80 ms, do A 20 ms", eimNeovisno, eimNeovisno, uvjetiProbe{doB: 80 * time.Millisecond}},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				m := internetUdaraca(t)
				m.DvaSTUN()
				a, b, _, _ := parIza(t, m, c.a, c.b)
				i := probaj(t, a, b, c.u)
				provjeriIshod(t, i, true, 1)
				if i.prijeSinkraB == 0 {
					t.Error("udarac nije stigao pozvanome prije sinkra")
				}
			})
		})
	}
}

// 20 % gubitka paketa izvana kod obiju strana (izmjereno prije gubitka):
// probijanje uspije unutar 3 pokušaja, kod obiju strana u istom.
func TestProbijanjeUzGubitak(t *testing.T) {
	izgubljeno := 0
	for _, c := range []struct {
		ime  string
		a, b natsim.Postavke
	}{
		{"EIM i EIM", eimNeovisno, eimNeovisno},
		{"EIM i EIM po portu", eimNeovisno, eimPoPortu},
		{"EIM po portu i EIM po portu", eimPoPortu, eimPoPortu},
		{"simetrični i EIM", simetricni, eimNeovisno},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				m := internetUdaraca(t)
				m.DvaSTUN()
				a, b, na, nb := parIza(t, m, c.a, c.b)
				na.Gubitak(0.2)
				nb.Gubitak(0.2)
				i := probaj(t, a, b, uvjetiProbe{})
				if i.a.Faza != FazaUspjeh || i.b.Faza != FazaUspjeh || i.a.Pokusaj != i.b.Pokusaj || i.a.Pokusaj > 3 {
					t.Errorf("A: faza %d u %d. (%s), B: faza %d u %d. (%s)", i.a.Faza, i.a.Pokusaj, i.a.Razlog, i.b.Faza, i.b.Pokusaj, i.b.Razlog)
				}
				izgubljeno += na.Statistika().Izgubljeno + nb.Statistika().Izgubljeno
			})
		})
	}
	if izgubljeno == 0 { // u jednom paru se može dogoditi, u svima ne
		t.Error("nijedan paket nije izgubljen")
	}
}

// Ostali slučajevi iz plana koji ne trebaju pravi QUIC: dvostruki NAT s
// putem koji udarcima radi samo u jednom smjeru, ured s dvije javne
// adrese, Docker most s conntrackom, vatrozid na računalu i ista privatna
// adresa u dvije kuće.
func TestProbijanjeOstaliNAT(t *testing.T) {
	dvijeJavne := natsim.Postavke{Preslikavanje: natsim.PreslikavanjePoAdresi, JavneNaizmjence: true, Kasnjenje: 10 * time.Millisecond}
	docker := func(t *testing.T, m *natsim.Mreza, laptopNAT natsim.Postavke) (*stranaProbe, *stranaProbe, func(*testing.T, ishodProbe)) {
		most := m.NAT(dockerMost, netip.MustParseAddr("203.0.113.9"))
		most.Objavi(4713, netip.MustParseAddrPort("172.17.0.2:4713"))
		spremnik := stranaIza(t, most, "172.17.0.2:4713", natsim.PostavkeRacunala{})
		laptop := stranaIza(t, m.NAT(laptopNAT, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", natsim.PostavkeRacunala{})
		return laptop, spremnik, func(t *testing.T, _ ishodProbe) {
			if most.Statistika().Sudari == 0 {
				t.Error("conntrack nije napravio krivo preslikavanje")
			}
		}
	}
	for _, c := range []struct {
		ime      string
		priprema func(t *testing.T, m *natsim.Mreza) (a, b *stranaProbe, provjera func(*testing.T, ishodProbe))
		uspjeh   bool
		pokusaja int
	}{
		// laptop iza kućnog usmjernika, a ThinkCentre ravno na usmjerniku
		// operatera, bez hairpina: udarci prolaze samo od laptopa prema
		// ThinkCentreu, a natrag ide odgovor na adresu koju vidi tek
		// ThinkCentre (kućni usmjernik na mreži operatera)
		{"dvostruki NAT kod kuće, put u jednom smjeru", func(t *testing.T, m *natsim.Mreza) (*stranaProbe, *stranaProbe, func(*testing.T, ishodProbe)) {
			operater := m.NAT(eimPoPortu, netip.MustParseAddr("203.0.113.1"))
			kucni := operater.NAT(eimNeovisno, netip.MustParseAddr("192.168.0.2"))
			return stranaIza(t, kucni, "192.168.1.10:4713", natsim.PostavkeRacunala{}),
				stranaIza(t, operater, "192.168.0.50:4713", natsim.PostavkeRacunala{}),
				func(t *testing.T, i ishodProbe) {
					if i.quicA != netip.MustParseAddrPort("192.168.0.50:4713") || i.quicB != netip.MustParseAddrPort("192.168.0.2:4713") {
						t.Errorf("QUIC na %s i %s", i.quicA, i.quicB)
					}
				}
		}, true, 1},
		{"ured s dvije javne adrese i EIM", func(t *testing.T, m *natsim.Mreza) (*stranaProbe, *stranaProbe, func(*testing.T, ishodProbe)) {
			ured := stranaIza(t, m.NAT(dvijeJavne, netip.MustParseAddr("203.0.113.1"), netip.MustParseAddr("203.0.113.3")), "10.10.0.5:4713", natsim.PostavkeRacunala{})
			if ured.nat.Vrsta != NATOvisan {
				t.Fatalf("ured: %+v", ured.nat)
			}
			return ured, stranaIza(t, m.NAT(eimNeovisno, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", natsim.PostavkeRacunala{}), nil
		}, true, 1},
		// spremnik šalje s porta koji je conntrack dao u utrci, a laptopov
		// NAT pušta samo port kojem je laptop slao: krivo preslikavanje se
		// drži dok ga udarci osvježavaju, pa ni odgoda u 2. i 3. pokušaju ne
		// pomaže (SERVER sync)
		{"Docker most s conntrackom i EIM po portu", func(t *testing.T, m *natsim.Mreza) (*stranaProbe, *stranaProbe, func(*testing.T, ishodProbe)) {
			return docker(t, m, eimPoPortu)
		}, false, 3},
		// isto, a laptopov NAT pušta svakoga: put radi i s krivim portom
		{"Docker most s conntrackom i EIM", func(t *testing.T, m *natsim.Mreza) (*stranaProbe, *stranaProbe, func(*testing.T, ishodProbe)) {
			return docker(t, m, eimNeovisno)
		}, true, 1},
		{"vatrozid na računalu i simetrični", func(t *testing.T, m *natsim.Mreza) (*stranaProbe, *stranaProbe, func(*testing.T, ishodProbe)) {
			return stranaIza(t, m.NAT(eimNeovisno, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713", natsim.PostavkeRacunala{Vatrozid: true}),
				stranaIza(t, m.NAT(simetricni, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", natsim.PostavkeRacunala{}), nil
		}, false, 3},
		{"ista privatna adresa u dvije kuće", func(t *testing.T, m *natsim.Mreza) (*stranaProbe, *stranaProbe, func(*testing.T, ishodProbe)) {
			return stranaIza(t, m.NAT(eimNeovisno, netip.MustParseAddr("203.0.113.1")), "192.168.1.23:4713", natsim.PostavkeRacunala{}),
				stranaIza(t, m.NAT(eimNeovisno, netip.MustParseAddr("203.0.113.2")), "192.168.1.23:4713", natsim.PostavkeRacunala{}),
				func(t *testing.T, i ishodProbe) {
					if i.quicA.Addr().IsPrivate() || i.quicB.Addr().IsPrivate() {
						t.Errorf("QUIC na %s i %s", i.quicA, i.quicB)
					}
				}
		}, true, 1},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				m := internetUdaraca(t)
				m.DvaSTUN()
				a, b, provjera := c.priprema(t, m)
				i := probaj(t, a, b, uvjetiProbe{})
				provjeriIshod(t, i, c.uspjeh, c.pokusaja)
				if provjera != nil {
					provjera(t, i)
				}
			})
		})
	}
}

// Dva računala iza istog NAT-a: bez hairpina radi samo lokalni kandidat, a
// s hairpinom javni stigne prvi, ali lokalni pobijedi jer stigne unutar
// 500 ms.
func TestProbijanjeIzaIstogNAT(t *testing.T) {
	for _, hairpin := range []bool{false, true} {
		uMjehuru(t, func(t *testing.T) {
			m := internetUdaraca(t)
			m.DvaSTUN()
			p := eimNeovisno
			p.Hairpin = hairpin
			n := m.NAT(p, netip.MustParseAddr("203.0.113.1"))
			a := stranaIza(t, n, "192.168.1.10:4713", natsim.PostavkeRacunala{})
			b := stranaIza(t, n, "192.168.1.11:4713", natsim.PostavkeRacunala{})
			i := probaj(t, a, b, uvjetiProbe{})
			provjeriIshod(t, i, true, 1)
			if i.quicA != netip.MustParseAddrPort("192.168.1.11:4713") || i.quicB != netip.MustParseAddrPort("192.168.1.10:4713") {
				t.Errorf("hairpin %v: QUIC na %s i %s", hairpin, i.quicA, i.quicB)
			}
			if hairpin && (i.stanjeA.Valjanih == 0 || len(i.a.Razlog) != 0) {
				t.Errorf("stanje %+v", i.stanjeA)
			}
		})
	}
}
