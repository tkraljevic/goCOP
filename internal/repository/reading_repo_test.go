package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Letve okoline očitanja: Primjerovo je mjerodavna za dionicu P.1.1 u
// području 1 (sektor P), Bezimena nije ni uz jednu dionicu, a ustava
// Probna je objekt u području 1.
const (
	letvaPrimjerovo = "01900000-0000-7000-8000-0000000b0001"
	letvaBezimena   = "01900000-0000-7000-8000-0000000b0002"
	ustavaProbna    = "01900000-0000-7000-8000-0000000b0003"
)

// kanalPrimjerova je kanal očitanja letve Primjerovo u 2026.
const kanalPrimjerova = "ocitanja/1/2026"

// okolinaOcitanja je baza s letvama i ustavom za upis i čitanje očitanja
type okolinaOcitanja struct {
	baza *sql.DB
	rec  *ledger.Recorder
	repo *ReadingRepository
}

func novaOkolinaOcitanja(t *testing.T) *okolinaOcitanja {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "ocitanja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	const kad = "'2026-01-01 00:00:00'"
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at)
			VALUES ('P.1.1', 1, 'P', 'probna dionica', ` + kad + `, ` + kad + `)`,
		`INSERT INTO stations (id, code, name, created_at, updated_at)
			VALUES ('` + letvaPrimjerovo + `', 'primjerovo', 'Primjerovo', ` + kad + `, ` + kad + `)`,
		`INSERT INTO stations (id, code, name, created_at, updated_at)
			VALUES ('` + letvaBezimena + `', 'bezimena', 'Bezimena', ` + kad + `, ` + kad + `)`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at)
			VALUES ('veza-primjerovo', 'P.1.1', '` + letvaPrimjerovo + `', ` + kad + `)`,
		`INSERT INTO structures (id, code, name, kind, sector_id, area_id, created_at, updated_at)
			VALUES ('` + ustavaProbna + `', 'ustava-probna', 'Ustava Probna', 'USTAVA', 'P', 1, ` + kad + `, ` + kad + `)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatalf("oslonac: %v\n%s", err, q)
		}
	}
	rec := ledger.New(baza, "cvor-probni")
	return &okolinaOcitanja{baza: baza, rec: rec, repo: NewReadingRepository(baza, rec)}
}

// cmVodostaja je vodostaj u cm kao pokazivač, kakav nosi očitanje
func cmVodostaja(v int) *int { return &v }

// u2026 je trenutak u 2026. po hrvatskom vremenu
func u2026(mjesec time.Month, dan, sat, minuta int) time.Time {
	return time.Date(2026, mjesec, dan, sat, minuta, 0, 0, models.Zagreb)
}

// upisi upisuje ručno očitanje kroz repozitorij; letva je postaja ili, uz
// objekt, prazna
func (o *okolinaOcitanja) upisi(t *testing.T, letva, objekt string, kad time.Time, cm *int, podrijetlo string) models.Reading {
	t.Helper()
	rd := models.Reading{StationID: letva, StructureID: objekt, MeasuredAt: kad, LevelCm: cm,
		Source: models.ReadingSourceManual, Origin: podrijetlo, Observer: "Pero Perić"}
	if err := o.repo.Create(context.Background(), &rd); err != nil {
		t.Fatal(err)
	}
	return rd
}

// verzije vraća verzije očitanja u knjizi, najnoviju prvu
func (o *okolinaOcitanja) verzije(t *testing.T, id uuid.UUID) []ledger.Version {
	t.Helper()
	v, err := o.rec.History(context.Background(), EntityReadings, id.String())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// vodostaj čita vodostaj očitanja izravno s površine; -1 kad očitanja nema
func (o *okolinaOcitanja) vodostaj(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var cm sql.NullInt64
	err := o.baza.QueryRow(`SELECT level_cm FROM readings WHERE id = ?`, id.String()).Scan(&cm)
	if err == sql.ErrNoRows {
		return -1
	}
	if err != nil {
		t.Fatal(err)
	}
	return int(cm.Int64)
}

// otkazan je kontekst zahtjeva koji je korisnik već prekinuo
func otkazan() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// zabranaKnjige je okidač koji odbija svaki upis verzije u knjigu
const zabranaKnjige = `CREATE TRIGGER zabrana_knjige BEFORE INSERT ON record_versions
	BEGIN SELECT RAISE(ABORT, 'knjiga ne prima'); END`

// Izmjena mijenja površinu i ostavlja novu verziju s izmijenjenim zapisom u
// kanalu područja letve i godine mjerenja. Očitanje kojeg nema ne mijenja
// se i ne ostavlja verziju.
func TestIzmjenaOcitanja(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	rd := o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 10, 7, 0), cmVodostaja(152), models.ReadingOriginGoCOP)

	rd.LevelCm = cmVodostaja(158)
	rd.Note = "ispravljen upis"
	rd.MeasuredAt = u2026(time.March, 10, 7, 30)
	if err := o.repo.Update(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	natrag, err := o.repo.Get(ctx, rd.ID)
	if err != nil || natrag == nil {
		t.Fatalf("izmijenjeno očitanje: %v %v", natrag, err)
	}
	if natrag.LevelCm == nil || *natrag.LevelCm != 158 || natrag.Note != "ispravljen upis" || !natrag.MeasuredAt.Equal(rd.MeasuredAt) {
		t.Errorf("površina nakon izmjene: vodostaj %v, napomena %q, vrijeme %v", natrag.LevelCm, natrag.Note, natrag.MeasuredAt)
	}
	verzije := o.verzije(t, rd.ID)
	if len(verzije) != 2 || verzije[0].Archived {
		t.Fatalf("verzije nakon izmjene: %d", len(verzije))
	}
	if verzije[0].Channel != kanalPrimjerova {
		t.Errorf("kanal izmjene: %q, očekivan %q", verzije[0].Channel, kanalPrimjerova)
	}
	var uVerziji models.Reading
	nuzno(t, json.Unmarshal(verzije[0].Payload, &uVerziji))
	if uVerziji.LevelCm == nil || *uVerziji.LevelCm != 158 || uVerziji.Note != "ispravljen upis" {
		t.Errorf("verzija ne nosi izmjenu: vodostaj %v, napomena %q", uVerziji.LevelCm, uVerziji.Note)
	}

	nema := models.Reading{ID: uuid.New(), StationID: letvaPrimjerovo, MeasuredAt: u2026(time.March, 10, 8, 0), LevelCm: cmVodostaja(1)}
	if err := o.repo.Update(ctx, &nema); err == nil || !strings.Contains(err.Error(), "očitanje ne postoji") {
		t.Errorf("izmjena nepostojećeg očitanja: %v", err)
	}
	if v := o.verzije(t, nema.ID); len(v) != 0 {
		t.Errorf("nepostojeće očitanje ima %d verzija", len(v))
	}
}

// Izmjena koja ne uspije u bilo kojem koraku ne mijenja ništa: površina
// ostaje kakva je bila, a u knjizi nema nove verzije
func TestIzmjenaOcitanjaNeuspjelaNistaNeMijenja(t *testing.T) {
	for _, slucaj := range []struct {
		naziv, q string
		otkazan  bool
	}{
		{"otkazan zahtjev", "", true},
		{"upis zabranjen", `CREATE TRIGGER zabrana BEFORE UPDATE OF level_cm ON readings
			BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`, false},
		{"izmijenjeni zapis se ne da pročitati", `CREATE TRIGGER pokvari AFTER UPDATE OF level_cm ON readings
			BEGIN UPDATE readings SET measured_at = 'nije vrijeme' WHERE id = NEW.id; END`, false},
		{"kanal se ne da upisati", `CREATE TRIGGER zabrana BEFORE UPDATE OF channel ON readings
			BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`, false},
		{"knjiga ne prima verziju", zabranaKnjige, false},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaOcitanja(t)
			rd := o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 10, 7, 0), cmVodostaja(152), models.ReadingOriginGoCOP)
			if slucaj.q != "" {
				if _, err := o.baza.Exec(slucaj.q); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if slucaj.otkazan {
				ctx = otkazan()
			}
			rd.LevelCm = cmVodostaja(158)
			if err := o.repo.Update(ctx, &rd); err == nil {
				t.Fatal("izmjena je prošla")
			}
			if cm := o.vodostaj(t, rd.ID); cm != 152 {
				t.Errorf("vodostaj na površini nakon neuspjele izmjene: %d, očekivano 152", cm)
			}
			if v := o.verzije(t, rd.ID); len(v) != 1 {
				t.Errorf("verzija nakon neuspjele izmjene: %d, očekivana 1", len(v))
			}
		})
	}
}

// Obrisano očitanje nestaje s površine, a u knjizi ostaje arhivirana verzija
// s cijelim zapisom, u kanalu područja letve — tako brisanje stiže na svaki
// čvor koji prati to područje. Očitanja kojeg nema nema ni što obrisati.
func TestBrisanjeOcitanja(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	rd := o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 10, 7, 0), cmVodostaja(152), models.ReadingOriginGoCOP)
	if err := o.repo.Delete(ctx, rd.ID); err != nil {
		t.Fatal(err)
	}
	if natrag, err := o.repo.Get(ctx, rd.ID); err != nil || natrag != nil {
		t.Errorf("obrisano očitanje je još na površini: %v %v", natrag, err)
	}
	verzije := o.verzije(t, rd.ID)
	if len(verzije) != 2 || !verzije[0].Archived || verzije[1].Archived {
		t.Fatalf("verzije nakon brisanja: %+v", verzije)
	}
	if verzije[0].Channel != kanalPrimjerova {
		t.Errorf("kanal brisanja: %q, očekivan %q", verzije[0].Channel, kanalPrimjerova)
	}
	var arhivirano models.Reading
	nuzno(t, json.Unmarshal(verzije[0].Payload, &arhivirano))
	if arhivirano.ID != rd.ID || arhivirano.LevelCm == nil || *arhivirano.LevelCm != 152 || arhivirano.StationID != letvaPrimjerovo {
		t.Errorf("arhivirana verzija ne nosi zapis: %+v", arhivirano)
	}

	nema := uuid.New()
	if err := o.repo.Delete(ctx, nema); err == nil || !strings.Contains(err.Error(), "očitanje ne postoji") {
		t.Errorf("brisanje nepostojećeg očitanja: %v", err)
	}
	if v := o.verzije(t, nema); len(v) != 0 {
		t.Errorf("nepostojeće očitanje ima %d verzija", len(v))
	}
}

// Brisanje koje ne uspije u bilo kojem koraku ne briše ništa: očitanje
// ostaje na površini, a u knjizi nema arhivirane verzije
func TestBrisanjeOcitanjaNeuspjeloNistaNeBrise(t *testing.T) {
	for _, slucaj := range []struct {
		naziv, q string
		otkazan  bool
	}{
		{"otkazan zahtjev", "", true},
		{"zapis se ne da pročitati", `UPDATE readings SET measured_at = 'nije vrijeme'`, false},
		{"brisanje zabranjeno", `CREATE TRIGGER zabrana BEFORE DELETE ON readings
			BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`, false},
		{"knjiga ne prima verziju", zabranaKnjige, false},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaOcitanja(t)
			rd := o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 10, 7, 0), cmVodostaja(152), models.ReadingOriginGoCOP)
			if slucaj.q != "" {
				if _, err := o.baza.Exec(slucaj.q); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if slucaj.otkazan {
				ctx = otkazan()
			}
			if err := o.repo.Delete(ctx, rd.ID); err == nil {
				t.Fatal("brisanje je prošlo")
			}
			if cm := o.vodostaj(t, rd.ID); cm != 152 {
				t.Errorf("očitanje nakon neuspjelog brisanja: vodostaj %d, očekivano 152", cm)
			}
			if v := o.verzije(t, rd.ID); len(v) != 1 || v[0].Archived {
				t.Errorf("knjiga nakon neuspjelog brisanja: %d verzija", len(v))
			}
		})
	}
}

// Pregled letvi: za svaku letvu (postaju ili objekt) zadnja dva očitanja,
// najnovije prvo, i broj svih njezinih očitanja
func TestZadnjaOcitanjaIBrojPoLetvi(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	for _, sat := range []int{7, 9, 8} {
		o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 10, sat, 0), cmVodostaja(100+sat), models.ReadingOriginGoCOP)
	}
	o.upisi(t, letvaBezimena, "", u2026(time.March, 10, 7, 0), cmVodostaja(40), models.ReadingOriginGoCOP)
	o.upisi(t, "", ustavaProbna, u2026(time.March, 11, 6, 0), cmVodostaja(75), models.ReadingOriginGoCOP)

	zadnja, err := o.repo.LatestPerGauge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(zadnja) != 3 {
		t.Fatalf("letvi u pregledu: %d, očekivane 3 (%v)", len(zadnja), zadnja)
	}
	p := zadnja["station:"+letvaPrimjerovo]
	if len(p) != 2 || *p[0].LevelCm != 109 || *p[1].LevelCm != 108 {
		t.Errorf("zadnja očitanja Primjerova: %+v", p)
	}
	if u := zadnja["structure:"+ustavaProbna]; len(u) != 1 || *u[0].LevelCm != 75 {
		t.Errorf("zadnja očitanja ustave: %+v", u)
	}

	broj, err := o.repo.CountPerGauge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := map[string]int{"station:" + letvaPrimjerovo: 3, "station:" + letvaBezimena: 1, "structure:" + ustavaProbna: 1}
	if !reflect.DeepEqual(broj, ocekivano) {
		t.Errorf("broj očitanja po letvi: %v, očekivano %v", broj, ocekivano)
	}
}

// Uvoz preskače što već ima: skup postojećih identifikatora sadrži samo
// očitanja danog podrijetla; podrijetlo bez očitanja daje prazan skup
func TestPostojeciIdentifikatoriPoPodrijetlu(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	prvo := o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 1, 7, 0), cmVodostaja(120), models.ReadingOriginArchive)
	drugo := o.upisi(t, letvaBezimena, "", u2026(time.March, 2, 7, 0), cmVodostaja(121), models.ReadingOriginArchive)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 3, 7, 0), cmVodostaja(122), models.ReadingOriginGoCOP)

	ids, err := o.repo.ExistingIDs(ctx, models.ReadingOriginArchive)
	if err != nil {
		t.Fatal(err)
	}
	if ocekivano := map[string]bool{prvo.ID.String(): true, drugo.ID.String(): true}; !reflect.DeepEqual(ids, ocekivano) {
		t.Errorf("arhivska očitanja: %v, očekivano %v", ids, ocekivano)
	}
	if ids, err := o.repo.ExistingIDs(ctx, "NEPOZNATO"); err != nil || ids == nil || len(ids) != 0 {
		t.Errorf("podrijetlo bez očitanja: %v %v", ids, err)
	}
}

// Ukupan broj očitanja i raspon vremena; prazna evidencija daje nulu i
// prazna vremena, a ne grešku
func TestStatistikaOcitanja(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	ukupno, prvo, zadnje, err := o.repo.Stats(ctx)
	if err != nil || ukupno != 0 || !prvo.IsZero() || !zadnje.IsZero() {
		t.Errorf("prazna evidencija: %d %v %v %v", ukupno, prvo, zadnje, err)
	}

	najranije, najkasnije := u2026(time.February, 28, 23, 30), u2026(time.March, 12, 7, 0)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 5, 7, 0), cmVodostaja(130), models.ReadingOriginGoCOP)
	o.upisi(t, letvaPrimjerovo, "", najkasnije, cmVodostaja(131), models.ReadingOriginGoCOP)
	o.upisi(t, "", ustavaProbna, najranije, cmVodostaja(60), models.ReadingOriginGoCOP)
	ukupno, prvo, zadnje, err = o.repo.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ukupno != 3 || !prvo.Equal(najranije) || !zadnje.Equal(najkasnije) {
		t.Errorf("statistika: %d očitanja od %v do %v, očekivano 3 od %v do %v", ukupno, prvo, zadnje, najranije, najkasnije)
	}
}

// Vrijeme iz agregata SQLite-a dolazi kao tekst u jednom od zapisa koje
// upisuju upravljački program i stariji uvozi; nepoznat zapis nije vrijeme
func TestVrijemeIzAgregata(t *testing.T) {
	trenutak := time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC)
	for _, slucaj := range []struct {
		tekst     string
		ocekivano time.Time
		ok        bool
	}{
		{"2026-03-10T06:00:00.25Z", trenutak.Add(250 * time.Millisecond), true},
		{"2026-03-10T07:00:00+01:00", trenutak, true},
		{"2026-03-10 06:00:00 +0000 UTC", trenutak, true},
		{"2026-03-10 07:00:00.5+01:00", trenutak.Add(500 * time.Millisecond), true},
		{"2026-03-10 06:00:00", trenutak, true},
		{"10.03.2026. 7:00", time.Time{}, false},
		{"", time.Time{}, false},
	} {
		kad, ok := parseDBTime(slucaj.tekst)
		if ok != slucaj.ok || !kad.Equal(slucaj.ocekivano) {
			t.Errorf("%q: %v %v, očekivano %v %v", slucaj.tekst, kad, ok, slucaj.ocekivano, slucaj.ok)
		}
	}
}

// Prvo očitanje letve je najstarije s vodostajem; očitanje bez vodostaja
// (samo temperatura) se ne broji. Letva bez očitanja, prazna šifra i
// spremište kojeg nema nemaju prvo očitanje.
func TestPrvoOcitanjeLetve(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	prvo := u2026(time.March, 2, 7, 0)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 1, 7, 0), nil, models.ReadingOriginGoCOP)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 3, 7, 0), cmVodostaja(140), models.ReadingOriginGoCOP)
	o.upisi(t, letvaPrimjerovo, "", prvo, cmVodostaja(141), models.ReadingOriginGoCOP)

	if kad, ok := o.repo.PrvoOcitanje(ctx, letvaPrimjerovo); !ok || !kad.Equal(prvo) {
		t.Errorf("prvo očitanje: %v %v, očekivano %v", kad, ok, prvo)
	}
	var bezSpremista *ReadingRepository
	for naziv, provjera := range map[string]func() (time.Time, bool){
		"letva bez očitanja": func() (time.Time, bool) { return o.repo.PrvoOcitanje(ctx, letvaBezimena) },
		"prazna šifra":       func() (time.Time, bool) { return o.repo.PrvoOcitanje(ctx, "") },
		"bez spremišta":      func() (time.Time, bool) { return bezSpremista.PrvoOcitanje(ctx, letvaPrimjerovo) },
	} {
		if kad, ok := provjera(); ok || !kad.IsZero() {
			t.Errorf("%s: %v %v", naziv, kad, ok)
		}
	}
}

// Krajnosti iz operativnih očitanja: najviši i najniži vodostaj letve, uz
// jednake vrijednosti onaj prvi zabilježen, s danom po hrvatskom vremenu i
// podrijetlom očitanja. Letva bez očitanja nema krajnosti.
func TestKrajnostiIzOcitanja(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	// 10. 3. u 23:30 UTC je 11. 3. u Hrvatskoj
	prviVrh := time.Date(2026, 3, 10, 23, 30, 0, 0, time.UTC)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 9, 7, 0), cmVodostaja(210), models.ReadingOriginGoCOP)
	o.upisi(t, letvaPrimjerovo, "", prviVrh, cmVodostaja(340), models.ReadingOriginArchive)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 14, 7, 0), cmVodostaja(340), models.ReadingOriginGoCOP)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 20, 7, 0), cmVodostaja(85), models.ReadingOriginGoCOP)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 21, 7, 0), nil, models.ReadingOriginGoCOP)

	dobiveno := o.repo.Krajnosti(ctx, letvaPrimjerovo)
	ocekivano := []models.KrajnostIzNiza{
		{Kind: models.ExtremeMax, LevelCm: 340, OnDate: "2026-03-11", Izvor: models.ReadingOriginArchive, Odakle: "očitanja"},
		{Kind: models.ExtremeMin, LevelCm: 85, OnDate: "2026-03-20", Izvor: models.ReadingOriginGoCOP, Odakle: "očitanja"},
	}
	if !reflect.DeepEqual(dobiveno, ocekivano) {
		t.Errorf("krajnosti: %+v\nočekivano %+v", dobiveno, ocekivano)
	}

	var bezSpremista *ReadingRepository
	for naziv, k := range map[string][]models.KrajnostIzNiza{
		"letva bez očitanja": o.repo.Krajnosti(ctx, letvaBezimena),
		"prazna šifra":       o.repo.Krajnosti(ctx, ""),
		"bez spremišta":      bezSpremista.Krajnosti(ctx, letvaPrimjerovo),
	} {
		if k != nil {
			t.Errorf("%s: %+v", naziv, k)
		}
	}
}

// Greška baze ne prolazi tiho: iz zatvorene baze popisi i brojevi vraćaju
// grešku, a ne prazan rezultat; prvo očitanje i krajnosti tada kažu da ih
// nema. Zapis koji se ne da pročitati ruši pregled umjesto da ga preskoči.
func TestOcitanjaGreskeBaze(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 10, 7, 0), cmVodostaja(152), models.ReadingOriginGoCOP)
	if _, err := o.baza.Exec(`UPDATE readings SET measured_at = 'nije vrijeme'`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.repo.LatestPerGauge(ctx); err == nil {
		t.Error("pregled s nečitljivim zapisom bez greške")
	}

	o.baza.Close()
	if _, err := o.repo.LatestPerGauge(ctx); err == nil {
		t.Error("pregled iz zatvorene baze bez greške")
	}
	if _, err := o.repo.CountPerGauge(ctx); err == nil {
		t.Error("broj po letvi iz zatvorene baze bez greške")
	}
	if _, err := o.repo.ExistingIDs(ctx, models.ReadingOriginArchive); err == nil {
		t.Error("postojeći identifikatori iz zatvorene baze bez greške")
	}
	if _, _, _, err := o.repo.Stats(ctx); err == nil {
		t.Error("statistika iz zatvorene baze bez greške")
	}
	if _, ok := o.repo.PrvoOcitanje(ctx, letvaPrimjerovo); ok {
		t.Error("prvo očitanje iz zatvorene baze")
	}
	if k := o.repo.Krajnosti(ctx, letvaPrimjerovo); k != nil {
		t.Errorf("krajnosti iz zatvorene baze: %+v", k)
	}
}
