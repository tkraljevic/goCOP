package peers_test

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// Datoteka ključa mreže bez zapisa u bazi (baza nastala iznova) ne smije
// zaustaviti osnivanje: osnivanje je preuzme, a ne pregazi.
func TestOsnivanjePreuzimaZateceniKljuc(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gocop.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	stari, err := razmjena.NewNetwork("stara")
	if err != nil {
		t.Fatal(err)
	}
	if err := razmjena.SaveKey(filepath.Join(dir, peers.NetworkKeyFileName), stari.Private()); err != nil {
		t.Fatal(err)
	}
	n, err := peers.LoadNode(dbPath, "ured", "ured", "test")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := peers.NewService(database, ledger.New(database, "ured"), n, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.NetworkInfo() != nil {
		t.Fatal("bez zapisa u bazi čvor ne smije biti u mreži")
	}
	if err := svc.CreateNetwork(ctx, "Hrvatske vode"); err != nil {
		t.Fatalf("osnivanje uz zatečeni ključ: %v", err)
	}
	info := svc.NetworkInfo()
	if info == nil || !info.CanAdmit || info.Name != "Hrvatske vode" {
		t.Fatalf("mreža: %+v", info)
	}
	if info.PublicKey != razmjena.PublicKeyString(stari.Public) {
		t.Error("osnivanje nije preuzelo zatečeni ključ")
	}
}

// Naziv upisan u postavke nakon uparivanja mora doći i do drugih čvorova, a
// javne adrese koje su drugi upisali pritom ostaju.
func TestOsvjeziSebeMijenjaSamoNaziv(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gocop.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	n, err := peers.LoadNode(dbPath, "laptop", "tkraljevic-laptop", "test")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := peers.NewService(database, ledger.New(database, "laptop"), n, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.OsvjeziSebe(ctx); err != nil {
		t.Fatalf("bez zapisa: %v", err)
	}
	if p, _ := svc.GetPeer(ctx, "laptop"); p != nil {
		t.Fatal("bez zatečenog zapisa ne smije nastati novi")
	}
	if err := svc.SavePeer(ctx, peers.Peer{NodeID: "laptop", Name: "Mac.home", PublicKey: n.PublicKey(), Addresses: []string{"https://primjer.hr"}, IsBootstrap: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.OsvjeziSebe(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := svc.GetPeer(ctx, "laptop")
	if p == nil || p.Name != "tkraljevic-laptop" || len(p.Addresses) != 1 || !p.IsBootstrap {
		t.Errorf("zapis nakon osvježavanja: %+v", p)
	}
}
