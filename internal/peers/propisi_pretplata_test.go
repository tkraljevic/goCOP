package peers_test

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
)

// Laptop koji prati samo „BP 16, 2026.” dobije propis i njegovu izmjenu, jer
// su u zajedničkom kanalu, a ne u kanalu po području i godini. Pravilo
// pretplate tumači pošiljatelj, pa poseban kanal propisa ne bi stigao ni
// preko starijeg posrednika (Odluke (25. 11. 2026.), O1). Primjer je
// izmišljen.
func TestPropisStizePretplatnikuPodrucja(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ured := startNode(t, ctx, "ured")
	laptop := startNode(t, ctx, "laptop")
	founder(t, ctx, ured, "Hrvatske vode")
	pair(t, ctx, ured, laptop)

	nn := models.IzdanjeNN{Godina: 2026, Broj: 12, Akt: 201}
	p := &models.Propis{ID: nn.ID(), Adresa: "zakon-o-cuvanju-nasipa", Naziv: "Zakon o čuvanju nasipa", NN: nn,
		Izvor: models.IzvorPropisaNN, Dijelovi: []models.DioPropisa{{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.",
			Stavci: []models.StavakPropisa{{Tekst: "Ovim se Zakonom uređuje čuvanje nasipa."}}}}}
	iz := &models.IzmjenaPropisa{ID: p.ID + "-izmjena-nn-2026-47-808", PropisID: p.ID, Naziv: "Zakon o izmjeni Zakona o čuvanju nasipa",
		NN: models.IzdanjeNN{Godina: 2026, Broj: 47, Akt: 808}, Izvor: models.IzvorPropisaNN}
	propisiUreda := repository.NewPropisiRepository(ured.db, ured.rec)
	if err := propisiUreda.Spremi(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := propisiUreda.SpremiIzmjenu(ctx, iz); err != nil {
		t.Fatal(err)
	}

	if _, err := laptop.svc.AddSubscription(ctx, peers.Subscription{AreaID: 16, YearFrom: 2026, YearTo: 2026}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := laptop.svc.SyncWith(ctx, ured.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	propisiLaptopa := repository.NewPropisiRepository(laptop.db, laptop.rec)
	if x, err := propisiLaptopa.Get(ctx, p.ID); err != nil || x == nil || x.Naziv != p.Naziv {
		t.Fatalf("propis nije stigao pretplatniku područja: %+v %v", x, err)
	}
	if x, err := propisiLaptopa.Izmjena(ctx, iz.ID); err != nil || x == nil {
		t.Fatalf("izmjena nije stigla pretplatniku područja: %+v %v", x, err)
	}
	if pogoci, err := propisiLaptopa.Trazi(ctx, []string{"cuvanj"}, "", 0); err != nil || len(pogoci) != 1 {
		t.Errorf("kazalo na laptopu: %+v %v", pogoci, err)
	}
}
