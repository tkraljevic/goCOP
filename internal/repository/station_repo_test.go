package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// okolinaPostaja je registar s četiri letve i dionicama u dva sektora:
// Primjerovo je mjerodavna za P.1.1, P.1.2 (područje 1, sektor P) i R.2.1
// (područje 2, sektor R), Probno samo za P.1.1, a Bezimena i Kanalska ni za
// jednu. Bezimena nema upisanu vodu, a Primjerovo čeka pregled.
type okolinaPostaja struct {
	baza                                   *sql.DB
	rec                                    *ledger.Recorder
	repo                                   *StationRepository
	primjerovo, probno, bezimena, kanalska *models.Station
}

func novaOkolinaPostaja(t *testing.T) *okolinaPostaja {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "postaje.db"))
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
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('R', 'Sektor R', 'VGO Rubno', 'COP Rubno')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (2, 'R', 'Mali sliv Rubnica', 'VGI Rubnica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'prva', ` + kad + `, ` + kad + `)`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.2', 1, 'P', 'druga', ` + kad + `, ` + kad + `)`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('R.2.1', 2, 'R', 'rubna', ` + kad + `, ` + kad + `)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatalf("oslonac: %v\n%s", err, q)
		}
	}
	rec := ledger.New(baza, "cvor-probni")
	o := &okolinaPostaja{baza: baza, rec: rec, repo: NewStationRepository(baza, rec),
		primjerovo: &models.Station{Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica", NeedsReview: true},
		probno:     &models.Station{Code: "probno", Name: "Probno", Watercourse: "Probnica"},
		bezimena:   &models.Station{Code: "bezimena", Name: "Bezimena", Watercourse: "   "},
		kanalska:   &models.Station{Code: "kanalska", Name: "Kanalska", Watercourse: "kanal Probni"},
	}
	ctx := context.Background()
	for _, st := range []*models.Station{o.primjerovo, o.probno, o.bezimena, o.kanalska} {
		if err := o.repo.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	// veze se upisuju mimo knjige, kao oslonac
	for i, veza := range []struct {
		dionica string
		letva   uuid.UUID
	}{
		{"R.2.1", o.primjerovo.ID}, {"P.1.2", o.primjerovo.ID}, {"P.1.1", o.primjerovo.ID}, {"P.1.1", o.probno.ID},
	} {
		if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
			"veza-"+string(rune('a'+i)), veza.dionica, veza.letva.String(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

// nazivi vraća nazive postaja redom
func nazivi(postaje []models.Station) []string {
	var out []string
	for _, st := range postaje {
		out = append(out, st.Name)
	}
	return out
}

// Mjerodavni vodomjeri dionice idu po nazivu; dionica bez vodomjera i
// dionica koje nema daju prazan popis, a ne grešku
func TestMjerodavniVodomjeriDionice(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	for _, slucaj := range []struct {
		dionica   string
		ocekivano []string
	}{
		{"P.1.1", []string{"Primjerovo", "Probno"}},
		{"R.2.1", []string{"Primjerovo"}},
		{"X.9.9", nil},
	} {
		postaje, err := o.repo.GetStationsForSection(ctx, slucaj.dionica)
		if err != nil {
			t.Fatalf("%s: %v", slucaj.dionica, err)
		}
		if !reflect.DeepEqual(nazivi(postaje), slucaj.ocekivano) {
			t.Errorf("%s: %v, očekivano %v", slucaj.dionica, nazivi(postaje), slucaj.ocekivano)
		}
	}
}

// Popis postaja svakoj letvi pridružuje dionice za koje je mjerodavna,
// poredane po šifri; letva bez dionica ih nema. Pretraga bez pogotka daje
// prazan popis.
func TestPopisPostajaSDionicama(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	postaje, err := o.repo.ListStations(ctx, "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	dionice := map[string][]string{}
	for _, st := range postaje {
		dionice[st.Code] = st.SectionCodes
	}
	ocekivano := map[string][]string{
		"primjerovo": {"P.1.1", "P.1.2", "R.2.1"},
		"probno":     {"P.1.1"},
		"bezimena":   nil,
		"kanalska":   nil,
	}
	if !reflect.DeepEqual(dionice, ocekivano) {
		t.Errorf("dionice po letvi: %v, očekivano %v", dionice, ocekivano)
	}
	if prazno, err := o.repo.ListStations(ctx, "nema-takve", "", "", false); err != nil || len(prazno) != 0 {
		t.Errorf("pretraga bez pogotka: %v %v", nazivi(prazno), err)
	}
}

// Popis postaja javlja grešku kad se veze s dionicama ne daju pročitati,
// umjesto da letve tiho ostanu bez dionica
func TestPopisPostajaVezeSeNeDajuProcitati(t *testing.T) {
	for _, slucaj := range []struct {
		naziv string
		upiti []string
	}{
		{"nema tablice veza", []string{`ALTER TABLE section_stations RENAME TO section_stations_negdje`}},
		{"veza bez letve", []string{
			`DROP TABLE section_stations`,
			`CREATE TABLE section_stations (station_id TEXT, section_code TEXT)`,
			`INSERT INTO section_stations VALUES (NULL, 'P.1.1')`,
		}},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaPostaja(t)
			for _, q := range slucaj.upiti {
				if _, err := o.baza.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if postaje, err := o.repo.ListStations(context.Background(), "", "", "", false); err == nil {
				t.Errorf("popis bez greške: %v", nazivi(postaje))
			}
		})
	}
}

// Sektori i područja postaje slijede iz dionica za koje je mjerodavna, svaki
// jednom; letva bez dionica nema ni sektor ni područje
func TestSektoriIPodrucjaPostaja(t *testing.T) {
	o := novaOkolinaPostaja(t)
	sektori, podrucja, err := o.repo.StationScopes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sektori {
		sort.Strings(s)
	}
	for _, p := range podrucja {
		sort.Ints(p)
	}
	ocekivaniSektori := map[string][]string{o.primjerovo.ID.String(): {"P", "R"}, o.probno.ID.String(): {"P"}}
	ocekivanaPodrucja := map[string][]int{o.primjerovo.ID.String(): {1, 2}, o.probno.ID.String(): {1}}
	if !reflect.DeepEqual(sektori, ocekivaniSektori) {
		t.Errorf("sektori: %v, očekivano %v", sektori, ocekivaniSektori)
	}
	if !reflect.DeepEqual(podrucja, ocekivanaPodrucja) {
		t.Errorf("područja: %v, očekivano %v", podrucja, ocekivanaPodrucja)
	}
}

// Popis voda za filtar: svaka voda jednom, bez velikih i malih slova u
// redoslijedu; letva bez upisane vode ne daje praznu vodu
func TestPopisVodaRegistra(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	druga := &models.Station{Code: "primjerovo-2", Name: "Primjerovo II", Watercourse: "Primjerica"}
	if err := o.repo.CreateStation(ctx, druga); err != nil {
		t.Fatal(err)
	}
	vode, err := o.repo.ListWatercourses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ocekivano := []string{"kanal Probni", "Primjerica", "Probnica"}; !reflect.DeepEqual(vode, ocekivano) {
		t.Errorf("vode: %q, očekivano %q", vode, ocekivano)
	}
}

// Brojevi registra: postaje, one koje čekaju pregled i veze s dionicama;
// uz letvu i broj njezinih očitanja, koji se pokazuje prije brisanja
func TestBrojeviRegistraPostaja(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	ukupno, zaPregled, veze, err := o.repo.CountStations(ctx)
	if err != nil || ukupno != 4 || zaPregled != 1 || veze != 4 {
		t.Errorf("registar: %d postaja, %d za pregled, %d veza (%v); očekivano 4, 1, 4", ukupno, zaPregled, veze, err)
	}

	ocitanja := NewReadingRepository(o.baza, o.rec)
	for _, sat := range []int{7, 19} {
		rd := models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: u2026(time.March, 10, sat, 0),
			LevelCm: cmVodostaja(120), Source: models.ReadingSourceManual, Origin: models.ReadingOriginGoCOP}
		if err := ocitanja.Create(ctx, &rd); err != nil {
			t.Fatal(err)
		}
	}
	if n := o.repo.BrojOcitanja(ctx, o.primjerovo.ID); n != 2 {
		t.Errorf("očitanja Primjerova: %d, očekivana 2", n)
	}
	if n := o.repo.BrojOcitanja(ctx, o.probno.ID); n != 0 {
		t.Errorf("očitanja Probnog: %d, očekivano 0", n)
	}
}

// Greška baze ne prolazi tiho: iz zatvorene baze popisi i brojevi registra
// vraćaju grešku, a ne prazan rezultat; broj očitanja tada je nula
func TestPostajeIzZatvoreneBaze(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	o.baza.Close()
	if _, err := o.repo.GetStationsForSection(ctx, "P.1.1"); err == nil {
		t.Error("vodomjeri dionice iz zatvorene baze bez greške")
	}
	if _, err := o.repo.ListWatercourses(ctx); err == nil {
		t.Error("popis voda iz zatvorene baze bez greške")
	}
	if _, _, err := o.repo.StationScopes(ctx); err == nil {
		t.Error("sektori postaja iz zatvorene baze bez greške")
	}
	if _, _, _, err := o.repo.CountStations(ctx); err == nil {
		t.Error("brojevi registra iz zatvorene baze bez greške")
	}
	if n := o.repo.BrojOcitanja(ctx, o.primjerovo.ID); n != 0 {
		t.Errorf("broj očitanja iz zatvorene baze: %d", n)
	}
}

// Upis postaje koji ne uspije ne ostavlja ništa: nova postaja ne ulazi u
// registar, a izmijenjena ostaje kakva je bila, i u knjizi nema nove
// verzije. Šifra je jedinstvena, pa ni nova ni izmijenjena letva ne može
// preuzeti tuđu.
func TestUpisPostajeNeuspioNistaNeMijenja(t *testing.T) {
	for _, slucaj := range []struct {
		naziv, q, sifra string
		nova, otkazan   bool
	}{
		{"nova/tuđa šifra", "", "probno", true, false},
		{"nova/knjiga ne prima verziju", zabranaKnjige, "nova", true, false},
		{"nova/otkazan zahtjev", "", "nova", true, true},
		{"izmjena/tuđa šifra", "", "primjerovo", false, false},
		{"izmjena/knjiga ne prima verziju", zabranaKnjige, "probno", false, false},
		{"izmjena/otkazan zahtjev", "", "probno", false, true},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaPostaja(t)
			if slucaj.q != "" {
				if _, err := o.baza.Exec(slucaj.q); err != nil {
					t.Fatal(err)
				}
			}
			ctx, citanje := context.Background(), context.Background()
			if slucaj.otkazan {
				ctx = otkazan()
			}
			if slucaj.nova {
				st := &models.Station{Code: slucaj.sifra, Name: "Nova"}
				if err := o.repo.CreateStation(ctx, st); err == nil {
					t.Fatal("upis nove postaje je prošao")
				}
				if ukupno, _, _, err := o.repo.CountStations(citanje); err != nil || ukupno != 4 {
					t.Errorf("postaja u registru: %d %v, očekivane 4", ukupno, err)
				}
				if v, err := o.rec.History(citanje, EntityStations, st.ID.String()); err != nil || len(v) != 0 {
					t.Errorf("neupisana postaja ima %d verzija (%v)", len(v), err)
				}
				return
			}
			st := *o.probno
			st.Code, st.Name = slucaj.sifra, "Probno novo"
			if err := o.repo.UpdateStation(ctx, &st); err == nil {
				t.Fatal("izmjena postaje je prošla")
			}
			natrag, err := o.repo.GetStationByID(citanje, o.probno.ID)
			if err != nil || natrag == nil || natrag.Code != "probno" || natrag.Name != "Probno" {
				t.Errorf("postaja nakon neuspjele izmjene: %+v %v", natrag, err)
			}
			if v, err := o.rec.History(citanje, EntityStations, o.probno.ID.String()); err != nil || len(v) != 1 {
				t.Errorf("verzija nakon neuspjele izmjene: %d (%v), očekivana 1", len(v), err)
			}
		})
	}
}

// Izmjena letve koje nema u registru javlja da je nema i ne ostavlja verziju
func TestIzmjenaPostajeKojeNema(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	nema := &models.Station{ID: uuid.New(), Code: "nema", Name: "Nema"}
	if err := o.repo.UpdateStation(ctx, nema); err == nil || err.Error() != "vodomjerna postaja nije pronađena" {
		t.Errorf("izmjena letve koje nema: %v", err)
	}
	if v, err := o.rec.History(ctx, EntityStations, nema.ID.String()); err != nil || len(v) != 0 {
		t.Errorf("letva koje nema ima %d verzija (%v)", len(v), err)
	}
}
