package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// otvoriCvorIzvjesca je prazna baza jednog čvora s repozitorijima izvješća
func otvoriCvorIzvjesca(t *testing.T, ime string) (*IzvjescaRepository, *SektorskaIzvjescaRepository, *ledger.Recorder) {
	t.Helper()
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

// Odluke (8. 10. 2026.), pitanje 12: trag predaje izvješća (prvo vrijeme
// predaje, tko je predao, tko je mijenjao nakon predaje) sprema se uz
// izvješće dionice i sektora, čita natrag i razmjenom stiže na drugi čvor
// isti. Ana Anić je prva predala, Ivo Ivić i Marko Markić mijenjali, Ivo
// ponovno predao.
func TestTragPredajeIzvjescaKrozBazuIRazmjenu(t *testing.T) {
	ctx := context.Background()
	dionA, sektA, recA := otvoriCvorIzvjesca(t, "cop")
	dionB, sektB, recB := otvoriCvorIzvjesca(t, "unraid")

	prvi := time.Date(2026, 10, 7, 9, 40, 0, 0, models.Zagreb)
	drugi := time.Date(2026, 10, 7, 14, 20, 0, 0, models.Zagreb)
	trag := models.TragPredaje{PrvoPredanoAt: &prvi, PredaoID: "ivo", Predao: "Ivo Ivić", PredaoAt: &drugi, Izmijenili: []models.Izmjenitelj{{ID: "ivo", Ime: "Ivo Ivić"}, {ID: "marko", Ime: "Marko Markić"}}}
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
		if t1.PrvoPredanoAt == nil || !t1.PrvoPredanoAt.Equal(prvi) || t1.Predao != "Ivo Ivić" || t1.PredaoID != "ivo" || t1.PredaoAt == nil || !t1.PredaoAt.Equal(drugi) || t1.TkoJeMijenjao() != "Ivo Ivić, Marko Markić" || t1.Izmijenili[1].ID != "marko" ||
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

// sektorskoIzvjesce0034 je izvješće sektora kako ga zapisuje čvor s
// programom 0.0.34: bez traga predaje
type sektorskoIzvjesce0034 struct {
	ID         string                  `json:"id"`
	JournalID  string                  `json:"journal_id,omitempty"`
	Sektor     string                  `json:"sektor"`
	Dan        time.Time               `json:"dan"`
	Sadrzaj    models.SektorskiSadrzaj `json:"sadrzaj"`
	IzradioID  string                  `json:"izradio_id"`
	Izradio    string                  `json:"izradio"`
	IzradenoAt time.Time               `json:"izradeno_at"`
	PredanoAt  *time.Time              `json:"predano_at,omitempty"`
	CreatedAt  time.Time               `json:"created_at"`
	UpdatedAt  time.Time               `json:"updated_at"`
}

// Odluke (8. 10. 2026.), pitanje 12, dok čvorovi rade s različitim
// programima: čvor s 0.0.34 trag predaje ne poznaje, pa ga knjiga verzija
// pri njegovoj predaji prepiše iz prethodne inačice. Takva predaja ne smije
// se pripisati onome tko je predao prije — tko ju je predao, ne zna se.
// Ana Anić sastavlja i predaje u 09:40, Ivo Ivić mijenja, a Marko Markić
// ponovno predaje u 14:20 na čvoru s 0.0.34.
func TestPredajaStarijegProgramaNijePripisanaPrethodnom(t *testing.T) {
	ctx := context.Background()
	_, sektA, recA := otvoriCvorIzvjesca(t, "cop")
	_, sektB, recB := otvoriCvorIzvjesca(t, "laptop")
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	ivo := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	dan := time.Date(2026, 10, 7, 0, 0, 0, 0, models.Zagreb)
	prvi := time.Date(2026, 10, 7, 9, 40, 0, 0, models.Zagreb)
	izmjena := time.Date(2026, 10, 7, 11, 0, 0, 0, models.Zagreb)
	drugi := time.Date(2026, 10, 7, 14, 20, 0, 0, models.Zagreb)

	sek := &models.SektorskoIzvjesce{Sektor: "P", Dan: dan, IzradioID: ana.ID.String(), Izradio: "Ana Anić", IzradenoAt: prvi.Add(-10 * time.Minute),
		Sadrzaj: models.SektorskiSadrzaj{Evakuacija: "Evakuirano 40 osoba."}}
	sek.PredanoAt = &prvi
	sek.ZabiljeziPredaju(prvi, ana)
	if err := sektA.Save(ctx, sek); err != nil {
		t.Fatal(err)
	}
	sek.Sadrzaj.Evakuacija = "Evakuirane 4 osobe."
	sek.IzmjenaPoslijePredaje(sek.PredanoAt, ivo, true)
	sek.PredanoAt, sek.IzradenoAt = nil, izmjena
	if err := sektA.Save(ctx, sek); err != nil {
		t.Fatal(err)
	}
	// predaja na čvoru s 0.0.34: nova verzija bez traga, knjiga ga dopuni
	stara := sektorskoIzvjesce0034{ID: sek.ID, Sektor: sek.Sektor, Dan: dan, Sadrzaj: sek.Sadrzaj, IzradioID: sek.IzradioID, Izradio: sek.Izradio,
		IzradenoAt: izmjena, PredanoAt: &drugi, CreatedAt: sek.CreatedAt, UpdatedAt: drugi}
	tx, err := sektA.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recA.Record(ctx, tx, EntitySektorskaIzvjesca, sek.ID, stara); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	verzije, err := recA.Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recB.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, sektB.db, recB, verzije); err != nil {
		t.Fatal(err)
	}
	g, err := sektB.Get(ctx, sek.ID)
	if err != nil || g == nil {
		t.Fatalf("izvješće na drugom čvoru: %v", err)
	}
	if !g.PonovnoPredano() || !g.PrviPutPredano().Equal(prvi) || g.Predao != "Ana Anić" {
		t.Fatalf("ponovna predaja sa starijeg čvora: %+v", g)
	}
	if g.TkoJePredao() != "" {
		t.Errorf("predaju sa starijeg čvora predao je %q", g.TkoJePredao())
	}
	if o := g.TragUDokumentu("", "15:04"); o != " · izmijenio poslije predaje: Ivo Ivić · prvi put predano 09:40, ponovno 14:20" {
		t.Errorf("trag: %q", o)
	}
}
