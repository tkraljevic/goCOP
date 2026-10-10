package main

// Ključ čvora pri pokretanju (Odluke (8. 10. 2026.), plan izdanja 0.0.35,
// stavka 6): postojeći čvor bez ključa ne kreće, a novi identitet dobiva
// samo uz zastavicu -novi-kljuc-cvora. Svjež čvor ključ dobiva sam, što
// zaključava TestZivotCvoraURunu.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/config"
	"gocop/internal/db"
	"gocop/internal/imecvora"
	"gocop/internal/peers"
)

// Baza čvora koji je već radio (knjiga nije prazna), a ključa uz nju nema:
// run javi koja datoteka nedostaje i kako dalje, završi s kodom 1 i ne
// zapiše ništa — ni ključ, ni primjer postavki, ni spremište sadržaja.
func TestPostojeciCvorBezKljucaNeKreceURunu(t *testing.T) {
	put := bazaZaOporavak(t)
	dir := filepath.Dir(put)
	dnevnik := dnevnikTesta(t)
	// otkazan kontekst: bez provjere čvor bi se podigao i odmah ugasio s 0
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	kod := run(ctx, []string{
		"-db", put, "-addr", "127.0.0.1:0", "-sync-port", "0", "-discovery-port", "0",
		"-auto-sync", "0", "-pakete", "", "-podaci", "",
	}, strings.NewReader(""))
	if kod != 1 {
		t.Errorf("izlazni kod %d, očekivano 1", kod)
	}
	for _, mora := range []string{"Čvor se ne pokreće", filepath.Join(dir, "node-key"), "novi identitet", "-novi-kljuc-cvora",
		"Uz bazu nema ni postavki " + filepath.Join(dir, config.FileName)} {
		if !strings.Contains(dnevnik.String(), mora) {
			t.Errorf("u dnevniku nema %q:\n%s", mora, dnevnik)
		}
	}
	for _, datoteka := range []string{"node-key", "gocop.toml", "sadrzaj.db"} {
		if _, err := os.Stat(filepath.Join(dir, datoteka)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("odbijeno pokretanje zapisalo je %s: %v", datoteka, err)
		}
	}
}

// Uz -novi-kljuc-cvora isti čvor kreće i dobiva novi ključ. Postavki uz
// bazu nema, a baza postoji, pa se primjer postavki ne zapisuje.
func TestNoviKljucCvoraURunu(t *testing.T) {
	if testing.Short() {
		t.Skip("podiže cijeli čvor")
	}
	put := bazaZaOporavak(t)
	_, _, ugasi := cvorURunu(t, "-db", put, "-node", "pperic-thinkpad", "-novi-kljuc-cvora")
	if _, err := os.Stat(filepath.Join(filepath.Dir(put), "node-key")); err != nil {
		t.Errorf("novi ključ nije napravljen: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(put), config.FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("uz postojeću bazu zapisan je primjer postavki: %v", err)
	}
	if kod := ugasi(); kod != 0 {
		t.Errorf("gašenje s kodom %d", kod)
	}
}

// Postojeća baza bez upisanog imena (gocop.toml je premješten) uzima ime pod
// kojim pamti ključ ovog čvora; bez ključa, ili kad ga baza ne zna, ostaje
// staro zadano ime, kao i dosad.
func TestImeCvoraIzBaze(t *testing.T) {
	put := bazaZaOporavak(t)
	dnevnik := dnevnikTesta(t)
	cfg := config.Config{DB: put}
	if odrediImeCvora(&cfg) || cfg.Node.ID != imecvora.Stari {
		t.Errorf("bez ključa: %q", cfg.Node.ID)
	}
	cvor, err := peers.LoadNode(put, "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Node.ID = ""
	if odrediImeCvora(&cfg) || cfg.Node.ID != imecvora.Stari {
		t.Errorf("baza ne zna ključ: %q", cfg.Node.ID)
	}

	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	_, err = baza.Exec(`INSERT INTO peers (node_id, public_key, created_at) VALUES ('pperic-unraid', ?, '2026-10-02T06:00:00Z')`, cvor.PublicKey())
	baza.Close()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Node.ID = ""
	if odrediImeCvora(&cfg) || cfg.Node.ID != "pperic-unraid" {
		t.Errorf("ime iz baze: %q", cfg.Node.ID)
	}
	if !strings.Contains(dnevnik.String(), "Ime čvora pperic-unraid pročitano iz baze") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Baza koja se ne da pročitati ne daje ime: vrijedi staro zadano, uz zapis
// u dnevniku
func TestImeCvoraIzNecitljiveBaze(t *testing.T) {
	put := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(put, []byte(strings.Repeat("nije sqlite ", 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := peers.LoadNode(put, "", "", "test"); err != nil {
		t.Fatal(err)
	}
	dnevnik := dnevnikTesta(t)
	cfg := config.Config{DB: put}
	if odrediImeCvora(&cfg) || cfg.Node.ID != imecvora.Stari {
		t.Errorf("nečitljiva baza: %q", cfg.Node.ID)
	}
	if !strings.Contains(dnevnik.String(), "ne može se pročitati pod kojim imenom zna ključ čvora") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Uz postojeću bazu bez postavki primjer se ne zapisuje (zamijenio bi
// izvorne postavke koje su premještene); dnevnik kaže koja datoteka
// nedostaje i što učiniti.
func TestPrimjerPostavkiUzPostojecuBazu(t *testing.T) {
	put := bazaZaOporavak(t)
	cfg := config.Default()
	cfg.DB = put
	cfg.Node.ID = "pperic-unraid"
	dnevnik := dnevnikTesta(t)
	zapisiPrimjerPostavki(cfg, "", "", false, false)
	primjer := filepath.Join(filepath.Dir(put), config.FileName)
	if _, err := os.Stat(primjer); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("uz postojeću bazu zapisan je primjer: %v", err)
	}
	for _, mora := range []string{"nema " + primjer, "Primjer se ne zapisuje", "pperic-unraid", "Vratite izvornu datoteku"} {
		if !strings.Contains(dnevnik.String(), mora) {
			t.Errorf("u dnevniku nema %q:\n%s", mora, dnevnik)
		}
	}
}

// Kad uz ključ nema ni gocop.toml (Unraid, 5. 10. 2026.: mover je odnio
// oboje), odbijanje kaže i za postavke, a novi identitet uz zastavicu javlja
// pod kojim imenom čvor kreće i kako zadržati dosadašnje. Kad su postavke
// pročitane, o njima se ne govori.
func TestProvjeraKljucaBezPostavki(t *testing.T) {
	put := bazaZaOporavak(t)
	postavke := filepath.Join(filepath.Dir(put), config.FileName)
	cfg := config.Config{DB: put}
	cfg.Node.ID = "pperic-thinkpad"

	var nema *peers.KljucNedostaje
	if err := provjeriKljuc(cfg, "", false); !errors.As(err, &nema) || nema.Postavke != postavke {
		t.Errorf("bez postavki: %v", err)
	}
	if err := provjeriKljuc(cfg, postavke, false); !errors.As(err, &nema) || nema.Postavke != "" {
		t.Errorf("s postavkama: %v", err)
	}

	dnevnik := dnevnikTesta(t)
	if err := provjeriKljuc(cfg, postavke, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dnevnik.String(), "nema ni postavki") {
		t.Errorf("postavke su pročitane, a dnevnik kaže da ih nema: %s", dnevnik)
	}
	if err := provjeriKljuc(cfg, "", true); err != nil {
		t.Fatal(err)
	}
	for _, mora := range []string{"Ime čvora: uz bazu nema ni postavki " + postavke, "kreće pod imenom pperic-thinkpad", "id = \"…\" pod [node]"} {
		if !strings.Contains(dnevnik.String(), mora) {
			t.Errorf("u dnevniku nema %q:\n%s", mora, dnevnik)
		}
	}
}
