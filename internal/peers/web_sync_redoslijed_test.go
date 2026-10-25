package peers

// Redoslijed putova u krugu razmjene (redoslijed.go) uz WEB sync, u
// mjehuru: razmjena ne čeka pripremu, LAN pobijedi živu sesiju, sesija
// preživi krugove, javni ulaz i tunel bez pripreme, ukupni rok, pauza po
// paru, Tailscale i zajednička mjesta za razmjenu.

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/mrezatest"
	"gocop/internal/razmjena"
)

// slusaNa: čvor prima razmjene na adresi u mreži m, do kraja testa
func slusaNa(t *testing.T, m *mrezatest.Mreza, s *Service, adresa string) {
	t.Helper()
	ln, err := m.Slusaj(adresa)
	if err != nil {
		t.Fatal(err)
	}
	s.slusalica = ln
	ctx, otkazi := context.WithCancel(context.Background())
	stao := make(chan struct{})
	go func() {
		defer close(stao)
		_ = s.Serve(ctx) // staje na kraju testa; vrata koja ne rade test vidi kao neuspjelu razmjenu
	}()
	t.Cleanup(func() {
		otkazi()
		<-stao
	})
}

// zapisnikPoziva pamti kad je koja adresa nazvana
type zapisnikPoziva struct {
	mu     sync.Mutex
	pozivi map[string]time.Time
}

// prema je spajanje čvora: adrese iz kamo idu na tu adresu mreže m, a
// ostale ne odgovaraju; svaki poziv se zapiše
func (z *zapisnikPoziva) prema(m *mrezatest.Mreza, kamo map[string]string) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, adresa string) (net.Conn, error) {
		z.mu.Lock()
		if z.pozivi == nil {
			z.pozivi = map[string]time.Time{}
		}
		z.pozivi[adresa] = time.Now()
		z.mu.Unlock()
		cilj, ima := kamo[adresa]
		if !ima {
			return bezOdgovora(ctx, adresa)
		}
		return m.DialContextOd("10.0.0.1")(ctx, "tcp", cilj)
	}
}

func (z *zapisnikPoziva) kad(adresa string) (time.Time, bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	t, ima := z.pozivi[adresa]
	return t, ima
}

// sesijaKucnogServera: kućni server pripremi WEB sync s ThinkCentreom i s
// njim razmijeni; vraća par, poštu i mrežu u memoriji na kojoj ThinkCentre
// prima razmjene na 192.168.2.20:4710
func sesijaKucnogServera(t *testing.T) (kucni, tc *Service, posta *postaWeba, m *mrezatest.Mreza) {
	t.Helper()
	kucni, tc, posta = parWeba(t, mrezaWeba(t), natEIM, natEIM)
	m = mrezatest.NovaMreza()
	slusaNa(t, m, tc, "192.168.2.20:4710")
	if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
		t.Fatalf("prvi krug: %v", err)
	}
	pricekajPozadinu()
	if kucni.webSync().Sesija(tc.javni()) == nil {
		t.Fatal("sesije nema")
	}
	return kucni, tc, posta, m
}

// Kad LAN ne odgovori, razmjena ide Tailscaleom odmah, a priprema WEB
// synca teče u pozadini i ne drži mjesto za razmjenu: krug završi dok
// priprema još čeka odziv, a idući krug ne šalje drugi poziv. Pločica kaže
// da se WEB sync priprema.
func TestRazmjenaNeCekaPripremu(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		kucni, tc, posta := parWeba(t, mrezaWeba(t), natEIM, natEIM)
		m := mrezatest.NovaMreza()
		slusaNa(t, m, tc, "100.101.0.2:4710")
		kucni.ZadajTailscale(adresa("100.101.0.1"))
		kucni.upisiTailscale(ctxRub, tc.node.ID, []string{"100.101.0.2:4710"})
		var z zapisnikPoziva
		kucni.dial = z.prema(m, map[string]string{"100.101.0.2:4710": "100.101.0.2:4710"})
		posta.zadrziAko(func(pp razmjena.PotpisanaPoruka) bool { return pp.Vrsta == razmjena.VrstaOdziv })
		napuniKnjigu(t, kucni, 2)
		pocetak := time.Now()
		for krug := range 2 {
			if out := kucni.syncPeers(t.Context(), []Peer{{NodeID: tc.node.ID}}); !strings.HasPrefix(out[tc.node.ID], "primljeno") {
				t.Fatalf("%d. krug: %v", krug+1, out)
			}
		}
		if time.Since(pocetak) >= time.Second || brojProbnih(t, tc) != 2 || posta.poslano(razmjena.VrstaPoziv) != 1 {
			t.Fatalf("krugovi su trajali %v, zapisa %d, poziva %d", time.Since(pocetak), brojProbnih(t, tc), posta.poslano(razmjena.VrstaPoziv))
		}
		if st := stanjeS(t, kucni, tc.node.ID); st.Put.Naziv != razmjena.NazivTailscale || st.Put.Razlog != razmjena.RazlogPutaPriprema {
			t.Errorf("put: %+v", st.Put)
		}
		pricekajPozadinu()
		if st := stanjeS(t, kucni, tc.node.ID); st.Put.PripremaNeuspjesi != 1 || st.Fails != 0 {
			t.Errorf("priprema bez odziva: %+v", st)
		}
	})
}

// Laptop dođe kući usred WEB sync sesije: LAN se zove prvi i pobijedi
// živu sesiju, pa iduća razmjena ide LAN syncom. Sesija preživi dva kruga
// od 5 min bez novog poziva.
func TestLANPobijediZivuSesiju(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		kucni, tc, posta, m := sesijaKucnogServera(t)
		for range 2 {
			time.Sleep(5 * time.Minute)
			if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); err != nil {
				t.Fatal(err)
			}
			if st := stanjeS(t, kucni, tc.node.ID); st.Put.Put != razmjena.PutProbijeno || posta.poslano(razmjena.VrstaPoziv) != 1 {
				t.Fatalf("krug preko sesije: %+v, poziva %d", st.Put, posta.poslano(razmjena.VrstaPoziv))
			}
		}
		upoznaj(t, kucni, tc, "192.168.2.20:4710")
		var z zapisnikPoziva
		kucni.dial = z.prema(m, map[string]string{"192.168.2.20:4710": "192.168.2.20:4710"})
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); err != nil {
			t.Fatal(err)
		}
		if st := stanjeS(t, kucni, tc.node.ID); st.Put.Naziv != razmjena.NazivLAN || st.Put.Put != razmjena.PutAdresa {
			t.Errorf("laptop kod kuće: %+v", st.Put)
		}
		if kucni.webSync().Sesija(tc.javni()) == nil {
			t.Error("sesija je zatvorena")
		}
		k := kucni.noviKrug(ctxRub, &Peer{NodeID: tc.node.ID}, tc.javni())
		if _, _, ok := k.sesijaWeba(otkazan()); ok {
			t.Error("razgovor na sesiji uz prekinut kontekst")
		}
		k.bezWeba = true
		if _, _, ok := k.sesijaWeba(ctxRub); ok {
			t.Error("sesija koju je druga strana odbila")
		}
	})
}

// Od tri adrese koje sve odgovore (javne, pa kreću zajedno) razgovor ide
// samo na jednoj: ostale veze se zatvaraju prije razgovora, pa druga strana
// ne vidi neuspjelu razmjenu. Javna adresa koju je upisao čovjek zove se
// zajedno s Tailscaleom i tunelom, a pločica kaže SERVER sync; WEB sync se
// prema javnoj adresi koja odgovara ne priprema.
func TestJavneAdreseZajedno(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		kucni, tc, posta := parWeba(t, mrezaWeba(t), natEIM, natEIM)
		m := mrezatest.NovaMreza()
		m.Kasnjenje = 20 * time.Millisecond
		slusaNa(t, m, tc, "198.51.100.7:4710")
		javne := []string{"198.51.100.7:4710", "cop-tc.primjer.hr:4710", "203.0.113.70:4710"}
		upoznaj(t, kucni, tc, javne...)
		kucni.ZadajTailscale(adresa("100.101.0.1"))
		kucni.upisiTailscale(ctxRub, tc.node.ID, []string{"100.101.0.2:4710"})
		var z zapisnikPoziva
		kamo := map[string]string{}
		for _, a := range javne {
			kamo[a] = "198.51.100.7:4710"
		}
		kucni.dial = z.prema(m, kamo)
		pocetak := time.Now()
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); err != nil {
			t.Fatal(err)
		}
		for _, a := range append(javne, "100.101.0.2:4710") {
			if kad, ima := z.kad(a); !ima || kad != pocetak {
				t.Errorf("%s zvana %v nakon početka (%v)", a, kad.Sub(pocetak), ima)
			}
		}
		pricekajPozadinu()
		if st := stanjeS(t, kucni, tc.node.ID); st.Put.Naziv != razmjena.NazivServer || st.Put.Pokusaji != 1 || posta.poslano(razmjena.VrstaPoziv) != 0 {
			t.Errorf("put %+v, poziva %d", st.Put, posta.poslano(razmjena.VrstaPoziv))
		}
		if st := stanjeS(t, tc, kucni.node.ID); st.Put.Pokusaji != 1 || st.Fails != 0 {
			t.Errorf("druga strana: %+v", st)
		}
	})
}

// Cilj s tunelom je javni ulaz (kućni server): krug ide tunelom odmah nakon
// LAN-a, a WEB sync se prema njemu ne priprema ni u idućim krugovima.
// Spajanje ima rok od 45 s i kad nijedna adresa ne odgovara.
func TestTunelOdmahBezPripreme(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tc, kucni, posta := parWeba(t, mrezaWeba(t), natEIM, natEIM)
		upoznaj(t, tc, kucni, "192.168.1.10:4710", "cop.primjer.hr:4710", "https://cop.primjer.hr")
		var tunel []time.Time
		tc.tunelKlijent.Dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			tunel = append(tunel, time.Now())
			return bezOdgovora(ctx, "")
		}
		prije := rokOstalih
		t.Cleanup(func() { rokOstalih = prije })
		rokOstalih = time.Hour
		for krug := range 2 {
			pocetak := time.Now()
			_, _, err := tc.SyncWith(t.Context(), kucni.node.ID)
			if err == nil || errors.Is(err, ErrWebSyncSePriprema) || time.Since(pocetak) != rokSpajanja {
				t.Fatalf("%d. krug: %v nakon %v", krug+1, err, time.Since(pocetak))
			}
			if len(tunel) != krug+1 || tunel[krug].Sub(pocetak) != rokLAN {
				t.Fatalf("%d. krug: tunel zvan %v", krug+1, tunel)
			}
		}
		if n := posta.poslano(razmjena.VrstaPoziv); n != 0 {
			t.Errorf("poziva %d", n)
		}
	})
}

// Pozvani s isključenim WEB syncom odmah odgovori „isključeno”, pa par
// odmah ide u pauzu od 30 min: idući krug ne poziva, a pločica zna kad WEB
// sync pokušava ponovno. Poziv druge strane s drugim stanjem mreže i nova
// UDP utičnica brišu pauzu.
func TestPauzaNakonIskljucenog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mrezaWeba(t)
		c := clanoviMreze(t, false, "cop-osijek", "thinkcentre-ured")
		kucni, tc := c[0], c[1]
		posta := novaPostaWeba(t)
		for i, s := range c {
			upoznaj(t, s, c[1-i])
			znaWeb(t, s, c[1-i].node.ID)
		}
		w := sWebom(t, kucni, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713", true)
		sWebom(t, tc, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", false)
		pocetak := time.Now()
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatal(err)
		}
		pricekajPozadinu()
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); err == nil || errors.Is(err, ErrWebSyncSePriprema) || posta.poslano(razmjena.VrstaPoziv) != 1 {
			t.Fatalf("u pauzi: %v, poziva %d", err, posta.poslano(razmjena.VrstaPoziv))
		}
		if r := stanjeS(t, kucni, tc.node.ID).Put.Razlog; r != razmjena.RazlogPutaPauza {
			t.Errorf("razlog puta u pauzi: %s", r)
		}
		st, err := kucni.Status(ctxRub, false)
		if err != nil || len(st.Peers) != 1 {
			t.Fatal(st, err)
		}
		if u := st.Peers[0].State.Put.PonovnoU; u.Before(pocetak.Add(prvaPauza)) || u.After(pocetak.Add(prvaPauza+time.Second)) {
			t.Errorf("ponovno u %v, a pauza je počela oko %v", u, pocetak)
		}
		kucni.naPozivWeba(tc.javni(), "drugo stanje")
		if _, ok := kucni.pripreme.pauzeDo(time.Now())[tc.node.ID]; !ok {
			t.Fatal("pauza bez poznatog stanja druge strane ne ide")
		}
		kucni.pripreme.mu.Lock()
		kucni.pripreme.pauze[razmjena.PublicKeyString(tc.javni())].druge = "staro stanje"
		kucni.pripreme.mu.Unlock()
		kucni.naPozivWeba(tc.javni(), "drugo stanje")
		if len(kucni.pripreme.pauzeDo(time.Now())) != 0 {
			t.Fatal("pauza je ostala uz drugo stanje druge strane")
		}
		kucni.pripreme.neuspjeh(razmjena.PublicKeyString(tc.javni()), tc.node.ID, w.MojOtisak(), razmjena.GreskaPripreme{Razlog: razmjena.RazlogIskljuceno}, time.Now())
		stani := kucni.PokreniWebSync(t.Context(), w, true, nil) // nova utičnica: isti WEB sync ovdje je dovoljan
		defer stani()
		if len(kucni.pripreme.pauzeDo(time.Now())) != 0 {
			t.Error("pauza je ostala uz novu utičnicu")
		}
	})
}

// Uz [sync] tailscale = false i promet do druge strane kroz Tailscale
// ThinkCentre odbacuje udarce, pa priprema ne uspije i par ide prema
// pauzi; postavke WEB synca prate prekidač.
func TestWebSyncBezTailscalea(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mrezaWeba(t)
		c := clanoviMreze(t, false, "cop-osijek", "thinkcentre-ured")
		kucni, tc := c[0], c[1]
		posta := novaPostaWeba(t)
		for i, s := range c {
			upoznaj(t, s, c[1-i])
			znaWeb(t, s, c[1-i].node.ID)
			s.dial = bezOdgovora
		}
		tc.PostaviTailscale(false)
		tc.ZadajTailscale(adresa("100.101.0.2"))
		tc.izlazUDP = func(netip.AddrPort) (netip.Addr, error) { return adresa("100.101.0.2"), nil }
		if !tc.PostavkeWebSynca(ctxRub).BezTailscalea() || kucni.PostavkeWebSynca(ctxRub).BezTailscalea() {
			t.Fatal("postavke ne prate prekidač Tailscalea")
		}
		sWebom(t, kucni, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713", true)
		sWebom(t, tc, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", true)
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatal(err)
		}
		pricekajPozadinu()
		if st := stanjeS(t, kucni, tc.node.ID); st.Put.PripremaNeuspjesi != 1 || tc.webSync().Sesija(kucni.javni()) != nil {
			t.Errorf("priprema kroz isključen Tailscale: %+v", st.Put)
		}
	})
}

// Mjesta za razmjenu su zajednička portu, tunelu i WEB syncu: dok četiri
// razmjene na portu drže mjesta, razgovor na WEB sync sesiji dobiva
// „zauzet”.
func TestWebSyncDijeliMjesta(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		kucni, tc, _, m := sesijaKucnogServera(t)
		for range 4 {
			raw, err := m.DialContextOd("10.0.0.9")(t.Context(), "tcp", "192.168.2.20:4710")
			if err != nil {
				t.Fatal(err)
			}
			c, err := razmjena.DialExchangeOn(t.Context(), raw, kucni.node.key, kucni.protokol(), kucni.vjerodajnice, "192.168.2.20:4710", tc.javni())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
		}
		synctest.Wait()
		_, _, err := kucni.SyncWith(t.Context(), tc.node.ID)
		var o odbijenaRazmjena
		if !errors.As(err, &o) || o.kod != razmjena.KodZauzet {
			t.Fatalf("peti razgovor: %v", err)
		}
	})
}

// Opoziv člana usred WEB sync sesije zatvori sesiju kodom 0x0104 čim se
// opoziv zapiše (javiPromjenuClanstva → ProvjeriSve).
func TestOpozivZatvaraWebSesiju(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mrezaWeba(t)
		kucni := rubnaMreza(t, "cop-osijek")
		tc := rubniCvor(t, "thinkcentre-ured")
		primiIzravno(t, kucni, tc)
		posta := novaPostaWeba(t)
		for _, p := range [][2]*Service{{kucni, tc}, {tc, kucni}} {
			upoznaj(t, p[0], p[1])
			znaWeb(t, p[0], p[1].node.ID)
		}
		sWebom(t, kucni, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713", true)
		sWebom(t, tc, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", true)
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatal(err)
		}
		pricekajPozadinu()
		ses := kucni.webSync().Sesija(tc.javni())
		if ses == nil {
			t.Fatal("sesije nema")
		}
		if _, err := kucni.RevokeMembership(ctxRub, tc.node.ID); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ses.Gotova():
		case <-time.After(5 * time.Second):
			t.Fatal("sesija opozvanog nije zatvorena")
		}
		if ses.Kod() != razmjena.KodNijeClan {
			t.Errorf("kod %#x", ses.Kod())
		}
	})
}

// Utrka bez ijedne adrese odmah javlja grešku; veze koje se spoje nakon
// pobjednika zatvaraju se, a prekinuta utrka adresu ni ne zove.
func TestUtrkaRubovi(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := clanoviMreze(t, false, "cop-osijek", "thinkcentre-ured")
		a, b := c[0], c[1]
		peer := &Peer{NodeID: b.node.ID}
		if _, _, err := a.utrka(t.Context(), peer, b.javni(), nil, 0, time.Second); !errors.Is(err, errBezKandidata) {
			t.Errorf("bez adresa: %v", err)
		}
		m := mrezatest.NovaMreza()
		slusaNa(t, m, b, "198.51.100.7:4710")
		var z zapisnikPoziva
		a.dial = z.prema(m, map[string]string{"198.51.100.7:4710": "198.51.100.7:4710", "198.51.100.8:4710": "198.51.100.7:4710"})
		ishodi := make(chan ishodPoziva, 2)
		for _, adr := range []string{"198.51.100.7:4710", "198.51.100.8:4710"} {
			c, err := a.nazovi(t.Context(), peer, kandidat{adr, razmjena.PutAdresa}, b.javni(), time.Second)
			ishodi <- ishodPoziva{conn: c, err: err}
		}
		zatvoriOstale(ishodi, 2)
		synctest.Wait()
		if r := a.nazoviNakon(otkazan(), time.Second, peer, kandidat{"198.51.100.9:4710", razmjena.PutAdresa}, b.javni(), time.Second); r.conn != nil || r.err == nil {
			t.Errorf("prekinuta utrka je zvala: %+v", r)
		}
		if _, ima := z.kad("198.51.100.9:4710"); ima {
			t.Error("prekinuta utrka je zvala adresu")
		}
	})
}

// otkazan je otkazan kontekst
func otkazan() context.Context {
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	return ctx
}

// Kandidati se razvrstaju po koracima: pronalaženje i privatne adrese su
// LAN, a Tailscale, javne adrese, domene i tunel idu zajedno.
func TestKrugRazvrstava(t *testing.T) {
	k := &krug{}
	for _, kk := range []kandidat{{"192.168.1.10:4710", razmjena.PutLAN}, {"10.0.0.2:4710", razmjena.PutAdresa}, {"127.0.0.1:4710", razmjena.PutAdresa},
		{"100.101.0.2:4710", razmjena.PutTailscale}, {"cop.primjer.hr:4710", razmjena.PutAdresa}, {"https://cop.primjer.hr", razmjena.PutKrozTunel}} {
		k.dodaj(kk)
	}
	if len(k.lan) != 3 || len(k.ostali) != 3 || !k.tunel || !k.javnih {
		t.Errorf("krug: %+v", k)
	}
	k.zadnjiWeb = true
	k.bez(kandidat{"10.0.0.2:4710", razmjena.PutAdresa})
	if len(k.lan) != 2 || len(k.ostali) != 2 || !k.bezWeba || k.zadnjiWeb || slices.ContainsFunc(k.ostali, func(c kandidat) bool { return c.put == razmjena.PutTailscale }) {
		t.Errorf("bez odbijenog: %+v", k)
	}
	if k.razlog() != "" {
		t.Errorf("razlog bez LAN pokušaja: %s", k.razlog())
	}
	k.neodgovorili = []kandidat{{"192.168.1.10:4710", razmjena.PutLAN}}
	k.razlogWeba = razmjena.RazlogPutaPauza
	if k.razlog() != razmjena.RazlogPutaPauza {
		t.Errorf("razlog uz pauzu: %s", k.razlog())
	}
	k.razlogWeba = ""
	if k.razlog() != razmjena.RazlogPutaNijeSeJavio {
		t.Errorf("razlog uz LAN koji šuti: %s", k.razlog())
	}
	for _, c := range []struct{ err, zapamceno error }{{errBezKandidata, errBezKandidata}, {errDrugaAdresa, errDrugaAdresa}, {errBezKandidata, errDrugaAdresa}, {nil, nil}} {
		if k.zapamtiGresku(c.err); k.greska != c.zapamceno {
			t.Errorf("nakon %v zapamćeno %v", c.err, k.greska)
		}
	}
}
