package peers

// WEB sync kroz cijeli SyncWith (plan WEB synca, F3b, „Testovi”), u mjehuru
// (testing/synctest): računala s bazom iza NAT-ova iz natsima izmjere
// mrežu lažnim STUN-om, poziv, odziv i sinkro idu preko lažnog
// sastajališta (20 ms), a TCP ide mrežom u memoriji (mrezatest). Ništa ne
// ide stvarnom mrežom: spajanje koje test ne preusmjeri nikad ne odgovori
// (bezOdgovora), a izlazna adresa za UDP je izmišljena.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/natsim"
	"gocop/internal/razmjena"
)

var (
	stunNatsima = []razmjena.PosluziteljSTUN{
		{Ime: "Google", Adresa: net.UDPAddrFromAddrPort(natsim.STUNGoogle)},
		{Ime: "Cloudflare", Adresa: net.UDPAddrFromAddrPort(natsim.STUNCloudflare)},
	}
	natEIM        = natsim.Postavke{CuvaPort: true, Kasnjenje: 10 * time.Millisecond}
	natPoPortu    = natsim.Postavke{CuvaPort: true, Filtriranje: natsim.FiltriranjePoAdresiIPortu, Kasnjenje: 10 * time.Millisecond}
	natSimetricni = natsim.Postavke{Preslikavanje: natsim.PreslikavanjePoAdresiIPortu, Filtriranje: natsim.FiltriranjePoAdresiIPortu, Kasnjenje: 10 * time.Millisecond}
	natBlokiran   = natsim.Postavke{BlokiranUDP: true}
)

// postaWeba je lažno sastajalište: potpisanu poruku preda WEB syncu
// računala kojem ide nakon 20 ms; računalo koje nije prijavljeno javlja
// „nema”. zadrzi odlučuje koja poruka ne stiže.
type postaWeba struct {
	kljuc   ed25519.PrivateKey
	mu      sync.Mutex
	cvorovi map[string]*razmjena.WebSync
	vrste   []string
	zadrzi  func(razmjena.PotpisanaPoruka) bool
	poruke  sync.WaitGroup
}

func novaPostaWeba(t *testing.T) *postaWeba {
	_, kljuc, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := &postaWeba{kljuc: kljuc, cvorovi: map[string]*razmjena.WebSync{}, zadrzi: func(razmjena.PotpisanaPoruka) bool { return false }}
	t.Cleanup(p.poruke.Wait)
	return p
}

func (p *postaWeba) javni() ed25519.PublicKey { return p.kljuc.Public().(ed25519.PublicKey) }

func (p *postaWeba) posalji(pp razmjena.PotpisanaPoruka) error {
	t, err := pp.Zaglavlje()
	if err != nil {
		return err
	}
	p.mu.Lock()
	w, zadrzi := p.cvorovi[t.Za], p.zadrzi
	p.vrste = append(p.vrste, pp.Vrsta)
	p.mu.Unlock()
	switch {
	case w == nil:
		return errors.New(razmjena.NemaNijePrijavljen)
	case zadrzi(pp):
		return nil
	}
	p.poruke.Go(func() {
		time.Sleep(20 * time.Millisecond)
		w.NaPoruku(pp)
	})
	return nil
}

// poslano je koliko je poslano poruka te vrste
func (p *postaWeba) poslano(vrsta string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, v := range p.vrste {
		if v == vrsta {
			n++
		}
	}
	return n
}

func (p *postaWeba) zadrziAko(f func(razmjena.PotpisanaPoruka) bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.zadrzi = f
}

// mrezaWeba je internet iz natsima s dva lažna STUN poslužitelja
func mrezaWeba(t *testing.T) *natsim.Mreza {
	m := natsim.Nova()
	m.DvaSTUN()
	t.Cleanup(func() { _ = m.Close() }) // greška zatvaranja na kraju testa nema što reći
	return m
}

// uticnicaIza je UDP utičnica računala na lokalnoj adresi iza NAT-a n,
// izmjerena mreža i kandidati (javni samo uz lak NAT)
func uticnicaIza(t *testing.T, n *natsim.NAT, lokalna string) (*razmjena.UDP, []razmjena.KandidatPoruke, *razmjena.NATPoruke) {
	t.Helper()
	a := netip.MustParseAddrPort(lokalna)
	u := razmjena.NoviUDP(n.Racunalo(a, natsim.PostavkeRacunala{Kasnjenje: time.Millisecond}))
	t.Cleanup(func() { _ = u.Close() }) // greška zatvaranja na kraju testa nema što reći
	m := u.ProvjeriMrezu(t.Context(), stunNatsima)
	var st razmjena.StanjeMreze
	st.Dodaj(m)
	var kandidati []razmjena.KandidatPoruke
	for _, k := range razmjena.SJavnim([]razmjena.Kandidat{{Vrsta: razmjena.KandidatLokalni, Adresa: a}}, m, nil) {
		kandidati = append(kandidati, razmjena.KandidatPoruke{Vrsta: k.Vrsta, Adresa: k.Adresa.String()})
	}
	return u, kandidati, &razmjena.NATPoruke{Vrsta: st.Vrsta, UDPBlokiran: st.Vrsta == razmjena.UDPBlokiran}
}

// izlazUTestu je izmišljena izlazna adresa: promet ne ide kroz Tailscale
func izlazUTestu(netip.AddrPort) (netip.Addr, error) { return netip.MustParseAddr("192.0.2.1"), nil }

// sWebom da čvoru WEB sync na utičnici iza NAT-a n, prijavljen kod poste;
// ukljucen je web_sync. WEB sync staje na kraju testa.
func sWebom(t *testing.T, s *Service, posta *postaWeba, n *natsim.NAT, lokalna string, ukljucen bool, uz ...func(*razmjena.PostavkeWeb)) *razmjena.WebSync {
	t.Helper()
	u, kandidati, nat := uticnicaIza(t, n, lokalna)
	if s.tailscaleAdrese == nil {
		s.ZadajTailscale()
	}
	if s.izlazUDP == nil {
		s.izlazUDP = izlazUTestu
	}
	p := s.PostavkeWebSynca(t.Context())
	p.Posalji, p.Sastajaliste = posta.posalji, posta.javni()
	p.Mreza = func() ([]razmjena.KandidatPoruke, *razmjena.NATPoruke) { return kandidati, nat }
	for _, f := range uz {
		f(&p)
	}
	w, err := razmjena.NoviWebSync(u, p)
	if err != nil {
		t.Fatal(err)
	}
	posta.mu.Lock()
	posta.cvorovi[razmjena.PublicKeyString(s.javni())] = w
	posta.mu.Unlock()
	t.Cleanup(s.PokreniWebSync(t.Context(), w, ukljucen, nil))
	return w
}

// bezOdgovora je spajanje koje nikad ne odgovori, do roka
func bezOdgovora(ctx context.Context, _ string) (net.Conn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// znaWeb: računalo s zna iz zadnje razmjene da druga strana ima web/1
func znaWeb(t *testing.T, s *Service, nodeID string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id, mogucnosti, mogucnosti_poznate) VALUES (?, ?, 1)
		ON CONFLICT(node_id) DO UPDATE SET mogucnosti = excluded.mogucnosti, mogucnosti_poznate = 1`,
		nodeID, `["granica-odbijenih/1","razmjena/1","rupe/1","web/1"]`); err != nil {
		t.Fatal(err)
	}
}

// parWeba su kućni server (cop-osijek) i ThinkCentre (thinkcentre-ured),
// članovi iste mreže koji znaju jedan za drugoga bez ijedne adrese, svaki iza
// svog NAT-a (192.168.1.10 i 192.168.2.20), s WEB syncom; svaki zna da drugi
// ima web/1, a spajanje koje test ne preusmjeri ne odgovara
func parWeba(t *testing.T, m *natsim.Mreza, pa, pb natsim.Postavke) (a, b *Service, posta *postaWeba) {
	t.Helper()
	c := clanoviMreze(t, false, "cop-osijek", "thinkcentre-ured")
	a, b = c[0], c[1]
	posta = novaPostaWeba(t)
	for i, s := range c {
		upoznaj(t, s, c[1-i])
		znaWeb(t, s, c[1-i].node.ID)
		s.dial = bezOdgovora
	}
	sWebom(t, a, posta, m.NAT(pa, netip.MustParseAddr("203.0.113.1")), "192.168.1.10:4713", true)
	sWebom(t, b, posta, m.NAT(pb, netip.MustParseAddr("203.0.113.2")), "192.168.2.20:4713", true)
	return a, b, posta
}

// stanjeS je stanje razmjene ovog čvora s drugim
func stanjeS(t *testing.T, s *Service, nodeID string) SyncState {
	t.Helper()
	st, err := s.syncStates(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	return st[nodeID]
}

// pricekajPozadinu pusti probni sat da pripreme u pozadini i razmjene nakon
// njih završe
func pricekajPozadinu() {
	time.Sleep(time.Minute)
	synctest.Wait()
}

var paroviNAT = []struct {
	ime    string
	a, b   natsim.Postavke
	uspjeh bool
}{
	{"EIM i EIM", natEIM, natEIM, true},
	{"EIM i EIM po portu", natEIM, natPoPortu, true},
	{"EIM i simetrični", natEIM, natSimetricni, true},
	{"EIM i blokiran", natEIM, natBlokiran, false},
	{"EIM po portu i EIM", natPoPortu, natEIM, true},
	{"EIM po portu i EIM po portu", natPoPortu, natPoPortu, true},
	{"EIM po portu i simetrični", natPoPortu, natSimetricni, false},
	{"EIM po portu i blokiran", natPoPortu, natBlokiran, false},
	{"simetrični i EIM", natSimetricni, natEIM, true},
	{"simetrični i EIM po portu", natSimetricni, natPoPortu, false},
	{"simetrični i simetrični", natSimetricni, natSimetricni, false},
	{"simetrični i blokiran", natSimetricni, natBlokiran, false},
}

// Tablica NAT parova iz plana kroz cijeli SyncWith, i kad zove računalo s
// manjim ključem i kad zove ono s većim: do druge strane nema puta, pa
// SyncWith vrati „WEB sync se priprema” i to ne broji kao neuspjeh. Kad
// priprema uspije, razmjena kreće odmah, sama, preko WEB synca; kad ne
// uspije, broji se neuspjela priprema, a podaci ne idu (SERVER sync ide
// preko kućnog servera u dva kruga, što ovdje nije).
func TestWebSyncTablicaKrozSyncWith(t *testing.T) {
	for _, c := range paroviNAT {
		for _, zoveManji := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, zove manji ključ %v", c.ime, zoveManji), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					a, b, posta := parWeba(t, mrezaWeba(t), c.a, c.b)
					napuniKnjigu(t, a, 2)
					napuniKnjigu(t, b, 1)
					zove, zvani := a, b
					if (string(a.javni()) < string(b.javni())) != zoveManji {
						zove, zvani = b, a
					}
					if _, _, err := zove.SyncWith(t.Context(), zvani.node.ID); !errors.Is(err, ErrWebSyncSePriprema) {
						t.Fatalf("SyncWith: %v", err)
					}
					pricekajPozadinu()
					provjeriTablicu(t, zove, zvani, c.uspjeh)
					if n := posta.poslano(razmjena.VrstaPoziv); n != 1 {
						t.Errorf("poziva %d", n)
					}
				})
			})
		}
	}
}

// provjeriTablicu: uspjeh je razmjena preko WEB synca na obje strane, a
// neuspjeh samo brojač; ni jedno nije neuspjela razmjena
func provjeriTablicu(t *testing.T, zove, zvani *Service, uspjeh bool) {
	t.Helper()
	st := stanjeS(t, zove, zvani.node.ID)
	if st.Fails != 0 || st.Put.PripremaPokusaji != 1 {
		t.Errorf("neuspjeha %d, priprema %d", st.Fails, st.Put.PripremaPokusaji)
	}
	if !uspjeh {
		if st.Put.PripremaNeuspjesi != 1 || st.Put.Razlog != razmjena.RazlogPutaPriprema || brojProbnih(t, zove)+brojProbnih(t, zvani) != 3 {
			t.Errorf("neuspjela priprema: %+v, zapisa %d i %d", st.Put, brojProbnih(t, zove), brojProbnih(t, zvani))
		}
		return
	}
	if st.Put.PripremaUspjesi != 1 || st.Put.Put != razmjena.PutProbijeno || st.Put.Naziv != razmjena.NazivWeb || st.Put.ZadnjiZvao != "ovaj" {
		t.Errorf("put nakon pripreme: %+v", st.Put)
	}
	if brojProbnih(t, zove) != 3 || brojProbnih(t, zvani) != 3 {
		t.Errorf("zapisa %d i %d", brojProbnih(t, zove), brojProbnih(t, zvani))
	}
	if d := stanjeS(t, zvani, zove.node.ID); d.Put.Put != razmjena.PutProbijeno || d.Put.ZadnjiZvao != "drugi" {
		t.Errorf("put kod druge strane: %+v", d.Put)
	}
}

// Kućni server zove ThinkCentre, u dva slučaja: kućni server nema nijednu
// adresu ThinkCentrea, i ima samo nedostupnu 192.168.24.70. U oba pripremi
// WEB sync u pozadini i odmah zatim razmijeni s ThinkCentreom. Razmjena
// preko WEB synca ne uči adrese (noteSync) i ne javlja Tailscale adrese, a
// stari LAN kandidat čeka samo svoj rok od 3 s.
func TestKucniServerZoveThinkCentre(t *testing.T) {
	for _, adrese := range [][]string{nil, {"192.168.24.70:4710"}} {
		t.Run(fmt.Sprint(adrese), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				kucni, tc, _ := parWeba(t, mrezaWeba(t), natEIM, natEIM)
				upoznaj(t, kucni, tc, adrese...)
				kucni.ZadajTailscale(adresa("100.101.0.1"))
				kucni.ports.Exchange = DefaultExchangePort
				napuniKnjigu(t, kucni, 3)
				pocetak := time.Now()
				_, _, err := kucni.SyncWith(t.Context(), tc.node.ID)
				if !errors.Is(err, ErrWebSyncSePriprema) || time.Since(pocetak) != time.Duration(len(adrese))*rokLAN {
					t.Fatalf("SyncWith: %v nakon %v", err, time.Since(pocetak))
				}
				pricekajPozadinu()
				if brojProbnih(t, tc) != 3 {
					t.Fatalf("ThinkCentre ima %d zapisa", brojProbnih(t, tc))
				}
				if n := kucni.naucene(ctxRub, tc.node.ID); len(n) != 0 {
					t.Errorf("kućni server je naučio %v", n)
				}
				if n := tc.naucene(ctxRub, kucni.node.ID); len(n) != 0 || len(tc.tvrdnjePutem(kucni.node.ID, razmjena.PutAdresa)) != 0 {
					t.Errorf("ThinkCentre je naučio %v", n)
				}
				if ts := tc.tailscaleOd(ctxRub, kucni.node.ID); len(ts) != 0 {
					t.Errorf("Tailscale adrese preko WEB synca: %v", ts)
				}
			})
		})
	}
}
