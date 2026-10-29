package web

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/repository"
)

// nizBrojki je jedan izmišljeni niz u popisu nizova arhive.
type nizBrojki struct {
	letva, izvor, velicina, vrsta, od string
	zapisa                            int64
}

// arhivaBrojki gradi arhivu s danim nizovima i upitima (spoj, profili) i
// otvara je za čitanje kao što to radi čvor.
func arhivaBrojki(t *testing.T, nizovi []nizBrojki, upiti ...string) *repository.ArhivaRepository {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	a, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(a); err != nil {
		t.Fatal(err)
	}
	for _, n := range nizovi {
		if _, err := a.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, od, zapisa) VALUES ('primjer', ?, ?, ?, ?, ?, ?)`,
			n.letva, n.izvor, n.velicina, n.vrsta, n.od, n.zapisa); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range upiti {
		if _, err := a.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	a.Close()
	arh, err := repository.OpenArhiva(put)
	if err != nil || arh == nil {
		t.Fatalf("arhiva: %v", err)
	}
	t.Cleanup(func() { arh.Close() })
	return arh
}

// nizoviSvihVrsta pokrivaju svaku vrstu brojke na naslovnoj: satne i dnevne
// vodostaje, protok, prave kišomjere, popunu iz ERA5, izvedenu oborinu,
// meteorologiju, temperaturu vode (samo u zbroju) i kotu. Varaždin je i u
// ARSO-u, pa zbroj postaja po izvoru (4) nije broj različitih postaja (3);
// satni protok ne smije u satne vodostaje, a radar ima više točaka od ERA5.
var nizoviSvihVrsta = []nizBrojki{
	{"batina", "dhmz", "vodostaj", "satni", "1987-01-01", 100},
	{"vukovar", "dhmz", "vodostaj", "satni", "1990-05-01", 50},
	{"ilok", "hv", "vodostaj", "satni", "1980-01-01", 30},
	{"dalj", "x-kasni", "vodostaj", "satni", "2001-01-01", 8},
	{"batina", "dhmz", "vodostaj", "dnevni", "1876-03-15", 1000},
	{"zemlja", "rucno", "vodostaj", "dnevni", "", 5},
	{"botovo", "dhmz", "protok", "dnevni", "1926-01-01", 200},
	{"botovo", "dhmz", "protok", "satni", "1990-01-01", 25},
	{"osijek-k", "kisomjer-dhmz", "oborina", "dnevni", "1950-01-01", 300},
	{"varazdin-k", "kisomjer-dhmz", "oborina", "dnevni", "1949-01-01", 310},
	{"varazdin-k", "kisomjer-dhmz", "oborina", "satni", "1960-01-01", 40},
	{"ljubljana-k", "kisomjer-arso", "oborina", "dnevni", "1955-01-01", 20},
	{"varazdin-k", "kisomjer-arso", "oborina", "dnevni", "1955-01-01", 5},
	{"varazdin-k", "hibrid-era5", "oborina", "dnevni", "1949-01-01", 7},
	{"sliv-a", "openmeteo-era5", "oborina", "dnevni", "1940-01-01", 100},
	{"sliv-b", "openmeteo-era5", "oborina", "dnevni", "1940-01-01", 100},
	{"sliv-c", "openmeteo-era5", "oborina", "dnevni", "1940-01-01", 100},
	{"sliv-d", "openmeteo-era5", "oborina", "dnevni", "1940-01-01", 100},
	{"sliv-e", "openmeteo-era5-land", "oborina", "dnevni", "1950-01-01", 60},
	{"sliv-a", "radar", "oborina", "satni", "2010-01-01", 9},
	{"sliv-b", "radar", "oborina", "satni", "2010-01-01", 1},
	{"sliv-c", "radar", "oborina", "satni", "2010-01-01", 1},
	{"sliv-d", "radar", "oborina", "satni", "2010-01-01", 1},
	{"sliv-e", "radar", "oborina", "satni", "2010-01-01", 1},
	{"zagreb-m", "dhmz", "snijeg", "dnevni", "", 11},
	{"zagreb-m", "dhmz", "temperatura-zraka", "satni", "", 12},
	{"zagreb-m", "dhmz", "visina-snijega", "dnevni", "", 13},
	{"batina", "dhmz", "temperatura", "dnevni", "", 14},
	{"vukovar", "geodezija", "kota", "dnevni", "", 1},
}

// unixSat je trenutak u UTC-u kao Unix sekunde, za upise u spoj.
func unixSat(g, m, d, h int) string {
	return strconv.FormatInt(time.Date(g, time.Month(m), d, h, 0, 0, 0, time.UTC).Unix(), 10)
}

// upitiSvihVrsta su rekordi u spoju (Batina dnevni, Vukovar satni, Osijek
// bez letve u radnoj bazi, preračun iz Mohácsa viši ali ne ulazi), dva
// profila s tri točke i jedna HQ krivulja.
var upitiSvihVrsta = []string{
	`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost) VALUES
		('batina', 'vodostaj', 'dnevni', ` + unixSat(1965, 6, 25, 0) + `, 900, 'dhmz', 'srednjak', 1),
		('batina', 'vodostaj', 'satni', ` + unixSat(2000, 1, 1, 3) + `, 950, 'preracun-mohacs', 'trenutna', 5),
		('vukovar', 'vodostaj', 'satni', ` + unixSat(2013, 6, 13, 17) + `, 911.5, 'hv', 'trenutna', 1),
		('osijek', 'vodostaj', 'dnevni', ` + unixSat(1975, 1, 1, 0) + `, 400, 'dhmz', 'srednjak', 1)`,
	`INSERT INTO profili (id, letva, datum, vodostaj, kota_nule) VALUES (1, 'batina', '2020-03-01', 120, 80.0), (2, 'ilok', '2021-04-01', 90, 75.0)`,
	`INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (1, 0, 82.5), (1, 5, 79.0), (2, 0, 77.1)`,
	`INSERT INTO hq_krivulje (id, letva, vrijedi_od, vrijedi_do, izvor) VALUES (1, 'batina', '2001-01-01', '', 'DHMZ')`,
}

// radnaBazaBrojki je radna baza s letvama Batina, Vukovar (bez imena) i
// Primjerovo; tri očitanja su iz zadnja 24 h (dvije letve), jedno starije.
func radnaBazaBrojki(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "radna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('s-batina', 'batina', 'Batina', '2026-01-01', '2026-01-01')`,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('s-vukovar', 'vukovar', '', '2026-01-01', '2026-01-01')`,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('s-primjer', 'primjerovo', 'Primjerovo', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	sad := time.Now().UTC()
	for i, o := range []struct {
		stanica string
		prije   time.Duration
	}{{"s-primjer", time.Hour}, {"s-primjer", 2 * time.Hour}, {"s-batina", 3 * time.Hour}, {"s-primjer", 72 * time.Hour}} {
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, user_id, created_at, updated_at)
			VALUES (?, ?, ?, 100, 'RUČNO', '', ?, ?)`, "r-"+strconv.Itoa(i), o.stanica, sad.Add(-o.prije), sad, sad); err != nil {
			t.Fatal(err)
		}
	}
	return baza
}

// bazaUpita je obična SQLite baza s danim upitima, za krnje sheme.
func bazaUpita(t *testing.T, upiti ...string) *sql.DB {
	t.Helper()
	baza, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "krnja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range upiti {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return baza
}

// prognozeBrojki je baza prognoza s tri vrijednosti satnog lanca iz dva
// izdanja i četiri dnevne; ostalih tablica nema, pa se ne broje.
func prognozeBrojki(t *testing.T) *CitacPrognoza {
	t.Helper()
	return &CitacPrognoza{db: bazaUpita(t,
		`CREATE TABLE izdane (izdano INTEGER, v REAL)`,
		`INSERT INTO izdane VALUES (1, 0), (1, 0), (2, 0)`,
		`CREATE TABLE dnevne (v REAL)`,
		`INSERT INTO dnevne VALUES (0), (0), (0), (0)`,
	)}
}

// pisiDatoteku piše datoteku s danim sadržajem, dopunjenu do zadanog broja
// bajtova.
func pisiDatoteku(t *testing.T, put string, bajtova int, sadrzaj string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		t.Fatal(err)
	}
	b := []byte(sadrzaj)
	for len(b) < bajtova {
		b = append(b, 'x')
	}
	if err := os.WriteFile(put, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// mapaPodatakaBrojki slaže datoteke uz bazu: radna baza sa -wal, arhiva
// vodostaja, prognoze, dvije datoteke provjere (dva retka i samo zaglavlje),
// oborine; i stablo izvornih datoteka od 11 bajtova. Vraća put baze i stabla.
func mapaPodatakaBrojki(t *testing.T) (dbPath, podaciDir string) {
	t.Helper()
	mapa := t.TempDir()
	pisiDatoteku(t, filepath.Join(mapa, "gocop.db"), 100, "")
	pisiDatoteku(t, filepath.Join(mapa, "gocop.db-wal"), 20, "")
	pisiDatoteku(t, filepath.Join(mapa, "vodostaji.db"), 300, "")
	pisiDatoteku(t, filepath.Join(mapa, "prognoze.db"), 50, "")
	pisiDatoteku(t, filepath.Join(mapa, "hindcast-sat.csv"), 0, "a,b\n1,2\n3,4\n")
	pisiDatoteku(t, filepath.Join(mapa, "hindcast-val.csv"), 0, "a\n")
	pisiDatoteku(t, filepath.Join(mapa, "oborine.db"), 7, "")
	stablo := t.TempDir()
	pisiDatoteku(t, filepath.Join(stablo, "a.txt"), 5, "")
	pisiDatoteku(t, filepath.Join(stablo, "pod", "b.txt"), 6, "")
	return filepath.Join(mapa, "gocop.db"), stablo
}

// posluziteljSaSvime ima arhivu, radnu bazu, prognoze i datoteke uz bazu.
func posluziteljSaSvime(t *testing.T) *Server {
	t.Helper()
	dbPath, podaciDir := mapaPodatakaBrojki(t)
	s := &Server{db: radnaBazaBrojki(t), dbPath: dbPath, podaciDir: podaciDir}
	s.SetArhiva(arhivaBrojki(t, nizoviSvihVrsta, upitiSvihVrsta...))
	s.SetPrognoze(prognozeBrojki(t))
	return s
}

// bezVremena izjednačava trenutak brojanja, jedino polje koje je „sada”.
func bezVremena(t *testing.T, b *BrojkePodataka) *BrojkePodataka {
	t.Helper()
	if b == nil {
		t.Fatal("brojke su nil")
	}
	if b.Izracunato.IsZero() || time.Since(b.Izracunato) > time.Minute {
		t.Errorf("izračunato %v nije sada", b.Izracunato)
	}
	k := *b
	k.Izracunato = time.Time{}
	return &k
}

func TestBrojkeSvegaStoCvorNosi(t *testing.T) {
	b := bezVremena(t, posluziteljSaSvime(t).izbrojiPodatke(context.Background()))
	want := &BrojkePodataka{
		Zapisa:  2624,
		Bajtova: 300 + 11 + 120 + 50 + 12 + 2 + 7,
		Godina:  time.Now().Year() - 1876,

		// batina, vukovar, ilok, dalj, zemlja, botovo iz arhive i primjerovo uživo
		Letvi:      7,
		LetviUzivo: 2,

		SatnihVodostaja:  188,
		SatniOd:          1980,
		DnevnihVodostaja: 1005,
		DnevniOd:         1876,
		Protoka:          225,
		ProtokOd:         1926,

		OborinaPrave:    675,
		PostajaPravih:   3,
		PraveOd:         1949,
		OborinaIzvedene: 473,
		OborinaPopuna:   7,
		TocakaIzvedenih: 4,
		IzvedeneOd:      1940,
		Meteo:           36,

		Prognoza: 7,
		Izdanja:  2,
		Provjera: 2,

		Ocitanja:    4,
		Ocitanja24h: 3,

		Profili:     2,
		ProfilTocke: 3,
		HQ:          1,

		NajstarijiDan:   "15. 3. 1876.",
		NajstarijaLetva: "Batina",

		Rekordi: []Rekord{
			{Voda: "Dunav", Letva: "Batina", Cm: 900, Kad: "25. 6. 1965."},
			{Voda: "Dunav", Letva: "vukovar", Cm: 911.5, Kad: "13. 6. 2013. u 19 h"},
			{Voda: "Drava", Letva: "osijek", Cm: 400, Kad: "1. 1. 1975."},
		},
		Velicine: []Velicina{
			{"arhiva vodostaja i meteorologije", 300},
			{"izvorne datoteke nizova", 11},
			{"radna baza", 120},
			{"prognoze i provjera unatrag", 64},
			{"oborine uživo", 7},
		},
	}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("brojke:\n dobio %+v\n htio  %+v", b, want)
	}
}

// Čvor bez arhive, bez baze i bez prognoza ima same nule i ne pada.
func TestBrojkePraznogCvora(t *testing.T) {
	b := bezVremena(t, (&Server{}).izbrojiPodatke(context.Background()))
	if !reflect.DeepEqual(b, &BrojkePodataka{}) {
		t.Errorf("prazan čvor ima brojke: %+v", b)
	}
}

// Arhiva koja se ne da čitati ne daje ni brojke ni rekorde; radna baza bez
// letava i dalje broji očitanja, ali nijednu letvu uživo.
func TestBrojkeKadArhivaIBazaNeOdgovaraju(t *testing.T) {
	a := arhivaBrojki(t, nizoviSvihVrsta, upitiSvihVrsta...)
	a.Close()
	s := &Server{db: bazaUpita(t,
		`CREATE TABLE readings (station_id TEXT, measured_at DATETIME)`,
		`INSERT INTO readings VALUES ('s1', '2000-01-01')`)}
	s.SetArhiva(a)
	b := bezVremena(t, s.izbrojiPodatke(context.Background()))
	if !reflect.DeepEqual(b, &BrojkePodataka{Ocitanja: 1}) {
		t.Errorf("brojke: %+v", b)
	}
}

// Letva uživo bez šifre se ne broji: red se ne da pročitati.
func TestBrojkeLetvaBezSifre(t *testing.T) {
	sad := time.Now().UTC()
	baza := bazaUpita(t,
		`CREATE TABLE stations (id TEXT, code TEXT, name TEXT)`,
		`CREATE TABLE readings (station_id TEXT, measured_at DATETIME)`,
		`INSERT INTO stations VALUES ('s1', NULL, 'Bezimena')`)
	if _, err := baza.Exec(`INSERT INTO readings VALUES ('s1', ?)`, sad.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	b := bezVremena(t, (&Server{db: baza}).izbrojiPodatke(context.Background()))
	if !reflect.DeepEqual(b, &BrojkePodataka{Ocitanja: 1, Ocitanja24h: 1}) {
		t.Errorf("brojke: %+v", b)
	}
}

// Najstariji dan koji nije datum: godina se računa iz prva četiri znaka, dan
// ostaje prazan, a bez radne baze letva ostaje pod šifrom, kao i rekord.
func TestBrojkeNajstarijiBezDatuma(t *testing.T) {
	s := &Server{}
	s.SetArhiva(arhivaBrojki(t, []nizBrojki{{"batina", "dhmz", "vodostaj", "dnevni", "1900-xx", 3}},
		`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost) VALUES
			('batina', 'vodostaj', 'dnevni', `+unixSat(1965, 6, 25, 0)+`, 900, 'dhmz', 'srednjak', 1)`))
	b := bezVremena(t, s.izbrojiPodatke(context.Background()))
	want := &BrojkePodataka{Zapisa: 3, Godina: time.Now().Year() - 1900, Letvi: 1, DnevnihVodostaja: 3, DnevniOd: 1900,
		NajstarijaLetva: "batina", Rekordi: []Rekord{{Voda: "Dunav", Letva: "batina", Cm: 900, Kad: "25. 6. 1965."}}}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("brojke:\n dobio %+v\n htio  %+v", b, want)
	}
}

// Početak niza bez godine (prekratak ili ne broj) ne daje ni godinu ni
// najstariji dan.
func TestBrojkeOdBezGodine(t *testing.T) {
	for _, od := range []string{"ab", "abcd-01-01"} {
		s := &Server{}
		s.SetArhiva(arhivaBrojki(t, []nizBrojki{{"batina", "dhmz", "vodostaj", "dnevni", od, 3}}))
		b := bezVremena(t, s.izbrojiPodatke(context.Background()))
		if !reflect.DeepEqual(b, &BrojkePodataka{Zapisa: 3, Letvi: 1, DnevnihVodostaja: 3}) {
			t.Errorf("od %q: %+v", od, b)
		}
	}
}

// Arhiva na drugom disku mjeri se ondje gdje stoji, a ne uz bazu.
func TestBrojkeArhivaNaDrugomMjestu(t *testing.T) {
	dbPath, _ := mapaPodatakaBrojki(t)
	put := filepath.Join(t.TempDir(), "druga.db")
	pisiDatoteku(t, put, 1000, "")
	pisiDatoteku(t, put+"-wal", 24, "")
	s := &Server{dbPath: dbPath}
	s.SetArhivaPut(put)
	b := bezVremena(t, s.izbrojiPodatke(context.Background()))
	if b.Velicine[0] != (Velicina{"arhiva vodostaja i meteorologije", 1024}) || b.Bajtova != 1024+120+64+7 || b.Provjera != 2 {
		t.Errorf("brojke: %+v", b)
	}
}

// Kišomjer sa satnim i dnevnim nizom broji se jednom, većim od dvaju, a
// postaje se zbrajaju po izvoru.
func TestZbrojiRedovePostajeKisomjera(t *testing.T) {
	b := &BrojkePodataka{}
	zbrojiRedove(b, []repository.RedNizova{
		{Velicina: "oborina", Vrsta: "dnevni", Izvor: "kisomjer-dhmz", Letvi: 2, Zapisa: 10},
		{Velicina: "oborina", Vrsta: "satni", Izvor: "kisomjer-dhmz", Letvi: 1, Zapisa: 3},
		{Velicina: "oborina", Vrsta: "dnevni", Izvor: "kisomjer-arso", Letvi: 1, Zapisa: 4},
	})
	if b.PostajaPravih != 3 || b.OborinaPrave != 17 || b.Zapisa != 17 {
		t.Errorf("brojke: %+v", b)
	}
}

// Broj različitih postaja iz arhive ima prednost pred zbrojem po izvoru; kad
// ga arhiva ne da (0), ostaje zbroj.
func TestBrojkeArhivePostajePravih(t *testing.T) {
	redovi := []repository.RedNizova{
		{Velicina: "oborina", Vrsta: "dnevni", Izvor: "kisomjer-dhmz", Letvi: 2, Zapisa: 10},
		{Velicina: "oborina", Vrsta: "dnevni", Izvor: "kisomjer-arso", Letvi: 2, Zapisa: 4},
	}
	for _, tc := range []struct{ pravih, want int }{{0, 4}, {3, 3}} {
		b := &BrojkePodataka{}
		(&Server{}).brojkeArhive(context.Background(), b, &repository.ArhivaBrojke{Redovi: redovi, PravihKisomjera: tc.pravih}, map[string]bool{})
		if b.PostajaPravih != tc.want {
			t.Errorf("arhiva kaže %d: postaja %d, htio %d", tc.pravih, b.PostajaPravih, tc.want)
		}
	}
}

// Najstariji dan s vremenom iza datuma čita se iz prvih deset znakova.
func TestNajstarijiVodostajSVremenom(t *testing.T) {
	b := &BrojkePodataka{}
	(&Server{}).najstarijiVodostaj(context.Background(), b, &repository.ArhivaBrojke{NajstarijiOd: "1876-03-15T00:00:00", NajstarijaLetva: "batina"})
	if b.NajstarijiDan != "15. 3. 1876." || b.NajstarijaLetva != "batina" || b.Godina != time.Now().Year()-1876 {
		t.Errorf("brojke: %+v", b)
	}
}
