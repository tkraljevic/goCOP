package main

// Djelatnici iz imenika ulaze u knjigu verzija na računalu koje je
// osnovalo mrežu: pri pokretanju, kad osnuje mrežu i kad se popis početnih
// lozinki preuzme ili mu prođe rok (Odluke (26. 11. 2026.)). Greška tog
// upisa ide samo u dnevnik i ne ruši preuzimanje popisa.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
)

// bazaSImenikom otvara novu bazu uz imenik s jednim računom, kao pri
// prvom pokretanju, s popisom lozinki koji čeka, na računalu koje je zatim
// osnovalo mrežu
func bazaSImenikom(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	imenik := `[{"username":"pperic","full_name":"Pero Perić","is_global_admin":true}]`
	if err := os.WriteFile(filepath.Join(dir, "imenik.json"), []byte(imenik), 0o600); err != nil {
		t.Fatal(err)
	}
	baza, sp, popis, err := otvoriBazu(filepath.Join(dir, "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { zatvoriBazu(t, baza) })
	t.Cleanup(func() { zatvoriSpremiste(t, sp) })
	if len(popis) == 0 {
		t.Fatalf("popis lozinki: %+v", popis)
	}
	upisiOsnovanuMrezu(t, baza)
	return baza
}

func zatvoriBazu(t *testing.T, baza *sql.DB) {
	t.Helper()
	if err := baza.Close(); err != nil {
		t.Error(err)
	}
}

// upisiOsnovanuMrezu upisuje mrežu kao da ju je ovo računalo osnovalo
// (bez datoteke ključa: računalo mrežu zna samo po javnom ključu)
func upisiOsnovanuMrezu(t *testing.T, baza *sql.DB) {
	t.Helper()
	kljuc, err := razmjena.NewNetwork("Primjerska mreža")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO network (id, name, public_key, joined_at) VALUES (1, ?, ?, ?)`,
		kljuc.Name, razmjena.PublicKeyString(kljuc.Public), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func cekajuLozinke(t *testing.T, baza *sql.DB) int {
	t.Helper()
	var n int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM pocetne_lozinke_cekaju`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNakonPopisaLozinkiUpisujeDjelatnike(t *testing.T) {
	baza := bazaSImenikom(t)
	dnevnik := dnevnikTesta(t)
	rec := ledger.New(baza, "cop-primjerovo")
	upisi := func(ctx context.Context) (int, error) {
		return repository.NewUserRepository(baza, rec).UpisiPunjenje(ctx, repository.NewOrgRepository(baza, rec))
	}

	if err := nakonPopisaLozinki(baza, upisi)(); err != nil {
		t.Fatal(err)
	}
	if n := cekajuLozinke(t, baza); n != 0 {
		t.Errorf("popis još čeka (%d računa)", n)
	}
	var verzija int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = 'users' AND entity_id = ?`,
		db.StableID("user", "pperic").String()).Scan(&verzija); err != nil {
		t.Fatal(err)
	}
	if verzija != 1 || !strings.Contains(dnevnik.String(), "upisano je za razmjenu") {
		t.Errorf("račun pperic ima %d verzija, dnevnik %q", verzija, dnevnik.String())
	}
}

// Greška upisa u knjigu ne vraća se preuzimanju popisa, pa se popis
// pokaže, a greška ostane u dnevniku
func TestNakonPopisaLozinkiGreskaUpisaSamoUDnevnik(t *testing.T) {
	baza := bazaSImenikom(t)
	dnevnik := dnevnikTesta(t)
	pozvan := false
	upisi := func(context.Context) (int, error) {
		pozvan = true
		return 0, errors.New("knjiga je zaključana")
	}

	if err := nakonPopisaLozinki(baza, upisi)(); err != nil {
		t.Fatalf("greška upisa vraćena je preuzimanju popisa: %v", err)
	}
	if !pozvan || cekajuLozinke(t, baza) != 0 {
		t.Fatalf("upis pozvan %v, popis čeka %d", pozvan, cekajuLozinke(t, baza))
	}
	if !strings.Contains(dnevnik.String(), "knjiga je zaključana") {
		t.Errorf("greška nije u dnevniku: %q", dnevnik.String())
	}
}

// Kad se popis ne da obrisati iz baze, preuzimanje to javlja, a upisa nema
func TestNakonPopisaLozinkiBezBrisanjaNemaUpisa(t *testing.T) {
	baza := bazaSImenikom(t)
	if _, err := baza.Exec(`DROP TABLE pocetne_lozinke_cekaju`); err != nil {
		t.Fatal(err)
	}
	pozvan := false
	upisi := func(context.Context) (int, error) { pozvan = true; return 0, nil }
	if err := nakonPopisaLozinki(baza, upisi)(); err == nil || pozvan {
		t.Fatalf("greška brisanja %v, upis pozvan %v", err, pozvan)
	}
}

// Bez ičega za upis dnevnik ne dobiva redak
func TestUpisPunjenjaBezZapisaTiho(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	upisiPunjenjeUKnjigu(context.Background(), func(context.Context) (int, error) { return 0, nil })
	if dnevnik.String() != "" {
		t.Errorf("dnevnik %q", dnevnik.String())
	}
}

// Kad računalo dobije ključ mreže, najprije upiše punjenje, pa potpiše
// stanje prava, da potvrdu dobiju i računi iz punjenja
func TestNakonKljucaMrezeUpisujePaPotpisuje(t *testing.T) {
	var redom []string
	upisi := func(context.Context) (int, error) { redom = append(redom, "upis"); return 0, nil }
	potpisi := func(context.Context) { redom = append(redom, "potpis") }
	nakonKljucaMreze(upisi, potpisi)(context.Background())
	if strings.Join(redom, ",") != "upis,potpis" {
		t.Errorf("redom: %v", redom)
	}
}

// verzijaEntiteta broji verzije entiteta u knjizi
func verzijaEntiteta(t *testing.T, baza *sql.DB, entitet string) int {
	t.Helper()
	var n int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ?`, entitet).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// mapaSPunjenjem je mapa uz bazu s izmišljenim imenikom i organizacijom
// (testdata/sektor-p), kao na prvom računalu mreže
func mapaSPunjenjem(t *testing.T) string {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	dir := t.TempDir()
	for _, ime := range []string{"imenik.json", "organizacija.json"} {
		raw, err := os.ReadFile(filepath.Join(db.DataDir, ime))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ime), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// bazaOsnivaca napravi bazu uz izmišljeni imenik i organizaciju kao pri
// prvom pokretanju, s popisom lozinki koji čeka, na računalu koje je zatim
// osnovalo mrežu, i zatvori je; vraća put do baze
func bazaOsnivaca(t *testing.T) string {
	t.Helper()
	put := filepath.Join(mapaSPunjenjem(t), "gocop.db")
	baza, sp, popis, err := otvoriBazu(put)
	if err != nil {
		t.Fatal(err)
	}
	if len(popis) != 6 {
		t.Fatalf("popis lozinki ima %d računa, očekivano 6", len(popis))
	}
	upisiOsnovanuMrezu(t, baza)
	zatvoriSpremiste(t, sp)
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	return put
}

// preuzmiPopis preuzme popis privremenih lozinki sa stranice Postavljanje,
// s ovog računala
func preuzmiPopis(t *testing.T, adresa string) {
	t.Helper()
	klijent := &http.Client{Timeout: 10 * time.Second}
	odg, err := klijent.PostForm("http://"+adresa+"/postavljanje/lozinke", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if err := odg.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if odg.StatusCode != http.StatusOK {
		t.Fatalf("preuzimanje popisa: %d", odg.StatusCode)
	}
}

// Spajanje u run: računalo koje je osnovalo mrežu pri pokretanju upiše
// sektore i područja iz punjenja, a račune i dužnosti tek kad se popis
// početnih lozinki preuzme na stranici Postavljanje
func TestPunjenjeUKnjiguURunu(t *testing.T) {
	if testing.Short() {
		t.Skip("podiže cijeli čvor")
	}
	put := bazaOsnivaca(t)
	adresa, _, ugasi := cvorURunu(t, "-db", put, "-node", "cop-primjerovo")
	citac, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { zatvoriBazu(t, citac) })
	if s, a, u := verzijaEntiteta(t, citac, repository.EntitySectors), verzijaEntiteta(t, citac, repository.EntityAreas),
		verzijaEntiteta(t, citac, repository.EntityUsers); s != 3 || a != 3 || u != 0 {
		t.Fatalf("nakon pokretanja: %d verzija sektora, %d područja, %d računa; očekivano 3, 3, 0", s, a, u)
	}

	preuzmiPopis(t, adresa)
	if u, d := verzijaEntiteta(t, citac, repository.EntityUsers), verzijaEntiteta(t, citac, repository.EntityDuties); u != 5 || d != 7 {
		t.Errorf("nakon preuzimanja popisa: %d verzija računa, %d dužnosti; očekivano 5 i 7", u, d)
	}
	if kod := ugasi(); kod != 0 {
		t.Errorf("gašenje s kodom %d", kod)
	}
}
