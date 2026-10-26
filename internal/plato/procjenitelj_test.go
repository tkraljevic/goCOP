package plato

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/skok"
)

// probnaBaza je baza čvora iz predloška s izmišljenim letvama na izmišljenoj
// rijeci: Gornji Brod (km 120), Donji Brod (km 100), Ustava Brod (uz
// ustavu) i Radar Brod bez stacionaže.
func probnaBaza(t *testing.T) *sql.DB {
	t.Helper()
	put := filepath.Join(t.TempDir(), "cvor.db")
	if err := db.UpisiPredlozak(put); err != nil {
		t.Fatal(err)
	}
	baza, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, s := range []struct{ id, sifra, ime, km string }{
		{"g", "gornji-brod", "Gornji Brod", "rkm 120,0"},
		{"d", "donji-brod", "Donji Brod", "rkm 100,0"},
		{"u", "ustava-brod", "Ustava Brod", "rkm 110,0"},
		{"r", "radar-brod", "Radar Brod", ""},
	} {
		if _, err := baza.Exec(`INSERT INTO stations (id, code, name, watercourse_code, stationing, created_at, updated_at)
			VALUES (?, ?, ?, 'rijeka-izmisljena', ?, ?, ?)`, s.id, s.sifra, s.ime, s.km, time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := baza.Exec(`INSERT INTO structures (id, code, name, kind, sector_id, area_id, station_id, created_at, updated_at)
		VALUES ('o', 'cs-brod', 'CS Brod', 'CRPNA_STANICA', 'x', 1, 'r', ?, ?)`, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return baza
}

// upisi upisuje satne vodostaje letve od sata h0 (sat()).
func upisi(t *testing.T, baza *sql.DB, stanica, izvor string, h0 int, cm ...float64) {
	t.Helper()
	for i, v := range cm {
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), stanica, sat(h0+i).UTC(), int(v), izvor, time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

// Registar: susjedi s iste rijeke po stacionaži, bez ustave; dodatni
// susjedi izvana; ime i šifra po ID-u; vodomjer uz crpnu stanicu izuzet.
func TestProcjeniteljRegistar(t *testing.T) {
	ctx := context.Background()
	p := Novi(probnaBaza(t), nil, nil, map[string][]string{"donji-brod": {"druga-obala", "ustava-brod", "gornji-brod"}})
	s, err := p.Svojstva(ctx, "donji-brod")
	if err != nil {
		t.Fatal(err)
	}
	var sifre []string
	for _, n := range s.Susjedi {
		sifre = append(sifre, n.Letva)
	}
	if !slices.Equal(sifre, []string{"gornji-brod", "druga-obala"}) {
		t.Errorf("susjedi %v", sifre)
	}
	if s.Trajanje != ZadanoTrajanje || s.Prag != skok.ZadaniPrag || s.Raspon != nil {
		t.Errorf("bez arhive %+v", s)
	}
	if p.Ime(ctx, "gornji-brod") != "Gornji Brod" || p.Ime(ctx, "nema") != "nema" || p.Sifra(ctx, "d") != "donji-brod" || p.Sifra(ctx, "x") != "" {
		t.Error("ime ili šifra")
	}
	if !p.Izuzeta(ctx, "radar-brod") || !p.Izuzeta(ctx, "ustava-brod") || p.Izuzeta(ctx, "donji-brod") {
		t.Error("izuzeća")
	}
}

// Kvar iz baze čvora: Donji Brod stoji na 412 cm, a Gornji Brod se diže;
// ručno upisano očitanje ne ulazi, a i stranica letve vidi da kvar traje.
// Ustava se ne provjerava. Bez arhive susjed ne potvrđuje (ne zna se kako
// dvije letve idu zajedno).
func TestProcjeniteljKvarIStanje(t *testing.T) {
	ctx := context.Background()
	baza := probnaBaza(t)
	arhiva := probnaArhiva(t, map[string][]Tocka{
		"donji-brod":  godisnjiSvakih(2020, 400, 30, 8, 100),
		"gornji-brod": godisnji(2020, 320, 1, 0),
	})
	upisi(t, baza, "d", models.ReadingSourceImport, 0, spoji([]float64{404, 406, 408, 410}, isto(412, 20))...)
	upisi(t, baza, "d", models.ReadingSourceManual, 30, 380)
	upisi(t, baza, "g", models.ReadingSourceImport, 0, od(300, 2, 24)...)
	upisi(t, baza, "u", models.ReadingSourceImport, 0, isto(50, 20)...)
	bez := Novi(baza, nil, nil, nil)
	bez.sada = func() time.Time { return sat(23).Add(10 * time.Minute) }
	if err := bez.Pripremi(ctx); err != nil {
		t.Fatal(err)
	}
	if k, err := bez.Stanje(ctx, "donji-brod"); err != nil || k != nil {
		t.Errorf("bez arhive %+v %v", k, err)
	}
	p := Novi(baza, arhiva, nil, nil)
	p.sada = bez.sada
	// dok svojstva nisu naučena stranica ne čeka: nil, a uči se u pozadini
	if k, err := p.Stanje(ctx, "donji-brod"); err != nil || k != nil {
		t.Errorf("prije učenja %+v %v", k, err)
	}
	p.uciUPozadini("donji-brod") // drugi poziv dok uči ne pokreće novo učenje
	for rok := time.Now().Add(5 * time.Second); ; {
		if _, ima := p.zapamcena(p.svojstva, "donji-brod"); ima || time.Now().After(rok) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	tocke, err := p.Ocitanja(ctx, "donji-brod", sat(0), sat(40))
	if err != nil || len(tocke) != 24 {
		t.Fatalf("očitanja %d %v", len(tocke), err)
	}
	k, err := p.Stanje(ctx, "donji-brod")
	if err != nil || k == nil || k.Susjed == nil || k.Susjed.Letva != "gornji-brod" || k.Cm != 412 {
		t.Fatalf("stanje %+v %v", k, err)
	}
	// zapamćeno stanje vrijedi i kad se baza promijeni
	upisi(t, baza, "d", models.ReadingSourceImport, 24, 300)
	if k2, err := p.Stanje(ctx, "donji-brod"); err != nil || k2 != k {
		t.Error("stanje nije zapamćeno")
	}
	if err := p.Pripremi(ctx); err != nil {
		t.Fatal(err)
	}
	if k, err := p.Stanje(ctx, "ustava-brod"); err != nil || k != nil {
		t.Errorf("ustava %+v %v", k, err)
	}
	if k, err := p.Kvarovi(ctx, "donji-brod", nil); err != nil || k != nil {
		t.Errorf("bez točaka %+v %v", k, err)
	}
}

// S arhivom: trajanje, raspon i prag susjeda naučeni i zapamćeni; susjed u
// vlastitom kvaru ne potvrđuje. Arhiva koja se ne da čitati daje zadane
// vrijednosti i ne pamti se.
func TestProcjeniteljSArhivom(t *testing.T) {
	ctx := context.Background()
	arhiva := probnaArhiva(t, map[string][]Tocka{
		"donji-brod":  godisnjiSvakih(2020, 100, 30, 8, 100),
		"gornji-brod": godisnji(2020, 50, 1, 0),
	})
	baza := probnaBaza(t)
	p := Novi(baza, arhiva, skok.NoviPragovi(arhiva), nil)
	s, err := p.Svojstva(ctx, "donji-brod")
	if err != nil || s.Raspon == nil || len(s.Susjedi) != 1 || s.Susjedi[0].Prag != NajmanjiPomakSusjeda || s.Susjedi[0].Raspon == nil {
		t.Fatalf("svojstva %+v %v", s, err)
	}
	if v := p.Vlastita(ctx, "gornji-brod"); v.Raspon == nil {
		t.Error("vlastita susjeda nisu zapamćena")
	}
	// susjed u kvaru: skoči na −372 i stoji, pa se „pomakne” 420 cm
	upisi(t, baza, "d", models.ReadingSourceImport, 0, isto(100, 30)...)
	upisi(t, baza, "g", models.ReadingSourceImport, 0, spoji(isto(48, 6), isto(-372, 24))...)
	tocke, err := p.Ocitanja(ctx, "donji-brod", sat(0), sat(40))
	if err != nil {
		t.Fatal(err)
	}
	if k, err := p.Kvarovi(ctx, "donji-brod", tocke); err != nil || len(k) != 0 {
		t.Errorf("susjed u kvaru potvrdio: %+v %v", k, err)
	}
	if _, err := arhiva.Exec(`DROP TABLE spoj`); err != nil {
		t.Fatal(err)
	}
	p2 := Novi(baza, arhiva, nil, nil)
	if s, err := p2.Svojstva(ctx, "donji-brod"); err != nil || s.Raspon != nil || !slicesInf(s.Susjedi) {
		t.Errorf("bez arhive %+v", s)
	}
	if _, ima := p2.zapamcena(p2.svojstva, "donji-brod"); ima {
		t.Error("zadana svojstva zapamćena")
	}
	if v := p2.Vlastita(ctx, "donji-brod"); v.Trajanje != ZadanoTrajanje {
		t.Errorf("vlastita bez arhive %+v", v)
	}
}

func slicesInf(s []Susjed) bool {
	return len(s) > 0 && !slices.ContainsFunc(s, func(x Susjed) bool { return x.Prag != BezPraga })
}

// Greške baze se vraćaju.
func TestProcjeniteljGreskaBaze(t *testing.T) {
	ctx := context.Background()
	baza := probnaBaza(t)
	p := Novi(baza, nil, nil, nil)
	if _, err := baza.Exec(`DROP TABLE readings`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Svojstva(ctx, "donji-brod"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stanje(ctx, "donji-brod"); err == nil {
		t.Error("stanje bez tablice očitanja")
	}
	if err := p.Pripremi(ctx); err == nil {
		t.Error("priprema bez tablice očitanja")
	}
	if _, err := p.Kvarovi(ctx, "donji-brod", niz(0, 1, 2)); err == nil {
		t.Error("kvarovi bez očitanja susjeda")
	}
	if _, err := baza.Exec(`DROP TABLE structures`); err != nil {
		t.Fatal(err)
	}
	p = Novi(baza, nil, nil, nil)
	if _, err := p.Svojstva(ctx, "donji-brod"); err == nil {
		t.Error("svojstva bez registra")
	}
	if p.Izuzeta(ctx, "ustava-brod") || p.Sifra(ctx, "d") != "" || p.Ime(ctx, "donji-brod") != "donji-brod" {
		t.Error("bez registra")
	}
	if _, err := p.Kvarovi(ctx, "donji-brod", niz(0, 1, 2)); err == nil {
		t.Error("kvarovi bez registra")
	}
	p.uciUPozadini("donji-brod") // greška učenja samo se zabilježi
	for rok := time.Now().Add(5 * time.Second); time.Now().Before(rok); time.Sleep(10 * time.Millisecond) {
		p.mu.Lock()
		gotovo := !p.uci["donji-brod"]
		p.mu.Unlock()
		if gotovo {
			break
		}
	}
}

// Priprema staje kad se kontekst prekine.
func TestPripremiPrekid(t *testing.T) {
	baza := probnaBaza(t)
	upisi(t, baza, "d", models.ReadingSourceImport, 0, 100, 101)
	ctx, prekini := context.WithCancel(context.Background())
	prekini()
	if err := Novi(baza, nil, nil, nil).Pripremi(ctx); err == nil {
		t.Error("prekinuta priprema bez greške")
	}
}

// Na pune sate: najbliže očitanje unutar pola sata, od jednako udaljenih
// ranije; lažni skok izbačen.
func TestPoSatuIBezSkokova(t *testing.T) {
	ulaz := []Tocka{{sat(1).Add(-20 * time.Minute), 10}, {sat(1).Add(10 * time.Minute), 11}, {sat(2).Add(-15 * time.Minute), 12},
		{sat(2).Add(15 * time.Minute), 13}, {sat(3), 900}, {sat(4), 14}, {sat(5), 15}}
	got := BezSkokova(PoSatu(ulaz), 50)
	want := []float64{11, 12, 14, 15}
	if len(got) != len(want) {
		t.Fatalf("sati %+v", got)
	}
	for i, x := range got {
		if x.Cm != want[i] || x.Kad.Minute() != 0 {
			t.Errorf("sat %d: %+v", i, x)
		}
	}
}

// Km iz stacionaže, susjedi iz registra.
func TestKmISusjediIzRegistra(t *testing.T) {
	for in, want := range map[string]float64{"rkm 1.380,30": 1380.30, "rkm 1424+850": 1424.85, "km 4,045": 4.045, "pkm 23,37": 23.37, "rkm 12": 12, " RKM 7,5 ": 7.5} {
		if got, ok := Km(in); !ok || got != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"nkm 19,55", "", "kod mosta"} {
		if _, ok := Km(in); ok {
			t.Errorf("%q se pročitao", in)
		}
	}
	st := []Stanica{
		{Sifra: "a", Voda: "r", Stacionaza: "km 10"}, {Sifra: "b", Voda: "r", Stacionaza: "km 20"},
		{Sifra: "c", Voda: "r", Stacionaza: "km 25"}, {Sifra: "d", Voda: "r", Stacionaza: "km 30"},
		{Sifra: "e", Voda: "r", Stacionaza: "km 200"}, {Sifra: "f", Voda: "s", Stacionaza: "km 21"},
		{Sifra: "he-g", Voda: "r", Stacionaza: "km 19"}, {Sifra: "bez", Voda: "r"}, {Sifra: "bezvode", Stacionaza: "km 5"},
	}
	if got := SusjediIzRegistra("b", st); !slices.Equal(got, []string{"c", "d", "a"}) {
		t.Errorf("susjedi %v", got)
	}
	for _, s := range []string{"bez", "bezvode", "nema"} {
		if got := SusjediIzRegistra(s, st); got != nil {
			t.Errorf("%s: %v", s, got)
		}
	}
	if !(Stanica{Sifra: "borovik"}).Izuzeta() || !(Stanica{Sifra: "x", KotaNM: true}).Izuzeta() || (Stanica{Sifra: "x"}).Izuzeta() {
		t.Error("izuzeća")
	}
}

// upisiNiz je upisi u jednoj transakciji, za duge nizove.
func upisiNiz(t *testing.T, baza *sql.DB, stanica string, h0 int, cm ...float64) {
	t.Helper()
	tx, err := baza.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range cm {
		if _, err := tx.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), stanica, sat(h0+i).UTC(), int(v), models.ReadingSourceImport, time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// Kvar dulji od prozora: točke zadnjih 20 sati leže usred kvara, pa se
// njegov početak traži u starijim očitanjima, dva tjedna po dva. Kvar počinje
// skokom sa 100 cm, a ne prvom točkom, i nije „barem od”.
func TestKvarPocinjePrijeProzora(t *testing.T) {
	ctx := context.Background()
	baza := probnaBaza(t)
	arhiva := probnaArhiva(t, map[string][]Tocka{"donji-brod": godisnjiSvakih(2020, 100, 30, 8, 100)})
	upisiNiz(t, baza, "d", -30, spoji(isto(100, 30), isto(1000, 401))...)
	p := Novi(baza, arhiva, nil, nil)
	tocke, err := p.Ocitanja(ctx, "donji-brod", sat(380), sat(400))
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Kvarovi(ctx, "donji-brod", tocke)
	if err != nil || len(k) != 1 {
		t.Fatalf("kvarovi %+v %v", k, err)
	}
	if !k[0].Od.Equal(sat(0)) || !k[0].ImaPrije || k[0].Prije != 100 || k[0].BaremOd || !k[0].Traje {
		t.Errorf("kvar %+v", k[0])
	}
}

// Očitanje malo prije prve točke koje se svede na njezin sat nije starije
// očitanje: traženje početka tu staje, a ne vrti se ukrug.
func TestPocetakBezStarijih(t *testing.T) {
	ctx := context.Background()
	baza := probnaBaza(t)
	upisiNiz(t, baza, "d", 0, isto(100, 10)...)
	if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, created_at, updated_at)
		VALUES ('ranije', 'd', ?, 100, ?, ?, ?)`, sat(0).Add(-20*time.Minute).UTC(), models.ReadingSourceImport, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	p := Novi(baza, nil, nil, nil)
	tocke := niz(0, isto(100, 10)...)
	got, nedovrseno, err := p.sPocetkom(ctx, "donji-brod", tocke, Svojstva{})
	if err != nil || nedovrseno || len(got) != 10 {
		t.Errorf("početak %d %v %v", len(got), nedovrseno, err)
	}
}

// Kvar stariji od NajdaljeUnatrag opisan je od te granice, „barem od”.
func TestKvarBaremOd(t *testing.T) {
	ctx := context.Background()
	baza := probnaBaza(t)
	arhiva := probnaArhiva(t, map[string][]Tocka{"donji-brod": godisnjiSvakih(2020, 100, 30, 8, 100)})
	sati := int(NajdaljeUnatrag.Hours()) + 200
	upisiNiz(t, baza, "d", -sati, isto(1000, sati+1)...)
	p := Novi(baza, arhiva, nil, nil)
	tocke, err := p.Ocitanja(ctx, "donji-brod", sat(-20), sat(0))
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Kvarovi(ctx, "donji-brod", tocke)
	if err != nil || len(k) != 1 || !k[0].BaremOd || !k[0].Od.Equal(sat(0).Add(-NajdaljeUnatrag)) {
		t.Fatalf("kvarovi %+v %v", k, err)
	}
	if got := Opis(k[0], imena); !strings.HasPrefix(got, "mjerenje barem od ") {
		t.Errorf("opis %q", got)
	}
}

// Izuzeća po imenu: ustava, brana, akumulacija, crpna stanica i elektrana
// kao cijela riječ; Branjin Vrh nije brana.
func TestIzuzetaPoImenu(t *testing.T) {
	for _, ime := range []string{"Sió, ustava (nizvodno)", "Brod, brana na Lajti", "Razina akumulacije Brod", "CS i ustava Brod", "HE Brod"} {
		if !(Stanica{Sifra: "x", Ime: ime}).Izuzeta() {
			t.Errorf("%q nije izuzeta", ime)
		}
	}
	for _, ime := range []string{"Branjin Vrh", "Donji Brod", "Heršak", "Ustavčica"} {
		if (Stanica{Sifra: "x", Ime: ime}).Izuzeta() {
			t.Errorf("%q je izuzeta", ime)
		}
	}
}

// Dok jedan uči iz arhive, drugi čeka i uzme ono što je prvi naučio, a ne
// uči iznova.
func TestUcenjeJednom(t *testing.T) {
	ctx := context.Background()
	p := Novi(probnaBaza(t), nil, nil, nil)
	naucena := Svojstva{Trajanje: 77 * time.Hour}
	p.uciMu.Lock()
	svojstva, vlastita := make(chan Svojstva), make(chan Svojstva)
	go func() {
		s, _ := p.Svojstva(ctx, "donji-brod") // greške nema: registar je tu
		svojstva <- s
	}()
	go func() { vlastita <- p.Vlastita(ctx, "gornji-brod") }()
	time.Sleep(50 * time.Millisecond)
	p.zapamti(p.svojstva, "donji-brod", naucena)
	p.zapamti(p.vlastita, "gornji-brod", naucena)
	p.uciMu.Unlock()
	if s, v := <-svojstva, <-vlastita; s.Trajanje != naucena.Trajanje || v.Trajanje != naucena.Trajanje {
		t.Errorf("naučeno iznova: %+v %+v", s, v)
	}
	if (Kvar{Od: sat(4), Do: sat(6)}).SatiDoPovratka() != 2 {
		t.Error("bez povratka")
	}
}
