package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Tuđim očima se sken vlastoručnog potpisa ne sprema ni ne uklanja, a akt se
// ne ovjerava ni u goCOP-u ni skenom, i ne poništava. Ista osoba svojim očima
// i dalje sprema sken, ovjerava i poništava.
func TestTudjimOcimaNemaSkenaPotpisaNiOvjere(t *testing.T) {
	o := novaOkolinaAkta(t)
	o.akti.SetKljuc(kljucCvora(1))
	ctx := context.Background()
	tudje := service.TudjimOcima(ctx)
	slika := slikaPNG(t, 300, 100)
	id := o.rukovod.ID.String()

	if err := o.akti.SpremiPotpisSliku(tudje, o.rukovod, slika); !errors.Is(err, service.ErrTudjimOcima) {
		t.Errorf("spremanje skena tuđim očima: %v", err)
	}
	if o.akti.PotpisSlika(ctx, id) != nil {
		t.Fatal("sken je spremljen tuđim očima")
	}
	if err := o.akti.SpremiPotpisSliku(ctx, o.rukovod, slika); err != nil {
		t.Fatalf("svojim očima: %v", err)
	}
	if err := o.akti.ObrisiPotpisSliku(tudje, o.rukovod); !errors.Is(err, service.ErrTudjimOcima) {
		t.Errorf("uklanjanje skena tuđim očima: %v", err)
	}
	if o.akti.PotpisSlika(ctx, id) == nil {
		t.Fatal("sken je uklonjen tuđim očima")
	}

	a := o.nacrtDezurnog(t, models.AktUspostava, time.Now().Add(-time.Hour).Truncate(time.Minute))
	if _, _, err := o.akti.Ovjeri(tudje, o.ovlasti, o.rukovod, a.ID, potvrdaPrimatelja); !errors.Is(err, service.ErrTudjimOcima) {
		t.Errorf("ovjera tuđim očima: %v", err)
	}
	sken := []byte("%PDF-1.4 potpisani akt")
	if _, _, err := o.akti.UcitajSkenirani(tudje, o.ovlasti, o.rukovod, a.ID, sken, id, potvrdaPrimatelja); !errors.Is(err, service.ErrTudjimOcima) {
		t.Errorf("sken ovjere tuđim očima: %v", err)
	}
	if x, err := o.akti.Get(ctx, a.ID); err != nil || x.Ovjeren() {
		t.Fatalf("akt je ovjeren tuđim očima: %+v %v", x, err)
	}
	if x, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, potvrdaPrimatelja); err != nil || !x.Ovjeren() {
		t.Fatalf("ovjera svojim očima: %v", err)
	}
	// poništenje se potpisuje na ime osobe, pa se ni ono tuđim očima ne radi
	if _, _, err := o.akti.Storniraj(tudje, o.ovlasti, o.rukovod, a.ID, "pogrešno izdan"); !errors.Is(err, service.ErrTudjimOcima) {
		t.Errorf("poništenje tuđim očima: %v", err)
	}
	if x, err := o.akti.Get(ctx, a.ID); err != nil || x.Storniran() {
		t.Fatalf("akt je poništen tuđim očima: %+v %v", x, err)
	}
	if x, _, err := o.akti.Storniraj(ctx, o.ovlasti, o.rukovod, a.ID, "pogrešno izdan"); err != nil || !x.Storniran() {
		t.Fatalf("poništenje svojim očima: %v", err)
	}
}
