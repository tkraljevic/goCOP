package peers

// Vrata za učenje adresa (naucene_adrese.go): što se nauči, što ostaje
// tvrdnja, a što ništa, i da razmjena zbog toga ne stvara nove verzije
// zapisa čvorova. Adrese veza su izmišljene: prerušene veze preko loopbacka
// i veze u memoriji (mrezatest), jer loopback nije privatna mreža.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/mrezatest"
	"gocop/internal/razmjena"
)

// verzijeZapisaCvorova broji verzije zapisa čvorova (peers) koje je napisalo
// ovo računalo; verzije koje su stigle razmjenom napisali su drugi
func verzijeZapisaCvorova(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ? AND node_id = ?`, EntityPeers, s.node.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// noviKljucCvora je izmišljeni ključ čvora
func noviKljucCvora(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// vezaIzMreze je prava veza razmjene čvora s, nakon TLS-a u kojem je druga
// strana dokazala ključ kljuc, preko mreže u memoriji: s zove s IP adrese
// lokalnaIP na adresu udaljena, koja smije biti i nešto što nije IP (p2p:x)
func vezaIzMreze(t *testing.T, s *Service, kljuc ed25519.PrivateKey, lokalnaIP, udaljena string) *razmjena.Conn {
	t.Helper()
	m := mrezatest.NovaMreza()
	ln, err := m.Slusaj(udaljena)
	if err != nil {
		t.Fatal(err)
	}
	ctx, otkazi := context.WithCancel(ctxRub)
	t.Cleanup(otkazi)
	vjeruje := func(ed25519.PublicKey, []byte) bool { return true }
	zatvori := func(c *razmjena.Conn) { c.Close() }
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, kljuc, Protocol, nil, ln, vjeruje, zatvori) // staje kad test završi, a vrata koja se ne otvore test vidi kao grešku poziva
	}()
	raw, err := m.DialContextOd(lokalnaIP)(ctxRub, "tcp", udaljena)
	if err != nil {
		t.Fatal(err)
	}
	c, err := razmjena.DialExchangeOn(ctxRub, raw, s.node.key, Protocol, s.vjerodajnice, udaljena, kljuc.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// prerusen je put prerušenog poziva: kamo poziv stvarno ide (loopback) i
// adresa druge strane koju veza pokazuje
type prerusen struct{ cilj, udaljena string }

// prerusenPoziv je s.dial koji adrese iz putevi zove prerušeno, kao da je
// veza otišla s adrese lokalna; ostale adrese zove kao i inače
func prerusenPoziv(lokalna string, putevi map[string]prerusen) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, adresa string) (net.Conn, error) {
		var d net.Dialer
		p, ok := putevi[adresa]
		if !ok {
			return d.DialContext(ctx, "tcp", adresa)
		}
		veza, err := d.DialContext(ctx, "tcp", p.cilj)
		if err != nil {
			return nil, err
		}
		return prerusenaVeza{Conn: veza, lokalna: mrezatest.TCP(lokalna), udaljena: mrezatest.TCP(p.udaljena)}, nil
	}
}

// obrisiBiljesku briše bilješku o razmjeni uz zapis čvora id, da test vidi
// kad stigne nova (cekajNovuBiljesku)
func obrisiBiljesku(t *testing.T, s *Service, id string) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE peers SET last_sync_note = '' WHERE node_id = ?`, id); err != nil {
		t.Fatal(err)
	}
}

// cekajNovuBiljesku čeka da s zapiše bilješku o razmjeni s čvorom id. To je
// zadnji upis noteSync, pa su nakon nje upisane i naučena adresa i tvrdnja.
func cekajNovuBiljesku(t *testing.T, s *Service, id string) {
	t.Helper()
	cekajDa(t, s.node.ID+" zabilježi razmjenu s "+id, func() bool {
		var b string
		err := s.db.QueryRow(`SELECT last_sync_note FROM peers WHERE node_id = ?`, id).Scan(&b)
		return err == nil && b != ""
	})
}

// Uči se samo adresa u privatnoj mreži (IPv4 i ULA, i IPv4 zapisan kao
// IPv6), ne Tailscaleova, 100.64.0.0/10, loopback, link-local, javna ni
// adresa koja nije IP. Odlazna veza nauči adresu na koju je otišla, a dolazna
// isti IP s portom razmjene koji se za računalo zna.
func TestPrivatnaIPIAdresaZaUcenje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	kljuc := noviKljucCvora(t)
	peer := &Peer{NodeID: "ured-vukovar", Addresses: []string{"cop-vukovar.example.hr:4720"}}
	for _, sl := range []struct {
		udaljena, odlazna, dolazna string // prazno: ne uči se
	}{
		{"192.168.24.70:4710", "192.168.24.70:4710", "192.168.24.70:4720"},
		{"10.20.30.40:4710", "10.20.30.40:4710", "10.20.30.40:4720"},
		{"172.16.5.6:4710", "172.16.5.6:4710", "172.16.5.6:4720"},
		{"[fd12::1]:4710", "[fd12::1]:4710", "[fd12::1]:4720"},
		{"[::ffff:192.168.1.2]:4710", "192.168.1.2:4710", "192.168.1.2:4720"},
		{"[fd7a:115c:a1e0::1]:4710", "", ""},
		{"100.64.0.7:4710", "", ""},
		{"127.0.0.1:4710", "", ""},
		{"[fe80::1]:4710", "", ""},
		{"93.184.216.34:4710", "", ""},
		{"p2p:x", "", ""},
	} {
		ap, ok := privatnaIP(mrezatest.TCP(sl.udaljena))
		if ok != (sl.odlazna != "") || (ok && ap.String() != sl.odlazna) {
			t.Errorf("privatnaIP(%s) = %s %v", sl.udaljena, ap, ok)
		}
		c := vezaIzMreze(t, s, kljuc, "192.168.24.92", sl.udaljena)
		c.Put = razmjena.PutAdresa
		if a, ok := s.adresaZaUcenje(peer, c, false); a != sl.odlazna || ok != (sl.odlazna != "") {
			t.Errorf("odlazna na %s uči %q %v", sl.udaljena, a, ok)
		}
		if a, ok := s.adresaZaUcenje(peer, c, true); a != sl.dolazna || ok != (sl.dolazna != "") {
			t.Errorf("dolazna s %s uči %q %v", sl.udaljena, a, ok)
		}
	}

	// put veze i lokalna adresa
	for _, sl := range []struct {
		ime, lokalnaIP string
		put            razmjena.Put
		uci            bool
	}{
		{"lan", "192.168.24.92", razmjena.PutLAN, true},
		{"adresa", "192.168.24.92", razmjena.PutAdresa, true},
		{"tailscale", "192.168.24.92", razmjena.PutTailscale, false},
		{"tunel", "192.168.24.92", razmjena.PutKrozTunel, false},
		{"probijeno", "192.168.24.92", razmjena.PutProbijeno, false},
		{"s adrese iz Tailscaleova raspona", "100.101.0.1", razmjena.PutAdresa, false},
		{"s Tailscaleove IPv6 adrese", "fd7a:115c:a1e0::5", razmjena.PutAdresa, false},
	} {
		c := vezaIzMreze(t, s, kljuc, sl.lokalnaIP, "192.168.24.70:4710")
		c.Put = sl.put
		if _, ok := s.adresaZaUcenje(peer, c, false); ok != sl.uci {
			t.Errorf("%s: uči %v", sl.ime, ok)
		}
	}
	if _, ok := s.adresaZaUcenje(peer, nil, false); ok {
		t.Error("bez veze se uči")
	}
}

// Bilješka o razmjeni ne stvara novu verziju zapisa čvora: ni kad veza ne
// smije učiti (tunel, Tailscale, probijeno, adresa koja nije IP, javna
// adresa), ni kad nauči adresu, ni kad je stariji program u zapisu ostavio
// Tailscale adresu (do 0.0.39 bilješka bi je očistila novom verzijom).
// Bilješka je svaki put zapisana, a adresa je naučena samo u zadnjem slučaju.
func TestNoteSyncBezNoveVerzije(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	kljuc := noviKljucCvora(t)
	if err := s.SavePeer(ctxRub, Peer{NodeID: "ured-vukovar", Name: "ured-vukovar", PublicKey: razmjena.PublicKeyString(kljuc.Public().(ed25519.PublicKey)),
		Addresses: []string{"cop-vukovar.example.hr:4710"}}); err != nil {
		t.Fatal(err)
	}
	const zapisStarijeg = `["100.101.0.9:4710","cop-vukovar.example.hr:4710"]`
	if _, err := s.db.Exec(`UPDATE peers SET addresses = ? WHERE node_id = 'ured-vukovar'`, zapisStarijeg); err != nil {
		t.Fatal(err)
	}
	peer, err := s.GetPeer(ctxRub, "ured-vukovar")
	if err != nil {
		t.Fatal(err)
	}
	verzija := verzijeZapisaCvorova(t, s)
	for i, sl := range []struct {
		ime, udaljena string
		put           razmjena.Put
		naucene       []string
	}{
		{"tunel", "192.168.24.70:4710", razmjena.PutKrozTunel, nil},
		{"tailscale", "192.168.24.70:4710", razmjena.PutTailscale, nil},
		{"probijeno", "192.168.24.70:4710", razmjena.PutProbijeno, nil},
		{"adresa koja nije IP", "p2p:x", razmjena.PutAdresa, nil},
		{"javna adresa", "93.184.216.34:4710", razmjena.PutAdresa, nil},
		{"privatna adresa", "192.168.24.70:4710", razmjena.PutAdresa, []string{"192.168.24.70:4710"}},
	} {
		c := vezaIzMreze(t, s, kljuc, "192.168.24.92", sl.udaljena)
		c.Put = sl.put
		s.noteSync(ctxRub, peer, s.izVeze(peer, c, syncOutcome{applied: i, sent: 1}))
		if n := verzijeZapisaCvorova(t, s); n != verzija {
			t.Errorf("%s: %d novih verzija zapisa čvora", sl.ime, n-verzija)
		}
		if got := s.naucene(ctxRub, "ured-vukovar"); !slices.Equal(got, sl.naucene) {
			t.Errorf("%s: naučeno %v", sl.ime, got)
		}
		p, err := s.GetPeer(ctxRub, "ured-vukovar")
		if err != nil || p.LastSyncNote != fmt.Sprintf("primljeno %d, poslano 1", i) || p.LastSync == nil || p.LastSeen == nil {
			t.Errorf("%s: bilješka %+v %v", sl.ime, p, err)
		}
	}
	var adrese string
	if err := s.db.QueryRow(`SELECT addresses FROM peers WHERE node_id = 'ured-vukovar'`).Scan(&adrese); err != nil || adrese != zapisStarijeg {
		t.Errorf("adrese u zapisu: %s %v", adrese, err)
	}
}

// Bilješka o neuspjeloj razmjeni ne briše kad je zadnja razmjena uspjela.
// Kad razmjena obriše zapis čvora (stigao je spomenik, pa je zapis maknut bez
// nove verzije ovdje), bilješka o toj razmjeni ga ne vraća. Do 0.0.39 ga je
// spremala iznova, a nova verzija otišla je svima.
func TestNoteSyncNeVracaObrisanZapis(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "ured-vukovar", Name: "ured-vukovar", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	peer, err := s.GetPeer(ctxRub, "ured-vukovar")
	if err != nil {
		t.Fatal(err)
	}
	s.noteSync(ctxRub, peer, syncOutcome{err: fmt.Errorf("veza prekinuta")})
	if p, err := s.GetPeer(ctxRub, "ured-vukovar"); err != nil || p == nil || p.LastSyncNote != "greška: veza prekinuta" || p.LastSync != nil || p.LastSeen == nil {
		t.Fatalf("bilješka bez ijedne uspješne razmjene: %+v %v", p, err)
	}
	s.noteSync(ctxRub, peer, syncOutcome{applied: 3, sent: 2})
	uspjela, err := s.GetPeer(ctxRub, "ured-vukovar")
	if err != nil || uspjela == nil || uspjela.LastSync == nil || uspjela.LastSyncNote != "primljeno 3, poslano 2" {
		t.Fatalf("bilješka o uspješnoj razmjeni: %+v %v", uspjela, err)
	}
	s.noteSync(ctxRub, peer, syncOutcome{err: fmt.Errorf("veza prekinuta")})
	if p, err := s.GetPeer(ctxRub, "ured-vukovar"); err != nil || p == nil || p.LastSyncNote != "greška: veza prekinuta" || p.LastSync == nil || !p.LastSync.Equal(*uspjela.LastSync) {
		t.Fatalf("bilješka o grešci nakon uspješne razmjene: %+v %v", p, err)
	}
	verzija := verzijeZapisaCvorova(t, s)
	if _, err := s.db.Exec(`DELETE FROM peers WHERE node_id = 'ured-vukovar'`); err != nil {
		t.Fatal(err)
	}
	s.noteSync(ctxRub, peer, syncOutcome{applied: 1})
	if p, err := s.GetPeer(ctxRub, "ured-vukovar"); p != nil || err != nil {
		t.Errorf("bilješka je vratila obrisan zapis: %+v %v", p, err)
	}
	if n := verzijeZapisaCvorova(t, s); n != verzija {
		t.Errorf("%d novih verzija zapisa čvora", n-verzija)
	}
}

// Dolazna razmjena s javne adrese ili iz 100.64.0.0/10 (Tailscale izvan
// ove mreže, mobilni operater) ne nauči ništa i ne stvara novu verziju.
// Dolazna s privatne adrese postane samo tvrdnja, s portom razmjene druge
// strane, i ni ona ne stvara verziju.
func TestDolazniJavniICGNATSeNeUci(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	verzija := verzijeZapisaCvorova(t, b)
	for _, sl := range []struct {
		udaljena string
		tvrdnja  []kandidat
	}{
		{"93.184.216.34:51234", nil},
		{"100.101.0.9:51234", nil},
		{"192.168.1.50:51234", []kandidat{{fmt.Sprintf("192.168.1.50:%d", a.ports.Exchange), razmjena.PutAdresa}}},
	} {
		upoznaj(t, a, b, prerusenaRazmjena(t, b, tcpAdresa(t, "192.168.1.2:4710"), tcpAdresa(t, sl.udaljena)))
		obrisiBiljesku(t, b, a.node.ID)
		if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
			t.Fatalf("%s: %v", sl.udaljena, err)
		}
		cekajNovuBiljesku(t, b, a.node.ID)
		if got := b.tvrdnjeZa(a.node.ID); !slices.Equal(got, sl.tvrdnja) {
			t.Errorf("%s: tvrdnja %v", sl.udaljena, got)
		}
		if got := b.naucene(ctxRub, a.node.ID); got != nil {
			t.Errorf("%s: naučeno %v", sl.udaljena, got)
		}
		if n := verzijeZapisaCvorova(t, b); n != verzija {
			t.Errorf("%s: %d novih verzija zapisa čvora", sl.udaljena, n-verzija)
		}
	}
}

// Dolazna razmjena s 192.168.24.1 na lokalnu 192.168.24.92 (s te adrese može
// zvati i usmjerivač koji prevodi adrese, a ne druga strana) ne nauči ništa
// dok ovo računalo ne nazove tu adresu i na njoj se ne javi očekivani ključ.
// Adresa iz zapisa ovdje ne odgovara, jer se tvrdnja zove tek iza nje
// (TestRedoslijedAdresaZaSpajanje). Kad poziv ne uspije, ne nauči se ništa. Kad uspije, adresa se nauči s portom razmjene
// druge strane. Tvrdnju svaki poziv potroši.
func TestDolaznaAdresaUciTekNakonPoziva(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	upoznaj(t, a, b, prerusenaRazmjena(t, b, tcpAdresa(t, "192.168.24.92:4710"), tcpAdresa(t, "192.168.24.1:51234")))
	zapis := fmt.Sprintf("192.0.2.1:%d", a.ports.Exchange)
	upoznaj(t, b, a, zapis)
	naA := fmt.Sprintf("192.168.24.1:%d", a.ports.Exchange)
	for _, sl := range []struct {
		ime     string
		cilj    string
		naucene []string
	}{
		{"nitko ne sluša", "127.0.0.1:1", nil},
		{"javi se druga strana", adresaNaPetlji(a), []string{naA}},
	} {
		obrisiBiljesku(t, b, a.node.ID)
		if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
			t.Fatalf("%s: %v", sl.ime, err)
		}
		cekajNovuBiljesku(t, b, a.node.ID)
		if got := b.tvrdnjeZa(a.node.ID); !slices.Equal(got, []kandidat{{naA, razmjena.PutAdresa}}) {
			t.Fatalf("%s: tvrdnja nakon dolazne razmjene %v", sl.ime, got)
		}
		if got := b.naucene(ctxRub, a.node.ID); got != nil {
			t.Fatalf("%s: naučeno prije poziva %v", sl.ime, got)
		}
		b.dial = prerusenPoziv("192.168.24.92:50000", map[string]prerusen{zapis: {"127.0.0.1:1", zapis}, naA: {sl.cilj, naA}})
		if _, _, err := b.SyncWith(ctxRub, a.node.ID); (err == nil) != (sl.naucene != nil) {
			t.Fatalf("%s: %v", sl.ime, err)
		}
		if got := b.naucene(ctxRub, a.node.ID); !slices.Equal(got, sl.naucene) {
			t.Errorf("%s: naučeno %v", sl.ime, got)
		}
		if got := b.tvrdnjeZa(a.node.ID); len(got) != 0 {
			t.Errorf("%s: tvrdnja nije potrošena: %v", sl.ime, got)
		}
		if p, ok := b.pokusajiAdresa(a.node.ID)[naA]; !ok || (p.Greska == "") != (sl.naucene != nil) {
			t.Errorf("%s: pokušaj na tvrdnji %+v %v", sl.ime, p, ok)
		}
	}
}

// Ruta podmreže: dolazna veza na Tailscale adresu ovog računala s obične
// adrese u drugoj mreži i odlazna s Tailscale adrese ovog računala na običnu
// adresu išle su preko Tailscalea, pa ne uče ništa i ne stvaraju verziju.
// Isto i kad računalo svoju Tailscale adresu ne prepozna (Mac kojem je u
// Tailscaleu isključen IPv6): put je tada adresa, ali lokalna adresa je iz
// Tailscaleova raspona. Kontrole: dolazna na običnu adresu postane tvrdnja, a
// odlazna s obične adrese nauči.
func TestRutaPodmrezeSeNeUci(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	verzijaB := verzijeZapisaCvorova(t, b)
	for _, sl := range []struct {
		ime, lokalna string
		tailscale    []netip.Addr
		put          razmjena.Put
		tvrdnje      []kandidat
	}{
		{"na prepoznatu Tailscale adresu", "100.101.0.2:4710", []netip.Addr{adresa("100.101.0.2")}, razmjena.PutTailscale, nil},
		{"na neprepoznatu Tailscale adresu", "100.101.0.2:4710", nil, razmjena.PutAdresa, nil},
		{"na običnu adresu", "192.168.24.92:4710", nil, razmjena.PutAdresa, []kandidat{{fmt.Sprintf("192.168.24.80:%d", a.ports.Exchange), razmjena.PutAdresa}}},
	} {
		b.ZadajTailscale(sl.tailscale...)
		upoznaj(t, a, b, prerusenaRazmjena(t, b, tcpAdresa(t, sl.lokalna), tcpAdresa(t, "192.168.24.80:51234")))
		obrisiBiljesku(t, b, a.node.ID)
		if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
			t.Fatalf("dolazna %s: %v", sl.ime, err)
		}
		cekajNovuBiljesku(t, b, a.node.ID)
		if got, put := b.tvrdnjeZa(a.node.ID), b.putZadnjeVeze(a.node.ID); !slices.Equal(got, sl.tvrdnje) || put != sl.put {
			t.Errorf("dolazna %s: tvrdnje %v, put %q", sl.ime, got, put)
		}
		if got := b.naucene(ctxRub, a.node.ID); got != nil {
			t.Errorf("dolazna %s: naučeno %v", sl.ime, got)
		}
	}
	if n := verzijeZapisaCvorova(t, b); n != verzijaB {
		t.Errorf("dolazne razmjene: %d novih verzija zapisa čvora", n-verzijaB)
	}

	naB := "192.168.24.80:4710"
	upoznaj(t, a, b, naB)
	for _, sl := range []struct {
		ime, lokalna string
		tailscale    []netip.Addr
		put          razmjena.Put
		naucene      []string
	}{
		{"s prepoznate Tailscale adrese", "100.101.0.1:51234", []netip.Addr{adresa("100.101.0.1")}, razmjena.PutTailscale, nil},
		{"s neprepoznate Tailscale adrese", "100.101.0.1:51234", nil, razmjena.PutAdresa, nil},
		{"s obične adrese", "192.168.24.92:51234", nil, razmjena.PutAdresa, []string{naB}},
	} {
		a.ZadajTailscale(sl.tailscale...)
		a.dial = prerusenPoziv(sl.lokalna, map[string]prerusen{naB: {adresaNaPetlji(b), naB}})
		verzija := verzijeZapisaCvorova(t, a)
		if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
			t.Fatalf("%s: %v", sl.ime, err)
		}
		if put := a.putZadnjeVeze(b.node.ID); put != sl.put {
			t.Errorf("%s: put %q", sl.ime, put)
		}
		if got := a.naucene(ctxRub, b.node.ID); !slices.Equal(got, sl.naucene) {
			t.Errorf("%s: naučeno %v", sl.ime, got)
		}
		if n := verzijeZapisaCvorova(t, a); n != verzija {
			t.Errorf("%s: %d novih verzija zapisa čvora", sl.ime, n-verzija)
		}
	}
}

// Naučena adresa, i iz obične razmjene i iz pronalaženja, ostaje samo na
// ovom računalu: ne stvara verziju i ne ide u zapis čvora ni drugom računalu.
// Drugo računalo, koje je pozvano s privatne adrese, iz toga ima samo
// tvrdnju. Naučena adresa ostaje i kad se program ponovno pokrene (nov
// Service nad istom bazom), a razmjena je zove iza adresa iz zapisa, ispred
// tunela.
func TestNaucenaAdresaOstajeLokalno(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	poImenu, tunel, izPronalazenja := "cop-vukovar.example.hr:4710", "https://cop.primjer.hr", "192.168.24.70:4711"
	upoznaj(t, a, b, poImenu, tunel)
	verzija, verzijaB := verzijeZapisaCvorova(t, a), verzijeZapisaCvorova(t, b)
	// ime vodi na adresu u lokalnoj mreži, a b vidi poziv s privatne adrese
	naB := prerusenaRazmjena(t, b, tcpAdresa(t, "192.168.24.80:4710"), tcpAdresa(t, "192.168.24.92:50000"))
	a.dial = prerusenPoziv("192.168.24.92:50000", map[string]prerusen{
		poImenu:        {naB, "192.168.24.80:4710"},
		izPronalazenja: {naB, izPronalazenja},
	})
	obrisiBiljesku(t, b, a.node.ID)
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	cekajNovuBiljesku(t, b, a.node.ID)
	if got := a.naucene(ctxRub, b.node.ID); !slices.Equal(got, []string{"192.168.24.80:4710"}) {
		t.Errorf("naučeno iz razmjene: %v", got)
	}
	a.zapamtiTvrdnju(ctxRub, razmjena.Found{Beacon: razmjena.Beacon{DeviceID: b.node.ID, ExchangePort: 4711}, Addr: "192.168.24.70"})
	obrisiBiljesku(t, b, a.node.ID)
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	cekajNovuBiljesku(t, b, a.node.ID)
	if got := a.naucene(ctxRub, b.node.ID); !slices.Equal(got, []string{izPronalazenja, "192.168.24.80:4710"}) {
		t.Errorf("naučeno iz pronalaženja: %v", got)
	}
	if put := a.putZadnjeVeze(b.node.ID); put != razmjena.PutLAN {
		t.Errorf("put veze na adresu iz pronalaženja: %q", put)
	}
	if n := verzijeZapisaCvorova(t, a); n != verzija {
		t.Errorf("%d novih verzija zapisa čvora", n-verzija)
	}
	for _, s := range []*Service{a, b} {
		var uKnjizi, uZapisu int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE payload LIKE '%192.168.24.%'`).Scan(&uKnjizi); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM peers WHERE addresses LIKE '%192.168.24.%'`).Scan(&uZapisu); err != nil {
			t.Fatal(err)
		}
		if uKnjizi != 0 || uZapisu != 0 {
			t.Errorf("%s: naučena adresa u %d verzija i %d zapisa čvorova", s.node.ID, uKnjizi, uZapisu)
		}
	}
	if got := b.naucene(ctxRub, a.node.ID); got != nil {
		t.Errorf("drugo računalo je naučilo %v", got)
	}
	if got := b.tvrdnjeZa(a.node.ID); !slices.Equal(got, []kandidat{{fmt.Sprintf("192.168.24.92:%d", a.ports.Exchange), razmjena.PutAdresa}}) {
		t.Errorf("tvrdnje drugog računala: %v", got)
	}
	if n := verzijeZapisaCvorova(t, b); n != verzijaB {
		t.Errorf("drugo računalo: %d novih verzija zapisa čvora", n-verzijaB)
	}

	ponovo, err := NewService(a.db, ledger.New(a.db, a.node.ID), a.node, Ports{})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := ponovo.GetPeer(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := []kandidat{{poImenu, razmjena.PutAdresa}, {izPronalazenja, razmjena.PutAdresa}, {"192.168.24.80:4710", razmjena.PutAdresa}, {tunel, razmjena.PutKrozTunel}}
	if got := ponovo.adreseZaSpajanje(ctxRub, peer); !slices.Equal(got, ocekivano) {
		t.Errorf("nakon ponovnog pokretanja: %v", got)
	}
}

// Na adresi iz pronalaženja javio se drugi član mreže, a ne čvor kojeg se
// traži: razmjena tu adresu odbije zbog drugog ključa, ne nauči je i potroši
// tvrdnju, a razmjena ide na sljedeću adresu. Kontrola: ista tvrdnja koja
// vodi do traženog čvora se nauči.
func TestTvrdnjaSKrivimKljucemSeNeUci(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar", "ured-osijek")
	a, b, d := c[0], c[1], c[2]
	naTvrdnji := "192.168.24.70:4711"
	for _, sl := range []struct {
		ime     string
		cvor    *Service
		naucene []string
	}{
		{"drugi član", d, nil},
		{"traženi čvor", b, []string{naTvrdnji}},
	} {
		a.dial = prerusenPoziv("192.168.24.92:50000", map[string]prerusen{naTvrdnji: {adresaNaPetlji(sl.cvor), naTvrdnji}})
		a.zapamtiTvrdnju(ctxRub, razmjena.Found{Beacon: razmjena.Beacon{DeviceID: b.node.ID, ExchangePort: 4711}, Addr: "192.168.24.70"})
		if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
			t.Fatalf("%s: %v", sl.ime, err)
		}
		greska := a.pokusajiAdresa(b.node.ID)[naTvrdnji].Greska
		if strings.Contains(greska, "DIFFERENT key") != (sl.naucene == nil) {
			t.Errorf("%s: pokušaj na tvrdnji: %q", sl.ime, greska)
		}
		if got := a.naucene(ctxRub, b.node.ID); !slices.Equal(got, sl.naucene) {
			t.Errorf("%s: naučeno %v", sl.ime, got)
		}
		if got := a.tvrdnjeZa(b.node.ID); len(got) != 0 {
			t.Errorf("%s: tvrdnja nije potrošena: %v", sl.ime, got)
		}
	}
}

// Ovo računalo pamti najviše četiri naučene adrese drugog računala, od
// najnovijeg uspjeha. Ponovljeni uspjeh pomakne adresu na vrh, a uspjeh
// stariji od sva četiri ne ulazi.
func TestNajviseCetiriNaucene(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	pocetak := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	for i := range 5 {
		s.upisiNaucenu(ctxRub, "ured-vukovar", fmt.Sprintf("192.168.24.%d:4710", 70+i), pocetak.Add(time.Duration(i)*time.Minute))
	}
	if got := s.naucene(ctxRub, "ured-vukovar"); !slices.Equal(got, []string{"192.168.24.74:4710", "192.168.24.73:4710", "192.168.24.72:4710", "192.168.24.71:4710"}) {
		t.Errorf("nakon pet uspjeha: %v", got)
	}
	s.upisiNaucenu(ctxRub, "ured-vukovar", "192.168.24.72:4710", pocetak.Add(time.Hour))
	if got := s.naucene(ctxRub, "ured-vukovar"); !slices.Equal(got, []string{"192.168.24.72:4710", "192.168.24.74:4710", "192.168.24.73:4710", "192.168.24.71:4710"}) {
		t.Errorf("nakon ponovljenog uspjeha: %v", got)
	}
	s.upisiNaucenu(ctxRub, "ured-vukovar", "192.168.24.99:4710", pocetak.Add(-time.Hour))
	if got := s.naucene(ctxRub, "ured-vukovar"); len(got) != 4 || slices.Contains(got, "192.168.24.99:4710") {
		t.Errorf("nakon starijeg uspjeha: %v", got)
	}
	if l := s.citajNaucene(ctxRub, "ured-vukovar"); !l[0].Uspjeh.Equal(pocetak.Add(time.Hour)) {
		t.Errorf("vrijeme uspjeha: %+v", l)
	}
	if got := s.naucene(ctxRub, "ured-osijek"); got != nil {
		t.Errorf("drugo računalo: %v", got)
	}
}

// Pokvaren zapis naučenih adresa znači da ih nema, a novi uspjeh ga
// prepiše. Kad se naučena adresa ne da spremiti, to piše u dnevniku, a
// zapamćeno ostaje.
func TestNauceneAdreseRubovi(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id, adrese) VALUES ('ured-vukovar', 'nije json')`); err != nil {
		t.Fatal(err)
	}
	if got := s.naucene(ctxRub, "ured-vukovar"); got != nil {
		t.Errorf("pokvaren zapis: %v", got)
	}
	s.upisiNaucenu(ctxRub, "ured-vukovar", "192.168.24.70:4710", time.Now())
	if got := s.naucene(ctxRub, "ured-vukovar"); !slices.Equal(got, []string{"192.168.24.70:4710"}) {
		t.Errorf("nakon uspjeha: %v", got)
	}
	dnevnik := zapisDnevnika(t)
	kvarUpisa(t, s, "UPDATE", "peer_sync", "")
	s.upisiNaucenu(ctxRub, "ured-vukovar", "192.168.24.71:4710", time.Now())
	if !strings.Contains(dnevnik.String(), "naučena adresa računala ured-vukovar nije spremljena") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
	if got := s.naucene(ctxRub, "ured-vukovar"); !slices.Equal(got, []string{"192.168.24.70:4710"}) {
		t.Errorf("nakon neuspjelog upisa: %v", got)
	}
}

// Tvrdnja iz pronalaženja ostaje i kad se ne da zapisati da je računalo
// viđeno (to piše u dnevniku). Ista adresa se ne ponavlja, najnovija je
// prva, a pamte se najviše četiri. Poziv adrese troši samo njezinu tvrdnju.
func TestTvrdnjeRubovi(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "ured-vukovar", Name: "ured-vukovar", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	kvarUpisa(t, s, "UPDATE", "peers", "")
	dnevnik := zapisDnevnika(t)
	for i := range 6 {
		s.zapamtiTvrdnju(ctxRub, razmjena.Found{Beacon: razmjena.Beacon{DeviceID: "ured-vukovar", ExchangePort: 4710}, Addr: fmt.Sprintf("192.168.24.%d", 70+i%5)})
	}
	lan := func(a string) kandidat { return kandidat{a, razmjena.PutLAN} }
	if got := s.tvrdnjeZa("ured-vukovar"); !slices.Equal(got, []kandidat{lan("192.168.24.70:4710"), lan("192.168.24.74:4710"), lan("192.168.24.73:4710"), lan("192.168.24.72:4710")}) {
		t.Errorf("tvrdnje: %v", got)
	}
	if !strings.Contains(dnevnik.String(), "nije spremljeno kad je računalo ured-vukovar viđeno u lokalnoj mreži") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
	s.potrosiTvrdnju("ured-vukovar", "192.168.24.74:4710")
	s.potrosiTvrdnju("ured-osijek", "192.168.24.70:4710")
	if got := s.tvrdnjeZa("ured-vukovar"); !slices.Equal(got, []kandidat{lan("192.168.24.70:4710"), lan("192.168.24.73:4710"), lan("192.168.24.72:4710")}) {
		t.Errorf("nakon poziva jedne adrese: %v", got)
	}
	if got := s.tvrdnjeZa("ured-osijek"); got != nil {
		t.Errorf("tvrdnje drugog računala: %v", got)
	}
}
