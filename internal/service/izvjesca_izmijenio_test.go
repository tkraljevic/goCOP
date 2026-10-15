package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Odluke (8. 10. 2026.), pitanje 12: pamti se tko je predano izvješće
// mijenjao. Spremanje bez ikakve promjene („Spremi i ponovo predaj” bez
// izmjene) ne smije onoga tko je stvarno mijenjao zamijeniti onim tko je
// samo spremio, a kad mijenja više ljudi, pamte se svi. Ana Anić sastavlja
// i predaje, Ivo Ivić ispravlja, Ana sprema bez promjene i ponovno predaje,
// Marko Markić ispravlja još jednom.
func TestSpremanjeBezPromjeneNeMijenjaTkoJeIzmijenio(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	ivo := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	marko := &models.User{ID: uuid.New(), FullName: "Marko Markić"}

	t.Run("izvješće sektora", func(t *testing.T) {
		obrazac := func(id, evakuacija string) *models.SektorskoIzvjesce {
			return &models.SektorskoIzvjesce{ID: id, Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Vodostaji rastu.", Evakuacija: evakuacija}}
		}
		iz := obrazac("", "Evakuirano 40 osoba.")
		if err := o.svc.SpremiSektorsko(ctx, ana, o.upravaP, iz, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.PredajSektorsko(ctx, ana, o.upravaP, iz.ID); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.SpremiSektorsko(ctx, ivo, o.upravaP, obrazac(iz.ID, "Evakuirano 4 osobe."), nil, nil); err != nil {
			t.Fatal(err)
		}
		// Ana sprema isto što je Ivo ostavio i ponovno predaje
		if err := o.svc.SpremiSektorsko(ctx, ana, o.upravaP, obrazac(iz.ID, "Evakuirano 4 osobe."), nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.PredajSektorsko(ctx, ana, o.upravaP, iz.ID); err != nil {
			t.Fatal(err)
		}
		g, _ := o.svc.GetSektorsko(ctx, iz.ID)
		if tr := g.TragUDokumentu("Glavnom centru", "15:04"); !strings.HasPrefix(tr, " · izmijenio poslije predaje: Ivo Ivić · prvi put predano Glavnom centru ") || !g.PonovnoPredano() {
			t.Errorf("Ana nije mijenjala, a trag je %q", tr)
		}
		// Marko stvarno mijenja: pamte se oba koja su mijenjala, redom
		if err := o.svc.SpremiSektorsko(ctx, marko, o.upravaP, obrazac(iz.ID, "Evakuirane 4 osobe."), nil, nil); err != nil {
			t.Fatal(err)
		}
		g, _ = o.svc.GetSektorsko(ctx, iz.ID)
		if tr := g.TragUDokumentu("Glavnom centru", "15:04"); !strings.HasPrefix(tr, " · izmijenili poslije predaje: Ivo Ivić, Marko Markić · prvi put predano Glavnom centru ") {
			t.Errorf("trag nakon dvije izmjene: %q", tr)
		}
	})

	t.Run("izvješće dionice", func(t *testing.T) {
		pise := o.pisePero
		obrazac := func(id, pregled string) *models.DnevnoIzvjesce {
			iz := punoIzvjesce("P.1.1", danas())
			iz.ID, iz.Sadrzaj.Pregled = id, pregled
			return iz
		}
		iz := obrazac("", "Procjeđivanje kod Primjerova, 40 m.")
		if err := o.svc.Spremi(ctx, ana, pise, o.d111, iz); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.Predaj(ctx, ana, pise, o.d111, iz.ID); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.Spremi(ctx, ivo, o.upravaP, o.d111, obrazac(iz.ID, "Procjeđivanje kod Primjerova, 4 m.")); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.Spremi(ctx, ana, pise, o.d111, obrazac(iz.ID, "Procjeđivanje kod Primjerova, 4 m.")); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.Predaj(ctx, ana, pise, o.d111, iz.ID); err != nil {
			t.Fatal(err)
		}
		g, _ := o.svc.Get(ctx, iz.ID)
		if tr := g.TragUDokumentu("u podcentar", "15:04"); !strings.HasPrefix(tr, " · izmijenio poslije predaje: Ivo Ivić · prvi put predano u podcentar ") || !g.PonovnoPredano() {
			t.Errorf("Ana nije mijenjala, a trag je %q", tr)
		}
		// promjena stadija je promjena izvješća
		promjena := obrazac(iz.ID, "Procjeđivanje kod Primjerova, 4 m.")
		promjena.Stadij = models.PhaseRegular
		if err := o.svc.Spremi(ctx, marko, o.upravaP, o.d111, promjena); err != nil {
			t.Fatal(err)
		}
		g, _ = o.svc.Get(ctx, iz.ID)
		if tr := g.TragUDokumentu("u podcentar", "15:04"); !strings.HasPrefix(tr, " · izmijenili poslije predaje: Ivo Ivić, Marko Markić · prvi put predano u podcentar ") {
			t.Errorf("trag nakon dvije izmjene: %q", tr)
		}
	})
}
