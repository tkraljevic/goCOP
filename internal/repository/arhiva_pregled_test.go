package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"

	_ "modernc.org/sqlite"
)

// ocitanjePregleda je jedan zapis niza u arhivi; nil vrijednost je NULL, koji prava
// arhiva ne dopušta, ali čitanje ga mora preživjeti bez pada.
type ocitanjePregleda struct {
	kad time.Time
	v   *float64
}

func vrPregleda(v float64) *float64 { return &v }

func satPregleda(god, mj, dan, h int) time.Time {
	return time.Date(god, time.Month(mj), dan, h, 0, 0, 0, time.UTC)
}

// nizPregleda je redak tablice nizovi s očitanjima koja mu pripadaju.
type nizPregleda struct {
	id                      int64
	velicina, vrsta, otisak string
	poDanu                  int
	od, do                  string
	ocitanja                []ocitanjePregleda
}

// arhivaZaPregled slaže arhivu s nizovima i otvara je kao čvor: samo za
// čitanje. Stupac vrijednosti smije biti NULL da se vidi što čitanje tada radi.
func arhivaZaPregled(t *testing.T, nizovi ...nizPregleda) *ArhivaRepository {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE nizovi (id INTEGER PRIMARY KEY, zona TEXT NOT NULL DEFAULT '', sliv TEXT NOT NULL,
			letva TEXT NOT NULL, izvor TEXT NOT NULL, velicina TEXT NOT NULL,
			vrsta TEXT NOT NULL, po_danu INTEGER NOT NULL DEFAULT 0,
			od TEXT NOT NULL DEFAULT '', do_ TEXT NOT NULL DEFAULT '',
			zapisa INTEGER NOT NULL DEFAULT 0, otisak TEXT NOT NULL DEFAULT '',
			napomena TEXT NOT NULL DEFAULT '');
		CREATE TABLE ocitanja (niz INTEGER NOT NULL, vrijeme INTEGER NOT NULL,
			vrijednost REAL, PRIMARY KEY (niz, vrijeme)) WITHOUT ROWID;`); err != nil {
		t.Fatal(err)
	}
	for _, n := range nizovi {
		if _, err := db.Exec(`INSERT INTO nizovi (id, sliv, letva, izvor, velicina, vrsta, po_danu, od, do_, zapisa, otisak, napomena)
			VALUES (?, 'drava', 'donji-miholjac', 'his2000', ?, ?, ?, ?, ?, ?, ?, 'napomena se u pregledu ne čita')`,
			n.id, n.velicina, n.vrsta, n.poDanu, n.od, n.do, len(n.ocitanja), n.otisak); err != nil {
			t.Fatal(err)
		}
		for _, o := range n.ocitanja {
			if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?, ?, ?)`,
				n.id, o.kad.Unix(), o.v); err != nil {
				t.Fatal(err)
			}
		}
	}
	db.Close()
	r, err := OpenArhiva(put)
	if err != nil || r == nil {
		t.Fatalf("arhiva se ne otvara: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// satniNiz preko dvije granice godine. Najniža i najviša vrijednost godine
// ponavljaju se drugog dana, a 2021. dostiže najviše iz 2020.
func satniNiz() nizPregleda {
	return nizPregleda{id: 1, velicina: "vodostaj", vrsta: "satni", od: "2019-12-30", do: "2021-07-15",
		otisak: "abc123", ocitanja: []ocitanjePregleda{
			{satPregleda(2019, 12, 30, 10), vrPregleda(130)},
			{satPregleda(2019, 12, 30, 11), vrPregleda(100)},
			{satPregleda(2019, 12, 31, 10), vrPregleda(100)},
			{satPregleda(2019, 12, 31, 11), vrPregleda(130)},
			{satPregleda(2020, 1, 1, 0), vrPregleda(140)},
			{satPregleda(2020, 1, 1, 1), vrPregleda(90)},
			{satPregleda(2020, 1, 2, 5), vrPregleda(140)},
			{satPregleda(2020, 1, 2, 6), vrPregleda(95)},
			{satPregleda(2021, 3, 1, 0), vrPregleda(140)},
			{satPregleda(2021, 7, 15, 12), vrPregleda(100)},
		}}
}

// dnevniNiz ima 150 dana od 1. 12. 1965., dovoljno za krivulju trajanja, i
// prelazi granicu godine prije 1970., gdje je unixovo vrijeme negativno.
func dnevniNiz() nizPregleda {
	n := nizPregleda{id: 2, velicina: "protok", vrsta: "dnevni", poDanu: 1, od: "1965-12-01", do: "1966-04-29"}
	for i := 0; i < 150; i++ {
		n.ocitanja = append(n.ocitanja, ocitanjePregleda{
			time.Date(1965, 12, 1+i, 0, 0, 0, 0, time.UTC), vrPregleda(float64(200 + (i*37)%90)),
		})
	}
	return n
}

func pronosNiz() nizPregleda {
	return nizPregleda{id: 3, velicina: "pronos", vrsta: "dnevni", ocitanja: []ocitanjePregleda{
		{satPregleda(2018, 5, 1, 0), vrPregleda(1.5)},
		{satPregleda(2018, 5, 2, 0), vrPregleda(2.5)},
		{satPregleda(2019, 6, 1, 0), vrPregleda(4)},
	}}
}

func jednoOcitanje() nizPregleda {
	return nizPregleda{id: 4, velicina: "vodostaj", vrsta: "satni", ocitanja: []ocitanjePregleda{
		{satPregleda(2024, 2, 29, 23), vrPregleda(-12)},
	}}
}

func prazanNiz() nizPregleda {
	return nizPregleda{id: 5, velicina: "temperatura", vrsta: "satni", od: "2001-01-01", do: "2001-12-31"}
}

// nullUGodini ima NULL uz pravu vrijednost iste godine: zbirni upit ga
// preskoči, a čitanje datuma ekstrema na njemu stane.
func nullUGodini() nizPregleda {
	return nizPregleda{id: 6, velicina: "vodostaj", vrsta: "satni", ocitanja: []ocitanjePregleda{
		{satPregleda(2010, 4, 1, 0), vrPregleda(55)},
		{satPregleda(2010, 4, 1, 1), nil},
	}}
}

// samoNull je godina bez ijedne vrijednosti: najmanja je NULL već u zbiru.
func samoNull() nizPregleda {
	return nizPregleda{id: 7, velicina: "vodostaj", vrsta: "satni", ocitanja: []ocitanjePregleda{
		{satPregleda(2011, 4, 1, 0), nil},
	}}
}

func sviNizovi() []nizPregleda {
	return []nizPregleda{satniNiz(), dnevniNiz(), pronosNiz(), jednoOcitanje(), prazanNiz(), nullUGodini(), samoNull()}
}

// Satni niz: ekstremi i srednjaci po godinama, a uz izjednačen ekstrem
// godina pamti kasniji dan, cijeli niz raniju godinu.
func TestPregledNizaSatni(t *testing.T) {
	r := arhivaZaPregled(t, sviNizovi()...)
	p, err := r.Pregled(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	wantNiz := models.HidroNiz{ID: 1, Sliv: "drava", Letva: "donji-miholjac", Izvor: "his2000",
		Velicina: "vodostaj", Vrsta: "satni", Od: "2019-12-30", Do: "2021-07-15", Zapisa: 10, Otisak: "abc123"}
	if !reflect.DeepEqual(p.Niz, wantNiz) {
		t.Errorf("niz %+v\nočekivano %+v", p.Niz, wantNiz)
	}
	wantGodine := []models.HidroGodina{
		{Godina: 2019, Zapisa: 4, Min: 100, Max: 130, MinNa: "2019-12-31", MaxNa: "2019-12-31", Srednjak: 115, Zbroj: 460},
		{Godina: 2020, Zapisa: 4, Min: 90, Max: 140, MinNa: "2020-01-01", MaxNa: "2020-01-02", Srednjak: 116.25, Zbroj: 465},
		{Godina: 2021, Zapisa: 2, Min: 100, Max: 140, MinNa: "2021-07-15", MaxNa: "2021-03-01", Srednjak: 120, Zbroj: 240},
	}
	if !reflect.DeepEqual(p.Godine, wantGodine) {
		t.Errorf("godine %+v\nočekivano %+v", p.Godine, wantGodine)
	}
	if p.Min != 90 || p.MinNa != "2020-01-01" || p.Max != 140 || p.MaxNa != "2020-01-02" {
		t.Errorf("ekstremi niza %v (%s) – %v (%s)", p.Min, p.MinNa, p.Max, p.MaxNa)
	}
	if p.Srednjak != 116.5 {
		t.Errorf("srednjak %v, očekivano 116.5 (vagan brojem zapisa)", p.Srednjak)
	}
	if p.ZbrojIma || p.Zbroj != 0 {
		t.Errorf("vodostaj se ne zbraja: %v %v", p.ZbrojIma, p.Zbroj)
	}
	wantMjeseci := []models.HidroMjesec{
		{Mjesec: 1, Zapisa: 4, Min: 90, Max: 140, Srednjak: 116.25},
		{Mjesec: 3, Zapisa: 1, Min: 140, Max: 140, Srednjak: 140},
		{Mjesec: 7, Zapisa: 1, Min: 100, Max: 100, Srednjak: 100},
		{Mjesec: 12, Zapisa: 4, Min: 100, Max: 130, Srednjak: 115},
	}
	if !reflect.DeepEqual(p.Mjeseci, wantMjeseci) {
		t.Errorf("mjeseci %+v\nočekivano %+v", p.Mjeseci, wantMjeseci)
	}
	if p.Trajanje != nil {
		t.Errorf("ispod 100 zapisa nema krivulje trajanja: %+v", p.Trajanje)
	}
}

// Dnevni niz ima krivulju trajanja, a godine prije 1970. čitaju se kao i
// ostale. Oznaka po_danu u pregled ne ulazi.
func TestPregledNizaDnevni(t *testing.T) {
	n := dnevniNiz()
	r := arhivaZaPregled(t, n)
	p, err := r.Pregled(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Niz.PoDanu {
		t.Error("pregled ne čita po_danu, a javio je dnevni niz")
	}
	if len(p.Godine) != 2 || p.Godine[0].Godina != 1965 || p.Godine[0].Zapisa != 31 ||
		p.Godine[1].Godina != 1966 || p.Godine[1].Zapisa != 119 {
		t.Fatalf("godine %+v", p.Godine)
	}
	var vrijednosti []float64
	for _, o := range n.ocitanja {
		vrijednosti = append(vrijednosti, *o.v)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(vrijednosti)))
	var wantTrajanje []models.TrajanjeTocka
	for _, pc := range []int{1, 5, 10, 30, 50, 70, 90, 95, 99} {
		wantTrajanje = append(wantTrajanje, models.TrajanjeTocka{Postotak: pc, Vrijednost: vrijednosti[redniBroj(150, pc)-1]})
	}
	if !reflect.DeepEqual(p.Trajanje, wantTrajanje) {
		t.Errorf("trajanje %+v\nočekivano %+v", p.Trajanje, wantTrajanje)
	}
	if p.Max != vrijednosti[0] || p.Min != vrijednosti[len(vrijednosti)-1] {
		t.Errorf("ekstremi %v – %v", p.Min, p.Max)
	}
	// najniže (200) i najviše (289) ponove se 1966., a niz pamti 1965.
	g65, g66 := p.Godine[0], p.Godine[1]
	if g65.MinNa != "1965-12-01" || g65.MaxNa != "1965-12-18" ||
		g66.MinNa != "1966-03-01" || g66.MaxNa != "1966-03-18" {
		t.Errorf("datumi ekstrema godina: %+v", p.Godine)
	}
	if p.MinNa != "1965-12-01" || p.MaxNa != "1965-12-18" {
		t.Errorf("datumi ekstrema niza %q %q", p.MinNa, p.MaxNa)
	}
	if len(p.Mjeseci) != 5 || p.Mjeseci[0].Mjesec != 1 || p.Mjeseci[4].Mjesec != 12 {
		t.Errorf("mjeseci %+v", p.Mjeseci)
	}
}

// Pronos se gomila, pa pregled nosi i zbroj cijelog niza.
func TestPregledNizaPronosSeZbraja(t *testing.T) {
	r := arhivaZaPregled(t, pronosNiz())
	p, err := r.Pregled(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if !p.ZbrojIma || p.Zbroj != 8 {
		t.Errorf("zbroj %v %v, očekivano 8", p.ZbrojIma, p.Zbroj)
	}
	if p.Godine[0].Zbroj != 4 || p.Godine[1].Zbroj != 4 {
		t.Errorf("zbroj po godinama %+v", p.Godine)
	}
	if p.Srednjak != 8.0/3 {
		t.Errorf("srednjak %v", p.Srednjak)
	}
}

// Jedno očitanje je i najniže i najviše; datum se uzima po UTC-u. Mjesna
// zona se namjerno pomiče od UTC-a, jer je CI u UTC-u i tamo bi svaki
// datum prošao. Zato test ne smije biti paralelan.
func TestPregledNizaJednoOcitanje(t *testing.T) {
	mjesna := time.Local
	time.Local = time.FixedZone("CET", 3600)
	t.Cleanup(func() { time.Local = mjesna })
	r := arhivaZaPregled(t, jednoOcitanje())
	p, err := r.Pregled(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []models.HidroGodina{{Godina: 2024, Zapisa: 1, Min: -12, Max: -12,
		MinNa: "2024-02-29", MaxNa: "2024-02-29", Srednjak: -12, Zbroj: -12}}
	if !reflect.DeepEqual(p.Godine, want) {
		t.Errorf("godine %+v", p.Godine)
	}
	if p.Min != -12 || p.Max != -12 || p.MinNa != "2024-02-29" || p.MaxNa != "2024-02-29" || p.Srednjak != -12 {
		t.Errorf("pregled %+v", p)
	}
	if len(p.Mjeseci) != 1 || p.Mjeseci[0].Mjesec != 2 || p.Trajanje != nil {
		t.Errorf("mjeseci %+v, trajanje %+v", p.Mjeseci, p.Trajanje)
	}
}

// Niz bez očitanja vraća samo opis niza; ništa se ne računa.
func TestPregledNizaPrazan(t *testing.T) {
	r := arhivaZaPregled(t, prazanNiz())
	p, err := r.Pregled(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	want := models.HidroPregled{Niz: models.HidroNiz{ID: 5, Sliv: "drava", Letva: "donji-miholjac",
		Izvor: "his2000", Velicina: "temperatura", Vrsta: "satni", Od: "2001-01-01", Do: "2001-12-31"}}
	if !reflect.DeepEqual(*p, want) {
		t.Errorf("pregled %+v\nočekivano %+v", *p, want)
	}
}

func TestPregledNizaKojegNema(t *testing.T) {
	r := arhivaZaPregled(t, satniNiz())
	p, err := r.Pregled(context.Background(), 99)
	if !errors.Is(err, sql.ErrNoRows) || p != nil {
		t.Errorf("niz kojeg nema: %v, %v", p, err)
	}
}

// Čvor bez arhive ima nil repozitorij i pregled bez greške.
func TestPregledNizaBezArhive(t *testing.T) {
	var r *ArhivaRepository
	p, err := r.Pregled(context.Background(), 1)
	if p != nil || err != nil {
		t.Errorf("bez arhive: %v, %v", p, err)
	}
}

// NULL uz pravu vrijednost prođe zbir godine, ali ne i čitanje datuma
// ekstrema; godina samih NULL-ova stane već na zbiru.
func TestPregledNizaSNullVrijednostima(t *testing.T) {
	r := arhivaZaPregled(t, nullUGodini(), samoNull())
	for _, sluc := range []struct {
		niz    int64
		stupac string
	}{{6, `name "vrijednost"`}, {7, `name "min(vrijednost)"`}} {
		p, err := r.Pregled(context.Background(), sluc.niz)
		if p != nil || err == nil || !strings.Contains(err.Error(), sluc.stupac) ||
			!strings.Contains(err.Error(), "NULL") {
			t.Errorf("niz %d: %v, %v", sluc.niz, p, err)
		}
		if err != nil && strings.HasPrefix(err.Error(), "karakteristične vrijednosti") {
			t.Errorf("niz %d: greška čitanja ne nosi oznaku upita: %v", sluc.niz, err)
		}
	}
}

// Arhiva bez tablice očitanja javi koji je upit pao.
func TestPregledNizaBezTabliceOcitanja(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bez.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE nizovi (id INTEGER PRIMARY KEY, sliv TEXT, letva TEXT, izvor TEXT,
		velicina TEXT, vrsta TEXT, po_danu INTEGER, od TEXT, do_ TEXT, zapisa INTEGER, otisak TEXT);
		INSERT INTO nizovi VALUES (1, 'drava', 'osijek', 'his2000', 'vodostaj', 'satni', 0, '', '', 0, '')`); err != nil {
		t.Fatal(err)
	}
	r := &ArhivaRepository{db: db}
	p, err := r.Pregled(context.Background(), 1)
	if p != nil || err == nil || !strings.HasPrefix(err.Error(), "karakteristične vrijednosti: ") {
		t.Errorf("bez očitanja: %v, %v", p, err)
	}
}

// Do datuma ekstrema, godišnjeg hoda i trajanja dolazi se tek kad je zbirni
// upit na istoj tablici već prošao, pa se njihova greška vidi samo na
// zatvorenoj vezi. U Pregledu te grane ostaju nepokrivene: NULL koji bi
// srušio godišnji hod ruši već čitanje datuma ekstrema.
func TestPregledNizaEkstremiNaZatvorenojArhivi(t *testing.T) {
	r := arhivaZaPregled(t, satniNiz())
	r.Close()
	ctx := context.Background()
	if po, err := r.ekstremiPoGodini(ctx, 1); po != nil || err == nil {
		t.Errorf("ekstremi na zatvorenoj arhivi: %v, %v", po, err)
	}
	if m, err := r.mjeseci(ctx, 1); m != nil || err == nil || !strings.HasPrefix(err.Error(), "godišnji hod: ") {
		t.Errorf("godišnji hod na zatvorenoj arhivi: %v, %v", m, err)
	}
	if tr, err := r.trajanje(ctx, 1); tr != nil || err == nil {
		t.Errorf("trajanje na zatvorenoj arhivi: %v, %v", tr, err)
	}
}
