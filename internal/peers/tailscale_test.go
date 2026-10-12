package peers

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"

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
		"https://cop-osijek.com":               false,
		"cop-osijek.com":                       false,
		"cop-osijek.com:4710":                  false,
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
		"https://100.112.136.99", "ured.tail1a2b.ts.net:4710", "https://cop-osijek.com"}}
	if err := s.SavePeer(ctxRub, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPeer(ctxRub, "ured-vukovar")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Addresses, []string{"192.168.24.70:4710", "https://cop-osijek.com"}) {
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
	for _, a := range []string{"https://", "cop osijek.com"} {
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

// Dva čvora s Tailscaleom razmijene svoje Tailscale adrese u razgovoru i
// svaki ih pamti samo kod sebe; knjiga ih ne vidi, a sljedeće spajanje ih
// zove iza izravne adrese
func TestCvoroviSTailscaleomRazmijeneAdreseIzravno(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	a.ZadajTailscale(adresa("100.101.0.1"))
	b.ZadajTailscale(adresa("100.101.0.2"), adresa("fd7a:115c:a1e0::2"))
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	odB := []string{fmt.Sprintf("100.101.0.2:%d", b.ports.Exchange), fmt.Sprintf("[fd7a:115c:a1e0::2]:%d", b.ports.Exchange)}
	if got := a.tailscaleOd(ctxRub, b.node.ID); !slices.Equal(got, odB) {
		t.Errorf("A zna B-ove Tailscale adrese %v, očekivano %v", got, odB)
	}
	odA := []string{fmt.Sprintf("100.101.0.1:%d", a.ports.Exchange)}
	cekajDa(t, "B zapamti A-ove Tailscale adrese", func() bool { return slices.Equal(b.tailscaleOd(ctxRub, a.node.ID), odA) })

	peerB, _ := a.GetPeer(ctxRub, b.node.ID)
	if got := a.adreseZaSpajanje(ctxRub, peerB); !slices.Equal(got, append([]string{adresaNaPetlji(b)}, odB...)) {
		t.Errorf("redoslijed spajanja: %v", got)
	}
	if st, err := a.Status(ctxRub, false); err != nil || len(st.Peers) != 1 || !slices.Equal(st.Peers[0].State.Tailscale, odB) {
		t.Errorf("ploča razmjene ne pokazuje Tailscale adrese: %+v %v", st, err)
	}
	nemaTailscaleaUKnjizi(t, a)
	nemaTailscaleaUKnjizi(t, b)

	// Drugi put B nazove A; A tada više nema Tailscale (isključen ili
	// ugašen), pa B zaboravi njegove adrese
	a.ZadajTailscale()
	if _, _, err := b.SyncWith(ctxRub, a.node.ID); err != nil {
		t.Fatal(err)
	}
	if got := b.tailscaleOd(ctxRub, a.node.ID); len(got) != 0 {
		t.Errorf("B pamti Tailscale adrese čvora koji ga više nema: %v", got)
	}
	if got := a.tailscaleOd(ctxRub, b.node.ID); !slices.Equal(got, odB) {
		t.Errorf("A bez Tailscalea ne prima ništa, a staro ne dira: %v", got)
	}
	if got := a.adreseZaSpajanje(ctxRub, peerB); !slices.Equal(got, []string{adresaNaPetlji(b)}) {
		t.Errorf("A bez Tailscalea zove samo izravnu adresu: %v", got)
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

// Redoslijed spajanja: adresa iz lokalne mreže, izravne adrese, Tailscale
// adrese koje je čvor sam javio, tunel. Stara Tailscale adresa iz zapisa
// se ne zove.
func TestRedoslijedAdresaZaSpajanje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	p := Peer{NodeID: "ured-vukovar", Addresses: []string{"https://cop-osijek.com", "192.168.24.70:4710", "100.101.0.9:4710", "cop-vukovar.example.hr:4710"}}
	s.upisiTailscale(ctxRub, "ured-vukovar", []string{"100.101.0.2:4710"})
	if got := s.adreseZaSpajanje(ctxRub, &p); !slices.Equal(got, []string{"192.168.24.70:4710", "cop-vukovar.example.hr:4710", "https://cop-osijek.com"}) {
		t.Errorf("bez Tailscalea: %v", got)
	}
	s.ZadajTailscale(adresa("100.101.0.1"))
	if got := s.adreseZaSpajanje(ctxRub, &p); !slices.Equal(got, []string{"192.168.24.70:4710", "cop-vukovar.example.hr:4710", "100.101.0.2:4710", "https://cop-osijek.com"}) {
		t.Errorf("s Tailscaleom: %v", got)
	}
	s.ports.Discovery = lazniGlasnik(t, razmjena.Beacon{Protocol: Protocol, DeviceID: "ured-vukovar", Name: "Ured Vukovar", ExchangePort: 4711})
	if got := s.adreseZaSpajanje(ctxRub, &p); len(got) != 5 || got[0] != "127.0.0.1:4711" {
		t.Errorf("s pronalaženjem u lokalnoj mreži: %v", got)
	}
	s.ports.Discovery = lazniGlasnik(t)
	if got := s.adreseZaSpajanje(ctxRub, &p); len(got) != 4 {
		t.Errorf("čvor se ne javlja u lokalnoj mreži: %v", got)
	}
}

// Ovaj čvor javlja svoje adrese samo kad je i drugi javio Tailscale i kad
// sam prima razmjene
func TestTailscaleZaDrugoga(t *testing.T) {
	s := &Service{ports: Ports{Exchange: 4710}}
	moje := []netip.Addr{adresa("100.101.0.1"), adresa("fd7a:115c:a1e0::1")}
	if got := s.tailscaleZaDrugoga(moje, frontierMsg{}); got != nil {
		t.Errorf("drugi nema Tailscale: %v", got)
	}
	if got := s.tailscaleZaDrugoga(moje, frontierMsg{Tailscale: true}); !slices.Equal(got, []string{"100.101.0.1:4710", "[fd7a:115c:a1e0::1]:4710"}) {
		t.Errorf("oba imaju Tailscale: %v", got)
	}
	s.ports.Exchange = 0
	if got := s.tailscaleZaDrugoga(moje, frontierMsg{Tailscale: true}); got != nil {
		t.Errorf("čvor bez porta razmjene: %v", got)
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
