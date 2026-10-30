package db

import (
	"bytes"
	"database/sql"
	"log"
	"strings"
	"testing"
	"time"
)

// punjenjeTesta je vrijeme prvog punjenja u testovima roka
var punjenjeTesta = time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)

// napunjenaBaza je baza napunjena iz imenika, s prvim punjenjem u
// punjenjeTesta
func napunjenaBaza(t *testing.T) *sql.DB {
	t.Helper()
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`UPDATE pocetne_lozinke_cekaju SET napunjeno = ?`, punjenjeTesta); err != nil {
		t.Fatal(err)
	}
	return baza
}

func cekaju(t *testing.T, baza *sql.DB) int {
	t.Helper()
	var n int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM pocetne_lozinke_cekaju`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dnevnikBaze(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(stari) })
	return &b
}

// Punjenje zapiše vrijeme od kojega popis vrijedi 24 sata; nov popis nakon
// ponovnog pokretanja rok ne produljuje
func TestRokPopisaOdPrvogPunjenja(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	if rok, err := RokPocetnihLozinki(baza); err != nil || !rok.IsZero() {
		t.Fatalf("prazna baza nema roka: %v %v", rok, err)
	}
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	if rok, err := RokPocetnihLozinki(baza); err != nil || rok.IsZero() {
		t.Fatalf("punjenje nije zapisalo vrijeme: %v %v", rok, err)
	}

	baza = napunjenaBaza(t)
	if _, err := ObnoviPocetneLozinke(baza); err != nil {
		t.Fatal(err)
	}
	rok, err := RokPocetnihLozinki(baza)
	if err != nil || !rok.Equal(punjenjeTesta.Add(24*time.Hour)) {
		t.Errorf("rok nakon obnove: %v %v", rok, err)
	}
}

// Popis kojemu je prošao rok briše se uz redak u dnevniku; prije roka ostaje
func TestIstekniPocetneLozinke(t *testing.T) {
	baza := napunjenaBaza(t)
	dnevnik := dnevnikBaze(t)
	sviNaCekanju := cekaju(t, baza)

	if istekao, err := IstekniPocetneLozinke(baza, punjenjeTesta.Add(24*time.Hour-time.Second)); err != nil || istekao || cekaju(t, baza) != sviNaCekanju {
		t.Fatalf("prije roka: %v %v, čeka %d", istekao, err, cekaju(t, baza))
	}
	if dnevnik.Len() != 0 {
		t.Errorf("prije roka dnevnik: %q", dnevnik.String())
	}
	if istekao, err := IstekniPocetneLozinke(baza, punjenjeTesta.Add(24*time.Hour)); err != nil || !istekao || cekaju(t, baza) != 0 {
		t.Fatalf("na rok: %v %v, čeka %d", istekao, err, cekaju(t, baza))
	}
	if !strings.Contains(dnevnik.String(), "nitko nije preuzeo") || !strings.Contains(dnevnik.String(), "poništenjem") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
	// istekao popis se više ne slaže iznova
	if popis, err := ObnoviPocetneLozinke(baza); err != nil || popis != nil {
		t.Errorf("nakon isteka: %d lozinki, %v", len(popis), err)
	}
	// bez popisa nema se što brisati
	if istekao, err := IstekniPocetneLozinke(baza, punjenjeTesta.Add(48*time.Hour)); err != nil || istekao {
		t.Errorf("bez popisa: %v %v", istekao, err)
	}
}

// Popis iz ranijeg izdanja, bez vremena punjenja, dobije za vrijeme
// punjenja vrijeme upisa računa: star popis istječe pri prvom pokretanju,
// a nedavni vrijedi do 24 sata od upisa. Redak bez računa dobije sada.
func TestPopisBezVremenaPunjenja(t *testing.T) {
	bezVremena := func(t *testing.T, upisano time.Time) *sql.DB {
		t.Helper()
		baza := napunjenaBaza(t)
		if _, err := baza.Exec(`UPDATE pocetne_lozinke_cekaju SET napunjeno = NULL`); err != nil {
			t.Fatal(err)
		}
		if _, err := baza.Exec(`UPDATE users SET created_at = ?`, upisano); err != nil {
			t.Fatal(err)
		}
		return baza
	}
	pokretanje := punjenjeTesta.Add(72 * time.Hour)

	t.Run("star popis istječe", func(t *testing.T) {
		baza := bezVremena(t, punjenjeTesta)
		if istekao, err := IstekniPocetneLozinke(baza, pokretanje); err != nil || !istekao || cekaju(t, baza) != 0 {
			t.Errorf("popis od prije tri dana: %v %v, čeka %d", istekao, err, cekaju(t, baza))
		}
	})
	t.Run("nedavni popis ostaje do roka", func(t *testing.T) {
		upisano := pokretanje.Add(-time.Hour)
		baza := bezVremena(t, upisano)
		if istekao, err := IstekniPocetneLozinke(baza, pokretanje); err != nil || istekao {
			t.Fatalf("popis od prije sat vremena: %v %v", istekao, err)
		}
		if rok, err := RokPocetnihLozinki(baza); err != nil || !rok.Equal(upisano.Add(24*time.Hour)) {
			t.Errorf("rok: %v %v", rok, err)
		}
	})
	t.Run("redak bez računa", func(t *testing.T) {
		baza := bezVremena(t, punjenjeTesta)
		if _, err := baza.Exec(`UPDATE pocetne_lozinke_cekaju SET user_id = 'nema-ga-' || user_id`); err != nil {
			t.Fatal(err)
		}
		if istekao, err := IstekniPocetneLozinke(baza, pokretanje); err != nil || istekao {
			t.Fatalf("redak bez računa: %v %v", istekao, err)
		}
		if rok, err := RokPocetnihLozinki(baza); err != nil || !rok.Equal(pokretanje.Add(24*time.Hour)) {
			t.Errorf("rok: %v %v", rok, err)
		}
	})
}

// Greške baze pri provjeri roka se javljaju
func TestRokPopisaGreskeBaze(t *testing.T) {
	zatvorena := bazaZaPunjenje(t)
	_ = zatvorena.Close() // baza se namjerno zatvara
	if _, err := RokPocetnihLozinki(zatvorena); err == nil {
		t.Error("zatvorena baza: rok bez greške")
	}
	if _, err := IstekniPocetneLozinke(zatvorena, punjenjeTesta); err == nil {
		t.Error("zatvorena baza: istek bez greške")
	}

	// popis se ne da obrisati: popis ostaje, a greška se javlja
	baza := napunjenaBaza(t)
	if _, err := baza.Exec(`CREATE TRIGGER bez_brisanja BEFORE DELETE ON pocetne_lozinke_cekaju
		BEGIN SELECT RAISE(ABORT, 'popis se ne briše'); END`); err != nil {
		t.Fatal(err)
	}
	if istekao, err := IstekniPocetneLozinke(baza, punjenjeTesta.Add(25*time.Hour)); err == nil || istekao || cekaju(t, baza) == 0 {
		t.Errorf("neobrisan popis: %v %v", istekao, err)
	}

	// tablica popisa bez stupca vremena: provjera ne uspije
	pokvarena := bazaZaPunjenje(t)
	if _, err := pokvarena.Exec(`DROP TABLE pocetne_lozinke_cekaju`); err != nil {
		t.Fatal(err)
	}
	if _, err := pokvarena.Exec(`CREATE TABLE pocetne_lozinke_cekaju (user_id TEXT PRIMARY KEY, password_hash TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := RokPocetnihLozinki(pokvarena); err == nil {
		t.Error("tablica bez stupca vremena: rok bez greške")
	}
}

// Popis iz ranije inačice, bez stupca vremena punjenja, dobije stupac pri
// otvaranju baze
func TestStupacVremenaPunjenjaUStarojBazi(t *testing.T) {
	baza := bazaZaPunjenje(t)
	if _, err := baza.Exec(`DROP TABLE pocetne_lozinke_cekaju`); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`CREATE TABLE pocetne_lozinke_cekaju (user_id TEXT PRIMARY KEY, password_hash TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if !imaStupac(t, baza, "pocetne_lozinke_cekaju", "napunjeno") {
		t.Error("stupac napunjeno nije dodan")
	}
}
