package peers_test

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// node je jedan goCOP čvor u testu: vlastita baza, ključ, portovi
type node struct {
	id       string
	svc      *peers.Service
	rec      *ledger.Recorder
	stations *repository.StationRepository
}

func startNode(t *testing.T, ctx context.Context, id string) *node {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gocop.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedInitialData(database); err != nil {
		t.Fatal(err)
	}

	rec := ledger.New(database, id)
	n, err := peers.LoadNode(dbPath, id, "test-"+id, "test")
	if err != nil {
		t.Fatal(err)
	}
	ports := peers.Ports{Exchange: freePort(t), Pair: freePort(t), Discovery: 0}
	svc, err := peers.NewService(database, rec, n, ports)
	if err != nil {
		t.Fatal(err)
	}
	svc.OnApplied(func(ctx context.Context, versions []ledger.Version) error {
		return repository.ApplyVersions(ctx, database, rec, versions)
	})
	go svc.Serve(ctx)

	return &node{id: id, svc: svc, rec: rec, stations: repository.NewStationRepository(database, rec)}
}

// pair upari dva čvora kao što bi to dva čovjeka učinila: jedan čeka,
// drugi nazove, oba vide isti kod i oba potvrde
func pair(t *testing.T, ctx context.Context, a, b *node) {
	t.Helper()
	if err := a.svc.StartListening(); err != nil {
		t.Fatal(err)
	}
	var err error
	for i := 0; i < 50; i++ {
		err = b.svc.DialPair(ctx, fmt.Sprintf("127.0.0.1:%d", a.svc.Ports().Pair))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("nazivanje: %v", err)
	}
	for i := 0; i < 50 && !a.svc.PairStatus().Pending; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	sa, sb := a.svc.PairStatus(), b.svc.PairStatus()
	if !sa.Pending || !sb.Pending || sa.SAS != sb.SAS {
		t.Fatalf("oba ekrana moraju čekati s istim kodom: a=%+v b=%+v", sa, sb)
	}

	type result struct {
		out peers.PairOutcome
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := a.svc.ConfirmPair(ctx, true); done <- result{out, err} }()
	outB, err := b.svc.ConfirmPair(ctx, true)
	if err != nil || !outB.Paired {
		t.Fatalf("potvrda na B: %+v err=%v", outB, err)
	}
	ra := <-done
	if ra.err != nil || !ra.out.Paired {
		t.Fatalf("potvrda na A: %+v err=%v", ra.out, ra.err)
	}
	t.Logf("uparivanje: A → %q | B → %q", ra.out.Message, outB.Message)
}

// founder osniva mrežu na čvoru — prvi korak prije ikakve razmjene
func founder(t *testing.T, ctx context.Context, n *node, name string) {
	t.Helper()
	if err := n.svc.CreateNetwork(ctx, name); err != nil {
		t.Fatalf("osnivanje mreže: %v", err)
	}
	if info := n.svc.NetworkInfo(); info == nil || !info.CanAdmit {
		t.Fatalf("osnivač mora držati ključ mreže: %+v", info)
	}
}

func stationNote(t *testing.T, n *node, name string) string {
	t.Helper()
	list, err := n.stations.ListStations(context.Background(), name, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Name == name {
			return s.Notes
		}
	}
	t.Fatalf("postaja %q nije na čvoru %s", name, n.id)
	return ""
}

func editStationNote(t *testing.T, n *node, name, note string) {
	t.Helper()
	ctx := context.Background()
	list, _ := n.stations.ListStations(ctx, name, "", false)
	for _, s := range list {
		if s.Name == name {
			s.Notes = note
			if err := n.stations.UpdateStation(ctx, &s); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("postaja %q nije na čvoru %s", name, n.id)
}

// Dva čvora, svaki sa svojom bazom i ključem: upare se preko loopbacka,
// jedan promijeni postaju bez znanja drugog, razmjena to prenese, a
// površina na drugom čvoru pokazuje promjenu. Ponovljena razmjena ne
// mijenja ništa.
func TestDvaCvoraSeUpareISinkroniziraju(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startNode(t, ctx, "cop-osijek")
	b := startNode(t, ctx, "laptop-vinkovci")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)

	if info := b.svc.NetworkInfo(); info == nil || info.Name != "Hrvatske vode" || info.CanAdmit {
		t.Fatalf("B je morao biti primljen u mrežu kao član bez ključa: %+v", info)
	}

	pa, _ := a.svc.ListPeers(ctx)
	pb, _ := b.svc.ListPeers(ctx)
	if len(pa) != 1 || len(pb) != 1 || pa[0].NodeID != b.id || pb[0].NodeID != a.id {
		t.Fatalf("nakon uparivanja svaki mora znati za drugoga: a=%+v b=%+v", pa, pb)
	}

	// A mijenja Županju; B to još ne zna
	editStationNote(t, a, "Županja", "izmjena na Osijeku")
	if got := stationNote(t, b, "Županja"); got == "izmjena na Osijeku" {
		t.Fatal("B ne bi smio znati za izmjenu prije razmjene")
	}

	// B nazove A — razmjena
	applied, sent, err := b.svc.SyncWith(ctx, a.id)
	if err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	if applied == 0 {
		t.Errorf("B je morao primiti barem jednu verziju, primio %d (poslao %d)", applied, sent)
	}
	if got := stationNote(t, b, "Županja"); got != "izmjena na Osijeku" {
		t.Errorf("površina na B nije osvježena: napomena %q", got)
	}

	// ista razmjena još jednom: ništa novo
	applied, _, err = b.svc.SyncWith(ctx, a.id)
	if err != nil {
		t.Fatalf("ponovljena razmjena: %v", err)
	}
	if applied != 0 {
		t.Errorf("ponovljena razmjena ne smije primiti ništa, primila %d", applied)
	}

	// obrnuti smjer: B mijenja, A nazove B
	editStationNote(t, b, "Županja", "ispravak iz Vinkovaca")
	if _, _, err := a.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("razmjena A→B: %v", err)
	}
	if got := stationNote(t, a, "Županja"); got != "ispravak iz Vinkovaca" {
		t.Errorf("površina na A nije osvježena: napomena %q", got)
	}

	// povijest na oba čvora nosi obje izmjene, s izvornim autorima
	var stID string
	list, _ := a.stations.ListStations(ctx, "Županja", "", false)
	for _, s := range list {
		if s.Name == "Županja" {
			stID = s.ID.String()
		}
	}
	for _, n := range []*node{a, b} {
		hist, _ := n.rec.History(ctx, repository.EntityStations, stID)
		if len(hist) < 2 || hist[0].NodeID != b.id || hist[1].NodeID != a.id {
			t.Errorf("povijest na %s: %d verzija, autori %v", n.id, len(hist), authors(hist))
		}
	}
}

// Nepoznat čvor — ispravan ključ, ali nikad uparen — ne dobiva razmjenu
func TestNeupareniCvorNeDobivaRazmjenu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a := startNode(t, ctx, "cop-osijek")
	stranger := startNode(t, ctx, "nepoznati")
	founder(t, ctx, a, "Hrvatske vode")
	founder(t, ctx, stranger, "Tuđa mreža")

	// stranac "zna" za A (ručno upisana adresa i ključ), ali A ne zna za njega
	if err := stranger.svc.SavePeer(ctx, peers.Peer{
		NodeID: a.id, Name: "A", PublicKey: a.svc.Node().PublicKey(),
		Addresses: []string{fmt.Sprintf("127.0.0.1:%d", a.svc.Ports().Exchange)},
	}); err != nil {
		t.Fatal(err)
	}
	editStationNote(t, a, "Županja", "tajna izmjena")

	if _, _, err := stranger.svc.SyncWith(ctx, a.id); err == nil {
		t.Error("neupareni čvor je dobio razmjenu — A ga je morao odbiti na vratima")
	}
	if got := stationNote(t, stranger, "Županja"); got == "tajna izmjena" {
		t.Error("izmjena je procurila neuparenom čvoru")
	}
}

// Arhiviranje putuje: veza uklonjena na jednom čvoru nestaje i na drugom
func TestArhiviranjePutujeMedjuCvorovima(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startNode(t, ctx, "cop-osijek")
	b := startNode(t, ctx, "laptop-vinkovci")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)

	var st models.Station
	list, _ := a.stations.ListStations(ctx, "Županja", "", false)
	for _, s := range list {
		if s.Name == "Županja" {
			st = s
		}
	}
	removed := st.SectionCodes[0]
	if err := a.stations.UnlinkStationFromSection(ctx, removed, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}

	onB, _ := b.stations.GetSectionCodesForStation(ctx, st.ID)
	for _, c := range onB {
		if c == removed {
			t.Errorf("veza %s uklonjena na A još postoji na B", removed)
		}
	}
	if len(onB) != len(st.SectionCodes)-1 {
		t.Errorf("B ima %d veza, očekivano %d", len(onB), len(st.SectionCodes)-1)
	}
}

func authors(h []ledger.Version) []string {
	var out []string
	for _, v := range h {
		out = append(out, v.NodeID)
	}
	return out
}

// Član bez ključa mreže može upariti outsidera, ali ga NE MOŽE primiti:
// outsider ostaje poznat, a razmjena mu se odbija na svakom čvoru.
// To je scenarij "jedan admin uparuje pogrešan laptop".
func TestClanBezKljucaNeMozePrimitiOutsidera(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startNode(t, ctx, "cop-osijek")
	b := startNode(t, ctx, "laptop-vinkovci")
	outsider := startNode(t, ctx, "tudji-laptop")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b) // B je član, bez ključa

	// B (bez ključa) uparuje outsidera
	if err := b.svc.StartListening(); err != nil {
		t.Fatal(err)
	}
	var err error
	for i := 0; i < 50; i++ {
		if err = outsider.svc.DialPair(ctx, fmt.Sprintf("127.0.0.1:%d", b.svc.Ports().Pair)); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50 && !b.svc.PairStatus().Pending; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	type result struct {
		out peers.PairOutcome
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := b.svc.ConfirmPair(ctx, true); done <- result{out, err} }()
	outO, _ := outsider.svc.ConfirmPair(ctx, true)
	rb := <-done
	if !rb.out.Paired || rb.out.Member {
		t.Errorf("B je uparen s outsiderom ali ga ne smije primiti: %+v", rb.out)
	}
	if outsider.svc.NetworkInfo() != nil {
		t.Errorf("outsider ne smije dobiti mrežu od člana bez ključa: %+v (poruka: %q)", outsider.svc.NetworkInfo(), outO.Message)
	}

	// razmjena je odbijena u oba smjera
	editStationNote(t, b, "Županja", "povjerljivo")
	if _, _, err := outsider.svc.SyncWith(ctx, b.id); err == nil {
		t.Error("outsider je dobio razmjenu od člana")
	}
	if _, _, err := b.svc.SyncWith(ctx, outsider.id); err == nil {
		t.Error("član je razmijenio s outsiderom")
	}
	if got := stationNote(t, outsider, "Županja"); got == "povjerljivo" {
		t.Error("podatak je procurio outsideru")
	}
}

// Dvije organizacije s istim programom: uparivanje uspije kao ceremonija,
// ali nijedna ne prima drugu — dva mrežna ključa nemaju ništa zajedničko
func TestDvijeMrezeNemajuNistaZajednicko(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	hv := startNode(t, ctx, "cop-osijek")
	hu := startNode(t, ctx, "vizugy-budapest")
	founder(t, ctx, hv, "Hrvatske vode")
	founder(t, ctx, hu, "Országos Vízügyi")

	if err := hv.svc.StartListening(); err != nil {
		t.Fatal(err)
	}
	var err error
	for i := 0; i < 50; i++ {
		if err = hu.svc.DialPair(ctx, fmt.Sprintf("127.0.0.1:%d", hv.svc.Ports().Pair)); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50 && !hv.svc.PairStatus().Pending; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	type result struct {
		out peers.PairOutcome
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := hv.svc.ConfirmPair(ctx, true); done <- result{out, err} }()
	outHU, _ := hu.svc.ConfirmPair(ctx, true)
	rHV := <-done
	if rHV.out.Member || outHU.Member {
		t.Errorf("čvorovi različitih mreža ne smiju postati članovi: HV=%+v HU=%+v", rHV.out, outHU)
	}
	if hv.svc.NetworkInfo().Name != "Hrvatske vode" || hu.svc.NetworkInfo().Name != "Országos Vízügyi" {
		t.Error("uparivanje ne smije promijeniti mrežu nijednog čvora")
	}
	if _, _, err := hu.svc.SyncWith(ctx, hv.id); err == nil {
		t.Error("razmjena između mreža je prošla")
	}
}

// Opoziv putuje: nakon opoziva na A i sinkronizacije, B više ne prima
// opozvanog — a opozvani ne prima nikoga
func TestOpozivClanstvaPutuje(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	a := startNode(t, ctx, "cop-osijek")
	b := startNode(t, ctx, "laptop-vinkovci")
	c := startNode(t, ctx, "laptop-zupanja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	pair(t, ctx, a, c)

	// B i C se međusobno uparuju kao dva člana (nijedan nema ključ) — članovi su
	if err := b.svc.StartListening(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := c.svc.DialPair(ctx, fmt.Sprintf("127.0.0.1:%d", b.svc.Ports().Pair)); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for i := 0; i < 50 && !b.svc.PairStatus().Pending; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	go b.svc.ConfirmPair(ctx, true)
	if out, _ := c.svc.ConfirmPair(ctx, true); !out.Member {
		t.Fatalf("dva člana iste mreže moraju si vjerovati nakon uparivanja: %+v", out)
	}
	if _, _, err := c.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("članovi B i C moraju razmjenjivati: %v", err)
	}

	// A opoziva C; B to sazna sinkronizacijom s A
	if err := a.svc.RevokeMembership(ctx, c.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.svc.SyncWith(ctx, b.id); err == nil {
		t.Error("opozvani čvor je i dalje dobio razmjenu od B")
	}
	if _, _, err := b.svc.SyncWith(ctx, c.id); err == nil {
		t.Error("B je razmijenio s opozvanim čvorom")
	}
}
