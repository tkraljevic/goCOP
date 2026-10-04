package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/javnivodostaji"
	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
)

// okolinaKruga je čvor s glavnom bazom, bazom prognoza i lažnim stranim
// prognozama; izdaje kaže ima li čvor ulogu izdavanja
func okolinaKruga(t *testing.T, izdaje bool) (*satniKrug, *javnivodostaji.Uvoznik) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gocop.db")
	baza, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "pperic-thinkpad")
	cvor, err := peers.LoadNode(dbPath, "pperic-thinkpad", "Probni", "test")
	if err != nil {
		t.Fatal(err)
	}
	mreza, err := peers.NewService(baza, rec, cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if err := mreza.PostaviUloge(context.Background(), peers.Uloge{Izdaje: izdaje}); err != nil {
		t.Fatal(err)
	}
	pb, err := prognoza.Otvori(filepath.Join(dir, "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pb.Close() })
	praznaArhiva, err := sql.Open("sqlite", filepath.Join(dir, "vodostaji.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { praznaArhiva.Close() })
	uvoznik := javnivodostaji.NoviUvoznik(repository.NewJavniSpremiste(baza, repository.NewReadingRepository(baza, rec)), t.Logf)
	k := &satniKrug{
		uvoznik: uvoznik, mreza: mreza, baza: baza, prognoze: pb,
		osvjezivac: &prognoza.Osvjezivac{Baza: pb, Ocitanja: baza, Arhiva: praznaArhiva, Najdalje: 96, Model: prognoza.ModelLanac, Cvor: "Probni"},
		objavi:     func(context.Context, *prognoza.Ishod) { t.Error("objava bez izračuna") },
	}
	return k, uvoznik
}

// lazneStranePrognoze zamijene dohvate s interneta; vraća koliko je puta
// koji pozvan
func lazneStranePrognoze(t *testing.T) *[3]int {
	t.Helper()
	stari := [3]func(context.Context, *http.Client) ([]prognoza.Letva, error){dohvatiMadjarsku, dohvatiAustrijsku, dohvatiSrpsku}
	t.Cleanup(func() { dohvatiMadjarsku, dohvatiAustrijsku, dohvatiSrpsku = stari[0], stari[1], stari[2] })
	var pozvano [3]int
	sutra := time.Now().UTC().Truncate(time.Hour).Add(24 * time.Hour)
	dohvatiMadjarsku = func(context.Context, *http.Client) ([]prognoza.Letva, error) {
		pozvano[0]++
		return []prognoza.Letva{
			{Naziv: "Zalabér", Izdano: time.Now(), Dani: []prognoza.Dan{{Kad: sutra, Cm: 312}, {Kad: sutra.Add(24 * time.Hour), Cm: 320}}},
			{Naziv: "Nepoznata letva", Izdano: time.Now(), Dani: []prognoza.Dan{{Kad: sutra, Cm: 1}}},
		}, nil
	}
	dohvatiAustrijsku = func(context.Context, *http.Client) ([]prognoza.Letva, error) {
		pozvano[1]++
		return nil, errors.New("noel.gv.at ne odgovara")
	}
	dohvatiSrpsku = func(context.Context, *http.Client) ([]prognoza.Letva, error) {
		pozvano[2]++
		return []prognoza.Letva{{Naziv: "SENTA", Izdano: time.Now(), Dani: []prognoza.Dan{{Kad: sutra, Cm: 205}}}}, nil
	}
	return &pozvano
}

// Čvor koji prognozu ne izdaje ne dohvaća ni tuđe prognoze ni kišu, ništa
// ne računa: izdanje mu stiže razmjenom
func TestSatniKrugCvorKojiNeIzdaje(t *testing.T) {
	k, uvoznik := okolinaKruga(t, false)
	pozvano := lazneStranePrognoze(t)
	k.vrti(context.Background())
	if *pozvano != [3]int{} {
		t.Errorf("dohvati: %v", *pozvano)
	}
	if r := uvoznik.Napredak().Redci; len(r) != 1 || r[0] != "prognozu izdaje drugi čvor; izdanje stiže razmjenom" {
		t.Errorf("redci: %q", r)
	}
}

// Čvor koji izdaje: tri strane prognoze redom (greška jedne ne zaustavlja
// ostale), zapisuju se samo nove vrijednosti (i postaje kojih nemamo, kao
// „strana-naziv”), pa izračun; bez podataka u arhivi izračun javlja grešku i
// ništa se ne objavljuje
func TestSatniKrugCvorKojiIzdaje(t *testing.T) {
	k, uvoznik := okolinaKruga(t, true)
	pozvano := lazneStranePrognoze(t)
	k.vrti(context.Background())
	if *pozvano != [3]int{1, 1, 1} {
		t.Errorf("dohvati: %v", *pozvano)
	}
	redci := uvoznik.Napredak().Redci
	zelim := []string{
		"mađarska prognoza (hydroinfo.hu)",
		prognoza.Podrijetlo + ": 2 letvi, 3 novih vrijednosti", // Zalabér dva dana i jedna strana letva
		"austrijska prognoza (noel.gv.at)",
		prognoza.PodrijetloNOEL + ": noel.gv.at ne odgovara",
		"srpska prognoza (hidmet.gov.rs)",
		prognoza.PodrijetloHidmet + ": 1 letvi, 1 novih vrijednosti",
		"izračun prognoze",
	}
	if len(redci) != len(zelim)+1 {
		t.Fatalf("redci: %q", redci)
	}
	for i, z := range zelim {
		if redci[i] != z {
			t.Errorf("redak %d: %q, želim %q", i, redci[i], z)
		}
	}
	if !strings.HasPrefix(redci[len(zelim)], "prognoza: ") {
		t.Errorf("izračun bez arhive: %q", redci[len(zelim)])
	}
	// drugi krug: iste vrijednosti nisu nove
	k.vrti(context.Background())
	redci = uvoznik.Napredak().Redci
	nadji := func(pocetak string) string {
		for i := len(redci) - 1; i >= 0; i-- {
			if strings.HasPrefix(redci[i], pocetak) {
				return redci[i]
			}
		}
		return ""
	}
	if r := nadji(prognoza.Podrijetlo + ":"); r != prognoza.Podrijetlo+": 2 letvi, 0 novih vrijednosti" {
		t.Errorf("drugi krug, mađarska: %q", r)
	}
	if r := nadji(prognoza.PodrijetloHidmet + ":"); r != prognoza.PodrijetloHidmet+": 1 letvi, 0 novih vrijednosti" {
		t.Errorf("drugi krug, srpska: %q", r)
	}
}
