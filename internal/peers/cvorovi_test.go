package peers

// Poznati čvorovi: zaborav, port razmjene za adresu bez porta, pronalaženje
// na lokalnoj mreži i čekanje na uparivanje. Druge čvorove na mreži glumi
// lažni glasnik na loopbacku.

import (
	"encoding/json"
	"net"
	"slices"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// Zaboravljeni čvor nestaje s popisa, a u knjizi ostaje arhivirana verzija,
// pa zaborav putuje i drugima. Nepoznat čvor se ne može zaboraviti.
func TestZaboravCvora(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetPeer(ctxRub, "laptop-baranja"); err != nil {
		t.Fatal(err)
	}
	if p, err := s.GetPeer(ctxRub, "laptop-baranja"); p != nil || err != nil {
		t.Errorf("zaboravljeni čvor je još tu: %+v %v", p, err)
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

// Kad brisanje ili arhiviranje ne uspije, čvor ostaje na popisu; kad se
// popis ne da pročitati, ne briše se ništa.
func TestZaboravKadBazaNeSuradjuje(t *testing.T) {
	for ime, kvar := range map[string]func(t *testing.T, s *Service){
		"brisanje padne":    func(t *testing.T, s *Service) { kvarUpisa(t, s, "DELETE", "peers", "") },
		"arhiviranje padne": func(t *testing.T, s *Service) { kvarUpisa(t, s, "INSERT", "record_versions", "NEW.archived = 1") },
		"popis se ne čita":  func(t *testing.T, s *Service) { pokvariTablicu(t, s, "peers") },
	} {
		t.Run(ime, func(t *testing.T) {
			s := rubniCvor(t, "cop-osijek")
			if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "laptop-baranja", PublicKey: javniKljuc(t)}); err != nil {
				t.Fatal(err)
			}
			kvar(t, s)
			if err := s.ForgetPeer(ctxRub, "laptop-baranja"); err == nil {
				t.Fatal("zaborav je prošao")
			}
			if ime == "popis se ne čita" {
				return
			}
			if p, _ := s.GetPeer(ctxRub, "laptop-baranja"); p == nil {
				t.Error("čvor je nestao iako zaborav nije uspio")
			}
		})
	}
}

// Adresa bez porta: za ovaj čvor vrijedi njegov port razmjene, za tuđi
// port iz neke od poznatih adresa, a inače zadani.
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
	} {
		if got := s.exchangePortOf(p.cvor); got != p.port {
			t.Errorf("%s: port %s, očekivan %s", p.ime, got, p.port)
		}
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

// Pronalaženje ne vraća ovaj čvor, za poznati čvor kaže da je poznat i
// osvježi mu adresu (laptop se preselio), a nepoznati samo navede.
func TestPronalazenjeOsvjezavaPoznateCvorove(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "laptop-baranja", PublicKey: javniKljuc(t),
		Addresses: []string{"192.0.2.10:4710"}}); err != nil {
		t.Fatal(err)
	}
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
	if !slices.Equal(laptop.Addresses, []string{"127.0.0.1:4711", "192.0.2.10:4710"}) || laptop.LastSeen == nil {
		t.Errorf("poznatom čvoru adresa nije osvježena: %v, viđen %v", laptop.Addresses, laptop.LastSeen)
	}
	if p, _ := s.GetPeer(ctxRub, "ured-vukovar"); p != nil {
		t.Error("nepoznati čvor je upisan samo zato što se javio")
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
