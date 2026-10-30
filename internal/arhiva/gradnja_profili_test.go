package arhiva

import (
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/uvoz/his2000"
)

func putProfila(koren, sliv, letva, datum string) string {
	return filepath.Join(koren, sliv, letva, "profil", letva+"_profil_"+datum+".csv")
}

// Zaključava čitanje poravnanja snimki: pomak s decimalnim zarezom, stupac
// „crtaj” neobavezan i bez obzira na velika slova, letva bez okolnih
// razmaka, a prekratak redak i zaglavlje se preskaču.
func TestPoravnanjaProfilaIzDatoteke(t *testing.T) {
	koren := t.TempDir()
	zapisiDatoteku(t, filepath.Join(koren, "poravnanje-profila.csv"),
		"letva;datum;pomak_m;crtaj\n"+
			"vodograd;2010-03-22;104,5\n"+
			"vodograd;2015-05-05;0;ne\n"+
			"vodograd;2016-06-06;-2.5;DA\n"+
			"vodograd;2017-07-07;1;0\n"+
			"vodograd;2018-08-08;1;false\n"+
			"vodograd;2019-09-09;3;\n"+
			"kratko;2010\n"+
			" rijecica ; 2020-01-01 ;7;true\n")
	got, err := poravnanjaProfila(koren)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Poravnanje{
		"vodograd|2010-03-22": {104.5, true},
		"vodograd|2015-05-05": {0, false},
		"vodograd|2016-06-06": {-2.5, true},
		"vodograd|2017-07-07": {1, false},
		"vodograd|2018-08-08": {1, false},
		"vodograd|2019-09-09": {3, true},
		"rijecica|2020-01-01": {7, true},
	}
	if len(got) != len(want) {
		t.Errorf("poravnanja: %+v", got)
	}
	for k, p := range want {
		if got[k] != p {
			t.Errorf("%s: %+v, očekivano %+v", k, got[k], p)
		}
	}
}

// Zaključava da se neispravno poravnanje javlja greškom s brojem retka, a ne
// preskače: snimka bez poravnanja crta se kao lažan oblik korita.
func TestNeispravnoPoravnanjeProfila(t *testing.T) {
	for _, s := range []struct {
		ime, sadrzaj, greska string
	}{
		{"pomak nije broj", "letva;datum;pomak_m\nvodograd;2010-03-22;desno\n", `redak 2: pomak "desno"`},
		{"crtaj nije da ni ne", "letva;datum;pomak_m;crtaj\nvodograd;2010-03-22;1;mozda\n",
			`redak 2: crtaj "mozda" (očekivano da ili ne)`},
		{"navodnik usred polja", "letva;datum;pomak_m\nvodograd;2010-03-22;1\"5\n", "poravnanje profila:"},
	} {
		koren := t.TempDir()
		zapisiDatoteku(t, filepath.Join(koren, "poravnanje-profila.csv"), s.sadrzaj)
		_, err := poravnanjaProfila(koren)
		greskaSadrzi(t, s.ime, err, s.greska)
	}
	// Stablo koje nije mapa ne znači da poravnanja nema.
	datoteka := filepath.Join(t.TempDir(), "stablo")
	zapisiDatoteku(t, datoteka, "x")
	if p, err := poravnanjaProfila(datoteka); err == nil {
		t.Errorf("poravnanja iz stabla koje nije mapa: %+v bez greške", p)
	}
}

// Zaključava upis snimki korita s poravnanjem: pomak i oznaka „ne crta se”
// ulaze u arhivu i u ispis, sustav kote čita se iza broja, snimka bez točaka
// i druga letva se ne upisuju, a nečitljiva točka se preskače.
func TestProfiliSPoravnanjem(t *testing.T) {
	db := praznaArhiva(t)
	koren := t.TempDir()
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"),
		"# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 121.55 TRST\n"+
			"stacionaza_m;visina_m\n0,0;90,113\n6,74;87,593\nx;y\n1\n14,07;85,763\n")
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2015-05-05"),
		"# poprečni profil korita, mjereno 2015-05-05, vodostaj pri mjerenju 90 cm, kota nule 80.45\n"+
			"stacionaza_m;visina_m\n0;82\n10;79\n")
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2016-06-06"),
		"# poprečni profil korita, mjereno 2016-06-06, vodostaj pri mjerenju 50 cm, kota nule 80.45\n"+
			"stacionaza_m;visina_m\n")
	zapisiDatoteku(t, putProfila(koren, "dunav", "rijecica", "2010-03-22"),
		"# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 10 cm, kota nule 70\n0;71\n")
	poravnanja := map[string]Poravnanje{
		"vodograd|2010-03-22": {PomakM: 104.5, Crtaj: true},
		"vodograd|2015-05-05": {Crtaj: false},
	}

	ispis := uhvatiIspis(t, &os.Stdout, func() {
		if err := profili(db, koren, "vodograd", poravnanja); err != nil {
			t.Fatal(err)
		}
	})
	for _, dio := range []string{
		"profil korita 2010-03-22   3 točaka, vodostaj 174 cm  — pomak +104.5 m",
		"profil korita 2015-05-05   2 točaka, vodostaj 90 cm  — ne crta se",
	} {
		if !strings.Contains(ispis, dio) {
			t.Errorf("ispis ne sadrži %q:\n%s", dio, ispis)
		}
	}
	if strings.Contains(ispis, "2016-06-06") || strings.Contains(ispis, "rijecica") {
		t.Errorf("upisana je snimka bez točaka ili druga letva:\n%s", ispis)
	}

	type snimka struct {
		vodostaj    int
		kota, pomak float64
		sustav      string
		crtaj       int
	}
	procitaj := func(datum string) snimka {
		var s snimka
		if err := db.QueryRow(`SELECT vodostaj, kota_nule, pomak_m, sustav_kote, crtaj FROM profili
			WHERE letva='vodograd' AND datum=?`, datum).Scan(&s.vodostaj, &s.kota, &s.pomak, &s.sustav, &s.crtaj); err != nil {
			t.Fatalf("snimka %s: %v", datum, err)
		}
		return s
	}
	if s := procitaj("2010-03-22"); s != (snimka{174, 121.55, 104.5, "TRST", 1}) {
		t.Errorf("snimka 2010-03-22: %+v", s)
	}
	if s := procitaj("2015-05-05"); s != (snimka{90, 80.45, 0, "", 0}) {
		t.Errorf("snimka 2015-05-05: %+v", s)
	}
	if n := broj(t, db, `SELECT count(*) FROM profili`); n != 2 {
		t.Errorf("upisano %d snimki, očekivane dvije", n)
	}
	var visina float64
	if err := db.QueryRow(`SELECT t.visina FROM profil_tocke t JOIN profili p ON p.id = t.profil
		WHERE p.datum='2010-03-22' AND t.stacionaza=6.74`).Scan(&visina); err != nil || visina != 87.593 {
		t.Errorf("točka 6,74 m: %v (%v)", visina, err)
	}
}

// Zaključava da se snimka korita koja se ne da pročitati ili upisati javlja
// greškom s imenom datoteke ili datumom snimke.
func TestProfiliKojiSeNeDajuUpisati(t *testing.T) {
	ispravna := "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80.45\n" +
		"stacionaza_m;visina_m\n0;90\n6;87\n"

	t.Run("krivi uzorak putanje", func(t *testing.T) {
		db := praznaArhiva(t)
		if err := profili(db, filepath.Join(t.TempDir(), "mapa["), "", nil); !errors.Is(err, filepath.ErrBadPattern) {
			t.Errorf("greška %v, očekivan krivi uzorak", err)
		}
	})
	t.Run("veza na datoteku koje nema", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		put := putProfila(koren, "dunav", "vodograd", "2010-03-22")
		if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(koren, "nema.csv"), put); err != nil {
			t.Fatal(err)
		}
		if err := profili(db, koren, "", nil); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("greška %v, očekivano da datoteke nema", err)
		}
	})
	t.Run("navodnik usred polja", func(t *testing.T) {
		db := praznaArhiva(t)
		koren := t.TempDir()
		zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"), "stacionaza_m;visina_m\n0;9\"0\n")
		greskaSadrzi(t, "navodnik", profili(db, koren, "", nil), "vodograd_profil_2010-03-22.csv:")
	})
	for _, s := range []struct {
		ime, kada, tablica, greska string
		dvaput                     bool
	}{
		{"upis snimke", "INSERT", "profili", "profil vodograd 2010-03-22: ", false},
		{"brisanje starih točaka", "DELETE", "profil_tocke", "namjerni kvar", true},
		{"upis točke", "INSERT", "profil_tocke", "namjerni kvar", false},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := praznaArhiva(t)
			koren := t.TempDir()
			zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"), ispravna)
			uhvatiIspis(t, &os.Stdout, func() {
				if s.dvaput {
					if err := profili(db, koren, "", nil); err != nil {
						t.Fatal(err)
					}
				}
				kvar(t, db, s.kada, s.tablica, "")
				err := profili(db, koren, "", nil)
				greskaSadrzi(t, s.ime, err, s.greska)
				greskaSadrzi(t, s.ime, err, "namjerni kvar")
			})
		})
	}
}

// Zaključava da nečitljiv broj u zaglavlju snimke korita ruši gradnju s
// imenom datoteke. Prije se upisivalo 0 cm ili 0 m, a to su stvarne
// vrijednosti, pa se po njima nije vidjelo da zaglavlje ne valja.
func TestZaglavljeSnimkeSNecitljivimBrojem(t *testing.T) {
	for _, s := range []struct{ ime, zaglavlje string }{
		{"vodostaj", "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju ? cm, kota nule 80.45"},
		{"kota nule", "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule n. p. TRST"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := praznaArhiva(t)
			koren := t.TempDir()
			zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"),
				s.zaglavlje+"\nstacionaza_m;visina_m\n0;90\n6;87\n")
			greskaSadrzi(t, s.ime, profili(db, koren, "", nil),
				"vodograd_profil_2010-03-22.csv: u zaglavlju snimke nije broj: „"+s.ime)
			if n := broj(t, db, `SELECT count(*) FROM profili`); n != 0 {
				t.Errorf("%s: snimka s nečitljivim zaglavljem je upisana", s.ime)
			}
		})
	}
}

// Zaključava da gradnja snimku pročita kako ju je goCOP zapisao uvozom iz
// HIS-a. HIS kotu nule piše i decimalnim zarezom, pa je rezanje zaglavlja po
// zarezu davalo 85 m bez sustava. Snimka bez kote, uz sustav odabran pri
// uvozu, zaustavljala je cijelu gradnju; sad joj kota ostaje 0, kao i prije,
// a sustav se pamti.
func TestSnimkaKakvuJeZapisaoUvoz(t *testing.T) {
	for _, s := range []struct {
		ime, kota, sustav string
		kotaNule          float64
	}{
		{"decimalni zarez", "85,21", "HVRS71", 85.21},
		{"decimalni zarez bez sustava", "80,45", "", 80.45},
		{"točka", "121.55", "TRST", 121.55},
		{"bez kote, sustav odabran", "", "HVRS71", 0},
		{"bez kote i sustava", "", "", 0},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := praznaArhiva(t)
			koren := t.TempDir()
			p := &his2000.Profil{Datum: time.Date(2010, 3, 22, 0, 0, 0, 0, time.UTC), Vodostaj: 174, KotaNule: s.kota, Sustav: s.sustav,
				Tocke: []his2000.Tocka{{Stacionaza: "0", Visina: "90,5"}, {Stacionaza: "6,7", Visina: "87"}}}
			zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-22"), string(his2000.ProfilCSV(p)))
			uhvatiIspis(t, &os.Stdout, func() {
				if err := profili(db, koren, "", nil); err != nil {
					t.Fatal(err)
				}
			})
			var vodostaj int
			var kota float64
			var sustav string
			if err := db.QueryRow(`SELECT vodostaj, kota_nule, sustav_kote FROM profili WHERE letva = 'vodograd'`).Scan(&vodostaj, &kota, &sustav); err != nil {
				t.Fatal(err)
			}
			if vodostaj != 174 || math.Abs(kota-s.kotaNule) > 1e-9 || sustav != s.sustav {
				t.Errorf("vodostaj %d, kota %v %q; želim 174, %v %q", vodostaj, kota, sustav, s.kotaNule, s.sustav)
			}
		})
	}
}

// Zaključava pravilo iz Odluka (13. 10. 2026.): snimka korita kojoj je
// datoteka maknuta iz stabla ne briše se sama, nego ostaje u arhivi i pojavi
// se na popisu bez datoteke, kao i niz. Miče je čovjek (MakniSnimku), a
// ponovna gradnja je poslije ne vraća. Snimka letve koja u stablu nema mapu,
// npr. primljena paketom, na popis ne ide.
func TestMaknutaSnimkaOstajeNaPopisuBezDatoteke(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_his2000_vodostaj_dnevni_2024.csv"),
		"datum;vodostaj_cm\n2024-03-01;120\n")
	staro := putProfila(koren, "dunav", "vodograd", "2010-03-22")
	zapisiDatoteku(t, staro, "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80.45\n0;90\n6;87\n")
	gradi := func(zapisi io.Writer) Izvjestaj {
		t.Helper()
		var iz Izvjestaj
		uhvatiIspis(t, &os.Stdout, func() {
			var err error
			if iz, err = Izgradi(koren, baza, "", zapisi); err != nil {
				t.Fatal(err)
			}
		})
		return iz
	}
	if iz := gradi(nil); len(iz.Sirotani) != 0 {
		t.Fatalf("snimka s datotekom javljena je kao sirotan: %+v", iz.Sirotani)
	}
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Snimka letve primljene paketom: u arhivi je, a mape u stablu nema.
	if _, err := db.Exec(`INSERT INTO profili (letva, datum) VALUES ('rijecica', '2015-05-05')`); err != nil {
		t.Fatal(err)
	}

	// Ispravak datuma u imenu i zaglavlju: stara datoteka nestane, nova dođe.
	if err := os.Remove(staro); err != nil {
		t.Fatal(err)
	}
	zapisiDatoteku(t, putProfila(koren, "dunav", "vodograd", "2010-03-23"),
		"# poprečni profil korita, mjereno 2010-03-23, vodostaj pri mjerenju 174 cm, kota nule 80.45\n0;90\n")
	var ispis strings.Builder
	iz := gradi(&ispis)
	zeli := Sirotan{Letva: "vodograd", Snimka: true, Datum: "2010-03-22", Zapisa: 2}
	if len(iz.Sirotani) != 1 || iz.Sirotani[0] != zeli {
		t.Errorf("gradnja javlja %+v, očekivano %+v", iz.Sirotani, zeli)
	}
	if !strings.Contains(ispis.String(), "vodograd         snimka korita 2010-03-22, 2 točke  SIROTAN — datoteke nema u stablu") {
		t.Errorf("ispis gradnje:\n%s", ispis.String())
	}
	if n := broj(t, db, `SELECT count(*) FROM profili WHERE letva='vodograd'`); n != 2 {
		t.Errorf("u arhivi je %d snimki, a gradnja ne smije sama maknuti nijednu", n)
	}
	if o, err := Sirotani(db, koren); err != nil || len(o) != 1 || o[0] != zeli {
		t.Errorf("popis bez gradnje: %+v, %v", o, err)
	}

	// Snimka koja u stablu ima datoteku i snimka letve bez mape u stablu ne
	// miču se, ni kad je stranica s gumbom ostala otvorena otprije.
	for _, x := range []struct{ letva, datum string }{{"vodograd", "2010-03-23"}, {"rijecica", "2015-05-05"}} {
		if _, err := MakniSnimku(db, baza, koren, x.letva, x.datum); err == nil ||
			!strings.Contains(err.Error(), "nije na popisu snimki bez datoteke") {
			t.Errorf("micanje snimke %s %s: %v", x.letva, x.datum, err)
		}
	}
	if n := broj(t, db, `SELECT count(*) FROM profili`); n != 3 {
		t.Fatalf("odbijeno micanje promijenilo je arhivu: %d snimki", n)
	}
	tocaka, err := MakniSnimku(db, baza, koren, "vodograd", "2010-03-22")
	if err != nil || tocaka != 2 {
		t.Fatalf("micanje snimke: %d točaka, %v", tocaka, err)
	}
	if n := broj(t, db, `SELECT count(*) FROM profili WHERE letva='vodograd'`); n != 1 {
		t.Errorf("nakon micanja u arhivi je %d snimki, a stablo ima jednu", n)
	}
	if n := broj(t, db, `SELECT count(*) FROM profil_tocke WHERE profil NOT IN (SELECT id FROM profili)`); n != 0 {
		t.Errorf("iza maknute snimke ostalo je %d točaka", n)
	}
	if o, err := Sirotani(db, koren); err != nil || len(o) != 0 {
		t.Errorf("nakon micanja popis bez datoteke: %+v, %v", o, err)
	}
	if iz := gradi(nil); len(iz.Sirotani) != 0 {
		t.Errorf("ponovna gradnja vratila je maknutu snimku: %+v", iz.Sirotani)
	}
	if n := broj(t, db, `SELECT count(*) FROM profili WHERE letva='rijecica'`); n != 1 {
		t.Errorf("snimka letve bez mape u stablu je dirana")
	}
	if _, err := MakniSnimku(db, baza, koren, "vodograd", "2010-03-22"); err == nil ||
		!strings.Contains(err.Error(), "snimka korita vodograd od 2010-03-22 nije na popisu snimki bez datoteke") {
		t.Errorf("micanje snimke koje nema: %v", err)
	}
}
