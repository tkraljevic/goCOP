package peers

// Nadzorna ploča na jednom čvoru, bez mreže: upozorenja ljudskim jezikom,
// redoslijed i dostupnost čvorova te ploča kad se baza ne može pročitati.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// Ploča kaže što ne štima: čvor bez mreže i čvor bez poznatih čvorova samo
// to; inače isključenu automatsku razmjenu, razmjenu koja nije uspjela ili
// davno, čvor bez članstva, čvor koji ne odgovara, bez adrese ili na drugom
// izdanju programa, i mrežu bez ijedne javne adrese.
func TestUpozorenjaPloce(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	every := 5 * time.Minute
	sada := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	davno := sada.Add(-2 * time.Hour)
	mreza := &Network{Name: "Hrvatske vode"}

	if a := s.alerts(&Status{}, every, sada); len(a) != 1 || !strings.Contains(a[0], "nije ni u jednoj mreži") {
		t.Errorf("bez mreže: %v", a)
	}
	if a := s.alerts(&Status{Network: mreza}, every, sada); len(a) != 1 || !strings.Contains(a[0], "Nema nijednog poznatog čvora") {
		t.Errorf("bez čvorova: %v", a)
	}

	st := &Status{Network: mreza, Program: "0.0.38", Total: 4, LastOK: &davno, Peers: []PeerStatus{
		{Peer: Peer{NodeID: "ured-vukovar", Name: "Ured Vukovar", Addresses: []string{"192.0.2.10:4710"}}, Member: true,
			State: SyncState{Program: "0.0.36", LastOK: &davno}},
		{Peer: Peer{NodeID: "laptop-baranja"}, MemberProblem: "nema potvrde članstva",
			State: SyncState{Fails: 3, LastError: "veza odbijena"}},
		{Peer: Peer{NodeID: "laptop-ilok", Name: "Laptop Ilok", Addresses: []string{"192.0.2.11:4710"}}, Member: true,
			State: SyncState{Fails: 4, LastOK: &davno, LastError: "istek roka", ProgramPoznat: true}},
		{Peer: Peer{NodeID: "stari-cvor", Name: "Stari čvor", Addresses: []string{"192.0.2.12:4710"}}, Member: true,
			State: SyncState{ProgramPoznat: true}},
	}}
	a := s.alerts(st, every, sada)
	vrijeme := davno.Local().Format("02.01. 15:04")
	ocekivano := []string{
		"Automatska sinkronizacija je isključena; podaci se razmjenjuju samo na ručni zahtjev.",
		"Ni s kim nije bilo uspješne razmjene od " + vrijeme + "; provjerite mrežu i adrese čvorova.",
		"Čvor Ured Vukovar radi na 0.0.36, ovaj na 0.0.38 — ažurirajte stariji čvor.",
		"laptop-baranja: nema potvrde članstva — razmjena s njim nije dopuštena.",
		"laptop-baranja: nikad nije odgovorio (3 pokušaja zaredom): veza odbijena",
		"laptop-baranja: nema nijednu poznatu adresu, pa ga ovaj čvor ne može nazvati.",
		"Laptop Ilok: ne odgovara od " + vrijeme + " (4 pokušaja zaredom): istek roka",
		"Čvor Laptop Ilok radi na verziji starijoj od 0.0.38 (ne javlja verziju) — ažurirajte ga.",
		"Nijedan čvor nema javnu adresu: razmjena radi samo unutar lokalne mreže.",
	}
	if !slices.Equal(a, ocekivano) {
		t.Errorf("upozorenja:\n%s\nočekivano:\n%s", strings.Join(a, "\n"), strings.Join(ocekivano, "\n"))
	}

	// svježa razmjena, uključena automatska, stalno izložen čvor: tiho
	nedavno := sada.Add(-time.Minute)
	st = &Status{Network: mreza, AutoSync: true, Total: 1, LastOK: &nedavno, Peers: []PeerStatus{
		{Peer: Peer{NodeID: "ured-vukovar", Name: "Ured Vukovar", IsBootstrap: true, Addresses: []string{"ured.example:4710"}}, Member: true,
			State: SyncState{LastOK: &nedavno}},
	}}
	if a := s.alerts(st, every, sada); len(a) != 0 {
		t.Errorf("sve u redu, a ploča javlja: %v", a)
	}
	// razmjena još nije uspjela ni s kim
	st.LastOK, st.Peers[0].State.LastOK = nil, nil
	if a := s.alerts(st, every, sada); !slices.Equal(a, []string{"Još nijedna razmjena nije uspjela ni s jednim čvorom."}) {
		t.Errorf("bez ijedne razmjene: %v", a)
	}
	st.LastOK, st.Peers[0].State.LastOK = &nedavno, &nedavno

	// javnu adresu ima ovaj čvor sam
	st.Peers[0].IsBootstrap = false
	if _, err := s.PublicAddress(ctxRub, s.node.ID, "cop-osijek.example:4710", ""); err != nil {
		t.Fatal(err)
	}
	if a := s.alerts(st, every, sada); len(a) != 0 {
		t.Errorf("ovaj čvor ima javnu adresu, a ploča javlja: %v", a)
	}
}

// upisiStanje upisuje zadnju uspješnu razmjenu s čvorom
func upisiStanje(t *testing.T, s *Service, id string, zadnjiPut time.Time, neuspjeha int) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id, last_attempt, last_ok, fails) VALUES (?, ?, ?, ?)`, id, zadnjiPut, zadnjiPut, neuspjeha); err != nil {
		t.Fatal(err)
	}
}

// Na ploči su prvo čvorovi na mreži, pa oni koji šute, pa oni s kojima
// razmjene još nije bilo, unutar toga po imenu. Čvor s kojim je razmjena
// nedavno uspjela na mreži je i kad ga ovaj ne može nazvati.
func TestPlocaRedoslijedIDostupnost(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	clan := izmisljeniKljuc(t)
	m, err := s.izdajClanstvo(ctxRub, "ured-vukovar", clan)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.saveMembership(ctxRub, m); err != nil {
		t.Fatal(err)
	}
	sada := time.Now().UTC()
	for _, p := range []struct {
		id, ime string
		kljuc   string
	}{
		{"ured-vukovar", "Ured Vukovar", razmjena.PublicKeyString(clan)},
		{"laptop-baranja", "Laptop Baranja", javniKljuc(t)},
		{"novi-b", "Novi B", javniKljuc(t)},
		{"novi-a", "Novi A", javniKljuc(t)},
	} {
		if err := s.SavePeer(ctxRub, Peer{NodeID: p.id, Name: p.ime, PublicKey: p.kljuc, Addresses: []string{"192.0.2.20:4710"}}); err != nil {
			t.Fatal(err)
		}
	}
	upisiStanje(t, s, "ured-vukovar", sada.Add(-time.Minute), 1)
	upisiStanje(t, s, "laptop-baranja", sada.Add(-2*time.Hour), 0)

	st, err := s.Status(ctxRub, true)
	if err != nil {
		t.Fatal(err)
	}
	var redom []string
	for _, p := range st.Peers {
		redom = append(redom, p.NodeID+" "+p.Reachability)
	}
	if want := []string{"ured-vukovar online", "laptop-baranja offline", "novi-a never", "novi-b never"}; !slices.Equal(redom, want) {
		t.Errorf("redoslijed %v, očekivano %v", redom, want)
	}
	if st.Online != 1 || st.Total != 4 || st.LastOK == nil || !st.Peers[0].SamoDolazi || st.Peers[1].SamoDolazi {
		t.Errorf("ploča: online %d, ukupno %d, zadnje %v, samo dolazi %v/%v", st.Online, st.Total, st.LastOK, st.Peers[0].SamoDolazi, st.Peers[1].SamoDolazi)
	}
	if !st.Peers[0].Member || st.Peers[1].Member || st.Peers[1].MemberProblem != "nema potvrde članstva" {
		t.Errorf("članstvo: %+v %+v", st.Peers[0], st.Peers[1])
	}
	if st.LanDiscovered != nil {
		t.Error("čvor bez pronalaženja pitao je lokalnu mrežu")
	}
}

// Ploča bez popisa čvorova ili stanja razmjene javlja grešku; stanje koje
// se ne da pročitati također.
func TestPlocaKadBazaNeRadi(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "ured-vukovar", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	pokvariTablicu(t, s, "peer_sync")
	if _, err := s.Status(ctxRub, false); err == nil {
		t.Error("ploča bez stanja razmjene")
	}
	if _, err := s.db.Exec(`CREATE TABLE peer_sync (node_id TEXT, their_frontier TEXT, last_attempt DATETIME, last_ok DATETIME, last_error TEXT,
		applied INTEGER, sent INTEGER, duration_ms INTEGER, fails INTEGER, program TEXT, gradnja TEXT, program_poznat INTEGER, tailscale TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id, their_frontier, last_error) VALUES ('ured-vukovar', '{}', NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.syncStates(ctxRub); err == nil {
		t.Error("nečitljivo stanje razmjene je pročitano")
	}
	pokvariTablicu(t, s, "peers")
	if _, err := s.Status(ctxRub, false); err == nil {
		t.Error("ploča bez popisa čvorova")
	}
	if sazetak := s.SyncAll(ctxRub); len(sazetak) != 1 || sazetak["*"] == "" {
		t.Errorf("razmjena sa svima bez popisa čvorova: %v", sazetak)
	}
}
