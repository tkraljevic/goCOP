package main

import (
	"bytes"
	"context"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/hidroview"
	"gocop/internal/mletva"
	"gocop/internal/posta"
	"gocop/internal/repository"
)

// Račun za HydroView traži se po letvi (adresa javne stranice ili šifra
// postaje na telemetriji), a kad ga letva nema, vrijedi račun čvora; lozinka
// koja se ne da otključati ne daje prijavu.
func TestRacunHidroView(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "racuni.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, javni_url, telemetrija_site, created_at, updated_at) VALUES
		('s1', 'ZAL', 'Zalabér', 'https://primjer.hr/zal', '', datetime('now'), datetime('now')),
		('s2', 'TEL', 'Telemetrija', '', 'site-42', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	kljuc := hidroview.Kljuc([]byte("tajna čvora"))
	repo := repository.NewHidroViewRepository(baza)
	ctx := context.Background()
	trazi := hidroViewRacun(baza, repo, kljuc)

	if _, _, ok := trazi("https://primjer.hr/zal"); ok {
		t.Fatal("bez upisanog računa nema prijave")
	}
	upisi := func(letva, korisnik, lozinka string, k []byte) {
		t.Helper()
		zakljucano, err := posta.Zakljucaj(k, lozinka)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Spremi(ctx, &repository.RacunHidroView{Letva: letva, Korisnik: korisnik, Lozinka: zakljucano}); err != nil {
			t.Fatal(err)
		}
	}
	upisi("", "cvor", "lozinka-cvora", kljuc)
	upisi("ZAL", "pperic", "lozinka-zal", kljuc)
	upisi("TEL", "telemetrija", "lozinka-tel", kljuc)

	for adresa, ocekivano := range map[string][2]string{
		"https://primjer.hr/zal":                 {"pperic", "lozinka-zal"},
		"https://hydroview.example/site-42/data": {"telemetrija", "lozinka-tel"},
		"https://primjer.hr/nepoznata":           {"cvor", "lozinka-cvora"},
	} {
		korisnik, lozinka, ok := trazi(adresa)
		if !ok || korisnik != ocekivano[0] || lozinka != ocekivano[1] {
			t.Errorf("%s: %q %q %v", adresa, korisnik, lozinka, ok)
		}
	}

	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	upisi("ZAL", "pperic", "lozinka-zal", hidroview.Kljuc([]byte("drugi čvor")))
	if _, _, ok := trazi("https://primjer.hr/zal"); ok {
		t.Fatal("lozinka zaključana drugim ključem ne smije dati prijavu")
	}
	if !strings.Contains(dnevnik.String(), `HydroView: lozinka za "ZAL" se ne da otključati`) {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
}

// Jedan račun čvora vrijedi za mletva.voda.hr i letva.voda.hr; oznaka je
// samo ime u dnevniku.
func TestRacunSustava(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "racuni.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	kljuc := hidroview.Kljuc([]byte("tajna čvora"))
	racuni := repository.NewRacuniSustavaRepository(baza)
	trazi := racunSustava(racuni, kljuc, "letva.voda.hr")
	if _, _, ok := trazi(); ok {
		t.Fatal("bez upisanog računa nema prijave")
	}
	spremi := func(k []byte) {
		t.Helper()
		zakljucano, err := posta.Zakljucaj(k, "lozinka")
		if err != nil {
			t.Fatal(err)
		}
		if err := racuni.Spremi(context.Background(), &repository.RacunSustava{Sustav: mletva.Podrijetlo, Korisnik: "pperic", Lozinka: zakljucano}); err != nil {
			t.Fatal(err)
		}
	}
	spremi(kljuc)
	if korisnik, lozinka, ok := trazi(); !ok || korisnik != "pperic" || lozinka != "lozinka" {
		t.Fatalf("račun čvora: %q %q %v", korisnik, lozinka, ok)
	}

	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	spremi(hidroview.Kljuc([]byte("drugi čvor")))
	if _, _, ok := trazi(); ok {
		t.Fatal("lozinka zaključana drugim ključem ne smije dati prijavu")
	}
	if !strings.Contains(dnevnik.String(), "letva.voda.hr: lozinka se ne da otključati") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
}
