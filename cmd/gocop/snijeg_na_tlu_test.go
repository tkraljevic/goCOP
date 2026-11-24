package main

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/oborine"
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
