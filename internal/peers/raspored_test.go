package peers

// Raspored automatske sinkronizacije: kome je red (odgoda za čvorove koji
// šute), krug koji zove samo njih i petlja koja krugove pokreće. Čvorovi
// iz testa nisu članovi mreže ili nemaju ispravan ključ, pa razmjena
// staje prije mreže, a ishod se vidi u sažetku kruga.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// Tko odgovara ili je stalno izložen, zove se svaki krug; tko šuti zaredom,
// zove se nakon onoliko razmaka koliko je puta šutio, najviše osam.
func TestRedNaCvorPremaSutnji(t *testing.T) {
	sad := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	prije := func(d time.Duration) *time.Time { v := sad.Add(-d); return &v }
	razmak := 5 * time.Minute
	for _, s := range []struct {
		ime    string
		cvor   Peer
		stanje SyncState
		red    bool
	}{
		{"nikad pokušan", Peer{}, SyncState{}, true},
		{"zadnji put odgovorio", Peer{}, SyncState{LastAttempt: prije(time.Second)}, true},
		{"šuti, ali nema vremena pokušaja", Peer{}, SyncState{Fails: 3}, true},
		{"stalno izložen ne čeka ni kad šuti", Peer{IsBootstrap: true}, SyncState{Fails: 5, LastAttempt: prije(time.Second)}, true},
		{"šutio dvaput, prošlo manje od dva razmaka", Peer{}, SyncState{Fails: 2, LastAttempt: prije(9 * time.Minute)}, false},
		{"šutio dvaput, prošla točno dva razmaka", Peer{}, SyncState{Fails: 2, LastAttempt: prije(10 * time.Minute)}, true},
		{"šutio dvadeset puta, čeka najviše osam razmaka", Peer{}, SyncState{Fails: 20, LastAttempt: prije(40 * time.Minute)}, true},
		{"šutio dvadeset puta, osam razmaka još nije prošlo", Peer{}, SyncState{Fails: 20, LastAttempt: prije(39 * time.Minute)}, false},
	} {
		if got := due(s.cvor, s.stanje, razmak, sad); got != s.red {
			t.Errorf("%s: red %v, očekivano %v", s.ime, got, s.red)
		}
	}
}

// Razmak automatske sinkronizacije: dok nije postavljen (ili je nula),
// vrijedi pet minuta.
func TestRazmakSinkronizacije(t *testing.T) {
	s := &Service{}
	if s.interval() != 5*time.Minute {
		t.Errorf("zadani razmak %v", s.interval())
	}
	s.SetInterval(time.Hour)
	if s.interval() != time.Hour {
		t.Errorf("postavljeni razmak %v", s.interval())
	}
	s.SetInterval(0)
	if s.interval() != 5*time.Minute {
		t.Errorf("nula vraća zadani razmak, a dobiven je %v", s.interval())
	}
}

// javniKljuc je ključ izmišljenog čvora u zapisu kao što ga razmjena pamti
func javniKljuc(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return razmjena.PublicKeyString(pub)
}

// cvorZaRaspored: poznati čvorovi kojima je red (jedan s ključem koji se
// ne da pročitati, jedan izvan mreže) i jedan koji šuti pa još čeka
func cvorZaRaspored(t *testing.T) *Service {
	t.Helper()
	s := rubniCvor(t, "cop-osijek")
	if _, err := s.db.Exec(`INSERT INTO peers (node_id, name, public_key, addresses, is_bootstrap, last_sync_note, created_at)
		VALUES ('los-kljuc', 'los-kljuc', 'nije-kljuc', '["192.0.2.9:4710"]', 0, '', ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ured-vukovar", "laptop-baranja"} {
		if err := s.SavePeer(ctxRub, Peer{NodeID: id, Name: id, PublicKey: javniKljuc(t), Addresses: []string{"192.0.2.10:4710"}}); err != nil {
			t.Fatal(err)
		}
	}
	// laptop je triput zaredom šutio prije minute: čeka tri razmaka
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id, last_attempt, last_error, fails) VALUES ('laptop-baranja', ?, 'ne odgovara', 3)`,
		time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	return s
}

// Krug zove samo čvorove kojima je red. Čvor izvan mreže se odbije prije
// ikakve veze, a krivi ključ javi grešku; obje greške stoje u sažetku.
func TestKrugZoveSamoOneKojimaJeRed(t *testing.T) {
	s := cvorZaRaspored(t)
	sazetak := s.SyncDue(ctxRub)
	if len(sazetak) != 2 {
		t.Fatalf("sažetak kruga: %v", sazetak)
	}
	if _, ima := sazetak["laptop-baranja"]; ima {
		t.Error("čvor koji šuti pozvan je prije isteka odgode")
	}
	if n := sazetak["ured-vukovar"]; !strings.HasPrefix(n, "greška: ") || !strings.Contains(n, "nije član naše mreže") {
		t.Errorf("čvor izvan mreže: %q", n)
	}
	if n := sazetak["los-kljuc"]; !strings.HasPrefix(n, "greška: ") {
		t.Errorf("čvor s krivim ključem: %q", n)
	}

	// kad odgoda prođe (razmak je kraći od minute), zove se i laptop
	s.SetInterval(10 * time.Second)
	if _, ima := s.SyncDue(ctxRub)["laptop-baranja"]; !ima {
		t.Error("odgoda je prošla, a čvor nije pozvan")
	}
}

// Krug bez popisa čvorova ne zove nikoga i javlja grešku pod "*"; krug bez
// čvorova vraća prazan sažetak.
func TestKrugKadPopisNijeDostupan(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if sazetak := s.SyncDue(ctxRub); len(sazetak) != 0 {
		t.Errorf("bez čvorova: %v", sazetak)
	}
	pokvariTablicu(t, s, "peers")
	sazetak := s.SyncDue(ctxRub)
	if len(sazetak) != 1 || sazetak["*"] == "" {
		t.Errorf("bez popisa čvorova: %v", sazetak)
	}
}

// dojavaDnevnika hvata dnevnik i javi kad se pojavi traženi tekst
type dojavaDnevnika struct {
	mu     sync.Mutex
	trazi  string
	stiglo chan struct{}
	javio  bool
}

func (d *dojavaDnevnika) Write(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.javio && strings.Contains(string(b), d.trazi) {
		d.javio = true
		close(d.stiglo)
	}
	return len(b), nil
}

// Automatska sinkronizacija bez razmaka se ne pokreće. S razmakom krug
// kreće sam, ishod piše u dnevnik i staje kad se zahtjev otkaže.
func TestAutomatskaSinkronizacija(t *testing.T) {
	s := cvorZaRaspored(t)
	s.RunAutoSync(ctxRub, 0) // vraća se odmah; inače test ovdje stoji
	if s.autoSync() {
		t.Fatal("sinkronizacija bez razmaka je uključena")
	}

	d := &dojavaDnevnika{trazi: "sinkronizacija s ured-vukovar: greška", stiglo: make(chan struct{})}
	stari := log.Writer()
	log.SetOutput(d)
	t.Cleanup(func() { log.SetOutput(stari) })

	ctx, otkazi := context.WithCancel(ctxRub)
	gotovo := make(chan struct{})
	go func() {
		s.RunAutoSync(ctx, time.Millisecond)
		close(gotovo)
	}()
	select {
	case <-d.stiglo:
	case <-time.After(30 * time.Second):
		t.Fatal("krug nije zapisan u dnevnik")
	}
	if !s.autoSync() || s.interval() != time.Millisecond {
		t.Errorf("uključeno %v, razmak %v", s.autoSync(), s.interval())
	}
	otkazi()
	select {
	case <-gotovo:
	case <-time.After(30 * time.Second):
		t.Fatal("automatska sinkronizacija nije stala nakon otkazivanja")
	}
}
