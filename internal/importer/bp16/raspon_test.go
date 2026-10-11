package bp16

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

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

// Odluke (8. 10. 2026.), pitanje 21: uvoz iz BP16 prima isti raspon kao
// ručni upis. Tipfeler u staroj evidenciji (21 500 umjesto 215) ne ulazi,
// nego se bilježi za stranicu letve; stanje crpne stanice iz istog zapisa
// ostaje (dorada: ne upisuje se samo odbačena vrijednost). Zapis kojem uz
// tipfeler ostane samo očitavač ne upisuje se.
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
	if rep.Fetched != 3 || rep.Inserted != 2 {
		t.Fatalf("pročitano %d, upisano %d; očekivano 3 i 2 (tipfeler bez ičega drugog otpada)", rep.Fetched, rep.Inserted)
	}
	var izvan, bezVodostaja int
	if err := database.QueryRow(`SELECT COUNT(*) FROM readings WHERE level_cm > 3000`).Scan(&izvan); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM readings WHERE level_cm IS NULL AND structure_state = ?`,
		models.StructureStateFault).Scan(&bezVodostaja); err != nil {
		t.Fatal(err)
	}
	if izvan != 0 || bezVodostaja != 1 {
		t.Errorf("izvan raspona upisano %d, stanje bez vodostaja %d", izvan, bezVodostaja)
	}
	s, err := readings.SazetakOdbacenih(context.Background(), p.letva(t))
	if err != nil || s == nil || s.Broj != 2 || s.Zadnja.Opis != "21600 cm" || s.IzvorLabel() != "evidencija VGI Baranja" {
		t.Errorf("sažetak: %+v, %v", s, err)
	}

	// zapis koji nije upisan uvoz ne preskače kao već uvezen: kad se u
	// evidenciji ispravi, sljedeći ga uvoz upiše
	writeJSON(t, dir, "vodostaji_na_crpnim_stanicama", []map[string]any{
		{"id": 11, "crpna_stanica": 1, "datum": "2026-09-05", "vrijeme": "07:00:00", "vodostaj": 216, "napomena": "Očitao Pero Perić."},
	})
	rep, err = Run(context.Background(), DirSource{Dir: dir}, deps)
	if err != nil || rep.Inserted != 1 || rep.Skipped != 0 {
		t.Errorf("ispravljeni zapis: upisano %d, preskočeno %d, %v", rep.Inserted, rep.Skipped, err)
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): zapis crpne stanice s
// tipfelerom u vodostaju (21500 umjesto 215) upisuje se bez vodostaja, sa
// satima rada agregata i očitavačem: ne upisuje se samo odbačena vrijednost.
// Stranica letve broji odbačeni vodostaj s danom i satom zapisa.
//
// Kad se zapis poslije u evidenciji BP16 ispravi, uvoz ga preskače kao već
// uvezen, kao i svaki drugi zapis koji je jednom upisan; vodostaj se tada
// upisuje izmjenom očitanja. Dopuna upisanog očitanja nije dio odluke.
func TestUvozBP16ZapisIzvanRasponaZadrzavaSateAgregata(t *testing.T) {
	p := novaPrimjerica(t)
	ctx := context.Background()
	zapis := map[string]any{"id": 20, "crpna_stanica": 1, "datum": "2026-09-05", "vrijeme": "07:00:00",
		"vodostaj": 21500, "ag_1": 7, "napomena": "Očitao Pero Perić."}
	writeJSON(t, p.dir, "vodostaji_na_crpnim_stanicama", []map[string]any{zapis})
	rep, err := Run(ctx, DirSource{Dir: p.dir}, p.deps)
	if err != nil || rep.Inserted != 1 {
		t.Fatalf("zapis s tipfelerom: upisano %d, %v", rep.Inserted, err)
	}
	var cm sql.NullInt64
	var ag int
	var ocitao string
	if err := p.database.QueryRow(`SELECT level_cm, ag_hours_1, observer FROM readings`).Scan(&cm, &ag, &ocitao); err != nil {
		t.Fatal(err)
	}
	if cm.Valid || ag != 7 || ocitao != "Pero Perić" {
		t.Errorf("zapis s tipfelerom: vodostaj %v, %d h agregata, očitao %q", cm, ag, ocitao)
	}
	kad := time.Date(2026, 9, 5, 7, 0, 0, 0, models.Zagreb)
	s, err := p.readings.SazetakOdbacenih(ctx, p.letva(t))
	if err != nil || s == nil || s.Broj != 1 || s.Zadnja.Opis != "21500 cm" || !s.Zadnja.Kad.Equal(kad) {
		t.Fatalf("sažetak: %+v, %v", s, err)
	}

	// zapis ispravljen u evidenciji BP16: već je uvezen, pa se preskače
	zapis["vodostaj"] = 215
	writeJSON(t, p.dir, "vodostaji_na_crpnim_stanicama", []map[string]any{zapis})
	rep, err = Run(ctx, DirSource{Dir: p.dir}, p.deps)
	if err != nil || rep.Inserted != 0 || rep.Skipped != 1 {
		t.Fatalf("ispravljeni zapis: upisano %d, preskočeno %d, %v", rep.Inserted, rep.Skipped, err)
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): pregled uvoza (bez -upisi)
// kaže isto što uvoz poslije učini. Prije je pregled zapis s tipfelerom
// brojao kao upisan, a sažetak pravog uvoza ga nije spominjao, pa se
// pročitano nije zbrajalo. Sažetak sada navodi zapise koji zbog raspona
// nisu upisani i vrijednosti izvan raspona, i one iz upisanih zapisa.
func TestUvozBP16PregledIUvozBrojeOdbaceneIzvanRaspona(t *testing.T) {
	p := novaPrimjerica(t)
	ctx := context.Background()
	writeJSON(t, p.dir, "vodostaji_na_crpnim_stanicama", []map[string]any{
		{"id": 30, "crpna_stanica": 1, "datum": "2026-09-04", "vrijeme": "07:00:00", "vodostaj": 215, "napomena": "Očitao Pero Perić."},
		{"id": 31, "crpna_stanica": 1, "datum": "2026-09-05", "vrijeme": "07:00:00", "vodostaj": 21500, "napomena": "Očitao Pero Perić."},
		{"id": 32, "crpna_stanica": 1, "datum": "2026-09-06", "vrijeme": "07:00:00", "vodostaj": 21600, "ag_1": 3},
	})
	const sazetak = "pročitano 3, upisano 2, već postojalo 0, nije upisano zbog raspona 1 (vrijednosti izvan raspona 2)"

	// pregled: objekt CS Primjerica već postoji, ništa se ne upisuje
	if _, err := Run(ctx, DirSource{Dir: p.dir}, p.deps); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec(`DELETE FROM readings`); err != nil {
		t.Fatal(err)
	}
	pregled := p.deps
	pregled.DryRun = true
	rep, err := Run(ctx, DirSource{Dir: p.dir}, pregled)
	if err != nil || rep.Inserted != 2 || rep.Odbaceno != 1 || rep.IzvanRaspona != 2 {
		t.Fatalf("pregled: %+v, %v", rep, err)
	}
	if !strings.HasPrefix(rep.Summary(), sazetak) {
		t.Errorf("sažetak pregleda: %s", rep.Summary())
	}
	var n int
	if err := p.database.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("pregled je upisao %d očitanja, %v", n, err)
	}

	rep, err = Run(ctx, DirSource{Dir: p.dir}, p.deps)
	if err != nil || rep.Inserted != 2 || rep.Odbaceno != 1 || rep.IzvanRaspona != 2 {
		t.Fatalf("uvoz: %+v, %v", rep, err)
	}
	if !strings.HasPrefix(rep.Summary(), sazetak) {
		t.Errorf("sažetak uvoza: %s", rep.Summary())
	}
	// uvoz bez ičega izvan raspona sažetak ne produljuje
	if s := (Report{Fetched: 1, Inserted: 1}).Summary(); s != "pročitano 1, upisano 1, već postojalo 0" {
		t.Errorf("sažetak bez odbačenih: %s", s)
	}
}
