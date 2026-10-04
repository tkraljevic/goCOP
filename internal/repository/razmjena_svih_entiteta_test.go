package repository

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/obracun"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

// Razmjena svih entiteta: svaki entitet koji primjena razmjene (applyOne)
// zna upisati na površinu prolazi isti put kao u pogonu — upis kroz
// repozitorij na čvoru A, verzije iz knjige A u knjigu B, primjena na
// površinu B — i redak na B mora biti jednak retku na A, stupac po stupac.
// Uz to, za svaki entitet:
//
//   - verzija s poljem koje ovaj program ne poznaje (iz novijeg programa)
//     primjenjuje se bez greške, a knjiga B je čuva bajt za bajt;
//   - lokalna izmjena na B kroz repozitorij to polje prenosi u novu verziju;
//   - arhivirana verzija miče redak s površine B.
//
// Punilo popunjava svako polje, i ugniježđene strukture, vremena, pokazivače,
// popise i mape, pa test pada i kad entitet dobije polje koje primjena ne
// upisuje. Zapis se na oba čvora čita i stvarnim čitačem repozitorija
// (procitaj), koji vremena raščlanjuje u time.Time.

// poznateGreske su provjere koje padaju zbog grešaka u programu koje se
// ispravljaju odvojeno. Ključ je "<entitet>/<provjera>"; provjera se tada
// preskače s objašnjenjem, a kad greška nestane, test traži da se zapis
// makne s popisa.
var poznateGreske = map[string]string{}

// prijaviIliPreskoci javlja grešku, osim kad je provjera na popisu poznatih
func prijaviIliPreskoci(t *testing.T, kljuc, format string, args ...any) {
	t.Helper()
	if razlog, ok := poznateGreske[kljuc]; ok {
		t.Skipf("poznata greška %s: %s (%s)", kljuc, razlog, fmt.Sprintf(format, args...))
	}
	t.Errorf(format, args...)
}

// prosloBezGreske javlja kad provjera s popisa poznatih grešaka prođe: zapis
// tada treba maknuti, inače bi skrivao buduće padove
func prosloBezGreske(t *testing.T, kljuc string) {
	t.Helper()
	if razlog, ok := poznateGreske[kljuc]; ok {
		t.Errorf("poznata greška %s više se ne javlja (%s) — makni je iz poznateGreske", kljuc, razlog)
	}
}

// ---- dva čvora ----

type cvor struct {
	ime string
	db  *sql.DB
	rec *ledger.Recorder
}

var (
	osnovaKorisnik = uuid.MustParse("01900000-0000-7000-8000-00000000a001")
	osnovaLetva    = uuid.MustParse("01900000-0000-7000-8000-00000000a002")
	osnovaDnevnik  = "01900000-0000-7000-8000-00000000a003"
)

const (
	osnovaDionica  = "B.16.1"
	osnovaZupanija = 901
	osnovaOpcina   = 801
)

// noviCvor otvara praznu bazu sa shemom i istim oslonom na oba čvora:
// sektor, područje, korisnik, letva, dionica, dnevnik, županija i općina.
// Oslonac se upisuje mimo knjige, pa ne putuje i ne miješa se s verzijama
// entiteta koji se ispituje.
func noviCvor(t *testing.T, ime string) *cvor {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	const kad = "2026-01-01 00:00:00"
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Područje 16', 'VGI')`,
		`INSERT INTO users (id, username, password_hash, full_name, org_type, created_at, updated_at)
			VALUES ('` + osnovaKorisnik.String() + `', 'proba', 'x', 'Proba Probić', 'HRVATSKE_VODE', '` + kad + `', '` + kad + `')`,
		`INSERT INTO stations (id, code, name, created_at, updated_at)
			VALUES ('` + osnovaLetva.String() + `', 'proba-letva', 'Proba', '` + kad + `', '` + kad + `')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at)
			VALUES ('` + osnovaDionica + `', 16, 'B', 'probna', '` + kad + `', '` + kad + `')`,
		`INSERT INTO journals (id, area_id, kind, created_at, updated_at)
			VALUES ('` + osnovaDnevnik + `', 16, 'COP', '` + kad + `', '` + kad + `')`,
		`INSERT INTO counties (id, code, name, seat) VALUES (901, 'PRB', 'Probna', 'Sjedište')`,
		`INSERT INTO municipalities (id, county_id, name, type) VALUES (801, 901, 'Probna Općina', 'OPĆINA')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatalf("oslonac: %v\n%s", err, q)
		}
	}
	return &cvor{ime: ime, db: baza, rec: ledger.New(baza, ime)}
}

var ctxRaz = context.Background()

// ---- punilo ----

var (
	tipVremena    = reflect.TypeOf(time.Time{})
	tipUUID       = reflect.TypeOf(uuid.UUID{})
	tipSirovog    = reflect.TypeOf(json.RawMessage{})
	vrijemePunila = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

	// zonaPunila je zona u kojoj punilo daje vremena; mijenja je samo
	// TestRazmjenaSvihEntitetaUZoniBezImena
	zonaPunila = time.UTC
)

// trenutakPunila je vrijemePunila u zoni punila
func trenutakPunila() time.Time { return vrijemePunila.In(zonaPunila) }

// punilo popunjava vrijednost rekurzivno: tekst "x-<Polje>", brojevi
// međusobno različiti i različiti od nule, istina, vremena u zoni punila na
// sekundu, pokazivači, UUID iz naziva polja, popisi s jednim ili dva
// člana, mape s jednim unosom i ugniježđene strukture
type punilo struct{ n int }

func puni(x any) {
	(&punilo{}).puni(reflect.ValueOf(x).Elem(), "", 0)
}

func (p *punilo) puni(v reflect.Value, ime string, dubina int) {
	if dubina > 8 || !v.CanSet() {
		return
	}
	switch v.Type() {
	case tipVremena:
		p.n++
		v.Set(reflect.ValueOf(trenutakPunila().Add(time.Duration(p.n) * time.Minute)))
		return
	case tipUUID:
		v.Set(reflect.ValueOf(uuid.NewSHA1(uuid.NameSpaceOID, []byte(ime))))
		return
	case tipSirovog:
		v.SetBytes([]byte(`{"x":"` + ime + `"}`))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("x-" + ime)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int16, reflect.Int32, reflect.Int64:
		p.n++
		v.SetInt(int64(100 + p.n))
	case reflect.Int8, reflect.Uint8:
		p.n++
		if v.Kind() == reflect.Int8 {
			v.SetInt(int64(p.n%100 + 1))
		} else {
			v.SetUint(uint64(p.n%100 + 1))
		}
	case reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		p.n++
		v.SetUint(uint64(100 + p.n))
	case reflect.Float32, reflect.Float64:
		p.n++
		v.SetFloat(float64(p.n) + 0.5)
	case reflect.Pointer:
		e := reflect.New(v.Type().Elem())
		p.puni(e.Elem(), ime, dubina+1)
		v.Set(e)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if f := v.Type().Field(i); f.IsExported() {
				p.puni(v.Field(i), f.Name, dubina+1)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			v.SetBytes([]byte("x-" + ime))
			return
		}
		n := 2
		el := v.Type().Elem()
		for el.Kind() == reflect.Pointer {
			el = el.Elem()
		}
		if el.Kind() == reflect.Struct && el != tipVremena {
			n = 1
		}
		s := reflect.MakeSlice(v.Type(), n, n)
		for i := 0; i < n; i++ {
			p.puni(s.Index(i), fmt.Sprintf("%s%d", ime, i+1), dubina+1)
		}
		v.Set(s)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			p.puni(v.Index(i), fmt.Sprintf("%s%d", ime, i+1), dubina+1)
		}
	case reflect.Map:
		m := reflect.MakeMapWithSize(v.Type(), 1)
		k := reflect.New(v.Type().Key()).Elem()
		p.puni(k, ime+"Kljuc", dubina+1)
		e := reflect.New(v.Type().Elem()).Elem()
		p.puni(e, ime, dubina+1)
		m.SetMapIndex(k, e)
		v.Set(m)
	}
}

// teret čita sadržaj verzije u zadani tip, za lokalnu izmjenu na B
func teret[T any](t *testing.T, payload []byte) *T {
	t.Helper()
	var x T
	if err := json.Unmarshal(payload, &x); err != nil {
		t.Fatalf("sadržaj verzije: %v", err)
	}
	return &x
}

func nuzno(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

var pdfProbe = []byte("%PDF-1.4\n% probni izvornik\n%%EOF\n")

// ---- slučajevi ----

// slucajRazmjene opisuje jedan entitet primjene razmjene
type slucajRazmjene struct {
	entitet string
	tablica string // tablica na površini
	kljuc   string // stupac identiteta zapisa
	broj    bool   // identitet je cijeli broj (INTEGER PRIMARY KEY)

	// napravi upisuje zapis na čvoru A kroz repozitorij, s punilom
	napravi func(t *testing.T, a *cvor)
	// uredi mijenja zapis na čvoru B kroz repozitorij; payload je zadnja
	// verzija na B. Nil kad entitet nema put izmjene, uz razlog.
	uredi          func(t *testing.T, b *cvor, id string, payload []byte)
	bezUredjivanja string
	// bezArhiviranja je razlog zbog kojeg arhivirana verzija namjerno ne
	// miče zapis s površine; prazno znači da ga mora maknuti
	bezArhiviranja string

	// procitaj čita zapis na čvoru stvarnim čitačem repozitorija, onim koji
	// koristi program; payload je zadnja verzija na tom čvoru. Vraća
	// pročitano, koje ne smije biti prazno.
	procitaj func(t *testing.T, n *cvor, id string, payload []byte) (any, error)

	// preskoci su stupci koji se namjerno ne uspoređuju, sa zašto
	preskoci map[string]string
}

// stupci koje primatelj puni vremenom verzije (v.CreatedAt), a izvor
// vremenom spremanja: razlika je u mikrosekundama i nije greška
const vrijemeVerzije = "primatelj upisuje vrijeme verzije (v.CreatedAt), izvor vrijeme spremanja"

func slucajeviRazmjene() []slucajRazmjene {
	letva := osnovaLetva.String()
	korisnik := osnovaKorisnik.String()
	return []slucajRazmjene{
		{
			entitet: EntityEpisodes, tablica: "defense_episodes", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var e models.DefenseEpisode
				puni(&e)
				e.SectionCode, e.StationID, e.Phase = osnovaDionica, letva, models.PhaseRegular
				nuzno(t, NewEpisodeRepository(a.db, a.rec).SaveEpisode(ctxRaz, &e))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				e := teret[models.DefenseEpisode](t, p)
				e.Note += " (izmjena)"
				nuzno(t, NewEpisodeRepository(b.db, b.rec).SaveEpisode(ctxRaz, e))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewEpisodeRepository(n.db, n.rec).ListEpisodes(ctxRaz, osnovaDionica)
			},
		},
		{
			entitet: EntityIspravci, tablica: "arhiva_ispravci", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var i models.ArhivaIspravak
				puni(&i)
				i.Korak = "satni"
				_, err := NewIspravakRepository(a.db, a.rec).Spremi(ctxRaz, []models.ArhivaIspravak{i})
				nuzno(t, err)
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				i := teret[models.ArhivaIspravak](t, p)
				i.Razlog += " (izmjena)"
				_, err := NewIspravakRepository(b.db, b.rec).Spremi(ctxRaz, []models.ArhivaIspravak{*i})
				nuzno(t, err)
			},
			procitaj: func(t *testing.T, n *cvor, _ string, p []byte) (any, error) {
				x := teret[models.ArhivaIspravak](t, p)
				return NewIspravakRepository(n.db, n.rec).ZaNiz(ctxRaz, x.Letva, x.Velicina, x.Korak, x.Vrijeme.Add(-time.Minute), x.Vrijeme.Add(time.Minute))
			},
		},
		{
			entitet: EntityBiljeske, tablica: "arhiva_biljeske", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var b models.ArhivaBiljeska
				puni(&b)
				b.Korak = "dnevni"
				_, err := NewBiljeskaRepository(a.db, a.rec).Spremi(ctxRaz, []models.ArhivaBiljeska{b})
				nuzno(t, err)
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.ArhivaBiljeska](t, p)
				x.Tekst += " (izmjena)"
				_, err := NewBiljeskaRepository(b.db, b.rec).Spremi(ctxRaz, []models.ArhivaBiljeska{*x})
				nuzno(t, err)
			},
			procitaj: func(t *testing.T, n *cvor, _ string, p []byte) (any, error) {
				x := teret[models.ArhivaBiljeska](t, p)
				return NewBiljeskaRepository(n.db, n.rec).ZaNiz(ctxRaz, x.Letva, x.Velicina, x.Korak, x.Vrijeme.Add(-time.Minute), x.Vrijeme.Add(time.Minute))
			},
		},
		{
			entitet: EntityStations, tablica: "stations", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var st models.Station
				puni(&st)
				st.SectionCodes = nil
				nuzno(t, NewStationRepository(a.db, a.rec).CreateStation(ctxRaz, &st))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				st := teret[models.Station](t, p)
				st.Notes += " (izmjena)"
				nuzno(t, NewStationRepository(b.db, b.rec).UpdateStation(ctxRaz, st))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewStationRepository(n.db, n.rec).GetStationByID(ctxRaz, uuid.MustParse(id))
			},
		},
		{
			entitet: EntitySections, tablica: "sections", kljuc: "code",
			napravi: func(t *testing.T, a *cvor) {
				var s models.Section
				puni(&s)
				// poddionica bez veza na registre: povezivanje objekata i voda po
				// nazivu ima svoje testove, ovdje se gleda samo prijenos dionice
				km1, km2 := 10.5, 12.25
				s.Code, s.AreaID, s.SectorID = "B.16.2", 16, "B"
				s.Parts = []models.SectionPart{{Seq: 1, StationingKind: "rkm", KmFrom: &km1, KmTo: &km2, Bank: "L",
					Extent: "x-Extent", Description: "x-Description", ProtectedText: "x-ProtectedText"}}
				s.CreatedAt, s.UpdatedAt = "", ""
				s.WatercourseCode, s.WatercourseName, s.AreaName, s.SectorName, s.Personnel = "", "", "", "", nil
				nuzno(t, NewSectionRepository(a.db, a.rec).SaveSection(ctxRaz, &s))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				s := teret[models.Section](t, p)
				s.Notes += " (izmjena)"
				nuzno(t, NewSectionRepository(b.db, b.rec).SaveSection(ctxRaz, s))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewSectionRepository(n.db, n.rec).GetSectionByCode(id)
			},
		},
		{
			entitet: EntityWatercourses, tablica: "watercourses", kljuc: "code",
			napravi: func(t *testing.T, a *cvor) {
				var w models.Watercourse
				puni(&w)
				nuzno(t, NewWatercourseRepository(a.db, a.rec).CreateWatercourse(ctxRaz, &w))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				w := teret[models.Watercourse](t, p)
				w.Notes += " (izmjena)"
				nuzno(t, NewWatercourseRepository(b.db, b.rec).UpdateWatercourse(ctxRaz, w))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewWatercourseRepository(n.db, n.rec).GetWatercourse(ctxRaz, id)
			},
		},
		{
			entitet: EntityKisomjeri, tablica: "kisomjeri", kljuc: "code",
			napravi: func(t *testing.T, a *cvor) {
				var k models.Kisomjer
				puni(&k)
				nuzno(t, NewKisomjerRepository(a.db, a.rec).CreateKisomjer(ctxRaz, &k))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				k := teret[models.Kisomjer](t, p)
				k.Napomena += " (izmjena)"
				nuzno(t, NewKisomjerRepository(b.db, b.rec).UpdateKisomjer(ctxRaz, k))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewKisomjerRepository(n.db, n.rec).GetKisomjer(ctxRaz, id)
			},
		},
		{
			entitet: EntitySlivovi, tablica: "slivovi", kljuc: "oznaka",
			napravi: func(t *testing.T, a *cvor) {
				var m models.Sliv
				puni(&m)
				nuzno(t, NewKisomjerRepository(a.db, a.rec).UpsertSliv(ctxRaz, &m))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				m := teret[models.Sliv](t, p)
				m.Napomena += " (izmjena)"
				nuzno(t, NewKisomjerRepository(b.db, b.rec).UpsertSliv(ctxRaz, m))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewKisomjerRepository(n.db, n.rec).ListSlivovi(ctxRaz)
			},
		},
		{
			entitet: EntityMaintainedWaters, tablica: "maintained_waters", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var m models.MaintainedWater
				puni(&m)
				m.AreaID = 16
				nuzno(t, NewMaintenanceRepository(a.db, a.rec).UpsertWater(ctxRaz, &m))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				m := teret[models.MaintainedWater](t, p)
				m.Source += " (izmjena)"
				nuzno(t, NewMaintenanceRepository(b.db, b.rec).UpsertWater(ctxRaz, m))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewMaintenanceRepository(n.db, n.rec).GetWater(ctxRaz, id)
			},
		},
		{
			entitet: EntityWorkItems, tablica: "work_items", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var w models.WorkItem
				puni(&w)
				w.AreaID = 16
				nuzno(t, NewMaintenanceRepository(a.db, a.rec).SaveItem(ctxRaz, &w))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				w := teret[models.WorkItem](t, p)
				w.Description += " (izmjena)"
				nuzno(t, NewMaintenanceRepository(b.db, b.rec).SaveItem(ctxRaz, w))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewMaintenanceRepository(n.db, n.rec).GetItem(ctxRaz, id)
			},
		},
		{
			entitet: EntityJournals, tablica: "journals", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var j models.Journal
				puni(&j)
				podrucje := 16
				j.AreaID, j.CentarSektor, j.CentarPodrucje = 16, "B", &podrucje
				nuzno(t, NewJournalRepository(a.db, a.rec).SaveJournal(ctxRaz, &j))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				j := teret[models.Journal](t, p)
				j.Notes += " (izmjena)"
				nuzno(t, NewJournalRepository(b.db, b.rec).SaveJournal(ctxRaz, j))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewJournalRepository(n.db, n.rec).GetJournal(ctxRaz, id)
			},
		},
		{
			entitet: EntityJournalSheets, tablica: "journal_sheets", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var s models.JournalSheet
				puni(&s)
				s.JournalID = osnovaDnevnik
				nuzno(t, NewJournalRepository(a.db, a.rec).SaveSheet(ctxRaz, &s))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				s := teret[models.JournalSheet](t, p)
				s.Conditions += " (izmjena)"
				nuzno(t, NewJournalRepository(b.db, b.rec).SaveSheet(ctxRaz, s))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewJournalRepository(n.db, n.rec).GetSheet(ctxRaz, id)
			},
		},
		{
			entitet: EntityJournalEntries, tablica: "journal_entries", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var e models.JournalEntry
				puni(&e)
				podrucje := 16
				e.JournalID, e.SectionCode, e.Podrucje = osnovaDnevnik, osnovaDionica, &podrucje
				nuzno(t, NewJournalRepository(a.db, a.rec).SaveEntry(ctxRaz, &e))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				e := teret[models.JournalEntry](t, p)
				e.Text += " (izmjena)"
				nuzno(t, NewJournalRepository(b.db, b.rec).SaveEntry(ctxRaz, e))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewJournalRepository(n.db, n.rec).GetEntry(ctxRaz, id)
			},
		},
		{
			entitet: EntityDezurstva, tablica: "dezurstva", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var d models.Dezurstvo
				puni(&d)
				podrucje := 16
				d.JournalID, d.UserID, d.Podrucje = osnovaDnevnik, korisnik, &podrucje
				nuzno(t, NewJournalRepository(a.db, a.rec).SaveDezurstvo(ctxRaz, &d))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				d := teret[models.Dezurstvo](t, p)
				d.Napomena += " (izmjena)"
				nuzno(t, NewJournalRepository(b.db, b.rec).SaveDezurstvo(ctxRaz, d))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewJournalRepository(n.db, n.rec).GetDezurstvo(ctxRaz, id)
			},
		},
		{
			entitet: EntityDnevnaIzvjesca, tablica: "dnevna_izvjesca", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var i models.DnevnoIzvjesce
				puni(&i)
				i.JournalID, i.SectionCode, i.Stadij = osnovaDnevnik, osnovaDionica, models.PhasePrep
				nuzno(t, NewIzvjescaRepository(a.db, a.rec).Save(ctxRaz, &i))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				i := teret[models.DnevnoIzvjesce](t, p)
				i.Izradio += " (izmjena)"
				nuzno(t, NewIzvjescaRepository(b.db, b.rec).Save(ctxRaz, i))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewIzvjescaRepository(n.db, n.rec).Get(ctxRaz, id)
			},
		},
		{
			entitet: EntitySektorskaIzvjesca, tablica: "sektorska_izvjesca", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var i models.SektorskoIzvjesce
				puni(&i)
				i.JournalID, i.Sektor = osnovaDnevnik, "B"
				nuzno(t, NewSektorskaIzvjescaRepository(a.db, a.rec).Save(ctxRaz, &i))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				i := teret[models.SektorskoIzvjesce](t, p)
				i.Izradio += " (izmjena)"
				nuzno(t, NewSektorskaIzvjescaRepository(b.db, b.rec).Save(ctxRaz, i))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewSektorskaIzvjescaRepository(n.db, n.rec).Get(ctxRaz, id)
			},
		},
		{
			entitet: EntityBlagdani, tablica: "blagdani", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var p obracun.Pravilo
				puni(&p)
				p.Vrsta = obracun.Stalni
				nuzno(t, NewObracunRepository(a.db, a.rec).SaveBlagdan(ctxRaz, p))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[obracun.Pravilo](t, p)
				x.Naziv += " (izmjena)"
				nuzno(t, NewObracunRepository(b.db, b.rec).SaveBlagdan(ctxRaz, *x))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewObracunRepository(n.db, n.rec).Blagdani(ctxRaz)
			},
			preskoci: map[string]string{"updated_at": vrijemeVerzije},
		},
		{
			entitet: EntityKoeficijenti, tablica: "koeficijenti", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var k Koeficijent
				puni(&k)
				nuzno(t, NewObracunRepository(a.db, a.rec).SaveKoeficijent(ctxRaz, k))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				k := teret[Koeficijent](t, p)
				k.K += 0.25
				nuzno(t, NewObracunRepository(b.db, b.rec).SaveKoeficijent(ctxRaz, *k))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewObracunRepository(n.db, n.rec).Koeficijenti(ctxRaz)
			},
			preskoci: map[string]string{"updated_at": vrijemeVerzije},
		},
		{
			entitet: EntityObracunPostavke, tablica: "obracun_postavke", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				// jedina postavka obračuna je radno vrijeme; repozitorij ga
				// prima kao obracun.RadnoVrijeme, a u knjigu piše Postavka
				rv, err := obracun.ParseRadnoVrijeme("07:00", "15:00")
				nuzno(t, err)
				nuzno(t, NewObracunRepository(a.db, a.rec).SaveRadnoVrijeme(ctxRaz, rv))
			},
			uredi: func(t *testing.T, b *cvor, _ string, _ []byte) {
				rv, err := obracun.ParseRadnoVrijeme("07:30", "15:30")
				nuzno(t, err)
				nuzno(t, NewObracunRepository(b.db, b.rec).SaveRadnoVrijeme(ctxRaz, rv))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewObracunRepository(n.db, n.rec).RadnoVrijeme(ctxRaz)
			},
			preskoci: map[string]string{"updated_at": vrijemeVerzije},
		},
		{
			entitet: EntityMtsVrste, tablica: "mts_vrste", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var v models.VrstaSredstva
				puni(&v)
				nuzno(t, NewMtsRepository(a.db, a.rec).SaveVrsta(ctxRaz, &v))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				v := teret[models.VrstaSredstva](t, p)
				v.Napomena += " (izmjena)"
				nuzno(t, NewMtsRepository(b.db, b.rec).SaveVrsta(ctxRaz, v))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewMtsRepository(n.db, n.rec).GetVrsta(ctxRaz, id)
			},
		},
		{
			entitet: EntityMtsSkladista, tablica: "mts_skladista", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var s models.Skladiste
				puni(&s)
				s.Sektor, s.AreaID = "B", 16
				nuzno(t, NewMtsRepository(a.db, a.rec).SaveSkladiste(ctxRaz, &s))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				s := teret[models.Skladiste](t, p)
				s.Napomena += " (izmjena)"
				nuzno(t, NewMtsRepository(b.db, b.rec).SaveSkladiste(ctxRaz, s))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewMtsRepository(n.db, n.rec).GetSkladiste(ctxRaz, id)
			},
		},
		{
			entitet: EntityMtsPromet, tablica: "mts_promet", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var x models.Promet
				puni(&x)
				x.Sektor, x.AreaID, x.SectionCode, x.JournalID = "B", 16, osnovaDionica, osnovaDnevnik
				nuzno(t, NewMtsRepository(a.db, a.rec).SavePromet(ctxRaz, []models.Promet{x}))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.Promet](t, p)
				x.Napomena += " (izmjena)"
				nuzno(t, NewMtsRepository(b.db, b.rec).SavePromet(ctxRaz, []models.Promet{*x}))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewMtsRepository(n.db, n.rec).ListPromet(ctxRaz, FiltarPrometa{JournalID: osnovaDnevnik})
			},
		},
		{
			entitet: EntityMtsPopisi, tablica: "mts_popisi", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var x models.Popis
				puni(&x)
				x.Sektor = "B"
				nuzno(t, NewMtsRepository(a.db, a.rec).SavePopis(ctxRaz, &x))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.Popis](t, p)
				x.Napomena += " (izmjena)"
				nuzno(t, NewMtsRepository(b.db, b.rec).SavePopis(ctxRaz, x))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewMtsRepository(n.db, n.rec).GetPopis(ctxRaz, id)
			},
		},
		{
			entitet: EntityMtsPotrebe, tablica: "mts_potrebe", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var x models.Potreba
				puni(&x)
				nuzno(t, NewMtsRepository(a.db, a.rec).SavePotrebe(ctxRaz, []models.Potreba{x}))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.Potreba](t, p)
				x.Napomena += " (izmjena)"
				nuzno(t, NewMtsRepository(b.db, b.rec).SavePotrebe(ctxRaz, []models.Potreba{*x}))
			},
			procitaj: func(t *testing.T, n *cvor, _ string, p []byte) (any, error) {
				x := teret[models.Potreba](t, p)
				return NewMtsRepository(n.db, n.rec).Potrebe(ctxRaz, "", x.SkladisteID, x.Godina)
			},
		},
		{
			entitet: EntityAkti, tablica: "akti", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var x models.Akt
				puni(&x)
				x.Sektor, x.AreaID, x.StationID, x.Stupanj, x.Status = "B", 16, letva, models.PhaseRegular, "NACRT"
				nuzno(t, NewAktiRepository(a.db, a.rec).SaveAkt(ctxRaz, &x))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.Akt](t, p)
				x.Napomena += " (izmjena)"
				nuzno(t, NewAktiRepository(b.db, b.rec).SaveAkt(ctxRaz, x))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewAktiRepository(n.db, n.rec).GetAkt(ctxRaz, id)
			},
		},
		{
			entitet: EntityZadaci, tablica: "vodocuvarski_zadaci", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var z models.Zadatak
				puni(&z)
				z.UserID, z.ZadaoID, z.Sektor, z.AreaID, z.Status = korisnik, korisnik, "B", 16, models.ZadatakOtvoren
				nuzno(t, NewVodocuvarRepository(a.db, a.rec).SaveZadatak(ctxRaz, &z))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				z := teret[models.Zadatak](t, p)
				z.Tekst += " (izmjena)"
				nuzno(t, NewVodocuvarRepository(b.db, b.rec).SaveZadatak(ctxRaz, z))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewVodocuvarRepository(n.db, n.rec).GetZadatak(ctxRaz, id)
			},
		},
		{
			entitet: EntityVodocuvarski, tablica: "vodocuvarski_listovi", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var l models.VodocuvarskiList
				puni(&l)
				l.UserID, l.Sektor, l.AreaID = korisnik, "B", 16
				nuzno(t, NewVodocuvarRepository(a.db, a.rec).Save(ctxRaz, &l))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				l := teret[models.VodocuvarskiList](t, p)
				l.Opis += " (izmjena)"
				nuzno(t, NewVodocuvarRepository(b.db, b.rec).Save(ctxRaz, l))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewVodocuvarRepository(n.db, n.rec).Get(ctxRaz, id)
			},
		},
		{
			entitet: EntityZigovi, tablica: "zigovi", kljuc: "sektor",
			napravi: func(t *testing.T, a *cvor) {
				var z models.Zig
				puni(&z)
				z.Sektor, z.Mime = "B", "image/png"
				nuzno(t, NewAktiRepository(a.db, a.rec).SaveZig(ctxRaz, &z))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				z := teret[models.Zig](t, p)
				z.Uredio += " (izmjena)"
				nuzno(t, NewAktiRepository(b.db, b.rec).SaveZig(ctxRaz, z))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewAktiRepository(n.db, n.rec).GetZig(ctxRaz, id)
			},
			preskoci: map[string]string{"updated_at": vrijemeVerzije},
		},
		{
			entitet: EntityPrijave, tablica: "prijave", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var x models.PrijavaSTerena
				puni(&x)
				x.UserID, x.Sektor, x.AreaID, x.DionicaCode = korisnik, "B", 16, osnovaDionica
				x.Vrsta, x.Status = models.PrijavaPrijava, models.PrijavaNacrt
				nuzno(t, NewPrijavaRepository(a.db, a.rec).Save(ctxRaz, &x))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.PrijavaSTerena](t, p)
				x.Opis += " (izmjena)"
				nuzno(t, NewPrijavaRepository(b.db, b.rec).Save(ctxRaz, x))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewPrijavaRepository(n.db, n.rec).Get(ctxRaz, id)
			},
		},
		{
			entitet: EntityPrijaveIzvornici, tablica: "prijave_izvornici", kljuc: "prijava_id",
			napravi: func(t *testing.T, a *cvor) {
				// izvornik visi o prijavi: ona stiže istom razmjenom
				x := models.PrijavaSTerena{UserID: korisnik, Sektor: "B", AreaID: 16, Godina: 2026,
					Datum: trenutakPunila(), Vrsta: models.PrijavaPrijava, Status: models.PrijavaNacrt, Naslov: "x-Naslov"}
				r := NewPrijavaRepository(a.db, a.rec)
				nuzno(t, r.Save(ctxRaz, &x))
				nuzno(t, r.SpremiIzvornik(ctxRaz, x.ID, pdfProbe, "x-Sazetak"))
			},
			uredi: func(t *testing.T, b *cvor, id string, p []byte) {
				iz := teret[models.IzvornikLista](t, p)
				nuzno(t, NewPrijavaRepository(b.db, b.rec).SpremiIzvornik(ctxRaz, id, append(pdfProbe, '\n'), iz.Sazetak+" (izmjena)"))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewPrijavaRepository(n.db, n.rec).Izvornik(ctxRaz, id)
			},
		},
		{
			entitet: EntityPotpisniKljucevi, tablica: "potpisni_kljucevi", kljuc: "user_id",
			napravi: func(t *testing.T, a *cvor) {
				var k models.PotpisniKljuc
				puni(&k)
				k.UserID = korisnik
				nuzno(t, NewPotpisRepository(a.db, a.rec).SaveKljuc(ctxRaz, &k))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				k := teret[models.PotpisniKljuc](t, p)
				k.Ime += " (izmjena)"
				nuzno(t, NewPotpisRepository(b.db, b.rec).SaveKljuc(ctxRaz, k))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewPotpisRepository(n.db, n.rec).GetKljuc(ctxRaz, id)
			},
		},
		{
			entitet: EntityPotpisniIzdavatelji, tablica: "potpisni_izdavatelji", kljuc: "cvor",
			napravi: func(t *testing.T, a *cvor) {
				var i models.IzdavateljPotpisa
				puni(&i)
				nuzno(t, NewPotpisRepository(a.db, a.rec).SaveIzdavatelj(ctxRaz, &i))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				i := teret[models.IzdavateljPotpisa](t, p)
				i.Cert = append(i.Cert, '!')
				nuzno(t, NewPotpisRepository(b.db, b.rec).SaveIzdavatelj(ctxRaz, i))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewPotpisRepository(n.db, n.rec).GetIzdavatelj(ctxRaz, id)
			},
		},
		{
			entitet: EntityVodocuvarskiIzvornici, tablica: "vodocuvarski_izvornici", kljuc: "list_id",
			napravi: func(t *testing.T, a *cvor) {
				l := models.VodocuvarskiList{UserID: korisnik, Sektor: "B", AreaID: 16, Datum: trenutakPunila()}
				r := NewVodocuvarRepository(a.db, a.rec)
				nuzno(t, r.Save(ctxRaz, &l))
				nuzno(t, r.SaveIzvornik(ctxRaz, &models.IzvornikLista{ListID: l.ID, PDF: pdfProbe, Sazetak: "x-Sazetak"}))
			},
			uredi: func(t *testing.T, b *cvor, id string, p []byte) {
				iz := teret[models.IzvornikLista](t, p)
				iz.PDF, iz.Sazetak = append(pdfProbe, '\n'), iz.Sazetak+" (izmjena)"
				nuzno(t, NewVodocuvarRepository(b.db, b.rec).SaveIzvornik(ctxRaz, iz))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewVodocuvarRepository(n.db, n.rec).GetIzvornik(ctxRaz, id)
			},
		},
		{
			entitet: EntityJournalIzvornici, tablica: "journal_izvornici", kljuc: "journal_id",
			napravi: func(t *testing.T, a *cvor) {
				// izvornik nastaje ovjerom dnevnika COP-a; dnevnik stiže istom razmjenom
				podrucje := 16
				j := models.Journal{AreaID: 16, CentarSektor: "B", CentarPodrucje: &podrucje, Kind: "COP", Year: 2026}
				r := NewJournalRepository(a.db, a.rec)
				nuzno(t, r.SaveJournal(ctxRaz, &j))
				kad := trenutakPunila()
				j.ZakljucioID, j.Zakljucio, j.ZakljucenoAt = korisnik, "Proba Probić", &kad
				nuzno(t, r.SaveOvjeraCOP(ctxRaz, &j, pdfProbe))
			},
			bezUredjivanja: "izvornik dnevnika nastaje samo ovjerom (SaveOvjeraCOP), a ovjeren dnevnik se ne ovjerava ponovno",
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewJournalRepository(n.db, n.rec).IzvornikDnevnika(ctxRaz, id)
			},
		},
		{
			entitet: EntityPotpisi, tablica: "posta_potpisi", kljuc: "user_id",
			napravi: func(t *testing.T, a *cvor) {
				var p PotpisPoste
				puni(&p)
				p.UserID = korisnik
				nuzno(t, NewAktiRepository(a.db, a.rec).SavePotpis(ctxRaz, &p))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[PotpisPoste](t, p)
				x.HTML += " (izmjena)"
				nuzno(t, NewAktiRepository(b.db, b.rec).SavePotpis(ctxRaz, x))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewAktiRepository(n.db, n.rec).GetPotpis(ctxRaz, id)
			},
			preskoci: map[string]string{"updated_at": vrijemeVerzije},
		},
		{
			entitet: EntityPostavke, tablica: "postavke", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				nuzno(t, NewAktiRepository(a.db, a.rec).SavePostavka(ctxRaz, PostavkaPosta, `{"posluzitelj":"x"}`))
			},
			uredi: func(t *testing.T, b *cvor, id string, p []byte) {
				x := teret[Postavka](t, p)
				nuzno(t, NewAktiRepository(b.db, b.rec).SavePostavka(ctxRaz, id, x.Vrijednost+" "))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewAktiRepository(n.db, n.rec).GetPostavka(ctxRaz, id)
			},
			preskoci: map[string]string{"updated_at": vrijemeVerzije},
		},
		{
			entitet: EntitySlanja, tablica: "akti_slanja", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var x models.SlanjeAkta
				puni(&x)
				nuzno(t, NewAktiRepository(a.db, a.rec).SaveSlanja(ctxRaz, []models.SlanjeAkta{x}))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.SlanjeAkta](t, p)
				x.Greska += " (izmjena)"
				nuzno(t, NewAktiRepository(b.db, b.rec).SaveSlanja(ctxRaz, []models.SlanjeAkta{*x}))
			},
			procitaj: func(t *testing.T, n *cvor, _ string, p []byte) (any, error) {
				x := teret[models.SlanjeAkta](t, p)
				return NewAktiRepository(n.db, n.rec).ListSlanja(ctxRaz, x.AktID)
			},
		},
		{
			entitet: EntityIzvornici, tablica: "akti_izvornici", kljuc: "akt_id",
			napravi: func(t *testing.T, a *cvor) {
				nuzno(t, NewAktiRepository(a.db, a.rec).SaveIzvornik(ctxRaz, &Izvornik{AktID: "x-AktID", PDF: pdfProbe, Sazetak: "x-Sazetak"}))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				iz := teret[Izvornik](t, p)
				iz.PDF, iz.Sazetak = append(pdfProbe, '\n'), iz.Sazetak+" (izmjena)"
				nuzno(t, NewAktiRepository(b.db, b.rec).SaveIzvornik(ctxRaz, iz))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewAktiRepository(n.db, n.rec).GetIzvornik(ctxRaz, id)
			},
		},
		{
			entitet: EntitySluzbe, tablica: "sluzbe", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var s models.Sluzba
				puni(&s)
				s.CountyID, s.MunicipalityID = osnovaZupanija, osnovaOpcina
				nuzno(t, NewTerritoryRepository(a.db, a.rec).SaveSluzba(ctxRaz, &s))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				s := teret[models.Sluzba](t, p)
				s.Napomena += " (izmjena)"
				nuzno(t, NewTerritoryRepository(b.db, b.rec).SaveSluzba(ctxRaz, s))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewTerritoryRepository(n.db, n.rec).GetSluzba(ctxRaz, id)
			},
		},
		{
			entitet: EntitySprance, tablica: "akti_sprance", kljuc: "sektor",
			napravi: func(t *testing.T, a *cvor) {
				var sp models.Spranca
				puni(&sp)
				sp.Sektor = "B"
				sp.Clanci = map[models.DefensePhase]string{models.PhaseRegular: "x-Clanak"}
				nuzno(t, NewAktiRepository(a.db, a.rec).SaveSpranca(ctxRaz, &sp))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				sp := teret[models.Spranca](t, p)
				sp.Zavrsno += " (izmjena)"
				nuzno(t, NewAktiRepository(b.db, b.rec).SaveSpranca(ctxRaz, sp))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewAktiRepository(n.db, n.rec).GetSpranca(ctxRaz, id)
			},
		},
		{
			entitet: EntityPrimatelji, tablica: "primatelji", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var x models.Primatelj
				puni(&x)
				x.Sektor, x.AreaID, x.OdStupnja = "B", 16, models.PhasePrep
				nuzno(t, NewAktiRepository(a.db, a.rec).SavePrimatelj(ctxRaz, &x))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.Primatelj](t, p)
				x.Naziv += " (izmjena)"
				nuzno(t, NewAktiRepository(b.db, b.rec).SavePrimatelj(ctxRaz, x))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewAktiRepository(n.db, n.rec).GetPrimatelj(ctxRaz, id)
			},
		},
		{
			entitet: EntityRoleModules, tablica: "role_modules", kljuc: "role",
			napravi: func(t *testing.T, a *cvor) {
				nuzno(t, NewModuleRepository(a.db, a.rec).SetRoleRule(ctxRaz, string(models.RoleAreaLeader),
					[]string{models.ModuleReadings, models.ModuleJournals}))
			},
			uredi: func(t *testing.T, b *cvor, id string, _ []byte) {
				nuzno(t, NewModuleRepository(b.db, b.rec).SetRoleRule(ctxRaz, id, []string{models.ModuleReadings}))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewModuleRepository(n.db, n.rec).RoleRules(ctxRaz)
			},
		},
		{
			entitet: EntityUserModules, tablica: "user_modules", kljuc: "user_id",
			napravi: func(t *testing.T, a *cvor) {
				nuzno(t, NewModuleRepository(a.db, a.rec).SetUserOverride(ctxRaz, korisnik,
					[]string{models.ModuleReadings}, []string{models.ModuleJournals}))
			},
			uredi: func(t *testing.T, b *cvor, id string, _ []byte) {
				nuzno(t, NewModuleRepository(b.db, b.rec).SetUserOverride(ctxRaz, id,
					[]string{models.ModuleReadings, models.ModuleRegisters}, []string{models.ModuleJournals}))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewModuleRepository(n.db, n.rec).UserOverride(ctxRaz, id)
			},
		},
		{
			entitet: EntityReadings, tablica: "readings", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var rd models.Reading
				puni(&rd)
				// unutar razdoblja koje čvor drži, pa ga ograda povijesti ne odbaci
				rd.StationID, rd.StructureID = letva, ""
				rd.MeasuredAt = time.Now().In(zonaPunila).Truncate(time.Second).Add(-time.Hour)
				rd.UserID = korisnik
				nuzno(t, NewReadingRepository(a.db, a.rec).Create(ctxRaz, &rd))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				rd := teret[models.Reading](t, p)
				rd.Note += " (izmjena)"
				nuzno(t, NewReadingRepository(b.db, b.rec).Update(ctxRaz, rd))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewReadingRepository(n.db, n.rec).Get(ctxRaz, uuid.MustParse(id))
			},
		},
		{
			entitet: EntityStructures, tablica: "structures", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var s models.Structure
				puni(&s)
				s.SectorID, s.AreaID, s.StationID, s.StationDownID = "B", 16, letva, letva
				s.SectionCodes, s.StationName, s.StationDownName, s.AreaName = nil, "", "", ""
				nuzno(t, NewStructureRepository(a.db, a.rec).CreateStructure(ctxRaz, &s))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				s := teret[models.Structure](t, p)
				s.Notes += " (izmjena)"
				nuzno(t, NewStructureRepository(b.db, b.rec).UpdateStructure(ctxRaz, s))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewStructureRepository(n.db, n.rec).GetStructure(ctxRaz, uuid.MustParse(id))
			},
		},
		{
			entitet: EntityUsers, tablica: "users", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var u models.User
				puni(&u)
				// novi korisnik se još nije prijavio, a zaduženja putuju zasebno
				u.OrgType, u.LastLoginAt, u.Duties = models.OrgHrvatskeVode, nil, nil
				nuzno(t, NewUserRepository(a.db, a.rec).CreateUser(&u, nil))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				uv := teret[userVersion](t, p)
				u := uv.User
				u.FullName += " (izmjena)"
				nuzno(t, NewUserRepository(b.db, b.rec).UpdateUser(&u))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewUserRepository(n.db, n.rec).GetUserByID(uuid.MustParse(id))
			},
		},
		{
			entitet: EntityDuties, tablica: "duties", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var d models.Duty
				puni(&d)
				sektor, podrucje, dodijelio := "B", 16, osnovaKorisnik
				d.UserID, d.SectorID, d.AreaID, d.AssignedBy = osnovaKorisnik, &sektor, &podrucje, &dodijelio
				d.Role, d.ScopeType, d.SectionCodes = models.RoleAreaLeader, models.ScopeArea, osnovaDionica
				nuzno(t, NewUserRepository(a.db, a.rec).AddDuty(&d))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				d := teret[models.Duty](t, p)
				d.Reason += " (izmjena)"
				nuzno(t, NewUserRepository(b.db, b.rec).UpdateDuty(d))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewUserRepository(n.db, n.rec).GetDuty(uuid.MustParse(id))
			},
		},
		{
			entitet: EntitySectors, tablica: "sectors", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var s models.Sector
				puni(&s)
				s.ID, s.Level = "C", 2
				nuzno(t, NewOrgRepository(a.db, a.rec).SaveSector(ctxRaz, &s))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				s := teret[models.Sector](t, p)
				s.Address += " (izmjena)"
				nuzno(t, NewOrgRepository(b.db, b.rec).SaveSector(ctxRaz, s))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewOrgRepository(n.db, n.rec).GetSector(ctxRaz, id)
			},
		},
		{
			entitet: EntityOrgTerms, tablica: "org_terms", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var o models.OrgTerms
				puni(&o)
				o.ID, o.LogoMime = models.TermsID, "image/png"
				nuzno(t, NewOrgRepository(a.db, a.rec).SaveTerms(ctxRaz, o))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				o := teret[models.OrgTerms](t, p)
				o.LoginInfo += " (izmjena)"
				nuzno(t, NewOrgRepository(b.db, b.rec).SaveTerms(ctxRaz, *o))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewOrgRepository(n.db, n.rec).GetTerms(ctxRaz)
			},
		},
		{
			entitet: EntityAreas, tablica: "areas", kljuc: "id", broj: true,
			napravi: func(t *testing.T, a *cvor) {
				var x models.Area
				puni(&x)
				x.ID, x.SectorID = 17, "B"
				nuzno(t, NewOrgRepository(a.db, a.rec).SaveArea(ctxRaz, &x))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[models.Area](t, p)
				x.Subcenter += " (izmjena)"
				nuzno(t, NewOrgRepository(b.db, b.rec).SaveArea(ctxRaz, x))
			},
			procitaj: func(t *testing.T, n *cvor, id string, _ []byte) (any, error) {
				broj, err := strconv.Atoi(id)
				nuzno(t, err)
				return NewOrgRepository(n.db, n.rec).GetArea(ctxRaz, broj)
			},
		},
		{
			entitet: EntityContractors, tablica: "contractors", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var c models.Contractor
				puni(&c)
				c.Assignments = nil
				nuzno(t, NewOrgRepository(a.db, a.rec).SaveContractor(ctxRaz, &c, nil))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				c := teret[models.Contractor](t, p)
				c.Notes += " (izmjena)"
				nuzno(t, NewOrgRepository(b.db, b.rec).SaveContractor(ctxRaz, c, nil))
			},
			procitaj: func(_ *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return NewOrgRepository(n.db, n.rec).GetContractor(ctxRaz, id)
			},
		},
		{
			entitet: EntityContractorAssignments, tablica: "contractor_assignments", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				c := models.Contractor{Name: "x-Name", Active: true}
				var w models.ContractorAssignment
				puni(&w)
				w.SectorID, w.AreaID = "B", 16
				nuzno(t, NewOrgRepository(a.db, a.rec).SaveContractor(ctxRaz, &c, []models.ContractorAssignment{w}))
			},
			bezUredjivanja: "veza firme se ne uređuje: SaveContractor postojeću vezu zadržava bez nove verzije, a maknutu arhivira",
			procitaj: func(t *testing.T, n *cvor, _ string, p []byte) (any, error) {
				x := teret[models.ContractorAssignment](t, p)
				return NewOrgRepository(n.db, n.rec).ListAssignments(ctxRaz, x.ContractorID)
			},
		},
		{
			entitet: EntityCounties, tablica: "counties", kljuc: "id", broj: true,
			napravi: func(t *testing.T, a *cvor) {
				var c models.County
				puni(&c)
				nuzno(t, NewTerritoryRepository(a.db, a.rec).CreateCounty(ctxRaz, &c))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				c := teret[models.County](t, p)
				c.Prefect += " (izmjena)"
				nuzno(t, NewTerritoryRepository(b.db, b.rec).UpdateCounty(ctxRaz, c))
			},
			procitaj: func(t *testing.T, n *cvor, id string, _ []byte) (any, error) {
				broj, err := strconv.Atoi(id)
				nuzno(t, err)
				return NewTerritoryRepository(n.db, n.rec).GetCountyByID(ctxRaz, broj)
			},
		},
		{
			entitet: EntityMunicipalities, tablica: "municipalities", kljuc: "id", broj: true,
			napravi: func(t *testing.T, a *cvor) {
				var m models.Municipality
				puni(&m)
				m.CountyID, m.CountyName = osnovaZupanija, ""
				nuzno(t, NewTerritoryRepository(a.db, a.rec).CreateMunicipality(ctxRaz, &m))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				m := teret[models.Municipality](t, p)
				m.HeadName += " (izmjena)"
				nuzno(t, NewTerritoryRepository(b.db, b.rec).UpdateMunicipality(ctxRaz, m))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewTerritoryRepository(n.db, n.rec).ListMunicipalities(ctxRaz, osnovaZupanija, "", "")
			},
		},
		{
			entitet: EntitySettlements, tablica: "settlements", kljuc: "id", broj: true,
			napravi: func(t *testing.T, a *cvor) {
				var s models.Settlement
				puni(&s)
				s.MunicipalityID, s.CountyID = osnovaOpcina, osnovaZupanija
				nuzno(t, NewTerritoryRepository(a.db, a.rec).CreateSettlement(ctxRaz, &s))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				s := teret[models.Settlement](t, p)
				s.Name += " (izmjena)"
				nuzno(t, NewTerritoryRepository(b.db, b.rec).UpdateSettlement(ctxRaz, s))
			},
			procitaj: func(_ *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return NewTerritoryRepository(n.db, n.rec).ListSettlements(ctxRaz, osnovaOpcina, 0, "")
			},
		},
		{
			entitet: peers.EntityMemberships, tablica: "memberships", kljuc: "node_id",
			napravi: func(t *testing.T, a *cvor) {
				// Članstvo upisuje samo primanje u mrežu (neizvezena
				// saveMembership, uz potpis mrežnog ključa); ovdje isti upis
				// površine i ista verzija, bez kriptografije
				var m razmjena.Membership
				puni(&m)
				tx, err := a.db.Begin()
				nuzno(t, err)
				defer tx.Rollback()
				primatelj, err := json.Marshal(m.Primatelj.UTC())
				nuzno(t, err)
				_, err = tx.Exec(`INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at, primatelj)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.DeviceID, m.DeviceKey, m.Network, m.IssuedBy, m.IssuedAt.UTC(), m.ExpiresAt.UTC(), m.Signature, time.Now().UTC(), string(primatelj))
				nuzno(t, err)
				_, err = a.rec.Record(ctxRaz, tx, peers.EntityMemberships, m.DeviceID, m)
				nuzno(t, err)
				nuzno(t, tx.Commit())
			},
			bezUredjivanja: "članstvo se mijenja samo primanjem u mrežu ili opozivom, kroz neizvezene metode servisa čvorova",
			procitaj: func(t *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return servisCvorova(t, n).ListMembers(ctxRaz)
			},
			preskoci: map[string]string{"created_at": vrijemeVerzije},
		},
		{
			entitet: peers.EntityOvlasti, tablica: "ovlasti", kljuc: "node_id",
			napravi: func(t *testing.T, a *cvor) {
				// ovlast upisuje samo nositelj ključa mreže (IzdajOvlast, uz
				// potpis); ovdje isti upis površine i ista verzija
				var o razmjena.Ovlast
				puni(&o)
				tx, err := a.db.Begin()
				nuzno(t, err)
				defer tx.Rollback()
				_, err = tx.Exec(`INSERT INTO ovlasti (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, o.DeviceID, o.DeviceKey, o.Network, o.IssuedBy, o.IssuedAt.UTC(), o.ExpiresAt.UTC(), o.Signature, time.Now().UTC())
				nuzno(t, err)
				_, err = a.rec.Record(ctxRaz, tx, peers.EntityOvlasti, o.DeviceID, o)
				nuzno(t, err)
				nuzno(t, tx.Commit())
			},
			bezUredjivanja: "ovlast se mijenja samo izdavanjem ili opozivom nositelja ključa mreže",
			procitaj: func(t *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return servisCvorova(t, n).ListOvlasti(ctxRaz)
			},
			preskoci: map[string]string{"created_at": vrijemeVerzije},
		},
		{
			entitet: peers.EntityOpozivi, tablica: "opozivi", kljuc: "id",
			napravi: func(t *testing.T, a *cvor) {
				var op peers.Opoziv
				puni(&op)
				tx, err := a.db.Begin()
				nuzno(t, err)
				defer tx.Rollback()
				_, err = tx.Exec(`INSERT INTO opozivi (id, vrsta, node_id, public_key, issued_at, opozvano_at, opozvao, potpisnik, potpis)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, op.ID, op.Vrsta, op.NodeID, op.PublicKey, op.IssuedAt.UTC(), op.OpozvanoAt.UTC(), op.Opozvao, op.Potpisnik, op.Potpis)
				nuzno(t, err)
				_, err = a.rec.Record(ctxRaz, tx, peers.EntityOpozivi, op.ID, op)
				nuzno(t, err)
				nuzno(t, tx.Commit())
			},
			bezUredjivanja: "opoziv je trajan: ne mijenja se i ne briše",
			bezArhiviranja: "opoziv se ne poništava: arhiviranu verziju mogao bi poslati i član s izmijenjenim programom; ponovno primanje izdaje novo, kasnije članstvo",
			procitaj: func(t *testing.T, n *cvor, _ string, _ []byte) (any, error) {
				return servisCvorova(t, n).ListOpozivi(ctxRaz)
			},
		},
		{
			entitet: peers.EntityPeers, tablica: "peers", kljuc: "node_id",
			napravi: func(t *testing.T, a *cvor) {
				var p peers.Peer
				puni(&p)
				p.PublicKey = razmjena.PublicKeyString(ed25519.PublicKey(bytes.Repeat([]byte{7}, ed25519.PublicKeySize)))
				nuzno(t, servisCvorova(t, a).SavePeer(ctxRaz, p))
			},
			uredi: func(t *testing.T, b *cvor, _ string, p []byte) {
				x := teret[peers.Peer](t, p)
				x.Name += " (izmjena)"
				nuzno(t, servisCvorova(t, b).SavePeer(ctxRaz, *x))
			},
			procitaj: func(t *testing.T, n *cvor, id string, _ []byte) (any, error) {
				return servisCvorova(t, n).GetPeer(ctxRaz, id)
			},
			preskoci: map[string]string{
				"last_seen":      "kad je čvor zadnji put viđen opisuje odnos dva čvora i namjerno ne putuje",
				"last_sync":      "zadnja razmjena opisuje odnos dva čvora i namjerno ne putuje",
				"last_sync_note": "bilješka o zadnjoj razmjeni opisuje odnos dva čvora i namjerno ne putuje",
			},
		},
	}
}

func servisCvorova(t *testing.T, n *cvor) *peers.Service {
	t.Helper()
	s, err := peers.NewService(n.db, n.rec, &peers.Node{ID: n.ime, Name: n.ime}, peers.Ports{})
	nuzno(t, err)
	return s
}

// ---- tijek ----

func TestRazmjenaSvihEntiteta(t *testing.T) {
	razmjenaSvihEntiteta(t)
}

// Isti put, ali sa svim vremenima u zoni bez imena (kao "+02:00" iz JSON-a
// na čvoru s TZ=UTC). modernc takvu zonu zapiše kao "+0200 +0200" i više je
// ne zna pročitati, pa čitač repozitorija javi "unsupported Scan, storing
// driver.Value type string into type *time.Time". Izvor dobiva vremena bez
// imena izravno iz punila, a primatelj iz JSON-a verzija (novo_polje
// prepiše u zonu punila i vremena koja je izvor sam postavio): pomak je
// izabran tako da ga mjesna zona nema, pa test hvata grešku bez obzira na TZ.
func TestRazmjenaSvihEntitetaUZoniBezImena(t *testing.T) {
	prije := zonaPunila
	zonaPunila = zonaBezImena(t)
	t.Cleanup(func() { zonaPunila = prije })
	razmjenaSvihEntiteta(t)
}

// zonaBezImena vraća zonu bez imena s pomakom koji mjesna zona nema ni u
// vrijeme punila ni sada (repozitoriji sami pišu time.Now()): tada i
// primatelj, čitajući JSON verzije, dobije zonu bez imena. Prvi izbor je
// "+02:00", ljetno vrijeme u Zagrebu; na čvoru koji taj pomak ima (Zagreb
// ljeti, Helsinki zimi) uzima se sljedeći. Mjesna zona u dva trenutka ima
// najviše dva pomaka, pa jedan od tri uvijek preostaje.
func zonaBezImena(t *testing.T) *time.Location {
	t.Helper()
	for _, pomak := range []int{2 * 3600, 3 * 3600, 4 * 3600} {
		mjesni := false
		for _, kad := range []time.Time{vrijemePunila, time.Now()} {
			if _, p := kad.In(time.Local).Zone(); p == pomak {
				mjesni = true
			}
		}
		if !mjesni {
			return time.FixedZone("", pomak)
		}
	}
	t.Fatal("mjesna zona ima sve probne pomake")
	return nil
}

func razmjenaSvihEntiteta(t *testing.T) {
	// spremište sadržaja je jedno za program; izvornici ga trebaju na oba čvora
	spremisteTesta, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	nuzno(t, err)
	prijasnje := Spremiste()
	SetSpremiste(spremisteTesta)
	// nazivi razina su globalni: primjena ih postavlja odmah, pa se vraćaju
	nazivi := models.Terms()
	t.Cleanup(func() {
		SetSpremiste(prijasnje)
		spremisteTesta.Zatvori()
		models.SetTerms(nazivi)
	})

	for _, s := range slucajeviRazmjene() {
		t.Run(s.entitet, func(t *testing.T) { provjeriRazmjenu(t, s) })
	}
}

func provjeriRazmjenu(t *testing.T, s slucajRazmjene) {
	a, b := noviCvor(t, "ured"), noviCvor(t, "unraid")
	s.napravi(t, a)
	id := jedinoIzKnjige(t, a, s.entitet)
	posalji(t, a, b)

	var arg any = id
	if s.broj {
		n, err := strconv.Atoi(id)
		nuzno(t, err)
		arg = n
	}
	usporedi := func(t *testing.T) {
		t.Helper()
		ra, rb := redak(t, a, s, arg), redak(t, b, s, arg)
		if ra == nil {
			t.Fatalf("zapis %s nije na površini izvora", id)
		}
		if rb == nil {
			t.Fatalf("zapis %s nije stigao na površinu primatelja", id)
		}
		stupci := make([]string, 0, len(ra))
		for c := range ra {
			stupci = append(stupci, c)
		}
		sort.Strings(stupci)
		for _, c := range stupci {
			if _, ok := s.preskoci[c]; ok {
				continue
			}
			if !reflect.DeepEqual(ra[c], rb[c]) {
				t.Errorf("stupac %s nije prenesen: izvor %v, primatelj %v", c, ra[c], rb[c])
			}
		}
	}

	// procitajNa čita zapis stvarnim čitačem repozitorija, kao program
	procitajNa := func(t *testing.T, n *cvor) {
		t.Helper()
		top, err := n.rec.Latest(ctxRaz, s.entitet, id)
		nuzno(t, err)
		x, err := s.procitaj(t, n, id, top.Payload)
		if err != nil {
			t.Fatalf("čitač repozitorija na čvoru %s: %v", n.ime, err)
		}
		if prazno(x) {
			t.Fatalf("čitač repozitorija na čvoru %s nije našao zapis %s", n.ime, id)
		}
	}

	t.Run("povrsina", usporedi)
	t.Run("citanje", func(t *testing.T) {
		procitajNa(t, a)
		procitajNa(t, b)
	})

	// verzija iz novijeg programa: polje koje ovaj program ne poznaje, a
	// sva vremena zapisana u zoni punila, kao s čvora u toj zoni — tako i
	// vremena koja izvor sam postavi (updated_at iz time.Now().UTC()) do
	// primjene stižu u zoni punila
	novoPolje := false
	t.Run("novo_polje", func(t *testing.T) {
		top, err := a.rec.Latest(ctxRaz, s.entitet, id)
		nuzno(t, err)
		var m map[string]any
		d := json.NewDecoder(bytes.NewReader(top.Payload))
		d.UseNumber()
		nuzno(t, d.Decode(&m))
		uZonu(m, zonaPunila)
		m["x_novo_polje"] = "iz novijeg programa"
		payload, err := json.Marshal(m)
		nuzno(t, err)
		v := ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: s.entitet, EntityID: id,
			NodeID: "noviji", Supersedes: top.VersionID, Payload: payload, CreatedAt: time.Now().In(zonaPunila),
			SchemaVersion: ledger.SchemaVersion, Channel: top.Channel}
		primi(t, b, []ledger.Version{v})
		got, err := b.rec.Latest(ctxRaz, s.entitet, id)
		nuzno(t, err)
		if got.VersionID != v.VersionID || !bytes.Equal(got.Payload, payload) {
			t.Fatalf("knjiga primatelja ne čuva verziju novijeg programa bajt za bajt:\nposlano %s\nčuva    %s", payload, got.Payload)
		}
		usporedi(t)
		procitajNa(t, b)
		novoPolje = !t.Failed()
	})

	t.Run("lokalna_izmjena", func(t *testing.T) {
		if s.uredi == nil {
			t.Skip("bez puta izmjene: " + s.bezUredjivanja)
		}
		if !novoPolje {
			t.Skip("verzija s novim poljem nije prošla")
		}
		prije, err := b.rec.Latest(ctxRaz, s.entitet, id)
		nuzno(t, err)
		s.uredi(t, b, id, prije.Payload)
		poslije, err := b.rec.Latest(ctxRaz, s.entitet, id)
		nuzno(t, err)
		if poslije.VersionID == prije.VersionID || poslije.NodeID != b.ime {
			t.Fatalf("lokalna izmjena nije zapisala novu verziju (zadnja s čvora %s)", poslije.NodeID)
		}
		procitajNa(t, b)
		var m map[string]json.RawMessage
		nuzno(t, json.Unmarshal(poslije.Payload, &m))
		if string(m["x_novo_polje"]) != `"iz novijeg programa"` {
			prijaviIliPreskoci(t, s.entitet+"/lokalna_izmjena", "izmjena na starijem čvoru izgubila je polje novijeg programa: %s", poslije.Payload)
			return
		}
		prosloBezGreske(t, s.entitet+"/lokalna_izmjena")
	})

	t.Run("arhiviranje", func(t *testing.T) {
		top, err := b.rec.Latest(ctxRaz, s.entitet, id)
		nuzno(t, err)
		v := *top
		v.VersionID, v.NodeID, v.Supersedes, v.Archived, v.CreatedAt = uuid.Must(uuid.NewV7()).String(), "ured", top.VersionID, true, time.Now().UTC()
		primi(t, b, []ledger.Version{v})
		var n int
		nuzno(t, b.db.QueryRow(`SELECT count(*) FROM `+s.tablica+` WHERE `+s.kljuc+` = ?`, arg).Scan(&n))
		if s.bezArhiviranja != "" {
			// namjerno ostaje na površini
			if n != 1 {
				t.Errorf("arhivirana verzija je maknula zapis %s, a ne smije: %s", id, s.bezArhiviranja)
			}
			return
		}
		if n != 0 {
			prijaviIliPreskoci(t, s.entitet+"/arhiviranje", "arhivirana verzija nije maknula zapis %s s površine primatelja", id)
			return
		}
		prosloBezGreske(t, s.entitet+"/arhiviranje")
	})
}

// uZonu prepisuje vremena (tekst u obliku RFC 3339) u dekodiranom JSON-u u
// zadanu zonu; trenutak ostaje isti
func uZonu(x any, zona *time.Location) any {
	switch x := x.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = uZonu(e, zona)
		}
	case []any:
		for i, e := range x {
			x[i] = uZonu(e, zona)
		}
	case string:
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t.In(zona).Format(time.RFC3339Nano)
		}
	}
	return x
}

// jsonUUTC vraća JSON zapisan u stupcu teksta s vremenima u UTC-u, da se
// isti trenutak iz druge zone ne javlja kao razlika; ostali tekst ne dira
func jsonUUTC(s string) string {
	if !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") {
		return s
	}
	var x any
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	if d.Decode(&x) != nil {
		return s
	}
	b, err := json.Marshal(uZonu(x, time.UTC))
	if err != nil {
		return s
	}
	return string(b)
}

// jedinoIzKnjige vraća identitet jedinog zapisa entiteta koji je izvor upisao
func jedinoIzKnjige(t *testing.T, n *cvor, entitet string) string {
	t.Helper()
	rows, err := n.db.Query(`SELECT DISTINCT entity_id FROM record_versions WHERE entity = ?`, entitet)
	nuzno(t, err)
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		nuzno(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	if len(ids) != 1 {
		t.Fatalf("izvor je za %s upisao %d zapisa u knjigu, a treba točno jedan: %v", entitet, len(ids), ids)
	}
	return ids[0]
}

// posalji prenosi sve verzije izvora primatelju, kao razmjena
func posalji(t *testing.T, a, b *cvor) {
	t.Helper()
	verzije, err := a.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	var zadrzane []ledger.Version
	for _, v := range verzije {
		if KeepVersion(v) {
			zadrzane = append(zadrzane, v)
		}
	}
	primi(t, b, zadrzane)
}

func primi(t *testing.T, b *cvor, verzije []ledger.Version) {
	t.Helper()
	if _, err := b.rec.Apply(ctxRaz, verzije); err != nil {
		t.Fatalf("knjiga primatelja: %v", err)
	}
	if err := ApplyVersions(ctxRaz, b.db, b.rec, verzije); err != nil {
		t.Fatalf("površina primatelja: %v", err)
	}
}

// redak čita cijeli redak zapisa s površine; nil kad ga nema
func redak(t *testing.T, n *cvor, s slucajRazmjene, arg any) map[string]any {
	t.Helper()
	rows, err := n.db.Query(`SELECT * FROM `+s.tablica+` WHERE `+s.kljuc+` = ?`, arg)
	nuzno(t, err)
	defer rows.Close()
	stupci, err := rows.Columns()
	nuzno(t, err)
	tipovi, err := rows.ColumnTypes()
	nuzno(t, err)
	if !rows.Next() {
		nuzno(t, rows.Err())
		return nil
	}
	vrijednosti := make([]any, len(stupci))
	ptrs := make([]any, len(stupci))
	for i := range vrijednosti {
		ptrs[i] = &vrijednosti[i]
	}
	nuzno(t, rows.Scan(ptrs...))
	out := make(map[string]any, len(stupci))
	for i, c := range stupci {
		switch x := vrijednosti[i].(type) {
		case []byte:
			out[c] = jsonUUTC(string(x))
		case time.Time:
			// isti trenutak smije biti zapisan u drugoj zoni
			out[c] = x.UTC().Format(time.RFC3339Nano)
		case string:
			// upravljač vrijeme koje ne zna raščlaniti vrati kao tekst
			switch tipovi[i].DatabaseTypeName() {
			case "DATE", "DATETIME", "TIMESTAMP":
				if x != "" {
					t.Errorf("%s.%s na čvoru %s: vrijeme %q se ne da pročitati", s.tablica, c, n.ime, x)
				}
			}
			out[c] = jsonUUTC(x)
		default:
			out[c] = x
		}
	}
	return out
}

// prazno javlja je li čitač vratio prazno: nil, prazan popis ili nulu
func prazno(x any) bool {
	v := reflect.ValueOf(x)
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	case reflect.Slice, reflect.Map:
		return v.Len() == 0
	}
	return v.IsZero()
}

// ---- pokrivenost ----

// Svaki entitet koji applyOne zna upisati mora imati slučaj u tablici, pa
// novi entitet bez provjere razmjene ruši test.
func TestRazmjenaSvihEntitetaPokrivenost(t *testing.T) {
	fset := token.NewFileSet()
	datoteke, err := filepath.Glob("*.go")
	nuzno(t, err)
	var stabla []*ast.File
	for _, d := range datoteke {
		if strings.HasSuffix(d, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, d, nil, 0)
		nuzno(t, err)
		stabla = append(stabla, f)
	}

	// vrijednosti tekstualnih konstanti paketa: EntityStations = "stations"…
	konstante := map[string]string{}
	var applyOne *ast.FuncDecl
	for _, f := range stabla {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				if d.Tok != token.CONST {
					continue
				}
				for _, sp := range d.Specs {
					vs := sp.(*ast.ValueSpec)
					for i, ime := range vs.Names {
						if i >= len(vs.Values) {
							continue
						}
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if v, err := strconv.Unquote(lit.Value); err == nil {
								konstante[ime.Name] = v
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Name.Name == "applyOne" && d.Recv == nil {
					applyOne = d
				}
			}
		}
	}
	if applyOne == nil {
		t.Fatal("applyOne nije nađen")
	}

	var entiteti []string
	ast.Inspect(applyOne.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		if sel, ok := sw.Tag.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Entity" {
			return true
		}
		for _, st := range sw.Body.List {
			for _, e := range st.(*ast.CaseClause).List {
				switch e := e.(type) {
				case *ast.BasicLit:
					v, _ := strconv.Unquote(e.Value)
					entiteti = append(entiteti, v)
				case *ast.Ident:
					v, ok := konstante[e.Name]
					if !ok {
						t.Errorf("konstanta %s iz applyOne nema tekstualnu vrijednost u paketu", e.Name)
						continue
					}
					entiteti = append(entiteti, v)
				case *ast.SelectorExpr:
					t.Errorf("entitet %s.%s iz drugog paketa: dodaj ga u pokrivenost", e.X, e.Sel.Name)
				}
			}
		}
		return false
	})
	if len(entiteti) < 40 {
		t.Fatalf("u applyOne nađeno je samo %d entiteta — je li se switch promijenio?", len(entiteti))
	}

	imaSlucaj := map[string]bool{}
	for _, s := range slucajeviRazmjene() {
		if imaSlucaj[s.entitet] {
			t.Errorf("entitet %s ima dva slučaja", s.entitet)
		}
		imaSlucaj[s.entitet] = true
		if s.procitaj == nil {
			t.Errorf("slučaj %s nema procitaj: zapis se mora pročitati čitačem repozitorija", s.entitet)
		}
	}
	uSwitchu := map[string]bool{}
	for _, e := range entiteti {
		uSwitchu[e] = true
		if !imaSlucaj[e] {
			t.Errorf("entitet %s primjenjuje se razmjenom (applyOne), a nema slučaj u slucajeviRazmjene", e)
		}
	}
	for e := range imaSlucaj {
		if !uSwitchu[e] {
			t.Errorf("slučaj za %s, a applyOne ga ne poznaje", e)
		}
	}
	for k := range poznateGreske {
		if e, _, _ := strings.Cut(k, "/"); !imaSlucaj[e] {
			t.Errorf("poznata greška %s odnosi se na entitet bez slučaja", k)
		}
	}
}
