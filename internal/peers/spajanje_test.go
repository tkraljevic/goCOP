package peers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// Razmjena se ne pokreće s nepoznatim čvorom ni s čvorom koji nije član,
// ni kad u tablici članstava za njega stoji red koji ne vrijedi; greška baze
// nije "nepoznat čvor" ni "nije član"
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
	// (plan WEB synca, „Ispravak zapisa čvora”). Ključ za razmjenu
	// dolazi iz članstva koje vrijedi, pa red s ključem koji se ne da
	// pročitati (prije greška) znači samo da čvor nije član.
	if _, err := s.db.Exec(`INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES ('tudji-laptop', 'nije-kljuc', 'x', 'x', ?, ?, 'x', ?)`, time.Now().UTC(), time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SyncWith(ctxRub, "tudji-laptop"); err == nil || !strings.Contains(err.Error(), "nije član") {
		t.Errorf("članstvo koje ne vrijedi: %v", err)
	}
	pokvariTablicu(t, s, "record_versions")
	if _, _, err := s.SyncWith(ctxRub, "tudji-laptop"); err == nil || strings.Contains(err.Error(), "nije član") {
		t.Errorf("bez knjige: %v", err)
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
	// port i dalje prima: rukovanje na njemu dokaže ključ čvora, a vrata
	// porta zabilježe da je rukovanje prekinuto nakon dokaza
	raw, err := net.Dial("tcp", adresaNaPetlji(s))
	if err != nil {
		t.Fatal(err)
	}
	ctx, otkazi := context.WithTimeout(ctxRub, 15*time.Second)
	defer otkazi()
	if _, err := razmjena.ProvjeriKljucOn(ctx, raw, s.node.key, Protocol, adresaNaPetlji(s), s.javni()); err != nil {
		t.Fatalf("port razmjene nakon zatvorenog tunela: %v", err)
	}
	cekajDa(t, "vrata porta bilježe prekinuto rukovanje", func() bool {
		z, err := s.Odbijeno(ctxRub, 1)
		return err == nil && len(z) == 1 && z[0].Put == PutVrataPort && z[0].Razlog == string(razmjena.VrataRukovanje)
	})
}

// Kad je port razmjene zauzet, Serve to javi odmah (program zapiše da
// razmjena nije dostupna), a tunel prima dalje dok ctx traje
func TestZauzetPortRazmjeneTunelPrima(t *testing.T) {
	c := clanoviMreze(t, false, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	zauzet, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { zauzet.Close() })
	b.ports.Exchange = zauzet.Addr().(*net.TCPAddr).Port
	ctx, otkazi := context.WithCancel(ctxRub)
	t.Cleanup(otkazi)
	stala := make(chan error, 1)
	go func() { stala <- b.Serve(ctx) }()
	select {
	case err := <-stala:
		if !errors.Is(err, syscall.EADDRINUSE) {
			t.Fatalf("razmjena na zauzetom portu: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("razmjena na zauzetom portu nije javila grešku")
	}
	conn, err := krozTunel(t, a, b, b.javni())
	if err != nil {
		t.Fatalf("kroz tunel uz zauzet port: %v", err)
	}
	conn.Close()
}

// Port razmjene i tunel dijele mjesta za razmjene (Serve): dok na portu
// stoje četiri razmjene, i razmjena kroz tunel dobije odbijenicu da je
// čvor zauzet
func TestTunelIPortDijeleMjestaRazmjena(t *testing.T) {
	c := clanoviMreze(t, false, "laptop-baranja", "ured-vukovar")
	a, b := c[0], c[1]
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b.slusalica = ln
	ctx, otkazi := context.WithCancel(ctxRub)
	t.Cleanup(otkazi)
	stala := make(chan error, 1)
	go func() { stala <- b.Serve(ctx) }()
	srv := httptest.NewTLSServer(b.TunelHandler())
	t.Cleanup(srv.Close)
	a.tunelKlijent.TLS = srv.Client().Transport.(*http.Transport).TLSClientConfig

	upoznaj(t, a, b, ln.Addr().String())
	peer, expect, err := a.partnerZaRazmjenu(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	// razmjena koja stane nakon granica drži svoje mjesto dok test traje
	for i := 0; i < 4; i++ {
		conn, err := a.nazovi(ctxRub, peer, kandidat{ln.Addr().String(), razmjena.PutAdresa}, expect, rokOstalih)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		r, err := a.zapocniRazgovor(ctxRub, conn, true)
		if err == nil {
			err = r.granice()
		}
		if err != nil {
			t.Fatalf("razmjena %d na portu: %v", i+1, err)
		}
	}
	upoznaj(t, a, b, srv.URL)
	if _, _, err := a.SyncWith(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), razmjena.RazlogZauzet) {
		select {
		case e := <-stala:
			t.Fatalf("razmjena kroz tunel: %v (slušalica je stala: %v)", err, e)
		default:
			t.Errorf("razmjena kroz tunel uz četiri razmjene na portu: %v", err)
		}
	}
}
