package bp16

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Odluke (8. 10. 2026.), pitanje 21: uvoz iz BP16 prima isti raspon kao
// ručni upis. Tipfeler u staroj evidenciji (21 500 umjesto 215) ne ulazi,
// nego se bilježi za stranicu letve. Kao ni ručnim upisom, ne upisuje se ni
// stanje crpne stanice iz istog zapisa (dorada, Odluke (8. 10. 2026.),
// pitanje 21; prije je stanje ostajalo bez vodostaja).
// primjerica je baza čvora i izvoz BP16 s jednom crpnom stanicom, CS
// Primjerica, za uvoz zapisa s vodostajem izvan raspona
type primjerica struct {
	database *sql.DB
	readings *repository.ReadingRepository
	deps     Deps
	dir      string
}

func novaPrimjerica(t *testing.T) *primjerica {
	t.Helper()
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "primjerica.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Područje 16', 'VGI')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(database, "cvor-p")
	readings := repository.NewReadingRepository(database, rec)
	p := &primjerica{database: database, readings: readings, dir: t.TempDir(), deps: Deps{
		Readings:      readings,
		Stations:      repository.NewStationRepository(database, rec),
		Structures:    repository.NewStructureRepository(database, rec),
		Log:           t.Logf,
		StvoriObjekte: true,
	}}
	writeJSON(t, p.dir, "crpne_stanice", []map[string]any{{"id": 1, "naziv": "CS Primjerica", "status": "published"}})
	for _, prazna := range []string{"ustave", "vodomjerna_letva", "vodostaji_na_ustavama", "ostali_vodostaji"} {
		writeJSON(t, p.dir, prazna, []map[string]any{})
	}
	return p
}

// letva je ključ crpne stanice za stranicu letve
func (p *primjerica) letva(t *testing.T) string {
	t.Helper()
	var letva string
	if err := p.database.QueryRow(`SELECT 'structure:' || id FROM structures WHERE name = 'CS Primjerica'`).Scan(&letva); err != nil {
		t.Fatal(err)
	}
	return letva
}

func TestUvozBP16NeUpisujeVodostajIzvanRaspona(t *testing.T) {
	p := novaPrimjerica(t)
	database, readings, deps, dir := p.database, p.readings, p.deps, p.dir
	writeJSON(t, dir, "vodostaji_na_crpnim_stanicama", []map[string]any{
		{"id": 10, "crpna_stanica": 1, "datum": "2026-09-04", "vrijeme": "07:00:00", "vodostaj": 215, "napomena": "Očitao Pero Perić."},
		{"id": 11, "crpna_stanica": 1, "datum": "2026-09-05", "vrijeme": "07:00:00", "vodostaj": 21500, "napomena": "Očitao Pero Perić."},
		{"id": 12, "crpna_stanica": 1, "datum": "2026-09-06", "vrijeme": "07:00:00", "vodostaj": 21600, "stanje_cs": "kvar"},
	})

	rep, err := Run(context.Background(), DirSource{Dir: dir}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Fetched != 3 || rep.Inserted != 1 {
		t.Fatalf("pročitano %d, upisano %d; očekivano 3 i 1 (zapisi s tipfelerom otpadaju cijeli)", rep.Fetched, rep.Inserted)
	}
	var izvan, bezVodostaja int
	if err := database.QueryRow(`SELECT COUNT(*) FROM readings WHERE level_cm > 3000`).Scan(&izvan); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM readings WHERE level_cm IS NULL AND structure_state = ?`,
		models.StructureStateFault).Scan(&bezVodostaja); err != nil {
		t.Fatal(err)
	}
	if izvan != 0 || bezVodostaja != 0 {
		t.Errorf("izvan raspona upisano %d, stanje bez vodostaja %d", izvan, bezVodostaja)
	}
	s, err := readings.SazetakOdbacenih(context.Background(), p.letva(t))
	if err != nil || s == nil || s.Broj != 2 || s.Zadnja.Opis != "21600 cm" || s.IzvorLabel() != "evidencija VGI Baranja" {
		t.Errorf("sažetak: %+v, %v", s, err)
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): zapis crpne stanice s
// tipfelerom u vodostaju (21500 umjesto 215) otpada cijeli, sa satima rada
// agregata i očitavačem, kao što ručni upis odbija cijelo očitanje. Stranica
// letve broji odbačeni vodostaj s danom i satom zapisa, a kad se zapis u
// evidenciji BP16 ispravi, sljedeći uvoz ga upiše cijelog: zapis koji nije
// upisan uvoz ne preskače kao već uvezen.
func TestUvozBP16ZapisIzvanRasponaOtpadaCijeliIVracaSeIspravljen(t *testing.T) {
	p := novaPrimjerica(t)
	ctx := context.Background()
	zapis := map[string]any{"id": 20, "crpna_stanica": 1, "datum": "2026-09-05", "vrijeme": "07:00:00",
		"vodostaj": 21500, "ag_1": 7, "napomena": "Očitao Pero Perić."}
	writeJSON(t, p.dir, "vodostaji_na_crpnim_stanicama", []map[string]any{zapis})
	rep, err := Run(ctx, DirSource{Dir: p.dir}, p.deps)
	if err != nil || rep.Inserted != 0 {
		t.Fatalf("zapis s tipfelerom: upisano %d, %v", rep.Inserted, err)
	}
	var n int
	if err := p.database.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("upisano %d očitanja (sati agregata bez vodostaja?), %v", n, err)
	}
	kad := time.Date(2026, 9, 5, 7, 0, 0, 0, models.Zagreb)
	s, err := p.readings.SazetakOdbacenih(ctx, p.letva(t))
	if err != nil || s == nil || s.Broj != 1 || s.Zadnja.Opis != "21500 cm" || !s.Zadnja.Kad.Equal(kad) {
		t.Fatalf("sažetak: %+v, %v", s, err)
	}

	// zapis ispravljen u evidenciji BP16: sljedeći uvoz ga upiše cijelog
	zapis["vodostaj"] = 215
	writeJSON(t, p.dir, "vodostaji_na_crpnim_stanicama", []map[string]any{zapis})
	rep, err = Run(ctx, DirSource{Dir: p.dir}, p.deps)
	if err != nil || rep.Inserted != 1 || rep.Skipped != 0 {
		t.Fatalf("ispravljeni zapis: upisano %d, preskočeno %d, %v", rep.Inserted, rep.Skipped, err)
	}
	var cm, ag int
	var ocitao string
	if err := p.database.QueryRow(`SELECT level_cm, ag_hours_1, observer FROM readings`).Scan(&cm, &ag, &ocitao); err != nil {
		t.Fatal(err)
	}
	if cm != 215 || ag != 7 || ocitao != "Pero Perić" {
		t.Errorf("ispravljeni zapis: %d cm, %d h agregata, očitao %q", cm, ag, ocitao)
	}
}
