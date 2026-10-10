package main

import (
	"context"
	"strings"
	"testing"

	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
)

// Odluke (8. 10. 2026.), pitanje 15: priprema modela baca naučene dnevne
// modele, pa ih idući krug uči iznova iz arhive, a ne tek iza ponoći — i kad
// namještanje lanca padne. Čvor koji prognozu ne izdaje ne priprema ništa.
func TestPripremaModelaUciDnevneIznova(t *testing.T) {
	stari := zaboraviDnevneModele
	t.Cleanup(func() { zaboraviDnevneModele = stari })
	zaboravljeno := 0
	zaboraviDnevneModele = func() { zaboravljeno++ }

	for _, izdaje := range []bool{false, true} {
		zaboravljeno = 0
		k, uvoznik := okolinaKruga(t, izdaje)
		priprema := pripremaModela(k.mreza, k.osvjezivac.Arhiva, k.prognoze, uvoznik, k.osvjezivac,
			func(context.Context, *prognoza.Ishod) { t.Error("objava bez namještenog modela") })
		// Arhiva je prazna, pa namještanje pada; dnevni modeli su već bačeni.
		err := priprema(context.Background(), &poslovi.Posao{})
		if err == nil {
			t.Fatalf("izdaje=%v: priprema iz prazne arhive prošla", izdaje)
		}
		want := 0
		if izdaje {
			want = 1
		} else if !strings.Contains(err.Error(), "čvor koji izdaje") {
			t.Errorf("čvor koji ne izdaje: %v", err)
		}
		if zaboravljeno != want {
			t.Errorf("izdaje=%v: dnevni modeli bačeni %d puta, želim %d", izdaje, zaboravljeno, want)
		}
	}
}
