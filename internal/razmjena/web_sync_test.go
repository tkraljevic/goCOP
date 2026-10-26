package razmjena

// WEB sync od poziva do razgovora (web_sync.go, sesija.go), u mjehuru, kroz
// natsim, s pravim QUIC-om: dvije strane iza zadanih NAT-ova izmjere mrežu
// lažnim STUN-om, poziv, odziv i sinkro idu preko lažnog sastajališta
// (20 ms u svakom smjeru), a nakon probijanja razgovor ide kanalom 0x01
// iste QUIC veze.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/natsim"
)

// postaWeb je lažno sastajalište: potpisanu poruku preda računalu kojem
// ide (Za) nakon kašnjenja. Zadrži odlučuje koja poruka ne stiže.
type postaWeb struct {
	kljuc     ed25519.PrivateKey
	kasnjenje time.Duration

	mu      sync.Mutex
	cvorovi map[string]*WebSync
	zadrzi  func(PotpisanaPoruka) bool
	poslano []PotpisanaPoruka
	poruke  sync.WaitGroup
}

func novaPosta(t *testing.T) *postaWeb {
	t.Helper()
	p := &postaWeb{kljuc: newKey(t), kasnjenje: kasnjenjeSignala, cvorovi: map[string]*WebSync{}, zadrzi: func(PotpisanaPoruka) bool { return false }}
	t.Cleanup(p.poruke.Wait)
	return p
}

func (p *postaWeb) javni() ed25519.PublicKey { return p.kljuc.Public().(ed25519.PublicKey) }

func (p *postaWeb) posalji(pp PotpisanaPoruka) error {
	t, err := pp.Zaglavlje()
	if err != nil {
		return err
	}
	p.mu.Lock()
	w, zadrzi := p.cvorovi[t.Za], p.zadrzi
	p.poslano = append(p.poslano, pp)
	p.mu.Unlock()
	if w == nil || zadrzi(pp) {
		return nil
	}
	p.poruke.Go(func() {
		time.Sleep(p.kasnjenje)
		w.NaPoruku(pp)
	})
	return nil
}

// vrste su vrste poslanih poruka, redom
func (p *postaWeb) vrste() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, pp := range p.poslano {
		out = append(out, pp.Vrsta)
	}
	return out
}

func (p *postaWeb) zadrziAko(f func(PotpisanaPoruka) bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.zadrzi = f
}

// cvorWeb je računalo s WEB syncom na utičnici iz probe: vjeruje članovima
// iz svog popisa, a razgovor na kanalu 0x01 vraća kao jeku
type cvorWeb struct {
	*stranaProbe
	w      *WebSync
	posta  *postaWeb
	sesije chan *SesijaWeb
	obrada atomic.Int32 // razgovora koji su došli do obrade
	stani  context.CancelFunc
	kraj   chan error

	mu      sync.Mutex
	clanovi map[string]bool
	drzi    chan struct{} // dok nije zatvoren, obrada razgovora čeka
}

// noviCvorWeb pokrene WEB sync računala s; uz mijenja postavke prije
// pokretanja
func noviCvorWeb(t *testing.T, s *stranaProbe, posta *postaWeb, uz ...func(*PostavkeWeb)) *cvorWeb {
	t.Helper()
	c := &cvorWeb{stranaProbe: s, posta: posta, sesije: make(chan *SesijaWeb, 16), clanovi: map[string]bool{}, kraj: make(chan error, 1)}
	p := PostavkeWeb{Priv: s.priv, Protokol: "testproto", Trusted: c.vjeruje, Handle: c.jeka, Posalji: posta.posalji,
		Sastajaliste: posta.javni(), Mreza: func() ([]KandidatPoruke, *NATPoruke) { return s.kandidati, s.nat },
		NaSesiju: func(x *SesijaWeb) { c.sesije <- x }}
	for _, f := range uz {
		f(&p)
	}
	w, err := NoviWebSync(s.u, p)
	bezGreske(t, err)
	if p.Izlaz == nil {
		w.izlaz = func(netip.AddrPort) (netip.Addr, error) { return netip.MustParseAddr("192.0.2.1"), nil }
	}
	if p.TailscaleAdrese == nil {
		w.ts = func() []netip.Addr { return nil }
	}
	c.w = w
	posta.mu.Lock()
	posta.cvorovi[PublicKeyString(s.kljuc)] = w
	posta.mu.Unlock()
	c.pokreni(t)
	return c
}

// pokreni pokrene WEB sync (Pokreni, pa petlja u gorutini); na kraju testa
// ga zaustavi i pričeka
func (c *cvorWeb) pokreni(t *testing.T) {
	ctx, stani := context.WithCancel(context.Background())
	c.stani = stani
	petlja, err := c.w.Pokreni(ctx)
	bezGreske(t, err)
	go func() {
		petlja()
		c.kraj <- nil
	}()
	t.Cleanup(c.zaustavi)
}

// zaustavi zaustavi Radi i pričeka ga; drugi put ne radi ništa
func (c *cvorWeb) zaustavi() {
	if c.stani == nil {
		return
	}
	c.stani()
	c.stani = nil
	<-c.kraj
}

func (c *cvorWeb) vjeruje(k ed25519.PublicKey, _ []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clanovi[PublicKeyString(k)]
}

func (c *cvorWeb) clan(k ed25519.PublicKey, je bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clanovi[PublicKeyString(k)] = je
}

// jeka je obrada razgovora: na svaku omotnicu vrati koju je primio, dok
// druga strana ne zatvori razgovor; dok test drži obradu (drzi), čeka
func (c *cvorWeb) jeka(conn *Conn) {
	defer conn.Close()
	c.obrada.Add(1)
	c.mu.Lock()
	drzi := c.drzi
	c.mu.Unlock()
	if drzi != nil {
		<-drzi
	}
	for {
		e, err := conn.Receive()
		if err != nil {
			return
		}
		odg, _ := NewEnvelope("odgovor", map[string]string{"stiglo": e.Kind, "put": string(conn.Put)}) // mapa teksta se uvijek da zapisati
		if conn.Send(odg) != nil {
			return
		}
	}
}

// drziObradu: obrada razgovora čeka dok test ne pozove vraćenu funkciju
// (najkasnije na kraju testa)
func (c *cvorWeb) drziObradu(t *testing.T) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	drzi := make(chan struct{})
	c.drzi = drzi
	pusti := sync.OnceFunc(func() { close(drzi) })
	t.Cleanup(pusti)
	return pusti
}

// sesijaIz čeka sesiju koju je javio NaSesiju
func (c *cvorWeb) sesijaIz(t *testing.T) *SesijaWeb {
	t.Helper()
	select {
	case s := <-c.sesije:
		return s
	case <-time.After(time.Minute):
		t.Fatal("sesija nije javljena")
		return nil
	}
}

// parWeb su dva računala koja su međusobno članovi; prvo zove, a ima
// manji ključ kad aJeA
func parWeb(t *testing.T, sa, sb *stranaProbe, aJeA bool, uz ...func(*PostavkeWeb)) (a, b *cvorWeb, posta *postaWeb) {
	t.Helper()
	if (bytes.Compare(sa.kljuc, sb.kljuc) < 0) != aJeA {
		sa.priv, sb.priv = sb.priv, sa.priv
		sa.kljuc, sb.kljuc = sb.kljuc, sa.kljuc
	}
	posta = novaPosta(t)
	a, b = noviCvorWeb(t, sa, posta, uz...), noviCvorWeb(t, sb, posta, uz...)
	a.clan(b.kljuc, true)
	b.clan(a.kljuc, true)
	return a, b, posta
}

// razgovorNa otvori razgovor na sesiji i vrati odgovor druge strane
func razgovorNa(ctx context.Context, s *SesijaWeb) (Envelope, error) {
	c, err := s.Razgovor(ctx)
	if err != nil {
		return Envelope{}, err
	}
	defer c.Close()
	return razgovaraj(c)
}

// provjeriRazgovor: razgovor na kanalu 0x01 prolazi s obje strane, putem
// probijeno
func provjeriRazgovor(t *testing.T, ctx context.Context, sa, sb *SesijaWeb) {
	t.Helper()
	for _, s := range []*SesijaWeb{sa, sb} {
		e, err := razgovorNa(ctx, s)
		var tijelo map[string]string
		if err != nil || e.Kind != "odgovor" || e.Decode(&tijelo) != nil || tijelo["stiglo"] != "frontier" || tijelo["put"] != string(PutProbijeno) {
			t.Errorf("razgovor: %+v %v", e, err)
		}
	}
}

var parovi = []struct {
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
}

// Tablica NAT parova iz plana (F3a) s pravim QUIC-om, i kad zove računalo
// s manjim ključem i kad zove ono s većim: par koji uspije razgovara
// kanalom 0x01 u oba smjera, a par koji ne uspije završi u istom broju
// pokušaja kao u probi bez QUIC-a (probijanje_natsim_test.go).
func TestWebSyncKrozNAT(t *testing.T) {
	for _, c := range parovi {
		for _, aJeA := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, zove A %v", c.ime, aJeA), func(t *testing.T) {
				uMjehuru(t, func(t *testing.T) {
					m := internetUdaraca(t)
					m.DvaSTUN()
					sa, sb, _, _ := parIza(t, m, c.a, c.b)
					a, b, _ := parWeb(t, sa, sb, aJeA)
					s, err := a.w.Pripremi(t.Context(), b.kljuc)
					provjeriPripremu(t, s, err, c.uspjeh, c.pokusaja)
					if c.uspjeh {
						provjeriRazgovor(t, t.Context(), s, b.sesijaIz(t))
					}
				})
			})
		}
	}
}

// provjeriPripremu: priprema je uspjela ili nije, u zadanom pokušaju
func provjeriPripremu(t *testing.T, s *SesijaWeb, err error, uspjeh bool, pokusaja int) {
	t.Helper()
	var g GreskaPripreme
	switch {
	case uspjeh && (err != nil || s.Pokusaj != pokusaja):
		t.Fatalf("priprema: %v, sesija %+v; želim uspjeh u %d. pokušaju", err, s, pokusaja)
	case !uspjeh && (!errors.As(err, &g) || g.Pokusaj != pokusaja):
		t.Fatalf("priprema: %v (%+v), želim neuspjeh nakon %d pokušaja", err, g, pokusaja)
	}
}

// stranaJavna je računalo s javnom adresom, bez NAT-a, s ključem priv i
// utičnicom s ključem za resetiranje QUIC veza
func stranaJavna(t *testing.T, m *natsim.Mreza, adresa string, priv ed25519.PrivateKey) *stranaProbe {
	t.Helper()
	a := netip.MustParseAddrPort(adresa)
	u := NoviUDPCvora(m.Racunalo(a, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond}), priv)
	t.Cleanup(func() { _ = u.Close() }) // greška zatvaranja na kraju testa nema što reći
	return &stranaProbe{priv: priv, kljuc: priv.Public().(ed25519.PublicKey), u: u, nat: &NATPoruke{Vrsta: NATLak},
		kandidati: []KandidatPoruke{{Vrsta: KandidatJavni, Adresa: adresa}}, vlastite: []netip.AddrPort{a}}
}

// javniPar su dva javna računala koja su međusobno članovi; A ima manji
// ključ. Ključevi se poredaju prije utičnica, jer ključ za resetiranje QUIC
// veza utičnica dobije iz ključa čvora.
func javniPar(t *testing.T, m *natsim.Mreza, uz ...func(*PostavkeWeb)) (a, b *cvorWeb, posta *postaWeb) {
	t.Helper()
	ka, kb := poredaniKljucevi(t)
	return parWeb(t, stranaJavna(t, m, "198.51.100.1:4713", ka), stranaJavna(t, m, "198.51.100.2:4713", kb), true, uz...)
}

// poredaniKljucevi su dva ključa, prvi manji
func poredaniKljucevi(t *testing.T) (ed25519.PrivateKey, ed25519.PrivateKey) {
	ka, kb := newKey(t), newKey(t)
	if bytes.Compare(ka.Public().(ed25519.PublicKey), kb.Public().(ed25519.PublicKey)) > 0 {
		return kb, ka
	}
	return ka, kb
}

// sesijaPara pripremi WEB sync od a do b i vrati sesije obiju strana
func sesijaPara(t *testing.T, a, b *cvorWeb) (sa, sb *SesijaWeb) {
	t.Helper()
	sa, err := a.w.Pripremi(t.Context(), b.kljuc)
	bezGreske(t, err)
	return sa, b.sesijaIz(t)
}

// zatvorena čeka da se sesija zatvori najviše rok
func zatvorena(s *SesijaWeb, rok time.Duration) bool {
	t := time.NewTimer(rok)
	defer t.Stop()
	select {
	case <-s.Gotova():
		return true
	case <-t.C:
	}
	select {
	case <-s.Gotova():
		return true
	default:
		return false
	}
}

// 20 % gubitka paketa izvana kod obiju strana: priprema s pravim QUIC-om
// uspije unutar 3 pokušaja, kod obiju strana u istom, a razgovor prođe.
func TestWebSyncUzGubitak(t *testing.T) {
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
				sa, sb, na, nb := parIza(t, m, c.a, c.b)
				na.Gubitak(0.2)
				nb.Gubitak(0.2)
				a, b, _ := parWeb(t, sa, sb, true)
				s, err := a.w.Pripremi(t.Context(), b.kljuc)
				if err != nil || s.Pokusaj > 3 {
					t.Fatalf("priprema: %v, %+v", err, s)
				}
				if sb := b.sesijaIz(t); sb.Pokusaj != s.Pokusaj || sb.Sesija != s.Sesija {
					t.Errorf("A u %d. pokušaju, B u %d.", s.Pokusaj, sb.Pokusaj)
				}
				if _, err := razgovorNa(t.Context(), s); err != nil {
					t.Errorf("razgovor uz gubitak: %v", err)
				}
				izgubljeno += na.Statistika().Izgubljeno + nb.Statistika().Izgubljeno
			})
		})
	}
	if izgubljeno == 0 {
		t.Error("nijedan paket nije izgubljen")
	}
}

// Sesija preživi dva kruga razmjene od 5 min, iza NAT-ova kojima
// preslikavanje istječe nakon 30 s, jer održavanje veze ide svakih 15 s.
// Bez razgovora istekne 6 min nakon zadnjeg, a dok razgovor traje (i
// 10 min), ne istječe.
func TestWebSyncSesijaPreziviDvaKruga(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		m.DvaSTUN()
		sa, sb, _, _ := parIza(t, m, eimPoPortu, eimPoPortu)
		a, b, _ := parWeb(t, sa, sb, true)
		s, sB := sesijaPara(t, a, b)
		for krug := range 3 {
			if krug > 0 {
				time.Sleep(5 * time.Minute)
			}
			if _, err := razgovorNa(t.Context(), s); err != nil {
				t.Fatalf("%d. krug: %v", krug+1, err)
			}
			if a.w.Sesija(b.kljuc) != s || b.w.Sesija(a.kljuc) != sB {
				t.Fatalf("%d. krug: druga sesija", krug+1)
			}
		}
		dugi, err := s.Razgovor(t.Context())
		bezGreske(t, err)
		for range 10 {
			if _, err := razgovaraj(dugi); err != nil {
				t.Fatalf("dugi razgovor: %v", err)
			}
			time.Sleep(time.Minute)
		}
		if zatvorena(s, 0) {
			t.Fatal("sesija je istekla usred razgovora od 10 min")
		}
		dugi.Close()
		if zatvorena(s, zivotSesije-time.Second) {
			t.Fatal("sesija je istekla prije 6 min")
		}
		if !zatvorena(s, 2*time.Second) || !zatvorena(sB, time.Second) || s.Kod() != 0 || sB.Kod() != 0 {
			t.Fatalf("sesija nije istekla 6 min nakon razgovora (kod %#x, %#x)", s.Kod(), sB.Kod())
		}
		synctest.Wait()
		if a.w.Sesija(b.kljuc) != nil || b.w.Sesija(a.kljuc) != nil {
			t.Error("istekla sesija je ostala upisana")
		}
	})
}

// Promjena mreže usred sesije: laptop prijeđe iza drugog NAT-a s novom
// utičnicom, a stara bez ijedne poruke nestane. Druga strana sesiju
// zatvori 30 do 45 s nakon zadnjeg prometa (rok mirovanja 30 s, dvaput
// razmak održavanja od 15 s; Odluke (25. 10. 2026.), 2), a nova priprema s
// nove mreže uspije i zamijeni je.
func TestWebSyncPromjenaMreze(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		m.DvaSTUN()
		sa, sb, _, _ := parIza(t, m, eimNeovisno, eimNeovisno)
		a, b, posta := parWeb(t, sa, sb, true)
		_, sB := sesijaPara(t, a, b)
		_ = a.u.Close() // mreža je otišla s utičnicom; greška zatvaranja nema što reći
		a.zaustavi()
		// rok mirovanja od 30 s quic-go mjeri od zadnjeg primljenog paketa,
		// ali ga i prvo vlastito održavanje veze nakon njega (najviše 15 s
		// kasnije) pokrene iznova (RFC 9000, 10.1): sesija se zatvori 30 do
		// 45 s nakon zadnjeg paketa druge strane, a ne nakon minute ili
		// 75 s, kao uz rok od 60 s
		if zatvorena(sB, 29*time.Second) || !zatvorena(sB, 17*time.Second) {
			t.Fatal("sesija druge strane nije se zatvorila 30 do 45 s nakon zadnjeg prometa")
		}
		nova := stranaIza(t, m.NAT(eimNeovisno, netip.MustParseAddr("203.0.113.7")), "10.20.0.5:4713", natsim.PostavkeRacunala{})
		nova.priv, nova.kljuc = sa.priv, sa.kljuc
		a2 := noviCvorWeb(t, nova, posta)
		a2.clan(b.kljuc, true)
		s2, err := a2.w.Pripremi(t.Context(), b.kljuc)
		bezGreske(t, err)
		if _, err := razgovorNa(t.Context(), s2); err != nil {
			t.Fatalf("razgovor s nove mreže: %v", err)
		}
		if sB2 := b.sesijaIz(t); b.w.Sesija(a.kljuc) != sB2 || sB2 == sB {
			t.Error("druga strana nema novu sesiju")
		}
	})
}

// Spavanje i buđenje: dok računala spavaju, ne stiže ništa i NAT
// zaboravi preslikavanja, pa obje sesije istekne mirovanje. Nakon buđenja
// nova priprema uspije.
func TestWebSyncSpavanjeIBudenje(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		m.DvaSTUN()
		sa, sb, na, nb := parIza(t, m, eimPoPortu, eimNeovisno)
		a, b, _ := parWeb(t, sa, sb, true)
		s, sB := sesijaPara(t, a, b)
		na.Gubitak(1)
		nb.Gubitak(1)
		time.Sleep(10 * time.Minute)
		if !zatvorena(s, 0) || !zatvorena(sB, 0) || s.Kod() != 0 || sB.Kod() != 0 {
			t.Fatalf("sesije su preživjele spavanje ili su zatvorene kodom (%#x, %#x)", s.Kod(), sB.Kod())
		}
		na.Gubitak(0)
		nb.Gubitak(0)
		s2, err := a.w.Pripremi(t.Context(), b.kljuc)
		bezGreske(t, err)
		if _, err := razgovorNa(t.Context(), s2); err != nil {
			t.Fatalf("razgovor nakon buđenja: %v", err)
		}
	})
}

// Ponovno pokretanje bez urednog zatvaranja: ključ za resetiranje QUIC
// veza izveden je iz ključa čvora, pa novi proces računala B (slušalice)
// na istoj adresi resetira staru vezu čim mu druga strana nešto pošalje
// (razgovor), umjesto da ona čeka 30 s mirovanja. S drugim ključem
// čvora reseta nema. Ograničenja quic-go-a: reset ide samo na paket veći
// od 42 B, pa ga održavanje veze ne izazove, a kad padne A, koji je zvao,
// druga strana čeka 30 s, jer znak za reset prvog identifikatora veze
// daje samo slušalica (obnova uz Docker most se ne radi; Odluke
// (25. 10. 2026.), 2).
func TestWebSyncPonovnoPokretanje(t *testing.T) {
	for _, isti := range []bool{true, false} {
		t.Run(fmt.Sprintf("isti ključ %v", isti), func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				m := internetUdaraca(t)
				a, b, _ := javniPar(t, m)
				sA, _ := sesijaPara(t, a, b)
				_ = b.u.Close() // proces je pao; greška zatvaranja nema što reći
				priv := b.priv
				if !isti {
					priv = newKey(t)
				}
				stranaJavna(t, m, "198.51.100.2:4713", priv)
				pocetak := time.Now()
				_, _ = razgovorNa(t.Context(), sA) // razgovor čeka dok veza ne padne; ishod je u tome kad je pala
				if brzo := time.Since(pocetak) < 5*time.Second; brzo != isti || !zatvorena(sA, time.Second) {
					t.Errorf("veza je pala nakon %v (%v)", time.Since(pocetak), context.Cause(sA.veza.Context()))
				}
			})
		})
	}
}

// Ostali slučajevi iz plana uz tablicu (ostaliNAT) s pravim QUIC-om: isti
// ishod i isti broj pokušaja kao sa strojem i lažnim QUIC-om, a QUIC ide
// adresama koje provjera traži.
func TestWebSyncOstaliNAT(t *testing.T) {
	for _, c := range ostaliNAT() {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				m := internetUdaraca(t)
				m.DvaSTUN()
				sa, sb, provjera := c.priprema(t, m)
				a, b, _ := parWeb(t, sa, sb, bytes.Compare(sa.kljuc, sb.kljuc) < 0)
				s, err := a.w.Pripremi(t.Context(), b.kljuc)
				provjeriPripremu(t, s, err, c.uspjeh, c.pokusaja)
				if !c.uspjeh {
					return
				}
				sB := b.sesijaIz(t)
				provjeriRazgovor(t, t.Context(), s, sB)
				if provjera != nil {
					provjera(t, ishodProbe{quicA: adresaVeze(s), quicB: adresaVeze(sB)})
				}
			})
		})
	}
}

// adresaVeze je adresa druge strane QUIC veze sesije
func adresaVeze(s *SesijaWeb) netip.AddrPort {
	a, _ := adresaPosluzitelja(s.veza.RemoteAddr()) // QUIC veza uvijek ima UDP adresu
	return a
}

// Dva računala iza istog NAT-a: bez hairpina radi samo lokalni kandidat,
// a s hairpinom lokalni pobijedi jer stigne unutar 500 ms; sesija ide
// lokalnim adresama.
func TestWebSyncIzaIstogNAT(t *testing.T) {
	for _, hairpin := range []bool{false, true} {
		uMjehuru(t, func(t *testing.T) {
			m := internetUdaraca(t)
			m.DvaSTUN()
			p := eimNeovisno
			p.Hairpin = hairpin
			n := m.NAT(p, netip.MustParseAddr("203.0.113.1"))
			sa := stranaIza(t, n, "192.168.1.10:4713", natsim.PostavkeRacunala{})
			sb := stranaIza(t, n, "192.168.1.11:4713", natsim.PostavkeRacunala{})
			a, b, _ := parWeb(t, sa, sb, true)
			s, err := a.w.Pripremi(t.Context(), b.kljuc)
			provjeriPripremu(t, s, err, true, 1)
			sB := b.sesijaIz(t)
			if adresaVeze(s) != netip.MustParseAddrPort("192.168.1.11:4713") || adresaVeze(sB) != netip.MustParseAddrPort("192.168.1.10:4713") {
				t.Errorf("hairpin %v: QUIC na %s i %s", hairpin, adresaVeze(s), adresaVeze(sB))
			}
			provjeriRazgovor(t, t.Context(), s, sB)
		})
	}
}

// Tek upareno računalo druga strana poznaje samo po vjerodajnicama: A ih
// dobije iz provjerenog odziva, a B iz poziva (slušalica ih u certifikatu
// ne šalje). Razgovor na kanalu 0x01 prolazi u oba smjera, i nakon
// ProvjeriSve, bez obzira ima li novo računalo manji ili veći ključ i tko
// zove.
func TestClanstvoIzVjerodajnica(t *testing.T) {
	for _, c := range []struct {
		ime            string
		noviJeB, zoveA bool
	}{
		{"novi B, zove A", true, true},
		{"novi B, zove B", true, false},
		{"novi A, zove A", false, true},
		{"novi A, zove B", false, false},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				m := internetUdaraca(t)
				ka, kb := poredaniKljucevi(t)
				sa, sb := stranaJavna(t, m, vrataA.String(), ka), stranaJavna(t, m, vrataB.String(), kb)
				stari, novi := sa, sb
				if !c.noviJeB {
					stari, novi = sb, sa
				}
				vj := []byte(`{"clan":"novi"}`)
				posta := novaPosta(t)
				cs := noviCvorWeb(t, stari, posta, func(p *PostavkeWeb) {
					p.Trusted = func(k ed25519.PublicKey, v []byte) bool { return k.Equal(novi.kljuc) && bytes.Equal(v, vj) }
				})
				cn := noviCvorWeb(t, novi, posta, func(p *PostavkeWeb) { p.Vj = func() []byte { return vj } })
				cn.clan(stari.kljuc, true)
				zove, zvani := cs, cn
				if (stari == sa) != c.zoveA {
					zove, zvani = cn, cs
				}
				s1, err := zove.w.Pripremi(t.Context(), zvani.kljuc)
				bezGreske(t, err)
				s2 := zvani.sesijaIz(t)
				provjeriRazgovor(t, t.Context(), s1, s2)
				cs.w.ProvjeriSve()
				cn.w.ProvjeriSve()
				provjeriRazgovor(t, t.Context(), s1, s2)
				if zatvorena(s1, 0) || zatvorena(s2, 0) {
					t.Errorf("sesija zatvorena: %#x %#x", s1.Kod(), s2.Kod())
				}
			})
		})
	}
}
