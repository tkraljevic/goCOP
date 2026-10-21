package peers

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// nemaTailscaleaUKnjizi provjerava da nijedna verzija zapisa čvora u knjizi
// ni zapis na računalu nema Tailscale adresu
func nemaTailscaleaUKnjizi(t *testing.T, s *Service) {
	t.Helper()
	rows, err := s.db.Query(`SELECT entity_id, payload FROM record_versions WHERE entity = ?`, EntityPeers)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			t.Fatal(err)
		}
		var p Peer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatal(err)
		}
		for _, a := range p.Addresses {
			if jeTailscaleAdresa(a) {
				t.Errorf("%s: u knjizi zapisa čvora %s je Tailscale adresa %s", s.node.ID, id, a)
			}
		}
		if strings.Contains(payload, "100.101.") || strings.Contains(payload, "fd7a:") {
			t.Errorf("%s: verzija zapisa čvora %s spominje Tailscale: %s", s.node.ID, id, payload)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var adrese []string
	for _, upit := range []string{`SELECT addresses FROM peers`} {
		r, err := s.db.Query(upit)
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			var a string
			if err := r.Scan(&a); err != nil {
				t.Fatal(err)
			}
			adrese = append(adrese, a)
		}
		r.Close()
	}
	for _, a := range adrese {
		if strings.Contains(a, "100.101.") || strings.Contains(a, "fd7a:") {
			t.Errorf("%s: zapis čvora na računalu ima Tailscale adresu: %s", s.node.ID, a)
		}
	}
}

func adresa(a string) netip.Addr { return netip.MustParseAddr(a) }

func TestJeTailscaleAdresa(t *testing.T) {
	for a, ocekivano := range map[string]bool{
		"100.101.102.103":                      true,
		"100.101.102.103:4710":                 true,
		"[fd7a:115c:a1e0::5]:4710":             true,
		"fd7a:115c:a1e0::5":                    true,
		"[fd7a:115c:a1e0::5%utun4]:1":          true,
		"https://100.112.136.99":               true,
		"https://100.112.136.99:8443":          true,
		"wss://[fd7a:115c:a1e0::9]":            true,
		"cop-osijek-ured.tail1a2b.ts.net:4710": true,
		"https://Ured.Tail1A2B.TS.NET":         true,
		" 100.64.0.1 ":                         true,
		"192.168.24.70:4710":                   false,
		"192.168.1.175":                        false,
		"100.128.0.1:4710":                     false,
		"https://cop.primjer.hr":               false,
		"cop.primjer.hr":                       false,
		"cop.primjer.hr:4710":                  false,
		"[fd00::1]:4710":                       false,
		"":                                     false,
	} {
		if jeTailscaleAdresa(a) != ocekivano {
			t.Errorf("%q: Tailscale %v, očekivano %v", a, !ocekivano, ocekivano)
		}
	}
}

// Primljene adrese: samo Tailscale IP s portom, bez ponavljanja, najviše osam
func TestSamoTailscale(t *testing.T) {
	ulaz := []string{"100.101.0.1:4710", "100.101.0.1:4710", "192.168.1.2:4710", "100.101.0.2", "100.101.0.3:0",
		"nije adresa", " [fd7a:115c:a1e0::1]:4710 "}
	for i := 10; i < 20; i++ {
		ulaz = append(ulaz, fmt.Sprintf("100.101.0.%d:4710", i))
	}
	got := samoTailscale(ulaz)
	if len(got) != najviseTailscaleAdresa || got[0] != "100.101.0.1:4710" || got[1] != "[fd7a:115c:a1e0::1]:4710" || got[2] != "100.101.0.10:4710" {
		t.Errorf("primljene adrese: %v", got)
	}
	if got := samoTailscale(nil); got == nil || len(got) != 0 {
		t.Errorf("bez adresa: %#v (prazan popis, ne nil, da se u bazu upiše [])", got)
	}
}

// Zapis čvora putuje knjigom svima: Tailscale adrese ne ulaze ni u zapis
// na računalu ni u verziju, odakle god stigle (viđena adresa, uparivanje,
// potvrda, ručni unos kroz HandleSetBootstrap)
func TestTailscaleAdreseNeIduUZapisCvora(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	p := Peer{NodeID: "ured-vukovar", Name: "Ured Vukovar", PublicKey: javniKljuc(t), Addresses: []string{
		"192.168.24.70:4710", "100.101.102.103:4710", "100.101.102.103", "[fd7a:115c:a1e0::5]:4710",
		"https://100.112.136.99", "ured.tail1a2b.ts.net:4710", "https://cop.primjer.hr"}}
	if err := s.SavePeer(ctxRub, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPeer(ctxRub, "ured-vukovar")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Addresses, []string{"192.168.24.70:4710", "https://cop.primjer.hr"}) {
		t.Errorf("adrese čvora: %v", got.Addresses)
	}
	nemaTailscaleaUKnjizi(t, s)

	// Zapis koji je Tailscale adresu dobio od starijeg programa (primjena
	// tuđe verzije) ne pokazuje je, a prvo spremanje je izbaci iz knjige
	if _, err := s.db.Exec(`UPDATE peers SET addresses = '["192.168.24.70:4710","100.101.102.103:4710"]' WHERE node_id = 'ured-vukovar'`); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPeer(ctxRub, "ured-vukovar")
	if !slices.Equal(got.Addresses, []string{"192.168.24.70:4710"}) {
		t.Errorf("stara Tailscale adresa se vidi: %v", got.Addresses)
	}
	if svi, _ := s.ListPeers(ctxRub); len(svi) != 1 || len(svi[0].Addresses) != 1 {
		t.Errorf("popis čvorova: %+v", svi)
	}
	got.Addresses = append(got.Addresses, "192.168.24.71:4710")
	if err := s.SavePeer(ctxRub, *got); err != nil {
		t.Fatal(err)
	}
	var zapis string
	if err := s.db.QueryRow(`SELECT addresses FROM peers WHERE node_id = 'ured-vukovar'`).Scan(&zapis); err != nil {
		t.Fatal(err)
	}
	if zapis != `["192.168.24.70:4710","192.168.24.71:4710"]` {
		t.Errorf("zapis nakon spremanja: %s", zapis)
	}
	nemaTailscaleaUKnjizi(t, s)
}

// Čovjek ne može upisati Tailscale adresu među adrese čvora; ostale adrese
// upisuje kao i prije
func TestRucnaTailscaleAdresaSeOdbija(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	s.ports.Exchange = 4799
	if err := s.SavePeer(ctxRub, Peer{NodeID: "ured-vukovar", Name: "Ured Vukovar", PublicKey: javniKljuc(t), Addresses: []string{"192.168.24.70:4720"}}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"100.112.136.99", "100.112.136.99:4710", "https://100.112.136.99", "fd7a:115c:a1e0::5", "ured.tail1a2b.ts.net"} {
		if _, err := s.PublicAddress(ctxRub, "ured-vukovar", a, ""); err == nil || !strings.Contains(err.Error(), "Tailscale adresa se ne upisuje") {
			t.Errorf("%s: %v", a, err)
		}
	}
	for _, a := range []string{"https://", "cop primjer.hr"} {
		if _, err := s.PublicAddress(ctxRub, "ured-vukovar", a, ""); err == nil || strings.Contains(err.Error(), "Tailscale") {
			t.Errorf("neispravna adresa %q: %v", a, err)
		}
	}
	if _, err := s.PublicAddress(ctxRub, "ured-vukovar", "cop-vukovar.example.hr", ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.PublicAddress(ctxRub, "ured-vukovar", "HTTPS://Cop-Vukovar.example.hr/razmjena", "192.168.24.70")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Addresses, []string{"cop-vukovar.example.hr:4720", "https://cop-vukovar.example.hr"}) || !p.IsBootstrap {
		t.Errorf("adrese: %v, izložen %v", p.Addresses, p.IsBootstrap)
	}
	self, err := s.PublicAddress(ctxRub, "cop-osijek", "cop-osijek.example.hr", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(self.Addresses, []string{"cop-osijek.example.hr:4799"}) {
		t.Errorf("vlastita adresa: %v", self.Addresses)
	}
	if _, err := s.PublicAddress(ctxRub, "nepoznati-cvor", "10.0.0.5", ""); err == nil {
		t.Error("adresa nepoznatog čvora je upisana")
	}
	nemaTailscaleaUKnjizi(t, s)
}

// Dva čvora s Tailscaleom javljaju jedan drugome svoje Tailscale adrese
// samo u razgovoru preko Tailscalea. Razgovor u lokalnoj mreži ih ne nosi,
// a onaj preko Tailscalea donese ih u oba smjera. Svaki ih pamti samo kod
// sebe; knjiga ih ne vidi, a spajanje ih zove iza izravne adrese.
func TestTailscaleAdreseSamoPrekoTailscalea(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	izmisljeniTailnet(t, a, b)
	b.ZadajTailscale(adresa("100.101.0.2"), adresa("fd7a:115c:a1e0::2"))

	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	cekajDa(t, "B zabilježi razgovor u lokalnoj mreži", func() bool {
		st, _ := b.syncStates(ctxRub)
		return st[a.node.ID].LastOK != nil
	})
	if got, got2 := a.tailscaleOd(ctxRub, b.node.ID), b.tailscaleOd(ctxRub, a.node.ID); len(got) != 0 || len(got2) != 0 {
		t.Fatalf("razgovor u lokalnoj mreži donio je Tailscale adrese: A zna %v, B zna %v", got, got2)
	}

	// A zna jednu B-ovu Tailscale adresu (npr. iz uparivanja), a lokalne
	// adrese više nema: razgovor ide preko Tailscalea
	b4 := fmt.Sprintf("100.101.0.2:%d", b.ports.Exchange)
	a.upisiTailscale(ctxRub, b.node.ID, []string{b4})
	upoznaj(t, a, b)
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	odB := []string{b4, fmt.Sprintf("[fd7a:115c:a1e0::2]:%d", b.ports.Exchange)}
	if got := a.tailscaleOd(ctxRub, b.node.ID); !slices.Equal(got, odB) {
		t.Errorf("A zna B-ove Tailscale adrese %v, očekivano %v", got, odB)
	}
	odA := []string{fmt.Sprintf("100.101.0.1:%d", a.ports.Exchange)}
	cekajDa(t, "B zapamti A-ove Tailscale adrese", func() bool { return slices.Equal(b.tailscaleOd(ctxRub, a.node.ID), odA) })

	upoznaj(t, a, b, adresaNaPetlji(b))
	peerB, _ := a.GetPeer(ctxRub, b.node.ID)
	if got := a.adreseZaSpajanje(ctxRub, peerB); !slices.Equal(got, []kandidat{{adresaNaPetlji(b), razmjena.PutAdresa}, {odB[0], razmjena.PutTailscale}, {odB[1], razmjena.PutTailscale}}) {
		t.Errorf("redoslijed spajanja: %v", got)
	}
	if st, err := a.Status(ctxRub, false); err != nil || len(st.Peers) != 1 || !slices.Equal(st.Peers[0].State.Tailscale, odB) {
		t.Errorf("ploča razmjene ne pokazuje Tailscale adrese: %+v %v", st, err)
	}
	nemaTailscaleaUKnjizi(t, a)
	nemaTailscaleaUKnjizi(t, b)

	// razgovor u lokalnoj mreži zapamćeno ne dira
	if _, _, err := b.SyncWith(ctxRub, a.node.ID); err != nil {
		t.Fatal(err)
	}
	if got := b.tailscaleOd(ctxRub, a.node.ID); !slices.Equal(got, odA) {
		t.Errorf("nakon razgovora u lokalnoj mreži B zna %v", got)
	}
}

// Razgovor ide preko Tailscalea samo kad je druga strana na Tailscale
// adresi, a veza na Tailscale adresi ovog računala. Čvor koji kroz
// Tailscale dođe do računala u uredskoj mreži (usmjerivač podmreže) nije
// na Tailscale adresi, pa ne dobiva ništa.
func TestPrekoTailscalea(t *testing.T) {
	s := &Service{}
	s.ZadajTailscale(adresa("100.101.0.1"), adresa("fd7a:115c:a1e0::1"))
	for _, k := range []struct {
		ova, druga string
		ocekivano  bool
	}{
		{"100.101.0.1:50000", "100.101.0.2:4710", true},
		{"[fd7a:115c:a1e0::1]:50000", "[fd7a:115c:a1e0::2]:4710", true},
		{"[::ffff:100.101.0.1]:4710", "[::ffff:100.101.0.2]:50000", true},
		{"100.101.0.1:50000", "192.168.24.80:4710", false}, // usmjerivač podmreže
		{"192.168.1.20:50000", "100.101.0.2:4710", false},  // tuđa adresa iz raspona, ne preko Tailscalea
		{"100.101.0.3:50000", "100.101.0.2:4710", false},   // nije adresa ovog računala
		{"127.0.0.1:50000", "127.0.0.1:4710", false},
	} {
		if got := s.prekoTailscalea(tcpAdresa(t, k.ova), tcpAdresa(t, k.druga)); got != k.ocekivano {
			t.Errorf("%s → %s: %v, očekivano %v", k.ova, k.druga, got, k.ocekivano)
		}
	}
	tunel := &net.UnixAddr{Name: "/razmjena/tunel", Net: "unix"}
	if s.prekoTailscalea(tunel, tunel) || s.prekoTailscalea(tunel, tcpAdresa(t, "100.101.0.2:4710")) {
		t.Error("razgovor kroz tunel nije preko Tailscalea")
	}
	s.PostaviTailscale(false)
	if s.prekoTailscalea(tcpAdresa(t, "100.101.0.1:50000"), tcpAdresa(t, "100.101.0.2:4710")) {
		t.Error("isključen Tailscale")
	}
}

// Dva čvora koja se upare preko Tailscale adrese (npr. novi čvor kod kuće
// i uredski) pamte tu adresu samo kod sebe i razmjenjuju preko nje; u
// zapisu čvora je nema
func TestUparivanjePrekoTailscalea(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	for _, par := range [][2]*Service{{a, b}, {b, a}} {
		if err := par[0].ForgetPeer(ctxRub, par[1].node.ID); err != nil {
			t.Fatal(err)
		}
	}
	izmisljeniTailnet(t, a, b)
	upari(t, a, b, "100.101.0.2")

	peerB, err := a.GetPeer(ctxRub, b.node.ID)
	if err != nil || peerB == nil || len(peerB.Addresses) != 0 {
		t.Fatalf("zapis čvora nakon uparivanja preko Tailscalea: %+v %v", peerB, err)
	}
	b4 := fmt.Sprintf("100.101.0.2:%d", b.ports.Exchange)
	if got := a.tailscaleOd(ctxRub, b.node.ID); !slices.Equal(got, []string{b4}) {
		t.Errorf("A pamti %v, očekivano %v", got, b4)
	}
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatalf("razmjena nakon uparivanja preko Tailscalea: %v", err)
	}
	cekajDa(t, "B zapamti A-ove Tailscale adrese", func() bool { return len(b.tailscaleOd(ctxRub, a.node.ID)) == 1 })
	nemaTailscaleaUKnjizi(t, a)
	nemaTailscaleaUKnjizi(t, b)
}

// Uparivanje pamti samo Tailscale adresu, i samo na čvoru koji ima
// Tailscale; bez porta razmjene vrijedi zadani
func TestZapamtiUparivanje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	s.zapamtiUparivanje(ctxRub, "ured-vukovar", "100.101.0.2", "4720")
	if got := s.tailscaleOd(ctxRub, "ured-vukovar"); len(got) != 0 {
		t.Errorf("čvor bez Tailscalea zapamtio je %v", got)
	}
	s.ZadajTailscale(adresa("100.101.0.1"))
	for _, host := range []string{"192.168.24.70", "", "nije-adresa"} {
		s.zapamtiUparivanje(ctxRub, "ured-vukovar", host, "4720")
	}
	if got := s.tailscaleOd(ctxRub, "ured-vukovar"); len(got) != 0 {
		t.Errorf("zapamćena adresa koja nije Tailscale: %v", got)
	}
	s.zapamtiUparivanje(ctxRub, "ured-vukovar", "100.101.0.2", "4720")
	s.zapamtiUparivanje(ctxRub, "ured-vukovar", "fd7a:115c:a1e0::2", "")
	if got, ocekivano := s.tailscaleOd(ctxRub, "ured-vukovar"), []string{fmt.Sprintf("[fd7a:115c:a1e0::2]:%d", DefaultExchangePort), "100.101.0.2:4720"}; !slices.Equal(got, ocekivano) {
		t.Errorf("zapamćeno %v, očekivano %v", got, ocekivano)
	}
}

// Zaboravljen čvor ostaje u knjizi kao spomenik, koji putuje svima. U njemu
// nema bilješke o razmjeni, pa ni adrese iz poruke o grešci (npr. Tailscale
// adrese koja nije odgovorila).
func TestZaboravljenCvorBezBiljeskeORazmjeni(t *testing.T) {
	c := clanovi(t, "ured-vukovar", "laptop-baranja", "ured-firme")
	ured, laptop, firma := c[0], c[1], c[2]
	izmisljeniTailnet(t, ured)
	upoznaj(t, ured, laptop)
	ured.upisiTailscale(ctxRub, laptop.node.ID, []string{"100.101.0.9:1"})
	if _, _, err := ured.SyncWith(ctxRub, laptop.node.ID); err == nil {
		t.Fatal("razmjena s laptopom koji ne odgovara")
	}
	if p, _ := ured.GetPeer(ctxRub, laptop.node.ID); p == nil || !strings.Contains(p.LastSyncNote, "100.101.0.9") {
		t.Fatalf("bilješka o grešci: %+v", p)
	}
	if err := ured.ForgetPeer(ctxRub, laptop.node.ID); err != nil {
		t.Fatal(err)
	}
	nemaTailscaleaUKnjizi(t, ured)
	if _, _, err := ured.SyncWith(ctxRub, firma.node.ID); err != nil {
		t.Fatal(err)
	}
	nemaTailscaleaUKnjizi(t, firma)
	var spomenik string
	if err := firma.db.QueryRow(`SELECT payload FROM record_versions WHERE entity = ? AND entity_id = ? AND archived = 1`, EntityPeers, laptop.node.ID).Scan(&spomenik); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spomenik, "last_sync_note") || strings.Contains(spomenik, "last_seen") {
		t.Errorf("spomenik nosi bilješku o razmjeni: %s", spomenik)
	}
}

// Veza koja stigne s Tailscale adrese ne upisuje tu adresu u zapis čvora:
// viđena adresa ide u knjigu samo kad nije Tailscale
func TestVidjenaTailscaleAdresaNeIdeUKnjigu(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	a.ZadajTailscale(adresa("100.101.0.1"))
	b.ZadajTailscale(adresa("100.101.0.2"))
	prerusena := prerusenaRazmjena(t, b, tcpAdresa(t, "100.101.0.2:4710"), tcpAdresa(t, "100.101.0.1:51234"))
	upoznaj(t, a, b, prerusena)
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	cekajDa(t, "B zabilježi razmjenu", func() bool {
		p, _ := b.GetPeer(ctxRub, a.node.ID)
		return p != nil && p.LastSync != nil
	})
	p, _ := b.GetPeer(ctxRub, a.node.ID)
	if !slices.Equal(p.Addresses, []string{adresaNaPetlji(a)}) {
		t.Errorf("B je upisao viđenu adresu: %v", p.Addresses)
	}
	nemaTailscaleaUKnjizi(t, b)
}

// Redoslijed spajanja: adresa iz lokalne mreže, tvrdnje o adresama
// (pronalaženje, dolazna razmjena), izravne adrese iz zapisa, naučene
// adrese, Tailscale adrese koje je čvor sam javio, tunel, svaka sa svojim
// putem. Stara Tailscale adresa iz zapisa se ne zove.
func TestRedoslijedAdresaZaSpajanje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	p := Peer{NodeID: "ured-vukovar", Addresses: []string{"https://cop.primjer.hr", "192.168.24.70:4710", "100.101.0.9:4710", "cop-vukovar.example.hr:4710"}}
	s.upisiTailscale(ctxRub, "ured-vukovar", []string{"100.101.0.2:4710"})
	izravne := []kandidat{{"192.168.24.70:4710", razmjena.PutAdresa}, {"cop-vukovar.example.hr:4710", razmjena.PutAdresa}}
	tunel := kandidat{"https://cop.primjer.hr", razmjena.PutKrozTunel}
	if got := s.adreseZaSpajanje(ctxRub, &p); !slices.Equal(got, append(slices.Clone(izravne), tunel)) {
		t.Errorf("bez Tailscalea: %v", got)
	}
	s.ZadajTailscale(adresa("100.101.0.1"))
	tailscale := kandidat{"100.101.0.2:4710", razmjena.PutTailscale}
	if got := s.adreseZaSpajanje(ctxRub, &p); !slices.Equal(got, append(slices.Clone(izravne), tailscale, tunel)) {
		t.Errorf("s Tailscaleom: %v", got)
	}
	// adresa bez porta dobije port koji se za čvor zna, pa se ista adresa
	// s portom ne zove dvaput
	bezPorta := Peer{NodeID: "ured-vukovar", Addresses: []string{"192.0.2.7", " 192.0.2.8 ", "192.0.2.7:4720"}}
	if got := s.adreseZaSpajanje(ctxRub, &bezPorta); !slices.Equal(got, []kandidat{{"192.0.2.7:4720", razmjena.PutAdresa}, {"192.0.2.8:4720", razmjena.PutAdresa}, tailscale}) {
		t.Errorf("adrese bez porta: %v", got)
	}
	s.ports.Discovery = lazniGlasnik(t, razmjena.Beacon{Protocol: Protocol, DeviceID: "ured-vukovar", Name: "Ured Vukovar", ExchangePort: 4711})
	lan := kandidat{"127.0.0.1:4711", razmjena.PutLAN}
	if got := s.adreseZaSpajanje(ctxRub, &p); len(got) != 5 || got[0] != lan {
		t.Errorf("s pronalaženjem u lokalnoj mreži: %v", got)
	}
	// ista adresa u zapisu ne zove se dvaput i ostaje lan; prazna se preskače
	p2 := Peer{NodeID: "ured-vukovar", Addresses: []string{" ", "127.0.0.1:4711 ", "192.168.24.70:4710"}}
	if got := s.adreseZaSpajanje(ctxRub, &p2); !slices.Equal(got, []kandidat{lan, izravne[0], tailscale}) {
		t.Errorf("adresa iz lokalne mreže i u zapisu: %v", got)
	}
	s.ports.Discovery = lazniGlasnik(t)
	if got := s.adreseZaSpajanje(ctxRub, &p); len(got) != 4 {
		t.Errorf("čvor se ne javlja u lokalnoj mreži: %v", got)
	}

	// tvrdnje idu iza adrese iz lokalne mreže, najnovija prva, a naučene
	// adrese iza adresa iz zapisa; naučena adresa koja je i u zapisu ne
	// zove se dvaput
	s.dodajTvrdnju("ured-vukovar", kandidat{"192.168.24.73:4710", razmjena.PutAdresa})
	s.zapamtiTvrdnju(ctxRub, razmjena.Found{Beacon: razmjena.Beacon{DeviceID: "ured-vukovar", ExchangePort: 4711}, Addr: "192.168.24.71"})
	s.upisiNaucenu(ctxRub, "ured-vukovar", "192.168.24.70:4710", time.Now())
	s.upisiNaucenu(ctxRub, "ured-vukovar", "192.168.24.72:4710", time.Now())
	s.ports.Discovery = lazniGlasnik(t, razmjena.Beacon{Protocol: Protocol, DeviceID: "ured-vukovar", Name: "Ured Vukovar", ExchangePort: 4711})
	ocekivano := append([]kandidat{lan, {"192.168.24.71:4711", razmjena.PutLAN}, {"192.168.24.73:4710", razmjena.PutAdresa}}, izravne...)
	ocekivano = append(ocekivano, kandidat{"192.168.24.72:4710", razmjena.PutAdresa}, tailscale, tunel)
	if got := s.adreseZaSpajanje(ctxRub, &p); !slices.Equal(got, ocekivano) {
		t.Errorf("s tvrdnjama i naučenim adresama: %v", got)
	}
}

// Ovaj čvor javlja svoje adrese samo u razgovoru preko Tailscalea i kad
// sam prima razmjene; primljene adrese vrijede samo iz takvog razgovora
func TestTailscaleZaDrugoga(t *testing.T) {
	s := &Service{ports: Ports{Exchange: 4710}}
	s.ZadajTailscale(adresa("100.101.0.1"), adresa("fd7a:115c:a1e0::1"))
	if got := s.tailscaleZaDrugoga(false); got != nil {
		t.Errorf("razgovor nije preko Tailscalea: %v", got)
	}
	if got := s.tailscaleZaDrugoga(true); !slices.Equal(got, []string{"100.101.0.1:4710", "[fd7a:115c:a1e0::1]:4710"}) {
		t.Errorf("razgovor preko Tailscalea: %v", got)
	}
	s.ports.Exchange = 0
	if got := s.tailscaleZaDrugoga(true); got != nil {
		t.Errorf("čvor bez porta razmjene: %v", got)
	}
	if got := primljeniTailscale(false, []string{"100.101.0.2:4710"}); got != nil {
		t.Errorf("primljeno izvan Tailscalea: %v", got)
	}
	if got := primljeniTailscale(true, []string{"100.101.0.2:4710", "192.168.1.2:4710"}); !slices.Equal(got, []string{"100.101.0.2:4710"}) {
		t.Errorf("primljeno preko Tailscalea: %v", got)
	}
}

// Neuspjeli razgovor ne mijenja zapamćene adrese; greška baze ide u dnevnik
func TestZapamceneTailscaleAdreseRubovi(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	s.ZadajTailscale(adresa("100.101.0.1"))
	s.zapamtiTailscale(ctxRub, "ured-vukovar", syncOutcome{tailscale: []string{"100.101.0.2:4710"}})
	s.zapamtiTailscale(ctxRub, "ured-vukovar", syncOutcome{err: fmt.Errorf("veza prekinuta")})
	if got := s.tailscaleOd(ctxRub, "ured-vukovar"); !slices.Equal(got, []string{"100.101.0.2:4710"}) {
		t.Errorf("nakon neuspjelog razgovora: %v", got)
	}
	if got := s.tailscaleOd(ctxRub, "nepoznati-cvor"); got != nil {
		t.Errorf("nepoznat čvor: %v", got)
	}
	dnevnik := zapisDnevnika(t)
	pokvariTablicu(t, s, "peer_sync")
	s.upisiTailscale(ctxRub, "ured-vukovar", nil)
	if !strings.Contains(dnevnik.String(), "Tailscale adrese čvora ured-vukovar nisu spremljene") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
}
