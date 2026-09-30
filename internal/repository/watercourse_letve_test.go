package repository

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Dodatne letve vode (Batina uz baranjsku Karašicu) spremaju se uz vodu i
// stižu na drugi čvor s njezinom verzijom.
func TestDodatneLetveVode(t *testing.T) {
	ctx := context.Background()
	otvori := func(ime string) (*WatercourseRepository, *ledger.Recorder, func()) {
		d, err := db.OpenDB(filepath.Join(t.TempDir(), ime+".db"))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.InitSchema(d); err != nil {
			t.Fatal(err)
		}
		rec := ledger.New(d, ime)
		return NewWatercourseRepository(d, rec), rec, func() { d.Close() }
	}
	repo, rec, zatvori := otvori("izvor")
	defer zatvori()

	w := &models.Watercourse{Code: "potok-karasica-baranja-test", OfficialName: "potok Karašica (test)", Name: "Karašica", Kind: "potok"}
	if err := repo.CreateWatercourse(ctx, w); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetWatercourse(ctx, w.Code); got == nil || got.ExtraStationIDs != nil {
		t.Fatalf("nova voda bez dodatnih letvi: %+v", got)
	}
	w.ExtraStationIDs = []string{"c625fa9d-0425-5115-8c49-8819cbb17bbd"}
	if err := repo.UpdateWatercourse(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetWatercourse(ctx, w.Code)
	if !reflect.DeepEqual(got.ExtraStationIDs, w.ExtraStationIDs) {
		t.Fatalf("spremljeno %v, htio %v", got.ExtraStationIDs, w.ExtraStationIDs)
	}

	verzije, err := rec.Since(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	drugi, drugiRec, zatvori2 := otvori("drugi")
	defer zatvori2()
	if _, err := drugiRec.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, drugi.db, drugiRec, verzije); err != nil {
		t.Fatal(err)
	}
	if g, _ := drugi.GetWatercourse(ctx, w.Code); g == nil || !reflect.DeepEqual(g.ExtraStationIDs, w.ExtraStationIDs) {
		t.Errorf("na drugom čvoru: %+v", g)
	}
}
