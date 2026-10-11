package javnivodostaji

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
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

// Zaglavljeno mjerilo: telemetrija satima javlja −1102 cm
const tablicaZaglavljena = `<table><tr><th scope="col">DATUM</th><th scope="col">VRIJEME</th><th scope="col"><span>VODOSTAJ</span></th><th scope="col">TREND</th></tr>
<tr> <td>18.09.2026.</td> <td>11:00 h</td> <td>-1102 cm</td> <td><div class="blue">0</div></td> </tr>
<tr> <td>18.09.2026.</td> <td>10:00 h</td> <td>-1102 cm</td> <td><div class="blue">0</div></td> </tr>
<tr> <td>18.09.2026.</td> <td>09:00 h</td> <td>-121 cm</td> <td><div class="blue">0</div></td> </tr>
</table>`

// preuzimanjeUBazu je uvoznik nad pravom bazom čvora, s letvom čija
// telemetrija javlja zaglavljeno mjerilo
type preuzimanjeUBazu struct {
	baza     *sql.DB
	ocitanja *repository.ReadingRepository
	u        *Uvoznik
	st       *models.Station
}

func novoPreuzimanjeUBazu(t *testing.T) *preuzimanjeUBazu {
	t.Helper()
	db.UseRepoImenik()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "preuzimanje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cvor-p")
	st := &models.Station{ID: uuid.New(), Name: "Pivnica Probna", Code: "pivnica-probna",
		JavniURL: "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424", JavniUvoz: true}
	if err := repository.NewStationRepository(baza, rec).CreateStation(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	ocitanja := repository.NewReadingRepository(baza, rec)
	u := NoviUvoznik(repository.NewJavniSpremiste(baza, ocitanja), nil)
	u.Client.HTTP = &http.Client{Transport: probniPrijenos{tablicaZaglavljena}}
	return &preuzimanjeUBazu{baza: baza, ocitanja: ocitanja, u: u, st: st}
}

// broj vraća broj redaka koje upit izbroji
func (p *preuzimanjeUBazu) broj(t *testing.T, upit string, args ...any) int {
	t.Helper()
	var n int
	if err := p.baza.QueryRow(upit, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Odluke (8. 10. 2026.), pitanje 21: satno preuzimanje ne upisuje vrijednost
// izvan raspona ručnog upisa. Odbačeno se broji u stanju preuzimanja i
// bilježi za stranicu letve, a sljedeći krug isti prozor ne broji dvaput.
func TestPreuzimanjeOdbacujeVrijednostiIzvanRaspona(t *testing.T) {
	p := novoPreuzimanjeUBazu(t)
	ctx := context.Background()

	s := p.u.Preuzmi(ctx, p.st)
	if s.Greska != "" || s.Preuzeto != 3 || s.Novih != 1 || s.Odbaceno != 2 {
		t.Fatalf("prvi krug: %+v", s)
	}
	if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND level_cm < -500`, p.st.ID.String()); n != 0 {
		t.Errorf("upisano %d vrijednosti izvan raspona", n)
	}
	sazetak, err := p.ocitanja.SazetakOdbacenih(ctx, "station:"+p.st.ID.String())
	if err != nil || sazetak == nil || sazetak.Broj != 2 || sazetak.Zadnja.Opis != "−1102 cm" || sazetak.Zadnja.Izvor != Podrijetlo ||
		!sazetak.Zadnja.Kad.Equal(time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("sažetak: %+v, %v", sazetak, err)
	}

	// sljedeći sat stranica pokazuje isti prozor: odbačeno se opet javi u
	// stanju kruga, ali se u bazi ne broji dvaput
	s = p.u.Preuzmi(ctx, p.st)
	if s.Novih != 0 || s.Odbaceno != 2 {
		t.Errorf("drugi krug: %+v", s)
	}
	if sazetak, _ := p.ocitanja.SazetakOdbacenih(ctx, "station:"+p.st.ID.String()); sazetak == nil || sazetak.Broj != 2 {
		t.Errorf("drugi krug udvostručio je odbačene: %+v", sazetak)
	}
}

// Kad se odbačena vrijednost ne može zabilježiti, ne prolazi ni ostatak
// preuzimanja: stanje letve javlja grešku upisa, a ništa se ne upiše, pa ni
// ispravno očitanje iz istog kruga (sljedeći krug ga donese ponovo). Inače bi
// mjerilo u kvaru tiho nestalo sa stranice letve.
func TestPreuzimanjeJavljaGreskuKadSeOdbacenoNeMozeZabiljeziti(t *testing.T) {
	p := novoPreuzimanjeUBazu(t)
	if _, err := p.baza.Exec(`CREATE TRIGGER odbij_biljezenje BEFORE INSERT ON odbacena_ocitanja
		BEGIN SELECT RAISE(ABORT, 'bilježenje odbijeno'); END`); err != nil {
		t.Fatal(err)
	}
	var zapisnik []string
	p.u.Zapisnik = func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) }

	s := p.u.Preuzmi(context.Background(), p.st)
	if !strings.Contains(s.Greska, "bilježenje odbijeno") || s.Novih != 0 || s.UkupnoNovo != 0 || s.Odbaceno != 2 {
		t.Errorf("stanje letve: %+v", s)
	}
	if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ?`, p.st.ID.String()); n != 0 {
		t.Errorf("upisano %d očitanja iz poništene serije", n)
	}
	if len(zapisnik) != 1 || !strings.Contains(zapisnik[0], "Pivnica Probna: upis: ") {
		t.Errorf("dnevnik: %q", zapisnik)
	}
}

// Letva akumulacije broji po rasponu kote, ostale po rasponu vodostaja
func TestNovaOcitanjaBrojiPoRasponuLetve(t *testing.T) {
	kad := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
	svi := []izRedak{
		{Redak{Kad: kad, LevelCm: intPtr(19089)}, "mletva.voda.hr"},
		{Redak{Kad: kad.Add(time.Hour), LevelCm: intPtr(150)}, "mletva.voda.hr"},
		{Redak{Kad: kad.Add(time.Hour), LevelCm: intPtr(151)}, "mletva.voda.hr"}, // isti sat drugi put
	}
	akumulacija := &models.Station{ID: uuid.New(), KotaNM: true}
	nova, odbaceno := novaOcitanja(akumulacija, svi, map[int64]bool{}, time.Now())
	if len(nova) != 2 || odbaceno != 1 {
		t.Errorf("akumulacija: %d novih, %d odbačeno", len(nova), odbaceno)
	}
	obicna := &models.Station{ID: uuid.New()}
	nova, odbaceno = novaOcitanja(obicna, svi, map[int64]bool{kad.Unix(): false}, time.Now())
	if len(nova) != 2 || odbaceno != 1 {
		t.Errorf("obična letva: %d novih, %d odbačeno", len(nova), odbaceno)
	}
	// što letva već ima, ne slaže se ni ne broji
	nova, odbaceno = novaOcitanja(obicna, svi, map[int64]bool{kad.Unix(): true}, time.Now())
	if len(nova) != 1 || odbaceno != 0 {
		t.Errorf("s postojećim: %d novih, %d odbačeno", len(nova), odbaceno)
	}
}

// probniIzvor daje zadane retke pod zadanim nazivom izvora (npr. vizugy.hu)
type probniIzvor struct {
	naziv string
	redci []Redak
}

func (p probniIzvor) Naziv() string                                     { return p.naziv }
func (p probniIzvor) Prepoznaje(string) bool                            { return true }
func (p probniIzvor) Ocitanja(context.Context, string) ([]Redak, error) { return p.redci, nil }

// danubeHISRedci je stranica DanubeHIS-a sa satnim vodostajima u zadanim
// trenucima
func danubeHISRedci(cm map[time.Time]int) string {
	var b strings.Builder
	for kad, v := range cm {
		fmt.Fprintf(&b, `<td class="views-field views-field-time" > %s </td>
<td class="views-field views-field-value views-align-right" > %d </td>
<td class="views-field views-field-unit" > cm </td>
<td class="views-field views-field-description" > Current water level </td>`, kad.In(models.Zagreb).Format("2006-01-02 15:04"), v)
	}
	return b.String()
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): rezerva DanubeHIS popunjava i
// sate čiju je vrijednost s vizugy.hu provjera raspona odbacila. Prozor
// rezerve počinje iza zadnjeg sata koji vizugy daje u rasponu, a ne iza
// zadnjeg retka, i odbačeno očitanje ne zauzima sat. Prije su takvi sati
// ostajali prazni iako ih je DanubeHIS imao.
func TestRezervaDanubeHISPopunjavaSateOdbaceneSVizugyja(t *testing.T) {
	p := novoPreuzimanjeUBazu(t)
	ctx := context.Background()
	mohacs := &models.Station{ID: uuid.New(), Name: "Mohács (HU)", Code: "mohacs",
		JavniURL: "https://www.vizugy.hu/?AllomasVOA=probna", JavniUvoz: true}
	if err := repository.NewStationRepository(p.baza, ledger.New(p.baza, "cvor-p")).CreateStation(ctx, mohacs); err != nil {
		t.Fatal(err)
	}
	sat := time.Now().UTC().Truncate(time.Hour)
	p.u.Izvori = []Izvor{probniIzvor{naziv: PodrijetloVizugy, redci: []Redak{
		{Kad: sat.Add(-6 * time.Hour), LevelCm: intPtr(300)},
		{Kad: sat.Add(-5 * time.Hour), LevelCm: intPtr(301)},
		{Kad: sat.Add(-4 * time.Hour), LevelCm: intPtr(-9999)},
		{Kad: sat.Add(-3 * time.Hour), LevelCm: intPtr(-9999)},
	}}}
	p.u.Client.HTTP = &http.Client{Transport: probniPrijenos{danubeHISRedci(map[time.Time]int{
		sat.Add(-5 * time.Hour): 301, sat.Add(-4 * time.Hour): 302, sat.Add(-3 * time.Hour): 303, sat.Add(-2 * time.Hour): 304,
	})}}

	s := p.u.Preuzmi(ctx, mohacs)
	if s.Greska != "" || s.Novih != 5 || s.Odbaceno != 2 {
		t.Fatalf("preuzimanje: %+v", s)
	}
	for _, x := range []struct {
		sati  int
		cm    int
		izvor string
	}{{6, 300, PodrijetloVizugy}, {5, 301, PodrijetloVizugy}, {4, 302, PodrijetloDanubeHIS}, {3, 303, PodrijetloDanubeHIS}, {2, 304, PodrijetloDanubeHIS}} {
		kad := sat.Add(-time.Duration(x.sati) * time.Hour)
		if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND measured_at = ? AND level_cm = ? AND origin = ?`,
			mohacs.ID.String(), kad, x.cm, x.izvor); n != 1 {
			t.Errorf("%s: %d očitanja %d cm s %s", kad.Format(time.RFC3339), n, x.cm, x.izvor)
		}
	}
	sazetak, err := p.ocitanja.SazetakOdbacenih(ctx, "station:"+mohacs.ID.String())
	if err != nil || sazetak == nil || sazetak.Broj != 2 || sazetak.Zadnja.Izvor != PodrijetloVizugy {
		t.Errorf("sažetak: %+v, %v", sazetak, err)
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): vizugy.hu uz vodostaj daje
// protok preračunat krivuljom iz tog vodostaja. Kad je vodostaj izvan
// raspona, s njim otpada i protok: inače bi se upisao protok iz krivog
// vodostaja kao mjeren, sat bi bio zauzet, a rezerva DanubeHIS ga ne bi
// popunila.
func TestRezervaDanubeHISPopunjavaSateKadVizugyUzOdbaceniVodostajDajeProtok(t *testing.T) {
	p := novoPreuzimanjeUBazu(t)
	ctx := context.Background()
	mohacs := &models.Station{ID: uuid.New(), Name: "Mohács (HU)", Code: "mohacs",
		JavniURL: "https://www.vizugy.hu/?AllomasVOA=probna", JavniUvoz: true}
	if err := repository.NewStationRepository(p.baza, ledger.New(p.baza, "cvor-p")).CreateStation(ctx, mohacs); err != nil {
		t.Fatal(err)
	}
	sat := time.Now().UTC().Truncate(time.Hour)
	redak := func(sati, cm int) Redak {
		return Redak{Kad: sat.Add(-time.Duration(sati) * time.Hour), LevelCm: intPtr(cm), FlowM3s: floatPtr(10),
			FlowMetoda: models.FlowMethodKrivulja, FlowBiljeska: "preračunat iz vodostaja krivuljom mađarske službe, s vizugy.hu"}
	}
	p.u.Izvori = []Izvor{probniIzvor{naziv: PodrijetloVizugy, redci: []Redak{redak(6, 300), redak(5, 301), redak(4, -9999), redak(3, -9999)}}}
	p.u.Client.HTTP = &http.Client{Transport: probniPrijenos{danubeHISRedci(map[time.Time]int{
		sat.Add(-5 * time.Hour): 301, sat.Add(-4 * time.Hour): 302, sat.Add(-3 * time.Hour): 303, sat.Add(-2 * time.Hour): 304,
	})}}

	s := p.u.Preuzmi(ctx, mohacs)
	if s.Greska != "" || s.Novih != 5 || s.Odbaceno != 2 {
		t.Fatalf("preuzimanje: %+v", s)
	}
	if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND level_cm IS NULL`, mohacs.ID.String()); n != 0 {
		t.Errorf("upisano %d očitanja s protokom iz odbačenog vodostaja", n)
	}
	for _, x := range []struct {
		sati  int
		cm    int
		izvor string
	}{{6, 300, PodrijetloVizugy}, {5, 301, PodrijetloVizugy}, {4, 302, PodrijetloDanubeHIS}, {3, 303, PodrijetloDanubeHIS}, {2, 304, PodrijetloDanubeHIS}} {
		kad := sat.Add(-time.Duration(x.sati) * time.Hour)
		if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND measured_at = ? AND level_cm = ? AND origin = ?`,
			mohacs.ID.String(), kad, x.cm, x.izvor); n != 1 {
			t.Errorf("%s: %d očitanja %d cm s %s", kad.Format(time.RFC3339), n, x.cm, x.izvor)
		}
	}
}

// Zadnji sat izvora za prozor rezerve je zadnji koji bi se upisao
func TestZadnjeUpisivo(t *testing.T) {
	sat := time.Now().UTC().Truncate(time.Hour)
	st := &models.Station{ID: uuid.New()}
	redci := []Redak{{Kad: sat.Add(-3 * time.Hour), LevelCm: intPtr(300)}, {Kad: sat.Add(-2 * time.Hour), LevelCm: intPtr(-9999)}}
	if z := zadnjeUpisivo(st, redci, PodrijetloVizugy, time.Now()); !z.Equal(sat.Add(-3 * time.Hour)) {
		t.Errorf("zadnje: %v", z)
	}
	if z := zadnjeUpisivo(st, redci[1:], PodrijetloVizugy, time.Now()); !z.IsZero() {
		t.Errorf("bez upisivog: %v", z)
	}
	// protok iz krivulje ne čini upisivim redak s vodostajem izvan raspona
	redci[1].FlowM3s = floatPtr(10)
	if z := zadnjeUpisivo(st, redci, PodrijetloVizugy, time.Now()); !z.Equal(sat.Add(-3 * time.Hour)) {
		t.Errorf("zadnje uz protok: %v", z)
	}
	if z := zadnjeUpisivo(st, nil, PodrijetloVizugy, time.Now()); !z.IsZero() {
		t.Errorf("bez redaka: %v", z)
	}
}
