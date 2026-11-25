package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/kisomjeri"
	"gocop/internal/oborine"
	"gocop/internal/prognoza"
	"gocop/internal/sadrzaj"
	"gocop/internal/snijeg"
)

// arhivaIzStabla gradi točku iz stabla i izdaje njezin paket, kao
// poslužitelj čvora.
type arhivaIzStabla struct{ podaci, put, paketi, cvor string }

func (a arhivaIzStabla) IzgradiLetvu(letva string, _ io.Writer) (arhiva.Izvjestaj, error) {
	return arhiva.Izgradi(a.podaci, a.put, letva, io.Discard)
}

func (a arhivaIzStabla) IzdajArhivu(letva string, probno bool, _ io.Writer) (arhiva.IzvjestajIzdanja, error) {
	d, err := sql.Open("sqlite", db.SamoZaCitanje(a.put))
	if err != nil {
		return arhiva.IzvjestajIzdanja{}, err
	}
	defer d.Close()
	return arhiva.Izdaj(d, a.paketi, a.cvor, letva, probno, nil, io.Discard)
}

// bazaZaTest otvori SQLite bazu do kraja testa.
func bazaZaTest(t *testing.T, put string) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// spremisteZaTest je spremište sadržaja u memoriji.
func spremisteZaTest(t *testing.T) *sadrzaj.Spremiste {
	t.Helper()
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// zabranjenIzvor je HTTP prijenos koji broji pozive i ne ide nikamo.
type zabranjenIzvor struct{ n atomic.Int32 }

func (z *zabranjenIzvor) RoundTrip(*http.Request) (*http.Response, error) {
	z.n.Add(1)
	return nil, errors.New("SNOWGRID se s ovog čvora ne zove")
}

// upisiTocku stavi u registar izvedenu točku k-a-1 alpskog međusliva A.
func upisiTocku(t *testing.T, baza *sql.DB) {
	t.Helper()
	if _, err := baza.Exec(`INSERT INTO kisomjeri (code, naziv, sliv, latitude, longitude, tezina, aktivan, created_at, updated_at, vrsta)
		VALUES ('k-a-1', 'Točka A', 'A', 47, 13, 1, 1, datetime('now'), datetime('now'), 'izvedeni')`); err != nil {
		t.Fatal(err)
	}
}

// izmisljeniSnijeg upiše u bazu oborina 25 zima izmišljenog snijega na tlu
// točke k-a-1, do jučer: svaka zima ima vrh oko 200. dana sezone.
func izmisljeniSnijeg(t *testing.T, ob *sql.DB, danas int64) {
	t.Helper()
	tx, err := ob.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for d := prognoza.PocetakSezone(danas) - 25*366; d < danas; d++ {
		pocetak := prognoza.PocetakSezone(d)
		vrh := 100 + 10*float64((pocetak/365)%10)
		v := math.Round(vrh*math.Max(0, 1-math.Abs(float64(d-pocetak-200))/120)*10) / 10
		if _, err := tx.Exec(`INSERT INTO snijeg_na_tlu (kisomjer, dan, swe) VALUES ('k-a-1', ?, ?)`, d, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// slivA je stanje međusliva A.
func slivA(t *testing.T, iz *snijeg.Izvor) snijeg.StanjeSliva {
	t.Helper()
	iz.Zaboravi()
	slivovi, err := iz.Slivovi()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slivovi {
		if s.Oznaka == "A" {
			return s
		}
	}
	t.Fatal("nema međusliva A")
	return snijeg.StanjeSliva{}
}

// Snijeg na tlu između dva čvora, kao vodostaji: SNOWGRID čita samo
// izdavač (kućni server). On preuzeto uloži u stablo kao letvu
// snowgrid-k-a-1, izgradi je i izda paket; drugi čvor (laptop) povijest
// dobije tim paketom i kad točku k-a-1 drži iz svog stabla, a tekuću
// sezonu uz izdanje prognoze, i izvor nijednom ne zove. Pragovi „jednom u
// 3/5/10 zima” i stanje sliva su mu isti kao izdavaču. Brojevi su
// izmišljeni.
func TestSnijegNaTluDvaCvora(t *testing.T) {
	ctx := context.Background()
	sad := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	danas := sad.Unix() / 86400
	bazaA, recA, pbA := cvorZaTest(t, "kucni-server")
	bazaB, recB, pbB := cvorZaTest(t, "laptop")
	upisiTocku(t, bazaA)
	upisiTocku(t, bazaB)
	obA, obB := baziOborina(t), baziOborina(t)
	izmisljeniSnijeg(t, obA, danas)

	// izdavač: ulaganje u stablo, gradnja, paket i kazalo
	dirA := t.TempDir()
	podaci := filepath.Join(dirA, "stablo")
	if err := os.MkdirAll(filepath.Join(podaci, kisomjeri.SlivStabla, "batina"), 0o755); err != nil {
		t.Fatal(err)
	}
	putA, paketiA := filepath.Join(dirA, "vodostaji.db"), filepath.Join(dirA, "pakete")
	spA := spremisteZaTest(t)
	ra := &razmjenaArhive{baza: bazaA, rec: recA, sp: spA, arhivaPut: putA, paketiDir: paketiA,
		javljeno: map[string]bool{}, potakni: make(chan struct{}, 1)}
	naTluA := &oborine.SnijegNaTlu{DB: obA, Tocke: oborine.TockeAlpskihSlivova(bazaA, snijeg.Oznake())}
	u := &snijegUArhivu{naTlu: naTluA, podaci: podaci, razmjenaArh: ra,
		arh: arhivaIzStabla{podaci: podaci, put: putA, paketi: paketiA, cvor: recA.Cvor()}}
	u.ulozi()
	if len(ra.potakni) != 1 {
		t.Error("izdani paket nije potaknuo krug razmjene arhive")
	}
	if n, err := ra.objavi(ctx); err != nil || n != 1 {
		t.Fatalf("objava paketa točke: %d %v", n, err)
	}
	// izdavač izdaje prognozu, a uz nju ide snijeg tekuće sezone
	sat := sad.Unix() / 3600
	ishod := &prognoza.Ishod{Sada: sat, Izdane: []prognoza.Izdana{{Letva: "batina", Velicina: "vodostaj", Izdano: sat,
		Ciljni: sat + 1, Vrijednost: 300, Dolje: 298, Gore: 302, Model: "lanac"}}}
	if err := (&prognoza.Osvjezivac{Baza: pbA, Cvor: "kucni-server"}).Zapisi(ishod); err != nil {
		t.Fatal(err)
	}
	if err := objaviPrognozu(ctx, bazaA, recA, pbA, obA, ishod, "kucni-server"); err != nil {
		t.Fatal(err)
	}

	// razmjena: knjiga s izdavača stiže drugom čvoru
	verzije, err := recA.Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recB.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	// laptop drži točku k-a-1 (ERA5) iz svog stabla i sam preuzima
	// vodostaje, pa paket te točke ne bi ugradio; snijeg je zasebna letva
	putB := filepath.Join(t.TempDir(), "vodostaji.db")
	arhivaB := bazaZaTest(t, putB)
	if err := arhiva.PripremiPraznu(arhivaB); err != nil {
		t.Fatal(err)
	}
	if _, err := arhivaB.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa, do_)
		VALUES ('drava', 'k-a-1', 'openmeteo-era5', 'oborina', 'satni', 9, '2026-11-19')`); err != nil {
		t.Fatal(err)
	}
	spB := spremisteZaTest(t)
	rb := &razmjenaArhive{baza: bazaB, rec: recB, sp: spB, arhivaPut: putB, javljeno: map[string]bool{},
		preuzima: func() bool { return true },
		zeli: func(context.Context, string, string) bool { return true },
		ugradi: func(s *arhiva.Sadrzaj) error {
			d, err := sql.Open("sqlite", putB)
			if err != nil {
				return err
			}
			defer d.Close()
			if err := arhiva.PripremiPraznu(d); err != nil {
				return err
			}
			return arhiva.Ugradi(d, putB, s)
		}}
	ugradnji := 0
	rb.naUgradnju(func() { ugradnji++ })
	rb.primi(ctx)
	z, err := spB.Zeljeni(ctx, 10)
	if err != nil || len(z) != 1 {
		t.Fatalf("drugi čvor traži %d paketa (%v), želim 1", len(z), err)
	}
	bajtovi, vrsta, err := spA.Citaj(ctx, z[0].Otisak)
	if err != nil {
		t.Fatal(err)
	}
	if err := spB.UpisiProvjereno(ctx, z[0].Otisak, vrsta, bajtovi, "kucni-server"); err != nil {
		t.Fatal(err)
	}
	rb.primi(ctx)
	if ugradnji != 1 {
		t.Errorf("nakon ugradnje paketa snijeg se nije računao iznova (%d)", ugradnji)
	}
	snijegStigao := 0
	p := &primateljIzdanja{pb: pbB, ob: obB, rec: recB, izdaje: func() bool { return false }}
	p.naSnijeg(func() { snijegStigao++ })
	p.upisi(verzije)
	if snijegStigao != 1 {
		t.Errorf("snijeg uz izdanje nije javljen (%d)", snijegStigao)
	}

	// drugi čvor: izvor ne zove, a snijeg ima iz arhive i izdanja
	arhA := bazaZaTest(t, db.SamoZaCitanje(putA))
	arhB := bazaZaTest(t, db.SamoZaCitanje(putB))
	izvor := &zabranjenIzvor{}
	naTluB := &oborine.SnijegNaTlu{DB: obB, Arhiva: arhB, Smije: func() bool { return false },
		Klijent: &http.Client{Transport: izvor}, Tocke: oborine.TockeAlpskihSlivova(bazaB, snijeg.Oznake()),
		Cekaj: func(context.Context, time.Duration) error { return context.Canceled }}
	naTluB.Vrti(ctx)
	if izvor.n.Load() != 0 {
		t.Fatalf("drugi čvor zvao je SNOWGRID %d puta", izvor.n.Load())
	}
	izA := izvorSnijega(bazaA, arhA, &oborine.Uvoznik{DB: obA}, &oborine.SnijegNaTlu{DB: obA, Arhiva: arhA})
	izB := izvorSnijega(bazaB, arhB, &oborine.Uvoznik{DB: obB}, naTluB)
	izA.Sad = func() time.Time { return sad }
	izB.Sad = izA.Sad
	a, b := slivA(t, izA), slivA(t, izB)
	if !b.Ima || !b.ImaPragove || b.Pragovi != a.Pragovi || b.Dan != danas-1 || b.Mm != a.Mm || len(b.Sezona) != len(a.Sezona) {
		t.Errorf("drugi čvor: %+v\nizdavač: %+v", b, a)
	}

	// pragovi su iz arhive: bez nje drugi čvor ima samo sezonu iz izdanja
	bezArhive := izvorSnijega(bazaB, nil, &oborine.Uvoznik{DB: obB}, &oborine.SnijegNaTlu{DB: obB})
	bezArhive.Sad = izA.Sad
	if s := slivA(t, bezArhive); !s.Ima || s.ImaPragove || s.Dan != danas-1 {
		t.Errorf("samo izdanje: %+v", s)
	}
	// a bez izdanja samo povijest iz arhive, s istim pragovima
	if _, err := obB.Exec(`DELETE FROM snijeg_na_tlu`); err != nil {
		t.Fatal(err)
	}
	if s := slivA(t, izB); !s.ImaPragove || s.Pragovi != a.Pragovi {
		t.Errorf("samo arhiva: %+v", s)
	}
}

// Čvor koji nije primio ništa nema snijega ni greške: sliv bez stanja, pa
// kartice nema.
func TestSnijegNaTluNistaPrimljeno(t *testing.T) {
	baza, _, _ := cvorZaTest(t, "novi")
	upisiTocku(t, baza)
	ob := baziOborina(t)
	prazna := bazaZaTest(t, filepath.Join(t.TempDir(), "vodostaji.db"))
	iz := izvorSnijega(baza, prazna, &oborine.Uvoznik{DB: ob}, &oborine.SnijegNaTlu{DB: ob, Arhiva: prazna})
	if s := slivA(t, iz); s.Ima || s.ImaPragove || s.Ukljucen || s.PunoZaDoba {
		t.Errorf("bez ičega: %+v", s)
	}
}

// Ulaganje bez stabla ili bez poslužitelja ne radi ništa, a čvor bez stabla
// to javi jednom.
func TestSnijegUArhivuBezStabla(t *testing.T) {
	ob := baziOborina(t)
	naTlu := &oborine.SnijegNaTlu{DB: ob, Tocke: func() ([]oborine.Tocka, error) {
		t.Error("bez stabla se točke ne čitaju")
		return nil, nil
	}}
	(&snijegUArhivu{naTlu: naTlu}).ulozi()
	u := &snijegUArhivu{naTlu: naTlu, podaci: t.TempDir(), arh: &laznaArhiva{}}
	dnevnik := zapisDnevnika(t)
	u.ulozi()
	u.ulozi()
	if n := strings.Count(dnevnik.String(), "nema stabla"); n != 1 {
		t.Errorf("„nema stabla” javljeno %d puta: %q", n, dnevnik.String())
	}
}

// Kiša bez snijega ne baca izračunati snijeg, a kiša koja se ne da upisati
// samo se javi u dnevnik.
func TestPrimiOborineIzdanja(t *testing.T) {
	ob := baziOborina(t)
	javljeno := 0
	p := &primateljIzdanja{ob: ob, snijeg: func() { javljeno++ }}
	satne := prognoza.Tablica{Stupci: []string{"kisomjer", "sat", "oborina", "prognoza", "preuzeto"},
		Redci: [][]any{{"k-a-1", 1000, 1.5, 0, 0}}}
	p.primiOborine("1000", map[string]prognoza.Tablica{"satne": satne})
	snijegTablica := prognoza.Tablica{Stupci: []string{"kisomjer", "dan", "swe"}, Redci: [][]any{{"k-a-1", 41, 120.5}}}
	p.primiOborine("1001", map[string]prognoza.Tablica{"satne": satne, prognoza.TablicaSnijegaNaTlu: snijegTablica})
	if javljeno != 1 {
		t.Errorf("snijeg javljen %d puta, želim 1", javljeno)
	}
	dnevnik := zapisDnevnika(t)
	ob.Close()
	p.primiOborine("1002", map[string]prognoza.Tablica{prognoza.TablicaSnijegaNaTlu: snijegTablica})
	if javljeno != 1 || !strings.Contains(dnevnik.String(), "kiša iz izdanja 1002") {
		t.Errorf("neupisana kiša: javljeno %d, dnevnik %q", javljeno, dnevnik.String())
	}
}
