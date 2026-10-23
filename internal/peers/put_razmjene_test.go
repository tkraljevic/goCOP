package peers

// Put zadnje razmjene (put_razmjene.go): obje strane upisuju put, tko je
// zvao, RTT i bajtove; brojači kreću ispočetka kad se promijeni naziv puta;
// razlog kaže da se računalo ne javlja u lokalnoj mreži, a zaborav
// računala sve to briše.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// putIz je put razmjene s računalom kako ga vidi Status
func putIz(t *testing.T, s *Service, nodeID string) PutRazmjene {
	t.Helper()
	st, err := s.syncStates(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	return st[nodeID].Put
}

// Prava razmjena na loopbacku: pozivatelj upiše svoj put, RTT i bajtove,
// a pozvani isti naziv puta, bez RTT-a, i da je zvalo drugo računalo.
func TestPutRazmjeneObjeStrane(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	p := putIz(t, a, b.node.ID)
	if p.Put != razmjena.PutAdresa || !p.Privatna || p.Naziv != razmjena.NazivLAN || p.RTTMs < 1 || p.Od == nil ||
		p.Pokusaji != 1 || p.Uspjesi != 1 || p.Neuspjesi != 0 || p.Primljeno == 0 || p.Poslano == 0 || p.ZadnjiZvao != "ovaj" || p.Razlog != "" {
		t.Errorf("pozivatelj: %+v", p)
	}
	cekajDa(t, "put kod pozvanog", func() bool { return putIz(t, b, a.node.ID).ZadnjiZvao == "drugi" })
	if q := putIz(t, b, a.node.ID); q.Naziv != razmjena.NazivLAN || q.RTTMs != 0 || q.Uspjesi != 1 || q.Primljeno == 0 || q.Poslano == 0 {
		t.Errorf("pozvani: %+v", q)
	}
}

// Kad adresa u lokalnoj mreži ne odgovori, a razmjena prođe na idućoj,
// razlog kaže da se računalo ne javlja u lokalnoj mreži. Kad ne odgovori
// nijedna, put ostaje prijašnji, a brojači broje neuspjeh. Pokušaji po
// adresi idu od najnovijeg, a zaborav računala briše put i brojače.
func TestPutRazmjeneRazlogIZaborav(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	upoznaj(t, a, b, "192.168.1.250:4710", adresaNaPetlji(b))
	nema := false
	a.dial = func(ctx context.Context, kamo string) (net.Conn, error) {
		if nema || kamo == "192.168.1.250:4710" {
			return nil, fmt.Errorf("dial tcp %s: i/o timeout", kamo)
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", kamo)
	}
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	if p := putIz(t, a, b.node.ID); p.Razlog != razmjena.RazlogPutaNijeSeJavio || p.Uspjesi != 1 {
		t.Errorf("nakon adrese koja ne odgovara: %+v", p)
	}
	pokusaji := a.PokusajiSpajanja(b.node.ID)
	if len(pokusaji) != 2 || pokusaji[0].Adresa != adresaNaPetlji(b) || pokusaji[0].Greska != "" ||
		pokusaji[1].Adresa != "192.168.1.250:4710" || pokusaji[1].Greska == "" {
		t.Errorf("pokušaji: %+v", pokusaji)
	}
	if p := a.PokusajiSpajanja("nepoznat"); p == nil || len(p) != 0 {
		t.Errorf("nepoznato računalo: %v", p)
	}

	nema = true
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err == nil {
		t.Fatal("razmjena bez ijedne adrese")
	}
	if p := putIz(t, a, b.node.ID); p.Put != razmjena.PutAdresa || p.Pokusaji != 2 || p.Neuspjesi != 1 || p.Razlog != razmjena.RazlogPutaNijeSeJavio {
		t.Errorf("nakon neuspjeha: %+v", p)
	}

	if err := a.ForgetPeer(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	var put, zvao string
	var od sql.NullTime
	var pokusaj int
	if err := a.db.QueryRow(`SELECT put, zadnji_zvao, put_od, put_pokusaji FROM peer_sync WHERE node_id = ?`, b.node.ID).Scan(&put, &zvao, &od, &pokusaj); err != nil {
		t.Fatal(err)
	}
	if put != "" || zvao != "" || od.Valid || pokusaj != 0 {
		t.Errorf("nakon zaborava: %q %q %v %d", put, zvao, od, pokusaj)
	}
}

// Redom bilješki: brojači kreću ispočetka samo kad se promijeni naziv puta;
// RTT dolazne veze se ne zna, pa ostaje prijašnji; razlog piše samo
// razmjena koju je započelo ovo računalo.
func TestPutRazmjeneBrojaci(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	const id = "laptop-baranja"
	greska := errors.New("veza je pukla")
	korak := func(opis string, o syncOutcome, provjera func(PutRazmjene) bool) PutRazmjene {
		t.Helper()
		s.recordSyncState(ctxRub, id, o)
		p := putIz(t, s, id)
		if !provjera(p) {
			t.Fatalf("%s: %+v", opis, p)
		}
		return p
	}
	prvi := korak("prva razmjena u LAN-u", syncOutcome{put: razmjena.PutLAN, rtt: 300 * time.Microsecond, primljeno: 100, poslano: 50},
		func(p PutRazmjene) bool {
			return p.Naziv == razmjena.NazivLAN && p.RTTMs == 1 && p.Od != nil && p.Pokusaji == 1 && p.Uspjesi == 1 && p.Primljeno == 100 && p.ZadnjiZvao == "ovaj"
		})
	korak("dolazna na port, isti naziv", syncOutcome{put: razmjena.PutAdresa, privatna: true, dolazna: true, primljeno: 10, err: greska},
		func(p PutRazmjene) bool {
			return p.Put == razmjena.PutAdresa && p.RTTMs == 1 && p.Od.Equal(*prvi.Od) && p.Pokusaji == 2 && p.Neuspjesi == 1 && p.Primljeno == 110 && p.ZadnjiZvao == "drugi"
		})
	korak("bez veze", syncOutcome{err: greska, razlog: razmjena.RazlogPutaNijeSeJavio},
		func(p PutRazmjene) bool {
			return p.Put == razmjena.PutAdresa && p.Pokusaji == 3 && p.Neuspjesi == 2 && p.Razlog == razmjena.RazlogPutaNijeSeJavio && p.ZadnjiZvao == "ovaj"
		})
	time.Sleep(10 * time.Millisecond) // put_od mora biti noviji od prvog
	korak("kroz tunel", syncOutcome{put: razmjena.PutKrozTunel, rtt: 40 * time.Millisecond, primljeno: 7, razlog: razmjena.RazlogPutaNijeSeJavio},
		func(p PutRazmjene) bool {
			return p.Naziv == razmjena.NazivServer && p.RTTMs == 40 && p.Od.After(*prvi.Od) && p.Pokusaji == 1 && p.Uspjesi == 1 && p.Neuspjesi == 0 && p.Primljeno == 7
		})
	korak("dolazna kroz tunel", syncOutcome{put: razmjena.PutKrozTunel, dolazna: true},
		func(p PutRazmjene) bool {
			return p.RTTMs == 40 && p.Razlog == razmjena.RazlogPutaNijeSeJavio && p.Pokusaji == 2
		})
	korak("dolazna preko Tailscalea", syncOutcome{put: razmjena.PutTailscale, dolazna: true},
		func(p PutRazmjene) bool { return p.Naziv == razmjena.NazivTailscale && p.RTTMs == 0 && p.Pokusaji == 1 })
	korak("uspjela iz LAN-a briše razlog", syncOutcome{put: razmjena.PutLAN, rtt: 2 * time.Millisecond},
		func(p PutRazmjene) bool { return p.Naziv == razmjena.NazivLAN && p.Razlog == "" && p.RTTMs == 2 })
}

// Kad se put ne da upisati, to piše u dnevnik, a razmjena ide dalje.
func TestPutRazmjeneBezBaze(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	s.upisiPut(ctxRub, "laptop-baranja", syncOutcome{put: razmjena.PutLAN}, time.Now())
	if n := s.nazivPuta(ctxRub, "laptop-baranja"); n != "" {
		t.Errorf("naziv bez baze: %q", n)
	}
}

func TestRazlogNakonIPrivatnaAdresa(t *testing.T) {
	for adresa, privatna := range map[string]bool{
		"192.168.1.20:4710": true, "10.0.0.5": true, "127.0.0.1:4710": true, "[fd00::5]:4710": true,
		"[::ffff:192.168.0.9]:4710": true, "8.8.8.8:4710": false, "100.101.0.2:4710": false,
		"[fd7a:115c:a1e0::2]:4710": false, "cop.primjer.hr:4710": false, "https://cop.primjer.hr": false, "<nil>": false,
	} {
		if got := privatnaAdresa(adresa); got != privatna {
			t.Errorf("%s: privatna %v", adresa, got)
		}
	}
	lan := kandidat{"192.168.1.20:4710", razmjena.PutAdresa}
	for _, c := range []struct {
		prije []kandidat
		want  razmjena.RazlogPuta
	}{
		{nil, ""},
		{[]kandidat{{"cop.primjer.hr:4710", razmjena.PutAdresa}, {"100.101.0.2:4710", razmjena.PutTailscale}}, ""},
		{[]kandidat{{"192.168.1.20:4710", razmjena.PutLAN}}, razmjena.RazlogPutaNijeSeJavio},
		{[]kandidat{{"cop.primjer.hr:4710", razmjena.PutAdresa}, lan}, razmjena.RazlogPutaNijeSeJavio},
	} {
		if got := razlogNakon(c.prije); got != c.want {
			t.Errorf("%v: %q", c.prije, got)
		}
	}
}

func TestSamoDolaziIRTT(t *testing.T) {
	kroz := PutRazmjene{Put: razmjena.PutKrozTunel}
	if !samoDolazi(SyncState{Fails: 1, Put: kroz}) || samoDolazi(SyncState{Put: kroz}) || samoDolazi(SyncState{Fails: 2, Put: PutRazmjene{Put: razmjena.PutLAN}}) {
		t.Error("samo dolazi")
	}
	for d, ms := range map[time.Duration]int{0: 0, -time.Second: 0, 100 * time.Microsecond: 1, 1500 * time.Microsecond: 1, 42 * time.Millisecond: 42} {
		if got := rttMs(d); got != ms {
			t.Errorf("rttMs(%s) = %d", d, got)
		}
	}
	if vrijemeIz(sql.NullTime{}) != nil {
		t.Error("vrijeme bez vrijednosti")
	}
	if o := (syncOutcome{put: razmjena.PutLAN}).uzVezu(nil); o.put != razmjena.PutLAN {
		t.Errorf("bez veze: %+v", o)
	}
}
