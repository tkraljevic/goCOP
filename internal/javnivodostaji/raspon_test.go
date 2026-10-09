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
