package repository

import (
	"context"
	"testing"
)

// Račun sustava vrijedi samo za svoj sustav: nema pada na neki drugi račun,
// jer bi se tuđa lozinka slala na krivo mjesto.
func TestRacunSustavaSamoSvoj(t *testing.T) {
	r := NewRacuniSustavaRepository(bazaRacuna(t))
	ctx := context.Background()
	if err := r.Spremi(ctx, &RacunSustava{Sustav: "mletva.voda.hr", Korisnik: "pero", Lozinka: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Racun(ctx, "hdv.voda.hr"); err != nil || got != nil {
		t.Fatalf("za drugi sustav vraćen račun %+v (%v)", got, err)
	}
	got, err := r.Racun(ctx, "mletva.voda.hr")
	if err != nil || got == nil || got.Korisnik != "pero" {
		t.Fatalf("račun se ne vidi: %+v (%v)", got, err)
	}
	if err := r.Obrisi(ctx, "mletva.voda.hr"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Racun(ctx, "mletva.voda.hr"); got != nil {
		t.Fatalf("račun nije obrisan")
	}
}
