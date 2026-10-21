package peers

// Pokušaji spajanja po adresi drugog računala (pokusaji.go)

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// Pokušaj spajanja pamti se za svaku adresu posebno, s vremenom i greškom,
// a uspjeli je bez greške. Pokušaji drugog računala ne miješaju se s ovima.
func TestGreskaPoAdresi(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	greske := map[string]error{
		"192.0.2.7:4710": errors.New("dial tcp 192.0.2.7:4710: connect: connection refused"),
		"192.0.2.8:4710": errors.New("dial tcp 192.0.2.8:4710: i/o timeout"),
	}
	a.dial = func(ctx context.Context, kamo string) (net.Conn, error) {
		if err, ok := greske[kamo]; ok {
			return nil, err
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", kamo)
	}
	a.zapamtiPokusaj("ured-osijek", "192.0.2.7:4710", errors.New("veza odbijena"))
	prije := time.Now()
	upoznaj(t, a, b, "192.0.2.7:4710", "192.0.2.8:4710", adresaNaPetlji(b))
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}

	got := a.pokusajiAdresa(b.node.ID)
	if len(got) != 3 {
		t.Fatalf("pokušaji: %v", got)
	}
	for kamo, err := range greske {
		if got[kamo].Greska != err.Error() {
			t.Errorf("%s: greška %q, očekivana %q", kamo, got[kamo].Greska, err)
		}
	}
	if g := got[adresaNaPetlji(b)].Greska; g != "" {
		t.Errorf("uspjelo spajanje ima grešku %q", g)
	}
	prvi, drugi, treci := got["192.0.2.7:4710"].Kada, got["192.0.2.8:4710"].Kada, got[adresaNaPetlji(b)].Kada
	if prvi.Before(prije) || drugi.Before(prvi) || treci.Before(drugi) {
		t.Errorf("vremena pokušaja: %v, %v, %v (prije %v)", prvi, drugi, treci, prije)
	}
	if tudji := a.pokusajiAdresa("ured-osijek"); len(tudji) != 1 || tudji["192.0.2.7:4710"].Greska != "veza odbijena" {
		t.Errorf("pokušaji drugog računala: %v", tudji)
	}
	if n := len(a.pokusajiAdresa("nepoznato-racunalo")); n != 0 {
		t.Errorf("nepoznato računalo ima %d pokušaja", n)
	}
}

// Za jedno računalo pamti se najviše 16 adresa, najnovije, a ponovljena
// adresa postaje najnovija. Zaboravljeno računalo nema pokušaja, a drugo
// računalo svoje zadrži.
func TestPokusajiNajviseSesnaestPoRacunalu(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	adresaBr := func(i int) string { return fmt.Sprintf("192.0.2.%d:4710", i) }
	for i := 1; i <= 20; i++ {
		s.zapamtiPokusaj("laptop-baranja", adresaBr(i), errors.New("connect: connection refused"))
	}
	got := s.pokusajiAdresa("laptop-baranja")
	if len(got) != najvisePokusaja {
		t.Fatalf("%d pokušaja, očekivano %d", len(got), najvisePokusaja)
	}
	for i := 1; i <= 20; i++ {
		if _, ima := got[adresaBr(i)]; ima != (i > 4) {
			t.Errorf("nakon 20 adresa: %s zapamćena %v", adresaBr(i), ima)
		}
	}
	s.zapamtiPokusaj("laptop-baranja", adresaBr(5), nil)
	s.zapamtiPokusaj("laptop-baranja", adresaBr(21), nil)
	got = s.pokusajiAdresa("laptop-baranja")
	if _, ima := got[adresaBr(6)]; ima || len(got) != najvisePokusaja || got[adresaBr(5)].Greska != "" {
		t.Errorf("ponovljena adresa nije postala najnovija: %v", got)
	}

	s.zapamtiPokusaj("ured-vukovar", "192.0.2.30:4710", nil)
	if err := s.ForgetPeer(ctxRub, "laptop-baranja"); err != nil {
		t.Fatal(err)
	}
	if got := s.pokusajiAdresa("laptop-baranja"); len(got) != 0 {
		t.Errorf("zaboravljeno računalo: %v", got)
	}
	if got := s.pokusajiAdresa("ured-vukovar"); len(got) != 1 {
		t.Errorf("drugo računalo nakon zaborava: %v", got)
	}
}
