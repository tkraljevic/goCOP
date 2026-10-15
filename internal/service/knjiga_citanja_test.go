package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
)

// Popis vodočuvara kojima rukovoditelj dionice zadaje zadatke čita akte
// sektora jednom, a ne za svakog vodočuvara i svaku dionicu: izvan obrane
// (najčešće) inače je svaki vodočuvar područja tražio čitanje svih akata
// sektora po dionici. Rukovoditelj bez upisanih dionica vodi sve tri
// dionice područja 1; Pero Perić čuva P.1.1, Ana Anić cijelo područje.
// Pojedinačna provjera čita akte jednom po sektoru, i samo kad ima dionica.
func TestPopisZaZadatkeCitaAkteJednom(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	citanja := 0
	stadiji := map[string]models.StanjeObrane{}
	k.o.vs.SetStanjaObrane(func(_ context.Context, _ string, _ time.Time) (map[string]models.StanjeObrane, error) {
		citanja++
		return stadiji, nil
	})
	cijelo := k.dionicar(models.RoleSectionLeader, &k.p1, "")
	if n := len(k.o.vs.Vodocuvari(ctx, cijelo)); n != 0 {
		t.Errorf("izvan obrane zadaje %d vodočuvara", n)
	}
	if citanja != 1 {
		t.Errorf("akti sektora pročitani %d puta za jedan popis", citanja)
	}
	// u obrani na P.1.3 (Ana je čuva, Pero ne): popis i dalje čita jednom
	citanja = 0
	stadiji["P.1.3"] = models.StanjeObrane{IzAkata: true, Aktivni: []models.AktivniStadij{{Stupanj: models.PhaseRegular}}}
	if popis := k.o.vs.Vodocuvari(ctx, cijelo); len(popis) != 1 || popis[0].ID != k.ana.ID || citanja != 1 {
		t.Errorf("u obrani na P.1.3: %d vodočuvara, %d čitanja", len(popis), citanja)
	}
	// dionica bez veze s vodočuvarom: akti se ne čitaju
	citanja = 0
	if k.o.vs.SmijeZadati(ctx, k.dionicar(models.RoleSectionLeader, &k.p1, "P.1.2"), k.listPere) || citanja != 0 {
		t.Errorf("zadatak bez veze s dionicom: %d čitanja", citanja)
	}
}

// Pamćenje popisa: stanja sektora i dionice područja čitaju se jednom, i
// greška čitanja akata pamti se kao „bez obrane”; bez pamćenja se čita
// svaki put.
func TestPamcenjeKnjige(t *testing.T) {
	k := novaKnjigaProba(t)
	vs := k.o.vs
	citanja := 0
	vs.SetStanjaObrane(func(_ context.Context, sektor string, _ time.Time) (map[string]models.StanjeObrane, error) {
		citanja++
		if sektor == "Q" {
			return map[string]models.StanjeObrane{"Q.1.1": {}}, errors.New("akti se ne čitaju")
		}
		return map[string]models.StanjeObrane{"P.1.1": {}}, nil
	})
	ctx := sPamcenjem(context.Background())
	for i := 0; i < 2; i++ {
		if st := vs.stanjaSektora(ctx, "P"); len(st) != 1 {
			t.Errorf("stanja sektora P: %v", st)
		}
		if st := vs.stanjaSektora(ctx, "Q"); st != nil {
			t.Errorf("stanja uz grešku: %v", st)
		}
	}
	if citanja != 2 {
		t.Errorf("uz pamćenje %d čitanja, očekivano 2", citanja)
	}
	vs.stanjaSektora(context.Background(), "P")
	vs.stanjaSektora(context.Background(), "P")
	if citanja != 4 {
		t.Errorf("bez pamćenja %d čitanja, očekivano 4", citanja)
	}
	// dionice područja: zapamćene se ne čitaju ponovno ni kad registra više nema
	if got := vs.dionicePodrucjaZa(ctx, 1); len(got) != 3 {
		t.Errorf("dionice područja 1: %v", got)
	}
	vs.SetDionice(nil)
	if got := vs.dionicePodrucjaZa(ctx, 1); len(got) != 3 {
		t.Errorf("zapamćene dionice područja 1: %v", got)
	}
	if got := vs.dionicePodrucjaZa(context.Background(), 1); len(got) != 0 {
		t.Errorf("bez pamćenja i bez registra: %v", got)
	}
	if pamcenjeIz(context.Background()) != nil {
		t.Error("pamćenje bez konteksta s pamćenjem")
	}
}
