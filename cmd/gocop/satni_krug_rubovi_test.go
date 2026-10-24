package main

// Rubovi satnog kruga i obnove prognoze: što se ne da zapisati ili otvoriti
// javi se i ne ruši ostatak čvora.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/oborine"
	"gocop/internal/prognoza"
)

// redciS su redci kruga koji počinju zadanim podrijetlom
func redciS(redci []string, podrijetlo string) []string {
	var s []string
	for _, r := range redci {
		if strings.HasPrefix(r, podrijetlo) {
			s = append(s, r)
		}
	}
	return s
}

// Tuđa prognoza koja se ne da zapisati javi se u retku kruga
func TestStranaPrognozaKojaSeNeDaZapisati(t *testing.T) {
	k, uvoznik := okolinaKruga(t, true)
	dnevnik := dnevnikTesta(t)
	if err := k.prognoze.Close(); err != nil {
		t.Fatal(err)
	}
	sutra := time.Now().UTC().Truncate(time.Hour).Add(24 * time.Hour)
	iz := stranaPrognoza{korak: "mađarska prognoza", postotak: 91, podrijetlo: prognoza.Podrijetlo, sifra: prognoza.Sifra,
		dohvati: func(context.Context, *http.Client) ([]prognoza.Letva, error) {
			return []prognoza.Letva{{Naziv: "Zalabér", Izdano: time.Now(), Dani: []prognoza.Dan{{Kad: sutra, Cm: 312}}}}, nil
		}}
	preuzmiStranuPrognozu(context.Background(), uvoznik, k.prognoze, iz)
	redci := redciS(uvoznik.Napredak().Redci, prognoza.Podrijetlo)
	if len(redci) != 1 || !strings.HasPrefix(redci[0], prognoza.Podrijetlo+": zapis: ") {
		t.Fatalf("redci: %q", redci)
	}
	if !strings.Contains(dnevnik.String(), prognoza.Podrijetlo+": zapis: ") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Tuđa prognoza s neurednim datumima: javi se u retku kruga, uredni početak
// se zapiše, a baza prognoza pamti tablicu dok čitanje ne uspije uredno;
// neuspjelo čitanje je ne briše.
func TestStranaPrognozaNeuredniDatumi(t *testing.T) {
	k, uvoznik := okolinaKruga(t, true)
	dnevnikTesta(t)
	izdano := time.Now().Truncate(time.Hour)
	sutra := izdano.UTC().Add(24 * time.Hour)
	neuredno := &prognoza.NeuredniDatumi{Izvor: prognoza.PodrijetloHidmet, Izdano: izdano, Urednih: 2, PrviJeMjerenje: true,
		Datumi: []time.Time{izdano, sutra, sutra}}
	var greska error = neuredno
	iz := stranaPrognoza{korak: "srpska prognoza", postotak: 94, podrijetlo: prognoza.PodrijetloHidmet, sifra: prognoza.SifraSrpske,
		dohvati: func(context.Context, *http.Client) ([]prognoza.Letva, error) {
			if greska != nil && len(prognoza.NeuredneIz(greska)) == 0 {
				return nil, greska
			}
			return []prognoza.Letva{{Naziv: "BEZDAN", Izdano: izdano, Dani: []prognoza.Dan{{Kad: sutra, Cm: 205}}}}, greska
		}}
	zapamceno := func() (n int) {
		t.Helper()
		if err := k.prognoze.QueryRow(`SELECT count(*) FROM tude_neuredne`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	preuzmiStranuPrognozu(context.Background(), uvoznik, k.prognoze, iz)
	redci := redciS(uvoznik.Napredak().Redci, prognoza.PodrijetloHidmet)
	if len(redci) != 2 || redci[0] != prognoza.PodrijetloHidmet+": "+neuredno.Error() ||
		redci[1] != prognoza.PodrijetloHidmet+": 1 letva, 1 nova vrijednost" {
		t.Fatalf("redci: %q", redci)
	}
	if n := zapamceno(); n != 1 {
		t.Fatalf("zapamćeno %d", n)
	}

	greska = errors.New("hidmet.gov.rs ne odgovara")
	preuzmiStranuPrognozu(context.Background(), uvoznik, k.prognoze, iz)
	if n := zapamceno(); n != 1 {
		t.Errorf("nakon neuspjelog čitanja: %d", n)
	}
	greska = nil
	preuzmiStranuPrognozu(context.Background(), uvoznik, k.prognoze, iz)
	if n := zapamceno(); n != 0 {
		t.Errorf("nakon urednog čitanja: %d", n)
	}

	// Zapis neuredne tablice koji ne uspije javi se u dnevniku, a tuđa
	// prognoza ostaje zapisana.
	if _, err := k.prognoze.Exec(`DROP TABLE tude_neuredne`); err != nil {
		t.Fatal(err)
	}
	dnevnik := dnevnikTesta(t)
	greska = neuredno
	preuzmiStranuPrognozu(context.Background(), uvoznik, k.prognoze, iz)
	if !strings.Contains(dnevnik.String(), prognoza.PodrijetloHidmet+": zapis neurednih datuma: ") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Postaja kojoj sati ne idu naprijed preskače se, a ostale postaje istog
// izvora zapišu se i redak kruga kaže koliko.
func TestStranaPrognozaPreskocenaPostaja(t *testing.T) {
	k, uvoznik := okolinaKruga(t, true)
	dnevnikTesta(t)
	izdano := time.Now().Truncate(time.Hour)
	sutra := izdano.UTC().Add(24 * time.Hour)
	iz := stranaPrognoza{korak: "mađarska prognoza", postotak: 91, podrijetlo: prognoza.Podrijetlo, sifra: prognoza.Sifra,
		dohvati: func(context.Context, *http.Client) ([]prognoza.Letva, error) {
			return []prognoza.Letva{
				{Naziv: "Osijek", Izdano: izdano, Dani: []prognoza.Dan{{Kad: sutra, Cm: 1}}},
				{Naziv: "Aljmaš", Izdano: izdano, Dani: []prognoza.Dan{{Kad: sutra, Cm: 1}, {Kad: sutra, Cm: 2}}},
			}, nil
		}}
	preuzmiStranuPrognozu(context.Background(), uvoznik, k.prognoze, iz)
	redci := redciS(uvoznik.Napredak().Redci, prognoza.Podrijetlo)
	if len(redci) != 2 || !strings.HasPrefix(redci[0], prognoza.Podrijetlo+": zapis: hydroinfo.hu aljmas: sat ") ||
		redci[1] != prognoza.Podrijetlo+": 2 letve, 1 nova vrijednost" {
		t.Errorf("redci: %q", redci)
	}
}

// Oborine s Open-Meteo: greška registra javi se, prazan registar je nula
// sati; bez oborinskih točaka izvor oborina za izračun ostaje kakav je bio
func TestOborineUKrugu(t *testing.T) {
	k, uvoznik := okolinaKruga(t, true)
	dnevnik := dnevnikTesta(t)
	ob, err := oborine.Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ob.Close() })
	k.oborine = &oborine.Uvoznik{DB: ob, Tocke: func() ([]oborine.Tocka, error) { return nil, errors.New("registar ne odgovara") }}
	k.preuzmiOborine(context.Background())
	k.oborine.Tocke = func() ([]oborine.Tocka, error) { return nil, nil }
	k.preuzmiOborine(context.Background())

	redci := redciS(uvoznik.Napredak().Redci, "oborine:")
	if len(redci) != 2 || redci[0] != "oborine: registar ne odgovara" || redci[1] != "oborine: 0 sati za kišomjere" {
		t.Fatalf("redci: %q", redci)
	}
	if !strings.Contains(dnevnik.String(), "oborine: registar ne odgovara") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
	if k.osvjezivac.Oborine != nil {
		t.Errorf("bez oborinskih točaka u registru nema izvora oborina: %+v", k.osvjezivac.Oborine)
	}
}

// Oborinska točka koja se u registru pojavi između dva kruga ulazi u
// izračun već idući krug
func TestOborinskaTockaIzRegistraUlaziUIzracun(t *testing.T) {
	k, uvoznik := okolinaKruga(t, true)
	ob, err := oborine.Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ob.Close() })
	k.oborine = &oborine.Uvoznik{DB: ob, Tocke: func() ([]oborine.Tocka, error) { return nil, nil }}
	if _, err := k.baza.Exec(`INSERT INTO kisomjeri (code, naziv, sliv, latitude, longitude, tezina, created_at, updated_at)
		VALUES ('ob-probna', 'Probna točka', 'sliv-proba', 45.5, 18.7, 1, datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	k.preuzmiOborine(context.Background())
	if o := k.osvjezivac.Oborine; o == nil || len(o.Tocke) != 1 || o.Tocke[0].Code != "ob-probna" {
		t.Fatalf("izvor oborina: %+v", o)
	}
	if r := redciS(uvoznik.Napredak().Redci, "oborine:"); len(r) != 1 || r[0] != "oborine: 0 sati za kišomjere" {
		t.Errorf("redci: %q", r)
	}
}

// Baza oborina koja se ne da otvoriti: čvor radi bez oborina i kišomjera
func TestOborineSeNeDajuOtvoriti(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "oborine.db"), 0o755); err != nil {
		t.Fatal(err)
	}
	dnevnik := dnevnikTesta(t)
	osvjezivac := &prognoza.Osvjezivac{}
	ou, ku, ob := pripremiOborine(nil, filepath.Join(dir, "gocop.db"), nil, nil, osvjezivac)
	if ou != nil || ku != nil || ob != nil || osvjezivac.Oborine != nil {
		t.Fatalf("%v %v %v %v", ou, ku, ob, osvjezivac.Oborine)
	}
	if !strings.Contains(dnevnik.String(), "Oborine se neće preuzimati") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Baza prognoza koja se ne da otvoriti: poslužitelj radi bez prognoze
func TestPrognozaSeNeDaOtvoriti(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, "prognoze.db")
	if err := os.Mkdir(put, 0o755); err != nil {
		t.Fatal(err)
	}
	dnevnik := dnevnikTesta(t)
	if pb := pokreniPrognozu(ovisnostiPrognoze{dbPath: filepath.Join(dir, "gocop.db"), prognozePut: put}); pb != nil {
		t.Fatal("bez baze prognoza nema prognoze")
	}
	if !strings.Contains(dnevnik.String(), "Prognoza se neće obnavljati") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}
