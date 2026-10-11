package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Odluke (8. 10. 2026.), pitanje 12, dok čvorovi rade s različitim
// programima: izmjena predanog izvješća vraća ga u nacrt, a predano
// izvješće sektora se ne briše. Program 0.0.34 to ne zna, pa izvješće koje
// je zadnji zapisao noviji program ne smije uređivati ni brisati: verzije
// izvješća nose višu shemu, a stariji program zapis novije sheme sprema,
// prenosi i prikazuje, ali ga ne mijenja (pravilo 2 knjige verzija). Ana
// Anić je izvješće predala na čvoru s novim programom.
func TestStarijiProgramNeUredujeIzvjescaNovijeSheme(t *testing.T) {
	for _, e := range []string{EntityDnevnaIzvjesca, EntitySektorskaIzvjesca} {
		if s := ledger.ShemaEntiteta(e); s != ShemaIzvjesca || s <= ledger.SchemaVersion {
			t.Errorf("shema %s: %d, a mora biti %d (viša od %d)", e, s, ShemaIzvjesca, ledger.SchemaVersion)
		}
	}
	ctx := context.Background()
	dion, sekt, rec := otvoriCvorIzvjesca(t, "cop")
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	dan := time.Date(2026, 10, 7, 0, 0, 0, 0, models.Zagreb)
	predano := time.Date(2026, 10, 7, 9, 40, 0, 0, models.Zagreb)
	sek := &models.SektorskoIzvjesce{Sektor: "P", Dan: dan, IzradioID: ana.ID.String(), Izradio: "Ana Anić", IzradenoAt: predano, PredanoAt: &predano,
		Sadrzaj: models.SektorskiSadrzaj{Evakuacija: "Evakuirano 40 osoba."}}
	sek.ZabiljeziPredaju(predano, ana)
	dio := &models.DnevnoIzvjesce{SectionCode: "P.1.1", Dan: dan, Stadij: models.PhaseRegular, IzradioID: ana.ID.String(), Izradio: "Ana Anić",
		IzradenoAt: predano, PredanoAt: &predano}
	dio.ZabiljeziPredaju(predano, ana)
	if err := sekt.Save(ctx, sek); err != nil {
		t.Fatal(err)
	}
	if err := dion.Save(ctx, dio); err != nil {
		t.Fatal(err)
	}
	for _, z := range []struct{ entitet, id string }{{EntitySektorskaIzvjesca, sek.ID}, {EntityDnevnaIzvjesca, dio.ID}} {
		if v, err := rec.Latest(ctx, z.entitet, z.id); err != nil || v.SchemaVersion != ShemaIzvjesca {
			t.Errorf("verzija %s nosi shemu %v (%v), a mora %d", z.entitet, v, err, ShemaIzvjesca)
		}
	}

	// isti zapis na programu koji zna samo početnu shemu
	for _, e := range []string{EntityDnevnaIzvjesca, EntitySektorskaIzvjesca} {
		ledger.PostaviShemu(e, ledger.SchemaVersion)
	}
	t.Cleanup(func() {
		for _, e := range []string{EntityDnevnaIzvjesca, EntitySektorskaIzvjesca} {
			ledger.PostaviShemu(e, ShemaIzvjesca)
		}
	})
	if err := sekt.Arhiviraj(ctx, sek); !errors.Is(err, ledger.ErrNovijaShema) {
		t.Errorf("stariji program briše izvješće sektora: %v", err)
	}
	sek.PredanoAt = nil
	if err := sekt.Save(ctx, sek); !errors.Is(err, ledger.ErrNovijaShema) {
		t.Errorf("stariji program mijenja izvješće sektora: %v", err)
	}
	if err := dion.Arhiviraj(ctx, dio); !errors.Is(err, ledger.ErrNovijaShema) {
		t.Errorf("stariji program briše izvješće dionice: %v", err)
	}
	if g, err := sekt.Get(ctx, sek.ID); err != nil || g == nil || !g.Predano() {
		t.Errorf("izvješće sektora nakon odbijene izmjene: %+v, %v", g, err)
	}
}
