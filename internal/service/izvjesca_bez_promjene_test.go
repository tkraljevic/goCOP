package service

import (
	"context"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Odluke (8. 10. 2026.), pitanje 12: u nacrt predano izvješće vraća
// spremljena izmjena. Spremanje bez ikakve promjene ništa ne mijenja:
// izvješće ostaje predano s istim vremenom predaje, nova verzija se ne
// piše, pa se ni izvješće dionice ne može praznim spremanjem povući bez
// potvrde. Pero Perić sastavlja i predaje, Pero Perić (zamjenik) sprema bez
// promjene.
func TestSpremanjePredanogBezPromjeneOstajePredano(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	verzija := func(entitet, id string) int {
		t.Helper()
		var n int
		if err := o.baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, entitet, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("izvješće dionice", func(t *testing.T) {
		iz := punoIzvjesce("P.1.1", danas())
		if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, iz); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, iz.ID); err != nil {
			t.Fatal(err)
		}
		predano, _ := o.svc.Get(ctx, iz.ID)
		n := verzija(repository.EntityDnevnaIzvjesca, iz.ID)
		isto := punoIzvjesce("P.1.1", danas())
		isto.ID = iz.ID
		if err := o.svc.Spremi(ctx, o.urednik, o.upravaP, o.d111, isto); err != nil {
			t.Fatal(err)
		}
		g, _ := o.svc.Get(ctx, iz.ID)
		if !g.Predano() || !g.PredanoAt.Equal(*predano.PredanoAt) || g.PonovnoPredano() || g.TkoJeMijenjao() != "" || !g.IzradenoAt.Equal(predano.IzradenoAt) {
			t.Errorf("predano izvješće dionice nakon spremanja bez promjene: %+v", g)
		}
		if m := verzija(repository.EntityDnevnaIzvjesca, iz.ID); m != n {
			t.Errorf("spremanje bez promjene upisalo je verziju: %d → %d", n, m)
		}
		if !isto.Predano() || !isto.PredanoAt.Equal(*predano.PredanoAt) {
			t.Errorf("pozivatelj ne vidi da je izvješće ostalo predano: %+v", isto)
		}
		// izmjena ga i dalje vraća u nacrt
		isto.Sadrzaj.Pregled = "Procjeđivanje kod Primjerova."
		if err := o.svc.Spremi(ctx, o.urednik, o.upravaP, o.d111, isto); err != nil {
			t.Fatal(err)
		}
		if g, _ := o.svc.Get(ctx, iz.ID); g.Predano() || !g.BiloPredano() || g.TkoJeMijenjao() != "Pero Perić (zamjenik)" {
			t.Errorf("izmjena predanog izvješća dionice: %+v", g)
		}
	})

	t.Run("izvješće sektora", func(t *testing.T) {
		obrazac := func(id string) *models.SektorskoIzvjesce {
			return &models.SektorskoIzvjesce{ID: id, Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Vodostaji rastu."}}
		}
		iz := obrazac("")
		if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, iz, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := o.svc.PredajSektorsko(ctx, o.pero, o.upravaP, iz.ID); err != nil {
			t.Fatal(err)
		}
		predano, _ := o.svc.GetSektorsko(ctx, iz.ID)
		n := verzija(repository.EntitySektorskaIzvjesca, iz.ID)
		isto := obrazac(iz.ID)
		if err := o.svc.SpremiSektorsko(ctx, o.urednik, o.upravaP, isto, nil, nil); err != nil {
			t.Fatal(err)
		}
		g, _ := o.svc.GetSektorsko(ctx, iz.ID)
		if !g.Predano() || !g.PredanoAt.Equal(*predano.PredanoAt) || g.PonovnoPredano() || g.TkoJeMijenjao() != "" {
			t.Errorf("predano izvješće sektora nakon spremanja bez promjene: %+v", g)
		}
		if m := verzija(repository.EntitySektorskaIzvjesca, iz.ID); m != n {
			t.Errorf("spremanje bez promjene upisalo je verziju: %d → %d", n, m)
		}
		if !isto.Predano() {
			t.Errorf("pozivatelj ne vidi da je izvješće ostalo predano: %+v", isto)
		}
	})
}
