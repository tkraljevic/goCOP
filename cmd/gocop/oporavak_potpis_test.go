package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// zadnjaVerzijaRacuna je verzija računa glavniadmin na površini
func zadnjaVerzijaRacuna(t *testing.T, put string) ledger.Version {
	t.Helper()
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	var id string
	if err := baza.QueryRow(`SELECT id FROM users WHERE username = 'glavniadmin'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	v, err := ledger.New(baza, "cvor").Latest(t.Context(), "users", id)
	if err != nil {
		t.Fatal(err)
	}
	return *v
}

// Poništenje s konzole potpisuje verziju ključem čvora uz bazu, pod imenom
// protokola iz postavki (Odluke (26. 11. 2026.), 8). Bez ključa verzija ide
// bez potpisa uz zapis u dnevniku, a ključ se ne stvara; datoteka ključa
// koja se ne da pročitati zaustavi naredbu.
func TestNaredbaPonistiLozinkuPotpisuje(t *testing.T) {
	put := bazaZaOporavak(t)
	node, err := peers.LoadNode(put, "cop-osijek-server", "cop-osijek-server", "test")
	if err != nil {
		t.Fatal(err)
	}
	dnevnik := dnevnikTesta(t)
	if kod := ponistiLozinkuSKonzole(put, "cop-osijek-server", "gocop-sim", "glavniadmin", false, &bytes.Buffer{}); kod != 0 {
		t.Fatalf("izlazni kod %d, dnevnik: %s", kod, dnevnik)
	}
	v := zadnjaVerzijaRacuna(t, put)
	if v.NodeID != "cop-osijek-server" || v.KljucAutora != node.PublicKey() {
		t.Fatalf("verzija u ime %s, ključ %q (čvor %q)", v.NodeID, v.KljucAutora, node.PublicKey())
	}
	if err := razmjena.ProvjeriPotpisVerzije("gocop-sim", peers.PodaciVerzije(v), v.Potpis, v.KljucAutora); err != nil {
		t.Errorf("potpis: %v", err)
	}

	bez := bazaZaOporavak(t)
	dnevnik.Reset()
	if kod := ponistiLozinkuSKonzole(bez, "cvor", "gocop", "glavniadmin", false, &bytes.Buffer{}); kod != 0 {
		t.Fatalf("bez ključa: izlazni kod %d, dnevnik: %s", kod, dnevnik)
	}
	if v := zadnjaVerzijaRacuna(t, bez); v.Potpis != "" || v.KljucAutora != "" {
		t.Errorf("bez ključa verzija ima potpis: %+v", v)
	}
	if !strings.Contains(dnevnik.String(), "bez potpisa") {
		t.Errorf("dnevnik ne kaže da verzija ide bez potpisa: %s", dnevnik)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(bez), peers.KeyFileName)); !os.IsNotExist(err) {
		t.Errorf("naredba je stvorila ključ čvora: %v", err)
	}

	krivi := bazaZaOporavak(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(krivi), peers.KeyFileName), []byte("nije ključ"), 0o600); err != nil {
		t.Fatal(err)
	}
	if kod := ponistiLozinkuSKonzole(krivi, "cvor", "gocop", "glavniadmin", false, &bytes.Buffer{}); kod == 0 {
		t.Error("ključ koji se ne da pročitati, a naredba je prošla")
	}
	if v := zadnjaVerzijaRacuna(t, krivi); v.Potpis != "" {
		t.Errorf("verzija nakon neuspjele naredbe: %+v", v)
	}
}
