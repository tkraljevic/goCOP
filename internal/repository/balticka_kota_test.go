package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

func TestBaltickaKotaPohranaIzmjenaIRazmjena(t *testing.T) {
	b, err := db.OpenDB(filepath.Join(t.TempDir(), "kote.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = db.InitSchema(b); err != nil {
		t.Fatal(err)
	}
	r := NewStationRepository(b, ledger.New(b, "test-kote"))
	ctx := context.Background()
	v, trst := 85.380, 86.055
	s := models.Station{Code: "test-balticka", Name: "Test", ZeroDatum: &trst, ZeroDatumBaltic: &v, ZeroDatumBalticSystem: "mBf", ZeroDatumBalticSource: "izvor"}
	if err = r.CreateStation(ctx, &s); err != nil {
		t.Fatal(err)
	}
	check := func(want float64) {
		t.Helper()
		got, err := r.GetStationByID(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ZeroDatumBaltic == nil || *got.ZeroDatumBaltic != want || got.ZeroDatumBalticSystem != "mBf" || got.ZeroDatumBalticSource != "izvor" {
			t.Fatalf("izgubljena baltička kota: %+v", got)
		}
		if got.ZeroDatum == nil || *got.ZeroDatum != trst || got.ZeroDatumNew != nil {
			t.Fatal("izmijenjeni drugi sustavi")
		}
	}
	check(v)
	s.Notes = "izmjena druge rubrike"
	if err = r.UpdateStation(ctx, &s); err != nil {
		t.Fatal(err)
	}
	check(v)
	v = 85.381
	tx, err := b.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = upsertStation(ctx, tx, s); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	check(v)
	s.ZeroDatumBaltic = nil
	if err = r.UpdateStation(ctx, &s); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetStationByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ZeroDatumBaltic != nil || got.ZeroDatum == nil || *got.ZeroDatum != trst {
		t.Fatal("brisanje baltičke kote utječe na Trst")
	}
}
