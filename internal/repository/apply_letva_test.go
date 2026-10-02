package repository

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Letva primljena razmjenom mora na drugom čvoru biti ista kao na izvornom,
// polje po polje. Polja koja razmjena nije prenosila (HydroView, ograde
// niza, povijest…) ostala su na Unraidu prazna i prognoza je stala jer dva
// vrha lanca nitko nije preuzimao. Test popuni svako jednostavno polje, pa
// pada i kad letva dobije novo polje koje primjena ne upisuje.
func TestLetvaKrozRazmjenuSvaPolja(t *testing.T) {
	ctx := context.Background()
	otvori := func(ime string) (*StationRepository, *ledger.Recorder, func(ledger.Version) error) {
		baza, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { baza.Close() })
		if err := db.InitSchema(baza); err != nil {
			t.Fatal(err)
		}
		rec := ledger.New(baza, ime)
		return NewStationRepository(baza, rec), rec, nil
	}
	repoA, recA, _ := otvori("ured")
	repoB, recB, _ := otvori("unraid")
	bazaB := repoB.db

	st := &models.Station{ID: uuid.New(), Code: "proba-letva", Name: "Proba"}
	v := reflect.ValueOf(st).Elem()
	for i := 0; i < v.NumField(); i++ {
		f, polje := v.Field(i), v.Type().Field(i)
		if !f.CanSet() || polje.Name == "ID" || polje.Name == "Code" || polje.Name == "Name" {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString("x-" + polje.Name)
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Float32, reflect.Float64:
			f.SetFloat(12.5)
		case reflect.Int, reflect.Int64:
			f.SetInt(7)
		case reflect.Pointer:
			switch f.Type().Elem().Kind() {
			case reflect.Float64:
				x := 45.5
				f.Set(reflect.ValueOf(&x))
			case reflect.Int:
				x := 3
				f.Set(reflect.ValueOf(&x))
			}
		}
	}
	st.OgradeNiza = []models.OgradaNiza{{Izvor: "his2000", Velicina: "vodostaj", Od: "2026-01-01"}}
	st.CreatedAt = time.Date(2026, 10, 2, 6, 0, 0, 0, time.UTC)
	st.UpdatedAt = st.CreatedAt
	if err := repoA.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := repoA.UpdateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	verzije, err := recA.Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recB.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, bazaB, recB, verzije); err != nil {
		t.Fatal(err)
	}
	a, _ := repoA.GetStationByID(ctx, st.ID)
	b, err := repoB.GetStationByID(ctx, st.ID)
	if err != nil || b == nil {
		t.Fatalf("letva nije stigla: %v", err)
	}
	va, vb := reflect.ValueOf(*a), reflect.ValueOf(*b)
	for i := 0; i < va.NumField(); i++ {
		ime := va.Type().Field(i).Name
		if ime == "SectionCodes" {
			continue // izvodi se iz veza s dionicama, ne iz same letve
		}
		if !reflect.DeepEqual(va.Field(i).Interface(), vb.Field(i).Interface()) {
			t.Errorf("polje %s nije preneseno: izvor %v, primatelj %v", ime, va.Field(i).Interface(), vb.Field(i).Interface())
		}
	}
}
