package peers_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// startNodeBezRegistra diže čvor bez registara iz data/: dovoljno za
// razmjenu sadržaja, a test ne ovisi o datotekama izvan repozitorija
func startNodeBezRegistra(t *testing.T, ctx context.Context, id string) *node {
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
	return &node{id: id, db: database, svc: svc, rec: rec}
}

// Sadržaj putuje po otisku prema razini pretplate: čvor koji prati samo
// kazalo primi zapis izvornika bez bajtova; kad pretplatu digne na "sve",
// sljedeća razmjena donese PDF, provjeren otiskom.
func TestSadrzajPutujePremaRaziniPretplate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)

	// A drži PDF u svom spremištu; B-ovo spremište je ono koje primjena
	// verzija koristi (jedno po programu), pa se A-ovo drži zasebno
	spA, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer spA.Zatvori()
	spB, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer spB.Zatvori()
	a.svc.SetSpremiste(spA)
	b.svc.SetSpremiste(spB)
	repository.SetSpremiste(spB)
	defer repository.SetSpremiste(nil)
	a.svc.SetWantsAll(true)

	pdf := []byte("%PDF-1.4\nizvornik prijave s Osijeka")
	kanal := "prijave/16/2026"
	otisak, err := spA.Upisi(ctx, "application/pdf", pdf, "ovdje", sadrzaj.Veza{Entitet: repository.EntityPrijave, EntitetID: "p1", Uloga: "izvornik", Kanal: kanal})
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := a.db.BeginTx(ctx, nil)
	if _, err := a.rec.RecordIn(ctx, tx, kanal, repository.EntityPrijaveIzvornici, "p1",
		models.IzvornikLista{ListID: "p1", Otisak: otisak, Bajtova: len(pdf), Vrsta: "application/pdf", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()

	// B prati prijave BP 16, ali samo kazalo
	b.svc.SetWantsAll(false)
	pravilo, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "prijave", AreaID: 16, Razina: peers.RazinaKazalo})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	var zapisa int
	_ = b.db.QueryRow(`SELECT count(*) FROM prijave_izvornici WHERE prijava_id = 'p1' AND otisak = ?`, otisak).Scan(&zapisa)
	if zapisa != 1 {
		t.Fatalf("B nije primio zapis izvornika: %d", zapisa)
	}
	if spB.Ima(ctx, otisak) {
		t.Fatal("B je primio sadržaj iako prati samo kazalo")
	}
	if z, _ := spB.Zeljeni(ctx, 10); len(z) != 1 || z[0].Kanal != kanal {
		t.Fatalf("B ne zna što mu nedostaje: %+v", z)
	}

	// pretplata na sve: sljedeća razmjena donese bajtove
	if err := b.svc.RemoveSubscription(ctx, pravilo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "prijave", AreaID: 16, Razina: peers.RazinaSve, DrziDana: 90}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("druga razmjena: %v", err)
	}
	got, _, err := spB.Citaj(ctx, otisak)
	if err != nil || string(got) != string(pdf) {
		t.Fatalf("B nije dobio PDF: %v", err)
	}
	if z, _ := spB.Zeljeni(ctx, 10); len(z) != 0 {
		t.Errorf("želja nije nestala: %+v", z)
	}
	// rok držanja još nije istekao, pa otpuštanje ništa ne dira
	if n, _, err := b.svc.OtpustiStare(ctx); err != nil || n != 0 {
		t.Errorf("otpušteno prerano: %d %v", n, err)
	}
	// A ne traži ništa natrag (ima sve), i razmjena u drugom smjeru prolazi
	if _, _, err := a.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("razmjena A→B: %v", err)
	}
}
