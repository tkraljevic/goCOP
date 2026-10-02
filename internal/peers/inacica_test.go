package peers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
)

// servisZaTest je čvor s praznom bazom, bez mreže i bez portova
func servisZaTest(t *testing.T) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "gocop.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	n, err := LoadNode(dbPath, "ured", "ured", "test")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewService(database, ledger.New(database, "ured"), n, Ports{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Inačica koju je drugi čvor javio pamti se uz stanje razmjene; razgovor
// koji pukne prije granice ne briše poznatu inačicu, a uspješna razmjena
// sa starijim programom (koji je ne javlja) ostavlja prazno.
func TestStanjeRazmjenePamtiProgram(t *testing.T) {
	ctx := context.Background()
	s := servisZaTest(t)
	procitaj := func() SyncState {
		t.Helper()
		states, err := s.syncStates(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return states["laptop"]
	}

	s.recordSyncState(ctx, "laptop", syncOutcome{program: "0.0.23-alfa", gradnja: "0.0.23-alfa (abc1234)", frontier: map[string]string{"laptop": "0001"}})
	if st := procitaj(); st.Program != "0.0.23-alfa" || st.Gradnja != "0.0.23-alfa (abc1234)" || st.LastOK == nil {
		t.Fatalf("nakon uspjeha: %+v", st)
	}

	s.recordSyncState(ctx, "laptop", syncOutcome{err: errors.New("veza prekinuta")})
	if st := procitaj(); st.Program != "0.0.23-alfa" || st.Gradnja != "0.0.23-alfa (abc1234)" || st.Fails != 1 {
		t.Fatalf("rana greška ne smije obrisati inačicu: %+v", st)
	}

	s.recordSyncState(ctx, "laptop", syncOutcome{program: "0.0.24-alfa", gradnja: "0.0.24-alfa (def5678)", err: errors.New("pukao nakon granice")})
	if st := procitaj(); st.Program != "0.0.24-alfa" || st.Gradnja != "0.0.24-alfa (def5678)" || st.Fails != 2 {
		t.Fatalf("greška nakon granice pamti inačicu: %+v", st)
	}

	s.recordSyncState(ctx, "laptop", syncOutcome{})
	if st := procitaj(); st.Program != "" || st.Gradnja != "" || st.Fails != 0 || !st.ProgramPoznat {
		t.Fatalf("uspjeh sa starijim programom: %+v", st)
	}

	// redak koji je upisao stariji program (prije stupca) nije dokaz o inačici
	if _, err := s.db.ExecContext(ctx, `INSERT INTO peer_sync (node_id, last_ok) VALUES ('stari-redak', ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.syncStates(ctx); states["stari-redak"].ProgramPoznat {
		t.Fatalf("redak prije nadogradnje: %+v", states["stari-redak"])
	}

	// prvi zapis o čvoru može biti i greška nakon granice
	s.recordSyncState(ctx, "unraid", syncOutcome{program: "0.0.24-alfa", err: errors.New("x")})
	if states, _ := s.syncStates(ctx); states["unraid"].Program != "0.0.24-alfa" {
		t.Fatalf("novi čvor s greškom: %+v", states["unraid"])
	}
}

// Ploča upozori kad drugi čvor radi na drugom izdanju, i kad se uspješno
// razmjenjuje a ne javlja izdanje (stariji program)
func TestUpozorenjaZaInacicuPrograma(t *testing.T) {
	s := servisZaTest(t)
	s.PostaviProgram("0.0.24-alfa", "0.0.24-alfa (abc1234)")
	now := time.Now().UTC()
	st := &Status{Network: &Network{Name: "mreža"}, AutoSync: true, Program: "0.0.24-alfa", LastOK: &now}
	adrese := []string{"10.0.0.2"}
	st.Peers = []PeerStatus{
		{Peer: Peer{NodeID: "a", Name: "Stari laptop", Addresses: adrese}, Member: true, State: SyncState{LastOK: &now, Program: "0.0.23-alfa"}},
		{Peer: Peer{NodeID: "b", Name: "Prastari", Addresses: adrese}, Member: true, State: SyncState{LastOK: &now, ProgramPoznat: true}},
		// redak iz vremena prije bilježenja inačice: o izdanju ne govori ništa
		{Peer: Peer{NodeID: "e", Name: "Prije nadogradnje", Addresses: adrese}, Member: true, State: SyncState{LastOK: &now}},
		{Peer: Peer{NodeID: "c", Name: "Isti", Addresses: adrese}, Member: true, State: SyncState{LastOK: &now, Program: "0.0.24-alfa"}},
		{Peer: Peer{NodeID: "d", Name: "Nikad", Addresses: adrese}, Member: true},
	}
	st.Total = len(st.Peers)
	sve := strings.Join(s.alerts(st, 5*time.Minute, now), "\n")
	for _, treba := range []string{
		"Čvor Stari laptop radi na 0.0.23-alfa, ovaj na 0.0.24-alfa — ažurirajte stariji čvor.",
		"Čvor Prastari radi na verziji starijoj od 0.0.24-alfa (ne javlja verziju) — ažurirajte ga.",
	} {
		if !strings.Contains(sve, treba) {
			t.Errorf("nema upozorenja %q u:\n%s", treba, sve)
		}
	}
	for _, ne := range []string{"Čvor Isti", "Čvor Nikad", "Čvor Prije nadogradnje"} {
		if strings.Contains(sve, ne) {
			t.Errorf("suvišno upozorenje za %q:\n%s", ne, sve)
		}
	}

	if !razlicitProgram("0.0.24-alfa", st.Peers[0].State) || !razlicitProgram("0.0.24-alfa", st.Peers[1].State) ||
		razlicitProgram("0.0.24-alfa", st.Peers[2].State) || razlicitProgram("0.0.24-alfa", st.Peers[3].State) ||
		razlicitProgram("0.0.24-alfa", st.Peers[4].State) {
		t.Error("razlicitProgram ne razlikuje izdanja")
	}
	// program bez poznatog vlastitog izdanja ne uspoređuje ništa
	if razlicitProgram("", st.Peers[0].State) {
		t.Error("bez vlastitog izdanja nema usporedbe")
	}
}

// copZaTest slaže najmanji paket zadane inačice: bez zapisa i sadržaja
func copZaTest(t *testing.T, inacica int) []byte {
	t.Helper()
	zapisi, sadrzaji := []byte{}, []byte("[]")
	m := CopManifest{Inacica: inacica, Otisak: otisakIzdanja(zapisi, nil)}
	mb, _ := json.Marshal(m)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for ime, b := range map[string][]byte{"manifest.json": mb, "zapisi.jsonl": zapisi, "sadrzaji.json": sadrzaji} {
		w, err := z.Create(ime)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(b)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Paket starije inačice se čita, noviji se odbija s porukom da treba
// ažurirati program
func TestCitajCopInacice(t *testing.T) {
	for _, inacica := range []int{CopInacica - 1, CopInacica} {
		b := copZaTest(t, inacica)
		if _, _, _, _, err := CitajCop(bytes.NewReader(b), int64(len(b))); err != nil {
			t.Errorf("inačica %d: %v", inacica, err)
		}
	}
	b := copZaTest(t, CopInacica+1)
	_, _, _, _, err := CitajCop(bytes.NewReader(b), int64(len(b)))
	if err == nil || !strings.Contains(err.Error(), "noviji program") || !strings.Contains(err.Error(), "ažurirajte goCOP") {
		t.Fatalf("noviji paket: %v", err)
	}
}
