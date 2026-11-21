package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Letve i vodostaji u ovom testu su izmišljeni.

// Ispravak telemetrije kroz bazu: preuzeto očitanje nosi sirovu vrijednost,
// usklađivanje mijenja samo preuzeta očitanja i ostavlja verziju, a stanje
// letve kaže ispravak zadnjeg preuzetog očitanja i zadnje ručno uz vrijednost
// uređaja.
func TestIspravakTelemetrijeUBazi(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "ispravak.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	stations := NewStationRepository(baza, rec)
	readings := NewReadingRepository(baza, rec)
	sp := NewJavniSpremiste(baza, readings)
	ctx := context.Background()
	jezero := &models.Station{ID: uuid.New(), Code: "jezero", Name: "Jezero", JavniURL: "https://vodostaji.voda.hr/Home/PregledVodostajaPostaje?postajaID=1", JavniUvoz: true}
	bez := &models.Station{ID: uuid.New(), Code: "bez-preuzimanja", Name: "Bez preuzimanja"}
	for _, st := range []*models.Station{jezero, bez} {
		if err := stations.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	sada := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	sat := func(h int) time.Time { return sada.Add(time.Duration(h) * time.Hour) }
	p := func(v int) *int { return &v }
	preuzeto := func(h, cm int, sirovo *int) models.Reading {
		return models.Reading{ID: uuid.New(), StationID: jezero.ID.String(), MeasuredAt: sat(h), LevelCm: p(cm), SirovoCm: sirovo,
			Source: models.ReadingSourceImport, Origin: "vodostaji.voda.hr"}
	}
	staro, prije, poslije := preuzeto(-30*24, 335, nil), preuzeto(-7, 355, nil), preuzeto(-4, 312, p(355))
	if n, err := sp.Upisi(ctx, []models.Reading{staro, prije, poslije}); err != nil || n != 3 {
		t.Fatalf("upis preuzetih: %d (%v)", n, err)
	}
	rucno := &models.Reading{StationID: jezero.ID.String(), MeasuredAt: sat(-6), LevelCm: p(312), Source: models.ReadingSourceManual}
	if err := readings.Create(ctx, rucno); err != nil {
		t.Fatal(err)
	}

	// vodostaji razdoblja, redom, uz zadnji preuzeti prije njega
	v, err := sp.Vodostaji(ctx, jezero.ID.String(), sat(-10), sat(0))
	if err != nil || len(v) != 4 {
		t.Fatalf("vodostaji: %d (%v)", len(v), err)
	}
	if v[0].ID != staro.ID || v[1].ID != prije.ID || !v[2].Rucno || v[2].Cm != 312 || v[3].Sirovo() != 355 || v[3].Podrijetlo != "vodostaji.voda.hr" {
		t.Errorf("vodostaji: %+v", v)
	}

	// usklađivanje: preuzeto se mijenja, ručno i nepostojeće ne
	if err := sp.Ispravi(ctx, nil); err != nil {
		t.Errorf("prazno usklađivanje: %v", err)
	}
	err = sp.Ispravi(ctx, []models.IspravljenoOcitanje{{ID: prije.ID, LevelCm: 312, SirovoCm: p(355)},
		{ID: rucno.ID, LevelCm: 1, SirovoCm: p(2)}, {ID: uuid.New(), LevelCm: 5}})
	if err != nil {
		t.Fatal(err)
	}
	nazad, err := readings.Get(ctx, prije.ID)
	if err != nil || nazad == nil || *nazad.LevelCm != 312 || nazad.IspravakCm() != -43 {
		t.Fatalf("usklađeno očitanje: %+v (%v)", nazad, err)
	}
	if r, err := readings.Get(ctx, rucno.ID); err != nil || r == nil || *r.LevelCm != 312 || r.SirovoCm != nil {
		t.Errorf("ručno očitanje je promijenjeno: %+v (%v)", r, err)
	}
	var verzija int
	if err := baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityReadings, prije.ID.String()).Scan(&verzija); err != nil || verzija != 2 {
		t.Errorf("verzija usklađenog očitanja: %d (%v)", verzija, err)
	}

	// stanje letve i popis ispravaka
	ispravci, err := readings.IspravciTelemetrije(ctx)
	if err != nil || len(ispravci) != 1 || ispravci[jezero.ID.String()] != -43 {
		t.Errorf("ispravci: %v (%v)", ispravci, err)
	}
	s, err := readings.StanjeIspravka(ctx, jezero.ID.String(), sada)
	if err != nil || s.PomakCm != -43 || !s.ImaRucno || s.RucnoCm != 312 || s.SirovoCm != 355 || !s.RucnoKad.Equal(sat(-6)) {
		t.Errorf("stanje: %+v (%v)", s, err)
	}
	// letva bez očitanja i ručno očitanje daleko od svakog mjerenja uređaja
	if s, err := readings.StanjeIspravka(ctx, bez.ID.String(), sada); err != nil || s != (models.StanjeIspravka{}) {
		t.Errorf("letva bez očitanja: %+v (%v)", s, err)
	}
	daleko := &models.Reading{StationID: bez.ID.String(), MeasuredAt: sat(-3), LevelCm: p(50), Source: models.ReadingSourceManual}
	if err := readings.Create(ctx, daleko); err != nil {
		t.Fatal(err)
	}
	if s, err := readings.StanjeIspravka(ctx, bez.ID.String(), sada); err != nil || s.ImaRucno || s.RucnoCm != 50 {
		t.Errorf("ručno bez mjerenja uređaja: %+v (%v)", s, err)
	}
}

// Zatvorena baza javlja grešku na svakom upitu ispravka.
func TestIspravakTelemetrijeGreskeBaze(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "ispravak.db"))
	if err != nil {
		t.Fatal(err)
	}
	readings := NewReadingRepository(baza, ledger.New(baza, "test"))
	sp := NewJavniSpremiste(baza, readings)
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := sp.Vodostaji(ctx, "x", time.Now(), time.Now()); err == nil {
		t.Error("Vodostaji: bez greške")
	}
	if err := sp.Ispravi(ctx, []models.IspravljenoOcitanje{{ID: uuid.New()}}); err == nil {
		t.Error("Ispravi: bez greške")
	}
	if _, err := readings.IspravciTelemetrije(ctx); err == nil {
		t.Error("IspravciTelemetrije: bez greške")
	}
	if _, err := readings.StanjeIspravka(ctx, "x", time.Now()); err == nil {
		t.Error("StanjeIspravka: bez greške")
	}
	if _, _, err := readings.uredjajUz(ctx, "x", time.Now()); err == nil {
		t.Error("uredjajUz: bez greške")
	}
}

// Greške usred posla: vrijednost koja se ne da pročitati kao cijeli broj
// prekida čitanje stanja, a upis koji ne prođe ili knjiga koje nema vraćaju
// usklađivanje na staro.
func TestIspravakTelemetrijeGreskeUsredPosla(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "ispravak.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	stations := NewStationRepository(baza, rec)
	readings := NewReadingRepository(baza, rec)
	ctx := context.Background()
	jezero := &models.Station{ID: uuid.New(), Code: "jezero", Name: "Jezero", JavniURL: "https://vodostaji.voda.hr/Home/PregledVodostajaPostaje?postajaID=1", JavniUvoz: true}
	if err := stations.CreateStation(ctx, jezero); err != nil {
		t.Fatal(err)
	}
	sada := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	cm := 335
	dobro := models.Reading{ID: uuid.New(), StationID: jezero.ID.String(), MeasuredAt: sada.Add(-2 * time.Hour), LevelCm: &cm, Source: models.ReadingSourceImport}
	if _, err := readings.ImportBatch(ctx, []models.Reading{dobro}); err != nil {
		t.Fatal(err)
	}
	ispravak := []models.IspravljenoOcitanje{{ID: dobro.ID, LevelCm: 295, SirovoCm: &cm}}

	// knjiga koje nema: izmjena se ne smije zadržati
	if _, err := baza.Exec(`ALTER TABLE record_versions RENAME TO sklonjeno`); err != nil {
		t.Fatal(err)
	}
	if err := readings.IspraviTelemetriju(ctx, ispravak); err == nil {
		t.Error("bez knjige: bez greške")
	}
	if r, err := readings.Get(ctx, dobro.ID); err != nil || *r.LevelCm != 335 {
		t.Errorf("izmjena bez verzije je ostala: %+v (%v)", r, err)
	}
	if _, err := baza.Exec(`ALTER TABLE sklonjeno RENAME TO record_versions`); err != nil {
		t.Fatal(err)
	}
	// stupac kojeg nema: ni čitanje očitanja ne prolazi
	if _, err := baza.Exec(`ALTER TABLE readings RENAME COLUMN source TO izvor`); err != nil {
		t.Fatal(err)
	}
	if err := readings.IspraviTelemetriju(ctx, ispravak); err == nil {
		t.Error("bez stupca izvora: bez greške")
	}
	if _, err := baza.Exec(`ALTER TABLE readings RENAME COLUMN izvor TO source`); err != nil {
		t.Fatal(err)
	}
	// upis koji baza odbije
	if _, err := baza.Exec(`CREATE TRIGGER bez_izmjene BEFORE UPDATE OF level_cm ON readings BEGIN SELECT RAISE(ABORT, 'ne'); END`); err != nil {
		t.Fatal(err)
	}
	if err := readings.IspraviTelemetriju(ctx, ispravak); err == nil {
		t.Error("odbijen upis: bez greške")
	}
	if _, err := baza.Exec(`DROP TRIGGER bez_izmjene`); err != nil {
		t.Fatal(err)
	}

	// vrijednosti koje nisu cijeli broj
	ubaci := func(izvor string, sati int, vodostaj, sirovo any) {
		t.Helper()
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, sirovo_cm, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), jezero.ID.String(), sada.Add(time.Duration(sati)*time.Hour), vodostaj, sirovo, izvor, sada, sada); err != nil {
			t.Fatal(err)
		}
	}
	ubaci(models.ReadingSourceManual, -1, "nije broj", nil)
	if _, err := readings.StanjeIspravka(ctx, jezero.ID.String(), sada); err == nil {
		t.Error("ručno očitanje koje nije broj: bez greške")
	}
	if _, err := baza.Exec(`UPDATE readings SET level_cm = 325 WHERE source = ?`, models.ReadingSourceManual); err != nil {
		t.Fatal(err)
	}
	ubaci(models.ReadingSourceImport, 0, 335.5, 335)
	if _, err := readings.IspravciTelemetrije(ctx); err == nil {
		t.Error("ispravak koji nije cijeli broj: bez greške u popisu")
	}
	if _, err := readings.StanjeIspravka(ctx, jezero.ID.String(), sada); err == nil {
		t.Error("ispravak koji nije cijeli broj: bez greške u stanju")
	}
	ubaci(models.ReadingSourceImport, -5, 334.5, nil)
	if _, _, err := readings.uredjajUz(ctx, jezero.ID.String(), sada.Add(-5*time.Hour)); err == nil {
		t.Error("vrijednost uređaja koja nije cijeli broj: bez greške")
	}
}
