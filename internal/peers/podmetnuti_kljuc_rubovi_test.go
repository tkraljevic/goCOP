package peers

import (
	"crypto/ed25519"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"gocop/internal/razmjena"
)

// Dolazna razmjena pripada računalu čije članstvo nosi dokazani ključ, i
// kad zapis drugog računala nosi isti ključ, i kad isti ključ stoji u
// krivotvorenom članstvu pod drugom oznakom
func TestCvorPoKljucuIzClanstva(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-vinkovci")
	c := rubniCvor(t, "laptop-zupanja")
	primiIzravno(t, a, b)
	primiIzravno(t, a, c)
	upoznaj(t, a, b, "192.0.2.10:4710")
	upoznaj(t, a, c, "192.0.2.11:4710")
	d := rubniCvor(t, "pperic-laptop") // uparen, a nije član
	upoznaj(t, a, d, "192.0.2.12:4710")
	// površina nakon podmetnutog zapisa čvora i krivotvorenog članstva,
	// najnovijeg, koje provjera prva gleda
	if _, err := a.db.Exec(`UPDATE peers SET public_key = ? WHERE node_id = ?`, c.node.PublicKey(), b.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES ('podmetnut', ?, 'x', 'x', ?, ?, 'x', ?)`, c.node.PublicKey(), time.Now().UTC().Add(time.Hour), time.Now().UTC().Add(time.Hour), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for ime, sl := range map[string]struct {
		kljuc ed25519.PublicKey
		cvor  string
	}{
		"C-ov ključ":    {c.javni(), c.node.ID},
		"B-ov ključ":    {b.javni(), b.node.ID},
		"tuđi ključ":    {rubniCvor(t, "tudji-laptop").javni(), ""},
		"samo u zapisu": {d.javni(), ""},
	} {
		p, err := a.peerByKey(ctxRub, sl.kljuc, nil)
		if err != nil {
			t.Fatalf("%s: %v", ime, err)
		}
		got := ""
		if p != nil {
			got = p.NodeID
		}
		if got != sl.cvor {
			t.Errorf("%s: čvor %q, očekivan %q", ime, got, sl.cvor)
		}
	}

	// red bez oznake ne da se pročitati; greška ide pozivatelju
	if _, err := a.db.Exec(`INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES (NULL, ?, 'x', 'x', ?, ?, 'x', ?)`, b.node.PublicKey(), time.Now().UTC(), time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.peerByKey(ctxRub, b.javni(), nil); err == nil {
		t.Error("red članstva bez oznake")
	}
	pokvariTablicu(t, a, "memberships")
	if p, err := a.peerByKey(ctxRub, c.javni(), nil); err == nil {
		t.Errorf("bez tablice članstava: %+v", p)
	}
}

// krozTunel otvara WebSocket do tunela čvora s, kao Cloudflare ispred njega,
// i u njemu TLS razmjene s ključem koji pozivatelj očekuje. Vanjski TLS
// (DialTunel) test iz ovog paketa ne može zadati, pa WebSocket ide izravno.
func krozTunel(t *testing.T, pozivatelj, s *Service, expect ed25519.PublicKey) (*razmjena.Conn, error) {
	t.Helper()
	srv := httptest.NewServer(s.TunelHandler())
	t.Cleanup(srv.Close)
	ws, err := websocket.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+razmjena.PutTunela, "", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ws.PayloadType = websocket.BinaryFrame
	return razmjena.DialExchangeOn(ctxRub, ws, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, srv.URL, expect)
}

// podmetnutiNaPovrsini su tri člana na loopbacku; A u popisu računala ima
// B s C-ovim ključem, kao nakon podmetnutog zapisa čvora
func podmetnutiNaPovrsini(t *testing.T) (a, b, c *Service) {
	t.Helper()
	cv := clanovi(t, "cop-osijek", "laptop-vinkovci", "laptop-zupanja")
	a, b, c = cv[0], cv[1], cv[2]
	if _, err := a.db.Exec(`UPDATE peers SET public_key = ? WHERE node_id = ?`, c.node.PublicKey(), b.node.ID); err != nil {
		t.Fatal(err)
	}
	return a, b, c
}

// Podmetnuti ključ ne vrijedi ni kroz tunel: s ključem koji razmjena traži
// za B, A kroz C-ov tunel ne dobiva vezu, a kroz B-ov dobiva i razmjenjuje
func TestPodmetnutiKljucKrozTunel(t *testing.T) {
	a, b, c := podmetnutiNaPovrsini(t)
	_, expect, err := a.partnerZaRazmjenu(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if conn, err := krozTunel(t, a, c, expect); err == nil {
		conn.Close()
		t.Error("A je kroz C-ov tunel dobio vezu s ključem koji traži za B")
	} else if !strings.Contains(err.Error(), "DIFFERENT key") {
		t.Errorf("kroz C-ov tunel: %v", err)
	}
	conn, err := krozTunel(t, a, b, expect)
	if err != nil {
		t.Fatalf("kroz B-ov tunel: %v", err)
	}
	_, _, _, err = a.exchange(ctxRub, conn, true)
	conn.Close()
	if err != nil {
		t.Errorf("razmjena s B kroz tunel: %v", err)
	}
}

// Kad C nazove A kroz tunel, A razmjenu bilježi C-u, a ne B-u čiji zapis
// nosi C-ov ključ
func TestDolaznaKrozTunelIdeOnomeKoJeDokazaoKljuc(t *testing.T) {
	a, b, c := podmetnutiNaPovrsini(t)
	conn, err := krozTunel(t, c, a, a.javni())
	if err != nil {
		t.Fatalf("C kroz A-ov tunel: %v", err)
	}
	_, _, _, err = c.exchange(ctxRub, conn, true)
	conn.Close()
	if err != nil {
		t.Fatalf("razmjena C s A kroz tunel: %v", err)
	}
	// A bilješku upisuje u svojoj gorutini, nakon razgovora
	var st map[string]SyncState
	for i := 0; i < 200; i++ {
		if st, err = a.syncStates(ctxRub); err != nil {
			t.Fatal(err)
		}
		if st[c.node.ID].LastOK != nil || st[b.node.ID].LastAttempt != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st[b.node.ID].LastAttempt != nil {
		t.Errorf("A je C-ovu razmjenu kroz tunel zabilježio B-u: %+v", st[b.node.ID])
	}
	if st[c.node.ID].LastOK == nil {
		t.Errorf("A nije zabilježio razmjenu s C kroz tunel: %+v", st)
	}
}
