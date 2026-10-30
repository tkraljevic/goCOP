package peers

// Poznati čvorovi: zaborav, port razmjene za adresu bez porta, pronalaženje
// na lokalnoj mreži i čekanje na uparivanje. Druge čvorove na mreži glumi
// lažni glasnik na loopbacku.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// Zaboravljeni čvor nestaje s popisa, a u knjizi ostaje arhivirana verzija,
// pa zaborav putuje i drugima. Zaborave se i adrese koje je ovaj čvor o njemu
// naučio. Nepoznat čvor se ne može zaboraviti.
func TestZaboravCvora(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	s.upisiNaucenu(ctxRub, "laptop-baranja", "192.168.24.70:4710", time.Now())
	if err := s.ForgetPeer(ctxRub, "laptop-baranja"); err != nil {
		t.Fatal(err)
	}
	if p, err := s.GetPeer(ctxRub, "laptop-baranja"); p != nil || err != nil {
		t.Errorf("zaboravljeni čvor je još tu: %+v %v", p, err)
	}
	if got := s.naucene(ctxRub, "laptop-baranja"); got != nil {
		t.Errorf("naučene adrese zaboravljenog čvora: %v", got)
	}
	var arhivirano int
	if err := s.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = 'laptop-baranja' AND archived = 1`,
		EntityPeers).Scan(&arhivirano); err != nil || arhivirano != 1 {
		t.Errorf("arhiviranih verzija %d (%v), očekivana jedna", arhivirano, err)
	}
	if err := s.ForgetPeer(ctxRub, "laptop-baranja"); err == nil {
		t.Error("nepoznat čvor je zaboravljen")
	}
}

// Kad brisanje zapisa ili naučenih adresa ili arhiviranje ne uspije, čvor
// ostaje na popisu, a naučene adrese ostaju; kad se popis ne da pročitati, ne
// briše se ništa. Pokušaji spajanja, put zadnje veze i tvrdnje o adresama
// zaborave se svaki put.
func TestZaboravKadBazaNeSuradjuje(t *testing.T) {
	for ime, kvar := range map[string]func(t *testing.T, s *Service){
		"brisanje padne":              func(t *testing.T, s *Service) { kvarUpisa(t, s, "DELETE", "peers", "") },
		"naučene se ne daju obrisati": func(t *testing.T, s *Service) { kvarUpisa(t, s, "UPDATE", "peer_sync", "") },
		"arhiviranje padne":           func(t *testing.T, s *Service) { kvarUpisa(t, s, "INSERT", "record_versions", "NEW.archived = 1") },
		"popis se ne čita":            func(t *testing.T, s *Service) { pokvariTablicu(t, s, "peers") },
	} {
		t.Run(ime, func(t *testing.T) {
			s := rubniCvor(t, "cop-osijek")
			if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "laptop-baranja", PublicKey: javniKljuc(t)}); err != nil {
				t.Fatal(err)
			}
			s.zapamtiPokusaj("laptop-baranja", "192.0.2.7:4710", nil)
			s.zapamtiPut(&Peer{NodeID: "laptop-baranja"}, &razmjena.Conn{Put: razmjena.PutLAN})
			s.dodajTvrdnju("laptop-baranja", kandidat{"192.168.24.70:4710", razmjena.PutAdresa})
			s.upisiNaucenu(ctxRub, "laptop-baranja", "192.168.24.71:4710", time.Now())
			kvar(t, s)
			if err := s.ForgetPeer(ctxRub, "laptop-baranja"); err == nil {
				t.Fatal("zaborav je prošao")
			}
			if n, put, tvrdnje := len(s.pokusajiAdresa("laptop-baranja")), s.putZadnjeVeze("laptop-baranja"), s.tvrdnjeZa("laptop-baranja"); n != 0 || put != "" || tvrdnje != nil {
				t.Errorf("nakon neuspjelog zaborava: %d pokušaja, put %q, tvrdnje %v", n, put, tvrdnje)
			}
			if ime == "popis se ne čita" {
				return
			}
			if p, _ := s.GetPeer(ctxRub, "laptop-baranja"); p == nil {
				t.Error("čvor je nestao iako zaborav nije uspio")
			}
			if got := s.naucene(ctxRub, "laptop-baranja"); !slices.Equal(got, []string{"192.168.24.71:4710"}) {
				t.Errorf("naučene adrese nakon neuspjelog zaborava: %v", got)
			}
		})
	}
}

// Adresa bez porta: za ovaj čvor vrijedi njegov port razmjene, za tuđi
// port iz neke od poznatih adresa, a inače zadani. Adresa tunela i adresa
// koja nije IP s portom ne daju port. Taj port zove razmjena (nazovi) i
// dobije ga ručno dodana adresa.
func TestPortRazmjeneZaAdresuBezPorta(t *testing.T) {
	s := &Service{node: &Node{ID: "cop-osijek"}, ports: Ports{Exchange: 4999}}
	for _, p := range []struct {
		ime  string
		cvor Peer
		port string
	}{
		{"ovaj čvor", Peer{NodeID: "cop-osijek", Addresses: []string{"gocop.example.hr:4710"}}, "4999"},
		{"port iz poznate adrese", Peer{NodeID: "ured-vukovar", Addresses: []string{"192.0.2.7", "192.0.2.8:4720"}}, "4720"},
		{"adrese bez porta", Peer{NodeID: "ured-vukovar", Addresses: []string{"192.0.2.7"}}, "4710"},
		{"bez adresa", Peer{NodeID: "ured-vukovar"}, "4710"},
		{"tunel ispred adrese bez porta", Peer{NodeID: "ured-vukovar", Addresses: []string{"https://cop.primjer.hr", "192.0.2.7"}}, "4710"},
		{"adresa koja nije IP", Peer{NodeID: "ured-vukovar", Addresses: []string{"p2p:x", "192.0.2.8:4720"}}, "4720"},
		{"port izvan raspona", Peer{NodeID: "ured-vukovar", Addresses: []string{"192.0.2.9:0", "192.0.2.10:70000", "192.0.2.8:4720"}}, "4720"},
	} {
		if got := s.exchangePortOf(p.cvor); got != p.port {
			t.Errorf("%s: port %s, očekivan %s", p.ime, got, p.port)
		}
	}

	var zvano []string
	s.dial = func(_ context.Context, adresa string) (net.Conn, error) {
		zvano = append(zvano, adresa)
		return nil, errors.New("dial tcp " + adresa + ": connect: connection refused")
	}
	for _, p := range []struct {
		adrese []string
		zove   string
	}{
		{[]string{"192.0.2.7", "192.0.2.8:4720"}, "192.0.2.7:4720"},
		{[]string{"https://cop.primjer.hr", "192.0.2.7"}, "192.0.2.7:4710"},
	} {
		zvano = nil
		peer := &Peer{NodeID: "ured-vukovar", Addresses: p.adrese}
		if _, err := s.nazovi(ctxRub, peer, kandidat{"192.0.2.7", razmjena.PutAdresa}, nil, rokOstalih); err == nil || !slices.Equal(zvano, []string{p.zove}) {
			t.Errorf("%v: zvano %v (%v), očekivano %s", p.adrese, zvano, err, p.zove)
		}
	}

	peer := Peer{NodeID: "ured-vukovar", Addresses: []string{"https://cop.primjer.hr"}}
	if err := s.dodajAdresu(&peer, "192.0.2.7"); err != nil || !slices.Equal(peer.Addresses, []string{"https://cop.primjer.hr", "192.0.2.7:4710"}) {
		t.Errorf("ručno dodana adresa iza tunela: %v %v", peer.Addresses, err)
	}
}

// lazniGlasnik odgovara na svaku probu pronalaženja svim zadanim
// beaconima, kao da se na lokalnoj mreži javlja više čvorova; vraća port
// na kojem sluša (samo loopback)
func lazniGlasnik(t *testing.T, beaconi ...razmjena.Beacon) int {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			_, od, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			for _, b := range beaconi {
				out, _ := json.Marshal(b)
				_, _ = conn.WriteTo(out, od)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

// Pronalaženje ne vraća ovaj čvor, za poznati čvor kaže da je poznat, a
// nepoznati samo navede. Adresa na kojoj se poznati čvor javio (laptop se
// preselio) ne ide u njegov zapis, pa nema nove verzije: to je tvrdnja, koju
// razmjena nazove ispred adresa iz zapisa. Tvrdnja ostaje dok je razmjena ne
// nazove (i kad je poziv prekinut zato što je prekinuta cijela razmjena), a
// nakon toga je nema.
func TestPronalazenjeOsvjezavaPoznateCvorove(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "laptop-baranja", PublicKey: javniKljuc(t),
		Addresses: []string{"192.0.2.10:4710"}}); err != nil {
		t.Fatal(err)
	}
	verzija := verzijeZapisaCvorova(t, s)
	s.ports.Discovery = lazniGlasnik(t,
		razmjena.Beacon{Protocol: Protocol, DeviceID: "cop-osijek", Name: "Osijek", ExchangePort: 4710},
		razmjena.Beacon{Protocol: Protocol, DeviceID: "laptop-baranja", Name: "Laptop Baranja", ExchangePort: 4711},
		razmjena.Beacon{Protocol: Protocol, DeviceID: "ured-vukovar", Name: "Ured Vukovar", ExchangePort: 4712},
	)
	nadjeni, err := s.Discover(ctxRub, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	poznat := map[string]bool{}
	for _, n := range nadjeni {
		poznat[n.DeviceID] = n.Known
		if n.Addr != "127.0.0.1" {
			t.Errorf("%s: adresa %q nije ona s koje je odgovor stigao", n.DeviceID, n.Addr)
		}
	}
	if len(nadjeni) != 2 || !poznat["laptop-baranja"] || poznat["ured-vukovar"] {
		t.Fatalf("pronađeni: %+v", nadjeni)
	}
	laptop, err := s.GetPeer(ctxRub, "laptop-baranja")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(laptop.Addresses, []string{"192.0.2.10:4710"}) || laptop.LastSeen == nil {
		t.Errorf("zapis poznatog čvora: adrese %v, viđen %v", laptop.Addresses, laptop.LastSeen)
	}
	if n := verzijeZapisaCvorova(t, s); n != verzija {
		t.Errorf("pronalaženje je napisalo %d novih verzija zapisa čvorova", n-verzija)
	}
	if p, err := s.GetPeer(ctxRub, "ured-vukovar"); p != nil || err != nil {
		t.Errorf("nepoznati čvor je upisan samo zato što se javio: %+v %v", p, err)
	}
	tvrdnja := kandidat{"127.0.0.1:4711", razmjena.PutLAN}
	if got := s.tvrdnjeZa("laptop-baranja"); !slices.Equal(got, []kandidat{tvrdnja}) {
		t.Errorf("tvrdnja o adresi poznatog čvora: %v", got)
	}
	if got := s.tvrdnjeZa("ured-vukovar"); got != nil {
		t.Errorf("tvrdnja o nepoznatom čvoru: %v", got)
	}

	// bez pronalaženja, da adresa ne dođe i iz lokalne mreže
	s.ports.Discovery = 0
	if got := s.adreseZaSpajanje(ctxRub, laptop); !slices.Equal(got, []kandidat{tvrdnja, {"192.0.2.10:4710", razmjena.PutAdresa}}) {
		t.Errorf("adrese za spajanje: %v", got)
	}
	if got := s.adreseZaSpajanje(ctxRub, laptop); len(got) != 2 {
		t.Errorf("tvrdnja je nestala prije nego što je nazvana: %v", got)
	}
	s.dial = func(_ context.Context, adresa string) (net.Conn, error) {
		return nil, errors.New("dial tcp " + adresa + ": connect: connection refused")
	}
	prekinuta, prekini := context.WithCancel(ctxRub)
	prekini()
	if _, err := s.nazovi(prekinuta, laptop, tvrdnja, nil, rokOstalih); err == nil {
		t.Fatal("poziv u prekinutoj razmjeni")
	}
	if got := s.tvrdnjeZa("laptop-baranja"); !slices.Equal(got, []kandidat{tvrdnja}) {
		t.Errorf("prekinuta razmjena potrošila je tvrdnju: %v", got)
	}
	if _, err := s.nazovi(ctxRub, laptop, tvrdnja, nil, rokOstalih); err == nil {
		t.Fatal("poziv na adresu na kojoj nitko ne sluša")
	}
	if got := s.adreseZaSpajanje(ctxRub, laptop); !slices.Equal(got, []kandidat{{"192.0.2.10:4710", razmjena.PutAdresa}}) {
		t.Errorf("nakon poziva tvrdnje: %v", got)
	}
}

// Kad se lokalna mreža ne da probati, pronalaženje javlja grešku. Tailscale
// adresa na kojoj se poznati čvor javi nije tvrdnja, a čvor je ipak viđen.
func TestPronalazenjeBezTvrdnjeOTailscaleAdresi(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "laptop-baranja", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	s.pronadji = func(string, time.Duration, int) ([]razmjena.Found, error) {
		return nil, errors.New("listen udp4 :0: address already in use")
	}
	if _, err := s.Discover(ctxRub, time.Second); err == nil {
		t.Error("pronalaženje bez lokalne mreže prošlo je bez greške")
	}
	var proba time.Duration
	s.pronadji = func(_ string, rok time.Duration, _ int) ([]razmjena.Found, error) {
		proba = rok
		return []razmjena.Found{{Beacon: razmjena.Beacon{DeviceID: "laptop-baranja", ExchangePort: 4710}, Addr: "100.101.0.9"}}, nil
	}
	nadjeni, err := s.Discover(ctxRub, 2*time.Second)
	if err != nil || len(nadjeni) != 1 || !nadjeni[0].Known || proba != 2*time.Second {
		t.Fatalf("pronađeni: %+v %v, rok probe %v", nadjeni, err, proba)
	}
	if got := s.tvrdnjeZa("laptop-baranja"); got != nil {
		t.Errorf("Tailscale adresa iz pronalaženja je tvrdnja: %v", got)
	}
	if p, err := s.GetPeer(ctxRub, "laptop-baranja"); err != nil || p == nil || p.LastSeen == nil {
		t.Errorf("čvor nije viđen: %+v %v", p, err)
	}
}

// Čvor koji se u lokalnoj mreži javi s Tailscale adrese nema adresu iz
// lokalne mreže: Tailscale adrese razmjena zove samo kad ih je čvor sam
// javio, i to dok je Tailscale u razmjeni uključen. Kontrola: obična adresa
// iz lokalne mreže zove se prva, s portom koji je čvor javio.
func TestLokalnaMrezaBezTailscaleAdrese(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	s.ports.Discovery = 4712
	p := &Peer{NodeID: "laptop-baranja", Addresses: []string{"192.0.2.10:4710"}}
	izZapisa := kandidat{"192.0.2.10:4710", razmjena.PutAdresa}
	for _, sl := range []struct {
		javljena  string
		ocekivano []kandidat
	}{
		{"100.101.0.9", []kandidat{izZapisa}},
		{"fd7a:115c:a1e0::9", []kandidat{izZapisa}},
		{"192.168.24.70", []kandidat{{"192.168.24.70:4711", razmjena.PutLAN}, izZapisa}},
	} {
		s.lan.zaboravi()
		s.trazi = func(protokol string, _ time.Duration, port int, trazeni []string) ([]razmjena.Found, error) {
			if protokol != Protocol || port != 4712 || !slices.Contains(trazeni, "laptop-baranja") {
				return nil, nil
			}
			return []razmjena.Found{{Beacon: razmjena.Beacon{Protocol: protokol, DeviceID: "laptop-baranja", ExchangePort: 4711}, Addr: sl.javljena}}, nil
		}
		if got := s.adreseZaSpajanje(ctxRub, p); !slices.Equal(got, sl.ocekivano) {
			t.Errorf("javio se s %s: %v", sl.javljena, got)
		}
	}
}

// Čekanje na uparivanje: dok traje, drugo se ne pokreće; prekid ga gasi,
// a drugi prekid ne radi ništa.
func TestCekanjeNaUparivanje(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ports.Pair = ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if err := s.StartListening(); err != nil {
		t.Fatal(err)
	}
	if !s.PairStatus().Waiting {
		t.Error("čvor ne čeka nakon pokretanja")
	}
	if err := s.StartListening(); err == nil {
		t.Error("drugo čekanje pokrenuto je dok prvo traje")
	}
	s.StopListening()
	if st := s.PairStatus(); st.Waiting || st.Error != "" {
		t.Errorf("nakon prekida: %+v", st)
	}
	s.StopListening()
	if s.PairStatus().Waiting {
		t.Error("drugi prekid je pokrenuo čekanje")
	}
}

// Ovo računalo ne zaboravlja samo sebe: vlastiti zapis s adresom za
// razmjenu ostaje, i u knjizi nema arhivirane verzije
func TestCvorNeZaboravljaSamSebe(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if _, err := s.PublicAddress(ctxRub, "cop-osijek", "cop.primjer.hr", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetPeer(ctxRub, "cop-osijek"); err == nil {
		t.Fatal("ovo računalo zaboravilo je samo sebe")
	}
	if p, err := s.SelfPeer(ctxRub); err != nil || len(p.Addresses) != 1 {
		t.Errorf("vlastiti zapis nakon odbijenog zaborava: %+v %v", p, err)
	}
	var arhivirano int
	if err := s.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = 'cop-osijek' AND archived = 1`,
		EntityPeers).Scan(&arhivirano); err != nil || arhivirano != 0 {
		t.Errorf("arhiviranih verzija vlastitog zapisa %d (%v)", arhivirano, err)
	}
}
