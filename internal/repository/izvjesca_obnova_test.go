package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Odluke (8. 10. 2026.), pitanje 12, pri nadogradnji s 0.0.34: čvor sa
// starijim programom drži cijeli trag predaje u knjizi verzija, ali ne u
// tablici, jer stupca nema. Nadogradnja doda prazan stupac, a obnova
// površine iz knjige pri pokretanju mora trag vratiti — inače bi izvješće
// koje je bilo predano izgledalo kao da nikad nije, dalo se obrisati, a
// prva izmjena na tom čvoru izbrisala bi prvo vrijeme predaje i popis onih
// koji su ga mijenjali na svim čvorovima. Ana Anić predaje u 09:40, Ivo
// Ivić mijenja.
func TestObnovaPovrsineVracaTragPredajeNakonNadogradnje(t *testing.T) {
	ctx := context.Background()
	dionA, sektA, recA := otvoriCvorIzvjesca(t, "cop")
	dionC, sektC, recC := otvoriCvorIzvjesca(t, "laptop")
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	ivo := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	dan := time.Date(2026, 10, 7, 0, 0, 0, 0, models.Zagreb)
	prvi := time.Date(2026, 10, 7, 9, 40, 0, 0, models.Zagreb)
	izmjena := time.Date(2026, 10, 7, 11, 0, 0, 0, models.Zagreb)

	sek := &models.SektorskoIzvjesce{Sektor: "P", Dan: dan, IzradioID: ana.ID.String(), Izradio: "Ana Anić", IzradenoAt: prvi,
		Sadrzaj: models.SektorskiSadrzaj{Evakuacija: "Evakuirano 40 osoba."}}
	dio := &models.DnevnoIzvjesce{SectionCode: "P.1.1", Dan: dan, Stadij: models.PhaseRegular, IzradioID: ana.ID.String(), Izradio: "Ana Anić", IzradenoAt: prvi}
	// predano u 09:40, pa izmijenjeno: nacrt koji čeka ponovnu predaju
	sek.PredanoAt, dio.PredanoAt = &prvi, &prvi
	sek.ZabiljeziPredaju(prvi, ana)
	dio.ZabiljeziPredaju(prvi, ana)
	if err := sektA.Save(ctx, sek); err != nil {
		t.Fatal(err)
	}
	if err := dionA.Save(ctx, dio); err != nil {
		t.Fatal(err)
	}
	sek.IzmjenaPoslijePredaje(sek.PredanoAt, ivo, true)
	dio.IzmjenaPoslijePredaje(dio.PredanoAt, ivo, true)
	sek.PredanoAt, sek.IzradenoAt = nil, izmjena
	dio.PredanoAt, dio.IzradenoAt = nil, izmjena
	if err := sektA.Save(ctx, sek); err != nil {
		t.Fatal(err)
	}
	if err := dionA.Save(ctx, dio); err != nil {
		t.Fatal(err)
	}

	// čvor C prima verzije dok još radi s 0.0.34: knjiga ih drži cijele
	verzije, err := recA.Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recC.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, sektC.db, recC, verzije); err != nil {
		t.Fatal(err)
	}
	// nadogradnja: stupac traga dodan je prazan
	for _, tablica := range []string{"sektorska_izvjesca", "dnevna_izvjesca"} {
		if _, err := sektC.db.Exec(`UPDATE ` + tablica + ` SET trag_predaje = ''`); err != nil {
			t.Fatal(err)
		}
	}
	if g, _ := sektC.Get(ctx, sek.ID); g == nil || g.BiloPredano() {
		t.Fatalf("priprema: izvješće sektora bez traga mora izgledati nepredano, a je %+v", g)
	}

	// pokretanje novog programa obnavlja površinu iz knjige
	if _, err := ReplaySurface(ctx, sektC.db, recC); err != nil {
		t.Fatal(err)
	}
	gs, err := sektC.Get(ctx, sek.ID)
	if err != nil || gs == nil {
		t.Fatalf("izvješće sektora: %v", err)
	}
	if !gs.BiloPredano() || !gs.PrviPutPredano().Equal(prvi) || gs.TkoJeMijenjao() != "Ivo Ivić" || gs.Predano() {
		t.Errorf("izvješće sektora nakon nadogradnje: %+v", gs.TragPredaje)
	}
	gd, err := dionC.Get(ctx, dio.ID)
	if err != nil || gd == nil {
		t.Fatalf("izvješće dionice: %v", err)
	}
	if !gd.BiloPredano() || !gd.PrviPutPredano().Equal(prvi) || gd.TkoJeMijenjao() != "Ivo Ivić" || gd.Predano() {
		t.Errorf("izvješće dionice nakon nadogradnje: %+v", gd.TragPredaje)
	}
}
