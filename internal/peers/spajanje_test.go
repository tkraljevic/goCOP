package peers

import (
	"fmt"
	"log"
	"strings"
	"testing"
	"time"
)

// Razmjena se ne pokreće s nepoznatim čvorom, s čvorom koji nije član ni
// s čvorom čiji se ključ u članstvu ne da pročitati; greška baze nije
// "nepoznat čvor"
func TestRazmjenaSamoSPoznatimClanom(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	if _, _, err := s.SyncWith(ctxRub, "nepoznati-cvor"); err == nil || !strings.Contains(err.Error(), "nije poznat") {
		t.Errorf("nepoznat čvor: %v", err)
	}
	if err := s.SavePeer(ctxRub, Peer{NodeID: "tudji-laptop", Name: "tudji-laptop", PublicKey: javniKljuc(t), Addresses: []string{"192.0.2.9:4710"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SyncWith(ctxRub, "tudji-laptop"); err == nil || !strings.Contains(err.Error(), "nije član") {
		t.Errorf("čvor izvan mreže: %v", err)
	}
	// namjerno promijenjeno: sigurnosni popravak podmetnutog ključa
	// (docs/plan-web-sync.md r. 87). Razmjena ključ više ne čita iz zapisa
	// čvora nego iz članstva, pa se kvari ključ u članstvu.
	if _, err := s.db.Exec(`INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES ('tudji-laptop', 'nije-kljuc', 'x', 'x', ?, ?, 'x', ?)`, time.Now().UTC(), time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SyncWith(ctxRub, "tudji-laptop"); err == nil || strings.Contains(err.Error(), "nije član") {
		t.Errorf("ključ koji se ne da pročitati: %v", err)
	}
	pokvariTablicu(t, s, "peers")
	if _, _, err := s.SyncWith(ctxRub, "tudji-laptop"); err == nil || strings.Contains(err.Error(), "nije poznat") {
		t.Errorf("bez tablice čvorova: %v", err)
	}
}

// Član bez ijedne adrese ne zove se; kad nijedna adresa ne odgovori,
// greška to kaže, a bilješka o razmjeni broji neuspjeh
func TestRazmjenaKadNijednaAdresaNeOdgovara(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	upoznaj(t, a, b)
	if _, err := a.db.Exec(`UPDATE peers SET addresses = '[]' WHERE node_id = ?`, b.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), "nije poznata nijedna adresa") {
		t.Errorf("bez adresa: %v", err)
	}
	zatvoren := fmt.Sprintf("127.0.0.1:%d", slobodanPort(t))
	upoznaj(t, a, b, zatvoren, "https://127.0.0.1:1")
	_, _, err := a.SyncWith(ctxRub, b.node.ID)
	if err == nil || !strings.Contains(err.Error(), "nije dostupan ni na jednoj adresi") || !strings.Contains(err.Error(), "tunel https://127.0.0.1:1") {
		t.Errorf("nijedna adresa ne odgovara: %v", err)
	}
	st, err := a.syncStates(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	if st[b.node.ID].Fails != 1 || st[b.node.ID].LastError == "" {
		t.Errorf("bilješka o neuspjehu: %+v", st[b.node.ID])
	}
}

// Kad tunel prestane primati dok čvor radi, razmjena na portu radi dalje,
// a dnevnik kaže što je s tunelom
func TestTunelKojiPrestaneRaditiIdeUDnevnik(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	d := &dojavaDnevnika{trazi: "razmjena kroz tunel:", stiglo: make(chan struct{})}
	stari := log.Writer()
	log.SetOutput(d)
	t.Cleanup(func() { log.SetOutput(stari) })
	naPetlji(t, s)
	if err := s.tunel.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.stiglo:
	case <-time.After(10 * time.Second):
		t.Fatal("zatvoren tunel nije zapisan u dnevnik")
	}
	cekajPort(t, s.ports.Exchange)
}
