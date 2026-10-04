package main

// Koraci pokretanja čvora izvan run: primjer postavki uz bazu i otvaranje
// baze sa spremištem sadržaja.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/config"
	"gocop/internal/db"
	"gocop/internal/peers"
	"gocop/internal/sadrzaj"
)

// Bez datoteke postavki uz bazu se jednom zapiše primjer sa zadanim
// vrijednostima i imenom čvora; postojeći primjer se ne dira.
func TestPrimjerPostavkiUzBazu(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DB = filepath.Join(dir, "gocop.db")
	cfg.Node.ID = "pperic-thinkpad"
	cfg.Addr = ":9999" // zastavica ovog pokretanja ne ide u primjer
	dnevnik := dnevnikTesta(t)

	zapisiPrimjerPostavki(cfg, "", "", true, false)
	primjer := filepath.Join(dir, config.FileName)
	b, err := os.ReadFile(primjer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "'pperic-thinkpad'") || strings.Contains(string(b), ":9999") {
		t.Errorf("primjer:\n%s", b)
	}
	if !strings.Contains(dnevnik.String(), "zapisan primjer") || !strings.Contains(dnevnik.String(), "Novi čvor dobio je ime pperic-thinkpad") {
		t.Errorf("dnevnik: %s", dnevnik)
	}

	dnevnik.Reset()
	if err := os.WriteFile(primjer, []byte("# moje\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zapisiPrimjerPostavki(cfg, "", "", false, false)
	if b, _ := os.ReadFile(primjer); string(b) != "# moje\n" {
		t.Errorf("postojeći primjer je prepisan: %q", b)
	}
	if strings.Contains(dnevnik.String(), "zapisan primjer") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Primjer koji se ne da zapisati samo se javi; pokretanje ide dalje.
func TestPrimjerPostavkiNeZapisiv(t *testing.T) {
	dir := t.TempDir()
	zapreka := filepath.Join(dir, "datoteka")
	if err := os.WriteFile(zapreka, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DB = filepath.Join(zapreka, "gocop.db") // mapa baze je datoteka
	dnevnik := dnevnikTesta(t)
	zapisiPrimjerPostavki(cfg, "", "", false, false)
	if !strings.Contains(dnevnik.String(), "primjer datoteke nije zapisan") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Ime koje datoteka postavki nije imala (novo ili iz zastavice) upiše se u
// nju, da preživi pokretanje bez zastavice; ime iz datoteke se ne prepisuje.
func TestImeCvoraUpisanoUPostavke(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, config.FileName)
	if err := os.WriteFile(put, []byte("[node]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DB = filepath.Join(dir, "gocop.db")
	cfg.Node.ID = "pperic-thinkpad"
	dnevnik := dnevnikTesta(t)

	zapisiPrimjerPostavki(cfg, put, "", false, true)
	if b, _ := os.ReadFile(put); !strings.Contains(string(b), "pperic-thinkpad") {
		t.Errorf("ime iz zastavice nije upisano:\n%s", b)
	}
	if !strings.Contains(dnevnik.String(), "čitane iz "+put) {
		t.Errorf("dnevnik: %s", dnevnik)
	}

	if err := os.WriteFile(put, []byte("[node]\nid = \"staro\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zapisiPrimjerPostavki(cfg, put, "staro", true, false)
	if b, _ := os.ReadFile(put); strings.Contains(string(b), "pperic-thinkpad") {
		t.Errorf("ime iz datoteke je prepisano:\n%s", b)
	}

	dnevnik.Reset()
	nema := filepath.Join(dir, "nema", config.FileName)
	zapisiPrimjerPostavki(cfg, nema, "", true, false)
	if !strings.Contains(dnevnik.String(), "ime čvora nije upisano u "+nema) {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// zatvoriSpremiste zatvori spremište sadržaja na kraju testa
func zatvoriSpremiste(t *testing.T, sp *sadrzaj.Spremiste) {
	t.Helper()
	if err := sp.Zatvori(); err != nil {
		t.Error(err)
	}
}

// Baza se otvara sa shemom, spremištem sadržaja i početnim podacima; ono što
// se ne da otvoriti vraća grešku s istom porukom kao prije.
func TestOtvaranjeBaze(t *testing.T) {
	t.Run("ispravno", func(t *testing.T) {
		dnevnik := dnevnikTesta(t)
		baza, sp, err := otvoriBazu(filepath.Join(t.TempDir(), "gocop.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer baza.Close()
		defer zatvoriSpremiste(t, sp)
		var n int
		if err := baza.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil || n == 0 {
			t.Errorf("početni podaci: %d %v", n, err)
		}
		if !strings.Contains(dnevnik.String(), "Spremište sadržaja:") {
			t.Errorf("dnevnik: %s", dnevnik)
		}
	})
	t.Run("nije baza", func(t *testing.T) {
		put := filepath.Join(t.TempDir(), "gocop.db")
		if err := os.WriteFile(put, []byte(strings.Repeat("nije sqlite ", 1000)), 0o644); err != nil {
			t.Fatal(err)
		}
		baza, sp, err := otvoriBazu(put)
		if err == nil || baza != nil || sp != nil {
			t.Fatalf("%v %v %v", baza, sp, err)
		}
		if !strings.HasPrefix(err.Error(), "Kritična greška pri") {
			t.Errorf("poruka: %v", err)
		}
	})
	t.Run("shema se ne da dopuniti", func(t *testing.T) {
		put := filepath.Join(t.TempDir(), "gocop.db")
		tudja, err := db.OpenDB(put)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tudja.Exec(`CREATE TABLE stations (nesto TEXT)`); err != nil {
			t.Fatal(err)
		}
		tudja.Close()
		baza, sp, err := otvoriBazu(put)
		if err == nil || baza != nil || sp != nil || !strings.HasPrefix(err.Error(), "Kritična greška pri inicijalizaciji sheme") {
			t.Fatalf("%v %v %v", baza, sp, err)
		}
	})
	t.Run("mapa baze je datoteka", func(t *testing.T) {
		zapreka := filepath.Join(t.TempDir(), "datoteka")
		if err := os.WriteFile(zapreka, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		baza, sp, err := otvoriBazu(filepath.Join(zapreka, "gocop.db"))
		if err == nil || baza != nil || sp != nil {
			t.Fatalf("%v %v %v", baza, sp, err)
		}
		if !strings.HasPrefix(err.Error(), "Kritična greška pri") {
			t.Errorf("poruka: %v", err)
		}
	})
	t.Run("pokvareni početni podaci", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "organizacija.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		baza, sp, err := otvoriBazu(filepath.Join(dir, "gocop.db"))
		if baza != nil {
			defer baza.Close()
		}
		if sp != nil {
			defer zatvoriSpremiste(t, sp)
		}
		// baza i spremište ostaju otvoreni: pozivatelj ih zatvara kao i inače
		if err == nil || baza == nil || sp == nil || !strings.HasPrefix(err.Error(), "Greška pri unosu početnih podataka") {
			t.Fatalf("%v %v %v", baza, sp, err)
		}
	})
	t.Run("spremište sadržaja", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "sadrzaj.db"), 0o755); err != nil {
			t.Fatal(err)
		}
		baza, sp, err := otvoriBazu(filepath.Join(dir, "gocop.db"))
		if err == nil || baza != nil || sp != nil {
			t.Fatalf("%v %v %v", baza, sp, err)
		}
		if !strings.HasPrefix(err.Error(), "Kritična greška pri otvaranju spremišta sadržaja") {
			t.Errorf("poruka: %v", err)
		}
	})
}

// Čvor od prije uloga zadrži što je radio: izdavao je prognozu u zadnjih
// tjedan dana (vlastito izdanje, ne ono stiglo razmjenom) — i dalje preuzima
// i izdaje; inače ne radi ni jedno.
func TestUlogeOdPrije(t *testing.T) {
	k, _ := okolinaKruga(t, true)
	izdanje := func(prije time.Duration, knjiga string) {
		t.Helper()
		if _, err := k.prognoze.Exec(`INSERT INTO izdanja (izdano, verzija, nastalo, knjiga) VALUES (?, 1, ?, ?)`,
			time.Now().Add(-prije).Unix()/3600, time.Now().Add(-prije).Unix(), knjiga); err != nil {
			t.Fatal(err)
		}
	}
	uloge := func() peers.Uloge { return k.mreza.TrenutneUloge() }

	zadrziUlogeOdPrije(k.prognoze, k.mreza)
	if u := uloge(); u.Izdaje || u.Preuzima {
		t.Fatalf("bez izdanja: %+v", u)
	}
	izdanje(8*24*time.Hour, "")
	izdanje(time.Hour, "pperic-thinkpad:1")
	zadrziUlogeOdPrije(k.prognoze, k.mreza)
	if u := uloge(); u.Izdaje || u.Preuzima {
		t.Fatalf("staro i primljeno izdanje: %+v", u)
	}
	izdanje(2*time.Hour, "")
	zadrziUlogeOdPrije(k.prognoze, k.mreza)
	if u := uloge(); !u.Izdaje || !u.Preuzima {
		t.Fatalf("izdavao je prošli tjedan: %+v", u)
	}

	if _, err := k.baza.Exec(`CREATE TRIGGER kvar_uloga BEFORE UPDATE ON uloge_cvora BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	dnevnik := dnevnikTesta(t)
	zadrziUlogeOdPrije(k.prognoze, k.mreza)
	if !strings.Contains(dnevnik.String(), "Uloge čvora nisu zapisane:") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}
