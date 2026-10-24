package peers

// Provjera veze s računalom (provjera_racunala.go)

import (
	"context"
	"errors"
	"net"
	"os"
	"slices"
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
	upoznaj(t, a, b, "192.0.2.7:4710", "192.0.2.8", "192.0.2.9:4710", "192.0.2.10:4710", adresaNaPetlji(treci), adresaNaPetlji(b))

	p, err := a.ProvjeriVezuS(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := []struct {
		adresa string
		razlog RazlogAdrese
		prosla bool
	}{
		{"192.0.2.7:4710", RazlogAdreseOdbijeno, false},
		{"192.0.2.8:4710", RazlogAdreseNemaOdgovor, false},
		{"192.0.2.9:4710", RazlogAdreseNemaPuta, false},
		{"192.0.2.10:4710", "", false},
		{adresaNaPetlji(treci), RazlogAdreseDrugiKljuc, false},
		{adresaNaPetlji(b), "", true},
	}
	if p.NodeID != b.node.ID || p.Naziv == "" || p.Kad.IsZero() || len(p.Adrese) != len(ocekivano) {
		t.Fatalf("provjera: %+v", p)
	}
	for i, o := range ocekivano {
		g := p.Adrese[i]
		if g.Adresa != o.adresa || g.Razlog != o.razlog || g.Prosla() != o.prosla || g.Izvor != IzvorZapis || g.Kada.IsZero() {
			t.Errorf("%d. adresa: %+v, a treba %+v", i+1, g, o)
		}
		if o.prosla && (g.RTT <= 0 || g.Put != razmjena.NazivLAN) || !o.prosla && (g.RTT != 0 || g.Greska == "") {
			t.Errorf("%s: RTT %v, put %q, greška %q", g.Adresa, g.RTT, g.Put, g.Greska)
		}
	}
	if g := p.Adrese[0].Put; g != razmjena.NazivServer {
		t.Errorf("javna adresa iz zapisa: put %q", g)
	}

	cekajDa(t, "drugo računalo broji prekinuto rukovanje", func() bool {
		z, _ := b.Odbijeno(ctxRub, 1)
		return slices.ContainsFunc(z, func(r ZapisOdbijenog) bool { return r.Razlog == string(razmjena.VrataRukovanje) })
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
	p, _ := a.GetPeer(ctxRub, b.node.ID)
	got := a.adreseSIzvorom(ctxRub, p)
	ocekivano := []kandidatIzvora{
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
