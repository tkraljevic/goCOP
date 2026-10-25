package peers

// Provjera veze s računalom (provjera_racunala.go)

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// Svaka poznata adresa računala dobije ishod, istim redom kao razmjena, s
// izvorom, nazivom puta i razlogom. Adresa na kojoj se javlja drugo računalo
// ne prolazi. Provjera ne otvara razmjenu: ni ovo ni drugo računalo ne
// bilježe razmjenu ni pokušaj, a drugo broji samo prekinuto rukovanje.
func TestProvjeraVezeSRacunalom(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar", "ured-osijek")
	a, b, treci := c[0], c[1], c[2]
	opGreska := func(e error) error {
		return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", e)}
	}
	greske := map[string]error{
		"192.0.2.7:4710":  opGreska(syscall.ECONNREFUSED),
		"192.0.2.8:4710":  context.DeadlineExceeded,
		"192.0.2.9:4710":  opGreska(syscall.EHOSTUNREACH),
		"192.0.2.10:4710": errors.New("nešto treće"),
	}
	a.dial = func(ctx context.Context, kamo string) (net.Conn, error) {
		if err, ok := greske[kamo]; ok {
			return nil, err
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", kamo)
	}
	a.tunelKlijent.Dial = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("web poslužitelj ne odgovara")
	}
	// pravi zatvoren port: razlog mora doći iz greške sustava, ne iz testa
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	zatvoren := ln.Addr().String()
	ln.Close()
	a.dodajTvrdnju(b.node.ID, kandidat{zatvoren, razmjena.PutAdresa})
	upoznaj(t, a, b, "192.0.2.7:4710", "192.0.2.8", "192.0.2.9:4710", "192.0.2.10:4710", adresaNaPetlji(treci), adresaNaPetlji(b), "https://cop.primjer.hr")

	p, err := a.ProvjeriVezuS(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := []struct {
		adresa string
		izvor  IzvorAdrese
		razlog RazlogAdrese
		prosla bool
	}{
		{"192.0.2.7:4710", IzvorZapis, RazlogAdreseOdbijeno, false},
		{"192.0.2.8:4710", IzvorZapis, RazlogAdreseNemaOdgovor, false},
		{"192.0.2.9:4710", IzvorZapis, RazlogAdreseNemaPuta, false},
		{"192.0.2.10:4710", IzvorZapis, "", false},
		{adresaNaPetlji(treci), IzvorZapis, RazlogAdreseDrugiKljuc, false},
		{adresaNaPetlji(b), IzvorZapis, "", true},
		{zatvoren, IzvorDolazna, RazlogAdreseOdbijeno, false},
		{"https://cop.primjer.hr", IzvorTunel, "", false},
	}
	if p.NodeID != b.node.ID || p.Naziv == "" || p.Kad.IsZero() || len(p.Adrese) != len(ocekivano) {
		t.Fatalf("provjera: %+v", p)
	}
	for i, o := range ocekivano {
		g := p.Adrese[i]
		if g.Adresa != o.adresa || g.Razlog != o.razlog || g.Prosla() != o.prosla || g.Izvor != o.izvor || g.Kada.IsZero() {
			t.Errorf("%d. adresa: %+v, a treba %+v", i+1, g, o)
		}
		if o.prosla && (g.RTT <= 0 || g.Put != razmjena.NazivLAN) || !o.prosla && (g.RTT != 0 || g.Greska == "") {
			t.Errorf("%s: RTT %v, put %q, greška %q", g.Adresa, g.RTT, g.Put, g.Greska)
		}
	}
	if g := p.Adrese[0].Put; g != razmjena.NazivServer {
		t.Errorf("javna adresa iz zapisa: put %q", g)
	}
	if g := p.Adrese[7]; g.Put != razmjena.NazivServer || !strings.Contains(g.Greska, "tunel https://cop.primjer.hr") {
		t.Errorf("tunel: %+v", g)
	}

	cekajDa(t, "drugo računalo broji prekinuto rukovanje", func() bool {
		z, err := b.Odbijeno(ctxRub, 1)
		return err == nil && slices.ContainsFunc(z, func(r ZapisOdbijenog) bool { return r.Razlog == string(razmjena.VrataRukovanje) })
	})
	for _, s := range []*Service{a, b} {
		st, err := s.syncStates(ctxRub)
		if err != nil {
			t.Fatal(err)
		}
		for id, stanje := range st {
			if stanje.LastOK != nil || stanje.Fails != 0 {
				t.Errorf("%s bilježi razmjenu s %s: %+v", s.node.ID, id, stanje)
			}
		}
	}
	if n := len(a.pokusajiAdresa(b.node.ID)); n != 0 {
		t.Errorf("provjera je zapamćena kao %d pokušaja razmjene", n)
	}
	if !slices.Equal(a.tvrdnjePutem(b.node.ID, razmjena.PutAdresa), []kandidat{{zatvoren, razmjena.PutAdresa}}) {
		t.Error("provjera je potrošila tvrdnju o adresi")
	}
}

// Izvor adrese: javila se u lokalnoj mreži, naučena, dolazna, Tailscale ili
// tunel; dvojnik zadrži prvi izvor, a adreseZaSpajanje isti redoslijed
func TestIzvoriAdresa(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	upoznaj(t, a, b, "192.0.2.20:4710", "https://cop.primjer.hr", "", "192.0.2.21")
	a.upisiNaucenu(ctxRub, b.node.ID, "192.168.1.30:4710", time.Now())
	a.dodajTvrdnju(b.node.ID, kandidat{"192.168.1.31:4710", razmjena.PutAdresa})
	a.dodajTvrdnju(b.node.ID, kandidat{"192.168.1.32:4710", razmjena.PutLAN})
	a.dodajTvrdnju(b.node.ID, kandidat{"192.0.2.20:4710", razmjena.PutAdresa})
	a.ports.Discovery = 4712
	a.lan.zaboravi()
	a.trazi = func(protokol string, _ time.Duration, _ int, _ []string) ([]razmjena.Found, error) {
		return []razmjena.Found{{Beacon: razmjena.Beacon{Protocol: protokol, DeviceID: b.node.ID, ExchangePort: 4711}, Addr: "192.168.1.33"}}, nil
	}
	p, err := a.GetPeer(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := a.adreseSIzvorom(ctxRub, p)
	ocekivano := []kandidatIzvora{
		{kandidat{"192.168.1.33:4711", razmjena.PutLAN}, IzvorLokalnaMreza},
		{kandidat{"192.168.1.32:4710", razmjena.PutLAN}, IzvorLokalnaMreza},
		{kandidat{"192.0.2.20:4710", razmjena.PutAdresa}, IzvorZapis},
		{kandidat{"192.0.2.21:4710", razmjena.PutAdresa}, IzvorZapis},
		{kandidat{"192.168.1.30:4710", razmjena.PutAdresa}, IzvorNaucena},
		{kandidat{"192.168.1.31:4710", razmjena.PutAdresa}, IzvorDolazna},
		{kandidat{"https://cop.primjer.hr", razmjena.PutKrozTunel}, IzvorTunel},
	}
	if !slices.Equal(got, ocekivano) {
		t.Errorf("adrese s izvorom:\n%v\na treba\n%v", got, ocekivano)
	}
	var bez []kandidat
	for _, k := range ocekivano {
		bez = append(bez, k.kandidat)
	}
	if z := a.adreseZaSpajanje(ctxRub, p); !slices.Equal(z, bez) {
		t.Errorf("adrese za spajanje: %v", z)
	}
}

// Računalo koje ovo ne smije zvati provjera ne zove: nepoznato i ono bez
// članstva vraćaju grešku. Računalo bez ijedne adrese daje praznu provjeru.
func TestProvjeraVezeBezRacunala(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	if _, err := a.ProvjeriVezuS(ctxRub, "nepoznato-racunalo"); err == nil {
		t.Error("nepoznato računalo")
	}
	upoznaj(t, a, b)
	p, err := a.ProvjeriVezuS(ctxRub, b.node.ID)
	if err != nil || len(p.Adrese) != 0 {
		t.Errorf("bez adresa: %+v, %v", p, err)
	}
	stranac := rubniCvor(t, "stranac")
	upoznaj(t, a, stranac, adresaNaPetlji(b))
	if _, err := a.ProvjeriVezuS(ctxRub, stranac.node.ID); err == nil {
		t.Error("računalo bez članstva")
	}
}

// Provjera veze zove najviše 8 adresa po putu, kao krug: privatne adrese
// broje se zajedno s LAN-om, a tunel ostaje i kad javnih ima previše.
func TestProvjeraNajviseAdresaPoPutu(t *testing.T) {
	var k []kandidatIzvora
	for i := range 20 {
		k = append(k,
			kandidatIzvora{kandidat: kandidat{adresa: fmt.Sprintf("192.168.1.%d", i+1), put: razmjena.PutAdresa}},
			kandidatIzvora{kandidat: kandidat{adresa: fmt.Sprintf("10.0.0.%d", i+1), put: razmjena.PutLAN}},
			kandidatIzvora{kandidat: kandidat{adresa: fmt.Sprintf("203.0.113.%d", i+1), put: razmjena.PutAdresa}})
	}
	k = append(k, kandidatIzvora{kandidat: kandidat{adresa: "wss://cop.primjer.hr", put: razmjena.PutKrozTunel}})
	broj := map[razmjena.Put]int{}
	for _, x := range ograniciPoPutu(k, putProvjere) {
		broj[putProvjere(x)]++
	}
	if broj[razmjena.PutLAN] != 8 || broj[razmjena.PutAdresa] != 8 || broj[razmjena.PutKrozTunel] != 1 {
		t.Errorf("po putu: %v", broj)
	}
}
