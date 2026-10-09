package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Odluke (8. 10. 2026.), pitanje 12: trag predaje izvješća (prvo vrijeme
// predaje, tko je predao, tko je mijenjao nakon predaje) sprema se uz
// izvješće dionice i sektora, čita natrag i razmjenom stiže na drugi čvor
// isti. Ana Anić je prva predala, Ivo Ivić i Marko Markić mijenjali, Ivo
// ponovno predao.
func TestTragPredajeIzvjescaKrozBazuIRazmjenu(t *testing.T) {
	ctx := context.Background()
	otvori := func(ime string) (*IzvjescaRepository, *SektorskaIzvjescaRepository, *ledger.Recorder) {
		baza, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { baza.Close() })
		if err := db.InitSchema(baza); err != nil {
			t.Fatal(err)
		}
		rec := ledger.New(baza, ime)
		return NewIzvjescaRepository(baza, rec), NewSektorskaIzvjescaRepository(baza, rec), rec
	}
	dionA, sektA, recA := otvori("cop")
	dionB, sektB, recB := otvori("unraid")

	prvi := time.Date(2026, 10, 7, 9, 40, 0, 0, models.Zagreb)
	drugi := time.Date(2026, 10, 7, 14, 20, 0, 0, models.Zagreb)
	trag := models.TragPredaje{PrvoPredanoAt: &prvi, PredaoID: "ivo", Predao: "Ivo Ivić", Izmijenili: []models.Izmjenitelj{{ID: "ivo", Ime: "Ivo Ivić"}, {ID: "marko", Ime: "Marko Markić"}}}
	dan := time.Date(2026, 10, 6, 0, 0, 0, 0, models.Zagreb)
	dio := &models.DnevnoIzvjesce{SectionCode: "P.1.1", Dan: dan, Stadij: models.PhaseRegular, IzradioID: "ana", Izradio: "Ana Anić",
		IzradenoAt: drugi, PredanoAt: &drugi, TragPredaje: trag}
	if err := dionA.Save(ctx, dio); err != nil {
		t.Fatal(err)
	}
	sek := &models.SektorskoIzvjesce{Sektor: "P", Dan: dan, IzradioID: "ana", Izradio: "Ana Anić", IzradenoAt: drugi, PredanoAt: &drugi, TragPredaje: trag}
	if err := sektA.Save(ctx, sek); err != nil {
		t.Fatal(err)
	}
	isti := func(sto string, t1 models.TragPredaje, predano *time.Time) {
		t.Helper()
		if t1.PrvoPredanoAt == nil || !t1.PrvoPredanoAt.Equal(prvi) || t1.Predao != "Ivo Ivić" || t1.PredaoID != "ivo" || t1.TkoJeMijenjao() != "Ivo Ivić, Marko Markić" || t1.Izmijenili[1].ID != "marko" ||
			predano == nil || !predano.Equal(drugi) {
			t.Errorf("%s: %+v, predano %v", sto, t1, predano)
		}
	}
	if g, err := dionA.Get(ctx, dio.ID); err != nil || g == nil {
		t.Fatalf("izvješće dionice: %v", err)
	} else {
		isti("izvješće dionice", g.TragPredaje, g.PredanoAt)
	}
	if g, err := sektA.Get(ctx, sek.ID); err != nil || g == nil {
		t.Fatalf("izvješće sektora: %v", err)
	} else {
		isti("izvješće sektora", g.TragPredaje, g.PredanoAt)
	}

	// razmjenom na drugi čvor
	verzije, err := recA.Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recB.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, dionB.db, recB, verzije); err != nil {
		t.Fatal(err)
	}
	if g, err := dionB.Get(ctx, dio.ID); err != nil || g == nil {
		t.Fatalf("izvješće dionice na drugom čvoru: %v", err)
	} else {
		isti("izvješće dionice na drugom čvoru", g.TragPredaje, g.PredanoAt)
	}
	if g, err := sektB.Get(ctx, sek.ID); err != nil || g == nil {
		t.Fatalf("izvješće sektora na drugom čvoru: %v", err)
	} else {
		isti("izvješće sektora na drugom čvoru", g.TragPredaje, g.PredanoAt)
	}

	// izvješće bez traga (zapis otprije stupca) čita se prazno
	if _, err := dionA.db.Exec(`UPDATE dnevna_izvjesca SET trag_predaje = '' WHERE id = ?`, dio.ID); err != nil {
		t.Fatal(err)
	}
	if g, err := dionA.Get(ctx, dio.ID); err != nil || g.PrvoPredanoAt != nil || g.Predao != "" {
		t.Errorf("izvješće bez traga: %+v, %v", g, err)
	}
	// pokvaren trag javlja se kao greška, ne kao prazan trag
	if _, err := dionA.db.Exec(`UPDATE dnevna_izvjesca SET trag_predaje = '{' WHERE id = ?`, dio.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := dionA.Get(ctx, dio.ID); err == nil || !strings.Contains(err.Error(), "trag predaje") {
		t.Errorf("pokvaren trag izvješća dionice: %v", err)
	}
	if _, err := sektA.db.Exec(`UPDATE sektorska_izvjesca SET trag_predaje = '{' WHERE id = ?`, sek.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := sektA.Get(ctx, sek.ID); err == nil || !strings.Contains(err.Error(), "trag predaje") {
		t.Errorf("pokvaren trag izvješća sektora: %v", err)
	}
}
