package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
)

// Zaključava da objava odbija broj koji je u međuvremenu dobila druga
// prijava istog sektora i godine (ErrBrojPrijaveZauzet), a prijavu koja je
// već objavljena odbija općom greškom. Bez provjere su obje prijave
// objavljene pod istim brojem. Vodočuvar Marko Markić je izmišljen.
func TestObjavaOdbijaZauzetBroj(t *testing.T) {
	_, repo := bazaPrenesenih(t)
	ctx := context.Background()
	nacrt := func(id string) *models.PrijavaSTerena {
		t.Helper()
		p := &models.PrijavaSTerena{ID: id, UserID: "g-1", Ime: "Marko Markić", Sektor: "B", AreaID: 16,
			Vrsta: models.PrijavaPrijava, Naslov: "Procjeđivanje", Opis: "Zatečeno pri obilasku.",
			Datum: time.Now().In(models.Zagreb), Status: models.PrijavaNacrt}
		if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	objavi := func(p *models.PrijavaSTerena, broj int) error {
		sad := time.Now()
		p.Broj, p.Godina, p.Status, p.ObjavljenoAt = broj, sad.In(models.Zagreb).Year(), models.PrijavaObjavljena, &sad
		return repo.Objavi(ctx, p, []byte("%PDF-1.4\n%%EOF\n"), "sazetak-"+p.ID)
	}
	prva, druga := nacrt("prijava-1"), nacrt("prijava-2")
	if err := objavi(prva, 1); err != nil {
		t.Fatal(err)
	}
	if err := objavi(druga, 1); !errors.Is(err, ErrBrojPrijaveZauzet) {
		t.Errorf("isti broj druge prijave: %v, treba ErrBrojPrijaveZauzet", err)
	}
	if p, err := repo.Get(ctx, druga.ID); err != nil || p.Objavljena() {
		t.Errorf("druga prijava objavljena pod zauzetim brojem: %+v, %v", p, err)
	}
	if err := objavi(druga, 2); err != nil {
		t.Errorf("sljedeći broj: %v", err)
	}
	if err := objavi(prva, 3); err == nil || errors.Is(err, ErrBrojPrijaveZauzet) {
		t.Errorf("ponovna objava objavljene prijave: %v", err)
	}
}
