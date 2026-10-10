package main

import (
	"bytes"
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

// -pripremi upiše ime iz instalacijskog programa prije prvog pokretanja,
// a postojeće ime nikad ne mijenja (ponovna instalacija preko starih podataka)
func TestPripremiUpisujeImeSamoJednom(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DB = filepath.Join(dir, "gocop.db")
	cfg.Node.ID = "pperic-thinkpad"
	var out bytes.Buffer
	if kod := pripremiPostavke(cfg, "", "", &out); kod != 0 {
		t.Fatalf("prvi put: %d %s", kod, out.String())
	}
	put := filepath.Join(dir, config.FileName)
	ucitano, od, err := config.Load([]string{put})
	if err != nil || od != put || ucitano.Node.ID != "pperic-thinkpad" {
		t.Fatalf("ime u postavkama: %q (%s, %v)", ucitano.Node.ID, od, err)
	}

	// ponovna instalacija s drugim imenom: ostaje prvo
	cfg.Node.ID = "drugo-ime"
	out.Reset()
	if kod := pripremiPostavke(cfg, put, "pperic-thinkpad", &out); kod != 0 || !strings.Contains(out.String(), "ostaje pperic-thinkpad") {
		t.Fatalf("drugi put: %s", out.String())
	}
	if u, _, _ := config.Load([]string{put}); u.Node.ID != "pperic-thinkpad" {
		t.Errorf("ime promijenjeno u %q", u.Node.ID)
	}

	// postavke bez imena i baza koja već postoji: staro zadano ime ostaje
	dir2 := t.TempDir()
	put2 := filepath.Join(dir2, config.FileName)
	_ = os.WriteFile(put2, []byte("addr = \":80\"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir2, "gocop.db"), []byte("baza"), 0o644)
	cfg2 := config.Default()
	cfg2.DB = filepath.Join(dir2, "gocop.db")
	cfg2.Node.ID = "novo-ime"
	out.Reset()
	if kod := pripremiPostavke(cfg2, put2, "", &out); kod != 0 || !strings.Contains(out.String(), imecvora.Stari) {
		t.Fatalf("stara baza: %s", out.String())
	}

	// neispravno ime se odbija
	cfg.Node.ID = "Pero Perić"
	if kod := pripremiPostavke(cfg, "", "", &out); kod == 0 {
		t.Error("neispravno ime prihvaćeno")
	}
}

// bazaPoznatogCvora je baza čvora koji je već radio, s ključem uz nju koji
// baza pamti pod imenom ime; gocop.toml nema
func bazaPoznatogCvora(t *testing.T, ime string) string {
	t.Helper()
	put := bazaZaOporavak(t)
	cvor, err := peers.LoadNode(put, "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`INSERT INTO peers (node_id, public_key, created_at) VALUES (?, ?, '2026-10-02T06:00:00Z')`, ime, cvor.PublicKey()); err != nil {
		t.Fatal(err)
	}
	return put
}

// Postojeća baza bez gocop.toml (premješten ili izgubljen), a instalacijska
// skripta na Linuxu pri svakoj instalaciji i nadogradnji pokrene -pripremi:
// primjer se ne zapisuje ni tada, jer bi u datoteci zauvijek ostalo ime
// gocop-cvor umjesto imena koje baza zna (Odluke (8. 10. 2026.), plan
// izdanja 0.0.35, stavka 6). Isto i kad skripta zada -node.
func TestPripremiUzPostojecuBazuBezPostavki(t *testing.T) {
	for _, zastavice := range [][]string{nil, {"-node", "drugo-ime"}} {
		put := bazaPoznatogCvora(t, "pperic-unraid")
		dnevnikTesta(t)
		if kod := run(context.Background(), append([]string{"-pripremi", "-db", put}, zastavice...), strings.NewReader("")); kod != 0 {
			t.Fatalf("%v: izlazni kod %d", zastavice, kod)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(put), config.FileName)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%v: uz postojeću bazu zapisan je primjer postavki: %v", zastavice, err)
		}
		z, err := procitajZastavice([]string{"-db", put})
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, _, err := odrediPostavke(&z)
		if err != nil {
			t.Fatal(err)
		}
		if odrediImeCvora(&cfg); cfg.Node.ID != "pperic-unraid" {
			t.Errorf("%v: nakon -pripremi čvor kreće pod imenom %q", zastavice, cfg.Node.ID)
		}
	}
}

// -pripremi kaže da postavki nema, gdje se očekuju i pod kojim imenom baza
// zna ovaj čvor
func TestPripremiJavljaDaPostavkiNema(t *testing.T) {
	put := bazaPoznatogCvora(t, "pperic-unraid")
	dnevnikTesta(t)
	cfg := config.Default()
	cfg.DB = put
	cfg.Node.ID = "drugo-ime"
	var out bytes.Buffer
	if kod := pripremiPostavke(cfg, "", "", &out); kod != 0 {
		t.Fatalf("izlazni kod %d: %s", kod, out.String())
	}
	for _, mora := range []string{"nema " + filepath.Join(filepath.Dir(put), config.FileName), "Primjer se ne zapisuje", "pperic-unraid", "Vratite izvornu datoteku"} {
		if !strings.Contains(out.String(), mora) {
			t.Errorf("u ispisu nema %q:\n%s", mora, out.String())
		}
	}
	if strings.Contains(out.String(), "drugo-ime") {
		t.Errorf("ispis nudi ime koje se neće koristiti:\n%s", out.String())
	}
}

// Postavke bez imena uz bazu koja zna ključ čvora: -pripremi ne upisuje
// ništa i kaže ime pod kojim će čvor krenuti, ono iz baze
func TestPripremiPostavkeBezImenaUzPoznatuBazu(t *testing.T) {
	put := bazaPoznatogCvora(t, "pperic-unraid")
	dnevnikTesta(t)
	postavke := filepath.Join(filepath.Dir(put), config.FileName)
	if err := os.WriteFile(postavke, []byte("addr = \":80\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DB = put
	cfg.Node.ID = "drugo-ime"
	var out bytes.Buffer
	if kod := pripremiPostavke(cfg, postavke, "", &out); kod != 0 || !strings.Contains(out.String(), "Ime čvora ostaje pperic-unraid") {
		t.Errorf("izlazni kod %d: %s", kod, out.String())
	}
	if b, _ := os.ReadFile(postavke); string(b) != "addr = \":80\"\n" {
		t.Errorf("postavke promijenjene: %q", b)
	}
}
