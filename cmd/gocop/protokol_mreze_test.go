package main

// Ime protokola mreže pri pokretanju: baza pamti ime pod kojim je ušla u
// mrežu, pa kopirana mapa podataka pokrenuta s drugim [sync] protokol ne
// kreće (peers.ProvjeriProtokolMreze).

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/config"
	"gocop/internal/db"
	"gocop/internal/peers"
)

// mapa0039 je mapa podataka čvora koji je radio na 0.0.39 i ušao u mrežu:
// baza s podacima i retkom mreže, bez stupca protokol, i ključ čvora uz nju
func mapa0039(t *testing.T) string {
	t.Helper()
	put := bazaZaOporavak(t)
	if _, err := peers.LoadNode(put, "ured-vukovar", "Ured Vukovar", "test"); err != nil {
		t.Fatal(err)
	}
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	for _, upit := range []string{
		`ALTER TABLE network DROP COLUMN protokol`,
		`INSERT INTO network (id, name, public_key, joined_at) VALUES (1, 'Hrvatske vode', 'izmisljeni-kljuc', '2026-10-18 07:00:00')`,
	} {
		if _, err := baza.Exec(upit); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// imaStupacProtokol javlja ima li tablica network stupac protokol
func imaStupacProtokol(t *testing.T, put string) bool {
	t.Helper()
	baza, err := sql.Open("sqlite", put+"?_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	var ima bool
	if err := baza.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('network') WHERE name = 'protokol'`).Scan(&ima); err != nil {
		t.Fatal(err)
	}
	return ima
}

// Zadano ime protokola isto je kao ime prave mreže u razmjeni, a postavka
// se čita točno kako je upisana: bez skraćivanja i pretvaranja u mala slova
func TestZadaniProtokolMreze(t *testing.T) {
	if p := config.Default().Sync.Protokol; p != peers.Protocol {
		t.Errorf("zadani [sync] protokol %q, a razmjena radi pod %q", p, peers.Protocol)
	}
	for _, s := range []struct{ tekst, protokol string }{
		{"", peers.Protocol},
		{"[sync]\nauto_sync = '1h'\n", peers.Protocol},
		{"[sync]\nprotokol = 'gocop-sim'\n", "gocop-sim"},
		{"[sync]\nprotokol = ' Gocop '\n", " Gocop "},
	} {
		cfg, err := config.ProcitajTekst([]byte(s.tekst))
		if err != nil || cfg.Sync.Protokol != s.protokol {
			t.Errorf("%q: %q (%v)", s.tekst, cfg.Sync.Protokol, err)
		}
	}
}

// Pokretanje prvo provjeri ključ čvora, pa ime protokola: postojeći čvor bez
// ključa odbija se kao i do sada, a baza mreže „gocop” s postavkom
// „gocop-sim” kaže da pripada drugoj mreži. Neispravno ime ne kreće. Bazu
// koja se ne da pročitati provjera pušta dalje, a ne otvori se ni poslije,
// pa čvor stane kao i do sada.
func TestProvjeraPokretanja(t *testing.T) {
	bezKljuca := config.Default()
	bezKljuca.DB = bazaZaOporavak(t)
	bezKljuca.Sync.Protokol = "gocop-sim"
	var nema *peers.KljucNedostaje
	if err := provjeriPokretanje(bezKljuca, "", false); !errors.As(err, &nema) {
		t.Errorf("baza bez ključa: %v", err)
	}

	cfg := config.Default()
	cfg.DB = mapa0039(t)
	postavke := filepath.Join(filepath.Dir(cfg.DB), config.FileName)
	if err := provjeriPokretanje(cfg, postavke, false); err != nil {
		t.Errorf("zadano ime uz bazu 0.0.39: %v", err)
	}
	cfg.Sync.Protokol = "gocop-sim"
	var drugi *peers.ProtokolDrugeMreze
	if err := provjeriPokretanje(cfg, postavke, false); !errors.As(err, &drugi) || drugi.Upisan != "gocop" {
		t.Errorf("gocop-sim uz bazu 0.0.39: %v", err)
	}
	cfg.Sync.Protokol = ""
	if err := provjeriPokretanje(cfg, postavke, false); err == nil || !strings.Contains(err.Error(), "smije imati samo mala slova") {
		t.Errorf("prazno ime: %v", err)
	}

	smece := config.Default()
	smece.DB = mapa0039(t)
	if err := os.WriteFile(smece.DB, []byte("ovo nije baza podataka nego izmišljeni tekst\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dnevnikTesta(t)
	if err := provjeriPokretanje(smece, filepath.Join(filepath.Dir(smece.DB), config.FileName), false); err != nil {
		t.Errorf("datoteka koja nije baza: %v", err)
	}
	if baza, spremiste, err := otvoriBazu(smece.DB); err == nil || baza != nil || spremiste != nil {
		t.Errorf("datoteka koja nije baza otvorila se: %v", err)
	}
}

// Kopirana mapa podataka čvora iz 0.0.39 pokrenuta uz [sync] protokol =
// 'gocop-sim': run kaže kojoj mreži mapa pripada, završi s kodom 1 i ne
// zapiše ništa (ni stupac protokol, ni spremište sadržaja, ni postavke).
func TestKopiranaMapaSDrugimProtokolomNeKreceURunu(t *testing.T) {
	put := mapa0039(t)
	dir := filepath.Dir(put)
	postavke := []byte("[node]\nid = 'ured-vukovar'\n\n[sync]\nprotokol = 'gocop-sim'\n")
	if err := os.WriteFile(filepath.Join(dir, config.FileName), postavke, 0o644); err != nil {
		t.Fatal(err)
	}
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
	poruka := "goCOP nije pokrenut: ova mapa podataka pripada mreži „gocop”, a u gocop.toml piše [sync] protokol = „gocop-sim”. Ispravi postavku ili pokreni goCOP s drugom mapom podataka."
	if !strings.Contains(dnevnik.String(), poruka) {
		t.Errorf("u dnevniku nema %q:\n%s", poruka, dnevnik)
	}
	if imaStupacProtokol(t, put) {
		t.Error("odbijeno pokretanje dodalo je stupac protokol")
	}
	if _, err := os.Stat(filepath.Join(dir, "sadrzaj.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("odbijeno pokretanje zapisalo je sadrzaj.db: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, config.FileName)); err != nil || string(b) != string(postavke) {
		t.Errorf("odbijeno pokretanje mijenjalo je postavke (%v):\n%s", err, b)
	}
}
