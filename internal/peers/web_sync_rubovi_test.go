package peers

// Rubovi WEB synca u krugu razmjene, u mjehuru: mrtva sesija ne drži krug,
// pauza uz staru LAN adresu i iza NAT-a koji ne čuva port, ključ iz
// članstva i kad zapis čvora nosi drugi, i najviše adresa po putu.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/mrezatest"
	"gocop/internal/natsim"
	"gocop/internal/razmjena"
)

// Druga strana nestane (njezin NAT baca sve), a sesija kod ovog računala
// još nije istekla. Sesija ne odgovori na ping u 1 s, pa se zatvara, a krug
// ide Tailscaleom: mrtva sesija ne drži Tailscale ni tunel.
func TestMrtvaSesijaNeDrziKrug(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		nm := mrezaWeba(t)
		c := clanoviMreze(t, false, "cop-osijek", "thinkcentre-ured")
		kucni, tc := c[0], c[1]
		posta := novaPostaWeba(t)
		for i, s := range c {
			upoznaj(t, s, c[1-i])
			znaWeb(t, s, c[1-i].node.ID)
			s.dial = bezOdgovora
		}
		sWebom(t, kucni, posta, nm.NAT(natEIM, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713", true)
		nb := nm.NAT(natEIM, netip.MustParseAddr("203.0.113.2"))
		sWebom(t, tc, posta, nb, "192.168.2.20:4713", true)
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatalf("prvi krug: %v", err)
		}
		pricekajPozadinu()
		ses := kucni.webSync().Sesija(tc.javni())
		if ses == nil {
			t.Fatal("sesije nema")
		}
		m := mrezatest.NovaMreza()
		slusaNa(t, m, tc, "100.101.0.2:4710")
		kucni.ZadajTailscale(adresa("100.101.0.1"))
		kucni.upisiTailscale(ctxRub, tc.node.ID, []string{"100.101.0.2:4710"})
		var z zapisnikPoziva
		kucni.dial = z.prema(m, map[string]string{"100.101.0.2:4710": "100.101.0.2:4710"})
		nb.Gubitak(1)
		time.Sleep(5 * time.Second)
		pocetak := time.Now()
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); err != nil {
			t.Fatal(err)
		}
		kad, zvan := z.kad("100.101.0.2:4710")
		if !zvan || kad.Sub(pocetak) != rokPinga {
			t.Errorf("Tailscale zvan %v, %v nakon početka", zvan, kad.Sub(pocetak))
		}
		if st := stanjeS(t, kucni, tc.node.ID); st.Put.Naziv != razmjena.NazivTailscale || st.Fails != 0 {
			t.Errorf("put %+v, neuspjeha %d", st.Put, st.Fails)
		}
		select {
		case <-ses.Gotova():
		case <-time.After(5 * time.Second):
			t.Error("mrtva sesija nije zatvorena")
		}
	})
}

// Par je u pauzi (druga strana ima isključen WEB sync), a kućni server ima
// staru nedostupnu LAN adresu ThinkCentrea (192.168.24.70): pločica kaže
// pauzu, ne „bez odgovora”.
func TestPauzaUzStaruLANAdresu(t *testing.T) {
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
		upoznaj(t, kucni, tc, "192.168.24.70:4710")
		sWebom(t, kucni, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713", true)
		sWebom(t, tc, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", false)
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatal(err)
		}
		pricekajPozadinu()
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); err == nil || errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatalf("u pauzi: %v", err)
		}
		if st := stanjeS(t, kucni, tc.node.ID); st.Put.Razlog != razmjena.RazlogPutaPauza {
			t.Errorf("razlog puta u pauzi: %q", st.Put.Razlog)
		}
	})
}

// mjerenjeWeba je mreža računala iz zadnjeg pravog mjerenja (STUN iz
// natsima), kao u programu (osvjeziMrezu): mjeri se prije svake pripreme
type mjerenjeWeba struct {
	u         *razmjena.UDP
	lokalna   netip.AddrPort
	mu        sync.Mutex
	st        razmjena.StanjeMreze
	kandidati []razmjena.KandidatPoruke
	nat       *razmjena.NATPoruke
	javne     []string // javni kandidati svakog mjerenja
}

func (m *mjerenjeWeba) mjeri(ctx context.Context) {
	r := m.u.ProvjeriMrezu(ctx, stunNatsima)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st.Dodaj(r)
	m.kandidati = nil
	for _, k := range razmjena.SJavnim([]razmjena.Kandidat{{Vrsta: razmjena.KandidatLokalni, Adresa: m.lokalna}}, r, nil) {
		m.kandidati = append(m.kandidati, razmjena.KandidatPoruke{Vrsta: k.Vrsta, Adresa: k.Adresa.String()})
		if k.Vrsta == razmjena.KandidatJavni {
			m.javne = append(m.javne, k.Adresa.String())
		}
	}
	m.nat = &razmjena.NATPoruke{Vrsta: m.st.Vrsta, UDPBlokiran: m.st.Vrsta == razmjena.UDPBlokiran}
}

func (m *mjerenjeWeba) mreza() ([]razmjena.KandidatPoruke, *razmjena.NATPoruke) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.kandidati, m.nat
}

// sWebomIMjerenjem je sWebom s pravim mjerenjem mreže prije svake pripreme
func sWebomIMjerenjem(t *testing.T, s *Service, posta *postaWeba, n *natsim.NAT, lokalna string) *mjerenjeWeba {
	t.Helper()
	m := &mjerenjeWeba{lokalna: netip.MustParseAddrPort(lokalna)}
	m.u = razmjena.NoviUDP(n.Racunalo(m.lokalna, natsim.PostavkeRacunala{Kasnjenje: time.Millisecond}))
	t.Cleanup(func() { _ = m.u.Close() }) // greška zatvaranja na kraju testa nema što reći
	m.mjeri(t.Context())
	s.ZadajTailscale()
	s.izlazUDP = izlazUTestu
	p := s.PostavkeWebSynca(t.Context())
	p.Posalji, p.Sastajaliste, p.Mreza = posta.posalji, posta.javni(), m.mreza
	w, err := razmjena.NoviWebSync(m.u, p)
	if err != nil {
		t.Fatal(err)
	}
	posta.mu.Lock()
	posta.cvorovi[razmjena.PublicKeyString(s.javni())] = w
	posta.mu.Unlock()
	t.Cleanup(s.PokreniWebSync(t.Context(), w, true, m.mjeri))
	return m
}

// Laptop iza NAT-a koji ne čuva port (istek preslikavanja 30 s) mjeri mrežu
// prije svake pripreme i svaki put dobije drugi javni port, a ThinkCentre
// ima blokiran UDP. Novi port nije nova mreža: nakon tri neuspjeha par je u
// pauzi, pa četvrti i peti krug ne šalju poziv.
func TestPauzaIzaNATaBezCuvanjaPorta(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mrezaWeba(t)
		c := clanoviMreze(t, false, "laptop", "thinkcentre-ured")
		lap, tc := c[0], c[1]
		posta := novaPostaWeba(t)
		for i, s := range c {
			upoznaj(t, s, c[1-i])
			znaWeb(t, s, c[1-i].node.ID)
			s.dial = bezOdgovora
		}
		mj := sWebomIMjerenjem(t, lap, posta, m.NAT(natsim.Postavke{Kasnjenje: 10 * time.Millisecond}, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713")
		sWebom(t, tc, posta, m.NAT(natBlokiran, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", true)
		for krug := range 5 {
			_, _, err := lap.SyncWith(t.Context(), tc.node.ID)
			if (krug < 3) != errors.Is(err, ErrWebSyncSePriprema) {
				t.Fatalf("%d. krug: %v", krug+1, err)
			}
			pricekajPozadinu()
			time.Sleep(4 * time.Minute)
		}
		mj.mu.Lock()
		javne := slices.Compact(slices.Clone(mj.javne))
		mj.mu.Unlock()
		if len(javne) < 3 {
			t.Fatalf("javni port se nije mijenjao: %v", javne)
		}
		if n := posta.poslano(razmjena.VrstaPoziv); n != 3 || len(lap.pripreme.pauzeDo(time.Now())) != 1 {
			t.Errorf("poziva %d, pauze %v", n, lap.pripreme.pauzeDo(time.Now()))
		}
		if r := stanjeS(t, lap, tc.node.ID).Put.Razlog; r != razmjena.RazlogPutaUDPDoDrugih {
			t.Errorf("razlog puta: %q", r)
		}
	})
}

// Zapis ThinkCentrea u popisu računala nosi podmetnut ključ: priprema WEB
// synca i razgovor na sesiji ipak traže ključ iz članstva, pa sesija nastaje
// s pravim ThinkCentreom i razmjena prođe.
func TestPodmetnutKljucUZapisuZaWebSync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		kucni, tc, posta := parWeba(t, mrezaWeba(t), natEIM, natEIM)
		podmetnut, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := kucni.db.Exec(`UPDATE peers SET public_key = ? WHERE node_id = ?`, razmjena.PublicKeyString(podmetnut), tc.node.ID); err != nil {
			t.Fatal(err)
		}
		napuniKnjigu(t, kucni, 2)
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatalf("SyncWith: %v", err)
		}
		pricekajPozadinu()
		if kucni.webSync().Sesija(tc.javni()) == nil || kucni.webSync().Sesija(podmetnut) != nil || posta.poslano(razmjena.VrstaPoziv) != 1 {
			t.Fatal("sesija nije s ključem iz članstva")
		}
		if brojProbnih(t, tc) != 2 {
			t.Errorf("ThinkCentre ima %d zapisa", brojProbnih(t, tc))
		}
	})
}

// Član u zapis čvora upiše 500 privatnih i 500 javnih adresa: jedan krug
// zove najviše 8 LAN adresa i 8 javnih, prve po redu, a ne sve odjednom.
func TestNajviseAdresaPoPutu(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		kucni, tc, _ := parWeba(t, mrezaWeba(t), natEIM, natEIM)
		var lan, javne []string
		for i := range 500 {
			lan = append(lan, fmt.Sprintf("10.1.%d.%d:4710", i/250, 1+i%250))
			javne = append(javne, fmt.Sprintf("203.0.%d.%d:4710", 113+i/250, 1+i%250))
		}
		upoznaj(t, kucni, tc, append(lan, javne...)...)
		var mu sync.Mutex
		var zvane []string
		uLetu, najvise := 0, 0
		kucni.dial = func(ctx context.Context, a string) (net.Conn, error) {
			mu.Lock()
			zvane = append(zvane, a)
			uLetu++
			najvise = max(najvise, uLetu)
			mu.Unlock()
			defer func() {
				mu.Lock()
				uLetu--
				mu.Unlock()
			}()
			return bezOdgovora(ctx, a)
		}
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatal(err)
		}
		mu.Lock() // razmjena nakon pripreme u pozadini zove ponovno
		prvi := slices.Sorted(slices.Values(zvane))
		odjednom := najvise
		mu.Unlock()
		ocekivane := append(slices.Clone(lan[:najviseAdresaPuta]), javne[:najviseAdresaPuta]...)
		slices.Sort(ocekivane)
		if !slices.Equal(prvi, ocekivane) || odjednom > najviseAdresaPuta {
			t.Errorf("zvane %v, odjednom najviše %d", prvi, odjednom)
		}
		pricekajPozadinu()
	})
}

// Ograničenje vrijedi po putu, redom: tunel i Tailscale ostaju i kad je
// javnih adresa previše.
func TestOgraniciPoPutu(t *testing.T) {
	var ulaz []kandidat
	for i := range najviseAdresaPuta + 2 {
		ulaz = append(ulaz, kandidat{fmt.Sprintf("203.0.113.%d:4710", i+1), razmjena.PutAdresa})
	}
	ulaz = append(ulaz, kandidat{"100.101.0.2:4710", razmjena.PutTailscale}, kandidat{"https://cop.primjer.hr", razmjena.PutKrozTunel})
	out := ograniciPoPutu(ulaz, func(kk kandidat) razmjena.Put { return kk.put })
	if len(out) != najviseAdresaPuta+2 || !slices.Equal(out[:najviseAdresaPuta], ulaz[:najviseAdresaPuta]) || out[najviseAdresaPuta].put != razmjena.PutTailscale {
		t.Errorf("ograničeno: %v", out)
	}
}

// Na sesiji koja odgovara na ping razgovor ne kreće kad druga strana više
// nije član (sesija se zatvara kodom 0x0104), a prekinut krug ne zatvara
// sesiju koja radi.
func TestSesijaWebaRubovi(t *testing.T) {
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
		var odbij atomic.Bool
		sWebom(t, kucni, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713", true, func(p *razmjena.PostavkeWeb) {
			vjeruje := p.Trusted
			p.Trusted = func(k ed25519.PublicKey, vj []byte) bool { return !odbij.Load() && vjeruje(k, vj) }
		})
		sWebom(t, tc, posta, m.NAT(natEIM, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", true)
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatal(err)
		}
		pricekajPozadinu()
		ses := kucni.webSync().Sesija(tc.javni())
		if ses == nil {
			t.Fatal("sesije nema")
		}
		k := kucni.noviKrug(ctxRub, &Peer{NodeID: tc.node.ID}, tc.javni())
		if _, _, ok := k.sesijaWeba(otkazan()); ok || k.bezWeba {
			t.Fatal("razgovor ili odbačena sesija uz prekinut krug")
		}
		synctest.Wait()
		if kucni.webSync().Sesija(tc.javni()) != ses {
			t.Fatal("prekinut krug je zatvorio sesiju")
		}
		odbij.Store(true)
		if _, _, ok := k.sesijaWeba(ctxRub); ok {
			t.Error("razgovor s računalom koje više nije član")
		}
		<-ses.Gotova()
		if ses.Kod() != razmjena.KodNijeClan {
			t.Errorf("kod %#x", ses.Kod())
		}
	})
}
