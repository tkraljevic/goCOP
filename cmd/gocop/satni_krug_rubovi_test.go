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
