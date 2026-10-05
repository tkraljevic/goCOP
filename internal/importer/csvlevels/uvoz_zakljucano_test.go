package csvlevels

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava uvoz tablice dnevnih vodostaja (Run) na maloj bazi koju test sam
// složi, bez data/: rane greške, preslikavanje stupaca na letve, probni
// prolaz prema upisu, razlike prema zatečenim očitanjima i sat očitanja.

type okolinaTablice struct {
	deps               Deps
	baza               *sql.DB
	primjerovo, probno *models.Station
	vodomjerObjekta    *models.Station
	objekt             *models.Structure
}

func novaOkolinaTablice(t *testing.T) *okolinaTablice {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "tablica.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	o := &okolinaTablice{baza: baza, deps: Deps{
		Readings:   repository.NewReadingRepository(baza, rec),
		Stations:   repository.NewStationRepository(baza, rec),
		Structures: repository.NewStructureRepository(baza, rec),
	}}
	ctx := context.Background()
	letva := func(sifra, naziv, voda string) *models.Station {
		st := &models.Station{ID: uuid.New(), Code: sifra, Name: naziv, Watercourse: voda}
		if err := o.deps.Stations.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	o.primjerovo = letva("primjerovo", "Primjerovo", "Primjerica")
	o.probno = letva("probno", "Probno", "Primjerica")
	letva("dvojnik-gornji", "Dvojnik", "Primjerica")
	letva("dvojnik-donji", "Dvojnik", "Kanal Probni")
	// objekt i njegov vodomjer nose isto ime
	o.vodomjerObjekta = letva("cs-probni-vodomjer", "CS Probni", "")
	o.objekt = &models.Structure{Code: "cs-probni", Name: "CS Probni", Kind: models.StructureKindPumpingStation,
		SectorID: "P", AreaID: 1, StationID: o.vodomjerObjekta.ID.String(), Origin: "RUČNI_UNOS"}
	if err := o.deps.Structures.CreateStructure(ctx, o.objekt); err != nil {
		t.Fatal(err)
	}
	return o
}

func (o *okolinaTablice) ocitanja(t *testing.T, stationID string) []models.Reading {
	t.Helper()
	out, err := o.deps.Readings.ListForGauges(context.Background(), []string{stationID}, nil,
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUvozTabliceRaneGreske(t *testing.T) {
	ctx := context.Background()
	// Provjere oblika tablice idu prije čitanja registra, pa prazne
	// ovisnosti ne smetaju.
	for ime, s := range map[string]struct {
		redci  []string
		greska string
	}{
		"samo zaglavlje":  {[]string{"Datum;Primjerovo"}, "nema ni zaglavlje"},
		"jedan stupac":    {[]string{"Datum", "01.09.2026."}, "barem jedan stupac postaje"},
		"prazna datoteka": {[]string{}, "nema ni zaglavlje"},
	} {
		_, err := Run(ctx, Options{Path: napisi(t, s.redci)})
		if err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: %v", ime, err)
		}
	}
	if _, err := Run(ctx, Options{Path: filepath.Join(t.TempDir(), "nema.csv")}); err == nil {
		t.Error("nepostojeća datoteka mora javiti grešku")
	}

	o := novaOkolinaTablice(t)
	rep, err := Run(ctx, Options{Path: napisi(t, []string{"Datum;Nepoznata;Q Primjerovo", "01.09.2026.;1;2"}),
		Skip: []string{"q primjerovo"}, Deps: o.deps})
	if err == nil || !strings.Contains(err.Error(), "nijedan stupac") {
		t.Errorf("bez prepoznatih stupaca: %v", err)
	}
	// izvješće do greške ipak nosi što je viđeno
	if len(rep.Unmatched) != 1 || len(rep.Skipped2) != 1 {
		t.Errorf("izvješće uz grešku: %+v", rep)
	}
}

func TestUvozTabliceStupci(t *testing.T) {
	o := novaOkolinaTablice(t)
	rep, err := Run(context.Background(), Options{
		Path: napisi(t, []string{
			"Datum;Primjerica - Primjerovo;Dvojnik;Q Primjerovo;pr;nr;CS Probni;Nepoznata;",
			"01.09.2026.;120;1;55,3;80;1;200;5;",
		}),
		Skip:    []string{"Q  primjerovo"},
		Aliases: map[string]string{"PR": "probno", "nr": "nema-je"},
		DryRun:  true, Deps: o.deps,
	})
	if err != nil {
		t.Fatal(err)
	}
	pokriveno := map[string]string{}
	for _, c := range rep.Matched {
		pokriveno[c.Header] = c.Key
	}
	ocekivano := map[string]string{
		"Primjerica - Primjerovo": "station:" + o.primjerovo.ID.String(), // voda + naziv
		"pr":                      "station:" + o.probno.ID.String(),     // zadani alias, bez obzira na velika slova
		"CS Probni":               "structure:" + o.objekt.ID.String(),   // objekt ispred svojeg vodomjera
	}
	for h, k := range ocekivano {
		if pokriveno[h] != k {
			t.Errorf("%s → %q, očekivano %q", h, pokriveno[h], k)
		}
	}
	if len(rep.Matched) != len(ocekivano) {
		t.Errorf("prepoznato %v", pokriveno)
	}
	if strings.Join(rep.Ambiguous, ",") != "Dvojnik" {
		t.Errorf("dvosmisleni: %v", rep.Ambiguous)
	}
	if strings.Join(rep.Skipped2, ",") != "Q Primjerovo" {
		t.Errorf("preskočeni: %v", rep.Skipped2)
	}
	nepoznati := strings.Join(rep.Unmatched, ",")
	if !strings.Contains(nepoznati, "nr (šifra nema-je nije u registru)") || !strings.Contains(nepoznati, "Nepoznata") || len(rep.Unmatched) != 2 {
		t.Errorf("neprepoznati: %v", rep.Unmatched)
	}
	if rep.Inserted != 3 || rep.Rows != 1 {
		t.Errorf("novih %d, redaka %d", rep.Inserted, rep.Rows)
	}
}

func TestUvozTabliceDvaStupcaNaIstuLetvu(t *testing.T) {
	o := novaOkolinaTablice(t)
	put := napisi(t, []string{
		"Datum;Primjerica - Primjerovo;Primjerovo (DHMZ)",
		"01.09.2026.;120;120",
		"02.09.2026.;121;130",
	})
	ctx := context.Background()
	// Oba stupca padaju na istu letvu. Vrijedi prvi; drugi se javlja kao
	// dvoznačan i ne uvozi, pa probni prolaz najavi točno ono što upis upiše.
	probni, err := Run(ctx, Options{Path: put, DryRun: true, Deps: o.deps})
	if err != nil {
		t.Fatal(err)
	}
	if len(probni.Matched) != 1 || probni.Matched[0].Header != "Primjerica - Primjerovo" || probni.Inserted != 2 || probni.Conflicts != 0 {
		t.Fatalf("probni prolaz: stupci %+v, novih %d, razlika %d", probni.Matched, probni.Inserted, probni.Conflicts)
	}
	if len(probni.Ambiguous) != 1 || !strings.HasPrefix(probni.Ambiguous[0], "Primjerovo (DHMZ)") ||
		!strings.Contains(probni.Ambiguous[0], "Primjerica - Primjerovo") {
		t.Errorf("dvoznačni: %v", probni.Ambiguous)
	}
	upis, err := Run(ctx, Options{Path: put, Deps: o.deps})
	if err != nil {
		t.Fatal(err)
	}
	if upis.Inserted != probni.Inserted {
		t.Errorf("upisano %d, a probni prolaz je najavio %d", upis.Inserted, probni.Inserted)
	}
	zapisano := o.ocitanja(t, o.primjerovo.ID.String())
	if len(zapisano) != 2 {
		t.Fatalf("u bazi %d očitanja", len(zapisano))
	}
	// drugog dana ostaje vrijednost iz prvog stupca, bez obzira na redoslijed mape
	for _, rd := range zapisano {
		if rd.LocalTime().Day() == 2 && *rd.LevelCm != 121 {
			t.Errorf("drugi dan: %d", *rd.LevelCm)
		}
	}
	// ponovni probni prolaz vidi oba dana prvog stupca kao već zapisana
	ponovo, err := Run(ctx, Options{Path: put, DryRun: true, Deps: o.deps})
	if err != nil {
		t.Fatal(err)
	}
	if ponovo.Inserted != 0 || ponovo.Skipped != 2 || ponovo.Conflicts != 0 {
		t.Errorf("ponovni prolaz: novih %d, zapisanih %d, razlika %d", ponovo.Inserted, ponovo.Skipped, ponovo.Conflicts)
	}
}

func TestUvozTabliceRazlikePremaZatecenom(t *testing.T) {
	o := novaOkolinaTablice(t)
	ctx := context.Background()
	// očitanje s terena u 8:15, iz drugog izvora
	cm := 140
	teren := models.Reading{ID: uuid.New(), StationID: o.primjerovo.ID.String(), LevelCm: &cm, Source: models.ReadingSourceManual,
		MeasuredAt: time.Date(2026, 9, 1, 8, 15, 0, 0, models.Zagreb).UTC()}
	if err := o.deps.Readings.Create(ctx, &teren); err != nil {
		t.Fatal(err)
	}
	put := napisi(t, []string{
		"Datum;Primjerovo",
		"01.09.2026.;120",
		"02.09.2026.;121",
		";5",
		"nije datum;5",
		"03.09.2026.;nije broj",
		"04.09.2026.;NaN",
		"05.09.2026.;1e3",
		"06.09.2026.;99999999999999999999",
		"07.09.2026.;3001",
	})
	rep, err := Run(ctx, Options{Path: put, DryRun: true, MaxShown: 1, Deps: o.deps})
	if err != nil {
		t.Fatal(err)
	}
	// prazan datum se ne broji kao nečitljiv; „NaN” i „1e3” nisu vodostaj, a
	// brojevi izvan raspona koji pušta ručni upis jesu čitljivi, pa se broje
	// zasebno (npr. tablica s apsolutnim kotama u cm)
	if rep.Rows != 7 || rep.BadDates != 1 || rep.BadValues != 3 || rep.IzvanRaspona != 2 {
		t.Errorf("redaka %d, nečitljivih datuma %d, vrijednosti %d, izvan raspona %d",
			rep.Rows, rep.BadDates, rep.BadValues, rep.IzvanRaspona)
	}
	if rep.Inserted != 1 || rep.Skipped != 1 || rep.Conflicts != 1 || len(rep.Differs) != 1 {
		t.Fatalf("novih %d, zapisanih %d, razlika %d (%v)", rep.Inserted, rep.Skipped, rep.Conflicts, rep.Differs)
	}
	r := rep.Differs[0]
	if r.Gauge != "Primjerovo" || r.Have != 140 || r.New != 120 || r.HaveAt.Format("15:04") != "08:15" {
		t.Errorf("razlika: %+v", r)
	}
	if !strings.HasPrefix(rep.Summary(), "PROBNI PROLAZ") || !strings.Contains(rep.Summary(), "nečitljivih datuma 1") ||
		!strings.Contains(rep.Summary(), "nečitljivih vrijednosti 3") ||
		!strings.Contains(rep.Summary(), "izvan raspona −500..3000 cm: 2") {
		t.Errorf("sažetak: %s", rep.Summary())
	}
	// samo konačni brojevi bez eksponenta
	for _, c := range []string{"1e3", "1E3", "NaN", "nan", "Inf", "-Inf", "+inf", "0x1p3", "1_000", "12-3", "--5"} {
		if lvl, st := parseLevel(c); st != levelBad {
			t.Errorf("%q → %d, %v", c, lvl, st)
		}
	}
	// čitljiv broj izvan raspona ručnog upisa nije nečitljiv, ali se ne uvozi
	for _, c := range []string{"99999999999999999999", "-99999999999999999999", "3001", "-501", "3.000,6", "-500,6"} {
		if lvl, st := parseLevel(c); st != levelIzvanRaspona {
			t.Errorf("%q → %d, %v; očekivano izvan raspona", c, lvl, st)
		}
	}
	// rubovi raspona su još vodostaj
	for c, ocekivano := range map[string]int{"3000": 3000, "-500": -500, "3.000": 3000, "2999,6": 3000, "-500,4": -500} {
		if lvl, st := parseLevel(c); st != levelOK || lvl != ocekivano {
			t.Errorf("%q → %d, %v; očekivano %d", c, lvl, st, ocekivano)
		}
	}
}

func TestUvozTabliceSatOcitanja(t *testing.T) {
	ctx := context.Background()
	sat := func(h int) *int { return &h }
	for _, s := range []struct {
		sat       *int
		minuta    int
		ocekivano string
	}{
		{nil, 0, "07:00"},    // nezadan sat je zadanih 7:00
		{sat(0), 0, "00:00"}, // ponoć se može zadati
		{sat(0), 30, "00:30"},
		{sat(6), 0, "06:00"},
	} {
		o := novaOkolinaTablice(t)
		// vrijeme u ćeliji datuma se zanemaruje
		put := napisi(t, []string{"Datum;Primjerovo", "2026-09-01 18:30:00;120"})
		if _, err := Run(ctx, Options{Path: put, Hour: s.sat, Minute: s.minuta, Origin: "tablica", Deps: o.deps}); err != nil {
			t.Fatal(err)
		}
		zapisano := o.ocitanja(t, o.primjerovo.ID.String())
		if len(zapisano) != 1 {
			t.Fatalf("%s: %d očitanja", s.ocekivano, len(zapisano))
		}
		rd := zapisano[0]
		if got := rd.LocalTime().Format("2006-01-02 15:04"); got != "2026-09-01 "+s.ocekivano {
			t.Errorf("%s → %s", s.ocekivano, got)
		}
		if rd.Source != models.ReadingSourceImport || rd.Origin != "tablica" || rd.SourceRef != "csv:Primjerovo:2026-09-01" {
			t.Errorf("trag zapisa: izvor %q, podrijetlo %q, oznaka %q", rd.Source, rd.Origin, rd.SourceRef)
		}
	}
}
