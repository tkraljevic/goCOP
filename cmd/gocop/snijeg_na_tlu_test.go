package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"gocop/internal/oborine"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// Snijeg na tlu kreće na svakom čvoru: preuzimanje ide u pozadinu, a
// poslužitelj dobiva izvor; novi dani bacaju izračunato.
func TestPokreniSnijegNaTlu(t *testing.T) {
	ob, err := oborine.Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ob.Close()
	staro := vrtiSnijegNaTlu
	defer func() { vrtiSnijegNaTlu = staro }()
	var pokrenut *oborine.SnijegNaTlu
	vrtiSnijegNaTlu = func(_ context.Context, s *oborine.SnijegNaTlu) { pokrenut = s }
	pokreniSnijegNaTlu(context.Background(), ob, nil, &oborine.Uvoznik{DB: ob}, &web.Server{})
	if pokrenut == nil || pokrenut.DB != ob || pokrenut.NakonUpisa == nil || pokrenut.Tocke == nil {
		t.Fatalf("preuzimanje: %+v", pokrenut)
	}
	pokrenut.NakonUpisa()
	if _, err := pokrenut.Tocke(); err == nil {
		t.Error("registar bez tablice kišomjera bez greške")
	}
	if _, err := izvorSnijega(ob, nil, &oborine.Uvoznik{DB: ob}, pokrenut).Tocke(); err == nil {
		t.Error("izvor: registar bez tablice kišomjera bez greške")
	}

	// pravo preuzimanje uz otkazan rad odmah stane, bez interneta
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	staro(ctx, &oborine.SnijegNaTlu{DB: ob, Tocke: func() ([]oborine.Tocka, error) { return nil, nil }})
}

// Voda za upozorenje o kiši i topljenju: idući dani iz baze oborina, a
// povijest iz arhive samo za alpske međuslivove. Brojevi su izmišljeni.
func TestIzvorSnijegaVoda(t *testing.T) {
	dir := t.TempDir()
	ob, err := oborine.Otvori(filepath.Join(dir, "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ob.Close()
	registar, err := sql.Open("sqlite", filepath.Join(dir, "registar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer registar.Close()
	arhiva, err := sql.Open("sqlite", filepath.Join(dir, "arhiva.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer arhiva.Close()
	u := &oborine.Uvoznik{DB: ob}
	iz := izvorSnijega(registar, arhiva, u, &oborine.SnijegNaTlu{DB: ob})
	// registar bez tablice kišomjera
	if _, _, err := iz.Voda(1000); err == nil {
		t.Error("voda bez registra bez greške")
	}
	if _, err := iz.PovijestVode(); err == nil {
		t.Error("povijest bez registra bez greške")
	}
	if _, err := registar.Exec(`CREATE TABLE kisomjeri (code TEXT, sliv TEXT, tezina REAL, aktivan INTEGER, vrsta TEXT);
		INSERT INTO kisomjeri VALUES ('k-a-1', 'A', 1, 1, 'izvedeni'), ('k-g-1', 'G', 1, 1, 'izvedeni')`); err != nil {
		t.Fatal(err)
	}
	if _, err := arhiva.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL);
		INSERT INTO spoj VALUES ('k-a-1', 'oborina', 'dnevni', 86400, 7), ('k-g-1', 'oborina', 'dnevni', 86400, 9)`); err != nil {
		t.Fatal(err)
	}
	if voda, kisa, err := iz.Voda(1000); err != nil || len(voda) != 0 || len(kisa) != 0 {
		t.Errorf("voda bez satnih: %v %v %v", voda, kisa, err)
	}
	povijest, err := iz.PovijestVode()
	if err != nil || len(povijest) != 1 || povijest[prognoza.OborinaKljuc("A")][1] != 7 {
		t.Errorf("povijest %v %v", povijest, err)
	}
}
