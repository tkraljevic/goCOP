package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Objekt na mjestu terena mora biti u registru: neispravan ili nepostojeći
// objekt odbija se i kad je područje zadano, umjesto da tiho ostane u retku.
// Objekt se čita s kontekstom zahtjeva. Okolina je pripremiMts (sektor B,
// područje 34); objekt „CS Primjerovo” je izmišljen.
func TestMjestoTerenaObjekt(t *testing.T) {
	s, sk, ctx, _, uprava := pripremiMts(t)
	pero := mpPperic()
	cs := &models.Structure{Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation, SectorID: "B", AreaID: 34}
	if err := s.structures.CreateStructure(ctx, cs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 5}); err != nil {
		t.Fatal(err)
	}
	izdaj := func(objekt string, podrucje int) ([]models.Promet, error) {
		return s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, AreaID: podrucje, StructureID: objekt})
	}
	if _, err := izdaj("nije-uuid", 34); err == nil || !strings.Contains(err.Error(), "neispravan objekt „nije-uuid”") {
		t.Errorf("neispravan objekt: %v", err)
	}
	nema := uuid.NewString()
	if _, err := izdaj(nema, 34); err == nil || !strings.Contains(err.Error(), "objekt "+nema+" nije u registru") {
		t.Errorf("nepostojeći objekt: %v", err)
	}
	// objekt iz registra daje područje kad nije upisano
	redci, err := izdaj(cs.ID.String(), 0)
	if err != nil || len(redci) != 2 || redci[1].StructureID != cs.ID.String() || redci[1].AreaID != 34 {
		t.Fatalf("izdavanje na objekt: %+v %v", redci, err)
	}
	if ima := kolicinaU(t, s, ctx, sk.ID, mpLopata); ima != 4 {
		t.Errorf("odbijena izdavanja skinula su zalihu: %v", ima)
	}

	// kontekst zahtjeva: prekinut zahtjev ne čita objekt
	prekinut, prekini := context.WithCancel(ctx)
	prekini()
	if _, err := s.mjestoTerena(prekinut, Zahvat{AreaID: 34, StructureID: cs.ID.String()}, "B"); !errors.Is(err, context.Canceled) {
		t.Errorf("prekinut zahtjev: %v", err)
	}
}
