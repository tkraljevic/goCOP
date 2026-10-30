package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// S oglasne ploče briše se samo verzija zapisa koji ploča prikazuje. Starija
// verzija računa (o njoj ovisi povijest zaduženja) ostaje u knjizi, a zapis s
// ploče i dalje se briše.
func TestZidBriseSamoZapiseSPloce(t *testing.T) {
	z := noviZidPrijava(t)
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}
	var racun []string
	for _, ime := range []string{"Ivo Ivić", "Ivo Ivić Horvat"} {
		id, err := z.rec.Record(z.ctx, z.baza, repository.EntityUsers, "korisnik-1", map[string]string{"full_name": ime})
		if err != nil {
			t.Fatal(err)
		}
		racun = append(racun, id)
	}
	if err := z.zid.ObrisiDogadjaj(z.ctx, ana, racun[0]); !errors.Is(err, ErrNijeSPloce) {
		t.Errorf("brisanje starije verzije računa: %v", err)
	}
	if v, err := z.rec.Verzije(z.ctx, racun[:1]); err != nil || len(v) != 1 {
		t.Errorf("starija verzija računa je obrisana: %v %v", v, err)
	}
	if err := z.zid.ObrisiDogadjaj(z.ctx, &models.UserPermissions{}, racun[0]); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("brisanje bez prava: %v", err)
	}

	pr := z.nova(t, models.PrijavaPrijava, "Procjeđivanje na nasipu", 42)
	z.spremi(t, pr)
	prva, err := z.rec.History(z.ctx, repository.EntityPrijave, pr.ID)
	if err != nil || len(prva) < 2 {
		t.Fatalf("povijest prijave: %v %v", prva, err)
	}
	stara := prva[len(prva)-1].VersionID
	if err := z.zid.ObrisiDogadjaj(z.ctx, ana, stara); err != nil {
		t.Errorf("brisanje zapisa s ploče: %v", err)
	}
	if v, err := z.rec.Verzije(z.ctx, []string{stara}); err != nil || len(v) != 0 {
		t.Errorf("zapis s ploče nije obrisan: %v %v", v, err)
	}
	if err := z.zid.ObrisiDogadjaj(z.ctx, ana, "nema-takve"); err == nil {
		t.Error("brisanje verzije koje nema prošlo je")
	}
}
